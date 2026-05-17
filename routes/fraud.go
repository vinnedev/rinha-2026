package routes

import (
	"sync"

	"github.com/valyala/fasthttp"

	"github.com/vinnedev/rinha-2026/internal/domain"
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

	contentTypeApplJSON = "application/json"
)

var payloadPool = sync.Pool{
	New: func() any { return new(domain.FraudPayload) },
}

type fraudHandler struct {
	svc *fraud.Service
}

func newFraudHandler(svc *fraud.Service) *fraudHandler {
	return &fraudHandler{svc: svc}
}

// scoreRaw is the fasthttp handler. fasthttp already owns the request body
// buffer (ctx.PostBody), parses headers once, and reuses ctx between calls,
// so we don't pool anything except the FraudPayload struct.
func (h *fraudHandler) scoreRaw(ctx *fasthttp.RequestCtx) {
	body := ctx.PostBody()
	if len(body) == 0 {
		writeFixed(ctx, respApprovedZero)
		return
	}

	p := payloadPool.Get().(*domain.FraudPayload)
	*p = domain.FraudPayload{}

	if err := fraud.ParsePayload(body, p); err != nil {
		payloadPool.Put(p)
		writeFixed(ctx, respApprovedZero)
		return
	}

	resp := h.svc.Score(p)
	payloadPool.Put(p)

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

func writeFixed(ctx *fasthttp.RequestCtx, body []byte) {
	ctx.SetContentType(contentTypeApplJSON)
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(body)
}
