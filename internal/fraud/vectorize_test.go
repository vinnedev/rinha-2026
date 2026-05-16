package fraud

import (
	"testing"
	"time"

	"github.com/vinnedev/rinha-2026/internal/domain"
)

func TestVectorizeLegitExample(t *testing.T) {
	ts, _ := time.Parse(time.RFC3339, "2026-03-11T18:45:53Z")
	p := &domain.FraudPayload{
		ID: "tx-1329056812",
		Transaction: domain.TransactionPayload{
			Amount: 41.12, Installments: 2, RequestedAt: ts,
		},
		Customer: domain.CustomerPayload{
			AvgAmount: 82.24, TxCount24h: 3,
			KnownMerchants: []string{"MERC-003", "MERC-016"},
		},
		Merchant: domain.MerchantPayload{ID: "MERC-016", MCC: "5411", AvgAmount: 60.25},
		Terminal: domain.TerminalPayload{IsOnline: false, CardPresent: true, KmFromHome: 29.23},
	}
	want := [domain.Dim]int16{41, 1667, 500, 7826, 3333, domain.Sentinel, domain.Sentinel, 292, 1500, 0, domain.Scale, 0, 1500, 60}
	var got [domain.Dim]int16
	Vectorize(p, got[:])
	for i := 0; i < domain.Dim; i++ {
		if got[i] != want[i] {
			t.Errorf("dim %d: got %d, want %d", i, got[i], want[i])
		}
	}
}

func TestVectorizeFraudExample(t *testing.T) {
	ts, _ := time.Parse(time.RFC3339, "2026-03-14T05:15:12Z")
	p := &domain.FraudPayload{
		ID: "tx-3330991687",
		Transaction: domain.TransactionPayload{
			Amount: 9505.97, Installments: 10, RequestedAt: ts,
		},
		Customer: domain.CustomerPayload{
			AvgAmount: 81.28, TxCount24h: 20,
			KnownMerchants: []string{"MERC-008", "MERC-007", "MERC-005"},
		},
		Merchant: domain.MerchantPayload{ID: "MERC-068", MCC: "7802", AvgAmount: 54.86},
		Terminal: domain.TerminalPayload{IsOnline: false, CardPresent: true, KmFromHome: 952.27},
	}
	want := [domain.Dim]int16{9506, 8333, domain.Scale, 2174, 8333, domain.Sentinel, domain.Sentinel, 9523, domain.Scale, 0, domain.Scale, domain.Scale, 7500, 55}
	var got [domain.Dim]int16
	Vectorize(p, got[:])
	for i := 0; i < domain.Dim; i++ {
		if got[i] != want[i] {
			t.Errorf("dim %d: got %d, want %d", i, got[i], want[i])
		}
	}
}
