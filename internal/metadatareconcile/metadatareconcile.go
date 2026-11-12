package metadatareconcile

import (
	"context"
	"fmt"
	"io"
	"net"

	"filesyncengine/internal/access"
	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/daemon"
	"filesyncengine/internal/discovery"
	"filesyncengine/internal/discoverycontrol"
	"filesyncengine/internal/peeridentity"
	"filesyncengine/internal/routing"
	"filesyncengine/internal/state"
	"filesyncengine/internal/streamsync"
	"filesyncengine/internal/structuredlog"
)

// PeerStreamEndpointCandidates builds metadata stream endpoint candidates from
// configured stream-capable peer endpoints plus live sidecar/helper observations.
// It deliberately filters out HTTP-only endpoints because metadata catch-up uses
// the stream protocol, not the manual HTTP peer API.
func PeerStreamEndpointCandidates(peer config.PeerConfig, sidecarObservations []routing.EndpointObservation, networkHints routing.NetworkHints) []routing.EndpointCandidate {
	manualEndpoints := []routing.EndpointObservation{}
	sidecarEndpoints := []routing.EndpointObservation{}
	for _, endpoint := range peer.Endpoints {
		path, ok := StreamEndpointPath(endpoint)
		if !ok {
			continue
		}
		if _, ok := discoverycontrol.StreamTCPAddress(endpoint.Address); !ok {
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
		if _, ok := discoverycontrol.StreamTCPAddress(observation.Address); !ok {
			continue
		}
		if observation.Path == "" {
			observation.Path = routing.DirectPath
		}
		sidecarEndpoints = append(sidecarEndpoints, observation)
	}
	return routing.BuildEndpointCandidates(routing.EndpointCandidateRequest{TargetPeerID: peer.ID, ManualEndpoints: manualEndpoints, SidecarEndpoints: sidecarEndpoints, NetworkHints: networkHints})
}

// StreamEndpointPath maps configured endpoint kinds onto metadata stream route
// path classes. Unsupported kinds are ignored by stream metadata reconciliation.
func StreamEndpointPath(endpoint config.EndpointConfig) (routing.PathKind, bool) {
	switch endpoint.Kind {
	case "manual", "vpn", "sidecar":
		return routing.DirectPath, true
	case "relay", "proxy":
		return routing.RelayPath, true
	default:
		return "", false
	}
}

type CatchupResult struct {
	Started   int
	Completed int
	Failed    int
}

type CatchupDialer func(context.Context, config.PeerConfig, config.FolderConfig) (io.ReadWriteCloser, error)

type TCPDialFunc func(context.Context, string, string) (io.ReadWriteCloser, error)

type RuntimeDialerOptions struct {
	EndpointObservations []routing.EndpointObservation
	NetworkHints         routing.NetworkHints
	DialTCP              TCPDialFunc
	DialLibp2p           func(context.Context, string) (io.ReadWriteCloser, error)
}

// RuntimeDialer builds the live metadata catch-up dialer used by the daemon.
// It keeps endpoint selection in this package so cmd/fse only supplies the
// concrete network dial function and live discovery observations.
func RuntimeDialer(opts RuntimeDialerOptions) CatchupDialer {
	dialTCP := opts.DialTCP
	if dialTCP == nil {
		dialTCP = func(ctx context.Context, network string, address string) (io.ReadWriteCloser, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, address)
		}
	}
	return func(ctx context.Context, peer config.PeerConfig, folder config.FolderConfig) (io.ReadWriteCloser, error) {
		for _, candidate := range PeerStreamEndpointCandidates(peer, opts.EndpointObservations, opts.NetworkHints) {
			if candidate.ControlPlaneOnly || !candidate.Reachable {
				continue
			}
			address, ok := discoverycontrol.StreamTCPAddress(candidate.Address)
			if !ok {
				continue
			}
			return dialTCP(ctx, "tcp", address)
		}
		if peer.DHTPeerID != "" {
			dialLibp2p := opts.DialLibp2p
			if dialLibp2p == nil {
				dialLibp2p = discovery.DialMetadata
			}
			stream, err := dialLibp2p(ctx, peer.DHTPeerID)
			if err != nil {
				structuredlog.Event("warn", "metadata.catchup.dial_failed", "metadata catch-up could not use the libp2p session", map[string]any{
					"peer": peer.ID, "dhtPeerId": peer.DHTPeerID, "folder": folder.ID, "error": err.Error(),
				})
				return nil, err
			}
			return stream, nil
		}
		err := fmt.Errorf("peer %s has no tcp stream endpoint for metadata catch-up", peer.ID)
		structuredlog.Event("warn", "metadata.catchup.dial_failed", "metadata catch-up has no route", map[string]any{
			"peer": peer.ID, "folder": folder.ID, "error": err.Error(),
		})
		return nil, err
	}
}

type EventPublisher interface {
	Publish(api.Event)
}

type CatchupOptions struct {
	Publisher EventPublisher
	Config    config.Config
	Store     state.JSONStore
	Dial      CatchupDialer
}

// ProcessCatchup runs metadata-only reconciliation for configured peers whose
// recorded peer metadata state lags the local folder summary. It owns the
// catch-up decision loop so cmd/fse only wires runtime dependencies.
func ProcessCatchup(ctx context.Context, opts CatchupOptions) CatchupResult {
	result := CatchupResult{}
	if opts.Dial == nil {
		return result
	}
	for _, peer := range opts.Config.Peers {
		statuses, err := opts.Store.PeerFolderStatuses(peer.ID)
		if err != nil {
			publish(opts.Publisher, api.Event{Type: "metadata.catchup.error", PeerID: peer.ID, Message: err.Error()})
			structuredlog.Event("warn", "metadata.catchup.failed", "peer folder metadata was not read", map[string]any{
				"peer": peer.ID, "error": err.Error(),
			})
			result.Failed++
			continue
		}
		if len(statuses) == 0 {
			for _, folder := range opts.Config.Folders {
				if !metadataFolderForPeer(opts.Config, peer, folder) {
					continue
				}
				statuses = append(statuses, state.PeerFolderStatus{PeerID: peer.ID, FolderID: folder.ID})
			}
		}
		for _, status := range statuses {
			if status.InSync {
				continue
			}
			folder, ok := findConfigFolder(opts.Config, status.FolderID)
			if !ok || !folder.Enabled || folder.Path == "" {
				continue
			}
			stream, err := opts.Dial(ctx, peer, folder)
			if err != nil {
				publish(opts.Publisher, api.Event{Type: "metadata.catchup.error", PeerID: peer.ID, FolderID: folder.ID, Message: err.Error()})
				structuredlog.Event("warn", "metadata.catchup.failed", "metadata catch-up could not dial", map[string]any{
					"peer": peer.ID, "folder": folder.ID, "error": err.Error(),
				})
				result.Failed++
				continue
			}
			result.Started++
			catchup, err := streamsync.MetadataCatchupOnly(ctx, stream, streamsync.PullOptions{
				NodeID:          opts.Config.NodeName,
				FolderID:        folder.ID,
				Identity:        peeridentity.Identity{PrivateKey: opts.Config.Identity.PrivateKey, PublicKey: opts.Config.Identity.PublicKey},
				EncryptionLevel: opts.Config.Identity.EncryptionLevel,
				PeerPublicKey:   peer.IdentityPublicKey,
				MetadataStore:   opts.Store,
			}, peer.ID)
			closeErr := stream.Close()
			if err != nil {
				publish(opts.Publisher, api.Event{Type: "metadata.catchup.error", PeerID: peer.ID, FolderID: folder.ID, Message: err.Error()})
				structuredlog.Event("warn", "metadata.catchup.failed", "metadata catch-up did not finish", map[string]any{
					"peer": peer.ID, "folder": folder.ID, "error": err.Error(),
				})
				result.Failed++
				continue
			}
			if closeErr != nil {
				publish(opts.Publisher, api.Event{Type: "metadata.catchup.error", PeerID: peer.ID, FolderID: folder.ID, Message: closeErr.Error()})
				structuredlog.Event("warn", "metadata.catchup.failed", "metadata catch-up stream did not close", map[string]any{
					"peer": peer.ID, "folder": folder.ID, "error": closeErr.Error(),
				})
				result.Failed++
				continue
			}
			result.Completed++
			publish(opts.Publisher, api.Event{Type: "metadata.catchup.finished", PeerID: peer.ID, FolderID: folder.ID, Message: fmt.Sprintf("changes=%d fullRefreshes=%d", catchup.MetadataChangesApplied, catchup.MetadataFullRefreshes)})
			if deleted, err := daemon.ReconcileReadySkippedDeletes(folder.Path, folder.ID, opts.Store); err == nil && deleted.Deleted > 0 {
				publish(opts.Publisher, api.Event{Type: "metadata.deferredDeletes.applied", FolderID: folder.ID, Message: fmt.Sprintf("deleted=%d remaining=%d", deleted.Deleted, deleted.Remaining)})
			}
		}
	}
	return result
}

func publish(publisher EventPublisher, event api.Event) {
	if publisher != nil {
		publisher.Publish(event)
	}
}

func findConfigFolder(cfg config.Config, folderID string) (config.FolderConfig, bool) {
	for _, folder := range cfg.Folders {
		if folder.ID == folderID {
			return folder, true
		}
	}
	return config.FolderConfig{}, false
}

func metadataFolderForPeer(cfg config.Config, peer config.PeerConfig, folder config.FolderConfig) bool {
	if !folder.Enabled || folder.Path == "" || folder.ID == "" {
		return false
	}
	role := access.RoleFor(access.EnabledGroupIDs(cfg), peer.IdentityGroupID)
	if role == access.RoleIdentity {
		return true
	}
	return access.MaySyncFolder(role, folder.SharedWith, peer.ID)
}
