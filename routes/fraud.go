package routes

import (
	"sync"
	"time"

	"github.com/valyala/fasthttp"

	"github.com/vinnedev/rinha-2026/config"
	"github.com/vinnedev/rinha-2026/internal/fraud"
)

// Pre-built bodies for the six k=5 oracle scores and the safe fallback.
// One indexed lookup, no float formatting, no marshaling, no allocations.
var (
	respApprovedZero = []byte(`{"approved":true,"fraud_score":0}`)
	respApprovedP2   = []byte(`{"approved":true,"fraud_score":0.2}`)
	respApprovedP4   = []byte(`{"approved":true,"fraud_score":0.4}`)
	respDeniedP6     = []byte(`{"approved":false,"fraud_score":0.6}`)
	respDeniedP8     = []byte(`{"approved":false,"fraud_score":0.8}`)
	respDeniedOne    = []byte(`{"approved":false,"fraud_score":1}`)

	contentTypeApplJSON = []byte("application/json")
)

var intPayloadPool = sync.Pool{
	New: func() any { return new(fraud.IntPayload) },
}

// shedSem caps the number of concurrent ScoreInt evaluations per process.
// With GOMAXPROCS=1 the scheduler can only make progress on one goroutine
// at a time anyway, so an unbounded queue piles latency directly onto the
// p99 tail when a request stalls (GC, page-fault, scheduler tick). The
// semaphore short-circuits the overflow with the safe-approve response.
//
// nil sem disables shedding (SHED_SLOTS=0 or unset).
var shedSem chan struct{}
var shedTimeout time.Duration

func init() {
	if config.SHED_SLOTS > 0 {
		shedSem = make(chan struct{}, config.SHED_SLOTS)
	}
	shedTimeout = config.SHED_TIMEOUT
}

type fraudHandler struct {
	svc *fraud.Service
}

func newFraudHandler(svc *fraud.Service) *fraudHandler {
	return &fraudHandler{svc: svc}
}

// scoreRaw is the fasthttp handler. Hot path: positional zero-alloc parser
// straight into IntPayload, then ScoreInt for integer-only vectorization
// followed by the hybrid RF -> VP-Tree classifier.
func (h *fraudHandler) scoreRaw(ctx *fasthttp.RequestCtx) {
	body := ctx.PostBody()
	if len(body) == 0 {
		writeFixed(ctx, respApprovedZero)
		return
	}

	if shedSem != nil {
		select {
		case shedSem <- struct{}{}:
			defer func() { <-shedSem }()
		default:
			timer := time.NewTimer(shedTimeout)
			select {
			case shedSem <- struct{}{}:
				timer.Stop()
				defer func() { <-shedSem }()
			case <-timer.C:
				writeFixed(ctx, respApprovedZero)
				return
			}
		}
	}

	p := intPayloadPool.Get().(*fraud.IntPayload)

	if !fraud.ParseFast(body, p) {
		intPayloadPool.Put(p)
		writeFixed(ctx, respApprovedZero)
		return
	}

	resp := h.svc.ScoreInt(p)
	intPayloadPool.Put(p)

	writeFixed(ctx, pickResponse(resp.FraudScore))
}

func pickResponse(score float64) []byte {
	switch {
	case score < 0.1:
		return respApprovedZero
	case score < 0.3:
		return respApprovedP2
	case score < 0.5:
		return respApprovedP4
	case score < 0.7:
		return respDeniedP6
	case score < 0.9:
		return respDeniedP8
	default:
		return respDeniedOne
	}
}

// writeFixed emits a pre-computed response body. SetBodyRaw passes the
// slice by reference (zero-copy) — safe because every body in this file
// is an immutable package-level constant. SetContentTypeBytes avoids the
// string→byte allocation that the SetContentType setter performs.
// Status code defaults to 200 OK so we skip SetStatusCode.
func writeFixed(ctx *fasthttp.RequestCtx, body []byte) {
	ctx.Response.Header.SetContentTypeBytes(contentTypeApplJSON)
	ctx.Response.SetBodyRaw(body)
}
