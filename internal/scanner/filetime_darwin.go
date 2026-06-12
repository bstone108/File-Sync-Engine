//go:build darwin

package scanner

import (
	"os"
	"syscall"
)

func changeTimeUnixNano(info os.FileInfo) int64 {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info.ModTime().UnixNano()
	}
	if stat.Birthtimespec.Sec != 0 || stat.Birthtimespec.Nsec != 0 {
		return stat.Birthtimespec.Nano()
	}
	return stat.Ctimespec.Nano()
}
