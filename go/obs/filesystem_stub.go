//go:build !(linux || darwin)

package obs

import "errors"

func statfsPath(string) (fsStat, error) {
	return fsStat{}, errors.New("filesystem stats are not available on this platform")
}
