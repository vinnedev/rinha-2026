package fraud

// IntPayload is the parsed-payload shape used by the fast path: every
// numeric field already in integer-milli (or unit) form, every boolean
// resolved, and the unknown-merchant comparison materialized at parse
// time. The hot path can read this struct straight into the quantizer
// without ever touching float64.
type IntPayload struct {
	AmountMilli            uint64
	Installments           uint8
	Hour                   uint8
	DayOfWeek              uint8
	CustomerAvgAmountMilli uint64
	TxCount24h             uint32
	KmFromHomeMilli        uint64
	MerchantAvgAmountMilli uint64
	IsOnline               bool
	CardPresent            bool
	IsUnknownMerchant      bool
	HasLastTx              bool
	KmFromCurrentMilli     uint64
	MinutesSinceLast       uint32
	Mcc                    uint32
}
