package fraud

import (
	"time"

	"github.com/vinnedev/rinha-2026/internal/domain"
)

func Vectorize(p *domain.FraudPayload, out []int16) {
	t := p.Transaction.RequestedAt.UTC()

	out[0] = quant(p.Transaction.Amount / domain.MaxAmount)
	out[1] = quant(float64(p.Transaction.Installments) / domain.MaxInstallments)

	var avgRatio float64
	if p.Customer.AvgAmount > 0 {
		avgRatio = (p.Transaction.Amount / p.Customer.AvgAmount) / domain.AmountVsAvgRatio
	}
	out[2] = quant(avgRatio)

	out[3] = quant(float64(t.Hour()) / domain.HoursPerDay)
	out[4] = quant(float64(weekdayMonZero(t)) / domain.DaysPerWeek)

	if p.LastTransaction == nil {
		out[5] = domain.Sentinel
		out[6] = domain.Sentinel
	} else {
		minutes := t.Sub(p.LastTransaction.Timestamp.UTC()).Minutes()
		if minutes < 0 {
			minutes = 0
		}
		out[5] = quant(minutes / domain.MaxMinutes)
		out[6] = quant(p.LastTransaction.KmFromCurrent / domain.MaxKm)
	}

	out[7] = quant(p.Terminal.KmFromHome / domain.MaxKm)
	out[8] = quant(float64(p.Customer.TxCount24h) / domain.MaxTxCount24h)
	out[9] = boolDim(p.Terminal.IsOnline)
	out[10] = boolDim(p.Terminal.CardPresent)
	out[11] = unknownMerchant(p.Merchant.ID, p.Customer.KnownMerchants)
	out[12] = quant(domain.MccRisk(p.Merchant.MCC))
	out[13] = quant(p.Merchant.AvgAmount / domain.MaxMerchantAvgAmount)
}

func quant(v float64) int16 {
	x := int32(v*domain.Scale + 0.5)
	if x < 0 {
		return 0
	}
	if x > domain.Scale {
		return domain.Scale
	}
	return int16(x)
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
