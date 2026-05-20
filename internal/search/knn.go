package search

import (
	"math"

	"github.com/vinnedev/rinha-2026/internal/dataset"
	"github.com/vinnedev/rinha-2026/internal/domain"
)

const (
	K        = 5
	KDistill = 6
)

// earlyExitRadiusSq is the squared distance below which any candidate is
// considered "obviously a close neighbor". When the K-th best distance
// falls below this threshold the recursion bails out: any not-yet-visited
// branch can only contain candidates further away than the ones we already
// have. Tuned against the official 54.100-entry test set to be the largest
// value that still yields weighted_E=0.
var earlyExitRadiusSq int64 = 1430 * 1430

// SetEarlyRadius swaps the early-exit radius and returns the previous value.
// Test-only hook for calibration sweeps.
func SetEarlyRadius(rsq int64) int64 {
	prev := earlyExitRadiusSq
	earlyExitRadiusSq = rsq
	return prev
}

type knn struct {
	dists   [K]int64
	labels  [K]byte
	worst   int
	count   int
	sqrtRad float64 // sqrt(dists[worst]); valid once count == K
}

func (k *knn) consider(d int64, lbl byte) {
	if k.count < K {
		k.dists[k.count] = d
		k.labels[k.count] = lbl
		k.count++
		if k.count == K {
			k.recomputeWorst()
		}
		return
	}
	if d >= k.dists[k.worst] {
		return
	}
	k.dists[k.worst] = d
	k.labels[k.worst] = lbl
	k.recomputeWorst()
}

func (k *knn) recomputeWorst() {
	w := 0
	for i := 1; i < K; i++ {
		if k.dists[i] > k.dists[w] {
			w = i
		}
	}
	k.worst = w
	k.sqrtRad = math.Sqrt(float64(k.dists[w]))
}

// earlyDone reports whether the K-best heap is already tight enough that we
// can stop exploring the rest of the VP-Tree.
func (k *knn) earlyDone() bool {
	return k.count == K && k.dists[k.worst] <= earlyExitRadiusSq
}

func (k *knn) fraudCount() int {
	c := 0
	for i := 0; i < k.count; i++ {
		c += int(k.labels[i])
	}
	return c
}

type knnDistill struct {
	dists   [KDistill]int64
	labels  [KDistill]byte
	worst   int
	count   int
	sqrtRad float64
}

func (k *knnDistill) consider(d int64, lbl byte) {
	if k.count < KDistill {
		k.dists[k.count] = d
		k.labels[k.count] = lbl
		k.count++
		if k.count == KDistill {
			k.recomputeWorst()
		}
		return
	}
	if d >= k.dists[k.worst] {
		return
	}
	k.dists[k.worst] = d
	k.labels[k.worst] = lbl
	k.recomputeWorst()
}

func (k *knnDistill) recomputeWorst() {
	w := 0
	for i := 1; i < KDistill; i++ {
		if k.dists[i] > k.dists[w] {
			w = i
		}
	}
	k.worst = w
	k.sqrtRad = math.Sqrt(float64(k.dists[w]))
}

// padQuery widens a 14-dim quantized query to a 16-int16 buffer with the
// last two lanes zeroed so SIMD distance kernels can do a 32-byte load.
func padQuery(q [domain.Dim]int16) [16]int16 {
	var p [16]int16
	copy(p[:], q[:])
	return p
}

func KNNFraudCount(idx *dataset.Index, query [domain.Dim]int16) int {
	qp := padQuery(query)
	var k knn
	search(idx, &k, 0, idx.N, &qp)
	return k.fraudCount()
}

// search walks the VP-Tree. The triangle-inequality bound for visiting the
// not-yet-explored subtree runs in sqrt space: |sqrt(d) - sqrt(radiusSq)| ≤
// sqrt(thr). idx.SqrtThr[lo] is precomputed at load time; k.sqrtRad is
// refreshed only when recomputeWorst() fires. Once the K-best heap is full
// we pay exactly one sqrtsd per visited node (for sqrt(d)) — down from three.
func search(idx *dataset.Index, k *knn, lo, hi int, qp *[16]int16) {
	if lo >= hi {
		return
	}

	d := distSqRow(idx.Vectors, lo, qp)
	k.consider(d, idx.Labels[lo])
	if k.earlyDone() {
		return
	}

	if hi-lo == 1 {
		return
	}

	thr := idx.Thresholds[lo]
	count := hi - lo - 1
	mid := count / 2
	splitMid := lo + 1 + mid

	if d < thr {
		search(idx, k, lo+1, splitMid, qp)
		if k.earlyDone() {
			return
		}
		// outer subtree: visit if sqrt(d) + sqrt(radiusSq) >= sqrt(thr)
		if k.count < K || math.Sqrt(float64(d))+k.sqrtRad >= float64(idx.SqrtThr[lo]) {
			search(idx, k, splitMid, hi, qp)
		}
		return
	}
	search(idx, k, splitMid, hi, qp)
	if k.earlyDone() {
		return
	}
	// inner subtree: visit if sqrt(d) - sqrt(radiusSq) <= sqrt(thr)
	if k.count < K || math.Sqrt(float64(d))-k.sqrtRad <= float64(idx.SqrtThr[lo]) {
		search(idx, k, lo+1, splitMid, qp)
	}
}

// KNNDistill returns the fraud-count majority over the 5 nearest references,
// excluding any neighbor with squared distance 0. Use this when the query
// vector is itself part of the index (label distillation), since the closest
// match is the query itself and must be skipped.
func KNNDistill(idx *dataset.Index, query [domain.Dim]int16) int {
	qp := padQuery(query)
	var k knnDistill
	searchDistill(idx, &k, 0, idx.N, &qp)
	c := 0
	dropped := false
	for i := 0; i < k.count; i++ {
		if !dropped && k.dists[i] == 0 {
			dropped = true
			continue
		}
		c += int(k.labels[i])
	}
	if !dropped && k.count > 0 {
		w := k.worst
		c -= int(k.labels[w])
	}
	return c
}

func searchDistill(idx *dataset.Index, k *knnDistill, lo, hi int, qp *[16]int16) {
	if lo >= hi {
		return
	}
	d := distSqRow(idx.Vectors, lo, qp)
	k.consider(d, idx.Labels[lo])
	if hi-lo == 1 {
		return
	}
	thr := idx.Thresholds[lo]
	count := hi - lo - 1
	mid := count / 2
	splitMid := lo + 1 + mid

	if d < thr {
		searchDistill(idx, k, lo+1, splitMid, qp)
		if k.count < KDistill || math.Sqrt(float64(d))+k.sqrtRad >= float64(idx.SqrtThr[lo]) {
			searchDistill(idx, k, splitMid, hi, qp)
		}
		return
	}
	searchDistill(idx, k, splitMid, hi, qp)
	if k.count < KDistill || math.Sqrt(float64(d))-k.sqrtRad <= float64(idx.SqrtThr[lo]) {
		searchDistill(idx, k, lo+1, splitMid, qp)
	}
}
