package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os/signal"
	"runtime"
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
