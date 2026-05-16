"""Distill k-NN(k=5) labels for the reference dataset.

For each reference vector R, find its 5 nearest neighbors (excluding R itself)
and assign label = majority-fraud. This makes a DT/RF trained on these labels
mimic the k-NN oracle decision boundary — what the AVALIACAO test compares
against.

Output:
    resources/labels_distilled.npy   — uint8 array of length N
"""

from __future__ import annotations

import argparse
import time
from pathlib import Path

import numpy as np
from sklearn.neighbors import NearestNeighbors

from fraud_dt import load_packed


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--vectors", type=Path, default=Path("../resources/vectors.bin"))
    ap.add_argument("--out", type=Path, default=Path("../resources/labels_distilled.npy"))
    ap.add_argument("--k", type=int, default=5)
    ap.add_argument("--algorithm", default="ball_tree", choices=["ball_tree", "kd_tree"])
    ap.add_argument("--leaf-size", type=int, default=40)
    args = ap.parse_args()

    X, y = load_packed(args.vectors)
    print(f"[load] {len(y):,} vectors  fraud_rate={y.mean():.4f}")

    t0 = time.perf_counter()
    nn = NearestNeighbors(
        n_neighbors=args.k + 1,
        algorithm=args.algorithm,
        leaf_size=args.leaf_size,
        n_jobs=-1,
    )
    nn.fit(X)
    print(f"[build] {args.algorithm} in {time.perf_counter()-t0:.1f}s")

    t0 = time.perf_counter()
    _, indices = nn.kneighbors(X)
    print(f"[query] kneighbors(k={args.k+1}) in {time.perf_counter()-t0:.1f}s")

    # majority vote among k=5, excluding self at idx 0
    others = indices[:, 1:]
    fraud_counts = y[others].sum(axis=1, dtype=np.int32)
    majority_threshold = (args.k // 2) + 1
    y_distilled = (fraud_counts >= majority_threshold).astype(np.uint8)

    agree = int((y_distilled == y).sum())
    print(
        f"[distill] vs original: {agree:,}/{len(y):,} agree "
        f"({100*agree/len(y):.2f}%)  →  fraud_rate {y_distilled.mean():.4f}"
    )

    np.save(args.out, y_distilled)
    sz = args.out.stat().st_size
    print(f"[save] {args.out}  ({sz/1024/1024:.1f} MB)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
