package filewatch

import (
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

type Event struct {
	Path string
	Op   string
}

type Watcher struct {
	watcher *fsnotify.Watcher
	events  chan Event
	errors  chan error
	done    chan struct{}
}

func New(root string) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{watcher: fsw, events: make(chan Event, 64), errors: make(chan error, 8), done: make(chan struct{})}
	if err := w.addRecursive(root); err != nil {
		_ = fsw.Close()
		return nil, err
	}
	go w.run()
	return w, nil
}

func (w *Watcher) Events() <-chan Event { return w.events }

func (w *Watcher) Errors() <-chan error { return w.errors }

func (w *Watcher) Close() error {
	close(w.done)
	return w.watcher.Close()
}

func (w *Watcher) run() {
	defer close(w.events)
	defer close(w.errors)
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := w.addRecursive(event.Name); err != nil {
						w.reportError(err)
					}
				}
			}
			w.reportEvent(Event{Path: event.Name, Op: event.Op.String()})
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.reportError(err)
		case <-w.done:
			return
		}
	}
}

func (w *Watcher) addRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		return w.watcher.Add(path)
	})
}

func (w *Watcher) reportEvent(event Event) {
	select {
	case w.events <- event:
	default:
	}
}

func (w *Watcher) reportError(err error) {
	select {
	case w.errors <- err:
	default:
	}
}
