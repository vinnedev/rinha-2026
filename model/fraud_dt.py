"""Reusable building blocks for the fraud Decision Tree pipeline.

Importable from CLI scripts (train.py, predict.py) and notebooks:

    from fraud_dt import (
        load_dataset, dataset_stats, analyze_sentinels,
        fit_tree, fit_forest, sweep_hyperparams,
        evaluate, save_model, load_model, predict_samples,
        FEATURE_NAMES, FRAUD_THRESHOLD,
    )
"""

from __future__ import annotations

import gzip
import json
import struct
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable

import joblib
import numpy as np
from sklearn.ensemble import RandomForestClassifier
from sklearn.metrics import (
    accuracy_score,
    classification_report,
    confusion_matrix,
    f1_score,
    precision_recall_curve,
    roc_auc_score,
    roc_curve,
)
from sklearn.model_selection import StratifiedKFold, cross_val_score, train_test_split
from sklearn.tree import DecisionTreeClassifier

DIM = 14
SCALE = 10000
SENTINEL_RAW = -1.0
MAGIC = 0x52494E48
FRAUD_THRESHOLD = 0.6

FEATURE_NAMES: list[str] = [
    "amount",
    "installments",
    "amount_vs_avg",
    "hour_of_day",
    "day_of_week",
    "minutes_since_last_tx",
    "km_from_last_tx",
    "km_from_home",
    "tx_count_24h",
    "is_online",
    "card_present",
    "unknown_merchant",
    "mcc_risk",
    "merchant_avg_amount",
]

SENTINEL_DIMS = [5, 6]


@dataclass
class TrainResult:
    clf: object
    X_train: np.ndarray
    X_test: np.ndarray
    y_train: np.ndarray
    y_test: np.ndarray
    fit_seconds: float


def load_packed(path: str | Path) -> tuple[np.ndarray, np.ndarray]:
    path = Path(path)
    with path.open("rb") as f:
        magic, _version, n, dim = struct.unpack("<IIII", f.read(16))
        if magic != MAGIC:
            raise ValueError(f"bad magic 0x{magic:08X} in {path}")
        if dim != DIM:
            raise ValueError(f"unexpected dim {dim} in {path}")
        vec_bytes = n * dim * 2
        X_int16 = np.frombuffer(f.read(vec_bytes), dtype=np.int16).reshape(n, dim)
        X = X_int16.astype(np.float32) / SCALE
        y = np.frombuffer(f.read(n), dtype=np.uint8).copy()
    return np.ascontiguousarray(X), y


def load_gzip(path: str | Path) -> tuple[np.ndarray, np.ndarray]:
    path = Path(path)
    vecs: list[list[float]] = []
    labels: list[int] = []
    with gzip.open(path, "rt") as f:
        data = json.load(f)
        for r in data:
            vecs.append(r["vector"])
            labels.append(1 if r["label"] == "fraud" else 0)
    return np.asarray(vecs, dtype=np.float32), np.asarray(labels, dtype=np.uint8)


def load_dataset(
    vectors_path: str | Path = "../resources/vectors.bin",
    gz_path: str | Path = "../resources/references.json.gz",
    verbose: bool = True,
) -> tuple[np.ndarray, np.ndarray]:
    vp = Path(vectors_path)
    gp = Path(gz_path)
    t0 = time.perf_counter()
    if vp.is_file():
        if verbose:
            print(f"[load] packed binary {vp}")
        X, y = load_packed(vp)
    elif gp.is_file():
        if verbose:
            print(f"[load] gzipped JSON {gp}")
        X, y = load_gzip(gp)
    else:
        raise FileNotFoundError(f"neither {vp} nor {gp} exists")
    if verbose:
        print(
            f"[load] {len(y):,} vectors  fraud_rate={y.mean():.4f}  "
            f"({int(y.sum()):,} fraud / {int(len(y)-y.sum()):,} legit)  "
            f"in {time.perf_counter()-t0:.1f}s"
        )
    return X, y


def dataset_stats(X: np.ndarray, y: np.ndarray) -> dict:
    """Per-dimension statistics, split by label."""
    out: dict = {
        "n_rows": int(len(X)),
        "n_fraud": int(y.sum()),
        "n_legit": int(len(y) - y.sum()),
        "fraud_rate": float(y.mean()),
        "any_nan": bool(np.isnan(X).any()),
        "any_inf": bool(np.isinf(X).any()),
        "per_feature": [],
    }
    for i, name in enumerate(FEATURE_NAMES):
        col = X[:, i]
        legit = col[y == 0]
        fraud = col[y == 1]
        out["per_feature"].append(
            {
                "index": i,
                "name": name,
                "min": float(col.min()),
                "max": float(col.max()),
                "mean": float(col.mean()),
                "std": float(col.std()),
                "legit_mean": float(legit.mean()),
                "fraud_mean": float(fraud.mean()),
                "sentinel_rate": float((col == SENTINEL_RAW).mean()),
            }
        )
    return out


def analyze_sentinels(X: np.ndarray, y: np.ndarray) -> dict:
    """Characterize the -1 sentinel in dims 5/6 ('no previous transaction')."""
    mask = np.zeros(len(X), dtype=bool)
    for d in SENTINEL_DIMS:
        mask |= X[:, d] == SENTINEL_RAW
    return {
        "rows_with_sentinel": int(mask.sum()),
        "rate": float(mask.mean()),
        "fraud_rate_within_sentinel": float(y[mask].mean()) if mask.any() else 0.0,
        "fraud_rate_outside_sentinel": float(y[~mask].mean()) if (~mask).any() else 0.0,
        "dims_checked": SENTINEL_DIMS,
    }


def split(
    X: np.ndarray,
    y: np.ndarray,
    test_size: float = 0.2,
    random_state: int = 42,
) -> tuple[np.ndarray, np.ndarray, np.ndarray, np.ndarray]:
    return train_test_split(
        X, y, test_size=test_size, random_state=random_state, stratify=y
    )


def fit_tree(
    X: np.ndarray,
    y: np.ndarray,
    max_depth: int = 20,
    min_samples_leaf: int = 50,
    class_weight: str | None = None,
    test_size: float = 0.2,
    random_state: int = 42,
    verbose: bool = True,
) -> TrainResult:
    X_train, X_test, y_train, y_test = split(X, y, test_size, random_state)
    if verbose:
        print(f"[split] train={X_train.shape[0]:,}  test={X_test.shape[0]:,}")
        print(
            f"[fit] DecisionTreeClassifier(max_depth={max_depth}, "
            f"min_samples_leaf={min_samples_leaf}, class_weight={class_weight!r})"
        )
    t0 = time.perf_counter()
    clf = DecisionTreeClassifier(
        criterion="gini",
        max_depth=max_depth,
        min_samples_leaf=min_samples_leaf,
        class_weight=class_weight,
        random_state=random_state,
    )
    clf.fit(X_train, y_train)
    fit_s = time.perf_counter() - t0
    if verbose:
        print(
            f"[fit] done in {fit_s:.1f}s  depth={clf.get_depth()}  "
            f"leaves={clf.get_n_leaves()}"
        )
    return TrainResult(clf, X_train, X_test, y_train, y_test, fit_s)


def fit_forest(
    X: np.ndarray,
    y: np.ndarray,
    n_estimators: int = 50,
    max_depth: int = 20,
    min_samples_leaf: int = 50,
    n_jobs: int = -1,
    test_size: float = 0.2,
    random_state: int = 42,
    verbose: bool = True,
) -> TrainResult:
    X_train, X_test, y_train, y_test = split(X, y, test_size, random_state)
    if verbose:
        print(
            f"[fit] RandomForestClassifier(n_estimators={n_estimators}, "
            f"max_depth={max_depth}, min_samples_leaf={min_samples_leaf})"
        )
    t0 = time.perf_counter()
    clf = RandomForestClassifier(
        n_estimators=n_estimators,
        max_depth=max_depth,
        min_samples_leaf=min_samples_leaf,
        n_jobs=n_jobs,
        random_state=random_state,
    )
    clf.fit(X_train, y_train)
    fit_s = time.perf_counter() - t0
    if verbose:
        print(f"[fit] done in {fit_s:.1f}s")
    return TrainResult(clf, X_train, X_test, y_train, y_test, fit_s)


def sweep_hyperparams(
    X: np.ndarray,
    y: np.ndarray,
    max_depths: Iterable[int] = (8, 12, 16, 20, 24, 28),
    min_samples_leaf: int = 50,
    cv: int = 3,
    sample: int = 200_000,
    random_state: int = 42,
    verbose: bool = True,
) -> list[dict]:
    """Quick K-fold sweep over max_depth on a subsample (cv on the full 3M is slow)."""
    if len(X) > sample:
        rng = np.random.default_rng(random_state)
        idx = rng.choice(len(X), size=sample, replace=False)
        Xs, ys = X[idx], y[idx]
    else:
        Xs, ys = X, y
    skf = StratifiedKFold(n_splits=cv, shuffle=True, random_state=random_state)
    rows = []
    for md in max_depths:
        t0 = time.perf_counter()
        clf = DecisionTreeClassifier(
            max_depth=md, min_samples_leaf=min_samples_leaf, random_state=random_state
        )
        scores = cross_val_score(clf, Xs, ys, cv=skf, scoring="f1", n_jobs=-1)
        rows.append(
            {
                "max_depth": int(md),
                "f1_mean": float(scores.mean()),
                "f1_std": float(scores.std()),
                "fit_seconds": round(time.perf_counter() - t0, 2),
            }
        )
        if verbose:
            print(
                f"  max_depth={md:>3d}  f1={scores.mean():.4f} ± {scores.std():.4f}  "
                f"({rows[-1]['fit_seconds']}s)"
            )
    return rows


def evaluate(
    clf,
    X_test: np.ndarray,
    y_test: np.ndarray,
    threshold: float | None = None,
    verbose: bool = True,
) -> dict:
    t0 = time.perf_counter()
    proba = clf.predict_proba(X_test)
    fraud_idx = list(clf.classes_).index(1) if 1 in clf.classes_ else 1
    scores = proba[:, fraud_idx]
    if threshold is None:
        y_pred = (scores >= 0.5).astype(np.uint8)
    else:
        y_pred = (scores >= threshold).astype(np.uint8)
    inference_ms = (time.perf_counter() - t0) * 1000

    cm = confusion_matrix(y_test, y_pred)
    tn, fp, fn, tp = int(cm[0, 0]), int(cm[0, 1]), int(cm[1, 0]), int(cm[1, 1])

    importance = [
        {"index": i, "name": FEATURE_NAMES[i], "importance": float(s)}
        for i, s in enumerate(getattr(clf, "feature_importances_", [0] * DIM))
    ]
    importance.sort(key=lambda d: d["importance"], reverse=True)

    auc = float(roc_auc_score(y_test, scores))
    fpr, tpr, _ = roc_curve(y_test, scores)
    prec, rec, _ = precision_recall_curve(y_test, scores)

    metrics = {
        "n_test": int(len(y_test)),
        "inference_ms_total": round(inference_ms, 3),
        "inference_us_per_sample": round(inference_ms * 1000 / len(y_test), 4),
        "accuracy": round(accuracy_score(y_test, y_pred), 6),
        "f1": round(f1_score(y_test, y_pred), 6),
        "roc_auc": round(auc, 6),
        "confusion_matrix": {"tn": tn, "fp": fp, "fn": fn, "tp": tp},
        "classification_report": classification_report(
            y_test, y_pred, target_names=["legit", "fraud"], digits=4, output_dict=True
        ),
        "feature_importance": importance,
        "roc_curve": {"fpr": fpr.tolist(), "tpr": tpr.tolist()},
        "pr_curve": {"precision": prec.tolist(), "recall": rec.tolist()},
    }
    if verbose:
        print(
            f"[eval] accuracy={metrics['accuracy']:.4f}  f1={metrics['f1']:.4f}  "
            f"auc={metrics['roc_auc']:.4f}  ({metrics['inference_us_per_sample']:.2f}µs/sample)"
        )
        print(f"[eval] TN={tn:,} FP={fp:,} FN={fn:,} TP={tp:,}")
    return metrics


def save_model(clf, path: str | Path = "fraud_dt.joblib") -> Path:
    path = Path(path)
    joblib.dump(clf, path, compress=3)
    return path


def load_model(path: str | Path = "fraud_dt.joblib"):
    return joblib.load(path)


def predict_samples(
    clf,
    samples: list[dict],
    threshold: float = FRAUD_THRESHOLD,
) -> list[dict]:
    X = np.asarray([s["vector"] for s in samples], dtype=np.float32)
    proba = clf.predict_proba(X)
    fraud_idx = list(clf.classes_).index(1) if 1 in clf.classes_ else 1
    return [
        {
            "name": s.get("name"),
            "fraud_score": float(pr[fraud_idx]),
            "approved": float(pr[fraud_idx]) < threshold,
            "expected": s.get("expected"),
        }
        for s, pr in zip(samples, proba)
    ]


def benchmark_inference(clf, X: np.ndarray, n: int = 10000) -> dict:
    """Microbenchmark single-row predict latency."""
    idx = np.random.default_rng(0).choice(len(X), size=n, replace=False)
    samples = X[idx]
    t0 = time.perf_counter()
    for row in samples:
        clf.predict_proba(row.reshape(1, -1))
    total_s = time.perf_counter() - t0
    return {
        "n": n,
        "total_seconds": round(total_s, 3),
        "us_per_sample": round(total_s / n * 1_000_000, 2),
    }
