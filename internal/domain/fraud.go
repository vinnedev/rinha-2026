package domain

import "time"

const (
	Dim      = 14
	Scale    = 10000
	Sentinel = -Scale
)

type FraudPayload struct {
	ID              string             `json:"id"`
	Transaction     TransactionPayload `json:"transaction"`
	Customer        CustomerPayload    `json:"customer"`
	Merchant        MerchantPayload    `json:"merchant"`
	Terminal        TerminalPayload    `json:"terminal"`
	LastTransaction *LastTxPayload     `json:"last_transaction"`
}

type TransactionPayload struct {
	Amount       float64   `json:"amount"`
	Installments int       `json:"installments"`
	RequestedAt  time.Time `json:"requested_at"`
}

type CustomerPayload struct {
	AvgAmount      float64  `json:"avg_amount"`
	TxCount24h     int      `json:"tx_count_24h"`
	KnownMerchants []string `json:"known_merchants"`
}

type MerchantPayload struct {
	ID        string  `json:"id"`
	MCC       string  `json:"mcc"`
	AvgAmount float64 `json:"avg_amount"`
}

type TerminalPayload struct {
	IsOnline    bool    `json:"is_online"`
	CardPresent bool    `json:"card_present"`
	KmFromHome  float64 `json:"km_from_home"`
}

type LastTxPayload struct {
	Timestamp     time.Time `json:"timestamp"`
	KmFromCurrent float64   `json:"km_from_current"`
}

type FraudResponse struct {
	Approved   bool    `json:"approved"`
	FraudScore float64 `json:"fraud_score"`
}
