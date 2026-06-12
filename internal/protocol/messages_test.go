package protocol

import (
	"bytes"
	"strings"
	"testing"

	"filesyncengine/internal/block"
)

func TestCodecRoundTripsHelloAndBlockRequest(t *testing.T) {
	var wire bytes.Buffer
	codec := NewCodec(&wire, &wire)

	hello := Message{
		Type: MessageHello,
		Hello: &Hello{
			ProtocolVersion: 1,
			NodeID:          "node-a",
			Capabilities:    []string{"folder-index", "block-transfer"},
			Transfer:        &TransferLimits{SendBytesPerSecond: 1024, ReceiveBytesPerSecond: 2048},
		},
	}
	if err := codec.Write(hello); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	request := Message{
		Type:         MessageBlockRequest,
		BlockRequest: &BlockRequest{FolderID: "docs", Path: "alpha.txt", Index: 2, BlockSize: 131072},
	}
	if err := codec.Write(request); err != nil {
		t.Fatalf("write request: %v", err)
	}

	first, err := codec.Read()
	if err != nil {
		t.Fatalf("read hello: %v", err)
	}
	if first.Type != MessageHello || first.Hello == nil || first.Hello.NodeID != "node-a" || len(first.Hello.Capabilities) != 2 {
		t.Fatalf("unexpected hello: %+v", first)
	}
	if first.Hello.Transfer == nil || first.Hello.Transfer.SendBytesPerSecond != 1024 || first.Hello.Transfer.ReceiveBytesPerSecond != 2048 {
		t.Fatalf("unexpected hello transfer limits: %+v", first.Hello.Transfer)
	}
	second, err := codec.Read()
	if err != nil {
		t.Fatalf("read request: %v", err)
	}
	if second.Type != MessageBlockRequest || second.BlockRequest == nil || second.BlockRequest.Path != "alpha.txt" || second.BlockRequest.Index != 2 {
		t.Fatalf("unexpected block request: %+v", second)
	}
}

func TestCodecRoundTripsPeerExchange(t *testing.T) {
	var wire bytes.Buffer
	codec := NewCodec(&wire, &wire)

	msg := Message{
		Type: MessagePeerExchange,
		PeerExchange: &PeerExchange{
			Peers: []PeerInfo{
				{ID: "peer-b", Addresses: []string{"tcp://10.0.0.2:22420"}},
				{ID: "peer-c", Addresses: []string{"tcp://10.0.0.3:22420", "tcp://10.0.0.3:22421"}},
			},
			IdentityGroups: []IdentityGroupAdvertisement{{
				GroupID: "family-sync",
				Folders: []FolderAdvertisement{{
					ID:    "docs",
					Label: "Documents",
					Snapshots: []SnapshotMarker{{
						ID:                    "snap-remote",
						FolderID:              "docs",
						Cursor:                7,
						StateHash:             "remote-hash",
						CreatedAt:             "2026-05-25T03:00:00Z",
						Description:           "remote snapshot",
						Pinned:                true,
						ArchiveFullyProtected: true,
						DBCheckpointAvailable: true,
					}},
				}},
			}},
		},
	}
	if err := codec.Write(msg); err != nil {
		t.Fatalf("write peer exchange: %v", err)
	}

	read, err := codec.Read()
	if err != nil {
		t.Fatalf("read peer exchange: %v", err)
	}
	if read.Type != MessagePeerExchange || read.PeerExchange == nil || len(read.PeerExchange.Peers) != 2 {
		t.Fatalf("unexpected peer exchange: %+v", read)
	}
	if read.PeerExchange.Peers[1].ID != "peer-c" || len(read.PeerExchange.Peers[1].Addresses) != 2 {
		t.Fatalf("unexpected peer-c advertisement: %+v", read.PeerExchange.Peers[1])
	}
	if got := read.PeerExchange.IdentityGroups[0].Folders[0].ID; got != "docs" {
		t.Fatalf("identity group folder advertisement = %q, want docs", got)
	}
	snapshots := read.PeerExchange.IdentityGroups[0].Folders[0].Snapshots
	if len(snapshots) != 1 || snapshots[0].ID != "snap-remote" || snapshots[0].FolderID != "docs" || snapshots[0].Cursor != 7 || !snapshots[0].Pinned || !snapshots[0].ArchiveFullyProtected || !snapshots[0].DBCheckpointAvailable {
		t.Fatalf("unexpected snapshot marker advertisement: %+v", snapshots)
	}
}

func TestCodecRejectsMalformedPeerExchangeSnapshotMarkers(t *testing.T) {
	for name, marker := range map[string]SnapshotMarker{
		"empty id":        {FolderID: "docs"},
		"empty folder id": {ID: "snap-remote"},
	} {
		t.Run(name, func(t *testing.T) {
			var wire bytes.Buffer
			codec := NewCodec(&wire, &wire)
			err := codec.Write(Message{
				Type: MessagePeerExchange,
				PeerExchange: &PeerExchange{IdentityGroups: []IdentityGroupAdvertisement{{
					GroupID: "family-sync",
					Folders: []FolderAdvertisement{{ID: "docs", Snapshots: []SnapshotMarker{marker}}},
				}}},
			})
			if err == nil || !strings.Contains(err.Error(), "snapshot") {
				t.Fatalf("expected snapshot validation error, got %v", err)
			}
		})
	}
}

func TestCodecRoundTripsMetadataSyncMessages(t *testing.T) {
	var wire bytes.Buffer
	codec := NewCodec(&wire, &wire)

	stateMsg := Message{
		Type: MessageMetadataState,
		MetadataState: &MetadataState{
			PeerID:  "node-a",
			Folders: []MetadataFolderSummary{{FolderID: "docs", Cursor: 7, Files: 3, Tombstones: 1, StateHash: "abc123"}},
		},
	}
	if err := codec.Write(stateMsg); err != nil {
		t.Fatalf("write metadata state: %v", err)
	}
	changesMsg := Message{
		Type: MessageMetadataChanges,
		MetadataChanges: &MetadataChanges{
			FolderID:   "docs",
			FromCursor: 4,
			ToCursor:   8,
			StateHash:  "abc123",
			Changes: []MetadataChange{
				{Kind: MetadataChangeDelete, Path: "old.txt", Revision: 5},
				{Kind: MetadataChangeUpsert, Path: "new.txt", Revision: 7, Manifest: &block.Manifest{Path: "new.txt", Size: 3}},
				{Kind: MetadataChangeMove, FromPath: "new.txt", Path: "archive/new.txt", Revision: 8, Manifest: &block.Manifest{Path: "archive/new.txt", Size: 3}},
			},
		},
	}
	if err := codec.Write(changesMsg); err != nil {
		t.Fatalf("write metadata changes: %v", err)
	}
	ackMsg := Message{
		Type:        MessageMetadataAck,
		MetadataAck: &MetadataAck{FolderID: "docs", FromCursor: 4, ToCursor: 8, StateHash: "abc123"},
	}
	if err := codec.Write(ackMsg); err != nil {
		t.Fatalf("write metadata ack: %v", err)
	}
	meshAckMsg := Message{
		Type: MessageMeshSettingsAck,
		MeshSettingsAck: &MeshSettingsAck{TargetNodeID: "node-b", Results: []MeshSettingsAckResult{
			{ID: "change-ok", Status: "acked", UpdatedAt: "2026-05-31T18:00:00Z"},
			{ID: "change-bad", Status: "failed", UpdatedAt: "2026-05-31T18:01:00Z", LastError: "validation failed"},
		}},
	}
	if err := codec.Write(meshAckMsg); err != nil {
		t.Fatalf("write mesh settings ack: %v", err)
	}

	stateRead, err := codec.Read()
	if err != nil {
		t.Fatalf("read metadata state: %v", err)
	}
	if stateRead.Type != MessageMetadataState || stateRead.MetadataState == nil || stateRead.MetadataState.Folders[0].Cursor != 7 {
		t.Fatalf("unexpected metadata state: %+v", stateRead)
	}
	changesRead, err := codec.Read()
	if err != nil {
		t.Fatalf("read metadata changes: %v", err)
	}
	if changesRead.Type != MessageMetadataChanges || changesRead.MetadataChanges == nil || changesRead.MetadataChanges.Changes[0].Kind != MetadataChangeDelete {
		t.Fatalf("unexpected metadata changes: %+v", changesRead)
	}
	if changesRead.MetadataChanges.Changes[2].Kind != MetadataChangeMove || changesRead.MetadataChanges.Changes[2].FromPath != "new.txt" || changesRead.MetadataChanges.Changes[2].Path != "archive/new.txt" {
		t.Fatalf("unexpected metadata move change: %+v", changesRead.MetadataChanges.Changes[2])
	}
	ackRead, err := codec.Read()
	if err != nil {
		t.Fatalf("read metadata ack: %v", err)
	}
	if ackRead.Type != MessageMetadataAck || ackRead.MetadataAck == nil || ackRead.MetadataAck.ToCursor != 8 || ackRead.MetadataAck.StateHash != "abc123" {
		t.Fatalf("unexpected metadata ack: %+v", ackRead)
	}
	meshAckRead, err := codec.Read()
	if err != nil {
		t.Fatalf("read mesh settings ack: %v", err)
	}
	if meshAckRead.Type != MessageMeshSettingsAck || meshAckRead.MeshSettingsAck == nil || meshAckRead.MeshSettingsAck.Results[1].Status != "failed" || meshAckRead.MeshSettingsAck.Results[1].LastError == "" {
		t.Fatalf("unexpected mesh settings ack: %+v", meshAckRead)
	}
}

func TestCodecRejectsUnknownAndMismatchedMessages(t *testing.T) {
	var wire bytes.Buffer
	codec := NewCodec(&wire, &wire)
	if err := codec.Write(Message{Type: MessageHello, BlockRequest: &BlockRequest{Path: "wrong"}}); err == nil || !strings.Contains(err.Error(), "hello payload") {
		t.Fatalf("expected hello payload error, got %v", err)
	}
	if err := codec.Write(Message{Type: MessageMetadataState}); err == nil || !strings.Contains(err.Error(), "metadata state payload") {
		t.Fatalf("expected metadata state payload error, got %v", err)
	}
	if err := codec.Write(Message{Type: MessageMetadataChanges}); err == nil || !strings.Contains(err.Error(), "metadata changes payload") {
		t.Fatalf("expected metadata changes payload error, got %v", err)
	}
	if err := codec.Write(Message{Type: MessageMetadataAck}); err == nil || !strings.Contains(err.Error(), "metadata ack payload") {
		t.Fatalf("expected metadata ack payload error, got %v", err)
	}
	if err := codec.Write(Message{Type: MessageMeshSettingsAck}); err == nil || !strings.Contains(err.Error(), "mesh settings ack payload") {
		t.Fatalf("expected mesh settings ack payload error, got %v", err)
	}
	if err := codec.Write(Message{Type: MessageType("mystery")}); err == nil || !strings.Contains(err.Error(), "unknown message type") {
		t.Fatalf("expected unknown type error, got %v", err)
	}
}

func TestCodecRejectsOversizedMessages(t *testing.T) {
	payload := strings.Repeat("x", MaxMessageBytes+1)
	wire := bytes.NewBufferString(`{"type":"error","error":{"code":"too_big","message":"` + payload + `"}}` + "\n")
	codec := NewCodec(wire, &bytes.Buffer{})
	if _, err := codec.Read(); err == nil || !strings.Contains(err.Error(), "message too large") {
		t.Fatalf("expected message too large error, got %v", err)
	}
}
