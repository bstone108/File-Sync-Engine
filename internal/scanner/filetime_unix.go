//go:build linux

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
	return stat.Ctim.Nano()
}
