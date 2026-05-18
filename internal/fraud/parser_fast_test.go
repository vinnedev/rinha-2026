package fraud

import (
	"testing"
)

func TestParseFastLegit(t *testing.T) {
	body := []byte(`{
  "id": "tx-1329056812",
  "transaction": { "amount": 41.12, "installments": 2, "requested_at": "2026-03-11T18:45:53Z" },
  "customer":    { "avg_amount": 82.24, "tx_count_24h": 3, "known_merchants": ["MERC-003","MERC-016"] },
  "merchant":    { "id": "MERC-016", "mcc": "5411", "avg_amount": 60.25 },
  "terminal":    { "is_online": false, "card_present": true, "km_from_home": 29.23 },
  "last_transaction": null
}`)
	var p IntPayload
	if !ParseFast(body, &p) {
		t.Fatal("ParseFast failed")
	}
	if p.AmountMilli != 41120 {
		t.Errorf("amount_milli: got %d want 41120", p.AmountMilli)
	}
	if p.Installments != 2 {
		t.Errorf("installments: %d", p.Installments)
	}
	if p.Hour != 18 {
		t.Errorf("hour: %d", p.Hour)
	}
	if p.DayOfWeek != 2 {
		t.Errorf("dow: %d (want 2 = Wed)", p.DayOfWeek)
	}
	if p.CustomerAvgAmountMilli != 82240 {
		t.Errorf("avg: %d", p.CustomerAvgAmountMilli)
	}
	if p.TxCount24h != 3 {
		t.Errorf("tx24h: %d", p.TxCount24h)
	}
	if p.Mcc != 5411 {
		t.Errorf("mcc: %d", p.Mcc)
	}
	if p.MerchantAvgAmountMilli != 60250 {
		t.Errorf("merchant avg: %d", p.MerchantAvgAmountMilli)
	}
	if p.IsOnline {
		t.Error("is_online should be false")
	}
	if !p.CardPresent {
		t.Error("card_present should be true")
	}
	if p.IsUnknownMerchant {
		t.Error("merchant MERC-016 is in known_merchants → not unknown")
	}
	if p.HasLastTx {
		t.Error("last_transaction should be null")
	}
	if p.KmFromHomeMilli != 29230 {
		t.Errorf("km_from_home: %d", p.KmFromHomeMilli)
	}
}

func TestParseFastLastTx(t *testing.T) {
	body := []byte(`{"id":"x","transaction":{"amount":100,"installments":1,"requested_at":"2026-03-11T18:45:53Z"},"customer":{"avg_amount":80,"tx_count_24h":3,"known_merchants":[]},"merchant":{"id":"M","mcc":"5411","avg_amount":50},"terminal":{"is_online":true,"card_present":false,"km_from_home":10.5},"last_transaction":{"timestamp":"2026-03-10T18:45:53Z","km_from_current":42.5}}`)
	var p IntPayload
	if !ParseFast(body, &p) {
		t.Fatal("ParseFast failed")
	}
	if !p.HasLastTx {
		t.Fatal("has_last_tx should be true")
	}
	if p.KmFromCurrentMilli != 42500 {
		t.Errorf("km_from_current: %d", p.KmFromCurrentMilli)
	}
	if p.MinutesSinceLast != 1440 {
		t.Errorf("minutes: %d (want 1440 = 24h)", p.MinutesSinceLast)
	}
	if !p.IsUnknownMerchant {
		t.Error("merchant 'M' not in empty known_merchants → unknown")
	}
}

const specBody = `{
  "id": "tx-1329056812",
  "transaction": { "amount": 41.12, "installments": 2, "requested_at": "2026-03-11T18:45:53Z" },
  "customer":    { "avg_amount": 82.24, "tx_count_24h": 3, "known_merchants": ["MERC-003","MERC-016"] },
  "merchant":    { "id": "MERC-016", "mcc": "5411", "avg_amount": 60.25 },
  "terminal":    { "is_online": false, "card_present": true, "km_from_home": 29.23 },
  "last_transaction": null
}`

func BenchmarkParseFast(b *testing.B) {
	body := []byte(specBody)
	var p IntPayload
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ParseFast(body, &p)
	}
}
