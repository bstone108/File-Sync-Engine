//go:build unix && !linux

package fatalexit

import "syscall"

func dupToStderr(from, to int) error {
	return syscall.Dup2(from, to)
}
