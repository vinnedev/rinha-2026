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

// Service implements a hybrid classifier:
//   - DT/RF predicts a fraud probability (constant-time tree walk)
//   - if the score is in the uncertain band [lo, hi], fall back to the
//     exact k-NN oracle (VP-Tree); otherwise the DT verdict is used as-is.
//
// idx may be nil: in that case the DT verdict is always used (no fallback).
type Service struct {
	tree *tree.Tree
	idx  *dataset.Index
	lo   float64
	hi   float64
}

func NewService(t *tree.Tree, idx *dataset.Index, lo, hi float64) *Service {
	return &Service{tree: t, idx: idx, lo: lo, hi: hi}
}

func (s *Service) Score(p *domain.FraudPayload) domain.FraudResponse {
	var q [domain.Dim]int16
	Vectorize(p, q[:])
	return s.scoreFromVector(q)
}

// ScoreInt is the fast path: take an already-parsed IntPayload (zero float,
// zero allocation) and run the same hybrid (RF → VP-Tree). Identical
// verdicts to Score for any payload; the only difference is shaved-off
// JSON parse + float→int conversion cost on the way in.
func (s *Service) ScoreInt(p *IntPayload) domain.FraudResponse {
	var q [domain.Dim]int16
	VectorizeInt(p, q[:])
	return s.scoreFromVector(q)
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
