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

type knn struct {
	dists  [K]int64
	labels [K]byte
	worst  int
	count  int
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
		return 1<<63 - 1
	}
	return k.dists[k.worst]
}

// KNNDistill returns the fraud-count majority over the 5 nearest references,
// excluding any neighbor with squared distance 0. Use this when the query
// vector is itself part of the index (label distillation), since the closest
// match is the query itself and must be skipped.
func KNNDistill(idx *dataset.Index, query [domain.Dim]int16) int {
	var k knnDistill
	searchDistill(idx, &k, 0, idx.N, query)
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
		// no self found: pop worst neighbor to keep k=5 majority count
		w := k.worst
		c -= int(k.labels[w])
	}
	return c
}

func searchDistill(idx *dataset.Index, k *knnDistill, lo, hi int, q [domain.Dim]int16) {
	if lo >= hi {
		return
	}
	d := distSqAt(idx.Vectors, lo, q)
	k.consider(d, idx.Labels[lo])
	if hi-lo == 1 {
		return
	}
	thr := idx.Thresholds[lo]
	count := hi - lo - 1
	mid := count / 2
	splitMid := lo + 1 + mid

	if d < thr {
		searchDistill(idx, k, lo+1, splitMid, q)
		if mayContain(d, thr, k.radius(), false) {
			searchDistill(idx, k, splitMid, hi, q)
		}
		return
	}
	searchDistill(idx, k, splitMid, hi, q)
	if mayContain(d, thr, k.radius(), true) {
		searchDistill(idx, k, lo+1, splitMid, q)
	}
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

func (k *knn) fraudCount() int {
	c := 0
	for i := 0; i < k.count; i++ {
		c += int(k.labels[i])
	}
	return c
}

func KNNFraudCount(idx *dataset.Index, query [domain.Dim]int16) int {
	var k knn
	search(idx, &k, 0, idx.N, query)
	return k.fraudCount()
}

func search(idx *dataset.Index, k *knn, lo, hi int, q [domain.Dim]int16) {
	if lo >= hi {
		return
	}

	d := distSqAt(idx.Vectors, lo, q)
	k.consider(d, idx.Labels[lo])

	if hi-lo == 1 {
		return
	}

	thr := idx.Thresholds[lo]
	count := hi - lo - 1
	mid := count / 2
	splitMid := lo + 1 + mid

	if d < thr {
		search(idx, k, lo+1, splitMid, q)
		if mayContain(d, thr, k.radius(), false) {
			search(idx, k, splitMid, hi, q)
		}
		return
	}
	search(idx, k, splitMid, hi, q)
	if mayContain(d, thr, k.radius(), true) {
		search(idx, k, lo+1, splitMid, q)
	}
}

// mayContain reports whether the not-yet-visited subtree could still hold a
// candidate within the current K-best radius. Works in squared-distance space
// using the triangle inequality:
//
//	|dist(q, p) − dist(q, vp)|² ≤ dist(p, vp)² ≤ (dist(q, p) + dist(q, vp))²
//
// inner=false means the outer subtree (dist(p, vp)² ≥ thr) is the candidate,
// inner=true means the inner subtree (dist(p, vp)² < thr) is.
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

func distSqAt(vecs []int16, row int, q [domain.Dim]int16) int64 {
	base := row * domain.Dim
	d0 := int32(vecs[base]) - int32(q[0])
	d1 := int32(vecs[base+1]) - int32(q[1])
	d2 := int32(vecs[base+2]) - int32(q[2])
	d3 := int32(vecs[base+3]) - int32(q[3])
	d4 := int32(vecs[base+4]) - int32(q[4])
	d5 := int32(vecs[base+5]) - int32(q[5])
	d6 := int32(vecs[base+6]) - int32(q[6])
	d7 := int32(vecs[base+7]) - int32(q[7])
	d8 := int32(vecs[base+8]) - int32(q[8])
	d9 := int32(vecs[base+9]) - int32(q[9])
	d10 := int32(vecs[base+10]) - int32(q[10])
	d11 := int32(vecs[base+11]) - int32(q[11])
	d12 := int32(vecs[base+12]) - int32(q[12])
	d13 := int32(vecs[base+13]) - int32(q[13])

	return int64(d0)*int64(d0) +
		int64(d1)*int64(d1) +
		int64(d2)*int64(d2) +
		int64(d3)*int64(d3) +
		int64(d4)*int64(d4) +
		int64(d5)*int64(d5) +
		int64(d6)*int64(d6) +
		int64(d7)*int64(d7) +
		int64(d8)*int64(d8) +
		int64(d9)*int64(d9) +
		int64(d10)*int64(d10) +
		int64(d11)*int64(d11) +
		int64(d12)*int64(d12) +
		int64(d13)*int64(d13)
}
