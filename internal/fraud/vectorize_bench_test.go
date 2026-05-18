package fraud

import (
	"testing"
	"time"

	"github.com/vinnedev/rinha-2026/internal/domain"
)

func BenchmarkVectorize(b *testing.B) {
	ts, _ := time.Parse(time.RFC3339, "2026-03-11T18:45:53Z")
	p := &domain.FraudPayload{
		Transaction: domain.TransactionPayload{Amount: 41.12, Installments: 2, RequestedAt: ts},
		Customer:    domain.CustomerPayload{AvgAmount: 82.24, TxCount24h: 3, KnownMerchants: []string{"MERC-003", "MERC-016"}},
		Merchant:    domain.MerchantPayload{ID: "MERC-016", MCC: "5411", AvgAmount: 60.25},
		Terminal:    domain.TerminalPayload{IsOnline: false, CardPresent: true, KmFromHome: 29.23},
	}
	var out [domain.Dim]int16
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Vectorize(p, out[:])
	}
}
