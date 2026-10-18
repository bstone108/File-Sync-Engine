package link

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestRestartRepublishesCodeForASecondLookupAndDHT(t *testing.T) {
	publisher, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pairing-codes.json")
	local, err := ListenLocalDirectory("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = local.Close() })
	firstDHT := &recordingDHT{}
	service := NewService(NewMultiDirectory(local, NewDHTDirectory(firstDHT), PeerAPIDirectory{}))
	service.SetStorePath(path)
	code, _, err := service.Issue(context.Background(), publisher, Profile{DiscoveryID: publisher.PublicKey, NodeName: "publisher"})
	if err != nil {
		t.Fatal(err)
	}
	if err := local.Close(); err != nil {
		t.Fatal(err)
	}

	restoredLocal, err := ListenLocalDirectory("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restoredLocal.Close() })
	restoredDHT := &recordingDHT{}
	restarted := NewService(NewMultiDirectory(restoredLocal, NewDHTDirectory(restoredDHT), PeerAPIDirectory{}))
	restarted.SetStorePath(path)
	if err := restarted.Republish(context.Background()); err != nil {
		t.Fatal(err)
	}
	namespace := Namespace(code)
	deadline := time.Now().Add(2 * time.Second)
	for restoredDHT.published[namespace] == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(restoredDHT.published[namespace]) == 0 {
		t.Fatal("DHT provider payload was not republished after restart")
	}

	redeemer, err := ListenLocalDirectory("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = redeemer.Close() })
	redeemer.SetDialTargets([]string{restoredLocal.Addr()})
	record, err := NewService(redeemer).Lookup(context.Background(), code)
	if err != nil {
		t.Fatalf("redeem lookup after restart: %v", err)
	}
	if record.DiscoveryID != publisher.PublicKey {
		t.Fatalf("record = %+v", record)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), NormalizeCode(code)) {
		t.Fatalf("store path %s = %s", path, data)
	}
}

func TestRepublishFailureKeepsTheStoredCode(t *testing.T) {
	publisher, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pairing-codes.json")
	service := NewService(NewMemoryDirectory())
	service.SetStorePath(path)
	if _, _, err := service.Issue(context.Background(), publisher, Profile{DiscoveryID: publisher.PublicKey, NodeName: "publisher"}); err != nil {
		t.Fatal(err)
	}
	restarted := NewService(rejectDirectory{})
	restarted.SetStorePath(path)
	if err := restarted.Republish(context.Background()); err == nil {
		t.Fatal("rejected republish returned success")
	}
	kept := NewService(NewMemoryDirectory())
	kept.SetStorePath(path)
	kept.mu.Lock()
	left := len(kept.codes)
	kept.mu.Unlock()
	if left != 1 {
		t.Fatalf("stored codes after a failed republish = %d", left)
	}
}

type rejectDirectory struct{}

func (rejectDirectory) Publish(context.Context, string, []byte) error {
	return errors.New("dht down")
}

func (rejectDirectory) Lookup(context.Context, string) ([][]byte, error) {
	return nil, errors.New("dht down")
}

func (rejectDirectory) Unpublish(context.Context, string) error { return nil }

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
