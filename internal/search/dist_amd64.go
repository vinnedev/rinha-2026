//go:build amd64

package search

import (
	"unsafe"

	"golang.org/x/sys/cpu"

	"github.com/vinnedev/rinha-2026/internal/domain"
)

//go:noescape
func distSqAVX2(vecs, query unsafe.Pointer) int64

// distSqRow returns the 14-dim squared Euclidean distance between
// vecs[row*Dim..row*Dim+Dim) and the padded query. queryPadded must be a
// pointer to a [16]int16 whose last two lanes are zero — the caller is
// responsible for that padding (typically done once per query in KNN).
//
// On CPUs with AVX2 the heavy lifting goes through one VPSUBW + one
// VPMADDWD + a small horizontal reduction. The 16-wide load reads two
// int16s past the row, which is always inside the mmap'd file (labels
// follow vectors), so the read is safe; a VPAND with a precomputed mask
// zeros those lanes in the diff before squaring.
//
// Without AVX2 the scalar path below is identical to the original tight
// loop in knn.go.
func distSqRow(vecs []int16, row int, queryPadded *[16]int16) int64 {
	if useAVX2 {
		return distSqAVX2(
			unsafe.Pointer(&vecs[row*domain.Dim]),
			unsafe.Pointer(queryPadded),
		)
	}
	return distSqRowScalar(vecs, row, queryPadded)
}

var useAVX2 = cpu.X86.HasAVX2

func distSqRowScalar(vecs []int16, row int, qp *[16]int16) int64 {
	q := *qp
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
