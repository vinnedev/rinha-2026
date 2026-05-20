"""Evaluate a trained model against test/test-data.json (the official k6 dataset).

Reproduces fraud.Vectorize from the Go side so the input matches what the
runtime API sees. Reports accuracy, FP, FN, weighted_E and projected
AVALIACAO failure_rate.
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from datetime import datetime
from pathlib import Path

import joblib
import numpy as np

MCC_RISK = {
    "5411": 0.15, "5812": 0.30, "5912": 0.20, "5944": 0.45,
    "7801": 0.80, "7802": 0.75, "7995": 0.85,
    "4511": 0.35, "5311": 0.25, "5999": 0.50,
}

MAX_AMOUNT = 10000.0
MAX_INSTALL = 12.0
AMOUNT_VS_AVG = 10.0
MAX_MINUTES = 1440.0
MAX_KM = 1000.0
MAX_TX_24H = 20.0
MAX_MERCH_AVG = 10000.0
THRESHOLD = 0.6


def clamp01(x: float) -> float:
    return 0.0 if x < 0 else (1.0 if x > 1 else x)


def vectorize(req: dict) -> list[float]:
    tx = req["transaction"]
    cu = req["customer"]
    me = req["merchant"]
    te = req["terminal"]
    lt = req.get("last_transaction")
    when = datetime.fromisoformat(tx["requested_at"].replace("Z", "+00:00"))
    hour = when.hour
    dow = (when.weekday())  # Monday=0, Sunday=6 — same as Go's weekdayMonZero

    amt = tx["amount"]
    avg = cu.get("avg_amount", 0.0) or 0.0
    avg_ratio = (amt / avg) / AMOUNT_VS_AVG if avg > 0 else 0.0
    mcc_risk = MCC_RISK.get(me.get("mcc", ""), 0.5)
    known = me["id"] in (cu.get("known_merchants") or [])

    v = [
        clamp01(amt / MAX_AMOUNT),
        clamp01(tx["installments"] / MAX_INSTALL),
        clamp01(avg_ratio),
        clamp01(hour / 23.0),
        clamp01(dow / 6.0),
        -1.0 if lt is None else clamp01(((when - datetime.fromisoformat(lt["timestamp"].replace("Z", "+00:00"))).total_seconds() / 60.0) / MAX_MINUTES),
        -1.0 if lt is None else clamp01(lt.get("km_from_current", 0.0) / MAX_KM),
        clamp01(te["km_from_home"] / MAX_KM),
        clamp01(cu["tx_count_24h"] / MAX_TX_24H),
        1.0 if te["is_online"] else 0.0,
        1.0 if te["card_present"] else 0.0,
        0.0 if known else 1.0,
        clamp01(mcc_risk),
        clamp01(me.get("avg_amount", 0.0) / MAX_MERCH_AVG),
    ]
    # Quantize-round-back-to-float to mirror the Go pipeline exactly
    out = []
    for x in v:
        if x == -1.0:
            out.append(-1.0)
        else:
            q = int(x * 10000 + 0.5)
            q = max(0, min(10000, q))
            out.append(q / 10000.0)
    return out


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", type=Path, required=True)
    ap.add_argument("--test-data", type=Path, default=Path("../test/test-data.json"))
    args = ap.parse_args()

    t0 = time.perf_counter()
    clf = joblib.load(args.model)
    print(f"[load] {args.model} in {(time.perf_counter()-t0)*1000:.0f}ms")

    data = json.loads(args.test_data.read_text())
    entries = data["entries"]
    print(f"[load] {len(entries):,} test entries")

    t0 = time.perf_counter()
    X = np.array([vectorize(e["request"]) for e in entries], dtype=np.float32)
    print(f"[vec] vectorized in {time.perf_counter()-t0:.1f}s")

    t0 = time.perf_counter()
    if hasattr(clf, "predict_proba"):
        fraud_idx = list(clf.classes_).index(1)
        proba = clf.predict_proba(X)[:, fraud_idx]
    else:
        proba = clf.predict(X)
    elapsed_ms = (time.perf_counter() - t0) * 1000
    print(f"[predict] {len(entries):,} samples in {elapsed_ms:.0f}ms ({elapsed_ms*1000/len(entries):.1f}µs/sample)")

    approved = proba < THRESHOLD
    expected = np.array([e["expected_approved"] for e in entries], dtype=bool)

    tp = int(((~approved) & (~expected)).sum())
    tn = int((approved & expected).sum())
    fp = int(((~approved) & expected).sum())
    fn = int((approved & (~expected)).sum())
    n = len(entries)
    acc = (tp + tn) / n
    fail = (fp + fn) / n
    weighted_e = fp + 3 * fn

    print()
    print(f"  accuracy:     {acc:.4f}")
    print(f"  TP={tp:,}  TN={tn:,}  FP={fp:,}  FN={fn:,}")
    print(f"  failure_rate: {fail:.4f}")
    print(f"  weighted_E  : {weighted_e:,}")
    print()
    # Confidence band analysis: how many fall in uncertain region?
    for lo, hi in [(0.2, 0.8), (0.25, 0.75), (0.3, 0.7), (0.35, 0.65), (0.4, 0.6)]:
        uncertain = ((proba > lo) & (proba < hi)).sum()
        confident = n - uncertain
        certain_fp = int(((~approved) & (proba >= hi) & expected).sum() +
                         ((~approved) & (proba <= lo) & expected).sum())
        certain_fn = int((approved & (proba <= lo) & (~expected)).sum() +
                         (approved & (proba >= hi) & (~expected)).sum())
        print(f"  band [{lo:.2f},{hi:.2f}]:  confident={confident:,} ({100*confident/n:.1f}%)  uncertain={uncertain:,}  certain_FP={certain_fp}  certain_FN={certain_fn}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
