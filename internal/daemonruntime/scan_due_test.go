package daemonruntime

import (
	"errors"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/foldersync"
	"filesyncengine/internal/monitor"
	"filesyncengine/internal/peerpullplan"
	"filesyncengine/internal/peersync"
	"filesyncengine/internal/routing"
)

type fakePublisher struct {
	events []api.Event
}

func (p *fakePublisher) Publish(event api.Event) {
	p.events = append(p.events, event)
}

type fakeTransferGate struct {
	cancelled map[string]bool
	paused    map[string]bool
	cleared   []string
}

func (g *fakeTransferGate) IsCancelled(folderID, peerID string) bool {
	return g.cancelled[folderID+"/"+peerID]
}

func (g *fakeTransferGate) ClearCancel(folderID, peerID string) {
	g.cleared = append(g.cleared, folderID+"/"+peerID)
}

func (g *fakeTransferGate) IsPaused(folderID, peerID string) bool {
	return g.paused[folderID+"/"+peerID]
}

type fakeSyncRunner struct {
	result foldersync.Result
	err    error
	calls  []string
}

func (r *fakeSyncRunner) ScanDue(folderID string) (foldersync.Result, error) {
	r.calls = append(r.calls, folderID)
	return r.result, r.err
}

type fakePeerPuller struct {
	results map[string]peersync.Result
	errors  map[string]error
	calls   []peerpullplan.Pull
}

func (p *fakePeerPuller) Pull(pull peerpullplan.Pull) (peersync.Result, error) {
	p.calls = append(p.calls, pull)
	if err := p.errors[pull.PeerID]; err != nil {
		return peersync.Result{}, err
	}
	return p.results[pull.PeerID], nil
}

func TestHandleMonitorEventPublishesMonitorEventAndRunsScanDue(t *testing.T) {
	publisher := &fakePublisher{}
	runner := &fakeSyncRunner{result: foldersync.Result{Targets: 1, Writes: 2}}
	puller := &fakePeerPuller{results: map[string]peersync.Result{"peer-ok": {BlocksReused: 3}}}
	pulls := []peerpullplan.Pull{{PeerID: "peer-ok", FolderID: "docs", Path: routing.DirectPath, Network: routing.LocalNetwork, RouteReason: routing.ReasonDirectPreferred}}

	HandleMonitorEvent(MonitorEventOptions{
		Event:      monitor.Event{Type: "scan.due", FolderID: "docs", Message: "scheduled scan"},
		Publisher:  publisher,
		SyncRunner: runner,
		PeerPulls:  pulls,
		PeerPuller: puller,
	})

	if len(runner.calls) != 1 || runner.calls[0] != "docs" {
		t.Fatalf("expected scan.due to run local sync for docs, got %#v", runner.calls)
	}
	if len(puller.calls) != 1 || puller.calls[0].PeerID != "peer-ok" {
		t.Fatalf("expected scan.due to run peer pulls, got %#v", puller.calls)
	}
	assertEvent(t, publisher.events, "scan.due", "docs", "", "scheduled scan")
	assertEvent(t, publisher.events, "sync.finished", "docs", "", "targets=1 writes=2 deletes=0 moves=0 reusedBlocks=0")
	assertEvent(t, publisher.events, "peer.sync.finished", "docs", "peer-ok", "writes=0 deletes=0 moves=0 blocksFetched=0 blocksReused=3 routePath=direct routeNetwork=local routeReason=direct_preferred")
}

func TestHandleMonitorEventDoesNotRunScanForOrdinaryMonitorEvent(t *testing.T) {
	publisher := &fakePublisher{}
	runner := &fakeSyncRunner{}

	HandleMonitorEvent(MonitorEventOptions{
		Event:      monitor.Event{Type: "watch.event", FolderID: "docs", Message: "file changed"},
		Publisher:  publisher,
		SyncRunner: runner,
	})

	if len(runner.calls) != 0 {
		t.Fatalf("ordinary monitor event should not run local sync, got %#v", runner.calls)
	}
	assertEvent(t, publisher.events, "watch.event", "docs", "", "file changed")
}

func TestProcessScanDueRunsLocalSyncAndPeerPullsWithTransferGates(t *testing.T) {
	publisher := &fakePublisher{}
	gate := &fakeTransferGate{
		cancelled: map[string]bool{"docs/peer-cancelled": true},
		paused:    map[string]bool{"docs/peer-paused": true},
	}
	runner := &fakeSyncRunner{result: foldersync.Result{Targets: 2, Writes: 3, Deletes: 1, Moves: 1, ReusedBlocks: 4}}
	puller := &fakePeerPuller{
		results: map[string]peersync.Result{"peer-ok": {Writes: 1, Deletes: 1, BlocksFetched: 2}},
		errors:  map[string]error{"peer-error": errors.New("remote unavailable")},
	}
	pulls := []peerpullplan.Pull{
		{PeerID: "peer-ok", FolderID: "docs", BaseURL: "http://peer-ok", APIKey: "secret", LocalPath: "/data/docs", Path: routing.DirectPath, Network: routing.LocalNetwork, RouteReason: routing.ReasonDirectPreferred},
		{PeerID: "peer-error", FolderID: "docs", BaseURL: "http://peer-error"},
		{PeerID: "peer-cancelled", FolderID: "docs", BaseURL: "http://peer-cancelled"},
		{PeerID: "peer-paused", FolderID: "docs", BaseURL: "http://peer-paused"},
	}

	ProcessScanDue(ScanDueOptions{FolderID: "docs", Publisher: publisher, TransferGate: gate, SyncRunner: runner, PeerPulls: pulls, PeerPuller: puller})

	if len(runner.calls) != 1 || runner.calls[0] != "docs" {
		t.Fatalf("expected one local scan for docs, got %#v", runner.calls)
	}
	if len(puller.calls) != 2 || puller.calls[0].PeerID != "peer-ok" || puller.calls[1].PeerID != "peer-error" {
		t.Fatalf("expected only non-paused/non-cancelled peers to be pulled, got %#v", puller.calls)
	}
	if len(gate.cleared) != 1 || gate.cleared[0] != "docs/peer-cancelled" {
		t.Fatalf("expected cancelled peer marker cleared, got %#v", gate.cleared)
	}
	assertEvent(t, publisher.events, "sync.finished", "docs", "", "targets=2 writes=3 deletes=1 moves=1 reusedBlocks=4")
	assertEvent(t, publisher.events, "sync.started", "docs", "", "folder sync pass started")
	assertEvent(t, publisher.events, "peer.sync.started", "docs", "peer-ok", "peer transfer pass started")
	assertEvent(t, publisher.events, "peer.sync.finished", "docs", "peer-ok", "writes=1 deletes=1 moves=0 blocksFetched=2 blocksReused=0 routePath=direct routeNetwork=local routeReason=direct_preferred")
	assertEvent(t, publisher.events, "peer.sync.error", "docs", "peer-error", "remote unavailable")
	assertEvent(t, publisher.events, "transfer.cancelled", "docs", "peer-cancelled", "peer transfer pass cancelled")
	assertEvent(t, publisher.events, "transfer.paused", "docs", "peer-paused", "peer transfer is paused")
}

func TestProcessScanDueHonorsFolderWidePauseBeforeLocalSync(t *testing.T) {
	publisher := &fakePublisher{}
	gate := &fakeTransferGate{paused: map[string]bool{"docs/": true}}
	runner := &fakeSyncRunner{}
	puller := &fakePeerPuller{}

	ProcessScanDue(ScanDueOptions{FolderID: "docs", Publisher: publisher, TransferGate: gate, SyncRunner: runner, PeerPuller: puller})

	if len(runner.calls) != 0 {
		t.Fatalf("folder-wide pause should skip local sync, got calls %#v", runner.calls)
	}
	assertEvent(t, publisher.events, "transfer.paused", "docs", "", "folder transfers are paused")
}

func assertEvent(t *testing.T, events []api.Event, typ, folderID, peerID, message string) {
	t.Helper()
	for _, event := range events {
		if event.Type == typ && event.FolderID == folderID && event.PeerID == peerID && event.Message == message {
			return
		}
	}
	t.Fatalf("missing event type=%s folder=%s peer=%s message=%q in %#v", typ, folderID, peerID, message, events)
}
