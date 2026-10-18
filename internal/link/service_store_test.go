package link

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filesyncengine/internal/peeridentity"
)

func TestPairingCodeSurvivesRestartUntilUsed(t *testing.T) {
	publisher, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	claimer, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pairing-codes.json")
	service := NewService(NewMemoryDirectory())
	service.SetStorePath(path)
	code, _, err := service.Issue(context.Background(), publisher, Profile{DiscoveryID: publisher.PublicKey, NodeName: "publisher"})
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewService(NewMemoryDirectory())
	restarted.SetStorePath(path)
	claim, err := SignClaim(claimer, ClaimRequest{Code: code, NodeName: "claimer", APIKey: "claimer-api"})
	if err != nil {
		t.Fatal(err)
	}
	record, err := restarted.Claim(context.Background(), claim)
	if err != nil {
		t.Fatal(err)
	}
	if record.DiscoveryID != publisher.PublicKey {
		t.Fatalf("record = %+v", record)
	}
	again := NewService(NewMemoryDirectory())
	again.SetStorePath(path)
	if _, err := again.Claim(context.Background(), claim); err == nil {
		t.Fatal("used code was loaded after restart")
	}
}

func TestExpiredPairingCodeIsNotLoaded(t *testing.T) {
	publisher, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pairing-codes.json")
	service := NewService(NewMemoryDirectory())
	service.SetTTL(time.Minute)
	service.SetStorePath(path)
	if _, _, err := service.Issue(context.Background(), publisher, Profile{DiscoveryID: publisher.PublicKey, NodeName: "publisher"}); err != nil {
		t.Fatal(err)
	}
	restarted := NewService(NewMemoryDirectory())
	restarted.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	restarted.SetStorePath(path)
	restarted.mu.Lock()
	left := len(restarted.codes)
	restarted.mu.Unlock()
	if left != 0 {
		t.Fatalf("expired codes loaded: %d", left)
	}
}

func TestFailedPublishDoesNotKeepTheCode(t *testing.T) {
	publisher, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pairing-codes.json")
	service := NewService(PeerAPIDirectory{})
	service.SetStorePath(path)
	if _, _, err := service.Issue(context.Background(), publisher, Profile{DiscoveryID: publisher.PublicKey, NodeName: "publisher"}); err == nil {
		t.Fatal("lookup-only directory published a record")
	}
	restarted := NewService(NewMemoryDirectory())
	restarted.SetStorePath(path)
	restarted.mu.Lock()
	left := len(restarted.codes)
	restarted.mu.Unlock()
	if left != 0 {
		t.Fatalf("unpublished code stayed on disk: %d", left)
	}
	if _, err := os.Stat(path); err == nil {
		data, _ := os.ReadFile(path)
		if len(data) > 0 && string(data) != "[]\n" && string(data) != "null\n" {
			t.Fatalf("store = %s", data)
		}
	}
}
