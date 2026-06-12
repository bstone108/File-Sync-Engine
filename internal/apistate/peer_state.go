package apistate

import (
	"strings"
	"time"

	"filesyncengine/internal/api"
	"filesyncengine/internal/backup"
	"filesyncengine/internal/config"
	"filesyncengine/internal/engine"
	"filesyncengine/internal/metadatastore"
	"filesyncengine/internal/routing"
	"filesyncengine/internal/snapshotcontrol"
	"filesyncengine/internal/state"
)

// StateBuildOptions supplies the daemon API state projection inputs that used
// to be assembled directly in the CLI/daemon entrypoint.
type StateBuildOptions struct {
	Config         config.Config
	ConfigPath     string
	Version        uint64
	Status         string
	Store          state.JSONStore
	ArchiveRoot    string
	CheckpointRoot string
	StartedAt      time.Time
}

// StoreOpener opens the configured metadata store for API-state projection.
type StoreOpener func(config.Config, string) (state.JSONStore, string, error)

// ConfiguredStateBuildOptions supplies config plus runtime identity for API-state projection.
type ConfiguredStateBuildOptions struct {
	Config      config.Config
	ConfigPath  string
	Version     uint64
	Status      string
	StartedAt   time.Time
	StoreOpener StoreOpener
}

// BuildConfiguredState opens the configured metadata store and projects daemon API state.
// If the configured store cannot be opened, it falls back to the default JSON store so
// status responses still expose config/runtime state while the metadata backend is degraded.
func BuildConfiguredState(opts ConfiguredStateBuildOptions) api.State {
	openStore := opts.StoreOpener
	if openStore == nil {
		openStore = metadatastore.Open
	}
	store, _, err := openStore(opts.Config, opts.ConfigPath)
	if err != nil {
		store = state.NewJSONStore(metadatastore.DefaultStatePath(opts.ConfigPath))
	} else {
		defer store.Close()
	}
	return BuildState(StateBuildOptions{
		Config:         opts.Config,
		ConfigPath:     opts.ConfigPath,
		Version:        opts.Version,
		Status:         opts.Status,
		Store:          store,
		ArchiveRoot:    snapshotcontrol.ArchivePath(opts.Config, opts.ConfigPath),
		CheckpointRoot: snapshotcontrol.CheckpointRootPath(opts.Config, opts.ConfigPath),
		StartedAt:      opts.StartedAt,
	})
}

// BuildState projects config plus metadata-store status into the daemon API state.
func BuildState(opts StateBuildOptions) api.State {
	startedAt := opts.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	folders := make([]api.FolderState, 0, len(opts.Config.Folders))
	eng := engine.New(opts.Store)
	for _, folder := range opts.Config.Folders {
		folderState := api.FolderState{ID: folder.ID, Path: folder.Path, Mode: string(folder.Mode), Status: "configured"}
		if index, err := eng.FolderIndexState(folder.ID); err == nil {
			folderState.Index = FolderIndexState(index)
		}
		if syncState, err := FolderSyncState(opts.Store, folder.ID); err == nil {
			folderState.Sync = syncState
		}
		if warningState, err := FolderWarningState(opts.Store, folder.ID); err == nil {
			folderState.Warnings = warningState
		}
		folders = append(folders, folderState)
	}

	peers := make([]api.PeerState, 0, len(opts.Config.Peers))
	for _, peer := range opts.Config.Peers {
		peers = append(peers, BuildPeerState(opts.Config, peer, opts.Store))
	}

	backupState := api.BackupState{Enabled: opts.Config.Backup.Enabled, Mode: string(opts.Config.Backup.Mode)}
	if opts.Config.Backup.Enabled {
		if availability, err := backup.ComputeSnapshotAvailabilityStatus(backup.SnapshotAvailabilityOptions{ArchiveRoot: opts.ArchiveRoot, CheckpointRoot: opts.CheckpointRoot, Store: opts.Store}); err == nil {
			backupState.Snapshots = BackupSnapshotState(availability)
		}
	}

	return api.State{
		NodeName:      opts.Config.NodeName,
		StartedAt:     startedAt,
		ConfigPath:    opts.ConfigPath,
		Folders:       len(opts.Config.Folders),
		Peers:         len(opts.Config.Peers),
		ConfigVersion: opts.Version,
		Status:        opts.Status,
		Maintenance:   api.MaintenanceState{Enabled: MaintenanceEnabled(opts.Config)},
		Backup:        backupState,
		FoldersState:  folders,
		PeersState:    peers,
	}
}

// BuildPeerState projects one configured peer into the daemon API state.
func BuildPeerState(cfg config.Config, peer config.PeerConfig, store state.JSONStore) api.PeerState {
	endpoint := ""
	if len(peer.Endpoints) > 0 {
		endpoint = peer.Endpoints[0].Kind + ":" + peer.Endpoints[0].Address
	} else if len(peer.Addresses) > 0 {
		endpoint = "manual:" + peer.Addresses[0]
	}
	peerState := api.PeerState{ID: peer.ID, Status: "configured", Endpoint: endpoint, Transfer: PeerTransferState(cfg.Transfer, peer), NetworkDiagnostics: PeerNetworkDiagnostics(peer, cfg)}
	if statuses, err := store.PeerFolderStatuses(peer.ID); err == nil {
		peerState.Metadata = PeerMetadataState(statuses)
	}
	return peerState
}

// MaintenanceEnabled reports whether global or per-folder maintenance is enabled.
func MaintenanceEnabled(cfg config.Config) bool {
	if cfg.Maintenance.Enabled {
		return true
	}
	for _, folder := range cfg.Folders {
		if folder.Maintenance.Enabled {
			return true
		}
	}
	return false
}

// PeerTransferState projects configured local transfer caps into the API peer state.
func PeerTransferState(global config.TransferConfig, peer config.PeerConfig) api.PeerTransferState {
	details := config.EffectiveTransferLimitDetails(global, peer, config.TransferConfig{}, config.PeerConfig{})
	return api.PeerTransferState{Configured: details.Effective, Effective: details.Effective, SendCause: details.SendCause, ReceiveCause: details.ReceiveCause}
}

// PeerNetworkDiagnostics projects routing endpoint diagnostics into the API peer state.
func PeerNetworkDiagnostics(peer config.PeerConfig, cfg config.Config) []api.PeerNetworkDiagnostic {
	candidates := PeerEndpointCandidates(peer, nil, RoutingNetworkHints(cfg))
	routingDiagnostics := routing.DiagnoseEndpointCandidates(candidates)
	diagnostics := make([]api.PeerNetworkDiagnostic, 0, len(routingDiagnostics))
	for _, diagnostic := range routingDiagnostics {
		diagnostics = append(diagnostics, api.PeerNetworkDiagnostic{
			Code:      string(diagnostic.Code),
			Address:   diagnostic.Address,
			RoutePath: string(diagnostic.RoutePath),
			Network:   string(diagnostic.Network),
			Guidance:  diagnostic.Guidance,
		})
	}
	return diagnostics
}

// BackupSnapshotState projects backup snapshot availability into the daemon API state.
func BackupSnapshotState(status backup.SnapshotAvailabilityStatus) api.BackupSnapshotState {
	items := make(map[string]api.BackupSnapshotAvailability, len(status.Snapshots))
	for id, snapshot := range status.Snapshots {
		items[id] = api.BackupSnapshotAvailability{
			SnapshotID:            snapshot.SnapshotID,
			FolderID:              snapshot.FolderID,
			MetadataPresent:       snapshot.MetadataPresent,
			DBCheckpointAvailable: snapshot.DBCheckpointAvailable,
			ArchiveFullyProtected: snapshot.ArchiveFullyProtected,
			Archive: api.BackupArchiveAvailability{
				TotalBlocks:          snapshot.Archive.TotalBlocks,
				ProtectedBlocks:      snapshot.Archive.ProtectedBlocks,
				PendingBlocks:        snapshot.Archive.PendingBlocks,
				FailedBlocks:         snapshot.Archive.FailedBlocks,
				MissingArchiveBlocks: snapshot.Archive.MissingArchiveBlocks,
			},
		}
	}
	return api.BackupSnapshotState{
		TotalSnapshots:            status.TotalSnapshots,
		MetadataSnapshots:         status.MetadataSnapshots,
		ArchiveProtectedSnapshots: status.ArchiveProtectedSnapshots,
		DBCheckpointSnapshots:     status.DBCheckpointSnapshots,
		Items:                     items,
	}
}

// FolderSyncState projects metadata cursor and deferred-delete state into API folder state.
func FolderSyncState(store state.JSONStore, folderID string) (api.FolderSyncState, error) {
	summary, err := store.FolderSummary(folderID)
	if err != nil {
		return api.FolderSyncState{}, err
	}
	deferred, err := store.SkippedDeletes(folderID)
	if err != nil {
		return api.FolderSyncState{}, err
	}
	ready, err := store.ReadySkippedDeletes(folderID, summary)
	if err != nil {
		return api.FolderSyncState{}, err
	}
	readyByPath := make(map[string]struct{}, len(ready))
	for _, delete := range ready {
		readyByPath[delete.Path] = struct{}{}
	}
	pendingCatchup := false
	for _, delete := range deferred {
		if delete.Reason != "metadata_catchup_pending" {
			continue
		}
		if _, ok := readyByPath[delete.Path]; !ok {
			pendingCatchup = true
			break
		}
	}
	return api.FolderSyncState{
		LocalCursor:            summary.Cursor,
		LocalStateHash:         summary.StateHash,
		DeferredDeletes:        len(deferred),
		ReadyDeferredDeletes:   len(ready),
		MetadataCatchupPending: pendingCatchup,
	}, nil
}

// FolderWarningState projects compact locked-apply warning state into API folder state.
func FolderWarningState(store state.JSONStore, folderID string) (api.FolderWarningState, error) {
	writes, err := store.PendingWrites(folderID)
	if err != nil {
		return api.FolderWarningState{}, err
	}
	warningState := api.FolderWarningState{}
	for _, write := range writes {
		if write.Committed {
			continue
		}
		if write.Reason != "locked_apply_pending" && write.Reason != "write_locked" {
			continue
		}
		warningState.PendingLockedApplies++
		warningState.Recent = append(warningState.Recent, api.FolderWarning{Kind: "locked_apply_pending", Path: write.Path, Message: "replacement blocks are cached until the target can be safely updated"})
	}
	return warningState, nil
}

// PeerMetadataState projects per-folder peer metadata status into API peer state.
func PeerMetadataState(statuses []state.PeerFolderStatus) api.PeerMetadataState {
	folders := make([]api.PeerFolderMetadataStatus, 0, len(statuses))
	for _, status := range statuses {
		folders = append(folders, api.PeerFolderMetadataStatus{
			FolderID:       status.FolderID,
			PeerCursor:     status.PeerCursor,
			PeerStateHash:  status.PeerStateHash,
			LocalCursor:    status.LocalCursor,
			LocalStateHash: status.LocalStateHash,
			InSync:         status.InSync,
		})
	}
	return api.PeerMetadataState{Folders: folders}
}

// FolderIndexState projects engine index counters into API folder state.
func FolderIndexState(index engine.FolderIndexState) api.FolderIndexState {
	return api.FolderIndexState{
		Mode:                   index.Mode,
		TotalFiles:             index.TotalFiles,
		VerifiedFiles:          index.VerifiedFiles,
		UnknownFiles:           index.UnknownFiles,
		UnverifiedSeedFiles:    index.UnverifiedSeedFiles,
		KnownBlocks:            index.KnownBlocks,
		BadBlocks:              index.BadBlocks,
		QueuedHashJobs:         index.QueuedHashJobs,
		ActiveHashJobs:         index.ActiveHashJobs,
		DateCorrectionsPending: index.DateCorrectionsPending,
		ProvisionalReadOnly:    index.ProvisionalReadOnly,
	}
}

// PeerEndpointCandidates builds status/routing candidates from configured peer endpoints.
func PeerEndpointCandidates(peer config.PeerConfig, sidecarObservations []routing.EndpointObservation, networkHints routing.NetworkHints) []routing.EndpointCandidate {
	manualEndpoints := []routing.EndpointObservation{}
	sidecarEndpoints := []routing.EndpointObservation{}
	for _, endpoint := range peer.Endpoints {
		path, ok := PeerEndpointPath(endpoint)
		if !ok {
			continue
		}
		observation := routing.EndpointObservation{PeerID: peer.ID, Address: endpoint.Address, Reachable: true, Path: path, NetworkHint: endpoint.NetworkHint}
		if endpoint.Kind == "sidecar" {
			sidecarEndpoints = append(sidecarEndpoints, observation)
		} else {
			manualEndpoints = append(manualEndpoints, observation)
		}
	}
	for _, observation := range sidecarObservations {
		if observation.PeerID != peer.ID {
			continue
		}
		if observation.Path == "" {
			observation.Path = routing.DirectPath
		}
		sidecarEndpoints = append(sidecarEndpoints, observation)
	}
	return routing.BuildEndpointCandidates(routing.EndpointCandidateRequest{TargetPeerID: peer.ID, ManualEndpoints: manualEndpoints, SidecarEndpoints: sidecarEndpoints, NetworkHints: networkHints})
}

// PeerEndpointPath maps configured HTTP endpoint kinds to data route kinds.
func PeerEndpointPath(endpoint config.EndpointConfig) (routing.PathKind, bool) {
	if !strings.HasPrefix(endpoint.Address, "http") {
		return "", false
	}
	switch endpoint.Kind {
	case "manual", "vpn", "sidecar":
		return routing.DirectPath, true
	case "relay", "proxy":
		return routing.RelayPath, true
	default:
		return "", false
	}
}

// PeerEndpointNetwork classifies a configured endpoint for transfer-source selection.
func PeerEndpointNetwork(cfg config.Config, endpoint config.EndpointConfig) routing.NetworkKind {
	if endpoint.Kind == "vpn" {
		return routing.VPNNetwork
	}
	if endpoint.Kind == "relay" || endpoint.Kind == "proxy" {
		return routing.WANNetwork
	}
	if endpoint.NetworkHint != "" {
		return routing.NetworkKind(endpoint.NetworkHint)
	}
	return routing.ClassifyEndpointNetworkWithHints(endpoint.Address, RoutingNetworkHints(cfg))
}

// RoutingNetworkHints converts validated config hints into routing-package hints.
func RoutingNetworkHints(cfg config.Config) routing.NetworkHints {
	return routing.NetworkHints{
		LocalContainerGatewayIPs: cfg.Discovery.NetworkHints.LocalContainerGatewayIPs,
		LocalCIDRs:               cfg.Discovery.NetworkHints.LocalCIDRs,
		PublishedPortMappings:    RoutingPublishedPortMappings(cfg.Discovery.NetworkHints.PublishedPortMappings),
	}
}

// RoutingPublishedPortMappings converts config published-port hints for routing decisions.
func RoutingPublishedPortMappings(mappings []config.PublishedPortMappingConfig) []routing.PublishedPortMapping {
	out := make([]routing.PublishedPortMapping, 0, len(mappings))
	for _, mapping := range mappings {
		out = append(out, routing.PublishedPortMapping{HostIP: mapping.HostIP, HostPort: mapping.HostPort})
	}
	return out
}
