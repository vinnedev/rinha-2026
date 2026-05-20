package main

import (
	"log/slog"
	"net"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/vinnedev/rinha-2026/config"
	"github.com/vinnedev/rinha-2026/internal/dataset"
	"github.com/vinnedev/rinha-2026/internal/fdpass"
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

	if pgoPath := os.Getenv("RINHA_PGO_PROFILE"); pgoPath != "" {
		if f, perr := os.Create(pgoPath); perr == nil {
			if perr := pprof.StartCPUProfile(f); perr == nil {
				log.Info("pgo_profile_started", slog.String("path", pgoPath))
				defer func() {
					pprof.StopCPUProfile()
					_ = f.Close()
				}()
			} else {
				_ = f.Close()
			}
		}
	}

	tr, err := tree.Load(config.TREE_PATH)
	if err != nil {
		log.Error("tree_load_failed", slog.Any("error", err))
		return
	}
	defer tr.Close()
	log.Info("tree_loaded",
		slog.String("path", config.TREE_PATH),
		slog.Int("nodes", tr.NodeCount()),
		slog.Int("trees", tr.TreeCount()),
	)

	var idx *dataset.Index
	if config.HYBRID_ENABLED {
		idx, err = dataset.Load(config.DATASET_PATH)
		if err != nil {
			log.Error("dataset_load_failed", slog.Any("error", err))
			return
		}
		defer idx.Close()
		log.Info("dataset_loaded",
			slog.String("path", config.DATASET_PATH),
			slog.Int("vectors", idx.N),
		)
	}

	svc := fraud.NewService(tr, idx, config.HYBRID_LO, config.HYBRID_HI)
	log.Info("classifier_ready",
		slog.Bool("hybrid", idx != nil),
		slog.Float64("lo", config.HYBRID_LO),
		slog.Float64("hi", config.HYBRID_HI),
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

	// Steady-state GC: the hot path has no heap escapes. Auto-GC firing
	// mid-request adds 50-500µs of STW to the tail. Disable it and run GC
	// on a fixed ticker between requests. GOMEMLIMIT is still enforced.
	if config.STEADY_GC_OFF {
		debug.SetGCPercent(-1)
		go steadyGCLoop(config.STEADY_GC_INTERVAL)
		log.Info("steady_gc_enabled", slog.Duration("interval", config.STEADY_GC_INTERVAL))
	}

	srv := routes.NewRawServer(svc)

	var regularLn net.Listener
	if config.SOCKET_PATH != "" {
		regularLn, err = srv.ServeUnix(config.SOCKET_PATH)
	} else {
		regularLn, err = srv.ServeTCP(net.JoinHostPort(config.HOST, config.PORT))
	}
	if err != nil {
		log.Error("listen_failed", slog.Any("error", err))
		return
	}

	if config.CTRL_SOCKET_PATH != "" {
		if ch, _, ferr := fdpass.Listen(config.CTRL_SOCKET_PATH); ferr == nil {
			go srv.ServeFDChannel(ch)
			log.Info("fdpass_listening", slog.String("path", config.CTRL_SOCKET_PATH))
		} else {
			log.Warn("fdpass_listen_failed", slog.String("path", config.CTRL_SOCKET_PATH), slog.Any("error", ferr))
		}
	}

	routes.RawMarkReady()
	log.Info("server_starting", slog.String("addr", regularLn.Addr().String()))

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Info("signal_received")
	routes.RawMarkNotReady()
	_ = regularLn.Close()
}

func steadyGCLoop(interval time.Duration) {
	if interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for range t.C {
		runtime.GC()
	}
}
