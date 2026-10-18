//go:build linux

package fatalexit

import "syscall"

// dupToStderr uses dup3. Linux arm64 and riscv64 do not export syscall.Dup2.
func dupToStderr(from, to int) error {
	return syscall.Dup3(from, to, 0)
}
