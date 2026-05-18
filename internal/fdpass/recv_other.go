//go:build !linux

package fdpass

import "errors"

func Listen(ctrlPath string) (<-chan int, int, error) {
	return nil, 0, errors.New("fdpass: SCM_RIGHTS only supported on Linux")
}
