package search

import (
	"sort"
	"testing"

	"github.com/vinnedev/rinha-2026/internal/dataset"
	"github.com/vinnedev/rinha-2026/internal/domain"
)

func loadIdx(tb testing.TB) *dataset.Index {
	tb.Helper()
	idx, err := dataset.Load("../../resources/vectors.bin")
	if err != nil {
		tb.Fatalf("load: %v", err)
	}
	return idx
}

// bruteForce5 returns the 5 nearest labels from a linear scan — the oracle.
func bruteForce5(idx *dataset.Index, q [domain.Dim]int16) int {
	type entry struct {
		d   int64
		lbl byte
	}
	all := make([]entry, idx.N)
	for i := 0; i < idx.N; i++ {
		base := i * domain.Dim
		var s int64
		for j := 0; j < domain.Dim; j++ {
			d := int32(idx.Vectors[base+j]) - int32(q[j])
			s += int64(d) * int64(d)
		}
		all[i] = entry{s, idx.Labels[i]}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].d < all[j].d })
	c := 0
	for i := 0; i < K; i++ {
		c += int(all[i].lbl)
	}
	return c
}

func TestVPTreeMatchesBruteForce(t *testing.T) {
	idx := loadIdx(t)
	defer idx.Close()

	queries := [][domain.Dim]int16{
		{41, 1667, 500, 7826, 3333, domain.Sentinel, domain.Sentinel, 292, 1500, 0, domain.Scale, 0, 1500, 60},
		{9506, 8333, domain.Scale, 2174, 8333, domain.Sentinel, domain.Sentinel, 9523, domain.Scale, 0, domain.Scale, domain.Scale, 7500, 55},
		{5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000, 5000},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{domain.Scale, domain.Scale, domain.Scale, domain.Scale, domain.Scale, domain.Scale, domain.Scale, domain.Scale, domain.Scale, domain.Scale, domain.Scale, domain.Scale, domain.Scale, domain.Scale},
	}
	for i, q := range queries {
		want := bruteForce5(idx, q)
		got := KNNFraudCount(idx, q)
		if got != want {
			t.Errorf("query %d: vp=%d brute=%d", i, got, want)
		}
	}
}

func BenchmarkKNN(b *testing.B) {
	idx := loadIdx(b)
	defer idx.Close()
	q := [domain.Dim]int16{41, 1667, 500, 7826, 3333, domain.Sentinel, domain.Sentinel, 292, 1500, 0, domain.Scale, 0, 1500, 60}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = KNNFraudCount(idx, q)
	}
}

func BenchmarkKNNRandom(b *testing.B) {
	idx := loadIdx(b)
	defer idx.Close()
	queries := make([][domain.Dim]int16, 1000)
	seed := uint32(1)
	for i := range queries {
		for j := 0; j < domain.Dim; j++ {
			seed = seed*1664525 + 1013904223
			queries[i][j] = int16(seed % uint32(domain.Scale+1))
		}
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = KNNFraudCount(idx, queries[i%len(queries)])
	}
}
