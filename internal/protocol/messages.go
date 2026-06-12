package protocol

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"filesyncengine/internal/block"
	"filesyncengine/internal/peeridentity"
)

const MaxMessageBytes = 1 << 20

type MessageType string

const (
	MessageHello               MessageType = "hello"
	MessageFolderIndex         MessageType = "folder_index"
	MessageBlockRequest        MessageType = "block_request"
	MessageBlockResponse       MessageType = "block_response"
	MessageMetadataState       MessageType = "metadata_state"
	MessageMetadataChanges     MessageType = "metadata_changes"
	MessageMetadataAck         MessageType = "metadata_ack"
	MessageMeshSettingsChanges MessageType = "mesh_settings_changes"
	MessageMeshSettingsAck     MessageType = "mesh_settings_ack"
	MessagePeerExchange        MessageType = "peer_exchange"
	MessageApplyResult         MessageType = "apply_result"
	MessageError               MessageType = "error"
)

type Message struct {
	Type                MessageType          `json:"type"`
	Hello               *Hello               `json:"hello,omitempty"`
	FolderIndex         *FolderIndex         `json:"folderIndex,omitempty"`
	BlockRequest        *BlockRequest        `json:"blockRequest,omitempty"`
	BlockResponse       *BlockResponse       `json:"blockResponse,omitempty"`
	MetadataState       *MetadataState       `json:"metadataState,omitempty"`
	MetadataChanges     *MetadataChanges     `json:"metadataChanges,omitempty"`
	MetadataAck         *MetadataAck         `json:"metadataAck,omitempty"`
	MeshSettingsChanges *MeshSettingsChanges `json:"meshSettingsChanges,omitempty"`
	MeshSettingsAck     *MeshSettingsAck     `json:"meshSettingsAck,omitempty"`
	PeerExchange        *PeerExchange        `json:"peerExchange,omitempty"`
	ApplyResult         *ApplyResult         `json:"applyResult,omitempty"`
	Error               *Error               `json:"error,omitempty"`
}

type Hello struct {
	ProtocolVersion int             `json:"protocolVersion"`
	NodeID          string          `json:"nodeID"`
	Capabilities    []string        `json:"capabilities,omitempty"`
	PublicKey       string          `json:"publicKey,omitempty"`
	EncryptionLevel int             `json:"encryptionLevel"`
	Transfer        *TransferLimits `json:"transfer,omitempty"`
	Nonce           string          `json:"nonce,omitempty"`
	Signature       string          `json:"signature,omitempty"`
}

type TransferLimits struct {
	SendBytesPerSecond    int64 `json:"sendBytesPerSecond"`
	ReceiveBytesPerSecond int64 `json:"receiveBytesPerSecond"`
}

type FolderIndex struct {
	FolderID        string                 `json:"folderID"`
	Files           []FolderIndexFile      `json:"files,omitempty"`
	MetadataSummary *MetadataFolderSummary `json:"metadataSummary,omitempty"`
	FullRefresh     bool                   `json:"fullRefresh,omitempty"`
}

type FolderIndexFile struct {
	RelativePath string         `json:"relativePath"`
	Manifest     block.Manifest `json:"manifest"`
	Revision     uint64         `json:"revision,omitempty"`
}

type BlockRequest struct {
	FolderID  string `json:"folderID"`
	Path      string `json:"path"`
	Index     int    `json:"index"`
	BlockSize int    `json:"blockSize"`
}

type BlockResponse struct {
	FolderID string `json:"folderID"`
	Path     string `json:"path"`
	Index    int    `json:"index"`
	Data     []byte `json:"data"`
}

type MetadataChangeKind string

const (
	MetadataChangeUpsert MetadataChangeKind = "upsert"
	MetadataChangeDelete MetadataChangeKind = "delete"
	MetadataChangeMove   MetadataChangeKind = "move"
)

type MetadataState struct {
	PeerID  string                  `json:"peerID"`
	Folders []MetadataFolderSummary `json:"folders"`
}

type MetadataFolderSummary struct {
	FolderID   string `json:"folderID"`
	Cursor     uint64 `json:"cursor"`
	Files      int    `json:"files"`
	Tombstones int    `json:"tombstones"`
	StateHash  string `json:"stateHash"`
}

type MetadataChanges struct {
	FolderID   string           `json:"folderID"`
	FromCursor uint64           `json:"fromCursor"`
	ToCursor   uint64           `json:"toCursor"`
	StateHash  string           `json:"stateHash"`
	More       bool             `json:"more,omitempty"`
	Changes    []MetadataChange `json:"changes"`
}

type MetadataChange struct {
	Kind     MetadataChangeKind `json:"kind"`
	FromPath string             `json:"fromPath,omitempty"`
	Path     string             `json:"path"`
	Revision uint64             `json:"revision"`
	Manifest *block.Manifest    `json:"manifest,omitempty"`
}

type MetadataAck struct {
	FolderID   string `json:"folderID"`
	FromCursor uint64 `json:"fromCursor"`
	ToCursor   uint64 `json:"toCursor"`
	StateHash  string `json:"stateHash"`
	Stop       bool   `json:"stop,omitempty"`
}

type MeshSettingsChanges struct {
	TargetNodeID string               `json:"targetNodeId"`
	Changes      []MeshSettingsChange `json:"changes,omitempty"`
}

type MeshSettingsChange struct {
	ID             string         `json:"id"`
	TargetNodeID   string         `json:"targetNodeId"`
	OriginNodeID   string         `json:"originNodeId"`
	IdempotencyKey string         `json:"idempotencyKey"`
	Revision       uint64         `json:"revision"`
	Status         string         `json:"status"`
	CreatedAt      string         `json:"createdAt,omitempty"`
	UpdatedAt      string         `json:"updatedAt,omitempty"`
	SettingsPatch  map[string]any `json:"settingsPatch,omitempty"`
	LastError      string         `json:"lastError,omitempty"`
}

type MeshSettingsAck struct {
	TargetNodeID string                  `json:"targetNodeId"`
	Results      []MeshSettingsAckResult `json:"results"`
}

type MeshSettingsAckResult struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	LastError string `json:"lastError,omitempty"`
}

type PeerExchange struct {
	Peers          []PeerInfo                   `json:"peers"`
	IdentityGroups []IdentityGroupAdvertisement `json:"identityGroups,omitempty"`
}

type IdentityGroupAdvertisement struct {
	GroupID string                `json:"groupID"`
	Folders []FolderAdvertisement `json:"folders,omitempty"`
}

type FolderAdvertisement struct {
	ID        string           `json:"id"`
	Label     string           `json:"label,omitempty"`
	Snapshots []SnapshotMarker `json:"snapshots,omitempty"`
}

type SnapshotMarker struct {
	ID                    string `json:"id"`
	FolderID              string `json:"folderID"`
	Cursor                uint64 `json:"cursor"`
	StateHash             string `json:"stateHash"`
	CreatedAt             string `json:"createdAt"`
	Description           string `json:"description,omitempty"`
	Pinned                bool   `json:"pinned,omitempty"`
	Deprecated            bool   `json:"deprecated,omitempty"`
	ArchiveFullyProtected bool   `json:"archiveFullyProtected,omitempty"`
	DBCheckpointAvailable bool   `json:"dbCheckpointAvailable,omitempty"`
}

type PeerInfo struct {
	ID        string   `json:"id"`
	Addresses []string `json:"addresses"`
}

type ApplyResult struct {
	FolderID string `json:"folderID"`
	Path     string `json:"path"`
	OK       bool   `json:"ok"`
	Message  string `json:"message,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Codec struct {
	r *bufio.Reader
	w io.Writer
}

func NewCodec(r io.Reader, w io.Writer) *Codec {
	return &Codec{r: bufio.NewReader(r), w: w}
}

func (c *Codec) Write(msg Message) error {
	if err := validateMessage(msg); err != nil {
		return err
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if len(data) > MaxMessageBytes {
		return fmt.Errorf("message too large: %d bytes", len(data))
	}
	data = append(data, '\n')
	_, err = c.w.Write(data)
	return err
}

func (c *Codec) Read() (Message, error) {
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		return Message{}, err
	}
	if len(line) > MaxMessageBytes {
		return Message{}, fmt.Errorf("message too large: %d bytes", len(line))
	}
	var msg Message
	if err := json.Unmarshal(line, &msg); err != nil {
		return Message{}, err
	}
	if err := validateMessage(msg); err != nil {
		return Message{}, err
	}
	return msg, nil
}

func validateMessage(msg Message) error {
	switch msg.Type {
	case MessageHello:
		if msg.Hello == nil {
			return fmt.Errorf("hello payload required")
		}
		if err := peeridentity.ValidateEncryptionLevel(msg.Hello.EncryptionLevel); err != nil {
			return err
		}
		if msg.Hello.Transfer != nil {
			if msg.Hello.Transfer.SendBytesPerSecond < 0 {
				return fmt.Errorf("hello transfer sendBytesPerSecond cannot be negative")
			}
			if msg.Hello.Transfer.ReceiveBytesPerSecond < 0 {
				return fmt.Errorf("hello transfer receiveBytesPerSecond cannot be negative")
			}
		}
	case MessageFolderIndex:
		if msg.FolderIndex == nil {
			return fmt.Errorf("folder index payload required")
		}
	case MessageBlockRequest:
		if msg.BlockRequest == nil {
			return fmt.Errorf("block request payload required")
		}
	case MessageBlockResponse:
		if msg.BlockResponse == nil {
			return fmt.Errorf("block response payload required")
		}
	case MessageMetadataState:
		if msg.MetadataState == nil {
			return fmt.Errorf("metadata state payload required")
		}
		for i, folder := range msg.MetadataState.Folders {
			if folder.FolderID == "" {
				return fmt.Errorf("metadata state folder[%d] folderID required", i)
			}
			if folder.StateHash == "" {
				return fmt.Errorf("metadata state folder[%d] stateHash required", i)
			}
		}
	case MessageMetadataChanges:
		if msg.MetadataChanges == nil {
			return fmt.Errorf("metadata changes payload required")
		}
		if msg.MetadataChanges.FolderID == "" {
			return fmt.Errorf("metadata changes folderID required")
		}
		for i, change := range msg.MetadataChanges.Changes {
			if change.Kind != MetadataChangeUpsert && change.Kind != MetadataChangeDelete && change.Kind != MetadataChangeMove {
				return fmt.Errorf("metadata changes change[%d] unknown kind %q", i, change.Kind)
			}
			if change.Path == "" {
				return fmt.Errorf("metadata changes change[%d] path required", i)
			}
			if (change.Kind == MetadataChangeUpsert || change.Kind == MetadataChangeMove) && change.Manifest == nil {
				return fmt.Errorf("metadata changes change[%d] manifest required for %s", i, change.Kind)
			}
			if change.Kind == MetadataChangeMove && change.FromPath == "" {
				return fmt.Errorf("metadata changes change[%d] fromPath required for move", i)
			}
		}
	case MessageMetadataAck:
		if msg.MetadataAck == nil {
			return fmt.Errorf("metadata ack payload required")
		}
		if msg.MetadataAck.FolderID == "" {
			return fmt.Errorf("metadata ack folderID required")
		}
		if msg.MetadataAck.ToCursor < msg.MetadataAck.FromCursor {
			return fmt.Errorf("metadata ack cursor range invalid")
		}
		if msg.MetadataAck.StateHash == "" {
			return fmt.Errorf("metadata ack stateHash required")
		}
	case MessageMeshSettingsChanges:
		if msg.MeshSettingsChanges == nil {
			return fmt.Errorf("mesh settings changes payload required")
		}
		if msg.MeshSettingsChanges.TargetNodeID == "" {
			return fmt.Errorf("mesh settings changes targetNodeId required")
		}
		for i, change := range msg.MeshSettingsChanges.Changes {
			if change.ID == "" {
				return fmt.Errorf("mesh settings changes change[%d] id required", i)
			}
			if change.TargetNodeID == "" {
				return fmt.Errorf("mesh settings changes change[%d] targetNodeId required", i)
			}
			if change.TargetNodeID != msg.MeshSettingsChanges.TargetNodeID {
				return fmt.Errorf("mesh settings changes change[%d] target mismatch", i)
			}
			if change.OriginNodeID == "" {
				return fmt.Errorf("mesh settings changes change[%d] originNodeId required", i)
			}
			if change.IdempotencyKey == "" {
				return fmt.Errorf("mesh settings changes change[%d] idempotencyKey required", i)
			}
		}
	case MessageMeshSettingsAck:
		if msg.MeshSettingsAck == nil {
			return fmt.Errorf("mesh settings ack payload required")
		}
		if msg.MeshSettingsAck.TargetNodeID == "" {
			return fmt.Errorf("mesh settings ack targetNodeId required")
		}
		for i, result := range msg.MeshSettingsAck.Results {
			if result.ID == "" {
				return fmt.Errorf("mesh settings ack result[%d] id required", i)
			}
			if result.Status != "acked" && result.Status != "failed" {
				return fmt.Errorf("mesh settings ack result[%d] unsupported status %q", i, result.Status)
			}
			if result.Status == "failed" && result.LastError == "" {
				return fmt.Errorf("mesh settings ack result[%d] failed status requires lastError", i)
			}
		}
	case MessagePeerExchange:
		if msg.PeerExchange == nil {
			return fmt.Errorf("peer exchange payload required")
		}
		for i, peer := range msg.PeerExchange.Peers {
			if peer.ID == "" {
				return fmt.Errorf("peer exchange peer[%d] id required", i)
			}
			if len(peer.Addresses) == 0 {
				return fmt.Errorf("peer exchange peer[%d] addresses required", i)
			}
			for j, address := range peer.Addresses {
				if address == "" {
					return fmt.Errorf("peer exchange peer[%d] address[%d] required", i, j)
				}
			}
		}
		for i, group := range msg.PeerExchange.IdentityGroups {
			if group.GroupID == "" {
				return fmt.Errorf("peer exchange identityGroups[%d] groupID required", i)
			}
			for j, folder := range group.Folders {
				if folder.ID == "" {
					return fmt.Errorf("peer exchange identityGroups[%d] folder[%d] id required", i, j)
				}
				for k, snapshot := range folder.Snapshots {
					if snapshot.ID == "" {
						return fmt.Errorf("peer exchange identityGroups[%d] folder[%d] snapshot[%d] id required", i, j, k)
					}
					if snapshot.FolderID == "" {
						return fmt.Errorf("peer exchange identityGroups[%d] folder[%d] snapshot[%d] folderID required", i, j, k)
					}
				}
			}
		}
	case MessageApplyResult:
		if msg.ApplyResult == nil {
			return fmt.Errorf("apply result payload required")
		}
	case MessageError:
		if msg.Error == nil {
			return fmt.Errorf("error payload required")
		}
	default:
		return fmt.Errorf("unknown message type %q", msg.Type)
	}
	return nil
}
