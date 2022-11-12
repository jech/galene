// +build linux

package limit

import (
	"golang.org/x/sys/unix"
)

func Nofile(desired uint64) (uint64, error) {
	var old unix.Rlimit

	err := unix.Getrlimit(unix.RLIMIT_NOFILE, &old)
	if err != nil {
		return 0, err
	}

	if old.Cur >= desired {
		return old.Cur, nil
	}

	if old.Max < desired {
		limit := unix.Rlimit{
			Cur: desired,
			Max: desired,
		}
		err := unix.Setrlimit(unix.RLIMIT_NOFILE, &limit)
		if err == nil {
			return limit.Cur, nil
		}

		desired = old.Max
	}

	limit := unix.Rlimit{
		Cur: desired,
		Max: old.Max,
	}
	err = unix.Setrlimit(unix.RLIMIT_NOFILE, &limit)
	if err != nil {
		return old.Cur, err
	}
	return limit.Cur, nil
}
