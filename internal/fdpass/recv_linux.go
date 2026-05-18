//go:build linux

package fdpass

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func Listen(ctrlPath string) (<-chan int, int, error) {
	_ = unix.Unlink(ctrlPath)

	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, 0, fmt.Errorf("socket: %w", err)
	}
	if err := unix.Bind(fd, &unix.SockaddrUnix{Name: ctrlPath}); err != nil {
		_ = unix.Close(fd)
		return nil, 0, fmt.Errorf("bind %s: %w", ctrlPath, err)
	}
	_ = os.Chmod(ctrlPath, 0o666)
	if err := unix.Listen(fd, 16); err != nil {
		_ = unix.Close(fd)
		return nil, 0, fmt.Errorf("listen: %w", err)
	}

	out := make(chan int, 256)
	go acceptLoop(fd, out)
	return out, fd, nil
}

func acceptLoop(listenFd int, out chan<- int) {
	defer close(out)
	for {
		connFd, _, err := unix.Accept4(listenFd, unix.SOCK_CLOEXEC)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return
		}
		go recvLoop(connFd, out)
	}
}

func recvLoop(connFd int, out chan<- int) {
	defer unix.Close(connFd)

	buf := make([]byte, 16)
	oob := make([]byte, unix.CmsgSpace(4*4))

	for {
		_, oobn, _, _, err := unix.Recvmsg(connFd, buf, oob, 0)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return
		}
		if oobn == 0 {
			continue
		}
		msgs, err := unix.ParseSocketControlMessage(oob[:oobn])
		if err != nil {
			continue
		}
		for i := range msgs {
			fds, err := unix.ParseUnixRights(&msgs[i])
			if err != nil {
				continue
			}
			for _, fd := range fds {
				if err := unix.SetNonblock(fd, true); err != nil {
					_ = unix.Close(fd)
					continue
				}
				out <- fd
			}
		}
	}
}
