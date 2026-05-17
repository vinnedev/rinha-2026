package routes

import (
	"sync/atomic"

	"github.com/valyala/fasthttp"
)

var ready atomic.Bool

var (
	aliveJSON = []byte(`{"status":"alive"}`)
	readyJSON = []byte(`{"status":"ready"}`)
	notReady  = []byte(`{"status":"not_ready"}`)
)

func MarkReady()    { ready.Store(true) }
func MarkNotReady() { ready.Store(false) }

func liveness(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType(contentTypeApplJSON)
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(aliveJSON)
}

func readiness(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType(contentTypeApplJSON)
	if !ready.Load() {
		ctx.SetStatusCode(fasthttp.StatusServiceUnavailable)
		ctx.SetBody(notReady)
		return
	}
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(readyJSON)
}
