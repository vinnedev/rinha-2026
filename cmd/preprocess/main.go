package main

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"time"
)

const (
	dim      = 14
	scale    = 10000
	sentinel = -scale
	magic    = uint32(0x52494E48)
	version  = uint32(2)
	headerSz = 16
)

type record struct {
	Vector [dim]float64 `json:"vector"`
	Label  string       `json:"label"`
}

func main() {
	inPath := "resources/references.json.gz"
	outPath := "resources/vectors.bin"
	if len(os.Args) > 1 {
		inPath = os.Args[1]
	}
	if len(os.Args) > 2 {
		outPath = os.Args[2]
	}

	start := time.Now()
	n, err := run(inPath, outPath)
	if err != nil {
		log.Fatalf("preprocess: %v", err)
	}
	log.Printf("preprocess: wrote %d vectors (VP-Tree) to %s in %s", n, outPath, time.Since(start))
}

func run(inPath, outPath string) (uint32, error) {
	vecs, labels, err := readGzip(inPath)
	if err != nil {
		return 0, err
	}
	log.Printf("preprocess: parsed %d vectors", len(labels))

	n := len(labels)
	thresholds := make([]int64, n)
	rng := rand.New(rand.NewPCG(0xC0FFEE, 0xBABE))
	buildVPTree(vecs, labels, thresholds, 0, n, rng)
	log.Printf("preprocess: vp-tree built")

	return writeIndex(outPath, vecs, labels, thresholds)
}

func readGzip(path string) ([]int16, []byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open input: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(bufio.NewReaderSize(f, 1<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	dec := json.NewDecoder(bufio.NewReaderSize(gz, 4<<20))
	if _, err := dec.Token(); err != nil {
		return nil, nil, fmt.Errorf("read open bracket: %w", err)
	}

	vecs := make([]int16, 0, 3_000_000*dim)
	labels := make([]byte, 0, 3_000_000)

	for dec.More() {
		var r record
		if err := dec.Decode(&r); err != nil {
			return nil, nil, fmt.Errorf("decode at %d: %w", len(labels), err)
		}
		for i := 0; i < dim; i++ {
			vecs = append(vecs, quantize(r.Vector[i]))
		}
		var lbl byte
		if r.Label == "fraud" {
			lbl = 1
		}
		labels = append(labels, lbl)
	}
	return vecs, labels, nil
}

func quantize(v float64) int16 {
	if v == -1 {
		return sentinel
	}
	x := int32(v*scale + 0.5)
	if x < 0 {
		return 0
	}
	if x > scale {
		return scale
	}
	return int16(x)
}

func distSq(vecs []int16, a, b int) int64 {
	ai := a * dim
	bi := b * dim
	var s int64
	for i := 0; i < dim; i++ {
		d := int32(vecs[ai+i]) - int32(vecs[bi+i])
		s += int64(d) * int64(d)
	}
	return s
}

func swap(vecs []int16, labels []byte, a, b int) {
	if a == b {
		return
	}
	ai := a * dim
	bi := b * dim
	for i := 0; i < dim; i++ {
		vecs[ai+i], vecs[bi+i] = vecs[bi+i], vecs[ai+i]
	}
	labels[a], labels[b] = labels[b], labels[a]
}

// buildVPTree rearranges vecs[lo:hi] and labels[lo:hi] in VP-tree order.
// Convention: arr[lo] is the vantage point for the subtree.
// thresholds[lo] = median squared distance from VP to the rest.
// Left subtree: [lo+1, mid+1)   (points with dist² <= threshold)
// Right subtree: [mid+1, hi)    (points with dist² > threshold)
// where mid = lo + 1 + (hi - lo - 1)/2.
func buildVPTree(vecs []int16, labels []byte, thresholds []int64, lo, hi int, rng *rand.Rand) {
	if hi-lo <= 1 {
		return
	}

	pivot := lo + rng.IntN(hi-lo)
	swap(vecs, labels, lo, pivot)

	if hi-lo == 2 {
		thresholds[lo] = distSq(vecs, lo, lo+1)
		return
	}

	count := hi - lo - 1
	dists := make([]int64, count)
	for k := 0; k < count; k++ {
		dists[k] = distSq(vecs, lo, lo+1+k)
	}

	mid := count / 2
	nthElement(vecs, labels, dists, lo+1, 0, count, mid)
	thresholds[lo] = dists[mid]

	splitMid := lo + 1 + mid
	buildVPTree(vecs, labels, thresholds, lo+1, splitMid, rng)
	buildVPTree(vecs, labels, thresholds, splitMid, hi, rng)
}

// nthElement partitions dists so that dists[k] is the k-th smallest. dists
// indices [0, len) correspond to vecs rows [vecsOff, vecsOff+len). After
// return, dists[0..k] <= dists[k] <= dists[k..len) — and vec rows are kept in
// sync with their distances.
func nthElement(vecs []int16, labels []byte, dists []int64, vecsOff, lo, hi, k int) {
	for hi-lo > 1 {
		p := partition(vecs, labels, dists, vecsOff, lo, hi)
		if p == k {
			return
		}
		if k < p {
			hi = p
			continue
		}
		lo = p + 1
	}
}

func partition(vecs []int16, labels []byte, dists []int64, vecsOff, lo, hi int) int {
	pivotIdx := lo + (hi-lo)/2
	pivotVal := dists[pivotIdx]
	swapTriple(vecs, labels, dists, vecsOff, pivotIdx, hi-1)
	store := lo
	for i := lo; i < hi-1; i++ {
		if dists[i] < pivotVal {
			swapTriple(vecs, labels, dists, vecsOff, i, store)
			store++
		}
	}
	swapTriple(vecs, labels, dists, vecsOff, store, hi-1)
	return store
}

func swapTriple(vecs []int16, labels []byte, dists []int64, vecsOff, a, b int) {
	if a == b {
		return
	}
	dists[a], dists[b] = dists[b], dists[a]
	swap(vecs, labels, vecsOff+a, vecsOff+b)
}

func writeIndex(outPath string, vecs []int16, labels []byte, thresholds []int64) (uint32, error) {
	tmp := outPath + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	bw := bufio.NewWriterSize(out, 4<<20)

	n := uint32(len(labels))
	var hdr [headerSz]byte
	binary.LittleEndian.PutUint32(hdr[0:4], magic)
	binary.LittleEndian.PutUint32(hdr[4:8], version)
	binary.LittleEndian.PutUint32(hdr[8:12], n)
	binary.LittleEndian.PutUint32(hdr[12:16], dim)
	if _, err := bw.Write(hdr[:]); err != nil {
		return 0, err
	}

	var buf [dim * 2]byte
	for i := 0; i < int(n); i++ {
		base := i * dim
		for j := 0; j < dim; j++ {
			binary.LittleEndian.PutUint16(buf[j*2:], uint16(vecs[base+j]))
		}
		if _, err := bw.Write(buf[:]); err != nil {
			return 0, err
		}
	}

	if _, err := bw.Write(labels); err != nil {
		return 0, err
	}

	var thrBuf [8]byte
	for _, t := range thresholds {
		binary.LittleEndian.PutUint64(thrBuf[:], uint64(t))
		if _, err := bw.Write(thrBuf[:]); err != nil {
			return 0, err
		}
	}

	if err := bw.Flush(); err != nil {
		return 0, err
	}
	if err := out.Close(); err != nil {
		return 0, err
	}
	return n, os.Rename(tmp, outPath)
}
