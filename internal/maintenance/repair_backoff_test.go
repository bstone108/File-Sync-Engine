package maintenance

import (
	"testing"
	"time"

	"filesyncengine/internal/block"
)

func TestRepairBackoffBlocksRepeatedFailuresUntilDelayExpires(t *testing.T) {
	store := newMemoryRepairBackoffStore()
	now := time.Unix(1700000000, 0).UTC()
	manifest := block.Manifest{Path: "data.bin", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 4, Hash: []byte{0xaa}}}}
	policy := RepairBackoffPolicy{BaseDelay: time.Minute, MaxDelay: 10 * time.Minute, MaxAttempts: 3, Now: func() time.Time { return now }}

	if err := RecordRepairFailure(store, "docs", "data.bin", manifest, policy, "verification failed"); err != nil {
		t.Fatalf("RecordRepairFailure: %v", err)
	}
	decision, err := ShouldAttemptRepair(store, "docs", "data.bin", manifest, policy)
	if err != nil {
		t.Fatalf("ShouldAttemptRepair: %v", err)
	}
	if decision.Allowed || decision.Reason != "backoff" || decision.Attempts != 1 {
		t.Fatalf("decision=%+v, want blocked by first backoff", decision)
	}
	if !decision.BackoffUntil.Equal(now.Add(time.Minute)) {
		t.Fatalf("backoffUntil=%v, want %v", decision.BackoffUntil, now.Add(time.Minute))
	}

	now = now.Add(time.Minute)
	decision, err = ShouldAttemptRepair(store, "docs", "data.bin", manifest, policy)
	if err != nil {
		t.Fatalf("ShouldAttemptRepair after delay: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("decision=%+v, want allowed after backoff expires", decision)
	}
}

func TestRepairBackoffCapsAttemptsUntilTrustedManifestChanges(t *testing.T) {
	store := newMemoryRepairBackoffStore()
	now := time.Unix(1700000000, 0).UTC()
	policy := RepairBackoffPolicy{BaseDelay: time.Minute, MaxDelay: 10 * time.Minute, MaxAttempts: 2, Now: func() time.Time { return now }}
	manifest := block.Manifest{Path: "data.bin", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 4, Hash: []byte{0xaa}}}}

	for i := 0; i < 2; i++ {
		if err := RecordRepairFailure(store, "docs", "data.bin", manifest, policy, "placement failed"); err != nil {
			t.Fatalf("RecordRepairFailure %d: %v", i+1, err)
		}
		now = now.Add(10 * time.Minute)
	}
	decision, err := ShouldAttemptRepair(store, "docs", "data.bin", manifest, policy)
	if err != nil {
		t.Fatalf("ShouldAttemptRepair: %v", err)
	}
	if decision.Allowed || decision.Reason != "max-attempts" || decision.Attempts != 2 {
		t.Fatalf("decision=%+v, want blocked after max attempts", decision)
	}

	changed := manifest
	changed.Blocks[0].Hash = []byte{0xbb}
	decision, err = ShouldAttemptRepair(store, "docs", "data.bin", changed, policy)
	if err != nil {
		t.Fatalf("ShouldAttemptRepair changed manifest: %v", err)
	}
	if !decision.Allowed || decision.Reason != "trusted-manifest-changed" || decision.Attempts != 0 {
		t.Fatalf("decision=%+v, want changed trusted manifest to reset loop prevention", decision)
	}
}

func TestRepairBackoffSuccessClearsPerFileRepairState(t *testing.T) {
	store := newMemoryRepairBackoffStore()
	now := time.Unix(1700000000, 0).UTC()
	manifest := block.Manifest{Path: "data.bin", Size: 4, BlockSize: 4, Blocks: []block.Block{{Index: 0, Offset: 0, Size: 4, Hash: []byte{0xaa}}}}
	policy := RepairBackoffPolicy{BaseDelay: time.Minute, MaxDelay: 10 * time.Minute, MaxAttempts: 3, Now: func() time.Time { return now }}

	if err := RecordRepairFailure(store, "docs", "data.bin", manifest, policy, "temporary failure"); err != nil {
		t.Fatalf("RecordRepairFailure: %v", err)
	}
	if err := RecordRepairSuccess(store, "docs", "data.bin"); err != nil {
		t.Fatalf("RecordRepairSuccess: %v", err)
	}
	decision, err := ShouldAttemptRepair(store, "docs", "data.bin", manifest, policy)
	if err != nil {
		t.Fatalf("ShouldAttemptRepair: %v", err)
	}
	if !decision.Allowed || decision.Attempts != 0 || decision.Reason != "" {
		t.Fatalf("decision=%+v, want clean state after success", decision)
	}
}

type memoryRepairBackoffStore struct {
	items map[string]RepairAttemptState
}

func newMemoryRepairBackoffStore() *memoryRepairBackoffStore {
	return &memoryRepairBackoffStore{items: map[string]RepairAttemptState{}}
}

func (s *memoryRepairBackoffStore) RepairAttemptState(folderID string, path string) (RepairAttemptState, bool, error) {
	state, ok := s.items[folderID+"\x00"+path]
	return state, ok, nil
}

func (s *memoryRepairBackoffStore) SaveRepairAttemptState(state RepairAttemptState) error {
	s.items[state.FolderID+"\x00"+state.Path] = state
	return nil
}

func (s *memoryRepairBackoffStore) ClearRepairAttemptState(folderID string, path string) error {
	delete(s.items, folderID+"\x00"+path)
	return nil
}
