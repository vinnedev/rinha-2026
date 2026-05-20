package dataset

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"unsafe"

	"github.com/vinnedev/rinha-2026/internal/domain"
)

const (
	Magic    = uint32(0x52494E48)
	Version  = uint32(2)
	HeaderSz = 16
)

type Index struct {
	N          int
	Vectors    []int16
	Labels     []byte
	Thresholds []int64
	// SqrtThr[i] = sqrt(Thresholds[i]) precomputed at load time. The
	// VP-Tree's triangle-inequality bound runs in sqrt space; precomputing
	// here cuts one sqrtsd (~14 cycles on Haswell) per visited node.
	// float32 keeps heap cost at 12MB (vs 24MB for float64) — max value is
	// sqrt(5.6e9) ≈ 75k, well inside float32's 24-bit mantissa.
	SqrtThr []float32
	raw     []byte
}

func Load(path string) (*Index, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("dataset open: %w", err)
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("dataset stat: %w", err)
	}
	sz := int(st.Size())
	if sz < HeaderSz {
		return nil, errors.New("dataset: file too small")
	}

	raw, err := mmapReadOnly(f, sz)
	if err != nil {
		return nil, fmt.Errorf("dataset mmap: %w", err)
	}

	if binary.LittleEndian.Uint32(raw[0:4]) != Magic {
		_ = munmap(raw)
		return nil, errors.New("dataset: bad magic")
	}
	if binary.LittleEndian.Uint32(raw[4:8]) != Version {
		_ = munmap(raw)
		return nil, errors.New("dataset: version mismatch (run preprocess)")
	}
	n := int(binary.LittleEndian.Uint32(raw[8:12]))
	dim := int(binary.LittleEndian.Uint32(raw[12:16]))
	if dim != domain.Dim {
		_ = munmap(raw)
		return nil, fmt.Errorf("dataset: dim mismatch %d", dim)
	}

	vecBytes := n * domain.Dim * 2
	thrBytes := n * 8
	want := HeaderSz + vecBytes + n + thrBytes
	if sz < want {
		_ = munmap(raw)
		return nil, fmt.Errorf("dataset: truncated %d < %d", sz, want)
	}

	vecOff := HeaderSz
	lblOff := vecOff + vecBytes
	thrOff := lblOff + n

	vectors := unsafe.Slice((*int16)(unsafe.Pointer(&raw[vecOff])), n*domain.Dim)
	labels := raw[lblOff : lblOff+n]
	thresholds := unsafe.Slice((*int64)(unsafe.Pointer(&raw[thrOff])), n)

	sqrtThr := make([]float32, n)
	for i := 0; i < n; i++ {
		sqrtThr[i] = float32(math.Sqrt(float64(thresholds[i])))
	}

	idx := &Index{
		N: n, Vectors: vectors, Labels: labels, Thresholds: thresholds, SqrtThr: sqrtThr, raw: raw,
	}
	idx.preload()
	return idx, nil
}

func (i *Index) Close() error {
	if i == nil || i.raw == nil {
		return nil
	}
	err := munmap(i.raw)
	i.raw = nil
	i.Vectors = nil
	i.Labels = nil
	i.Thresholds = nil
	i.SqrtThr = nil
	return err
}

func (i *Index) preload() {
	var sum byte
	for k := 0; k < len(i.raw); k += 4096 {
		sum ^= i.raw[k]
	}
	_ = sum
}
