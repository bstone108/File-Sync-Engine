package monitor

import (
	"time"

	"filesyncengine/internal/filewatch"
	"filesyncengine/internal/scheduler"
)

type Folder struct {
	ID   string
	Path string
}

type Options struct {
	EventDebounce    time.Duration
	FallbackInterval time.Duration
	PollInterval     time.Duration
}

type Event struct {
	Type     string
	FolderID string
	Path     string
	Message  string
}

type Monitor struct {
	watchers []*filewatch.Watcher
	done     chan struct{}
}

func New(folders []Folder, opts Options, emit func(Event)) (*Monitor, error) {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 25 * time.Millisecond
	}
	sched := scheduler.New(scheduler.Options{EventDebounce: opts.EventDebounce, FallbackInterval: opts.FallbackInterval})
	mon := &Monitor{done: make(chan struct{})}
	now := time.Now()
	for _, folder := range folders {
		sched.AddFolder(folder.ID, now)
		watcher, err := filewatch.New(folder.Path)
		if err != nil {
			emit(Event{Type: "watch.error", FolderID: folder.ID, Path: folder.Path, Message: err.Error()})
			continue
		}
		mon.watchers = append(mon.watchers, watcher)
		go mon.forwardWatch(folder.ID, watcher, sched, emit)
	}
	go mon.pollDue(sched, opts.PollInterval, emit)
	return mon, nil
}

func (m *Monitor) Close() error {
	select {
	case <-m.done:
	default:
		close(m.done)
	}
	var err error
	for _, watcher := range m.watchers {
		if closeErr := watcher.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}

func (m *Monitor) forwardWatch(folderID string, watcher *filewatch.Watcher, sched *scheduler.Scheduler, emit func(Event)) {
	for {
		select {
		case event, ok := <-watcher.Events():
			if !ok {
				return
			}
			sched.Notify(folderID, time.Now())
			emit(Event{Type: "watch.event", FolderID: folderID, Path: event.Path, Message: event.Op})
		case err, ok := <-watcher.Errors():
			if !ok {
				return
			}
			emit(Event{Type: "watch.error", FolderID: folderID, Message: err.Error()})
		case <-m.done:
			return
		}
	}
}

func (m *Monitor) pollDue(sched *scheduler.Scheduler, interval time.Duration, emit func(Event)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			for _, folderID := range sched.Due(now) {
				emit(Event{Type: "scan.due", FolderID: folderID})
			}
		case <-m.done:
			return
		}
	}
}
