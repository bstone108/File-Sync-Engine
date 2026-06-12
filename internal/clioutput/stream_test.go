package clioutput

import (
	"testing"

	"filesyncengine/internal/streamsync"
)

func TestStreamPullSummaryRendersStableCounters(t *testing.T) {
	result := streamsync.PullResult{
		FilesWritten:  2,
		FilesDeleted:  3,
		FilesMoved:    1,
		BlocksFetched: 5,
		BlocksReused:  8,
	}

	got := StreamPullSummary(result)
	want := "stream pull finished: writes=2 deletes=3 moves=1 blocksFetched=5 blocksReused=8\n"
	if got != want {
		t.Fatalf("StreamPullSummary() = %q, want %q", got, want)
	}
}
