package daemonmonitor

import (
	"errors"
	"testing"

	"filesyncengine/internal/config"
	"filesyncengine/internal/monitor"
)

type testMonitor struct {
	closed bool
	err    error
}

func (m *testMonitor) Close() error {
	m.closed = true
	return m.err
}

func TestFoldersFromConfigProjectsFolderIDsAndPaths(t *testing.T) {
	cfg := config.Config{Folders: []config.FolderConfig{
		{ID: "docs", Path: "/srv/docs", Mode: config.ModeSendReceive},
		{ID: "photos", Path: "/srv/photos", Mode: config.ModeReceiveOnly},
	}}

	got := FoldersFromConfig(cfg)
	want := []monitor.Folder{{ID: "docs", Path: "/srv/docs"}, {ID: "photos", Path: "/srv/photos"}}
	if len(got) != len(want) {
		t.Fatalf("folder count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("folder %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestRebuildStartsReplacementBeforeClosingOldMonitor(t *testing.T) {
	oldMonitor := &testMonitor{}
	newMonitor := &testMonitor{}
	cfg := config.Config{Folders: []config.FolderConfig{{ID: "docs", Path: "/srv/new-docs"}}}
	startedBeforeClose := false

	next, rebuilt, err := Rebuild(oldMonitor, []monitor.Folder{{ID: "docs", Path: "/srv/old-docs"}}, cfg, func(folders []monitor.Folder) (Closable, error) {
		startedBeforeClose = !oldMonitor.closed
		if len(folders) != 1 || folders[0].Path != "/srv/new-docs" {
			t.Fatalf("replacement folders = %+v", folders)
		}
		return newMonitor, nil
	})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if !rebuilt || next != newMonitor || !oldMonitor.closed || !startedBeforeClose {
		t.Fatalf("rebuilt=%v next=%T oldClosed=%v startedBeforeClose=%v", rebuilt, next, oldMonitor.closed, startedBeforeClose)
	}
}

func TestRebuildKeepsOldMonitorWhenReplacementStartFails(t *testing.T) {
	oldMonitor := &testMonitor{}
	startErr := errors.New("watcher unavailable")
	cfg := config.Config{Folders: []config.FolderConfig{{ID: "docs", Path: "/srv/new-docs"}}}

	next, rebuilt, err := Rebuild(oldMonitor, []monitor.Folder{{ID: "docs", Path: "/srv/old-docs"}}, cfg, func([]monitor.Folder) (Closable, error) {
		return nil, startErr
	})
	if !errors.Is(err, startErr) {
		t.Fatalf("error = %v, want %v", err, startErr)
	}
	if rebuilt || next != oldMonitor || oldMonitor.closed {
		t.Fatalf("rebuilt=%v next=%T oldClosed=%v", rebuilt, next, oldMonitor.closed)
	}
}

func TestRebuildClosesReplacementWhenOldCloseFails(t *testing.T) {
	closeErr := errors.New("close failed")
	oldMonitor := &testMonitor{err: closeErr}
	newMonitor := &testMonitor{}
	cfg := config.Config{Folders: []config.FolderConfig{{ID: "docs", Path: "/srv/new-docs"}}}

	next, rebuilt, err := Rebuild(oldMonitor, []monitor.Folder{{ID: "docs", Path: "/srv/old-docs"}}, cfg, func([]monitor.Folder) (Closable, error) {
		return newMonitor, nil
	})
	if !errors.Is(err, closeErr) {
		t.Fatalf("error = %v, want %v", err, closeErr)
	}
	if rebuilt || next != oldMonitor || !newMonitor.closed {
		t.Fatalf("rebuilt=%v next=%T replacementClosed=%v", rebuilt, next, newMonitor.closed)
	}
}
