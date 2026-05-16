//go:build linux || darwin

package dataset

import (
	"os"
	"syscall"
)

func mmapReadOnly(f *os.File, sz int) ([]byte, error) {
	return syscall.Mmap(int(f.Fd()), 0, sz, syscall.PROT_READ, syscall.MAP_SHARED)
}

func munmap(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	return syscall.Munmap(b)
}
