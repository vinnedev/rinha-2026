package fraud

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/vinnedev/rinha-2026/internal/dataset"
	"github.com/vinnedev/rinha-2026/internal/domain"
	"github.com/vinnedev/rinha-2026/internal/search"
	"github.com/vinnedev/rinha-2026/internal/tree"
)

type testEntry struct {
	Request          domain.FraudPayload `json:"request"`
	ExpectedApproved bool                `json:"expected_approved"`
	ExpectedScore    float64             `json:"expected_fraud_score"`
}

type testDataset struct {
	Entries []testEntry `json:"entries"`
}

func loadTestEntries(tb testing.TB) []testEntry {
	tb.Helper()
	raw, err := os.ReadFile("../../test/test-data.json")
	if err != nil {
		tb.Skipf("test-data.json not found: %v", err)
	}
	var d testDataset
	if err := json.Unmarshal(raw, &d); err != nil {
		tb.Fatalf("decode: %v", err)
	}
	return d.Entries
}

type confusion struct {
	TP, TN, FP, FN int
}

func (c *confusion) record(predicted, expected bool) {
	switch {
	case !predicted && !expected:
		c.TP++
	case predicted && expected:
		c.TN++
	case !predicted && expected:
		c.FP++
	case predicted && !expected:
		c.FN++
	}
}

func (c confusion) accuracy(n int) float64 {
	return float64(c.TP+c.TN) / float64(n)
}

func (c confusion) weightedE() int { return c.FP + 3*c.FN }

func (c confusion) failureRate(n int) float64 {
	return float64(c.FP+c.FN) / float64(n)
}

// TestEvalHybridFull benchmarks DT-alone, oracle-alone, and the hybrid path
// against the official 54.100-entry test set. Reports accuracy, weighted_E,
// average latency for each, plus how often the hybrid fell back to oracle.
func TestEvalHybridFull(t *testing.T) {
	entries := loadTestEntries(t)

	tr, err := tree.Load("../../resources/fraud_dt.bin")
	if err != nil {
		t.Skipf("fraud_dt.bin not found: %v", err)
	}
	defer tr.Close()
	idx, err := dataset.Load("../../resources/vectors.bin")
	if err != nil {
		t.Skipf("vectors.bin not found: %v", err)
	}
	defer idx.Close()

	const threshold = 0.6
	bands := []struct{ lo, hi float64 }{
		{0.20, 0.80},
		{0.25, 0.75},
		{0.30, 0.70},
		{0.40, 0.60},
	}
	scale := float32(domain.Scale)

	var dt, oracle confusion
	var dtLat, orLat time.Duration

	// Pre-compute DT score and oracle score for each entry, plus latency
	dtScores := make([]float64, len(entries))
	orScores := make([]float64, len(entries))

	for i := range entries {
		e := &entries[i]
		var q [domain.Dim]int16
		var fq [domain.Dim]float32
		Vectorize(&e.Request, q[:])
		for j := 0; j < domain.Dim; j++ {
			fq[j] = float32(q[j]) / scale
		}

		t0 := time.Now()
		ds := float64(tr.Predict(fq))
		dtLat += time.Since(t0)
		dtScores[i] = ds

		t0 = time.Now()
		k := search.KNNFraudCount(idx, q)
		orLat += time.Since(t0)
		os := float64(k) / float64(search.K)
		orScores[i] = os

		dt.record(ds < threshold, e.ExpectedApproved)
		oracle.record(os < threshold, e.ExpectedApproved)
	}

	n := len(entries)
	t.Logf("entries: %d", n)
	t.Logf("")
	t.Logf("=== Model alone (RF distilled, 30 trees) ===")
	t.Logf("  acc=%.4f  TP=%d TN=%d FP=%d FN=%d  failure_rate=%.4f  weighted_E=%d  lat=%.2fµs",
		dt.accuracy(n), dt.TP, dt.TN, dt.FP, dt.FN, dt.failureRate(n), dt.weightedE(),
		float64(dtLat.Microseconds())/float64(n))
	t.Logf("")
	t.Logf("=== Oracle alone (VP-Tree exact k-NN) ===")
	t.Logf("  acc=%.4f  TP=%d TN=%d FP=%d FN=%d  failure_rate=%.4f  weighted_E=%d  lat=%.2fµs",
		oracle.accuracy(n), oracle.TP, oracle.TN, oracle.FP, oracle.FN, oracle.failureRate(n), oracle.weightedE(),
		float64(orLat.Microseconds())/float64(n))
	t.Logf("")
	t.Logf("=== Hybrid (sweep confidence bands) ===")

	for _, b := range bands {
		var hyb confusion
		fallback := 0
		var hybLat time.Duration
		for i := range entries {
			t0 := time.Now()
			ds := dtScores[i]
			var final float64
			if ds <= b.lo || ds >= b.hi {
				final = ds
			} else {
				fallback++
				final = orScores[i]
			}
			hybLat += time.Since(t0)
			hyb.record(final < threshold, entries[i].ExpectedApproved)
		}
		// Approximate latency: DT for confident + oracle for uncertain
		avgLat := (float64(dtLat.Microseconds())*float64(n-fallback) + float64(orLat.Microseconds())*float64(fallback)) / float64(n) / float64(n)
		_ = hybLat
		t.Logf("  band [%.2f,%.2f]: acc=%.4f  TP=%d TN=%d FP=%d FN=%d  weighted_E=%d  fallback=%.2f%%  approx_lat=%.2fµs",
			b.lo, b.hi, hyb.accuracy(n), hyb.TP, hyb.TN, hyb.FP, hyb.FN, hyb.weightedE(),
			100*float64(fallback)/float64(n), avgLat)
	}
}
