package maintenance

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeCrawler struct {
	items []string
}

func (c *fakeCrawler) Step(_ context.Context, cursor Cursor) (StepResult, error) {
	idx := int(cursor.Position)
	if idx >= len(c.items) {
		return StepResult{Cursor: Cursor{Position: 0}, Complete: true}, nil
	}
	return StepResult{Cursor: Cursor{Position: uint64(idx + 1)}, FilesScanned: 1, BytesScanned: int64(len(c.items[idx])), Complete: idx+1 >= len(c.items)}, nil
}

type memoryCheckpoint struct {
	cursor Cursor
	saves  []Cursor
}

func (m *memoryCheckpoint) LoadMaintenanceCursor() (Cursor, error) { return m.cursor, nil }

func (m *memoryCheckpoint) SaveMaintenanceCursor(cursor Cursor) error {
	m.cursor = cursor
	m.saves = append(m.saves, cursor)
	return nil
}

func TestRunOncePersistsCursorAndStopsAtFileBudget(t *testing.T) {
	crawler := &fakeCrawler{items: []string{"a", "b", "c"}}
	checkpoint := &memoryCheckpoint{}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:     crawler,
		Checkpoint:  checkpoint,
		MaxFiles:    2,
		MaxDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.FilesScanned != 2 {
		t.Fatalf("FilesScanned=%d, want 2", result.FilesScanned)
	}
	if result.Complete {
		t.Fatalf("Complete=true, want false when budget stops crawl")
	}
	if checkpoint.cursor.Position != 2 {
		t.Fatalf("saved cursor position=%d, want 2", checkpoint.cursor.Position)
	}

	result, err = RunOnce(context.Background(), RunOptions{
		Crawler:     crawler,
		Checkpoint:  checkpoint,
		MaxFiles:    2,
		MaxDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("resume RunOnce: %v", err)
	}
	if result.FilesScanned != 1 || !result.Complete {
		t.Fatalf("resume result=(files %d complete %v), want one file and complete", result.FilesScanned, result.Complete)
	}
	if checkpoint.cursor.Position != 0 {
		t.Fatalf("cursor after complete=%d, want reset to 0", checkpoint.cursor.Position)
	}
}

func TestRunOnceYieldsBeforeStartingWhenForegroundBusy(t *testing.T) {
	crawler := &fakeCrawler{items: []string{"a"}}
	checkpoint := &memoryCheckpoint{}

	result, err := RunOnce(context.Background(), RunOptions{
		Crawler:     crawler,
		Checkpoint:  checkpoint,
		MaxFiles:    1,
		MaxDuration: time.Minute,
		ShouldYield: func() bool { return true },
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !result.Yielded || result.FilesScanned != 0 {
		t.Fatalf("result=(yielded %v files %d), want yielded before scanning", result.Yielded, result.FilesScanned)
	}
	if len(checkpoint.saves) != 0 {
		t.Fatalf("saved cursor despite no work: %v", checkpoint.saves)
	}
}

func TestWorkerRunsScheduledPassesUntilStopped(t *testing.T) {
	crawler := &fakeCrawler{items: []string{"a", "b"}}
	checkpoint := &memoryCheckpoint{}
	worker := NewWorker(WorkerOptions{
		Crawler:    crawler,
		Checkpoint: checkpoint,
		Interval:   time.Millisecond,
		MaxFiles:   1,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := worker.Start(ctx)

	first := waitForUpdate(t, updates)
	if first.FilesScanned != 1 {
		t.Fatalf("first files=%d, want 1", first.FilesScanned)
	}
	second := waitForUpdate(t, updates)
	if second.FilesScanned != 1 || !second.Complete {
		t.Fatalf("second=(files %d complete %v), want one file and complete", second.FilesScanned, second.Complete)
	}
	cancel()
}

func TestRunOncePropagatesCrawlerErrorWithoutAdvancingCursor(t *testing.T) {
	boom := errors.New("boom")
	checkpoint := &memoryCheckpoint{cursor: Cursor{Position: 7}}
	_, err := RunOnce(context.Background(), RunOptions{
		Crawler: crawlerFunc(func(context.Context, Cursor) (StepResult, error) {
			return StepResult{}, boom
		}),
		Checkpoint:  checkpoint,
		MaxFiles:    1,
		MaxDuration: time.Minute,
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v, want boom", err)
	}
	if checkpoint.cursor.Position != 7 || len(checkpoint.saves) != 0 {
		t.Fatalf("cursor advanced on error: cursor=%v saves=%v", checkpoint.cursor, checkpoint.saves)
	}
}

type crawlerFunc func(context.Context, Cursor) (StepResult, error)

func (f crawlerFunc) Step(ctx context.Context, cursor Cursor) (StepResult, error) {
	return f(ctx, cursor)
}

func waitForUpdate(t *testing.T, updates <-chan RunResult) RunResult {
	t.Helper()
	select {
	case result := <-updates:
		return result
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for maintenance update")
	}
	return RunResult{}
}
