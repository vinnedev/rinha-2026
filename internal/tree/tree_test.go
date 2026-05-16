package tree

import (
	"testing"

	"github.com/vinnedev/rinha-2026/internal/domain"
)

func loadOrSkip(tb testing.TB) *Tree {
	tb.Helper()
	t, err := Load("../../resources/fraud_dt.bin")
	if err != nil {
		tb.Skipf("fraud_dt.bin not found: %v", err)
	}
	return t
}

func TestPredictSpecExamples(t *testing.T) {
	tr := loadOrSkip(t)
	defer tr.Close()

	legit := [domain.Dim]float32{0.0041, 0.1667, 0.05, 0.7826, 0.3333, -1, -1, 0.0292, 0.15, 0, 1, 0, 0.15, 0.006}
	fraud := [domain.Dim]float32{0.9506, 0.8333, 1.0, 0.2174, 0.8333, -1, -1, 0.9523, 1.0, 0, 1, 1, 0.75, 0.0055}

	legitScore := tr.Predict(legit)
	fraudScore := tr.Predict(fraud)

	t.Logf("legit_score=%.4f  fraud_score=%.4f", legitScore, fraudScore)
	if legitScore >= 0.6 {
		t.Errorf("legit should be approved, got fraud_score=%.4f (>=0.6)", legitScore)
	}
	if fraudScore < 0.6 {
		t.Errorf("fraud should be denied, got fraud_score=%.4f (<0.6)", fraudScore)
	}
}

func BenchmarkPredict(b *testing.B) {
	tr := loadOrSkip(b)
	defer tr.Close()
	q := [domain.Dim]float32{0.0041, 0.1667, 0.05, 0.7826, 0.3333, -1, -1, 0.0292, 0.15, 0, 1, 0, 0.15, 0.006}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = tr.Predict(q)
	}
}
