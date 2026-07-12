package daemonruntime

import (
	"fmt"

	"filesyncengine/internal/api"
	"filesyncengine/internal/foldersync"
	"filesyncengine/internal/monitor"
	"filesyncengine/internal/peerevents"
	"filesyncengine/internal/peerpullplan"
	"filesyncengine/internal/peersync"
)

// Publisher is the small daemon API event surface needed by scan-due runtime
// handling. Keeping this narrow lets the daemon entrypoint wire process-owned
// API state while the transfer control flow stays package-tested.
type Publisher interface {
	Publish(api.Event)
}

// TransferGate gates local and peer transfer passes for runtime pause/cancel
// commands.
type TransferGate interface {
	IsCancelled(folderID, peerID string) bool
	ClearCancel(folderID, peerID string)
	IsPaused(folderID, peerID string) bool
}

// SyncRunner performs a local folder scan/sync pass.
type SyncRunner interface {
	ScanDue(folderID string) (foldersync.Result, error)
}

// PeerPuller performs one peer pull selected by the daemon planner.
type PeerPuller interface {
	Pull(peerpullplan.Pull) (peersync.Result, error)
}

// PeerPullerFunc adapts a function to PeerPuller.
type PeerPullerFunc func(peerpullplan.Pull) (peersync.Result, error)

func (fn PeerPullerFunc) Pull(pull peerpullplan.Pull) (peersync.Result, error) {
	return fn(pull)
}

// ScanDueOptions contains the process-owned dependencies for handling a single
// monitor scan-due event.
type ScanDueOptions struct {
	FolderID       string
	Publisher      Publisher
	TransferGate   TransferGate
	SyncRunner     SyncRunner
	PeerPulls      []peerpullplan.Pull
	PeerPuller     PeerPuller
	WarningHandler func([]foldersync.InaccessibleWarning)
	Cooperative    func(folderID string, pulls []peerpullplan.Pull)
}

// MonitorEventOptions contains the process-owned dependencies for projecting a
// monitor event into daemon API events and, for scan-due events, into the
// transfer runtime.
type MonitorEventOptions struct {
	Event          monitor.Event
	Publisher      Publisher
	TransferGate   TransferGate
	SyncRunner     SyncRunner
	PeerPulls      []peerpullplan.Pull
	PeerPuller     PeerPuller
	WarningHandler func([]foldersync.InaccessibleWarning)
	Cooperative    func(folderID string, pulls []peerpullplan.Pull)
}

// HandleMonitorEvent publishes the raw monitor event and runs the scan-due
// transfer path only for scan.due events. This keeps cmd/fse focused on wiring
// live dependencies instead of embedding monitor event business logic inline.
func HandleMonitorEvent(opts MonitorEventOptions) {
	publish(opts.Publisher, api.Event{Type: opts.Event.Type, FolderID: opts.Event.FolderID, Message: opts.Event.Message})
	if opts.Event.Type != "scan.due" {
		return
	}
	ProcessScanDue(ScanDueOptions{
		FolderID:       opts.Event.FolderID,
		Publisher:      opts.Publisher,
		TransferGate:   opts.TransferGate,
		SyncRunner:     opts.SyncRunner,
		PeerPulls:      opts.PeerPulls,
		PeerPuller:     opts.PeerPuller,
		WarningHandler: opts.WarningHandler,
		Cooperative:    opts.Cooperative,
	})
}

// ProcessScanDue handles one daemon scan-due event: local sync, warning
// propagation, cooperative-fetch diagnostics, and configured peer pulls. It is
// intentionally side-effect-light so cmd/fse only wires concrete runners.
func ProcessScanDue(opts ScanDueOptions) {
	if opts.TransferGate != nil && opts.TransferGate.IsCancelled(opts.FolderID, "") {
		opts.TransferGate.ClearCancel(opts.FolderID, "")
		publish(opts.Publisher, api.Event{Type: "transfer.cancelled", FolderID: opts.FolderID, Message: "folder transfer pass cancelled"})
		return
	}
	if opts.TransferGate != nil && opts.TransferGate.IsPaused(opts.FolderID, "") {
		publish(opts.Publisher, api.Event{Type: "transfer.paused", FolderID: opts.FolderID, Message: "folder transfers are paused"})
		return
	}
	if opts.SyncRunner == nil {
		publish(opts.Publisher, api.Event{Type: "sync.error", FolderID: opts.FolderID, Message: "sync runner is not configured"})
		return
	}
	publish(opts.Publisher, api.Event{Type: "sync.started", FolderID: opts.FolderID, Message: "folder sync pass started"})
	result, err := opts.SyncRunner.ScanDue(opts.FolderID)
	if err != nil {
		publish(opts.Publisher, api.Event{Type: "sync.error", FolderID: opts.FolderID, Message: err.Error()})
		return
	}
	publish(opts.Publisher, api.Event{Type: "sync.finished", FolderID: opts.FolderID, Message: fmt.Sprintf("targets=%d writes=%d deletes=%d moves=%d reusedBlocks=%d", result.Targets, result.Writes, result.Deletes, result.Moves, result.ReusedBlocks)})
	if opts.WarningHandler != nil {
		opts.WarningHandler(result.Inaccessible)
	}
	if opts.Cooperative != nil {
		opts.Cooperative(opts.FolderID, opts.PeerPulls)
	}
	for _, pull := range opts.PeerPulls {
		processPeerPull(opts, pull)
	}
}

func processPeerPull(opts ScanDueOptions, pull peerpullplan.Pull) {
	if opts.TransferGate != nil && opts.TransferGate.IsCancelled(opts.FolderID, pull.PeerID) {
		opts.TransferGate.ClearCancel(opts.FolderID, pull.PeerID)
		publish(opts.Publisher, api.Event{Type: "transfer.cancelled", FolderID: opts.FolderID, PeerID: pull.PeerID, Message: "peer transfer pass cancelled"})
		return
	}
	if opts.TransferGate != nil && opts.TransferGate.IsPaused(opts.FolderID, pull.PeerID) {
		publish(opts.Publisher, api.Event{Type: "transfer.paused", FolderID: opts.FolderID, PeerID: pull.PeerID, Message: "peer transfer is paused"})
		return
	}
	if opts.PeerPuller == nil {
		publish(opts.Publisher, api.Event{Type: "peer.sync.error", FolderID: opts.FolderID, PeerID: pull.PeerID, Message: "peer puller is not configured"})
		return
	}
	publish(opts.Publisher, api.Event{Type: "peer.sync.started", FolderID: opts.FolderID, PeerID: pull.PeerID, Message: "peer transfer pass started"})
	result, err := opts.PeerPuller.Pull(pull)
	if err != nil {
		publish(opts.Publisher, api.Event{Type: "peer.sync.error", FolderID: opts.FolderID, PeerID: pull.PeerID, Message: err.Error()})
		return
	}
	publish(opts.Publisher, api.Event{Type: "peer.sync.finished", FolderID: opts.FolderID, PeerID: pull.PeerID, Message: peerevents.SyncFinishedMessageWithRoute(result, peerevents.Route{Path: string(pull.Path), Network: string(pull.Network), RouteReason: string(pull.RouteReason)})})
}

func publish(publisher Publisher, event api.Event) {
	if publisher != nil {
		publisher.Publish(event)
	}
}
