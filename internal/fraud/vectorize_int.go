package fraud

import "github.com/vinnedev/rinha-2026/internal/domain"

// VectorizeInt is the IntPayload→[14]int16 transform. Same output contract
// as Vectorize but every input is already integer-milli, so the body is
// purely integer math (no float division, no float→int conversions).
//
// The shared helpers quantMilli/amountRatio/boolDim live in vectorize.go;
// this file just orchestrates them from the IntPayload fields.
func VectorizeInt(p *IntPayload, out []int16) {
	out[0] = quantMilli(p.AmountMilli, maxAmountMilli)
	out[1] = quantMilli(uint64(p.Installments)*1000, maxInstallmentsMilli)
	out[2] = amountRatio(p.AmountMilli, p.CustomerAvgAmountMilli)
	out[3] = quantMilli(uint64(p.Hour)*1000, hoursPerDayMilli)
	out[4] = quantMilli(uint64(p.DayOfWeek)*1000, daysPerWeekMilli)
	if !p.HasLastTx {
		out[5] = domain.Sentinel
		out[6] = domain.Sentinel
	} else {
		out[5] = quantMilli(uint64(p.MinutesSinceLast)*1000, maxMinutesMilli)
		out[6] = quantMilli(p.KmFromCurrentMilli, maxKmMilli)
	}
	out[7] = quantMilli(p.KmFromHomeMilli, maxKmMilli)
	out[8] = quantMilli(uint64(p.TxCount24h)*1000, maxTxCount24hMilli)
	if p.IsOnline {
		out[9] = domain.Scale
	} else {
		out[9] = 0
	}
	if p.CardPresent {
		out[10] = domain.Scale
	} else {
		out[10] = 0
	}
	if p.IsUnknownMerchant {
		out[11] = domain.Scale
	} else {
		out[11] = 0
	}
	out[12] = mccRiskQuantInt(p.Mcc)
	out[13] = quantMilli(p.MerchantAvgAmountMilli, maxMerchantAvgAmountMilli)
}

// mccRiskQuantInt does the same MCC table lookup as mccRiskQuant but on
// the int32 form the positional parser leaves in IntPayload.Mcc.
func mccRiskQuantInt(mcc uint32) int16 {
	switch mcc {
	case 5411:
		return 1500
	case 5812:
		return 3000
	case 5912:
		return 2000
	case 5944:
		return 4500
	case 7801:
		return 8000
	case 7802:
		return 7500
	case 7995:
		return 8500
	case 4511:
		return 3500
	case 5311:
		return 2500
	case 5999:
		return 5000
	default:
		return 5000
	}
}
