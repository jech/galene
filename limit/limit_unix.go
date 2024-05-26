//go:build unix

package limit

import (
	"golang.org/x/sys/unix"
)

func Nofile() (uint64, error) {
	var limit unix.Rlimit

	err := unix.Getrlimit(unix.RLIMIT_NOFILE, &limit)
	if err != nil {
		return 0, err
	}

	return uint64(limit.Cur), nil
}
