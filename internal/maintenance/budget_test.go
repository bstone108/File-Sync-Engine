package maintenance

import (
	"context"
	"testing"
	"time"
)

func TestRunOnceDefaultsToSingleStepWhenNoBudgetIsConfigured(t *testing.T) {
	crawler := &fakeCrawler{items: []string{"a", "b", "c"}}
	checkpoint := &memoryCheckpoint{}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:    crawler,
		Checkpoint: checkpoint,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FilesScanned != 1 {
		t.Fatalf("FilesScanned=%d, want default single-step budget", result.FilesScanned)
	}
	if result.Complete {
		t.Fatalf("Complete=true, want false after default single-step budget")
	}
	if checkpoint.cursor.Position != 1 {
		t.Fatalf("cursor=%d, want 1", checkpoint.cursor.Position)
	}
}

func TestRunOnceFinishesOversizedSingleFileBeforeByteBudgetStopsNextFile(t *testing.T) {
	crawler := &fakeCrawler{items: []string{"large", "next"}}
	checkpoint := &memoryCheckpoint{}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:     crawler,
		Checkpoint:  checkpoint,
		MaxBytes:    1,
		MaxDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FilesScanned != 1 || result.BytesScanned != int64(len("large")) {
		t.Fatalf("result=(files %d bytes %d), want finish first oversized file", result.FilesScanned, result.BytesScanned)
	}
	if result.Complete {
		t.Fatalf("Complete=true, want byte budget to stop before next file")
	}
}
