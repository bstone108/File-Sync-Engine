//go:build windows

package scanner

import "os"

func changeTimeUnixNano(info os.FileInfo) int64 {
	return info.ModTime().UnixNano()
}
