package routes

import (
	"net/http"

	"github.com/vinnedev/rinha-2026/internal/fraud"
)

func New(svc *fraud.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", liveness)
	mux.HandleFunc("GET /ready", readiness)
	mux.HandleFunc("GET /readyz", readiness)
	if svc != nil {
		h := newFraudHandler(svc)
		mux.HandleFunc("POST /fraud-score", h.scoreRaw)
	}
	return mux
}
