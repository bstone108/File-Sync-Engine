package maintenance

import (
	"context"
	"errors"
	"time"
)

// Cursor is the durable resume point for the low-priority maintenance crawl.
// Position is intentionally generic for the prototype worker; concrete crawlers
// can map it to a sorted DB/file record offset without exposing store internals.
type Cursor struct {
	Position uint64 `json:"position"`
	FolderID string `json:"folderId,omitempty"`
	Path     string `json:"path,omitempty"`
	Revision uint64 `json:"revision,omitempty"`
}

type StepResult struct {
	Cursor       Cursor
	FilesScanned int
	BytesScanned int64
	Pruned       int
	Reported     int
	Quarantined  int
	Complete     bool
}

type RunResult struct {
	Cursor       Cursor
	FilesScanned int
	BytesScanned int64
	Pruned       int
	Reported     int
	Quarantined  int
	Complete     bool
	Yielded      bool
	StartedAt    time.Time
	FinishedAt   time.Time
}

type Crawler interface {
	Step(context.Context, Cursor) (StepResult, error)
}

type CheckpointStore interface {
	LoadMaintenanceCursor() (Cursor, error)
	SaveMaintenanceCursor(Cursor) error
}

type RunOptions struct {
	Crawler     Crawler
	Checkpoint  CheckpointStore
	MaxFiles    int
	MaxBytes    int64
	MaxDuration time.Duration
	ShouldYield func() bool
	Now         func() time.Time
}

type WorkerOptions struct {
	Crawler     Crawler
	Checkpoint  CheckpointStore
	Interval    time.Duration
	MaxFiles    int
	MaxBytes    int64
	MaxDuration time.Duration
	ShouldYield func() bool
	Now         func() time.Time
}

type Worker struct {
	opts WorkerOptions
}

func NewWorker(opts WorkerOptions) Worker {
	return Worker{opts: opts}
}

func (w Worker) Start(ctx context.Context) <-chan RunResult {
	updates := make(chan RunResult, 1)
	interval := w.opts.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		defer close(updates)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			result, err := RunOnce(ctx, RunOptions{
				Crawler:     w.opts.Crawler,
				Checkpoint:  w.opts.Checkpoint,
				MaxFiles:    w.opts.MaxFiles,
				MaxBytes:    w.opts.MaxBytes,
				MaxDuration: w.opts.MaxDuration,
				ShouldYield: w.opts.ShouldYield,
				Now:         w.opts.Now,
			})
			if err == nil {
				select {
				case updates <- result:
				case <-ctx.Done():
					return
				}
			}
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			}
		}
	}()
	return updates
}

func RunOnce(ctx context.Context, opts RunOptions) (RunResult, error) {
	if opts.Crawler == nil {
		return RunResult{}, errors.New("maintenance crawler is required")
	}
	if opts.Checkpoint == nil {
		return RunResult{}, errors.New("maintenance checkpoint store is required")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	started := now()
	result := RunResult{StartedAt: started}
	if shouldYield(opts.ShouldYield) {
		result.Yielded = true
		result.FinishedAt = now()
		return result, nil
	}
	cursor, err := opts.Checkpoint.LoadMaintenanceCursor()
	if err != nil {
		return RunResult{}, err
	}
	if opts.MaxFiles <= 0 && opts.MaxBytes <= 0 && opts.MaxDuration <= 0 {
		opts.MaxFiles = 1
	}
	deadline := time.Time{}
	if opts.MaxDuration > 0 {
		deadline = started.Add(opts.MaxDuration)
	}
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if shouldYield(opts.ShouldYield) || durationBudgetReached(deadline, now()) || fileBudgetReached(result.FilesScanned, opts.MaxFiles) || byteBudgetReached(result.BytesScanned, opts.MaxBytes) {
			result.Yielded = shouldYield(opts.ShouldYield)
			result.Cursor = cursor
			result.FinishedAt = now()
			return result, nil
		}
		step, err := opts.Crawler.Step(ctx, cursor)
		if err != nil {
			return result, err
		}
		cursor = step.Cursor
		if step.Complete {
			cursor = Cursor{}
		}
		result.Cursor = cursor
		result.FilesScanned += step.FilesScanned
		result.BytesScanned += step.BytesScanned
		result.Pruned += step.Pruned
		result.Reported += step.Reported
		result.Quarantined += step.Quarantined
		if err := opts.Checkpoint.SaveMaintenanceCursor(cursor); err != nil {
			return result, err
		}
		if step.Complete {
			result.Complete = true
			result.FinishedAt = now()
			return result, nil
		}
	}
}

func shouldYield(fn func() bool) bool {
	return fn != nil && fn()
}

func fileBudgetReached(scanned int, max int) bool {
	return max > 0 && scanned >= max
}

func byteBudgetReached(scanned int64, max int64) bool {
	return max > 0 && scanned >= max
}

func durationBudgetReached(deadline time.Time, now time.Time) bool {
	return !deadline.IsZero() && !now.Before(deadline)
}
