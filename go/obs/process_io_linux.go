//go:build linux

package obs

import "github.com/prometheus/procfs"

func readProcIO() (procIO, error) {
	p, err := procfs.Self()
	if err != nil {
		return procIO{}, err
	}
	in, err := p.IO()
	if err != nil {
		return procIO{}, err
	}
	return procIO{
		RChar:      in.RChar,
		WChar:      in.WChar,
		ReadBytes:  in.ReadBytes,
		WriteBytes: in.WriteBytes,
	}, nil
}
