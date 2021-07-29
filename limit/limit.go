// +build windows plan9

package limit

func Nofile(desired uint64) (uint64, error) {
	return desired, nil
}
