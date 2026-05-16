package routes

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/vinnedev/rinha-2026/config"
	"github.com/vinnedev/rinha-2026/pkg/logger"
)

func NewServer(ctx context.Context, handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadTimeout:       config.READ_TIMEOUT,
		ReadHeaderTimeout: config.READ_HEADER_TIMEOUT,
		WriteTimeout:      config.WRITE_TIMEOUT,
		IdleTimeout:       config.IDLE_TIMEOUT,
		MaxHeaderBytes:    config.MAX_HEADER_BYTES,
		BaseContext:       func(_ net.Listener) context.Context { return ctx },
		ErrorLog:          slog.NewLogLogger(logger.L().Handler(), slog.LevelError),
	}
}

// NewListener picks a Unix-domain socket when SOCKET_PATH is set, else TCP.
// Unix sockets shave significant per-request CPU compared to TCP/loopback
// inside containers and let the LB/API pair share kernel buffers directly.
func NewListener(ctx context.Context, fallbackAddr string) (net.Listener, error) {
	if path := config.SOCKET_PATH; path != "" {
		_ = os.Remove(path)
		lc := &net.ListenConfig{}
		ln, err := lc.Listen(ctx, "unix", path)
		if err != nil {
			return nil, err
		}
		_ = os.Chmod(path, 0o666)
		return ln, nil
	}
	lc := &net.ListenConfig{KeepAlive: config.TCP_KEEPALIVE}
	return lc.Listen(ctx, "tcp", fallbackAddr)
}

func ListenAddr() string {
	if path := config.SOCKET_PATH; path != "" {
		return "unix://" + path
	}
	return net.JoinHostPort(config.HOST, config.PORT)
}

func Shutdown(srv *http.Server, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := srv.Shutdown(ctx)
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return srv.Close()
	}
	return err
}
