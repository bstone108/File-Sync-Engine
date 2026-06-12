package clioutput

import (
	"errors"
	"testing"
)

func TestErrorLineUsesConciseHumanReadablePrefix(t *testing.T) {
	line := ErrorLine("scan", errors.New("missing folder"))

	if line != "fse: scan: missing folder" {
		t.Fatalf("unexpected error line: %q", line)
	}
}

func TestErrorLineOmitsEmptyContextSeparator(t *testing.T) {
	line := ErrorLine("", errors.New("bad config"))

	if line != "fse: bad config" {
		t.Fatalf("unexpected error line: %q", line)
	}
}

func TestFormattedLineUsesPrefixAndNewline(t *testing.T) {
	line := FormattedLine("%s %d", "bad", 7)

	if line != "fse: bad 7\n" {
		t.Fatalf("unexpected formatted line: %q", line)
	}
}
