package fraud

import (
	"math"
	"time"

	"github.com/vinnedev/rinha-2026/internal/domain"
)

// Integer-only vectorization. Each input value is converted once into a
// uint64 "milli" representation (× 1000) at the boundary, then every
// division/clamp uses integer math with round-half-up. This removes float
// divisions from the hot path and makes the output deterministic across
// CPU vendors. The conversion factor mirrors C's parse_scaled1000:
//
//	value_milli   = round(value * 1000)
//	out[i]        = quantMilli(value_milli, max_units)
//	              = (value_milli * Scale + (max_milli/2)) / max_milli
//
// Some scales are denominators in "units" (max_amount = 10000 amount-units,
// max_km = 1000 km-units, etc) — multiplied by 1000 internally to compare
// against milli numerators.
//
// The denominators below are precomputed constants the compiler can fold.
const (
	maxAmountMilli            uint64 = 10000 * 1000
	maxInstallmentsMilli      uint64 = 12 * 1000
	maxMinutesMilli           uint64 = 1440 * 1000
	maxKmMilli                uint64 = 1000 * 1000
	maxTxCount24hMilli        uint64 = 20 * 1000
	maxMerchantAvgAmountMilli uint64 = 10000 * 1000
	hoursPerDayMilli          uint64 = 23 * 1000
	daysPerWeekMilli          uint64 = 6 * 1000
	amountVsAvgRatioMilli     uint64 = 10 * 1000
)

func Vectorize(p *domain.FraudPayload, out []int16) {
	t := p.Transaction.RequestedAt.UTC()

	amountMilli := toMilli(p.Transaction.Amount)
	avgMilli := toMilli(p.Customer.AvgAmount)

	out[0] = quantMilli(amountMilli, maxAmountMilli)
	out[1] = quantMilli(uint64(p.Transaction.Installments)*1000, maxInstallmentsMilli)
	out[2] = amountRatio(amountMilli, avgMilli)
	out[3] = quantMilli(uint64(t.Hour())*1000, hoursPerDayMilli)
	out[4] = quantMilli(uint64(weekdayMonZero(t))*1000, daysPerWeekMilli)

	if p.LastTransaction == nil {
		out[5] = domain.Sentinel
		out[6] = domain.Sentinel
	} else {
		// minutes diff via Sub().Nanoseconds() / 60e9 — staying in int math.
		diffNs := t.Sub(p.LastTransaction.Timestamp.UTC()).Nanoseconds()
		var minutesMilli uint64
		if diffNs > 0 {
			// 1 minute = 60_000_000_000 ns, so milli-minutes = ns * 1000 / 60e9 = ns / 60_000_000
			minutesMilli = uint64(diffNs / 60_000_000)
		}
		out[5] = quantMilli(minutesMilli, maxMinutesMilli)
		out[6] = quantMilli(toMilli(p.LastTransaction.KmFromCurrent), maxKmMilli)
	}

	out[7] = quantMilli(toMilli(p.Terminal.KmFromHome), maxKmMilli)
	out[8] = quantMilli(uint64(p.Customer.TxCount24h)*1000, maxTxCount24hMilli)
	out[9] = boolDim(p.Terminal.IsOnline)
	out[10] = boolDim(p.Terminal.CardPresent)
	out[11] = unknownMerchant(p.Merchant.ID, p.Customer.KnownMerchants)
	out[12] = mccRiskQuant(p.Merchant.MCC)
	out[13] = quantMilli(toMilli(p.Merchant.AvgAmount), maxMerchantAvgAmountMilli)
}

// toMilli converts a float64 input into the integer "milli" representation
// used throughout the integer vectorizer. Negative or NaN/Inf inputs clamp
// to 0 — they should never appear in valid payloads.
func toMilli(v float64) uint64 {
	if !(v > 0) || math.IsInf(v, 0) {
		return 0
	}
	scaled := v*1000.0 + 0.5
	if scaled > float64(math.MaxUint64) {
		return math.MaxUint64
	}
	return uint64(scaled)
}

// quantMilli computes round(num/den * Scale) clamped to [0, Scale] using
// integer math. Both num and den are expressed in "milli" units so the
// ratio is dimensionless and the multiply-then-divide cannot overflow:
// max(num)·Scale ≈ 1e16·1e4 = 1e20 fits in uint64? No — but the inputs are
// already clamped, so num ≤ den · 1.something on the hot path. The branch
// after the multiply handles edge cases where num exceeds den.
func quantMilli(num, den uint64) int16 {
	if den == 0 {
		return domain.Scale
	}
	if num >= den {
		return domain.Scale
	}
	// num · Scale fits because num < den ≤ 1e10 (typical limits) and
	// Scale = 10_000, so the product is bounded by 1e14.
	q := (num*uint64(domain.Scale) + den/2) / den
	if q > uint64(domain.Scale) {
		return domain.Scale
	}
	return int16(q)
}

// amountRatio is dim 2: clamp((amount / customer_avg_amount) / 10) quantized
// to Scale. The float reference does this in one go (a single round-half-up
// from the continuous ratio to the integer bin). The previous int version
// rounded twice (intermediate "milli" then Scale) and produced off-by-a-bin
// errors when the continuous ratio sat between bin centers.
//
// Identity used here: with ratio = amount / (avg · 10), the final integer
// out = round(ratio · Scale) = round(amount · Scale / (avg · 10)). Carrying
// both inputs in milli units (= ×1000) cancels the factor, so:
//   out = round(amountMilli · Scale / (avgMilli · 10))
// Computed with the standard round-half-up: (num + den/2) / den.
func amountRatio(amountMilli, avgMilli uint64) int16 {
	if avgMilli == 0 {
		return 0
	}
	den := avgMilli * 10
	if amountMilli >= den {
		return domain.Scale
	}
	out := (amountMilli*uint64(domain.Scale) + den/2) / den
	if out > uint64(domain.Scale) {
		return domain.Scale
	}
	return int16(out)
}

func boolDim(b bool) int16 {
	if b {
		return domain.Scale
	}
	return 0
}

func unknownMerchant(id string, known []string) int16 {
	for i := range known {
		if known[i] == id {
			return 0
		}
	}
	return domain.Scale
}

func weekdayMonZero(t time.Time) int {
	return (int(t.Weekday()) + 6) % 7
}

// mccRiskQuant returns the quantized mcc_risk for dim 12. Uses a hardcoded
// switch with int16 values to skip the map lookup + float multiplication
// the previous version did.
func mccRiskQuant(mcc string) int16 {
	switch mcc {
	case "5411":
		return 1500
	case "5812":
		return 3000
	case "5912":
		return 2000
	case "5944":
		return 4500
	case "7801":
		return 8000
	case "7802":
		return 7500
	case "7995":
		return 8500
	case "4511":
		return 3500
	case "5311":
		return 2500
	case "5999":
		return 5000
	default:
		return 5000
	}
}
