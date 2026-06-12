package config

import (
	"os"
	"sync"
	"time"
)

type DebouncedManager struct {
	manager     *Manager
	path        string
	quietPeriod time.Duration
	mu          sync.Mutex
	pendingMod  time.Time
}

func NewDebouncedManager(path string, quietPeriod time.Duration) (*DebouncedManager, error) {
	manager, err := NewManager(path)
	if err != nil {
		return nil, err
	}
	return &DebouncedManager{manager: manager, path: path, quietPeriod: quietPeriod}, nil
}

func (m *DebouncedManager) Current() Config {
	return m.manager.Current()
}

func (m *DebouncedManager) ReloadIfQuiet(now time.Time) (bool, error) {
	st, err := os.Stat(m.path)
	if err != nil {
		return false, err
	}
	m.mu.Lock()
	if !st.ModTime().Equal(m.pendingMod) {
		m.pendingMod = st.ModTime()
	}
	pending := m.pendingMod
	m.mu.Unlock()

	if now.Sub(pending) < m.quietPeriod {
		return false, nil
	}
	changed, err := m.manager.ReloadIfChanged()
	if err != nil {
		return false, err
	}
	return changed, nil
}
