//go:build !unix

package limit

func Nofile() (uint64, error) {
	return 0xFFFF, nil
}
