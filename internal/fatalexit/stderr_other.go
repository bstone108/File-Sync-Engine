//go:build !unix

package fatalexit

// DetachStderrWriteErrors is a no-op where the container stop path does not run.
func DetachStderrWriteErrors() {}
