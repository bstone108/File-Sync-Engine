package scheduler

import (
	"sort"
	"time"
)

type Options struct {
	EventDebounce    time.Duration
	FallbackInterval time.Duration
}

type Scheduler struct {
	eventDebounce    time.Duration
	fallbackInterval time.Duration
	folders          map[string]*folderState
}

type folderState struct {
	lastScan  time.Time
	lastEvent time.Time
	pending   bool
}

func New(opts Options) *Scheduler {
	if opts.EventDebounce <= 0 {
		opts.EventDebounce = 500 * time.Millisecond
	}
	if opts.FallbackInterval <= 0 {
		opts.FallbackInterval = time.Minute
	}
	return &Scheduler{eventDebounce: opts.EventDebounce, fallbackInterval: opts.FallbackInterval, folders: map[string]*folderState{}}
}

func (s *Scheduler) AddFolder(id string, now time.Time) {
	if _, ok := s.folders[id]; ok {
		return
	}
	s.folders[id] = &folderState{lastScan: now}
}

func (s *Scheduler) RemoveFolder(id string) {
	delete(s.folders, id)
}

func (s *Scheduler) Notify(id string, at time.Time) {
	state, ok := s.folders[id]
	if !ok {
		state = &folderState{lastScan: at}
		s.folders[id] = state
	}
	state.lastEvent = at
	state.pending = true
}

func (s *Scheduler) Due(now time.Time) []string {
	ids := make([]string, 0)
	for id, state := range s.folders {
		eventDue := state.pending && !now.Before(state.lastEvent.Add(s.eventDebounce))
		fallbackDue := !now.Before(state.lastScan.Add(s.fallbackInterval))
		if eventDue || fallbackDue {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		state := s.folders[id]
		state.lastScan = now
		state.pending = false
	}
	return ids
}
