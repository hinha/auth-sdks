//go:build !linux

package obs

import "errors"

func readProcIO() (procIO, error) {
	return procIO{}, errors.New("process io is not available on this platform")
}
