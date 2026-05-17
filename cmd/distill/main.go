// Build the distilled-labels file: for each reference vector R, the label
// is the k-NN(k=5) majority over its 5 nearest references, excluding R
// itself. Output is consumed by model/train.py --distilled.
//
// Format: a raw uint8 array of length N — labels[i] in {0=legit, 1=fraud}.
// Run:    go run ./cmd/distill
package main

import (
	"log"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vinnedev/rinha-2026/internal/dataset"
	"github.com/vinnedev/rinha-2026/internal/domain"
	"github.com/vinnedev/rinha-2026/internal/search"
)

func main() {
	in := "resources/vectors.bin"
	out := "resources/labels_distilled.bin"
	if len(os.Args) > 1 {
		in = os.Args[1]
	}
	if len(os.Args) > 2 {
		out = os.Args[2]
	}

	t0 := time.Now()
	idx, err := dataset.Load(in)
	if err != nil {
		log.Fatalf("load: %v", err)
	}
	defer idx.Close()
	log.Printf("loaded %d vectors in %s", idx.N, time.Since(t0))

	labels := make([]byte, idx.N)
	majorityThr := search.K/2 + 1

	workers := runtime.NumCPU()
	chunk := (idx.N + workers - 1) / workers
	log.Printf("running %d workers, chunk=%d", workers, chunk)

	var (
		wg       sync.WaitGroup
		done     atomic.Int64
		fraudCnt atomic.Int64
	)
	tStart := time.Now()
	for w := 0; w < workers; w++ {
		startIdx := w * chunk
		endIdx := startIdx + chunk
		if endIdx > idx.N {
			endIdx = idx.N
		}
		if startIdx >= endIdx {
			continue
		}
		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			for i := s; i < e; i++ {
				var q [domain.Dim]int16
				base := i * domain.Dim
				for j := 0; j < domain.Dim; j++ {
					q[j] = idx.Vectors[base+j]
				}
				fc := search.KNNDistill(idx, q)
				labels[i] = byte(fc)
				if fc >= majorityThr {
					fraudCnt.Add(1)
				}
				if d := done.Add(1); d%200000 == 0 {
					rate := float64(d) / time.Since(tStart).Seconds()
					log.Printf("  progress %d/%d  (%.0f/s)", d, idx.N, rate)
				}
			}
		}(startIdx, endIdx)
	}
	wg.Wait()
	log.Printf("distilled in %s  fraud_rate=%.4f",
		time.Since(tStart), float64(fraudCnt.Load())/float64(idx.N))

	if err := os.WriteFile(out, labels, 0o644); err != nil {
		log.Fatalf("write: %v", err)
	}
	log.Printf("wrote %s (%d bytes)", out, len(labels))
}
