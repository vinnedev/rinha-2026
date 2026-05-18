package fraud

import "math/rand/v2"

// Warmup runs n synthetic ScoreInt calls before the listener opens. The
// goal is to warm L1/L2/L3 caches, the branch predictor, and bring the
// mmap'd VP-Tree pages into RAM so the first real request doesn't pay
// cold-cache penalty on the hot search path.
//
// Inputs are deterministic via a fixed PCG seed so warmup is reproducible
// across container restarts and PGO-profiling runs.
func (s *Service) Warmup(n int) {
	if s == nil || n <= 0 {
		return
	}
	rng := rand.New(rand.NewPCG(0xC0FFEE, 0xCAFE))
	var p IntPayload
	for i := 0; i < n; i++ {
		p.AmountMilli = uint64(rng.Uint64N(10_000_000))
		p.Installments = uint8(rng.UintN(12) + 1)
		p.Hour = uint8(rng.UintN(24))
		p.DayOfWeek = uint8(rng.UintN(7))
		p.CustomerAvgAmountMilli = uint64(rng.Uint64N(5_000_000) + 10_000)
		p.TxCount24h = uint32(rng.UintN(20))
		p.KmFromHomeMilli = uint64(rng.Uint64N(1_000_000))
		p.MerchantAvgAmountMilli = uint64(rng.Uint64N(5_000_000) + 10_000)
		p.IsOnline = (i & 1) == 0
		p.CardPresent = (i & 2) == 0
		p.IsUnknownMerchant = (i & 4) == 0
		p.HasLastTx = (i & 8) == 0
		p.KmFromCurrentMilli = uint64(rng.Uint64N(1_000_000))
		p.MinutesSinceLast = uint32(rng.UintN(2880))
		mccs := [10]uint32{5411, 5812, 5912, 5944, 7801, 7802, 7995, 4511, 5311, 5999}
		p.Mcc = mccs[i%len(mccs)]
		_ = s.ScoreInt(&p)
	}
}
