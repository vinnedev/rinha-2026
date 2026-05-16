package routes

import (
	"net/http"
	"sync/atomic"
)

var ready atomic.Bool

var (
	aliveJSON = []byte(`{"status":"alive"}`)
	readyJSON = []byte(`{"status":"ready"}`)
	notReady  = []byte(`{"status":"not_ready"}`)
)

func MarkReady()    { ready.Store(true) }
func MarkNotReady() { ready.Store(false) }

func liveness(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, aliveJSON)
}

func readiness(w http.ResponseWriter, _ *http.Request) {
	if !ready.Load() {
		h := w.Header()
		h["Content-Type"] = hdrContentType
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write(notReady)
		return
	}
	writeJSON(w, readyJSON)
}
