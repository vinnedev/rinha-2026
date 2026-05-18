package search

import (
	"math"

	"github.com/vinnedev/rinha-2026/internal/dataset"
	"github.com/vinnedev/rinha-2026/internal/domain"
)

const (
	K        = 5
	KDistill = 6

	// earlyExitRadiusSq is the squared distance below which any candidate is
	// considered "obviously a close neighbor". When the K-th best distance
	// falls below this threshold the recursion bails out: any not-yet-visited
	// branch can only contain candidates further away than the ones we
	// already have. The constant mirrors RINHA_EARLY_DISTANCE_MILLI=140 from
	// the C top-1 implementation: 0.14 in normalized [0,1] coordinates,
	// squared, then upgraded to the Scale=10000 quantized space.
	earlyExitRadiusSq int64 = 1400 * 1400
)

type knn struct {
	dists  [K]int64
	labels [K]byte
	worst  int
	count  int
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
}

func (k *knn) radius() int64 {
	if k.count < K {
		return math.MaxInt64
	}
	return k.dists[k.worst]
}

// earlyDone reports whether the K-best heap is already tight enough that we
// can stop exploring the rest of the VP-Tree. We require the heap to be full
// and the worst-of-best to sit inside the earlyExitRadiusSq sphere.
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
	dists  [KDistill]int64
	labels [KDistill]byte
	worst  int
	count  int
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
}

func (k *knnDistill) radius() int64 {
	if k.count < KDistill {
		return math.MaxInt64
	}
	return k.dists[k.worst]
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
		if mayContain(d, thr, k.radius(), false) {
			search(idx, k, splitMid, hi, qp)
		}
		return
	}
	search(idx, k, splitMid, hi, qp)
	if k.earlyDone() {
		return
	}
	if mayContain(d, thr, k.radius(), true) {
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
		if mayContain(d, thr, k.radius(), false) {
			searchDistill(idx, k, splitMid, hi, qp)
		}
		return
	}
	searchDistill(idx, k, splitMid, hi, qp)
	if mayContain(d, thr, k.radius(), true) {
		searchDistill(idx, k, lo+1, splitMid, qp)
	}
}

// mayContain reports whether the not-yet-visited subtree could still hold a
// candidate within the current K-best radius. Works in squared-distance
// space using the triangle inequality.
func mayContain(d, thr, radiusSq int64, inner bool) bool {
	if radiusSq == math.MaxInt64 {
		return true
	}
	df := math.Sqrt(float64(d))
	tf := math.Sqrt(float64(thr))
	rf := math.Sqrt(float64(radiusSq))
	if inner {
		return df-rf <= tf
	}
	return df+rf >= tf
}
