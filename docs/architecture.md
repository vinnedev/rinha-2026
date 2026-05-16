# Architecture

## Topology

```mermaid
flowchart LR
    client((k6 / clients))
    nginx[nginx LB<br/>:9999<br/>round-robin]
    api1[api1<br/>net/http]
    api2[api2<br/>net/http]
    sockets((tmpfs<br/>/sockets))
    data1[(vectors.bin<br/>fraud_dt.bin)]
    data2[(vectors.bin<br/>fraud_dt.bin)]

    client -->|TCP 9999| nginx
    nginx -. api1.sock .-> sockets
    nginx -. api2.sock .-> sockets
    sockets --> api1
    sockets --> api2
    api1 --> data1
    api2 --> data2
```

- **Two API instances** to satisfy the challenge requirement; both are
  identical and read the same packed indexes.
- **nginx** is the load balancer on `:9999` with round-robin upstream and
  Unix-socket transport to each API (no TCP between LB and APIs).
- A **tmpfs volume** is mounted into every container at `/sockets/`; that is
  where the APIs `bind()` their listening sockets and nginx connects.

## Request lifecycle

```mermaid
sequenceDiagram
    participant C as client
    participant N as nginx LB
    participant A as api (net/http)
    participant H as fraudHandler.scoreRaw
    participant S as fraud.Service
    participant T as RF (tree.Tree)
    participant V as VP-Tree (search)

    C->>N: POST /fraud-score
    N->>A: forward via unix socket
    A->>H: scoreRaw(w, r)
    H->>H: pool.Get FraudPayload + buf
    H->>H: io.ReadAll(body)
    H->>H: sonic.Unmarshal
    H->>S: Score(payload)
    S->>S: Vectorize() → 14-dim int16
    S->>T: tree.Predict(query)
    alt RF confident (≤ lo or ≥ hi)
        T-->>S: score (≈0 or ≈1)
    else uncertain
        S->>V: KNNFraudCount(query)
        V-->>S: count ∈ 0..5
        S->>S: score = count / 5
    end
    S-->>H: FraudResponse{Approved, FraudScore}
    H->>H: pickResponse(score) → one of 6 byte slices
    H->>A: w.Write(precomputed body)
    A-->>N: HTTP response
    N-->>C: HTTP response
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
    class KNNFraudCount {
        <<func>>
        +KNNFraudCount(*Index, [14]int16) int
    }

    Service --> Tree
    Service --> Index
    Service ..> Vectorize
    Service ..> KNNFraudCount
    Vectorize --> FraudPayload
    Vectorize --> "out []int16" FraudPayload
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
