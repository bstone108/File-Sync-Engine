package link

import (
	"context"
	"strings"
	"testing"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/peeridentity"
)

func TestCodeLookupRejectsHijackAndIsSingleUse(t *testing.T) {
	publisher, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	attacker, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	directory := NewMemoryDirectory()
	service := NewService(directory)
	service.SetPeerID("12D3KooWpublisher")
	service.SetRelays([]string{"http://relay.example:22420"})
	profile := Profile{
		DiscoveryID: publisher.PublicKey,
		NodeName:    "publisher",
		Addresses: []Address{
			{Kind: "manual", Address: "http://127.0.0.1:1", NetworkHint: "local"},
			{Kind: "vpn", Address: "http://100.64.0.8:22420", NetworkHint: "vpn_overlay"},
			{Kind: "manual", Address: "http://203.0.113.9:22420", NetworkHint: "wan"},
		},
	}
	code, expires, err := service.Issue(context.Background(), publisher, profile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(code, "-") || len(NormalizeCode(code)) != 8 {
		t.Fatalf("code = %q", code)
	}
	if expires.Before(time.Now()) {
		t.Fatal("code already expired")
	}
	namespace := Namespace(code)
	if strings.Contains(namespace, NormalizeCode(code)) || strings.Contains(strings.ToLower(namespace), strings.ToLower(code)) {
		t.Fatalf("namespace exposes the code: %s", namespace)
	}
	attackerCode, err := NewCode()
	if err != nil {
		t.Fatal(err)
	}
	_, forgedPayload, err := Seal(attackerCode, attacker, Profile{
		DiscoveryID: attacker.PublicKey,
		NodeName:    "attacker",
		Addresses:   []Address{{Kind: "manual", Address: "http://198.51.100.9:22420", NetworkHint: "wan"}},
	}, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := directory.Publish(context.Background(), namespace, forgedPayload); err != nil {
		t.Fatal(err)
	}
	// A copied record with the attacker's address and the real MAC must also fail.
	payloads, err := directory.Lookup(context.Background(), namespace)
	if err != nil || len(payloads) < 2 {
		t.Fatalf("payloads = %d err=%v", len(payloads), err)
	}
	tampered := append([]byte(nil), payloads[0]...)
	tampered = []byte(strings.Replace(string(tampered), "203.0.113.9", "198.51.100.8", 1))
	if err := directory.Publish(context.Background(), namespace, tampered); err != nil {
		t.Fatal(err)
	}

	got, err := service.Lookup(context.Background(), strings.ToLower(code))
	if err != nil {
		t.Fatal(err)
	}
	if got.DiscoveryID != publisher.PublicKey || got.DHTPeerID != "12D3KooWpublisher" {
		t.Fatalf("lookup accepted the wrong peer: %+v", got.Profile())
	}
	if len(got.Relays) != 1 || got.Relays[0] != "http://relay.example:22420" {
		t.Fatalf("relays = %+v", got.Relays)
	}

	redeemer, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	claim, err := SignClaim(redeemer, ClaimRequest{
		Code:     code,
		NodeName: "redeemer",
		APIKey:   "redeemer-api",
		Profile:  Profile{DiscoveryID: redeemer.PublicKey, NodeName: "redeemer", Addresses: []Address{{Kind: "manual", Address: "http://10.0.0.5:22420", NetworkHint: "local"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Claim(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Claim(context.Background(), claim); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("second claim = %v", err)
	}
	if _, err := service.Lookup(context.Background(), code); err == nil {
		t.Fatal("unpublished code was still looked up")
	}
}

func TestExpiredPairingCodeIsRejected(t *testing.T) {
	identity, err := peeridentity.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(NewMemoryDirectory())
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	service.ttl = time.Minute
	code, _, err := service.Issue(context.Background(), identity, Profile{DiscoveryID: identity.PublicKey, NodeName: "node", Addresses: []Address{{Kind: "manual", Address: "http://10.0.0.2:22420"}}})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now.Add(2 * time.Minute) }
	if _, err := service.Lookup(context.Background(), code); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("lookup = %v", err)
	}
}

func TestManualProfileKeepsLANTailscalePublicRelayAndPin(t *testing.T) {
	cfg := config.Config{
		NodeName: "node-a",
		API:      config.APIConfig{Listen: "0.0.0.0:22420", AdvertiseURL: "https://203.0.113.20:22420"},
		Identity: config.IdentityConfig{PublicKey: "public-key"},
	}
	profile := Gather(cfg, Extras{
		IPs:               []string{"10.1.1.5", "100.64.1.9", "172.17.0.2"},
		Relays:            []string{"https://relay.example:22420"},
		DHTPeerID:         "12D3KooWnode",
		CertificateSHA256: strings.Repeat("ab", 32),
	})
	if profile.DHTPeerID != "12D3KooWnode" || profile.CertificateSHA256 != strings.Repeat("ab", 32) {
		t.Fatalf("identity fields = %+v", profile)
	}
	byHint := map[string]string{}
	for _, address := range profile.Addresses {
		byHint[address.NetworkHint+"/"+address.Kind] = address.Address
	}
	if byHint["local/manual"] != "https://10.1.1.5:22420" {
		t.Fatalf("lan = %+v", profile.Addresses)
	}
	if byHint["vpn_overlay/vpn"] != "https://100.64.1.9:22420" {
		t.Fatalf("tailscale = %+v", profile.Addresses)
	}
	if byHint["wan/manual"] != "https://203.0.113.20:22420" {
		t.Fatalf("public = %+v", profile.Addresses)
	}
	if _, ok := byHint["container_bridge/manual"]; ok {
		t.Fatalf("bridge address was advertised: %+v", profile.Addresses)
	}
	for _, address := range profile.Addresses {
		if strings.Contains(address.Address, "172.17.0.2") {
			t.Fatalf("bridge address was advertised: %+v", profile.Addresses)
		}
	}
	if len(profile.Relays) != 1 || profile.Relays[0] != "https://relay.example:22420" {
		t.Fatalf("relays = %+v", profile.Relays)
	}
	endpoints := profile.Endpoints("https://203.0.113.20:22420")
	if len(endpoints) != 4 {
		t.Fatalf("endpoints = %+v", endpoints)
	}
	withHostPort := Gather(cfg, Extras{ExtraURLs: []string{"https://192.168.1.20:22420", "https://172.17.0.4:22420"}})
	foundHostPort := false
	for _, address := range withHostPort.Addresses {
		if strings.Contains(address.Address, "172.17.") || strings.Contains(address.Address, "172.18.") {
			t.Fatalf("bridge URL in extra addresses: %+v", withHostPort.Addresses)
		}
		if address.Address == "https://192.168.1.20:22420" && address.NetworkHint == "local" {
			foundHostPort = true
		}
	}
	if !foundHostPort {
		t.Fatalf("published host port missing: %+v", withHostPort.Addresses)
	}
}

func TestDHTDirectoryPublishesHashedNamespace(t *testing.T) {
	fake := &recordingDHT{}
	directory := NewDHTDirectory(fake)
	code := "ABCD-EFGH"
	payload := []byte(`{"version":"fse-pair-code-v1"}`)
	if err := directory.Publish(context.Background(), Namespace(code), payload); err != nil {
		t.Fatal(err)
	}
	if len(fake.published) != 1 {
		t.Fatalf("published = %+v", fake.published)
	}
	for namespace := range fake.published {
		if strings.Contains(namespace, "ABCD") || !strings.HasPrefix(namespace, CodeNamespacePrefix) {
			t.Fatalf("namespace = %s", namespace)
		}
	}
	got, err := directory.Lookup(context.Background(), Namespace(code))
	if err != nil || len(got) != 1 || string(got[0]) != string(payload) {
		t.Fatalf("lookup = %q err=%v", got, err)
	}
}

type recordingDHT struct {
	published map[string][]byte
}

func (r *recordingDHT) PublishPairing(_ context.Context, namespace string, payload []byte) error {
	if r.published == nil {
		r.published = map[string][]byte{}
	}
	r.published[namespace] = append([]byte(nil), payload...)
	return nil
}

func (r *recordingDHT) LookupPairing(_ context.Context, namespace string) ([][]byte, error) {
	payload := r.published[namespace]
	if len(payload) == 0 {
		return nil, nil
	}
	return [][]byte{append([]byte(nil), payload...)}, nil
}

func (r *recordingDHT) UnpublishPairing(_ context.Context, namespace string) error {
	delete(r.published, namespace)
	return nil
}

func (r *recordingDHT) PeerID() string { return "12D3KooWfake" }
