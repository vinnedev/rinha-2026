"""Export sklearn DT or RandomForest into a Go-readable packed binary.

DT layout (V1):
    Header (12 bytes):
      magic    uint32 = 0x54524545 ('TREE')
      version  uint32 = 1
      n_nodes  uint32
    Then n_nodes × 20 bytes:
      feature  int16   (-1 if leaf)
      _pad     int16
      threshold float32
      left     int32   (-1 if leaf)
      right    int32   (-1 if leaf)
      proba    float32 (fraud probability at leaf)

RF layout (V2):
    Header (16 bytes):
      magic    uint32 = 0x54524545
      version  uint32 = 2
      n_trees  uint32
      total_nodes uint32
    Then n_trees × tree_header (4 bytes):
      n_nodes_in_tree uint32
    Then for each tree: n_nodes_in_tree × 20 bytes (same layout as V1)

Go-side averaging: walk each tree's root, accumulate proba, divide by n_trees.
"""

from __future__ import annotations

import argparse
import struct
import sys
from pathlib import Path

import joblib
from sklearn.ensemble import RandomForestClassifier
from sklearn.tree import DecisionTreeClassifier

MAGIC = 0x54524545
VERSION_DT = 1
VERSION_RF = 2
NODE_BYTES = 20


def encode_tree(t, fraud_idx: int) -> bytes:
    n = int(t.node_count)
    buf = bytearray(n * NODE_BYTES)
    for i in range(n):
        feature = int(t.feature[i])
        threshold = float(t.threshold[i])
        left = int(t.children_left[i])
        right = int(t.children_right[i])
        value = t.value[i].flatten()
        total = float(value.sum())
        proba = float(value[fraud_idx] / total) if total > 0 else 0.0
        feat_out = -1 if left == -1 else feature
        off = i * NODE_BYTES
        struct.pack_into("<hh", buf, off, feat_out, 0)
        struct.pack_into("<f", buf, off + 4, threshold)
        struct.pack_into("<ii", buf, off + 8, left, right)
        struct.pack_into("<f", buf, off + 16, proba)
    return bytes(buf)


def export_dt(clf: DecisionTreeClassifier, out_path: Path) -> dict:
    fraud_idx = list(clf.classes_).index(1)
    t = clf.tree_
    body = encode_tree(t, fraud_idx)
    header = struct.pack("<III", MAGIC, VERSION_DT, int(t.node_count))
    out_path.write_bytes(header + body)
    return {
        "algo": "dt",
        "depth": clf.get_depth(),
        "leaves": clf.get_n_leaves(),
        "n_nodes": int(t.node_count),
    }


def export_rf(clf: RandomForestClassifier, out_path: Path) -> dict:
    fraud_idx = list(clf.classes_).index(1)
    trees = clf.estimators_
    n_trees = len(trees)
    total_nodes = sum(int(t.tree_.node_count) for t in trees)
    header = struct.pack("<IIII", MAGIC, VERSION_RF, n_trees, total_nodes)
    parts = [header]
    for t in trees:
        parts.append(struct.pack("<I", int(t.tree_.node_count)))
    for t in trees:
        parts.append(encode_tree(t.tree_, fraud_idx))
    out_path.write_bytes(b"".join(parts))
    return {
        "algo": "rf",
        "n_trees": n_trees,
        "total_nodes": total_nodes,
        "avg_depth": sum(t.get_depth() for t in trees) / n_trees,
        "avg_leaves": sum(t.get_n_leaves() for t in trees) / n_trees,
    }


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--input", type=Path, default=Path("fraud_dt.joblib"))
    ap.add_argument("--output", type=Path, default=Path("../resources/fraud_dt.bin"))
    args = ap.parse_args()

    clf = joblib.load(args.input)
    if isinstance(clf, RandomForestClassifier):
        info = export_rf(clf, args.output)
    elif isinstance(clf, DecisionTreeClassifier):
        info = export_dt(clf, args.output)
    else:
        raise SystemExit(f"unsupported classifier type: {type(clf).__name__}")

    size = args.output.stat().st_size
    print(f"exported {info}  →  {args.output} ({size:,} bytes)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
