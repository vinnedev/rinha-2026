"""Train + serialize the fraud classifier.

Algos:
- dt           DecisionTreeClassifier  (binary labels)
- rf           RandomForestClassifier  (binary labels)
- rf-soft      RandomForestRegressor   (soft k-NN labels 0..1)

When --distilled is set the script reads ../resources/labels_distilled.bin
as raw uint8 — values 0/1 are interpreted as binary (back-compat) and
values 0..5 as soft k-NN counts (count/5 → continuous target).

Soft distillation produces a regressor whose leaf "predicted value" is the
mean k-NN density in that leaf, which gives the API a *much* tighter
hybrid uncertain band ([0.2, 0.8] still keeps every confident prediction
correct but the band shrinks because the model now mimics the oracle in
continuous space, not just lateral classification).
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from pathlib import Path

import numpy as np
from sklearn.ensemble import RandomForestClassifier, RandomForestRegressor
from sklearn.metrics import (
    accuracy_score,
    classification_report,
    confusion_matrix,
    f1_score,
)
from sklearn.model_selection import train_test_split
from sklearn.tree import DecisionTreeClassifier

from fraud_dt import FEATURE_NAMES, load_dataset, save_model

ALGOS = ("dt", "rf", "rf-soft")


def _json_safe(o):
    if isinstance(o, np.integer):
        return int(o)
    if isinstance(o, np.floating):
        return float(o)
    if isinstance(o, np.ndarray):
        return o.tolist()
    raise TypeError(f"not JSON-serializable: {type(o).__name__}")


def build_model(args, random_state):
    if args.algo == "rf":
        return RandomForestClassifier(
            n_estimators=args.n_estimators,
            max_depth=args.max_depth,
            min_samples_leaf=args.min_samples_leaf,
            n_jobs=-1,
            random_state=random_state,
        )
    if args.algo == "rf-soft":
        return RandomForestRegressor(
            n_estimators=args.n_estimators,
            max_depth=args.max_depth,
            min_samples_leaf=args.min_samples_leaf,
            n_jobs=-1,
            random_state=random_state,
        )
    return DecisionTreeClassifier(
        criterion="gini",
        max_depth=args.max_depth,
        min_samples_leaf=args.min_samples_leaf,
        random_state=random_state,
    )


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--vectors", type=Path, default=Path("../resources/vectors.bin"))
    ap.add_argument("--gz", type=Path, default=Path("../resources/references.json.gz"))
    ap.add_argument("--distilled", action="store_true")
    ap.add_argument("--distilled-path", type=Path,
                    default=Path("../resources/labels_distilled.bin"))
    ap.add_argument("--algo", choices=ALGOS, default="dt")
    ap.add_argument("--n-estimators", type=int, default=30)
    ap.add_argument("--max-depth", type=int, default=20,
                    help="set to 0 for None (fully grown)")
    ap.add_argument("--min-samples-leaf", type=int, default=10)
    ap.add_argument("--out-model", type=Path, default=Path("fraud_dt.joblib"))
    ap.add_argument("--out-metrics", type=Path, default=Path("metrics.json"))
    ap.add_argument("--test-size", type=float, default=0.2)
    ap.add_argument("--random-state", type=int, default=42)
    args = ap.parse_args()
    if args.max_depth == 0:
        args.max_depth = None

    X, y = load_dataset(args.vectors, args.gz)
    soft = args.algo == "rf-soft"
    if args.distilled:
        if not args.distilled_path.is_file():
            raise SystemExit(f"distilled labels not found at {args.distilled_path}; run distill.py first")
        if args.distilled_path.suffix == ".npy":
            raw = np.load(args.distilled_path)
        else:
            raw = np.frombuffer(args.distilled_path.read_bytes(), dtype=np.uint8).copy()
        if len(raw) != len(y):
            raise SystemExit(f"distilled labels length mismatch: {len(raw)} vs {len(y)}")
        is_soft_file = int(raw.max()) > 1
        if soft:
            if not is_soft_file:
                raise SystemExit(
                    "labels_distilled.bin contains binary labels but --algo rf-soft "
                    "needs counts 0..5 — rerun cmd/distill (it now writes counts by default)"
                )
            y_train_target = raw.astype(np.float32) / 5.0
            print(
                f"[labels] soft k-NN counts: min={int(raw.min())} max={int(raw.max())} "
                f"mean={float(raw.mean()):.3f}"
            )
        else:
            if is_soft_file:
                y_train_target = (raw >= 3).astype(np.uint8)
            else:
                y_train_target = raw
            diff = int((y_train_target != y).sum())
            print(f"[labels] distilled binary, {diff:,} differ from original ({100*diff/len(y):.2f}%)")
        y = y_train_target
    elif soft:
        raise SystemExit("--algo rf-soft requires --distilled (soft labels come from cmd/distill)")

    # Soft targets don't make sense to stratify on continuous values
    stratify = None if soft else y
    X_train, X_test, y_train, y_test = train_test_split(
        X, y, test_size=args.test_size, random_state=args.random_state, stratify=stratify
    )
    print(f"[split] train={X_train.shape[0]:,}  test={X_test.shape[0]:,}")

    clf = build_model(args, args.random_state)
    print(
        f"[fit] {type(clf).__name__}(max_depth={args.max_depth}, "
        f"min_samples_leaf={args.min_samples_leaf}"
        + (f", n_estimators={args.n_estimators}" if args.algo != 'dt' else "") + ")"
    )
    t0 = time.perf_counter()
    clf.fit(X_train, y_train)
    fit_s = time.perf_counter() - t0

    if args.algo == "dt":
        print(f"[fit] done in {fit_s:.1f}s  depth={clf.get_depth()}  leaves={clf.get_n_leaves()}")
    else:
        depths = [e.get_depth() for e in clf.estimators_]
        leaves = [e.get_n_leaves() for e in clf.estimators_]
        print(
            f"[fit] done in {fit_s:.1f}s  trees={len(clf.estimators_)}  "
            f"avg_depth={np.mean(depths):.1f}  avg_leaves={np.mean(leaves):.0f}"
        )

    t0 = time.perf_counter()
    if soft:
        score_pred = clf.predict(X_test)
        y_pred = (score_pred >= 0.6).astype(np.uint8)
        y_test_binary = (y_test >= 0.6).astype(np.uint8)
    else:
        y_pred = clf.predict(X_test)
        y_test_binary = y_test
    inference_ms = (time.perf_counter() - t0) * 1000

    cm = confusion_matrix(y_test_binary, y_pred)
    tn, fp, fn, tp = int(cm[0, 0]), int(cm[0, 1]), int(cm[1, 0]), int(cm[1, 1])
    importance = sorted(
        (
            {"index": i, "name": FEATURE_NAMES[i], "importance": float(s)}
            for i, s in enumerate(clf.feature_importances_)
        ),
        key=lambda d: d["importance"], reverse=True,
    )

    metrics = {
        "n_train": int(len(X_train)),
        "n_test": int(len(X_test)),
        "algo": args.algo,
        "distilled": bool(args.distilled),
        "fit_seconds": round(fit_s, 3),
        "inference_us_per_sample": round(inference_ms * 1000 / len(y_test), 4),
        "accuracy": round(accuracy_score(y_test_binary, y_pred), 6),
        "f1": round(f1_score(y_test_binary, y_pred), 6),
        "confusion_matrix": {"tn": tn, "fp": fp, "fn": fn, "tp": tp},
        "classification_report": classification_report(
            y_test_binary, y_pred, target_names=["legit", "fraud"], digits=4, output_dict=True
        ),
        "feature_importance": importance,
        "params": {
            "max_depth": args.max_depth,
            "min_samples_leaf": args.min_samples_leaf,
            "n_estimators": args.n_estimators if args.algo != "dt" else None,
        },
    }

    if soft:
        # Confidence-band distribution: how many test queries would skip the oracle?
        band_lo, band_hi = 0.2, 0.8
        certain = ((score_pred <= band_lo) | (score_pred >= band_hi)).sum()
        metrics["soft_band"] = {
            "band": [band_lo, band_hi],
            "n_certain": int(certain),
            "n_uncertain": int(len(score_pred) - certain),
            "uncertain_rate": float(1 - certain / len(score_pred)),
        }
        print(
            f"[eval/soft] uncertain in [{band_lo},{band_hi}]: "
            f"{metrics['soft_band']['uncertain_rate']*100:.2f}% of test set "
            f"({metrics['soft_band']['n_uncertain']:,}/{len(score_pred):,})"
        )

    print(
        f"[eval] accuracy={metrics['accuracy']:.4f}  f1={metrics['f1']:.4f}  "
        f"({metrics['inference_us_per_sample']:.2f}µs/sample)"
    )
    print(f"[eval] TN={tn:,} FP={fp:,} FN={fn:,} TP={tp:,}")

    out = save_model(clf, args.out_model)
    args.out_metrics.write_text(json.dumps(metrics, indent=2, default=_json_safe))
    print(f"[save] {out}  ({out.stat().st_size/1024/1024:.1f} MB)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
