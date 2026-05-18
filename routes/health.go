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
	ctx.Response.Header.SetContentTypeBytes(contentTypeApplJSON)
	ctx.Response.SetBodyRaw(aliveJSON)
}

func readiness(ctx *fasthttp.RequestCtx) {
	ctx.Response.Header.SetContentTypeBytes(contentTypeApplJSON)
	if !ready.Load() {
		ctx.SetStatusCode(fasthttp.StatusServiceUnavailable)
		ctx.Response.SetBodyRaw(notReady)
		return
	}
	ctx.Response.SetBodyRaw(readyJSON)
}
