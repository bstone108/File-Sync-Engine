package streamsync

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"filesyncengine/internal/backup"
	"filesyncengine/internal/block"
	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
	"filesyncengine/internal/peeridentity"
	"filesyncengine/internal/protocol"
	"filesyncengine/internal/ratelimit"
	"filesyncengine/internal/recovery"
	"filesyncengine/internal/routing"
	"filesyncengine/internal/scanner"
	"filesyncengine/internal/state"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

type MetadataStore interface {
	FolderSummary(folderID string) (state.FolderSummary, error)
	ChangesSince(folderID string, cursor uint64) (state.FolderChanges, error)
	SavePeerFolderState(peerID string, summary state.FolderSummary) error
}

type SnapshotStore interface {
	ListSnapshotMarkers(folderID string) ([]state.SnapshotMarker, error)
	SaveSnapshotMarker(marker state.SnapshotMarker) error
	ListArchiveIntakeJobs(snapshotID string) ([]state.ArchiveIntakeJob, error)
}

type PendingSettingsStore interface {
	ListPendingSettingsChanges(targetNodeID string) ([]state.PendingSettingsChange, error)
	UpdatePendingSettingsChangeStatus(targetNodeID string, changeID string, status string, updatedAt string, lastError string) error
}

type MeshSettingsApplyStore interface {
	SavePendingSettingsChange(targetNodeID string, change state.PendingSettingsChange) error
	ApplyPendingSettingsChange(targetNodeID string, changeID string, appliedAt string) (state.NodeSettingsDocument, state.PendingSettingsChange, error)
}

type limitedMetadataStore interface {
	ChangesSinceLimit(folderID string, cursor uint64, maxChanges int) (state.FolderChanges, error)
}

type PullMetadataStore interface {
	FolderSummary(folderID string) (state.FolderSummary, error)
	PeerStateVector(peerID string) (state.PeerStateVector, error)
	ApplyPeerFolderChanges(peerID string, changes state.FolderChanges) error
}

type fullRefreshMetadataStore interface {
	ReplacePeerFolderFromFullRefresh(peerID string, folderID string, summary state.FolderSummary, manifests map[string]block.Manifest, revisions map[string]uint64) error
}

type skippedDeleteStore interface {
	SaveSkippedDelete(delete state.SkippedDelete) error
}

type revisionMetadataStore interface {
	ListManifestRevisions(folderID string) (map[string]uint64, error)
}

type ServerConfig struct {
	NodeID                       string
	BlockSize                    int
	Folders                      map[string]string
	Identity                     peeridentity.Identity
	EncryptionLevel              int
	AllowWeakerEncryptionLevel   bool
	TrustedPeerPublicKeys        map[string]string
	KnownPeers                   []discovery.Peer
	IdentityGroups               []discovery.IdentityGroupState
	Transfer                     config.TransferConfig
	Peer                         config.PeerConfig
	MetadataStore                MetadataStore
	SnapshotStore                SnapshotStore
	PendingSettingsStore         PendingSettingsStore
	SnapshotArchiveRoot          string
	SnapshotCheckpointRoot       string
	MaxMetadataChangesPerMessage int
}

type Server struct {
	cfg ServerConfig
}

func NewServer(cfg ServerConfig) *Server {
	return &Server{cfg: cfg}
}

func (s *Server) Serve(ctx context.Context, stream io.ReadWriter) error {
	codec := protocol.NewCodec(stream, stream)
	remotePeer := discovery.Peer{}
	sendLimiter := ratelimit.NewLimiter(0)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		msg, err := codec.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch msg.Type {
		case protocol.MessageHello:
			if err := s.verifyClientHello(msg.Hello); err != nil {
				if writeErr := codec.Write(protocol.Message{Type: protocol.MessageError, Error: &protocol.Error{Code: "peer_auth", Message: err.Error()}}); writeErr != nil {
					return writeErr
				}
				continue
			}
			level, err := negotiateEncryptionLevel(s.encryptionLevel(), msg.Hello.EncryptionLevel, s.cfg.AllowWeakerEncryptionLevel)
			if err != nil {
				return err
			}
			hello, sessionPrivate, err := signedSessionProtocolHello(s.cfg.NodeID, s.cfg.Identity, level, msg.Hello.Nonce, advertisedTransferLimits(s.cfg.Transfer, s.cfg.Peer))
			if err != nil {
				return err
			}
			hello.Capabilities = []string{"folder-index", "block-transfer"}
			if err := codec.Write(protocol.Message{Type: protocol.MessageHello, Hello: hello}); err != nil {
				return err
			}
			if level > 0 {
				secure, err := secureCodec(stream, sessionPrivate, msg.Hello, hello, false)
				if err != nil {
					return err
				}
				codec = secure
			}
			remotePeer = discovery.Peer{ID: msg.Hello.NodeID}
			negotiated := negotiatedTransferLimits(s.cfg.Transfer, s.cfg.Peer, msg.Hello.Transfer)
			sendLimiter = ratelimit.NewLimiter(negotiated.SendBytesPerSecond)
		case protocol.MessagePeerExchange:
			plan := discovery.PlanPeerExchange(s.cfg.NodeID, s.cfg.KnownPeers, remotePeer, peersFromProtocol(msg.PeerExchange.Peers))
			groups := planIdentityGroupResponses(s.cfg.NodeID, identityGroupsWithSnapshots(s.cfg.IdentityGroups, s.cfg.SnapshotStore, s.cfg.SnapshotArchiveRoot, s.cfg.SnapshotCheckpointRoot), remotePeer.ID, groupsFromProtocol(msg.PeerExchange.IdentityGroups))
			if err := codec.Write(protocol.Message{Type: protocol.MessagePeerExchange, PeerExchange: &protocol.PeerExchange{Peers: peersToProtocol(plan.ShareWithRemote), IdentityGroups: groupsToProtocol(groups)}}); err != nil {
				return err
			}
		case protocol.MessageMetadataState:
			if s.cfg.MetadataStore == nil {
				if err := codec.Write(protocol.Message{Type: protocol.MessageError, Error: &protocol.Error{Code: "metadata_unavailable", Message: "metadata store not configured"}}); err != nil {
					return err
				}
				continue
			}
			if err := s.serveMetadataState(codec, remotePeer.ID, msg.MetadataState); err != nil {
				if writeErr := codec.Write(protocol.Message{Type: protocol.MessageError, Error: metadataError(err)}); writeErr != nil {
					return writeErr
				}
				continue
			}
		case protocol.MessageMeshSettingsChanges:
			if err := s.serveMeshSettingsChanges(codec, remotePeer.ID, msg.MeshSettingsChanges); err != nil {
				if writeErr := codec.Write(protocol.Message{Type: protocol.MessageError, Error: &protocol.Error{Code: "mesh_settings", Message: err.Error()}}); writeErr != nil {
					return writeErr
				}
				continue
			}
		case protocol.MessageMeshSettingsAck:
			if err := s.recordMeshSettingsAck(remotePeer.ID, msg.MeshSettingsAck); err != nil {
				if writeErr := codec.Write(protocol.Message{Type: protocol.MessageError, Error: &protocol.Error{Code: "mesh_settings_ack", Message: err.Error()}}); writeErr != nil {
					return writeErr
				}
				continue
			}
		case protocol.MessageFolderIndex:
			folderID := msg.FolderIndex.FolderID
			root, ok := s.cfg.Folders[folderID]
			if !ok {
				if err := codec.Write(protocol.Message{Type: protocol.MessageError, Error: &protocol.Error{Code: "unknown_folder", Message: "folder not configured"}}); err != nil {
					return err
				}
				continue
			}
			index, err := scanner.ScanFolder(root, scanner.Options{BlockSize: s.blockSize()})
			if err != nil {
				return err
			}
			revisions := map[string]uint64{}
			if revStore, ok := s.cfg.MetadataStore.(revisionMetadataStore); ok {
				revisions, err = revStore.ListManifestRevisions(folderID)
				if err != nil {
					return err
				}
			}
			files := make([]protocol.FolderIndexFile, 0, len(index.Files))
			for _, file := range index.Files {
				manifest := file.Manifest
				manifest.Path = file.RelativePath
				files = append(files, protocol.FolderIndexFile{RelativePath: file.RelativePath, Manifest: manifest, Revision: revisions[file.RelativePath]})
			}
			var metadataSummary *protocol.MetadataFolderSummary
			if s.cfg.MetadataStore != nil {
				summary, err := s.cfg.MetadataStore.FolderSummary(folderID)
				if err != nil {
					return err
				}
				converted := summaryToProtocol(summary)
				metadataSummary = &converted
			}
			if err := codec.Write(protocol.Message{Type: protocol.MessageFolderIndex, FolderIndex: &protocol.FolderIndex{FolderID: folderID, Files: files, MetadataSummary: metadataSummary}}); err != nil {
				return err
			}
		case protocol.MessageBlockRequest:
			req := msg.BlockRequest
			root, ok := s.cfg.Folders[req.FolderID]
			if !ok {
				if err := codec.Write(protocol.Message{Type: protocol.MessageError, Error: &protocol.Error{Code: "unknown_folder", Message: "folder not configured"}}); err != nil {
					return err
				}
				continue
			}
			path, err := safePath(root, req.Path)
			if err != nil {
				if err := codec.Write(protocol.Message{Type: protocol.MessageError, Error: &protocol.Error{Code: "bad_path", Message: err.Error()}}); err != nil {
					return err
				}
				continue
			}
			data, err := readBlock(path, req.Index, req.BlockSize)
			if err != nil {
				if err := codec.Write(protocol.Message{Type: protocol.MessageError, Error: &protocol.Error{Code: "read_block", Message: err.Error()}}); err != nil {
					return err
				}
				continue
			}
			if err := sendLimiter.Wait(ctx, len(data)); err != nil {
				return err
			}
			if err := codec.Write(protocol.Message{Type: protocol.MessageBlockResponse, BlockResponse: &protocol.BlockResponse{FolderID: req.FolderID, Path: req.Path, Index: req.Index, Data: data}}); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported message type %s", msg.Type)
		}
	}
}

func (s *Server) blockSize() int {
	if s.cfg.BlockSize > 0 {
		return s.cfg.BlockSize
	}
	return 128 * 1024
}

func (s *Server) encryptionLevel() int {
	if s.cfg.EncryptionLevel == 0 {
		return 0
	}
	return s.cfg.EncryptionLevel
}

func (s *Server) verifyClientHello(hello *protocol.Hello) error {
	if hello == nil {
		return fmt.Errorf("hello payload required")
	}
	expected := s.cfg.TrustedPeerPublicKeys[hello.NodeID]
	if expected == "" {
		return nil
	}
	return peeridentity.VerifyHello(peeridentity.SignedHello{NodeID: hello.NodeID, PublicKey: hello.PublicKey, SessionPublicKey: hello.SessionPublicKey, EncryptionLevel: hello.EncryptionLevel, Signature: hello.Signature}, expected, []byte(hello.Nonce))
}

func metadataError(err error) *protocol.Error {
	var compacted *state.MetadataCompactedError
	if errors.As(err, &compacted) {
		return &protocol.Error{Code: "metadata_full_refresh_required", Message: compacted.Error()}
	}
	return &protocol.Error{Code: "metadata_state", Message: err.Error()}
}

func (s *Server) serveMetadataState(codec *protocol.Codec, peerID string, remote *protocol.MetadataState) error {
	if remote == nil {
		return fmt.Errorf("metadata state payload required")
	}
	if remote.PeerID != "" {
		peerID = remote.PeerID
	}
	if peerID == "" {
		return fmt.Errorf("metadata peer id required")
	}
	for _, folder := range remote.Folders {
		remoteSummary := state.FolderSummary{FolderID: folder.FolderID, Cursor: folder.Cursor, Files: folder.Files, Tombstones: folder.Tombstones, StateHash: folder.StateHash}
		if err := s.cfg.MetadataStore.SavePeerFolderState(peerID, remoteSummary); err != nil {
			return err
		}
		cursor := folder.Cursor
		for {
			changes, err := s.changesSince(folder.FolderID, cursor)
			if err != nil {
				return err
			}
			msg := metadataChangesToProtocol(changes)
			msg.More = changes.ToCursor < s.localCursor(folder.FolderID)
			if err := codec.Write(protocol.Message{Type: protocol.MessageMetadataChanges, MetadataChanges: &msg}); err != nil {
				return err
			}
			if msg.More {
				keepSending, err := readMetadataAck(codec, msg)
				if err != nil {
					return err
				}
				if !keepSending {
					break
				}
			}
			if !msg.More || changes.ToCursor == cursor {
				break
			}
			cursor = changes.ToCursor
		}
	}
	return nil
}

func (s *Server) serveMeshSettingsChanges(codec *protocol.Codec, peerID string, request *protocol.MeshSettingsChanges) error {
	if request == nil {
		return fmt.Errorf("mesh settings changes payload required")
	}
	if s.cfg.PendingSettingsStore == nil {
		return fmt.Errorf("pending settings store not configured")
	}
	if peerID == "" {
		return fmt.Errorf("mesh settings peer id required")
	}
	if request.TargetNodeID != "" && request.TargetNodeID != peerID {
		return fmt.Errorf("mesh settings target %q does not match authenticated peer %q", request.TargetNodeID, peerID)
	}
	changes, err := s.cfg.PendingSettingsStore.ListPendingSettingsChanges(peerID)
	if err != nil {
		return err
	}
	out := protocol.MeshSettingsChanges{TargetNodeID: peerID, Changes: make([]protocol.MeshSettingsChange, 0, len(changes))}
	for _, change := range changes {
		if change.Status != "queued" {
			continue
		}
		out.Changes = append(out.Changes, protocol.MeshSettingsChange{
			ID:             change.ID,
			TargetNodeID:   change.TargetNodeID,
			OriginNodeID:   change.OriginNodeID,
			IdempotencyKey: change.IdempotencyKey,
			Revision:       change.Revision,
			Status:         change.Status,
			CreatedAt:      change.CreatedAt,
			UpdatedAt:      change.UpdatedAt,
			SettingsPatch:  change.SettingsPatch,
			LastError:      change.LastError,
		})
	}
	return codec.Write(protocol.Message{Type: protocol.MessageMeshSettingsChanges, MeshSettingsChanges: &out})
}

func (s *Server) recordMeshSettingsAck(peerID string, ack *protocol.MeshSettingsAck) error {
	if ack == nil {
		return fmt.Errorf("mesh settings ack payload required")
	}
	if s.cfg.PendingSettingsStore == nil {
		return fmt.Errorf("pending settings store not configured")
	}
	if peerID == "" {
		return fmt.Errorf("mesh settings peer id required")
	}
	if ack.TargetNodeID != peerID {
		return fmt.Errorf("mesh settings ack target %q does not match authenticated peer %q", ack.TargetNodeID, peerID)
	}
	for _, result := range ack.Results {
		if err := s.cfg.PendingSettingsStore.UpdatePendingSettingsChangeStatus(ack.TargetNodeID, result.ID, result.Status, result.UpdatedAt, result.LastError); err != nil {
			return err
		}
	}
	return nil
}

func metadataChangesToProtocol(changes state.FolderChanges) protocol.MetadataChanges {
	out := protocol.MetadataChanges{FolderID: changes.FolderID, FromCursor: changes.FromCursor, ToCursor: changes.ToCursor, StateHash: changes.StateHash, Changes: make([]protocol.MetadataChange, 0, len(changes.Changes))}
	for _, change := range changes.Changes {
		kind := protocol.MetadataChangeUpsert
		if change.Kind == state.ChangeDelete {
			kind = protocol.MetadataChangeDelete
		} else if change.Kind == state.ChangeMove {
			kind = protocol.MetadataChangeMove
		}
		out.Changes = append(out.Changes, protocol.MetadataChange{Kind: kind, FromPath: change.FromPath, Path: change.Path, Revision: change.Revision, Manifest: change.Manifest})
	}
	return out
}

func readMetadataAck(codec *protocol.Codec, changes protocol.MetadataChanges) (bool, error) {
	msg, err := codec.Read()
	if err != nil {
		return false, err
	}
	if msg.Type == protocol.MessageError {
		return false, fmt.Errorf("peer error %s: %s", msg.Error.Code, msg.Error.Message)
	}
	if msg.Type != protocol.MessageMetadataAck || msg.MetadataAck == nil {
		return false, fmt.Errorf("expected metadata ack, got %s", msg.Type)
	}
	want := metadataAckForChanges(changes)
	got := *msg.MetadataAck
	stop := got.Stop
	got.Stop = false
	if got != *want {
		return false, fmt.Errorf("metadata ack mismatch for folder %s cursor %d-%d", changes.FolderID, changes.FromCursor, changes.ToCursor)
	}
	return !stop, nil
}

func metadataAckForChanges(changes protocol.MetadataChanges) *protocol.MetadataAck {
	return &protocol.MetadataAck{FolderID: changes.FolderID, FromCursor: changes.FromCursor, ToCursor: changes.ToCursor, StateHash: changes.StateHash}
}

func (s *Server) changesSince(folderID string, cursor uint64) (state.FolderChanges, error) {
	if s.cfg.MaxMetadataChangesPerMessage > 0 {
		if limited, ok := s.cfg.MetadataStore.(limitedMetadataStore); ok {
			return limited.ChangesSinceLimit(folderID, cursor, s.cfg.MaxMetadataChangesPerMessage)
		}
	}
	return s.cfg.MetadataStore.ChangesSince(folderID, cursor)
}

func (s *Server) localCursor(folderID string) uint64 {
	summary, err := s.cfg.MetadataStore.FolderSummary(folderID)
	if err != nil {
		return 0
	}
	return summary.Cursor
}

type StreamBlockSource struct {
	PeerID         string
	Dial           func(context.Context) (io.ReadWriteCloser, error)
	Path           routing.PathKind
	Network        routing.NetworkKind
	RelayViaPeerID string
	Reachable      bool
	PeerPublicKey  string
}

type PullOptions struct {
	NodeID                               string
	FolderID                             string
	LocalRoot                            string
	Identity                             peeridentity.Identity
	EncryptionLevel                      int
	PeerPublicKey                        string
	AllowWeakerEncryptionLevel           bool
	KnownPeers                           []discovery.Peer
	IdentityGroups                       []discovery.IdentityGroupState
	Transfer                             config.TransferConfig
	Peer                                 config.PeerConfig
	BlockSources                         []StreamBlockSource
	MetadataStore                        PullMetadataStore
	SnapshotStore                        SnapshotStore
	MeshSettingsStore                    MeshSettingsApplyStore
	SnapshotArchiveRoot                  string
	SnapshotCheckpointRoot               string
	MaxMetadataBatchesBeforeFileTransfer int
	AsyncMetadataCatchupDial             func(context.Context) (io.ReadWriteCloser, error)
}

type PullResult struct {
	FilesWritten                   int
	FilesDeleted                   int
	FilesMoved                     int
	BlocksFetched                  int
	BlocksReused                   int
	MissingIgnoreIncludes          []string
	NegotiatedEncryptionLevel      int
	NegotiatedTransfer             config.EffectiveTransferConfig
	NegotiatedTransferSendCause    string
	NegotiatedTransferReceiveCause string
	LearnedPeers                   []discovery.Peer
	LearnedFolders                 []discovery.LearnedFolder
	SnapshotMarkersLearned         int
	MeshSettingsChangesApplied     int
	MetadataChangesApplied         int
	MetadataCatchupStarted         bool
	MetadataFullRefreshes          int
}

func negotiateEncryptionLevel(localLevel, peerLevel int, allowWeaker bool) (int, error) {
	if err := peeridentity.ValidateEncryptionLevel(localLevel); err != nil {
		return 0, err
	}
	if err := peeridentity.ValidateEncryptionLevel(peerLevel); err != nil {
		return 0, err
	}
	if allowWeaker {
		return localLevel, nil
	}
	if peerLevel > localLevel {
		return peerLevel, nil
	}
	return localLevel, nil
}

func signedProtocolHello(nodeID string, identity peeridentity.Identity, encryptionLevel int, nonce string, transfer *protocol.TransferLimits) (*protocol.Hello, error) {
	hello, _, err := signedSessionProtocolHello(nodeID, identity, encryptionLevel, nonce, transfer)
	return hello, err
}

func signedSessionProtocolHello(nodeID string, identity peeridentity.Identity, encryptionLevel int, nonce string, transfer *protocol.TransferLimits) (*protocol.Hello, []byte, error) {
	var sessionPrivate []byte
	var sessionPublic string
	if encryptionLevel > 0 {
		priv := make([]byte, curve25519.ScalarSize)
		if _, err := rand.Read(priv); err != nil {
			return nil, nil, err
		}
		pub, err := curve25519.X25519(priv, curve25519.Basepoint)
		if err != nil {
			return nil, nil, err
		}
		sessionPrivate = priv
		sessionPublic = base64.StdEncoding.EncodeToString(pub)
	}
	if encryptionLevel == 0 && identity.PrivateKey == "" {
		return &protocol.Hello{ProtocolVersion: 1, NodeID: nodeID, EncryptionLevel: 0, Transfer: transfer, Nonce: nonce}, sessionPrivate, nil
	}
	if identity.PrivateKey == "" {
		return &protocol.Hello{ProtocolVersion: 1, NodeID: nodeID, PublicKey: identity.PublicKey, SessionPublicKey: sessionPublic, EncryptionLevel: encryptionLevel, Transfer: transfer, Nonce: nonce}, sessionPrivate, nil
	}
	signed, err := peeridentity.SignSessionHello(identity, nodeID, encryptionLevel, sessionPublic, []byte(nonce))
	if err != nil {
		return nil, nil, err
	}
	return &protocol.Hello{ProtocolVersion: 1, NodeID: signed.NodeID, PublicKey: signed.PublicKey, SessionPublicKey: signed.SessionPublicKey, EncryptionLevel: signed.EncryptionLevel, Transfer: transfer, Nonce: nonce, Signature: signed.Signature}, sessionPrivate, nil
}

func advertisedTransferLimits(global config.TransferConfig, peer config.PeerConfig) *protocol.TransferLimits {
	local := config.EffectiveTransferLimits(global, peer, config.TransferConfig{}, config.PeerConfig{})
	return &protocol.TransferLimits{SendBytesPerSecond: local.SendBytesPerSecond, ReceiveBytesPerSecond: local.ReceiveBytesPerSecond}
}

func negotiatedTransferLimits(localGlobal config.TransferConfig, localPeer config.PeerConfig, remote *protocol.TransferLimits) config.EffectiveTransferConfig {
	return negotiatedTransferLimitDetails(localGlobal, localPeer, remote).Effective
}

func negotiatedTransferLimitDetails(localGlobal config.TransferConfig, localPeer config.PeerConfig, remote *protocol.TransferLimits) config.TransferLimitDetails {
	if remote == nil {
		return config.EffectiveTransferLimitDetails(localGlobal, localPeer, config.TransferConfig{}, config.PeerConfig{})
	}
	remoteGlobal := config.TransferConfig{SendBytesPerSecond: remote.SendBytesPerSecond, ReceiveBytesPerSecond: remote.ReceiveBytesPerSecond}
	return config.EffectiveTransferLimitDetails(localGlobal, localPeer, remoteGlobal, config.PeerConfig{})
}

func verifyServerHello(hello *protocol.Hello, expectedPublicKey string, nonce string) error {
	if expectedPublicKey == "" {
		return nil
	}
	if hello == nil {
		return fmt.Errorf("hello payload required")
	}
	return peeridentity.VerifyHello(peeridentity.SignedHello{NodeID: hello.NodeID, PublicKey: hello.PublicKey, SessionPublicKey: hello.SessionPublicKey, EncryptionLevel: hello.EncryptionLevel, Signature: hello.Signature}, expectedPublicKey, []byte(nonce))
}

func secureCodec(stream io.ReadWriter, localSessionPrivate []byte, clientHello *protocol.Hello, serverHello *protocol.Hello, clientSide bool) (*protocol.Codec, error) {
	level := 0
	if serverHello != nil {
		level = serverHello.EncryptionLevel
	}
	if level == 0 {
		return protocol.NewCodec(stream, stream), nil
	}
	if len(localSessionPrivate) != curve25519.ScalarSize {
		return nil, fmt.Errorf("local stream session key required for encrypted level %d", level)
	}
	remoteSessionPublic := ""
	if clientSide {
		remoteSessionPublic = serverHello.SessionPublicKey
	} else if clientHello != nil {
		remoteSessionPublic = clientHello.SessionPublicKey
	}
	remotePublic, err := base64.StdEncoding.DecodeString(remoteSessionPublic)
	if err != nil || len(remotePublic) != curve25519.PointSize {
		return nil, fmt.Errorf("peer stream session public key required for encrypted level %d", level)
	}
	shared, err := curve25519.X25519(localSessionPrivate, remotePublic)
	if err != nil {
		return nil, err
	}
	clientNonce := ""
	serverNonce := ""
	clientID := ""
	serverID := ""
	if clientHello != nil {
		clientNonce = clientHello.Nonce
		clientID = clientHello.NodeID
	}
	if serverHello != nil {
		serverNonce = serverHello.Nonce
		serverID = serverHello.NodeID
	}
	salt := []byte("fse-stream-aead-v1|" + clientNonce + "|" + serverNonce + "|" + clientID + "|" + serverID)
	reader := hkdf.New(sha256.New, shared, salt, []byte("stream payload encryption"))
	keyClientToServer := make([]byte, chacha20poly1305.KeySize)
	keyServerToClient := make([]byte, chacha20poly1305.KeySize)
	clientNoncePrefix := make([]byte, chacha20poly1305.NonceSizeX-8)
	serverNoncePrefix := make([]byte, chacha20poly1305.NonceSizeX-8)
	if _, err := io.ReadFull(reader, keyClientToServer); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(reader, keyServerToClient); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(reader, clientNoncePrefix); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(reader, serverNoncePrefix); err != nil {
		return nil, err
	}
	clientAEAD, err := chacha20poly1305.NewX(keyClientToServer)
	if err != nil {
		return nil, err
	}
	serverAEAD, err := chacha20poly1305.NewX(keyServerToClient)
	if err != nil {
		return nil, err
	}
	if clientSide {
		return protocol.NewEncryptedCodec(stream, stream, clientAEAD, serverAEAD, clientNoncePrefix, serverNoncePrefix), nil
	}
	return protocol.NewEncryptedCodec(stream, stream, serverAEAD, clientAEAD, serverNoncePrefix, clientNoncePrefix), nil
}

func newNonce() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

type localBlock struct {
	Path  string
	Block scannerBlock
}

type scannerBlock struct {
	Index  int
	Offset int64
	Size   int
	Hash   []byte
}

func PullFolder(ctx context.Context, stream io.ReadWriter, opts PullOptions) (PullResult, error) {
	codec := protocol.NewCodec(stream, stream)
	nonce, err := newNonce()
	if err != nil {
		return PullResult{}, err
	}
	hello, sessionPrivate, err := signedSessionProtocolHello(opts.NodeID, opts.Identity, opts.EncryptionLevel, nonce, advertisedTransferLimits(opts.Transfer, opts.Peer))
	if err != nil {
		return PullResult{}, err
	}
	hello.Capabilities = []string{"folder-index", "block-transfer"}
	if err := codec.Write(protocol.Message{Type: protocol.MessageHello, Hello: hello}); err != nil {
		return PullResult{}, err
	}
	if msg, err := codec.Read(); err != nil {
		return PullResult{}, err
	} else if msg.Type == protocol.MessageError {
		return PullResult{}, fmt.Errorf("peer error %s: %s", msg.Error.Code, msg.Error.Message)
	} else if msg.Type != protocol.MessageHello {
		return PullResult{}, fmt.Errorf("expected hello, got %s", msg.Type)
	} else if err := verifyServerHello(msg.Hello, opts.PeerPublicKey, nonce); err != nil {
		return PullResult{}, err
	} else if msg.Hello.EncryptionLevel < opts.EncryptionLevel && !opts.AllowWeakerEncryptionLevel {
		return PullResult{}, fmt.Errorf("peer negotiated weaker encryption level %d below local level %d", msg.Hello.EncryptionLevel, opts.EncryptionLevel)
	} else if err := peeridentity.ValidateEncryptionLevel(msg.Hello.EncryptionLevel); err != nil {
		return PullResult{}, err
	} else {
		if msg.Hello.EncryptionLevel > 0 {
			secure, err := secureCodec(stream, sessionPrivate, hello, msg.Hello, true)
			if err != nil {
				return PullResult{}, err
			}
			codec = secure
		}
		transferDetails := negotiatedTransferLimitDetails(opts.Transfer, opts.Peer, msg.Hello.Transfer)
		result := PullResult{
			NegotiatedEncryptionLevel:      msg.Hello.EncryptionLevel,
			NegotiatedTransfer:             transferDetails.Effective,
			NegotiatedTransferSendCause:    transferDetails.SendCause,
			NegotiatedTransferReceiveCause: transferDetails.ReceiveCause,
		}
		receiveLimiter := ratelimit.NewLimiter(result.NegotiatedTransfer.ReceiveBytesPerSecond)
		if len(opts.KnownPeers) > 0 || len(opts.IdentityGroups) > 0 {
			if err := codec.Write(protocol.Message{Type: protocol.MessagePeerExchange, PeerExchange: &protocol.PeerExchange{Peers: peersToProtocol(opts.KnownPeers), IdentityGroups: groupsToProtocol(identityGroupsWithSnapshots(opts.IdentityGroups, opts.SnapshotStore, opts.SnapshotArchiveRoot, opts.SnapshotCheckpointRoot))}}); err != nil {
				return PullResult{}, err
			}
			peerMsg, err := codec.Read()
			if err != nil {
				return PullResult{}, err
			}
			if peerMsg.Type == protocol.MessageError {
				return PullResult{}, fmt.Errorf("peer error %s: %s", peerMsg.Error.Code, peerMsg.Error.Message)
			}
			if peerMsg.Type != protocol.MessagePeerExchange || peerMsg.PeerExchange == nil {
				return PullResult{}, fmt.Errorf("expected peer exchange, got %s", peerMsg.Type)
			}
			remote := discovery.Peer{ID: msg.Hello.NodeID}
			plan := discovery.PlanPeerExchange(opts.NodeID, opts.KnownPeers, remote, peersFromProtocol(peerMsg.PeerExchange.Peers))
			result.LearnedPeers = plan.Learned
			remoteGroups := groupsFromProtocol(peerMsg.PeerExchange.IdentityGroups)
			result.LearnedFolders = planIdentityGroupLearnedFolders(opts.NodeID, opts.IdentityGroups, remote.ID, remoteGroups)
			learnedSnapshots, err := saveIdentitySnapshotMarkers(opts.SnapshotStore, opts.IdentityGroups, remoteGroups)
			if err != nil {
				return PullResult{}, err
			}
			result.SnapshotMarkersLearned = learnedSnapshots
		}
		if opts.MeshSettingsStore != nil {
			applied, err := exchangeMeshSettingsChanges(codec, opts, msg.Hello.NodeID)
			if err != nil {
				return PullResult{}, err
			}
			result.MeshSettingsChangesApplied = applied
		}
		metadataCurrent := true
		if opts.MetadataStore != nil {
			applied, stopped, fullRefresh, err := exchangeMetadataState(codec, opts, msg.Hello.NodeID)
			if err != nil {
				return PullResult{}, err
			}
			result.MetadataChangesApplied = applied
			metadataCurrent = !stopped
			if fullRefresh {
				result.MetadataFullRefreshes = 1
				metadataCurrent = true
			}
			if stopped && opts.AsyncMetadataCatchupDial != nil {
				result.MetadataCatchupStarted = true
				startAsyncMetadataCatchup(ctx, opts, msg.Hello.NodeID)
			}
		}
		return pullFolderAfterHandshake(ctx, codec, opts, result, msg.Hello.NodeID, metadataCurrent, receiveLimiter)
	}
}

func ExchangeMeshSettings(ctx context.Context, stream io.ReadWriter, opts PullOptions) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}
	if opts.MeshSettingsStore == nil {
		return 0, fmt.Errorf("mesh settings store is required")
	}
	codec := protocol.NewCodec(stream, stream)
	nonce, err := newNonce()
	if err != nil {
		return 0, err
	}
	hello, err := signedProtocolHello(opts.NodeID, opts.Identity, opts.EncryptionLevel, nonce, advertisedTransferLimits(opts.Transfer, opts.Peer))
	if err != nil {
		return 0, err
	}
	hello.Capabilities = []string{"mesh-settings"}
	if err := codec.Write(protocol.Message{Type: protocol.MessageHello, Hello: hello}); err != nil {
		return 0, err
	}
	msg, err := codec.Read()
	if err != nil {
		return 0, err
	}
	if msg.Type == protocol.MessageError {
		return 0, fmt.Errorf("peer error %s: %s", msg.Error.Code, msg.Error.Message)
	}
	if msg.Type != protocol.MessageHello {
		return 0, fmt.Errorf("expected hello, got %s", msg.Type)
	}
	if err := verifyServerHello(msg.Hello, opts.PeerPublicKey, nonce); err != nil {
		return 0, err
	}
	if msg.Hello.EncryptionLevel < opts.EncryptionLevel && !opts.AllowWeakerEncryptionLevel {
		return 0, fmt.Errorf("peer negotiated weaker encryption level %d below local level %d", msg.Hello.EncryptionLevel, opts.EncryptionLevel)
	}
	if err := peeridentity.ValidateEncryptionLevel(msg.Hello.EncryptionLevel); err != nil {
		return 0, err
	}
	return exchangeMeshSettingsChanges(codec, opts, msg.Hello.NodeID)
}

func exchangeMeshSettingsChanges(codec *protocol.Codec, opts PullOptions, peerID string) (int, error) {
	if opts.NodeID == "" {
		return 0, fmt.Errorf("local node id is required for mesh settings exchange")
	}
	if err := codec.Write(protocol.Message{Type: protocol.MessageMeshSettingsChanges, MeshSettingsChanges: &protocol.MeshSettingsChanges{TargetNodeID: opts.NodeID}}); err != nil {
		return 0, err
	}
	msg, err := codec.Read()
	if err != nil {
		return 0, err
	}
	if msg.Type == protocol.MessageError {
		return 0, fmt.Errorf("peer error %s: %s", msg.Error.Code, msg.Error.Message)
	}
	if msg.Type != protocol.MessageMeshSettingsChanges || msg.MeshSettingsChanges == nil {
		return 0, fmt.Errorf("expected mesh settings changes, got %s", msg.Type)
	}
	if msg.MeshSettingsChanges.TargetNodeID != opts.NodeID {
		return 0, fmt.Errorf("mesh settings target %q does not match local node %q", msg.MeshSettingsChanges.TargetNodeID, opts.NodeID)
	}
	applied := 0
	ack := protocol.MeshSettingsAck{TargetNodeID: opts.NodeID, Results: make([]protocol.MeshSettingsAckResult, 0, len(msg.MeshSettingsChanges.Changes))}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, incoming := range msg.MeshSettingsChanges.Changes {
		if incoming.TargetNodeID != opts.NodeID {
			return 0, fmt.Errorf("mesh settings change %q targets %q, want %q", incoming.ID, incoming.TargetNodeID, opts.NodeID)
		}
		change := state.PendingSettingsChange{
			ID:             incoming.ID,
			TargetNodeID:   incoming.TargetNodeID,
			OriginNodeID:   incoming.OriginNodeID,
			IdempotencyKey: incoming.IdempotencyKey,
			Revision:       incoming.Revision,
			Status:         incoming.Status,
			CreatedAt:      incoming.CreatedAt,
			UpdatedAt:      incoming.UpdatedAt,
			SettingsPatch:  incoming.SettingsPatch,
			LastError:      incoming.LastError,
		}
		if change.Status == "" {
			change.Status = "delivered"
		}
		if err := opts.MeshSettingsStore.SavePendingSettingsChange(opts.NodeID, change); err != nil {
			return 0, err
		}
		if _, appliedChange, err := opts.MeshSettingsStore.ApplyPendingSettingsChange(opts.NodeID, change.ID, now); err != nil {
			ack.Results = append(ack.Results, protocol.MeshSettingsAckResult{ID: change.ID, Status: "failed", UpdatedAt: now, LastError: err.Error()})
			if writeErr := codec.Write(protocol.Message{Type: protocol.MessageMeshSettingsAck, MeshSettingsAck: &ack}); writeErr != nil {
				return 0, writeErr
			}
			return 0, err
		} else if appliedChange.Status == "applied" {
			ack.Results = append(ack.Results, protocol.MeshSettingsAckResult{ID: change.ID, Status: "acked", UpdatedAt: appliedChange.UpdatedAt})
			applied++
		}
	}
	if len(ack.Results) > 0 {
		if err := codec.Write(protocol.Message{Type: protocol.MessageMeshSettingsAck, MeshSettingsAck: &ack}); err != nil {
			return 0, err
		}
	}
	_ = peerID
	return applied, nil
}

func exchangeMetadataState(codec *protocol.Codec, opts PullOptions, peerID string) (int, bool, bool, error) {
	summary, err := metadataSummaryForPeer(opts.MetadataStore, peerID, opts.FolderID)
	if err != nil {
		return 0, false, false, err
	}
	stateMsg := &protocol.MetadataState{PeerID: opts.NodeID, Folders: []protocol.MetadataFolderSummary{summaryToProtocol(summary)}}
	if err := codec.Write(protocol.Message{Type: protocol.MessageMetadataState, MetadataState: stateMsg}); err != nil {
		return 0, false, false, err
	}
	applied := 0
	batches := 0
	for {
		msg, err := codec.Read()
		if err != nil {
			return 0, false, false, err
		}
		if msg.Type == protocol.MessageError {
			if msg.Error != nil && msg.Error.Code == "metadata_full_refresh_required" {
				return applied, false, true, nil
			}
			return 0, false, false, fmt.Errorf("peer error %s: %s", msg.Error.Code, msg.Error.Message)
		}
		if msg.Type != protocol.MessageMetadataChanges || msg.MetadataChanges == nil {
			return 0, false, false, fmt.Errorf("expected metadata changes, got %s", msg.Type)
		}
		changes := protocolChangesToState(*msg.MetadataChanges)
		if err := opts.MetadataStore.ApplyPeerFolderChanges(peerID, changes); err != nil {
			return 0, false, false, err
		}
		batches++
		applied += len(changes.Changes)
		if msg.MetadataChanges.More {
			ack := metadataAckForChanges(*msg.MetadataChanges)
			if opts.MaxMetadataBatchesBeforeFileTransfer > 0 && batches >= opts.MaxMetadataBatchesBeforeFileTransfer {
				ack.Stop = true
			}
			if err := codec.Write(protocol.Message{Type: protocol.MessageMetadataAck, MetadataAck: ack}); err != nil {
				return 0, false, false, err
			}
			if ack.Stop {
				return applied, true, false, nil
			}
			continue
		}
		return applied, false, false, nil
	}
}

func startAsyncMetadataCatchup(ctx context.Context, opts PullOptions, peerID string) {
	go func() {
		stream, err := opts.AsyncMetadataCatchupDial(ctx)
		if err != nil {
			return
		}
		defer stream.Close()
		catchupOpts := opts
		catchupOpts.MaxMetadataBatchesBeforeFileTransfer = 0
		catchupOpts.AsyncMetadataCatchupDial = nil
		_ = metadataCatchupOnly(ctx, stream, catchupOpts, peerID)
	}()
}

func MetadataCatchupOnly(ctx context.Context, stream io.ReadWriter, opts PullOptions, peerID string) (PullResult, error) {
	codec := protocol.NewCodec(stream, stream)
	nonce, err := newNonce()
	if err != nil {
		return PullResult{}, err
	}
	hello, err := signedProtocolHello(opts.NodeID, opts.Identity, opts.EncryptionLevel, nonce, advertisedTransferLimits(opts.Transfer, opts.Peer))
	if err != nil {
		return PullResult{}, err
	}
	hello.Capabilities = []string{"metadata-state"}
	if err := codec.Write(protocol.Message{Type: protocol.MessageHello, Hello: hello}); err != nil {
		return PullResult{}, err
	}
	msg, err := codec.Read()
	if err != nil {
		return PullResult{}, err
	}
	if msg.Type == protocol.MessageError {
		return PullResult{}, fmt.Errorf("peer error %s: %s", msg.Error.Code, msg.Error.Message)
	}
	if msg.Type != protocol.MessageHello {
		return PullResult{}, fmt.Errorf("expected hello, got %s", msg.Type)
	}
	if err := verifyServerHello(msg.Hello, opts.PeerPublicKey, nonce); err != nil {
		return PullResult{}, err
	}
	select {
	case <-ctx.Done():
		return PullResult{}, ctx.Err()
	default:
	}
	applied, stopped, fullRefresh, err := exchangeMetadataState(codec, opts, peerID)
	if err != nil {
		return PullResult{}, err
	}
	result := PullResult{MetadataChangesApplied: applied}
	if stopped {
		result.MetadataCatchupStarted = true
	}
	if fullRefresh {
		result.MetadataFullRefreshes = 1
	}
	return result, nil
}

func metadataCatchupOnly(ctx context.Context, stream io.ReadWriter, opts PullOptions, peerID string) error {
	_, err := MetadataCatchupOnly(ctx, stream, opts, peerID)
	return err
}

func metadataSummaryForPeer(store PullMetadataStore, peerID string, folderID string) (state.FolderSummary, error) {
	vector, err := store.PeerStateVector(peerID)
	if err != nil {
		return state.FolderSummary{}, err
	}
	for _, summary := range vector.Folders {
		if summary.FolderID == folderID {
			return summary, nil
		}
	}
	return store.FolderSummary(folderID)
}

func summaryToProtocol(summary state.FolderSummary) protocol.MetadataFolderSummary {
	return protocol.MetadataFolderSummary{FolderID: summary.FolderID, Cursor: summary.Cursor, Files: summary.Files, Tombstones: summary.Tombstones, StateHash: summary.StateHash}
}

func protocolChangesToState(changes protocol.MetadataChanges) state.FolderChanges {
	out := state.FolderChanges{FolderID: changes.FolderID, FromCursor: changes.FromCursor, ToCursor: changes.ToCursor, StateHash: changes.StateHash, Changes: make([]state.FolderChange, 0, len(changes.Changes))}
	for _, change := range changes.Changes {
		kind := state.ChangeUpsert
		if change.Kind == protocol.MetadataChangeDelete {
			kind = state.ChangeDelete
		} else if change.Kind == protocol.MetadataChangeMove {
			kind = state.ChangeMove
		}
		out.Changes = append(out.Changes, state.FolderChange{Kind: kind, FromPath: change.FromPath, Path: change.Path, Revision: change.Revision, Manifest: change.Manifest})
	}
	return out
}

func applyFullRefreshIndex(store PullMetadataStore, peerID string, index *protocol.FolderIndex) error {
	refreshStore, ok := store.(fullRefreshMetadataStore)
	if !ok {
		return fmt.Errorf("metadata full refresh required but store cannot replace peer folder cache")
	}
	if index.MetadataSummary == nil {
		return fmt.Errorf("metadata full refresh required but peer folder index has no metadata summary")
	}
	manifests := make(map[string]block.Manifest, len(index.Files))
	revisions := make(map[string]uint64, len(index.Files))
	for _, file := range index.Files {
		manifest := file.Manifest
		manifest.Path = file.RelativePath
		manifests[file.RelativePath] = manifest
		revisions[file.RelativePath] = file.Revision
	}
	summary := state.FolderSummary{FolderID: index.MetadataSummary.FolderID, Cursor: index.MetadataSummary.Cursor, Files: index.MetadataSummary.Files, Tombstones: index.MetadataSummary.Tombstones, StateHash: index.MetadataSummary.StateHash}
	return refreshStore.ReplacePeerFolderFromFullRefresh(peerID, index.FolderID, summary, manifests, revisions)
}

func peersToProtocol(peers []discovery.Peer) []protocol.PeerInfo {
	out := make([]protocol.PeerInfo, 0, len(peers))
	for _, peer := range peers {
		out = append(out, protocol.PeerInfo{ID: peer.ID, Addresses: append([]string(nil), peer.Addresses...)})
	}
	return out
}

func peersFromProtocol(peers []protocol.PeerInfo) []discovery.Peer {
	out := make([]discovery.Peer, 0, len(peers))
	for _, peer := range peers {
		out = append(out, discovery.Peer{ID: peer.ID, Addresses: append([]string(nil), peer.Addresses...)})
	}
	return out
}

func groupsToProtocol(groups []discovery.IdentityGroupState) []protocol.IdentityGroupAdvertisement {
	out := make([]protocol.IdentityGroupAdvertisement, 0, len(groups))
	for _, group := range groups {
		folders := make([]protocol.FolderAdvertisement, 0, len(group.SharedFolders))
		for _, folder := range group.SharedFolders {
			folders = append(folders, protocol.FolderAdvertisement{ID: folder.ID, Label: folder.Label, Snapshots: snapshotsToProtocol(folder.Snapshots)})
		}
		out = append(out, protocol.IdentityGroupAdvertisement{GroupID: group.GroupID, Folders: folders})
	}
	return out
}

func groupsFromProtocol(groups []protocol.IdentityGroupAdvertisement) []discovery.IdentityGroupState {
	out := make([]discovery.IdentityGroupState, 0, len(groups))
	for _, group := range groups {
		folders := make([]discovery.FolderAdvertisement, 0, len(group.Folders))
		for _, folder := range group.Folders {
			folders = append(folders, discovery.FolderAdvertisement{ID: folder.ID, Label: folder.Label, Snapshots: snapshotsFromProtocol(folder.Snapshots)})
		}
		out = append(out, discovery.IdentityGroupState{GroupID: group.GroupID, SharedFolders: folders})
	}
	return out
}

func snapshotsToProtocol(markers []discovery.SnapshotMarker) []protocol.SnapshotMarker {
	out := make([]protocol.SnapshotMarker, 0, len(markers))
	for _, marker := range markers {
		out = append(out, protocol.SnapshotMarker{ID: marker.ID, FolderID: marker.FolderID, Cursor: marker.Cursor, StateHash: marker.StateHash, CreatedAt: marker.CreatedAt, Description: marker.Description, Pinned: marker.Pinned, Deprecated: marker.Deprecated, ArchiveFullyProtected: marker.ArchiveFullyProtected, DBCheckpointAvailable: marker.DBCheckpointAvailable})
	}
	return out
}

func snapshotsFromProtocol(markers []protocol.SnapshotMarker) []discovery.SnapshotMarker {
	out := make([]discovery.SnapshotMarker, 0, len(markers))
	for _, marker := range markers {
		out = append(out, discovery.SnapshotMarker{ID: marker.ID, FolderID: marker.FolderID, Cursor: marker.Cursor, StateHash: marker.StateHash, CreatedAt: marker.CreatedAt, Description: marker.Description, Pinned: marker.Pinned, Deprecated: marker.Deprecated, ArchiveFullyProtected: marker.ArchiveFullyProtected, DBCheckpointAvailable: marker.DBCheckpointAvailable})
	}
	return out
}

func identityGroupsWithSnapshots(groups []discovery.IdentityGroupState, store SnapshotStore, archiveRoot string, checkpointRoot string) []discovery.IdentityGroupState {
	if store == nil {
		return groups
	}
	archiveBySnapshot := map[string]backup.ArchiveProtectionSnapshotStatus{}
	if archiveRoot != "" {
		if jobs, err := store.ListArchiveIntakeJobs(""); err == nil {
			archiveBySnapshot = backup.ComputeArchiveProtectionStatusFromJobs(archiveRoot, jobs).Snapshots
		}
	}
	out := make([]discovery.IdentityGroupState, 0, len(groups))
	for _, group := range groups {
		copyGroup := discovery.IdentityGroupState{GroupID: group.GroupID, KnownPeers: append([]discovery.Peer(nil), group.KnownPeers...), SharedFolders: make([]discovery.FolderAdvertisement, 0, len(group.SharedFolders))}
		for _, folder := range group.SharedFolders {
			copyFolder := discovery.FolderAdvertisement{ID: folder.ID, Label: folder.Label, Snapshots: append([]discovery.SnapshotMarker(nil), folder.Snapshots...)}
			markers, err := store.ListSnapshotMarkers(folder.ID)
			if err == nil {
				for _, marker := range markers {
					archiveStatus := archiveBySnapshot[marker.ID]
					copyFolder.Snapshots = append(copyFolder.Snapshots, discovery.SnapshotMarker{ID: marker.ID, FolderID: marker.FolderID, Cursor: marker.Cursor, StateHash: marker.StateHash, CreatedAt: marker.CreatedAt, Description: marker.Description, Pinned: marker.Pinned, Deprecated: marker.Deprecated, ArchiveFullyProtected: marker.ArchiveFullyProtected || (archiveStatus.TotalBlocks > 0 && archiveStatus.ProtectedBlocks == archiveStatus.TotalBlocks), DBCheckpointAvailable: marker.DBCheckpointAvailable || snapshotCheckpointAvailable(checkpointRoot, marker)})
				}
			}
			copyGroup.SharedFolders = append(copyGroup.SharedFolders, copyFolder)
		}
		out = append(out, copyGroup)
	}
	return out
}

func snapshotCheckpointAvailable(root string, marker state.SnapshotMarker) bool {
	if root == "" || marker.FolderID == "" || marker.ID == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(root, marker.FolderID, marker.ID+".json"))
	return err == nil && !info.IsDir()
}

func saveIdentitySnapshotMarkers(store SnapshotStore, localGroups []discovery.IdentityGroupState, remoteGroups []discovery.IdentityGroupState) (int, error) {
	if store == nil {
		return 0, nil
	}
	learned := 0
	for _, local := range localGroups {
		for _, remote := range remoteGroups {
			if local.GroupID == "" || local.GroupID != remote.GroupID {
				continue
			}
			localFolders := map[string]struct{}{}
			for _, folder := range local.SharedFolders {
				localFolders[folder.ID] = struct{}{}
			}
			for _, folder := range remote.SharedFolders {
				if _, ok := localFolders[folder.ID]; !ok {
					continue
				}
				for _, marker := range folder.Snapshots {
					if marker.ID == "" || marker.FolderID == "" {
						continue
					}
					if err := store.SaveSnapshotMarker(state.SnapshotMarker{ID: marker.ID, FolderID: marker.FolderID, Cursor: marker.Cursor, StateHash: marker.StateHash, CreatedAt: marker.CreatedAt, Description: marker.Description, Pinned: marker.Pinned, Deprecated: marker.Deprecated, ArchiveFullyProtected: marker.ArchiveFullyProtected, DBCheckpointAvailable: marker.DBCheckpointAvailable}); err != nil {
						return learned, err
					}
					learned++
				}
			}
		}
	}
	return learned, nil
}

func planIdentityGroupResponses(selfID string, localGroups []discovery.IdentityGroupState, remoteID string, remoteGroups []discovery.IdentityGroupState) []discovery.IdentityGroupState {
	responses := make([]discovery.IdentityGroupState, 0, len(localGroups))
	for _, local := range localGroups {
		for _, remote := range remoteGroups {
			plan := discovery.PlanIdentityGroupExchange(selfID, local, remoteID, remote)
			if len(plan.ShareFolders) == 0 {
				continue
			}
			responses = append(responses, discovery.IdentityGroupState{GroupID: local.GroupID, SharedFolders: plan.ShareFolders})
		}
	}
	return responses
}

func planIdentityGroupLearnedFolders(selfID string, localGroups []discovery.IdentityGroupState, remoteID string, remoteGroups []discovery.IdentityGroupState) []discovery.LearnedFolder {
	var learned []discovery.LearnedFolder
	for _, local := range localGroups {
		for _, remote := range remoteGroups {
			plan := discovery.PlanIdentityGroupExchange(selfID, local, remoteID, remote)
			learned = append(learned, plan.LearnedFolders...)
		}
	}
	return learned
}

func pullFolderAfterHandshake(ctx context.Context, codec *protocol.Codec, opts PullOptions, result PullResult, peerID string, metadataCurrent bool, receiveLimiter *ratelimit.Limiter) (PullResult, error) {
	missingIncludes, err := fetchMissingInShareIgnoreIncludes(ctx, codec, opts, receiveLimiter)
	if err != nil {
		return result, err
	}
	result.MissingIgnoreIncludes = missingIncludes
	if err := codec.Write(protocol.Message{Type: protocol.MessageFolderIndex, FolderIndex: &protocol.FolderIndex{FolderID: opts.FolderID}}); err != nil {
		return PullResult{}, err
	}
	msg, err := codec.Read()
	if err != nil {
		return PullResult{}, err
	}
	if msg.Type == protocol.MessageError {
		return PullResult{}, fmt.Errorf("peer error %s: %s", msg.Error.Code, msg.Error.Message)
	}
	if msg.Type != protocol.MessageFolderIndex || msg.FolderIndex == nil {
		return PullResult{}, fmt.Errorf("expected folder index, got %s", msg.Type)
	}
	if result.MetadataFullRefreshes > 0 {
		if err := applyFullRefreshIndex(opts.MetadataStore, peerID, msg.FolderIndex); err != nil {
			return result, err
		}
	}
	if _, err := cleanNonResumableTemps(opts.LocalRoot, msg.FolderIndex.Files); err != nil {
		return result, err
	}
	moved, err := moveMatchingStaleLocalFiles(opts.LocalRoot, msg.FolderIndex.Files, blockSizeFromIndex(msg.FolderIndex.Files))
	if err != nil {
		return result, err
	}
	result.FilesMoved += moved
	localBlocks, err := scanLocalBlocks(opts.LocalRoot, blockSizeFromIndex(msg.FolderIndex.Files))
	if err != nil {
		return result, err
	}
	seen := make(map[string]struct{}, len(msg.FolderIndex.Files))
	for _, file := range msg.FolderIndex.Files {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		seen[file.RelativePath] = struct{}{}
		finalPath, err := safePath(opts.LocalRoot, file.RelativePath)
		if err != nil {
			return result, err
		}
		matches, err := localFileMatchesManifest(finalPath, file.Manifest)
		if err != nil {
			return result, err
		}
		if matches {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(finalPath), 0o700); err != nil {
			return result, err
		}
		tmpPath := finalPath + ".fse-stream-tmp"
		out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return result, err
		}
		if err := out.Truncate(file.Manifest.Size); err != nil {
			out.Close()
			os.Remove(tmpPath)
			return result, err
		}
		ok := false
		for _, want := range file.Manifest.Blocks {
			resumed, err := hasVerifiedTempBlock(out, want.Offset, want.Size, want.Hash)
			if err != nil {
				out.Close()
				os.Remove(tmpPath)
				return result, err
			}
			if resumed {
				continue
			}
			if source, ok := localBlocks[blockKey(want.Size, want.Hash)]; ok {
				data, err := readLocalBlock(source)
				if err != nil {
					out.Close()
					os.Remove(tmpPath)
					return result, err
				}
				hash := sha256.Sum256(data)
				if len(data) != want.Size || !bytes.Equal(hash[:], want.Hash) {
					out.Close()
					os.Remove(tmpPath)
					return result, fmt.Errorf("local block verification failed for %s block %d", file.RelativePath, want.Index)
				}
				if _, err := out.Seek(want.Offset, io.SeekStart); err != nil {
					out.Close()
					os.Remove(tmpPath)
					return result, err
				}
				if _, err := out.Write(data); err != nil {
					out.Close()
					os.Remove(tmpPath)
					return result, err
				}
				result.BlocksReused++
				continue
			}
			data, err := fetchStreamBlock(ctx, codec, opts, file.RelativePath, want.Index, file.Manifest.BlockSize, receiveLimiter)
			if err != nil {
				out.Close()
				os.Remove(tmpPath)
				return result, err
			}
			hash := sha256.Sum256(data)
			if len(data) != want.Size || !bytes.Equal(hash[:], want.Hash) {
				out.Close()
				os.Remove(tmpPath)
				return result, fmt.Errorf("block verification failed for %s block %d", file.RelativePath, want.Index)
			}
			if _, err := out.Seek(want.Offset, io.SeekStart); err != nil {
				out.Close()
				os.Remove(tmpPath)
				return result, err
			}
			if _, err := out.Write(data); err != nil {
				out.Close()
				os.Remove(tmpPath)
				return result, err
			}
			result.BlocksFetched++
		}
		if err := out.Close(); err != nil {
			os.Remove(tmpPath)
			return result, err
		}
		if _, _, err := backup.RetainExistingBackupIntakeFile(opts.LocalRoot, file.RelativePath, time.Time{}); err != nil {
			os.Remove(tmpPath)
			return result, err
		}
		if err := os.Rename(tmpPath, finalPath); err != nil {
			os.Remove(tmpPath)
			return result, err
		}
		ok = true
		if ok {
			result.FilesWritten++
		}
	}
	if !metadataCurrent {
		if err := deferStaleDeletes(opts, msg.FolderIndex, seen); err != nil {
			return result, err
		}
		return result, nil
	}
	deleted, err := deleteStale(opts.LocalRoot, seen)
	if err != nil {
		return result, err
	}
	result.FilesDeleted = deleted
	return result, nil
}

func fetchStreamBlock(ctx context.Context, primary *protocol.Codec, opts PullOptions, rel string, index int, blockSize int, receiveLimiter *ratelimit.Limiter) ([]byte, error) {
	source := chooseStreamBlockSource(opts.BlockSources, rel, index)
	if source.Dial == nil {
		return requestStreamBlock(ctx, primary, opts.FolderID, rel, index, blockSize, receiveLimiter)
	}
	stream, err := source.Dial(ctx)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	codec := protocol.NewCodec(stream, stream)
	if err := streamBlockSourceHandshake(codec, opts, source.PeerPublicKey); err != nil {
		return nil, err
	}
	return requestStreamBlock(ctx, codec, opts.FolderID, rel, index, blockSize, receiveLimiter)
}

func chooseStreamBlockSource(sources []StreamBlockSource, rel string, index int) StreamBlockSource {
	if len(sources) == 0 {
		return StreamBlockSource{Path: routing.RelayPath, Network: routing.WANNetwork, Reachable: true}
	}
	candidates := make([]routing.CandidateSource, 0, len(sources))
	contentID := fmt.Sprintf("%s:%d", rel, index)
	for i, source := range sources {
		peerID := source.PeerID
		if peerID == "" {
			peerID = fmt.Sprintf("stream-source-%06d", i+1)
		}
		path := source.Path
		if path == "" {
			path = routing.RelayPath
		}
		reachable := source.Reachable
		if !source.Reachable && source.Path == "" {
			reachable = true
		}
		candidates = append(candidates, routing.CandidateSource{PeerID: peerID, ContentID: contentID, Path: path, Network: source.Network, RelayViaPeerID: source.RelayViaPeerID, Reachable: reachable})
	}
	selected, ok := routing.ChooseTransferSource(routing.SourceSelectionRequest{ContentID: contentID, Candidates: candidates})
	if !ok {
		return StreamBlockSource{Path: routing.RelayPath, Network: routing.WANNetwork, Reachable: true}
	}
	for i, candidate := range candidates {
		if candidate.PeerID == selected.PeerID {
			return sources[i]
		}
	}
	return StreamBlockSource{Path: routing.RelayPath, Network: routing.WANNetwork, Reachable: true}
}

func streamBlockSourceHandshake(codec *protocol.Codec, opts PullOptions, peerPublicKey string) error {
	nonce, err := newNonce()
	if err != nil {
		return err
	}
	hello, err := signedProtocolHello(opts.NodeID, opts.Identity, opts.EncryptionLevel, nonce, advertisedTransferLimits(opts.Transfer, opts.Peer))
	if err != nil {
		return err
	}
	hello.Capabilities = []string{"block-transfer"}
	if err := codec.Write(protocol.Message{Type: protocol.MessageHello, Hello: hello}); err != nil {
		return err
	}
	msg, err := codec.Read()
	if err != nil {
		return err
	}
	if msg.Type == protocol.MessageError {
		return fmt.Errorf("peer error %s: %s", msg.Error.Code, msg.Error.Message)
	}
	if msg.Type != protocol.MessageHello {
		return fmt.Errorf("expected hello, got %s", msg.Type)
	}
	if peerPublicKey == "" {
		peerPublicKey = opts.PeerPublicKey
	}
	if err := verifyServerHello(msg.Hello, peerPublicKey, nonce); err != nil {
		return err
	}
	if msg.Hello.EncryptionLevel < opts.EncryptionLevel && !opts.AllowWeakerEncryptionLevel {
		return fmt.Errorf("peer negotiated weaker encryption level %d below local level %d", msg.Hello.EncryptionLevel, opts.EncryptionLevel)
	}
	return peeridentity.ValidateEncryptionLevel(msg.Hello.EncryptionLevel)
}

func requestStreamBlock(ctx context.Context, codec *protocol.Codec, folderID string, rel string, index int, blockSize int, receiveLimiter *ratelimit.Limiter) ([]byte, error) {
	if err := codec.Write(protocol.Message{Type: protocol.MessageBlockRequest, BlockRequest: &protocol.BlockRequest{FolderID: folderID, Path: rel, Index: index, BlockSize: blockSize}}); err != nil {
		return nil, err
	}
	blockMsg, err := codec.Read()
	if err != nil {
		return nil, err
	}
	if blockMsg.Type == protocol.MessageError {
		return nil, fmt.Errorf("peer error %s: %s", blockMsg.Error.Code, blockMsg.Error.Message)
	}
	if blockMsg.Type != protocol.MessageBlockResponse || blockMsg.BlockResponse == nil || blockMsg.BlockResponse.Index != index {
		return nil, fmt.Errorf("unexpected block response: %+v", blockMsg)
	}
	if err := receiveLimiter.Wait(ctx, len(blockMsg.BlockResponse.Data)); err != nil {
		return nil, err
	}
	return blockMsg.BlockResponse.Data, nil
}

func fetchMissingInShareIgnoreIncludes(ctx context.Context, codec *protocol.Codec, opts PullOptions, receiveLimiter *ratelimit.Limiter) ([]string, error) {
	for {
		missing, err := scanner.MissingInShareSyncIgnoreIncludes(opts.LocalRoot)
		if err != nil {
			return nil, err
		}
		if len(missing) == 0 {
			return nil, nil
		}
		fetchedAny := false
		for _, rel := range missing {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
			fetched, err := fetchIgnoreInclude(ctx, codec, opts, rel, receiveLimiter)
			if err != nil {
				return nil, err
			}
			fetchedAny = fetchedAny || fetched
		}
		if !fetchedAny {
			return missing, nil
		}
	}
}

func fetchIgnoreInclude(ctx context.Context, codec *protocol.Codec, opts PullOptions, rel string, receiveLimiter *ratelimit.Limiter) (bool, error) {
	targetPath, err := safePath(opts.LocalRoot, rel)
	if err != nil {
		return false, err
	}
	blockSize := 32 * 1024
	tmpPath := targetPath + ".fse-stream-tmp"
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return false, err
	}
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return false, err
	}
	fetched := false
	for index := 0; ; index++ {
		select {
		case <-ctx.Done():
			out.Close()
			os.Remove(tmpPath)
			return false, ctx.Err()
		default:
		}
		if err := codec.Write(protocol.Message{Type: protocol.MessageBlockRequest, BlockRequest: &protocol.BlockRequest{FolderID: opts.FolderID, Path: rel, Index: index, BlockSize: blockSize}}); err != nil {
			out.Close()
			os.Remove(tmpPath)
			return false, err
		}
		msg, err := codec.Read()
		if err != nil {
			out.Close()
			os.Remove(tmpPath)
			return false, err
		}
		if msg.Type == protocol.MessageError {
			if !fetched {
				out.Close()
				os.Remove(tmpPath)
				return false, nil
			}
			break
		}
		if msg.Type != protocol.MessageBlockResponse || msg.BlockResponse == nil || msg.BlockResponse.Index != index {
			out.Close()
			os.Remove(tmpPath)
			return false, fmt.Errorf("unexpected ignore include block response: %+v", msg)
		}
		if err := receiveLimiter.Wait(ctx, len(msg.BlockResponse.Data)); err != nil {
			out.Close()
			os.Remove(tmpPath)
			return false, err
		}
		if _, err := out.Write(msg.BlockResponse.Data); err != nil {
			out.Close()
			os.Remove(tmpPath)
			return false, err
		}
		fetched = true
		if len(msg.BlockResponse.Data) < blockSize {
			break
		}
	}
	if err := out.Close(); err != nil {
		os.Remove(tmpPath)
		return false, err
	}
	if !fetched {
		os.Remove(tmpPath)
		return false, nil
	}
	return true, os.Rename(tmpPath, targetPath)
}

func localFileMatchesManifest(path string, manifest block.Manifest) (bool, error) {
	local, err := block.BuildManifest(path, manifest.BlockSize)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if local.Size != manifest.Size || local.BlockSize != manifest.BlockSize || len(local.Blocks) != len(manifest.Blocks) {
		return false, nil
	}
	for i, want := range manifest.Blocks {
		have := local.Blocks[i]
		if have.Index != want.Index || have.Offset != want.Offset || have.Size != want.Size || !bytes.Equal(have.Hash, want.Hash) {
			return false, nil
		}
	}
	return true, nil
}

func cleanNonResumableTemps(root string, files []protocol.FolderIndexFile) (recovery.Result, error) {
	keep := make(map[string]struct{}, len(files))
	for _, file := range files {
		path, err := safePath(root, file.RelativePath)
		if err != nil {
			return recovery.Result{}, err
		}
		keep[path+".fse-stream-tmp"] = struct{}{}
	}
	result := recovery.Result{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() || !recovery.IsInterruptedTempName(entry.Name()) {
			return nil
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if _, ok := keep[abs]; ok {
			return nil
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		result.Removed++
		return nil
	})
	if os.IsNotExist(err) {
		return result, nil
	}
	return result, err
}

func hasVerifiedTempBlock(file *os.File, offset int64, size int, hash []byte) (bool, error) {
	buf := make([]byte, size)
	n, err := file.ReadAt(buf, offset)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if n != size {
		return false, nil
	}
	got := sha256.Sum256(buf)
	return bytes.Equal(got[:], hash), nil
}

func readBlock(path string, index int, blockSize int) ([]byte, error) {
	if index < 0 || blockSize <= 0 {
		return nil, fmt.Errorf("invalid block request")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if _, err := file.Seek(int64(index*blockSize), io.SeekStart); err != nil {
		return nil, err
	}
	buf := make([]byte, blockSize)
	n, err := io.ReadFull(file, buf)
	if err == io.EOF {
		return nil, fmt.Errorf("block index out of range")
	}
	if err == io.ErrUnexpectedEOF {
		return buf[:n], nil
	}
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func scanLocalBlocks(root string, blockSize int) (map[string]localBlock, error) {
	index := make(map[string]localBlock)
	if blockSize <= 0 {
		return index, nil
	}
	local, err := scanner.ScanFolder(root, scanner.Options{BlockSize: blockSize})
	if os.IsNotExist(err) {
		if err := os.MkdirAll(root, 0o755); err != nil {
			return nil, err
		}
		return index, nil
	}
	if err != nil {
		return nil, err
	}
	for _, file := range local.Files {
		path, err := safePath(root, file.RelativePath)
		if err != nil {
			return nil, err
		}
		for _, b := range file.Manifest.Blocks {
			key := blockKey(b.Size, b.Hash)
			if _, exists := index[key]; !exists {
				index[key] = localBlock{Path: path, Block: scannerBlock{Index: b.Index, Offset: b.Offset, Size: b.Size, Hash: b.Hash}}
			}
		}
	}
	return index, nil
}

func readLocalBlock(source localBlock) ([]byte, error) {
	file, err := os.Open(source.Path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if _, err := file.Seek(source.Block.Offset, io.SeekStart); err != nil {
		return nil, err
	}
	buf := make([]byte, source.Block.Size)
	if _, err := io.ReadFull(file, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func blockSizeFromIndex(files []protocol.FolderIndexFile) int {
	for _, file := range files {
		if file.Manifest.BlockSize > 0 {
			return file.Manifest.BlockSize
		}
	}
	return 0
}

func blockKey(size int, hash []byte) string {
	return fmt.Sprintf("%d:%x", size, hash)
}

func moveMatchingStaleLocalFiles(root string, remoteFiles []protocol.FolderIndexFile, blockSize int) (int, error) {
	if blockSize <= 0 || len(remoteFiles) == 0 {
		return 0, nil
	}
	local, err := scanner.ScanFolder(root, scanner.Options{BlockSize: blockSize})
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	remoteByPath := make(map[string]protocol.FolderIndexFile, len(remoteFiles))
	remotePaths := make([]string, 0, len(remoteFiles))
	for _, file := range remoteFiles {
		remoteByPath[file.RelativePath] = file
		remotePaths = append(remotePaths, file.RelativePath)
	}
	sort.Strings(remotePaths)
	candidates := make(map[string][]string)
	for _, file := range local.Files {
		if _, stillPresent := remoteByPath[file.RelativePath]; stillPresent {
			continue
		}
		key := manifestKey(file.Manifest)
		if key == "" {
			continue
		}
		candidates[key] = append(candidates[key], file.RelativePath)
	}
	for key := range candidates {
		sort.Strings(candidates[key])
	}
	moved := 0
	for _, rel := range remotePaths {
		remote := remoteByPath[rel]
		finalPath, err := safePath(root, rel)
		if err != nil {
			return moved, err
		}
		matches, err := localFileMatchesManifest(finalPath, remote.Manifest)
		if err != nil {
			return moved, err
		}
		if matches {
			continue
		}
		if _, err := os.Stat(finalPath); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return moved, err
		}
		moveRel, ok := takeMoveCandidate(candidates, remote.Manifest)
		if !ok {
			continue
		}
		sourcePath, err := safePath(root, moveRel)
		if err != nil {
			return moved, err
		}
		if err := os.MkdirAll(filepath.Dir(finalPath), 0o700); err != nil {
			return moved, err
		}
		if err := os.Rename(sourcePath, finalPath); err != nil {
			return moved, err
		}
		moved++
	}
	return moved, nil
}

func takeMoveCandidate(candidates map[string][]string, manifest block.Manifest) (string, bool) {
	key := manifestKey(manifest)
	paths := candidates[key]
	if len(paths) == 0 {
		return "", false
	}
	path := paths[0]
	if len(paths) == 1 {
		delete(candidates, key)
	} else {
		candidates[key] = paths[1:]
	}
	return path, true
}

func manifestKey(manifest block.Manifest) string {
	if manifest.Size == 0 && len(manifest.Blocks) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("%d", manifest.Size))
	for _, b := range manifest.Blocks {
		builder.WriteString(fmt.Sprintf("|%d:%x", b.Size, b.Hash))
	}
	return builder.String()
}

func safePath(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("unsafe path %q", rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." || len(clean) >= 3 && clean[:3] == "../" {
		return "", fmt.Errorf("unsafe path %q", rel)
	}
	full := filepath.Join(root, clean)
	rootClean, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	fullClean, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	relToRoot, err := filepath.Rel(rootClean, fullClean)
	if err != nil {
		return "", err
	}
	if relToRoot == ".." || len(relToRoot) >= 3 && relToRoot[:3] == ".."+string(os.PathSeparator) {
		return "", fmt.Errorf("unsafe path %q", rel)
	}
	return fullClean, nil
}

func deferStaleDeletes(opts PullOptions, index *protocol.FolderIndex, keep map[string]struct{}) error {
	store, ok := opts.MetadataStore.(skippedDeleteStore)
	if !ok || index == nil || index.MetadataSummary == nil {
		return nil
	}
	stale, err := listStale(opts.LocalRoot, keep)
	if err != nil {
		return err
	}
	for _, entry := range stale {
		if err := store.SaveSkippedDelete(state.SkippedDelete{
			FolderID:                  opts.FolderID,
			Path:                      entry.RelativePath,
			RequiredMetadataCursor:    index.MetadataSummary.Cursor,
			RequiredMetadataStateHash: index.MetadataSummary.StateHash,
			Reason:                    "metadata_catchup_pending",
		}); err != nil {
			return err
		}
	}
	return nil
}

func deleteStale(root string, keep map[string]struct{}) (int, error) {
	stale, err := listStale(root, keep)
	if err != nil {
		return 0, err
	}
	for _, entry := range stale {
		if _, _, err := backup.RetainExistingBackupIntakeFile(root, entry.RelativePath, time.Time{}); err != nil {
			return 0, err
		}
		if err := os.Remove(entry.AbsolutePath); err != nil {
			return 0, err
		}
	}
	return len(stale), nil
}

type stalePath struct {
	RelativePath string
	AbsolutePath string
}

func listStale(root string, keep map[string]struct{}) ([]stalePath, error) {
	matcher, err := scanner.LoadSyncIgnoreMatcher(root)
	if err != nil {
		return nil, err
	}
	var stale []stalePath
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel == "." {
				return nil
			}
			if rel == ".sync" || strings.HasPrefix(rel, ".sync/") {
				return filepath.SkipDir
			}
			if matcher.IsIgnored(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Base(path) == ".DS_Store" {
			return nil
		}
		if matcher.IsIgnored(rel) {
			return nil
		}
		if _, ok := keep[rel]; !ok {
			stale = append(stale, stalePath{RelativePath: rel, AbsolutePath: path})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Slice(stale, func(i, j int) bool { return stale[i].RelativePath < stale[j].RelativePath })
	return stale, nil
}
