// cmd/lb is a minimal load balancer: it accepts TCP on PORT and pipes
// raw bytes to a round-robin upstream Unix socket. On Linux io.Copy uses
// splice(2) for sock-to-sock transfer, so the LB never touches the
// payload bytes in userland. There is no HTTP parsing.
package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

func main() {
	port := getenv("PORT", "9999")
	upstreamCSV := getenv("UPSTREAMS", "/sockets/api1.sock,/sockets/api2.sock")
	upstreams := splitNonEmpty(upstreamCSV)
	if len(upstreams) == 0 {
		log.Fatal("lb: no upstreams configured (env UPSTREAMS=path1,path2)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	lc := net.ListenConfig{KeepAlive: 3 * time.Minute}
	ln, err := lc.Listen(ctx, "tcp", ":"+port)
	if err != nil {
		log.Fatalf("lb: listen :%s: %v", port, err)
	}
	log.Printf("lb: listening on :%s, upstreams=%v", port, upstreams)

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	var idx atomic.Uint64
	for {
		client, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return
			}
			log.Printf("lb: accept: %v", err)
			continue
		}
		up := upstreams[idx.Add(1)%uint64(len(upstreams))]
		go proxy(client, up)
	}
}

func proxy(client net.Conn, upstream string) {
	defer client.Close()
	if tcp, ok := client.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
		_ = tcp.SetKeepAlive(true)
	}
	server, err := net.Dial("unix", upstream)
	if err != nil {
		return
	}
	defer server.Close()

	done := make(chan struct{}, 1)
	go func() {
		_, _ = io.Copy(server, client)
		if u, ok := server.(*net.UnixConn); ok {
			_ = u.CloseWrite()
		}
		done <- struct{}{}
	}()
	_, _ = io.Copy(client, server)
	if t, ok := client.(*net.TCPConn); ok {
		_ = t.CloseWrite()
	}
	<-done
}

func splitNonEmpty(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
