//go:build unix

package fatalexit

import (
	"os"
	"sync"
	"syscall"
)

var stderrDetachOnce sync.Once

// DetachStderrWriteErrors keeps a library from calling os.Exit(2) when a
// requested stop closes the container log pipe. Later stderr writes still
// reach the original stream; a broken pipe is ignored.
func DetachStderrWriteErrors() {
	stderrDetachOnce.Do(func() {
		orig, err := syscall.Dup(int(os.Stderr.Fd()))
		if err != nil {
			return
		}
		reader, writer, err := os.Pipe()
		if err != nil {
			_ = syscall.Close(orig)
			return
		}
		if err := dupToStderr(int(writer.Fd()), int(os.Stderr.Fd())); err != nil {
			_ = reader.Close()
			_ = writer.Close()
			_ = syscall.Close(orig)
			return
		}
		_ = writer.Close()
		go func() {
			buf := make([]byte, 4096)
			for {
				n, readErr := reader.Read(buf)
				if n > 0 {
					_, _ = syscall.Write(orig, buf[:n])
				}
				if readErr != nil {
					_ = syscall.Close(orig)
					return
				}
			}
		}()
	})
}
