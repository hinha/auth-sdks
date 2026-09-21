//go:build linux || darwin

package obs

import "golang.org/x/sys/unix"

func statfsPath(path string) (fsStat, error) {
	var s unix.Statfs_t
	if err := unix.Statfs(path, &s); err != nil {
		return fsStat{}, err
	}
	bsize := uint64(s.Bsize)
	return fsStat{
		Avail: uint64(s.Bavail) * bsize,
		Size:  uint64(s.Blocks) * bsize,
	}, nil
}
