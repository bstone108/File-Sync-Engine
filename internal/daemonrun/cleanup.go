package daemonrun

import (
	"context"
	"time"
)

// Closable is the narrow lifecycle surface for daemon-owned resources such as
// folder monitors. It intentionally matches io.Closer without forcing callers
// to import an unrelated abstraction.
type Closable interface {
	Close() error
}

// HTTPServer is the narrow shutdown surface needed from net/http.Server.
type HTTPServer interface {
	Shutdown(context.Context) error
}

// CleanupOptions contains daemon-owned resources that should be released when
// the runtime loop exits.
type CleanupOptions struct {
	Monitor             Closable
	HTTPServer          HTTPServer
	HTTPShutdownTimeout time.Duration
}

// Cleanup closes process-owned daemon resources in a deterministic order. It
// collects cleanup errors for callers that want to log/report them, but it does
// not panic or stop after the first failure because shutdown should attempt to
// release every resource it owns.
func Cleanup(opts CleanupOptions) []error {
	var errs []error
	if opts.Monitor != nil {
		if err := opts.Monitor.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if opts.HTTPServer != nil {
		timeout := opts.HTTPShutdownTimeout
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := opts.HTTPServer.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}
