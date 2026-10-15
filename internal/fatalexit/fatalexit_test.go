package fatalexit

import (
	"bytes"
	"flag"
	"io"
	"os"
	"strings"
	"testing"
)

func TestInstallSendsLibraryLogsToStderr(t *testing.T) {
	Install()
	if got := flag.Lookup("logtostderr").Value.String(); got != "true" {
		t.Fatalf("logtostderr = %q", got)
	}
	if got := flag.Lookup("stderrthreshold").Value.String(); got != "INFO" && got != "0" {
		t.Fatalf("stderrthreshold = %q", got)
	}
}

func TestReportWritesTheExitToBothStreams(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	restore := redirect(t, stdout, stderr)
	Report(2, "metadata assertion failed")
	restore()

	for _, got := range []string{stdout.String(), stderr.String()} {
		if !strings.Contains(got, "fse fatal exit 2: metadata assertion failed") {
			t.Fatalf("stream missed the fatal line: %q", got)
		}
	}
}

func redirect(t *testing.T, stdout, stderr io.Writer) func() {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errRead, errWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = outWrite
	os.Stderr = errWrite
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(stdout, outRead)
		close(done)
	}()
	errDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(stderr, errRead)
		close(errDone)
	}()
	return func() {
		_ = outWrite.Close()
		_ = errWrite.Close()
		<-done
		<-errDone
		os.Stdout = oldOut
		os.Stderr = oldErr
	}
}
