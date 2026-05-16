package fraud

import (
	"testing"
	"time"

	"github.com/vinnedev/rinha-2026/internal/domain"
)

const specLegitPayload = `{
  "id": "tx-1329056812",
  "transaction": { "amount": 41.12, "installments": 2, "requested_at": "2026-03-11T18:45:53Z" },
  "customer":    { "avg_amount": 82.24, "tx_count_24h": 3, "known_merchants": ["MERC-003","MERC-016"] },
  "merchant":    { "id": "MERC-016", "mcc": "5411", "avg_amount": 60.25 },
  "terminal":    { "is_online": false, "card_present": true, "km_from_home": 29.23 },
  "last_transaction": null
}`

const specFraudWithHistory = `{
  "id": "tx-x",
  "transaction": {"amount": 100.0, "installments": 1, "requested_at":"2026-03-14T05:15:12Z"},
  "customer": {"avg_amount": 80.0, "tx_count_24h": 5, "known_merchants":[]},
  "merchant": {"id": "M", "mcc": "5411", "avg_amount": 50.0},
  "terminal": {"is_online": true, "card_present": false, "km_from_home": 10.5},
  "last_transaction": {"timestamp":"2026-03-13T05:15:12Z","km_from_current":42.0}
}`

func TestParsePayloadLegit(t *testing.T) {
	var p domain.FraudPayload
	if err := ParsePayload([]byte(specLegitPayload), &p); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Transaction.Amount != 41.12 {
		t.Errorf("amount: %v", p.Transaction.Amount)
	}
	if p.Transaction.Installments != 2 {
		t.Errorf("installments: %v", p.Transaction.Installments)
	}
	if got, want := p.Transaction.RequestedAt.UTC(), time.Date(2026, 3, 11, 18, 45, 53, 0, time.UTC); !got.Equal(want) {
		t.Errorf("requested_at: %v want %v", got, want)
	}
	if p.Customer.AvgAmount != 82.24 {
		t.Errorf("customer.avg: %v", p.Customer.AvgAmount)
	}
	if len(p.Customer.KnownMerchants) != 2 || p.Customer.KnownMerchants[0] != "MERC-003" || p.Customer.KnownMerchants[1] != "MERC-016" {
		t.Errorf("known_merchants: %v", p.Customer.KnownMerchants)
	}
	if p.Merchant.ID != "MERC-016" || p.Merchant.MCC != "5411" {
		t.Errorf("merchant: %v %v", p.Merchant.ID, p.Merchant.MCC)
	}
	if p.Terminal.IsOnline || !p.Terminal.CardPresent {
		t.Errorf("terminal: online=%v card=%v", p.Terminal.IsOnline, p.Terminal.CardPresent)
	}
	if p.LastTransaction != nil {
		t.Errorf("last_transaction should be nil, got %#v", p.LastTransaction)
	}
}

func TestParsePayloadWithLastTx(t *testing.T) {
	var p domain.FraudPayload
	if err := ParsePayload([]byte(specFraudWithHistory), &p); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.LastTransaction == nil {
		t.Fatalf("last_transaction should be non-nil")
	}
	if got, want := p.LastTransaction.Timestamp.UTC(), time.Date(2026, 3, 13, 5, 15, 12, 0, time.UTC); !got.Equal(want) {
		t.Errorf("timestamp: %v want %v", got, want)
	}
	if p.LastTransaction.KmFromCurrent != 42.0 {
		t.Errorf("km_from_current: %v", p.LastTransaction.KmFromCurrent)
	}
}

func BenchmarkParsePayload(b *testing.B) {
	body := []byte(specLegitPayload)
	var p domain.FraudPayload
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ParsePayload(body, &p)
	}
}
