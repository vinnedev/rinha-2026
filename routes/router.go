package routes

import (
	"github.com/valyala/fasthttp"

	"github.com/vinnedev/rinha-2026/internal/fraud"
)

func New(svc *fraud.Service) fasthttp.RequestHandler {
	var fraudH func(ctx *fasthttp.RequestCtx)
	if svc != nil {
		h := newFraudHandler(svc)
		fraudH = h.scoreRaw
	}

	fraudPath := []byte("/fraud-score")
	readyPath := []byte("/ready")
	readyzPath := []byte("/readyz")
	healthzPath := []byte("/healthz")

	return func(ctx *fasthttp.RequestCtx) {
		path := ctx.Path()
		if ctx.IsPost() && eqPath(path, fraudPath) {
			if fraudH != nil {
				fraudH(ctx)
				return
			}
			ctx.SetStatusCode(fasthttp.StatusServiceUnavailable)
			return
		}
		if ctx.IsGet() {
			switch {
			case eqPath(path, readyPath), eqPath(path, readyzPath):
				readiness(ctx)
				return
			case eqPath(path, healthzPath):
				liveness(ctx)
				return
			}
		}
		ctx.SetStatusCode(fasthttp.StatusNotFound)
	}
}

func eqPath(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
