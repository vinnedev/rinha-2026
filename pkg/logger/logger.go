package logger

import (
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/vinnedev/rinha-2026/config"
)

var (
	once   sync.Once
	logger *slog.Logger
)

var levels = map[string]slog.Level{
	"debug": slog.LevelDebug,
	"info":  slog.LevelInfo,
	"warn":  slog.LevelWarn,
	"error": slog.LevelError,
}

func L() *slog.Logger {
	once.Do(func() {
		lvl, ok := levels[strings.ToLower(config.LOG_LEVEL)]
		if !ok {
			lvl = slog.LevelInfo
		}
		opts := &slog.HandlerOptions{Level: lvl}
		var h slog.Handler
		if strings.ToLower(config.LOG_FORMAT) == "text" {
			h = slog.NewTextHandler(os.Stdout, opts)
		} else {
			h = slog.NewJSONHandler(os.Stdout, opts)
		}
		logger = slog.New(h).With(
			slog.String("service", config.SERVICE),
			slog.String("env", config.ENV_MODE),
		)
		slog.SetDefault(logger)
	})
	return logger
}
