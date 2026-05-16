package routes

import (
	"io"
	"net/http"
	"strconv"
	"sync"

	"github.com/vinnedev/rinha-2026/internal/domain"
	"github.com/vinnedev/rinha-2026/internal/fraud"
)

// Pre-built bodies for the six k=5 oracle scores and the safe fallback.
// One indexed lookup, no float formatting, no marshaling, no allocations.
var (
	respApprovedZero  = []byte(`{"approved":true,"fraud_score":0}`)
	respApprovedP2    = []byte(`{"approved":true,"fraud_score":0.2}`)
	respApprovedP4    = []byte(`{"approved":true,"fraud_score":0.4}`)
	respDeniedP6      = []byte(`{"approved":false,"fraud_score":0.6}`)
	respDeniedP8     = []byte(`{"approved":false,"fraud_score":0.8}`)
	respDeniedOne    = []byte(`{"approved":false,"fraud_score":1}`)
	safeResponseBytes = respApprovedZero

	lenApprovedZero = strconv.Itoa(len(respApprovedZero))
	lenApprovedP2   = strconv.Itoa(len(respApprovedP2))
	lenApprovedP4   = strconv.Itoa(len(respApprovedP4))
	lenDeniedP6     = strconv.Itoa(len(respDeniedP6))
	lenDeniedP8    = strconv.Itoa(len(respDeniedP8))
	lenDeniedOne   = strconv.Itoa(len(respDeniedOne))

	hdrContentType = []string{"application/json"}
)

const maxPayloadBytes = 8 << 10

var payloadPool = sync.Pool{
	New: func() any { return new(domain.FraudPayload) },
}

var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 4096)
		return &b
	},
}

type fraudHandler struct {
	svc *fraud.Service
}

func newFraudHandler(svc *fraud.Service) *fraudHandler {
	return &fraudHandler{svc: svc}
}

func (h *fraudHandler) scoreRaw(w http.ResponseWriter, r *http.Request) {
	bufPtr := bufPool.Get().(*[]byte)
	body := (*bufPtr)[:0]

	body, err := readAll(body, io.LimitReader(r.Body, maxPayloadBytes))
	if err != nil || len(body) == 0 {
		*bufPtr = body[:0]
		bufPool.Put(bufPtr)
		writeFixed(w, respApprovedZero, lenApprovedZero)
		return
	}

	p := payloadPool.Get().(*domain.FraudPayload)
	*p = domain.FraudPayload{}

	if err := fraud.ParsePayload(body, p); err != nil {
		payloadPool.Put(p)
		*bufPtr = body[:0]
		bufPool.Put(bufPtr)
		writeFixed(w, respApprovedZero, lenApprovedZero)
		return
	}

	resp := h.svc.Score(p)
	payloadPool.Put(p)
	*bufPtr = body[:0]
	bufPool.Put(bufPtr)

	out, length := pickResponse(resp.FraudScore)
	writeFixed(w, out, length)
}

func pickResponse(score float64) ([]byte, string) {
	switch {
	case score < 0.1:
		return respApprovedZero, lenApprovedZero
	case score < 0.3:
		return respApprovedP2, lenApprovedP2
	case score < 0.5:
		return respApprovedP4, lenApprovedP4
	case score < 0.7:
		return respDeniedP6, lenDeniedP6
	case score < 0.9:
		return respDeniedP8, lenDeniedP8
	default:
		return respDeniedOne, lenDeniedOne
	}
}

func writeFixed(w http.ResponseWriter, body []byte, length string) {
	h := w.Header()
	h["Content-Type"] = hdrContentType
	h["Content-Length"] = []string{length}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func writeJSON(w http.ResponseWriter, b []byte) {
	h := w.Header()
	h["Content-Type"] = hdrContentType
	h["Content-Length"] = []string{strconv.Itoa(len(b))}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

func readAll(dst []byte, r io.Reader) ([]byte, error) {
	var tmp [1024]byte
	for {
		n, err := r.Read(tmp[:])
		if n > 0 {
			dst = append(dst, tmp[:n]...)
		}
		if err == io.EOF {
			return dst, nil
		}
		if err != nil {
			return dst, err
		}
	}
}
