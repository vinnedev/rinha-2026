"""Load the trained fraud DT and classify samples.

Run:
    uv run python predict.py
    uv run python predict.py --input samples.json
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from pathlib import Path

from fraud_dt import FRAUD_THRESHOLD, load_model, predict_samples

SPEC_SAMPLES = [
    {
        "name": "spec/legit (tx-1329056812)",
        "vector": [0.0041, 0.1667, 0.05, 0.7826, 0.3333, -1, -1, 0.0292, 0.15, 0, 1, 0, 0.15, 0.006],
        "expected": "legit",
    },
    {
        "name": "spec/fraud (tx-3330991687)",
        "vector": [0.9506, 0.8333, 1.0, 0.2174, 0.8333, -1, -1, 0.9523, 1.0, 0, 1, 1, 0.75, 0.0055],
        "expected": "fraud",
    },
]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", type=Path, default=Path("fraud_dt.joblib"))
    ap.add_argument("--input", default=None, help='JSON file or "-" for stdin')
    args = ap.parse_args()

    t0 = time.perf_counter()
    clf = load_model(args.model)
    print(f"[load] {args.model} in {(time.perf_counter()-t0)*1000:.1f}ms")

    if args.input is None:
        samples = SPEC_SAMPLES
    elif args.input == "-":
        samples = json.load(sys.stdin)
    else:
        samples = json.loads(Path(args.input).read_text())

    t0 = time.perf_counter()
    results = predict_samples(clf, samples)
    elapsed_us = (time.perf_counter() - t0) * 1_000_000
    print(
        f"[predict] {len(samples)} samples in {elapsed_us:.0f}µs "
        f"({elapsed_us/max(len(samples),1):.1f}µs/sample)  threshold={FRAUD_THRESHOLD}"
    )

    print()
    print(f"  {'sample':<32s} {'fraud_score':>11s} {'approved':>8s} {'expected':>9s}  result")
    for r in results:
        exp = r.get("expected") or "?"
        if exp == "?":
            verdict = "OK"
        else:
            verdict = "OK" if (exp == "legit") == r["approved"] else "MISS"
        print(
            f"  {r['name']:<32s} {r['fraud_score']:>11.4f} {str(r['approved']):>8s} {exp:>9s}  {verdict}"
        )
    return 0


if __name__ == "__main__":
    sys.exit(main())
