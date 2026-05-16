# Performance

## Target

- p99 ≤ 1.10 ms
- failure rate 0 %
- score ≈ 6,000 (the formula caps `score_p99` at `p99 = 1 ms`)

## Best observed run (Docker on macOS)

```json
{
  "expected": { "total": 54100, "fraud_count": 24058, "legit_count": 30042 },
  "p99": "1.05ms",
  "scoring": {
    "breakdown": {
      "false_positive_detections": 0,
      "false_negative_detections": 0,
      "true_positive_detections": 24037,
      "true_negative_detections": 30022,
      "http_errors": 0
    },
    "failure_rate": "0%",
    "weighted_errors_E": 0,
    "p99_score": { "value": 2978.81, "cut_triggered": false },
    "detection_score": { "value": 3000, "cut_triggered": false },
    "final_score": 5978.81
  }
}
```

## Iteration trail

| Step | p99 | score | What changed |
| --- | --- | --- | --- |
| 1. brute-force k-NN (no model) | ~700 ms | ~3,156 | baseline |
| 2. VP-Tree exact, gin + sonic | ~700 ms | ~3,156 | exact in µs, but HTTP saturated |
| 3. Hybrid (DT 97 % acc, oracle fallback) | ~700 ms | ~3,156 | unchanged — still HTTP bound |
| 4. Distilled RF + tight hybrid band [0.2, 0.8] | ~700 ms | ~3,156 | detection now perfect |
| 5. Replace gin with `net/http.ServeMux` | ~830 ms | ~1,900 | regression: nginx still TCP/HTTP |
| 6. **Unix sockets LB ↔ API** | **1.36 ms** | **5,864** | nginx CPU dropped from 100 % to ~40 % |
| 7. Pre-computed JSON responses | 1.05–1.50 ms | 5,800–5,980 | + Go heap squeezed to GOMEMLIMIT 30 MiB |
| 8. fastjson parser | same range | same range | ~3× cheaper unmarshal, fewer allocs |
| 9. AVX2 VPMADDWD distance | same range | same range | 2× faster inner loop, helps p99 tail on uncertain queries |
| 10. **Go LB with TCP-splice** | **1.04–1.14 ms** | **5,944–5,981** | dropped nginx entirely; eight back-to-back runs all 0 FP / 0 FN / 0 errs |

After step 10 every run on this Mac sits in a tight 100 µs window. On
Linux native (the rinha test environment) variance is lower still.

## Per-component cost (warm, ~µs)

| Component | Time | Notes |
| --- | --- | --- |
| RF `Predict` | ~250 ns | walk 30 trees · int16 features |
| VP-Tree `KNNFraudCount` | ~2 µs warm / ~50 µs cold | hit by 4.65 % of queries, AVX2 distance |
| `Vectorize` | ~5 µs | 14 dims, branchy |
| `fastjson` parse (~300 B) | ~1.6 µs | path-based, 32 B / 4 allocs |
| net/http parse + write | ~30–80 µs | per request |
| Go LB → unix socket → API | ~10–25 µs | splice-only, no HTTP parse |
| total typical | ~50–150 µs | wall-clock on the API box |

In a 0.42 CPU container `tail` (GC, scheduler, packet bursts) adds ~0.8 ms to
p99 — hence the ~1 ms p99 in practice.

## Tuning matrix

| Knob | Tried | Best | Why |
| --- | --- | --- | --- |
| `GOMAXPROCS` | 1, 2, 4, 8 | **4** | 4 OS threads parallelize stream of goroutines while still fitting the 0.42 CPU quota |
| `GOGC` | 50, 100, 200, off | **200** | less frequent GC under low allocation pressure |
| `GOMEMLIMIT` | 30 – 120 MiB | **30 MiB** | tiny Go heap keeps mmap'd files resident |
| LB CPU | 0.10, 0.16, 0.20 | **0.16** | nginx is fully loaded at 0.10; 0.20 wastes budget |
| Hybrid band | [0.2, 0.8] … [0.4, 0.6] | **[0.2, 0.8]** | wider keeps detection at 100 %; same fallback rate |
| nginx `keepalive` | 256, 512, 1024 | **512** | LB ↔ unix socket multiplexing |

## Done in this iteration

- ✓ **fastjson schema parser** — replaces sonic.Unmarshal on the request
  path. ~3× cheaper, 32 B / 4 allocs per request.
- ✓ **AVX2 VPMADDWD** — replaces the unrolled scalar 14-dim loop with a
  single SIMD instruction inside the recursive VP-Tree walk. The Go
  fallback is the original tight loop and is selected at startup via
  `golang.org/x/sys/cpu.X86.HasAVX2`.
- ✓ **Go LB with `splice(2)` proxy** — replaces nginx. Zero HTTP
  parsing in the load balancer; the kernel moves bytes between the
  client TCP socket and the upstream Unix socket without entering
  userspace.

## What is left on the table

- **SCM_RIGHTS FD-passing LB** — even cheaper than splice, since the LB
  hands the client TCP file descriptor straight to a worker via
  `SCM_RIGHTS`. The leader's Rust implementation uses this. It would
  skip the two splice goroutines per connection on our side. Worth
  another ~50 µs on the LB side.
- **AVX-512** — the Mac Mini 2014 doesn't have it, so this only
  matters on more modern hardware.

## Reproducing

```bash
# bring stack up (uses pre-built resources/*.bin)
docker compose up -d --build

# warm the page cache
for i in $(seq 1 5000); do curl -s -o /dev/null http://localhost:9999/ready; done

# run k6
./run.sh
cat test/results.json | jq
```
