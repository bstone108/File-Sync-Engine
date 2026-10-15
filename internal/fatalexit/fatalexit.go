// Package fatalexit makes process death visible on both standard streams.
// Structured daemon logs normally go to a file, and some libraries call
// os.Exit(2) when their own log file cannot be created, so docker logs can
// otherwise end on the last ordinary event.
package fatalexit

import (
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"filesyncengine/internal/structuredlog"
	_ "github.com/golang/glog"
)

// Install sends library logs to stderr and copies runtime crash traces to
// stdout as well. It is safe to call more than once.
func Install() {
	if err := flag.Set("logtostderr", "true"); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "fse fatal exit setup: logtostderr: %v\n", err)
	}
	if err := flag.Set("stderrthreshold", "INFO"); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "fse fatal exit setup: stderrthreshold: %v\n", err)
	}
	if err := debug.SetCrashOutput(os.Stdout, debug.CrashOptions{}); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "fse fatal exit setup: crash output: %v\n", err)
	}
}

// Report writes one fatal line to stdout, stderr, and the structured log.
func Report(code int, message string) {
	line := fmt.Sprintf("fse fatal exit %d: %s\n", code, message)
	writeLine(os.Stdout, line)
	writeLine(os.Stderr, line)
	structuredlog.Event("error", "process.exit", message, map[string]any{"code": code})
}

// Exit reports message and then terminates with code.
func Exit(code int, message string) {
	Report(code, message)
	os.Exit(code)
}

// Go runs fn and, if it panics, reports the panic and exits 2.
func Go(fn func()) {
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				Exit(2, fmt.Sprintf("panic: %v\n%s", recovered, debug.Stack()))
			}
		}()
		fn()
	}()
}

func writeLine(w io.Writer, line string) {
	if w == nil {
		return
	}
	_, _ = io.WriteString(w, line)
}
