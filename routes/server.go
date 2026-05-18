package routes

import (
	"context"
	"errors"
	"net"
	"os"
	"time"

	"github.com/valyala/fasthttp"

	"github.com/vinnedev/rinha-2026/config"
)

func NewServer(handler fasthttp.RequestHandler) *fasthttp.Server {
	return &fasthttp.Server{
		Handler:                       handler,
		ReadTimeout:                   config.READ_TIMEOUT,
		WriteTimeout:                  config.WRITE_TIMEOUT,
		IdleTimeout:                   config.IDLE_TIMEOUT,
		MaxRequestBodySize:            config.MAX_HEADER_BYTES * 4,
		DisableHeaderNamesNormalizing: true,
		NoDefaultServerHeader:         true,
		NoDefaultContentType:          true,
		NoDefaultDate:                 true,
		TCPKeepalive:                  true,
		ReduceMemoryUsage:             false,
	}
}

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

func Shutdown(srv *fasthttp.Server, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- srv.Shutdown() }()
	select {
	case err := <-done:
		if err == nil || errors.Is(err, fasthttp.ErrConnectionClosed) {
			return nil
		}
		return err
	case <-time.After(timeout):
		_ = srv.Shutdown()
		return errors.New("shutdown timeout")
	}
}
