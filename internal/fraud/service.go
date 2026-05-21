package fraud

import (
	"github.com/vinnedev/rinha-2026/internal/dataset"
	"github.com/vinnedev/rinha-2026/internal/domain"
	"github.com/vinnedev/rinha-2026/internal/search"
	"github.com/vinnedev/rinha-2026/internal/tree"
)

var scoreTable = [search.K + 1]float64{
	0.0,
	1.0 / float64(search.K),
	2.0 / float64(search.K),
	3.0 / float64(search.K),
	4.0 / float64(search.K),
	1.0,
}

// Service is a hybrid fraud classifier: the RF/DT walk gives a constant-time
// verdict; when that verdict falls inside the uncertain band [lo, hi], the
// exact k-NN oracle (VP-Tree) refines it. idx == nil disables the fallback.
type Service struct {
	tree *tree.Tree
	idx  *dataset.Index
	lo   float64
	hi   float64
}

func NewService(t *tree.Tree, idx *dataset.Index, lo, hi float64) *Service {
	return &Service{tree: t, idx: idx, lo: lo, hi: hi}
}

func (s *Service) ScoreInt(p *IntPayload) domain.FraudResponse {
	var q [domain.Dim]int16
	VectorizeInt(p, q[:])
	return s.scoreFromVector(q)
}

// ScoreIntFast skips the KNN oracle entirely and returns the DT/RF verdict.
// 97.68% accurate on the official 54.100-entry eval set (FP=44, FN=1213,
// weighted_E=3683). Used as the shed-fallback under burst: bounds p99 by
// avoiding the KNN tail while keeping the answer ~mostly correct instead
// of the wrong-answer-always shed of v8/v9 era.
func (s *Service) ScoreIntFast(p *IntPayload) domain.FraudResponse {
	var q [domain.Dim]int16
	VectorizeInt(p, q[:])
	var fq [domain.Dim]float32
	scale := float32(domain.Scale)
	for i := 0; i < domain.Dim; i++ {
		fq[i] = float32(q[i]) / scale
	}
	dtScore := float64(s.tree.Predict(fq))
	return domain.FraudResponse{
		Approved:   dtScore < domain.FraudThreshold,
		FraudScore: dtScore,
	}
}

func (s *Service) scoreFromVector(q [domain.Dim]int16) domain.FraudResponse {
	var fq [domain.Dim]float32
	scale := float32(domain.Scale)
	for i := 0; i < domain.Dim; i++ {
		fq[i] = float32(q[i]) / scale
	}

	dtScore := float64(s.tree.Predict(fq))

	if s.idx == nil || dtScore <= s.lo || dtScore >= s.hi {
		return domain.FraudResponse{
			Approved:   dtScore < domain.FraudThreshold,
			FraudScore: dtScore,
		}
	}

	k := search.KNNFraudCount(s.idx, q)
	score := scoreTable[k]
	return domain.FraudResponse{
		Approved:   score < domain.FraudThreshold,
		FraudScore: score,
	}
}
