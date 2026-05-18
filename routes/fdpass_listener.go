package routes

import (
	"errors"
	"net"
	"os"
	"sync"
)

// CombinedListener accepts both regular UDS conns and conns adopted from a
// SCM_RIGHTS fd channel, exposing them as a single net.Listener so the
// fasthttp server hot path stays unchanged.
type CombinedListener struct {
	regular  net.Listener
	fdCh     <-chan int
	out      chan net.Conn
	close    chan struct{}
	closeOnce sync.Once
}

func NewCombinedListener(regular net.Listener, fdCh <-chan int) *CombinedListener {
	cl := &CombinedListener{
		regular: regular,
		fdCh:    fdCh,
		out:     make(chan net.Conn, 256),
		close:   make(chan struct{}),
	}
	if regular != nil {
		go cl.runRegular()
	}
	if fdCh != nil {
		go cl.runFDPass()
	}
	return cl
}

func (c *CombinedListener) runRegular() {
	for {
		conn, err := c.regular.Accept()
		if err != nil {
			return
		}
		select {
		case c.out <- conn:
		case <-c.close:
			_ = conn.Close()
			return
		}
	}
}

func (c *CombinedListener) runFDPass() {
	for fd := range c.fdCh {
		f := os.NewFile(uintptr(fd), "scm-fd")
		conn, err := net.FileConn(f)
		_ = f.Close()
		if err != nil {
			continue
		}
		select {
		case c.out <- conn:
		case <-c.close:
			_ = conn.Close()
			return
		}
	}
}

func (c *CombinedListener) Accept() (net.Conn, error) {
	select {
	case <-c.close:
		return nil, errors.New("listener closed")
	case conn, ok := <-c.out:
		if !ok {
			return nil, errors.New("listener closed")
		}
		return conn, nil
	}
}

func (c *CombinedListener) Close() error {
	c.closeOnce.Do(func() {
		close(c.close)
		if c.regular != nil {
			_ = c.regular.Close()
		}
	})
	return nil
}

func (c *CombinedListener) Addr() net.Addr {
	if c.regular != nil {
		return c.regular.Addr()
	}
	return fdpassAddr{}
}

type fdpassAddr struct{}

func (fdpassAddr) Network() string { return "unix-fdpass" }
func (fdpassAddr) String() string  { return "scm-rights" }
