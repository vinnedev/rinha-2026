package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	defaultAddr      = ":9999"
	defaultUpstreams = "/sockets/api1.ctrl,/sockets/api2.ctrl"
	dialTimeout      = 100 * time.Millisecond
)

type upstream struct {
	path string
	mu   sync.Mutex
	conn *net.UnixConn
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	addr := env("LB_ADDR", defaultAddr)
	upstreams := newUpstreams(env("LB_UPSTREAMS", defaultUpstreams))
	if len(upstreams) == 0 {
		log.Error("no_upstreams")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		log.Error("listen_failed", slog.String("addr", addr), slog.Any("error", err))
		os.Exit(1)
	}
	defer ln.Close()

	log.Warn("lb_ready",
		slog.String("addr", addr),
		slog.Int("upstreams", len(upstreams)),
		slog.String("go_version", runtime.Version()),
	)

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	var next atomic.Uint64
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		i := next.Add(1)
		go passConn(conn, upstreams, int(i))
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func newUpstreams(raw string) []*upstream {
	parts := strings.Split(raw, ",")
	out := make([]*upstream, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, &upstream{path: p})
		}
	}
	return out
}

func passConn(conn net.Conn, upstreams []*upstream, start int) {
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		_ = conn.Close()
		return
	}
	_ = tcp.SetNoDelay(true)
	_ = tcp.SetKeepAlive(true)
	_ = tcp.SetKeepAlivePeriod(3 * time.Minute)

	file, err := tcp.File()
	if err != nil {
		_ = conn.Close()
		return
	}
	defer file.Close()
	defer conn.Close()

	fd := int(file.Fd())
	for i := range upstreams {
		u := upstreams[(start+i)%len(upstreams)]
		if u.send(fd) == nil {
			return
		}
	}
}

func (u *upstream) send(fd int) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.conn == nil {
		conn, err := net.DialTimeout("unix", u.path, dialTimeout)
		if err != nil {
			return err
		}
		u.conn = conn.(*net.UnixConn)
	}

	err := sendFD(u.conn, fd)
	if err == nil {
		return nil
	}

	_ = u.conn.Close()
	u.conn = nil
	return err
}

func sendFD(conn *net.UnixConn, fd int) error {
	rights := unix.UnixRights(fd)
	_, _, err := conn.WriteMsgUnix([]byte{1}, rights, nil)
	return err
}
