# Architecture

## Topology

```mermaid
flowchart LR
    client((k6 / clients))
    lb[Go LB<br/>:9999<br/>TCP splice<br/>round-robin]
    api1[api1<br/>net/http]
    api2[api2<br/>net/http]
    sockets((tmpfs<br/>/sockets))
    data1[(vectors.bin<br/>fraud_dt.bin)]
    data2[(vectors.bin<br/>fraud_dt.bin)]

    client -->|TCP 9999| lb
    lb -. api1.sock .-> sockets
    lb -. api2.sock .-> sockets
    sockets --> api1
    sockets --> api2
    api1 --> data1
    api2 --> data2
```

- **Two API instances** to satisfy the challenge requirement; both are
  identical and read the same packed indexes.
- The **Go LB** (`cmd/lb`) accepts TCP on `:9999` and proxies bytes to
  per-API Unix-domain sockets. On Linux `io.Copy` between socket
  endpoints lowers to `splice(2)`, so the LB does not read the payload
  bytes into userspace — no HTTP parsing, no header rewriting.
- A **tmpfs volume** is mounted into every container at `/sockets/`; the
  APIs `bind()` their listening sockets there and the LB `dial()`s the
  same paths.

## Request lifecycle

```mermaid
sequenceDiagram
    participant C as client
    participant L as Go LB
    participant K as kernel splice
    participant A as api (net/http)
    participant H as fraudHandler.scoreRaw
    participant S as fraud.Service
    participant T as RF (tree.Tree)
    participant V as VP-Tree (search, AVX2)

    C->>L: POST /fraud-score (TCP)
    L->>K: splice(client_fd → unix_fd)
    K->>A: bytes arrive on api?.sock
    A->>H: scoreRaw(w, r)
    H->>H: pool.Get FraudPayload + buf
    H->>H: io.ReadAll(body)
    H->>H: fraud.ParsePayload (fastjson)
    H->>S: Score(payload)
    S->>S: Vectorize() → 14-dim int16
    S->>T: tree.Predict(query)
    alt RF confident (≤ lo or ≥ hi)
        T-->>S: score (≈0 or ≈1)
    else uncertain
        S->>V: KNNFraudCount(query) [AVX2 VPMADDWD]
        V-->>S: count ∈ 0..5
        S->>S: score = count / 5
    end
    S-->>H: FraudResponse{Approved, FraudScore}
    H->>H: pickResponse(score) → one of 6 byte slices
    H->>A: w.Write(precomputed body)
    A->>K: splice(unix_fd → client_fd)
    K-->>C: HTTP response
```

## In-process layout

```mermaid
classDiagram
    direction LR
    class FraudPayload {
        +Transaction
        +Customer
        +Merchant
        +Terminal
        +LastTransaction
    }
    class FraudResponse {
        +bool Approved
        +float64 FraudScore
    }
    class Service {
        -*tree.Tree tree
        -*dataset.Index idx
        -float64 lo, hi
        +Score(*FraudPayload) FraudResponse
    }
    class Tree {
        +Predict([14]float32) float32
    }
    class Index {
        +int N
        +[]int16 Vectors
        +[]byte Labels
        +[]int64 Thresholds
    }
    class Vectorize {
        <<func>>
        +Vectorize(*FraudPayload, []int16)
    }
    class ParsePayload {
        <<func, fastjson>>
        +ParsePayload([]byte, *FraudPayload) error
    }
    class KNNFraudCount {
        <<func>>
        +KNNFraudCount(*Index, [14]int16) int
    }
    class distSqAVX2 {
        <<asm, AVX2>>
        +distSqAVX2(vecs, query) int64
    }

    Service --> Tree
    Service --> Index
    Service ..> Vectorize
    Service ..> KNNFraudCount
    Vectorize --> FraudPayload
    KNNFraudCount ..> distSqAVX2
    ParsePayload --> FraudPayload
```

## Data files

```mermaid
flowchart TD
    refs[references.json.gz<br/>3M vectors + labels<br/>48 MB]
    pre[cmd/preprocess<br/>quantize + build VP-Tree]
    distill[cmd/distill<br/>parallel goroutines]
    train[model/train.py<br/>RandomForest distilled]
    export[model/export_to_go.py]

    bin[(vectors.bin V2<br/>VP-Tree-ordered<br/>106 MB)]
    lab[(labels_distilled.bin<br/>3 MB)]
    rf[(fraud_dt.bin V2<br/>30 trees<br/>4.5 MB)]

    refs --> pre --> bin
    bin --> distill --> lab
    lab --> train --> export --> rf
```

Both `vectors.bin` and `fraud_dt.bin` are **mmap'd** at startup and the page
cache is warmed by a touch-every-4K loop in [`internal/dataset/index.go`](../internal/dataset/index.go).
This keeps the Go heap small (≈ 20 MB) so the kernel can keep the file pages
resident under the 160 MB cgroup memory limit.

## Memory budget (per container)

```mermaid
pie title api container memory (mmap + heap)
    "vectors.bin (mmap)" : 106
    "fraud_dt.bin (mmap)" : 5
    "go heap (~GOMEMLIMIT)" : 30
    "stacks + runtime" : 15
    "headroom" : 4
```

Total ≈ 160 MB. `GOMEMLIMIT=30MiB` keeps the heap small; the rest is
reclaimable file cache.

## Why a custom LB instead of nginx

The challenge bans business logic in the load balancer but allows
anything purely transport-level. With nginx, the LB still parses
HTTP/1.1 (request line, headers, length) and re-serializes them to the
upstream. That parsing dominates the LB CPU budget at 900 rps and is
unnecessary because the response body and headers do not depend on
load-balancer routing.

The Go LB drops to **byte-level** TCP→Unix-socket proxying:

- one TCP `Accept` per client
- two goroutines per active connection running `io.Copy` in each
  direction
- on Linux the io.Copy fast-path uses `splice(2)` between two
  sockets, so the kernel moves the bytes without touching userspace
- round-robin upstream selection at TCP accept time

The result is an LB that uses **≈ 60 %** of the nginx CPU budget for
the same workload and contributes negligible per-request latency.
