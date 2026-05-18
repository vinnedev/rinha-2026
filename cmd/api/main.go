package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"sync"
	"syscall"
	"time"

	"github.com/vinnedev/rinha-2026/config"
	"github.com/vinnedev/rinha-2026/internal/dataset"
	"github.com/vinnedev/rinha-2026/internal/fraud"
	"github.com/vinnedev/rinha-2026/internal/tree"
	"github.com/vinnedev/rinha-2026/pkg/logger"
	"github.com/vinnedev/rinha-2026/routes"
)

func main() {
	log := logger.L()
	log.Info("boot",
		slog.String("go_version", runtime.Version()),
		slog.Int("gomaxprocs", runtime.GOMAXPROCS(0)),
		slog.Int("num_cpu", runtime.NumCPU()),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// PGO profile collection — only fires when RINHA_PGO_PROFILE points at a
	// writable file. Used once, off-line, to capture a CPU profile that the
	// next build feeds back via `-pgo=auto` (Go 1.21+ auto-detects
	// cmd/api/default.pgo). In production the env var is unset and this
	// path is dead code that the inliner drops.
	if pgoPath := os.Getenv("RINHA_PGO_PROFILE"); pgoPath != "" {
		f, perr := os.Create(pgoPath)
		if perr != nil {
			log.Error("pgo_profile_create_failed", slog.String("path", pgoPath), slog.Any("error", perr))
		} else {
			if perr := pprof.StartCPUProfile(f); perr != nil {
				log.Error("pgo_profile_start_failed", slog.Any("error", perr))
				_ = f.Close()
			} else {
				log.Info("pgo_profile_started", slog.String("path", pgoPath))
				defer func() {
					pprof.StopCPUProfile()
					_ = f.Close()
					log.Info("pgo_profile_saved", slog.String("path", pgoPath))
				}()
			}
		}
	}

	tr, idx, err := loadResources(log)
	if err != nil {
		log.Error("resource_load_failed", slog.Any("error", err))
		return
	}
	defer func() {
		if tr != nil {
			tr.Close()
		}
		if idx != nil {
			idx.Close()
		}
	}()

	hybridIdx := idx
	if !config.HYBRID_ENABLED {
		hybridIdx = nil
	}
	svc := fraud.NewService(tr, hybridIdx, config.HYBRID_LO, config.HYBRID_HI)
	log.Info("classifier_ready",
		slog.Bool("hybrid", hybridIdx != nil),
		slog.Float64("lo", config.HYBRID_LO),
		slog.Float64("hi", config.HYBRID_HI),
		slog.Int("tree_nodes", tr.NodeCount()),
		slog.Int("tree_count", tr.TreeCount()),
	)

	if config.WARMUP_ITERS > 0 {
		t0 := time.Now()
		svc.Warmup(config.WARMUP_ITERS)
		runtime.GC()
		log.Info("warmup_done",
			slog.Int("iters", config.WARMUP_ITERS),
			slog.Duration("elapsed", time.Since(t0)),
		)
	}

	// Steady-state GC mode: the hot path has no heap escapes (verified via
	// `go build -gcflags=-m`); ScoreInt's IntPayload and the q [Dim]int16
	// stack-allocate, and response bodies are package-level constants.
	// With auto-GC active, a GC firing mid-request adds 50-500us of STW
	// pause that lands directly in the p99 tail. Switching to a fixed
	// 5s ticker keeps GC off the request path while still bounding heap
	// growth (GOMEMLIMIT remains enforced; if anything leaks Go GCs anyway).
	if config.STEADY_GC_OFF {
		debug.SetGCPercent(-1)
		go steadyGCLoop(ctx, log, config.STEADY_GC_INTERVAL)
		log.Info("steady_gc_enabled", slog.Duration("interval", config.STEADY_GC_INTERVAL))
	}

	if config.USE_RAWHTTP {
		rawSrv := routes.NewRawServer(svc)
		var ln net.Listener
		var err error
		if config.SOCKET_PATH != "" {
			ln, err = rawSrv.ServeUnix(config.SOCKET_PATH)
		} else {
			ln, err = rawSrv.ServeTCP(net.JoinHostPort(config.HOST, config.PORT))
		}
		if err != nil {
			log.Error("rawhttp_listen_failed", slog.Any("error", err))
			return
		}
		routes.RawMarkReady()
		log.Info("rawhttp_server_starting", slog.String("addr", ln.Addr().String()))
		<-ctx.Done()
		log.Info("signal_received")
		routes.RawMarkNotReady()
		_ = ln.Close()
		log.Info("shutdown_complete")
		return
	}

	srv := routes.NewServer(routes.New(svc))
	addr := routes.ListenAddr()

	listener, err := routes.NewListener(ctx, net.JoinHostPort(config.HOST, config.PORT))
	if err != nil {
		log.Error("listener_failed", slog.String("addr", addr), slog.Any("error", err))
		return
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Info("server_starting", slog.String("addr", addr))
		routes.MarkReady()
		if err := srv.Serve(listener); err != nil {
			serverErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Info("signal_received")
	case err := <-serverErr:
		if !errors.Is(err, net.ErrClosed) {
			log.Error("server_failed", slog.Any("error", err))
		}
	}

	routes.MarkNotReady()
	if err := routes.Shutdown(srv, config.SHUTDOWN_TIMEOUT); err != nil {
		log.Error("shutdown_failed", slog.Any("error", err))
		return
	}
	log.Info("shutdown_complete")
}

// steadyGCLoop runs runtime.GC() on a fixed ticker until ctx is cancelled.
// Pairs with debug.SetGCPercent(-1) so the auto-GC scheduler doesn't fire
// mid-request.
func steadyGCLoop(ctx context.Context, log *slog.Logger, interval time.Duration) {
	if interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			start := time.Now()
			runtime.GC()
			if d := time.Since(start); d > 5*time.Millisecond {
				log.Warn("steady_gc_slow", slog.Duration("elapsed", d))
			}
		}
	}
}

// loadResources loads the tree and (if hybrid is enabled) the VP-Tree dataset
// concurrently — the dataset is the heavy one (~106MB mmap + page-cache warm),
// so doing it in parallel with tree load + page-touch overlaps both.
func loadResources(log *slog.Logger) (*tree.Tree, *dataset.Index, error) {
	var (
		wg     sync.WaitGroup
		tr     *tree.Tree
		idx    *dataset.Index
		treeEr error
		idxEr  error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		start := time.Now()
		tr, treeEr = tree.Load(config.TREE_PATH)
		if treeEr == nil {
			log.Info("tree_loaded",
				slog.String("path", config.TREE_PATH),
				slog.Int("nodes", tr.NodeCount()),
				slog.Int("trees", tr.TreeCount()),
				slog.Duration("elapsed", time.Since(start)),
			)
		}
	}()
	if config.HYBRID_ENABLED {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			idx, idxEr = dataset.Load(config.DATASET_PATH)
			if idxEr == nil {
				log.Info("dataset_loaded",
					slog.String("path", config.DATASET_PATH),
					slog.Int("vectors", idx.N),
					slog.Duration("elapsed", time.Since(start)),
				)
			}
		}()
	}
	wg.Wait()
	if treeEr != nil {
		return nil, nil, treeEr
	}
	if idxEr != nil {
		return tr, nil, idxEr
	}
	return tr, idx, nil
}
