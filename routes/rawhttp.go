package routes

import (
	"context"
	"net"
	"os"
	"sync"
	"sync/atomic"

	"github.com/vinnedev/rinha-2026/internal/fraud"
)

const (
	maxRequestSize = 8 * 1024
	readBufSize    = 4 * 1024
)

// Pre-built full HTTP/1.1 responses (status line + headers + body) for the
// six possible scoreRaw outcomes and the readiness / liveness probes. Each
// entry is the exact byte sequence written to the client socket — no
// formatting, no setters, one Write per request.
var (
	rawApprovedZero []byte
	rawApprovedP2   []byte
	rawApprovedP4   []byte
	rawDeniedP6     []byte
	rawDeniedP8     []byte
	rawDeniedOne    []byte

	rawReadyOK      = []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 18\r\n\r\n{\"status\":\"ready\"}")
	rawReadyNotYet  = []byte("HTTP/1.1 503 Service Unavailable\r\nContent-Type: application/json\r\nContent-Length: 22\r\n\r\n{\"status\":\"not_ready\"}")
	rawHealthOK     = []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 18\r\n\r\n{\"status\":\"alive\"}")
	rawMethodDenied = []byte("HTTP/1.1 405 Method Not Allowed\r\nContent-Length: 0\r\n\r\n")
	rawNotFound     = []byte("HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n")
)

func init() {
	rawApprovedZero = buildHTTP(respApprovedZero)
	rawApprovedP2 = buildHTTP(respApprovedP2)
	rawApprovedP4 = buildHTTP(respApprovedP4)
	rawDeniedP6 = buildHTTP(respDeniedP6)
	rawDeniedP8 = buildHTTP(respDeniedP8)
	rawDeniedOne = buildHTTP(respDeniedOne)
}

func buildHTTP(body []byte) []byte {
	clen := itoaSmall(len(body))
	out := make([]byte, 0, 80+len(body))
	out = append(out, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: "...)
	out = append(out, clen...)
	out = append(out, "\r\n\r\n"...)
	out = append(out, body...)
	return out
}

func itoaSmall(n int) []byte {
	if n == 0 {
		return []byte{'0'}
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return append([]byte(nil), buf[i:]...)
}

var rawReadBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, readBufSize)
		return &b
	},
}

// RawServer owns a Unix-domain listener and serves HTTP/1.1 with a per-
// connection keep-alive loop. The per-request hot path:
//
//	1. find header end (\r\n\r\n)
//	2. extract path and content-length
//	3. read body bytes into the same buffer
//	4. call the route handler
//	5. write a pre-computed response byte slice
//
// No header map, no per-request allocations on the hot path. Pipelined
// requests share one read buffer per connection.
type RawServer struct {
	svc *fraud.Service
	ln  net.Listener
}

// NewRawServer creates a server but does not start it. Call ServeUnix to
// bind a socket and start accepting.
func NewRawServer(svc *fraud.Service) *RawServer {
	return &RawServer{svc: svc}
}

// ServeUnix removes any stale socket file at path, binds a new Unix
// listener, and runs the accept loop in a goroutine. Returns the listener
// so the caller can close it on shutdown.
func (s *RawServer) ServeUnix(path string) (net.Listener, error) {
	_ = os.Remove(path)
	lc := &net.ListenConfig{}
	ln, err := lc.Listen(context.Background(), "unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o666)
	s.ln = ln
	go s.acceptLoop(ln)
	return ln, nil
}

// ServeTCP binds a TCP listener at addr and runs the accept loop.
func (s *RawServer) ServeTCP(addr string) (net.Listener, error) {
	lc := &net.ListenConfig{}
	ln, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		return nil, err
	}
	s.ln = ln
	go s.acceptLoop(ln)
	return ln, nil
}

func (s *RawServer) acceptLoop(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(c)
	}
}

func (s *RawServer) handleConn(conn net.Conn) {
	defer conn.Close()
	bufRef := rawReadBufPool.Get().(*[]byte)
	buf := *bufRef
	used := 0
	pos := 0
	defer func() {
		if cap(buf) <= maxRequestSize {
			*bufRef = buf[:cap(buf)]
			rawReadBufPool.Put(bufRef)
		}
	}()

	for {
		var headEnd int
		for {
			if idx := indexHeaderEnd(buf[pos:used]); idx >= 0 {
				headEnd = pos + idx + 4
				break
			}
			if used == len(buf) {
				if pos > 0 {
					copy(buf, buf[pos:used])
					used -= pos
					pos = 0
				} else if used >= maxRequestSize {
					return
				} else {
					nb := make([]byte, len(buf)*2)
					copy(nb, buf[:used])
					buf = nb
				}
			}
			n, err := conn.Read(buf[used:])
			if n > 0 {
				used += n
				continue
			}
			if err != nil {
				return
			}
		}

		method, path, contentLen := parseRequestLine(buf[pos:headEnd])
		if contentLen > maxRequestSize-(headEnd-pos) {
			return
		}
		bodyEnd := headEnd + contentLen
		for used < bodyEnd {
			if used == len(buf) {
				if pos > 0 {
					copy(buf, buf[pos:used])
					used -= pos
					headEnd -= pos
					bodyEnd -= pos
					pos = 0
				} else {
					nb := make([]byte, len(buf)*2)
					copy(nb, buf[:used])
					buf = nb
				}
			}
			n, err := conn.Read(buf[used:])
			if n > 0 {
				used += n
				continue
			}
			if err != nil {
				return
			}
		}

		resp := s.route(method, path, buf[headEnd:bodyEnd])
		if _, err := conn.Write(resp); err != nil {
			return
		}

		pos = bodyEnd
		if pos == used {
			pos = 0
			used = 0
		}
	}
}

func (s *RawServer) route(method, path, body []byte) []byte {
	if len(path) == 12 && string(path) == "/fraud-score" {
		if !equalBytes(method, "POST") {
			return rawMethodDenied
		}
		return s.scoreRawHTTP(body)
	}
	if equalBytes(method, "GET") {
		switch {
		case equalBytes(path, "/readyz") || equalBytes(path, "/ready"):
			if rawReady.Load() {
				return rawReadyOK
			}
			return rawReadyNotYet
		case equalBytes(path, "/healthz"):
			return rawHealthOK
		}
	}
	return rawNotFound
}

func (s *RawServer) scoreRawHTTP(body []byte) []byte {
	if len(body) == 0 {
		return rawApprovedZero
	}
	if shedSem != nil {
		select {
		case shedSem <- struct{}{}:
			defer func() { <-shedSem }()
		default:
			return rawApprovedZero
		}
	}
	p := intPayloadPool.Get().(*fraud.IntPayload)
	if !fraud.ParseFast(body, p) {
		intPayloadPool.Put(p)
		return rawApprovedZero
	}
	resp := s.svc.ScoreInt(p)
	intPayloadPool.Put(p)
	return pickRawResponse(resp.FraudScore)
}

func pickRawResponse(score float64) []byte {
	switch {
	case score < 0.1:
		return rawApprovedZero
	case score < 0.3:
		return rawApprovedP2
	case score < 0.5:
		return rawApprovedP4
	case score < 0.7:
		return rawDeniedP6
	case score < 0.9:
		return rawDeniedP8
	default:
		return rawDeniedOne
	}
}

var rawReady atomic.Bool

// RawMarkReady flips the readiness flag for the raw server. Mirrors the
// fasthttp-side MarkReady so both servers share readiness semantics when
// running in raw mode.
func RawMarkReady()    { rawReady.Store(true) }
func RawMarkNotReady() { rawReady.Store(false) }

func indexHeaderEnd(b []byte) int {
	for i := 0; i+3 < len(b); i++ {
		if b[i] == '\r' && b[i+1] == '\n' && b[i+2] == '\r' && b[i+3] == '\n' {
			return i
		}
	}
	return -1
}

func parseRequestLine(buf []byte) (method, path []byte, contentLen int) {
	i := 0
	for i < len(buf) && buf[i] != ' ' {
		i++
	}
	method = buf[:i]
	i++
	pathStart := i
	for i < len(buf) && buf[i] != ' ' {
		i++
	}
	path = buf[pathStart:i]
	contentLen = findContentLength(buf)
	return
}

func findContentLength(buf []byte) int {
	for i := 0; i+16 < len(buf); i++ {
		if (buf[i] == 'C' || buf[i] == 'c') && isContentLengthPrefix(buf[i:]) {
			j := i + 16
			for j < len(buf) && buf[j] == ' ' {
				j++
			}
			n := 0
			for j < len(buf) && buf[j] >= '0' && buf[j] <= '9' {
				n = n*10 + int(buf[j]-'0')
				j++
			}
			return n
		}
	}
	return 0
}

func isContentLengthPrefix(b []byte) bool {
	const name = "content-length: "
	if len(b) < len(name) {
		return false
	}
	for i := 0; i < 15; i++ {
		c := b[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != name[i] {
			return false
		}
	}
	return true
}

func equalBytes(b []byte, s string) bool {
	if len(b) != len(s) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if b[i] != s[i] {
			return false
		}
	}
	return true
}
