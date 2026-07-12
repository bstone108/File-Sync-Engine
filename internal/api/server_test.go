package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"filesyncengine/internal/config"
	"filesyncengine/internal/pairing"
	"filesyncengine/internal/state"
)

func TestStatusRequiresAPIKeyAndReturnsRuntimeSnapshot(t *testing.T) {
	server := NewServer(State{
		NodeName:      "node-a",
		StartedAt:     time.Unix(100, 0),
		ConfigPath:    "/tmp/fse.jsonc",
		Folders:       2,
		Peers:         3,
		ConfigVersion: 4,
	}, "secret")

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("status code = %d body=%s", ok.Code, ok.Body.String())
	}
	body := ok.Body.String()
	for _, want := range []string{`"nodeName":"node-a"`, `"folders":2`, `"peers":3`, `"configVersion":4`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
}

func TestIdentityPackageEndpointRequiresAuthAndReturnsPairingPackageWithoutDaemonSecrets(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	server.SetIdentityPackageHandler(func(ctx context.Context, req IdentityPackageRequest) (pairing.IdentityPackage, error) {
		if req.GroupID != "family-sync" {
			t.Fatalf("unexpected identity package request: %+v", req)
		}
		return pairing.IdentityPackage{
			Version:                    pairing.IdentityPackageVersion,
			NodeName:                   "node-a",
			DiscoveryID:                "public-discovery-key",
			GroupID:                    "family-sync",
			BootstrapProofKey:          strings.Repeat("k", 80),
			BootstrapEncryptionLevel:   10,
			DefaultPeerEncryptionLevel: 4,
		}, nil
	})

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/identity-package", strings.NewReader(`{"groupId":"family-sync"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/identity-package", strings.NewReader(`{"groupId":"family-sync"}`))
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("identity package code = %d body=%s", ok.Code, ok.Body.String())
	}
	body := ok.Body.String()
	for _, want := range []string{`"version":"fse-identity-package-v1"`, `"discoveryId":"public-discovery-key"`, `"groupId":"family-sync"`, `"bootstrapEncryptionLevel":10`, `"defaultPeerEncryptionLevel":4`} {
		if !strings.Contains(body, want) {
			t.Fatalf("identity package response missing %s: %s", want, body)
		}
	}
	for _, leaked := range []string{"api-secret", "private-secret"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("identity package response leaked daemon secret %q: %s", leaked, body)
		}
	}

	events := httptest.NewRecorder()
	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	eventsReq.Header.Set("X-FSE-API-Key", "secret")
	server.Router().ServeHTTP(events, eventsReq)
	if strings.Contains(events.Body.String(), strings.Repeat("k", 80)) {
		t.Fatalf("identity package event leaked bootstrap key: %s", events.Body.String())
	}
	if !strings.Contains(events.Body.String(), "identity.package.generated") {
		t.Fatalf("identity package event missing: %s", events.Body.String())
	}
}

func TestAPITrustEndpointRequiresAuthAndReturnsCertificatePairingStatus(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	server.SetAPITrustHandler(func(ctx context.Context) (APITrustResponse, error) {
		return APITrustResponse{
			Mode:                         "auto",
			TLSEnabled:                   true,
			TLSRequired:                  true,
			CertificateSHA256:            strings.Repeat("a", 64),
			TrustedCertificateSHA256:     strings.Repeat("a", 64),
			TrustedCertificateConfigured: true,
			TrustedCertificateMatches:    true,
			Message:                      "api certificate is pinned and matches the configured trust fingerprint",
		}, nil
	})

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/api/trust", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/api/trust", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("api trust code = %d body=%s", ok.Code, ok.Body.String())
	}
	body := ok.Body.String()
	for _, want := range []string{`"mode":"auto"`, `"tlsEnabled":true`, `"tlsRequired":true`, `"certificateSha256":"` + strings.Repeat("a", 64) + `"`, `"trustedCertificateConfigured":true`, `"trustedCertificateMatches":true`} {
		if !strings.Contains(body, want) {
			t.Fatalf("api trust response missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, "api-secret") || strings.Contains(body, "privateKey") {
		t.Fatalf("api trust response leaked secret-looking data: %s", body)
	}
}

func TestAPITrustCommandEndpointPinsActiveCertificateWithoutLeakingSecrets(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	server.SetAPITrustCommandHandler(func(ctx context.Context, req APITrustCommandRequest) (APITrustCommandResponse, error) {
		if req.Action != "pin-active-certificate" {
			t.Fatalf("unexpected trust command request: %+v", req)
		}
		return APITrustCommandResponse{
			Action:                       req.Action,
			Status:                       "accepted",
			CertificateSHA256:            strings.Repeat("b", 64),
			TrustedCertificateConfigured: true,
			TrustedCertificateMatches:    true,
			Message:                      "active API certificate fingerprint pinned for future HTTPS requests",
		}, nil
	})

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/api/trust-command", strings.NewReader(`{"action":"pin-active-certificate"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/api/trust-command", strings.NewReader(`{"action":"pin-active-certificate"}`))
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("api trust command code = %d body=%s", ok.Code, ok.Body.String())
	}
	body := ok.Body.String()
	for _, want := range []string{`"action":"pin-active-certificate"`, `"status":"accepted"`, `"certificateSha256":"` + strings.Repeat("b", 64) + `"`, `"trustedCertificateConfigured":true`, `"trustedCertificateMatches":true`} {
		if !strings.Contains(body, want) {
			t.Fatalf("api trust command response missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, "api-secret") || strings.Contains(body, "privateKey") {
		t.Fatalf("api trust command response leaked secret-looking data: %s", body)
	}

	events := httptest.NewRecorder()
	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	eventsReq.Header.Set("X-FSE-API-Key", "secret")
	server.Router().ServeHTTP(events, eventsReq)
	if !strings.Contains(events.Body.String(), "api.trust.pinned") {
		t.Fatalf("api trust command event missing: %s", events.Body.String())
	}
	if strings.Contains(events.Body.String(), strings.Repeat("b", 64)) {
		t.Fatalf("api trust command event should not include full certificate fingerprint: %s", events.Body.String())
	}
}

func TestFilesystemBrowseEndpointRequiresAuthAndListsOnlyDirectories(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	server.SetFilesystemBrowseHandler(func(ctx context.Context, req FilesystemBrowseRequest) (FilesystemBrowseResponse, error) {
		if req.Path != "/srv/share" {
			t.Fatalf("unexpected browse path: %+v", req)
		}
		return FilesystemBrowseResponse{
			Path: "/srv/share",
			Entries: []FilesystemBrowseEntry{
				{Name: "Documents", Path: "/srv/share/Documents", Type: "directory", Readable: true},
				{Name: "movie.mkv", Path: "/srv/share/movie.mkv", Type: "file", Readable: true},
			},
		}, nil
	})

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/filesystem/browse?path=/srv/share", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/filesystem/browse?path=/srv/share", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("filesystem browse code = %d body=%s", ok.Code, ok.Body.String())
	}
	body := ok.Body.String()
	for _, want := range []string{`"path":"/srv/share"`, `"name":"Documents"`, `"type":"directory"`, `"readable":true`} {
		if !strings.Contains(body, want) {
			t.Fatalf("filesystem browse response missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, "movie.mkv") {
		t.Fatalf("filesystem browse should omit files for folder picker safety: %s", body)
	}
}

func TestMeshSettingsEndpointListsPerNodeDocumentsWithoutSecrets(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	server.SetMeshSettingsHandler(func(ctx context.Context, req MeshSettingsRequest) (MeshSettingsResponse, error) {
		if req.NodeID != "node-b" {
			t.Fatalf("unexpected mesh settings request: %+v", req)
		}
		return MeshSettingsResponse{Documents: []state.NodeSettingsDocument{{
			NodeID:      "node-b",
			Revision:    4,
			UpdatedAt:   "2026-05-31T12:00:00Z",
			Settings:    map[string]any{"logging.level": "warn"},
			Source:      "identity-mesh-cache",
			ApplyStatus: "cached-read-only",
		}}}, nil
	})

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/mesh/settings?nodeId=node-b", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/mesh/settings?nodeId=node-b", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("mesh settings code = %d body=%s", ok.Code, ok.Body.String())
	}
	body := ok.Body.String()
	for _, want := range []string{`"nodeId":"node-b"`, `"revision":4`, `"applyStatus":"cached-read-only"`, `"logging.level":"warn"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("mesh settings response missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, "api-secret") || strings.Contains(body, "private") {
		t.Fatalf("mesh settings response leaked secret-looking value: %s", body)
	}
}

func TestMeshSettingsCommandEndpointQueuesPendingRemoteChangeWithoutSecrets(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	var captured MeshSettingsCommandRequest
	server.SetMeshSettingsCommandHandler(func(ctx context.Context, req MeshSettingsCommandRequest) (MeshSettingsCommandResponse, error) {
		captured = req
		return MeshSettingsCommandResponse{
			Action:         req.Action,
			Status:         "queued",
			ChangeID:       "change-123",
			TargetNodeID:   req.TargetNodeID,
			OriginNodeID:   req.OriginNodeID,
			IdempotencyKey: req.IdempotencyKey,
		}, nil
	})

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/mesh/settings-command", strings.NewReader(`{"action":"queue"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	reqBody := `{"action":"queue","targetNodeId":"node-b","originNodeId":"node-a","idempotencyKey":"node-a:node-b:1","settingsPatch":{"logging.level":"warn","transfer.receiveBytesPerSecond":2048}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/mesh/settings-command", strings.NewReader(reqBody))
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("mesh settings command code = %d body=%s", ok.Code, ok.Body.String())
	}
	if captured.Action != "queue" || captured.TargetNodeID != "node-b" || captured.OriginNodeID != "node-a" || captured.IdempotencyKey != "node-a:node-b:1" || captured.SettingsPatch["logging.level"] != "warn" {
		t.Fatalf("unexpected mesh settings command request: %+v", captured)
	}
	response := ok.Body.String()
	for _, want := range []string{`"status":"queued"`, `"changeId":"change-123"`, `"targetNodeId":"node-b"`, `"originNodeId":"node-a"`} {
		if !strings.Contains(response, want) {
			t.Fatalf("mesh settings command response missing %s: %s", want, response)
		}
	}
	events := httptest.NewRecorder()
	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	eventsReq.Header.Set("X-FSE-API-Key", "secret")
	server.Router().ServeHTTP(events, eventsReq)
	if !strings.Contains(events.Body.String(), "mesh.settings.command.queued") {
		t.Fatalf("mesh settings command event missing: %s", events.Body.String())
	}
	if strings.Contains(events.Body.String(), "logging.level") || strings.Contains(events.Body.String(), "warn") {
		t.Fatalf("mesh settings command event leaked patch values: %s", events.Body.String())
	}
}

func TestIdentityImportEndpointExecutesDaemonOwnedImportWithoutLeakingSecrets(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	server.SetIdentityImportHandler(func(ctx context.Context, req IdentityImportRequest) (IdentityImportResponse, error) {
		if req.Package.GroupID != "family-sync" || req.Package.DiscoveryID != "remote-public" || req.Package.BootstrapProofKey != strings.Repeat("p", 80) {
			t.Fatalf("unexpected identity import request: %+v", req)
		}
		return IdentityImportResponse{
			Status:                       "accepted",
			Message:                      "identity import accepted; dedicated peer-pair key negotiation queued",
			GroupID:                      "family-sync",
			RemoteDiscoveryID:            "remote-public",
			IntroductionEncryptionLevel:  10,
			PeerPairEncryptionLevel:      4,
			RequiresDedicatedPeerPairKey: true,
			UsesBootstrapKeyForTraffic:   false,
			PairID:                       "pair-123",
			KeyID:                        "key-456",
		}, nil
	})

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/identity-import", strings.NewReader(`{"package":{"groupId":"family-sync"}}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	body := fmt.Sprintf(`{"package":{"version":"%s","discoveryId":"remote-public","groupId":"family-sync","bootstrapProofKey":"%s","bootstrapEncryptionLevel":10,"defaultPeerEncryptionLevel":4}}`, pairing.IdentityPackageVersion, strings.Repeat("p", 80))
	req := httptest.NewRequest(http.MethodPost, "/v1/identity-import", strings.NewReader(body))
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("identity import code = %d body=%s", ok.Code, ok.Body.String())
	}
	response := ok.Body.String()
	for _, want := range []string{`"status":"accepted"`, `"remoteDiscoveryId":"remote-public"`, `"introductionEncryptionLevel":10`, `"peerPairEncryptionLevel":4`, `"requiresDedicatedPeerPairKey":true`, `"usesBootstrapKeyForTraffic":false`, `"pairId":"pair-123"`, `"keyId":"key-456"`} {
		if !strings.Contains(response, want) {
			t.Fatalf("identity import response missing %s: %s", want, response)
		}
	}
	for _, leaked := range []string{strings.Repeat("p", 80), "bootstrapProofKey", "SecretKey"} {
		if strings.Contains(response, leaked) {
			t.Fatalf("identity import response leaked secret %q: %s", leaked, response)
		}
	}

	events := httptest.NewRecorder()
	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	eventsReq.Header.Set("X-FSE-API-Key", "secret")
	server.Router().ServeHTTP(events, eventsReq)
	if strings.Contains(events.Body.String(), strings.Repeat("p", 80)) {
		t.Fatalf("identity import event leaked bootstrap proof: %s", events.Body.String())
	}
	if !strings.Contains(events.Body.String(), "identity.package.imported") {
		t.Fatalf("identity import event missing: %s", events.Body.String())
	}
}

func TestConfigEndpointReadsRedactedConfigAndRunsNonSecretUpdateHandler(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	server.SetConfigReadHandler(func(ctx context.Context) (config.Config, error) {
		return config.Config{
			NodeName: "node-a",
			API:      config.APIConfig{Listen: "127.0.0.1:0", Key: "super-secret"},
			Identity: config.IdentityConfig{PrivateKey: "identity-secret", PublicKey: "public"},
			Peers:    []config.PeerConfig{{ID: "peer-a", APIKey: "peer-secret"}},
		}, nil
	})
	var patch ConfigUpdateRequest
	server.SetConfigUpdateHandler(func(ctx context.Context, req ConfigUpdateRequest) (ConfigUpdateResponse, error) {
		patch = req
		return ConfigUpdateResponse{Status: "accepted", Message: "config update accepted; daemon hot reload will adopt the config change"}, nil
	})

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/config", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	readReq := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	readReq.Header.Set("X-FSE-API-Key", "secret")
	readResp := httptest.NewRecorder()
	server.Router().ServeHTTP(readResp, readReq)
	if readResp.Code != http.StatusOK {
		t.Fatalf("read code = %d body=%s", readResp.Code, readResp.Body.String())
	}
	body := readResp.Body.String()
	for _, want := range []string{`"nodeName": "node-a"`, `"key": "[REDACTED]"`, `"privateKey": "[REDACTED]"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("redacted config missing %s: %s", want, body)
		}
	}
	for _, leaked := range []string{"super-secret", "identity-secret", "peer-secret"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("config response leaked secret %q: %s", leaked, body)
		}
	}

	badMethod := httptest.NewRecorder()
	badMethodReq := httptest.NewRequest(http.MethodDelete, "/v1/config", nil)
	badMethodReq.Header.Set("X-FSE-API-Key", "secret")
	server.Router().ServeHTTP(badMethod, badMethodReq)
	if badMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("bad method code = %d", badMethod.Code)
	}

	updateReq := httptest.NewRequest(http.MethodPatch, "/v1/config", strings.NewReader(`{"nodeName":"node-b","logging":{"level":"warn"},"transfer":{"sendBytesPerSecond":2048}}`))
	updateReq.Header.Set("X-FSE-API-Key", "secret")
	updateResp := httptest.NewRecorder()
	server.Router().ServeHTTP(updateResp, updateReq)
	if updateResp.Code != http.StatusOK {
		t.Fatalf("update code = %d body=%s", updateResp.Code, updateResp.Body.String())
	}
	if patch.NodeName == nil || *patch.NodeName != "node-b" || patch.Logging == nil || patch.Logging.Level != config.LogLevelWarn || patch.Transfer == nil || patch.Transfer.SendBytesPerSecond != 2048 {
		t.Fatalf("unexpected config update patch: %+v", patch)
	}
	if strings.Contains(updateResp.Body.String(), "secret") {
		t.Fatalf("config update response should not echo secrets: %s", updateResp.Body.String())
	}
	var decoded ConfigUpdateResponse
	if err := json.Unmarshal(updateResp.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if decoded.Status != "accepted" {
		t.Fatalf("unexpected update response: %+v", decoded)
	}

	forbidden := httptest.NewRequest(http.MethodPatch, "/v1/config", strings.NewReader(`{"api":{"key":"new-secret"}}`))
	forbidden.Header.Set("X-FSE-API-Key", "secret")
	forbiddenResp := httptest.NewRecorder()
	server.Router().ServeHTTP(forbiddenResp, forbidden)
	if forbiddenResp.Code != http.StatusBadRequest {
		t.Fatalf("secret-bearing config update code = %d body=%s", forbiddenResp.Code, forbiddenResp.Body.String())
	}
	if strings.Contains(forbiddenResp.Body.String(), "new-secret") {
		t.Fatalf("secret-bearing config update error leaked secret: %s", forbiddenResp.Body.String())
	}
}

func TestEventsStreamsSnapshotJSONEventsWithAPIKey(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	server.Publish(Event{Type: "hash.finished", FolderID: "docs", Path: "seeded.txt", Progress: &ProgressState{QueuedHashJobs: 1, CompletedHashJobs: 1, DateCorrectionsPending: 1}})
	req := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("events code = %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content type = %q", got)
	}
	body := w.Body.String()
	for _, want := range []string{"event: hash.finished", `"folderID":"docs"`, `"path":"seeded.txt"`, `"completedHashJobs":1`, `"dateCorrectionsPending":1`} {
		if !strings.Contains(body, want) {
			t.Fatalf("events body missing %s: %s", want, body)
		}
	}
}

func TestTransfersReturnsCurrentScopesAndBoundedHistory(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	server.Publish(Event{Type: "peer.sync.started", FolderID: "docs", PeerID: "peer-a", Message: "peer transfer pass started"})
	server.Publish(Event{Type: "sync.finished", FolderID: "photos", Message: "targets=1 writes=2 deletes=0 moves=0 reusedBlocks=3"})
	server.Publish(Event{Type: "peer.sync.finished", FolderID: "docs", PeerID: "peer-a", Message: "writes=1 deletes=0 blocksFetched=2"})
	server.Publish(Event{Type: "peer.sync.started", FolderID: "media", PeerID: "peer-b", Message: "peer transfer pass started"})

	req := httptest.NewRequest(http.MethodGet, "/v1/transfers?limit=2", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var response TransferReadModel
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode transfer read model: %v", err)
	}
	if response.LiveRatesAvailable || response.ByteProgressAvailable {
		t.Fatalf("unsupported rate/byte metrics advertised: %+v", response)
	}
	if len(response.Active) != 1 || response.Active[0].FolderID != "media" || response.Active[0].PeerID != "peer-b" || response.Active[0].Status != "active" {
		t.Fatalf("active transfers = %+v", response.Active)
	}
	if len(response.History) != 2 || response.History[0].FolderID != "docs" || response.History[0].Status != "completed" || response.History[1].FolderID != "photos" {
		t.Fatalf("bounded newest-first history = %+v", response.History)
	}
}

func TestTransfersRejectsInvalidLimitAndMethod(t *testing.T) {
	server := NewServer(State{}, "secret")
	for _, tc := range []struct {
		method string
		path   string
		want   int
	}{{http.MethodGet, "/v1/transfers?limit=0", http.StatusBadRequest}, {http.MethodPost, "/v1/transfers", http.StatusMethodNotAllowed}} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("X-FSE-API-Key", "secret")
		w := httptest.NewRecorder()
		server.Router().ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("%s %s status=%d want=%d body=%s", tc.method, tc.path, w.Code, tc.want, w.Body.String())
		}
	}
}

func TestEventsEndpointStaysOpenForRealtimePublishes(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	httpServer := httptest.NewServer(server.Router())
	defer httpServer.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-FSE-API-Key", "secret")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	lines := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	server.Publish(Event{Type: "scan.finished", FolderID: "docs"})
	deadline := time.After(2 * time.Second)
	for {
		select {
		case line := <-lines:
			if strings.Contains(line, "scan.finished") {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for realtime event")
		}
	}
}

func TestWebGUICommandEndpointRequiresPostInvokesHandlerAndPublishesEvent(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	var got WebGUICommandRequest
	server.SetWebGUICommandHandler(func(ctx context.Context, req WebGUICommandRequest) (WebGUICommandResponse, error) {
		got = req
		return WebGUICommandResponse{Action: req.Action, Status: "installed", Version: "1.2.3", Message: "web GUI installed"}, nil
	})

	badMethod := httptest.NewRecorder()
	badMethodReq := httptest.NewRequest(http.MethodGet, "/v1/web-gui-command", nil)
	badMethodReq.Header.Set("X-FSE-API-Key", "secret")
	server.Router().ServeHTTP(badMethod, badMethodReq)
	if badMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("bad method code = %d", badMethod.Code)
	}

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/web-gui-command", strings.NewReader(`{"action":"install"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/web-gui-command", strings.NewReader(`{"action":"install"}`))
	req.Header.Set("X-FSE-API-Key", "secret")
	resp := httptest.NewRecorder()
	server.Router().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("web gui command code = %d body=%s", resp.Code, resp.Body.String())
	}
	if got.Action != "install" {
		t.Fatalf("handler request = %+v", got)
	}
	if !strings.Contains(resp.Body.String(), `"status":"installed"`) || !strings.Contains(resp.Body.String(), `"version":"1.2.3"`) {
		t.Fatalf("unexpected web gui response: %s", resp.Body.String())
	}
	if len(server.events) != 1 || server.events[0].Type != "webgui.command.finished" || strings.Contains(server.events[0].Message, "secret") {
		t.Fatalf("web gui event missing/redaction issue: %+v", server.events)
	}
}

func TestStopEndpointRequiresPostInvokesHandlerAndMarksStopping(t *testing.T) {
	server := NewServer(State{NodeName: "node-a", Status: "running"}, "secret")
	called := false
	server.SetStopHandler(func(ctx context.Context) error {
		called = true
		return nil
	})

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/stop", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	badMethod := httptest.NewRecorder()
	badMethodReq := httptest.NewRequest(http.MethodGet, "/v1/stop", nil)
	badMethodReq.Header.Set("X-FSE-API-Key", "secret")
	server.Router().ServeHTTP(badMethod, badMethodReq)
	if badMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("bad method code = %d", badMethod.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/stop", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("stop code = %d body=%s", ok.Code, ok.Body.String())
	}
	if !called {
		t.Fatalf("stop handler was not called")
	}
	if state := server.CurrentState(); state.Status != "stopping" {
		t.Fatalf("status = %q, want stopping", state.Status)
	}
	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	eventsReq.Header.Set("X-FSE-API-Key", "secret")
	events := httptest.NewRecorder()
	server.Router().ServeHTTP(events, eventsReq)
	if !strings.Contains(events.Body.String(), "daemon.stopping") {
		t.Fatalf("stop event not published: %s", events.Body.String())
	}
}

func TestMaintenanceScrubEndpointRequiresPostRunsHandlerAndUpdatesStatus(t *testing.T) {
	server := NewServer(State{NodeName: "node-a", Maintenance: MaintenanceState{Enabled: true}}, "secret")
	server.SetMaintenanceScrubHandler(func(ctx context.Context, req MaintenanceScrubRequest) (MaintenanceScrubResponse, error) {
		if req.FolderID != "docs" {
			t.Fatalf("folder id = %q", req.FolderID)
		}
		return MaintenanceScrubResponse{Folders: 1, Results: []MaintenanceScrubFolderResult{{FolderID: "docs", Mode: "full-blocks", FilesScanned: 2, BytesScanned: 128, Reported: 1, Quarantined: 0, Complete: true, Cursor: 3}}}, nil
	})

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/maintenance/scrub", strings.NewReader(`{"folderId":"docs"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	badMethod := httptest.NewRecorder()
	badMethodReq := httptest.NewRequest(http.MethodGet, "/v1/maintenance/scrub", nil)
	badMethodReq.Header.Set("X-FSE-API-Key", "secret")
	server.Router().ServeHTTP(badMethod, badMethodReq)
	if badMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("bad method code = %d", badMethod.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/maintenance/scrub", strings.NewReader(`{"folderId":"docs"}`))
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("scrub code = %d body=%s", ok.Code, ok.Body.String())
	}
	body := ok.Body.String()
	for _, want := range []string{`"folders":1`, `"folderId":"docs"`, `"filesScanned":2`, `"reported":1`, `"cursor":3`} {
		if !strings.Contains(body, want) {
			t.Fatalf("scrub response missing %s: %s", want, body)
		}
	}
	state := server.CurrentState()
	if state.Maintenance.LastManualScrub == nil || state.Maintenance.LastManualScrub.Reported != 1 || state.Maintenance.LastManualScrub.Folders != 1 {
		t.Fatalf("maintenance status not updated: %+v", state.Maintenance)
	}
	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	eventsReq.Header.Set("X-FSE-API-Key", "secret")
	events := httptest.NewRecorder()
	server.Router().ServeHTTP(events, eventsReq)
	if !strings.Contains(events.Body.String(), "maintenance.scrub.finished") {
		t.Fatalf("maintenance event not published: %s", events.Body.String())
	}
}

func TestBackupJobsEndpointListsDurableOperationJobs(t *testing.T) {
	server := NewServer(State{NodeName: "node-a", Backup: BackupState{Enabled: true}}, "secret")
	server.SetBackupJobsHandler(func(ctx context.Context, req BackupJobsRequest) (BackupJobsResponse, error) {
		if req.SnapshotID != "snap-001" {
			t.Fatalf("snapshot filter = %q", req.SnapshotID)
		}
		return BackupJobsResponse{
			RestoreJobs:   []state.BackupRestoreJob{{ID: "restore-1", SnapshotID: "snap-001", FolderID: "docs", Status: "completed", TotalFiles: 2, RestoredFiles: 1, SkippedFiles: 1}},
			RetentionJobs: []state.BackupRetentionJob{{ID: "retention-1", Status: "running", KeepLast: 2, TotalOperations: 5, RemainingOperations: 3}},
			RepairJobs:    []state.BackupRepairJob{{ID: "repair-1", Status: "waiting", TotalBlocks: 4, RemainingBlocks: 4}},
		}, nil
	})

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/backup/jobs?snapshotId=snap-001", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	badMethodReq := httptest.NewRequest(http.MethodPost, "/v1/backup/jobs", nil)
	badMethodReq.Header.Set("X-FSE-API-Key", "secret")
	badMethod := httptest.NewRecorder()
	server.Router().ServeHTTP(badMethod, badMethodReq)
	if badMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("bad method code = %d", badMethod.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/backup/jobs?snapshotId=snap-001", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	resp := httptest.NewRecorder()
	server.Router().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("jobs code = %d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	for _, want := range []string{`"restoreJobs":[`, `"id":"restore-1"`, `"retentionJobs":[`, `"id":"retention-1"`, `"repairJobs":[`, `"id":"repair-1"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("jobs response missing %s: %s", want, body)
		}
	}
}

func TestBackupScrubEndpointRunsHandlerUpdatesStatusAndPublishesEvent(t *testing.T) {
	server := NewServer(State{NodeName: "node-a", Backup: BackupState{Enabled: true}}, "secret")
	server.SetBackupScrubHandler(func(ctx context.Context, req BackupScrubRequest) (BackupScrubResponse, error) {
		return BackupScrubResponse{
			Archive:     BackupArchiveScrubState{CheckedJobs: 2, ProtectedBlocks: 1, MissingBlocks: 1, Issues: 1},
			Checkpoints: BackupCheckpointScrubState{CheckedSnapshots: 1, MissingCheckpoints: 1, DegradedSnapshots: 1, Issues: 2},
			RepairPlan:  BackupRepairPlanState{RepairableBlocks: 1, UnresolvedBlocks: 1},
		}, nil
	})

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/backup/scrub", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}
	badMethod := httptest.NewRecorder()
	badMethodReq := httptest.NewRequest(http.MethodGet, "/v1/backup/scrub", nil)
	badMethodReq.Header.Set("X-FSE-API-Key", "secret")
	server.Router().ServeHTTP(badMethod, badMethodReq)
	if badMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("bad method code = %d", badMethod.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/backup/scrub", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("backup scrub code = %d body=%s", ok.Code, ok.Body.String())
	}
	body := ok.Body.String()
	for _, want := range []string{`"checkedJobs":2`, `"missingBlocks":1`, `"checkedSnapshots":1`, `"degradedSnapshots":1`, `"repairableBlocks":1`, `"unresolvedBlocks":1`} {
		if !strings.Contains(body, want) {
			t.Fatalf("backup scrub response missing %s: %s", want, body)
		}
	}
	state := server.CurrentState()
	if state.Backup.LastScrub == nil || state.Backup.LastScrub.ArchiveMissingBlocks != 1 || state.Backup.LastScrub.DegradedSnapshots != 1 || state.Backup.LastScrub.RepairableBlocks != 1 {
		t.Fatalf("backup scrub status not updated: %+v", state.Backup)
	}
	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	eventsReq.Header.Set("X-FSE-API-Key", "secret")
	events := httptest.NewRecorder()
	server.Router().ServeHTTP(events, eventsReq)
	if !strings.Contains(events.Body.String(), "backup.scrub.finished") || !strings.Contains(events.Body.String(), "repairable=1") {
		t.Fatalf("backup scrub event not published: %s", events.Body.String())
	}
}

func TestSnapshotEndpointRunsHandler(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	server.SetSnapshotHandler(func(ctx context.Context, req SnapshotRequest) (SnapshotResponse, error) {
		if req.Action != "create" || req.FolderID != "docs" || req.Description != "before cleanup" {
			t.Fatalf("unexpected snapshot request: %+v", req)
		}
		return SnapshotResponse{Markers: []SnapshotMarker{{ID: "snap-001", FolderID: "docs", Cursor: 4, StateHash: "hash", CreatedAt: "2026-05-24T12:00:00Z"}}}, nil
	})
	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/snapshots", strings.NewReader(`{"action":"create","folderId":"docs"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/snapshots", strings.NewReader(`{"action":"create","folderId":"docs","description":"before cleanup"}`))
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("snapshot code = %d body=%s", ok.Code, ok.Body.String())
	}
	body := ok.Body.String()
	for _, want := range []string{`"id":"snap-001"`, `"folderId":"docs"`, `"cursor":4`, `"stateHash":"hash"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("snapshot response missing %s: %s", want, body)
		}
	}
}

func TestRestorePlanEndpointRunsHandler(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	server.SetRestorePlanHandler(func(ctx context.Context, req RestorePlanRequest) (RestorePlanResponse, error) {
		if req.SnapshotID != "snap-001" || len(req.Paths) != 1 || req.Paths[0] != "dir/alpha.txt" || req.DestinationRoot != "/tmp/restore" || req.AlternatePath != "restored/alpha.txt" {
			t.Fatalf("unexpected restore plan request: %+v", req)
		}
		return RestorePlanResponse{SnapshotID: "snap-001", FolderID: "docs", Destination: "/tmp/restore", DryRun: true, TotalFiles: 1, TotalBytes: 5, Files: []RestorePlanFile{{Path: "dir/alpha.txt", DestinationPath: "/tmp/restore/restored/alpha.txt", Size: 5, Blocks: 1, ArchiveAvailable: true}}}, nil
	})
	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/restore-plans", strings.NewReader(`{"snapshotId":"snap-001"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/restore-plans", strings.NewReader(`{"snapshotId":"snap-001","paths":["dir/alpha.txt"],"destinationRoot":"/tmp/restore","alternatePath":"restored/alpha.txt"}`))
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("restore plan code = %d body=%s", ok.Code, ok.Body.String())
	}
	body := ok.Body.String()
	for _, want := range []string{`"snapshotId":"snap-001"`, `"folderId":"docs"`, `"dryRun":true`, `"archiveAvailable":true`} {
		if !strings.Contains(body, want) {
			t.Fatalf("restore plan response missing %s: %s", want, body)
		}
	}
}

func TestSnapshotRetentionEndpointRunsHandlerAndPublishesEvent(t *testing.T) {
	server := NewServer(State{NodeName: "node-a", Backup: BackupState{Enabled: true}}, "secret")
	server.SetSnapshotRetentionHandler(func(ctx context.Context, req SnapshotRetentionRequest) (SnapshotRetentionResponse, error) {
		if req.KeepLast != 2 {
			t.Fatalf("unexpected retention request: %+v", req)
		}
		return SnapshotRetentionResponse{JobID: "retention-job-1", KeepLast: 2, DeprecatedSnapshots: []string{"snap-old"}, DeletedSnapshots: []string{"snap-older"}, PromotedManifests: 1, SweepEligibleBlocks: 3}, nil
	})
	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/snapshot-retention", strings.NewReader(`{"keepLast":2}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/snapshot-retention", strings.NewReader(`{"keepLast":2}`))
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("retention code = %d body=%s", ok.Code, ok.Body.String())
	}
	body := ok.Body.String()
	for _, want := range []string{`"jobId":"retention-job-1"`, `"keepLast":2`, `"deprecatedSnapshots":["snap-old"]`, `"deletedSnapshots":["snap-older"]`, `"promotedManifests":1`, `"sweepEligibleBlocks":3`} {
		if !strings.Contains(body, want) {
			t.Fatalf("retention response missing %s: %s", want, body)
		}
	}
	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	eventsReq.Header.Set("X-FSE-API-Key", "secret")
	events := httptest.NewRecorder()
	server.Router().ServeHTTP(events, eventsReq)
	if !strings.Contains(events.Body.String(), "snapshot.retention.finished") || !strings.Contains(events.Body.String(), "deleted=1") {
		t.Fatalf("retention event not published: %s", events.Body.String())
	}
}

func TestRestoreEndpointRunsHandlerUpdatesStatusAndPublishesEvent(t *testing.T) {
	server := NewServer(State{NodeName: "node-a", Backup: BackupState{Enabled: true}}, "secret")
	server.SetRestoreHandler(func(ctx context.Context, req RestoreRequest) (RestoreResponse, error) {
		if req.SnapshotID != "snap-001" || len(req.Paths) != 1 || req.Paths[0] != "dir/alpha.txt" || req.DestinationRoot != "/tmp/restore" || req.AlternatePath != "restored/alpha.txt" {
			t.Fatalf("unexpected restore request: %+v", req)
		}
		return RestoreResponse{SnapshotID: "snap-001", FolderID: "docs", Destination: "/tmp/restore", TotalFiles: 2, RestoredFiles: 1, RestoredBytes: 5, SkippedFiles: 1, RemainingFiles: 0}, nil
	})
	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/restores", strings.NewReader(`{"snapshotId":"snap-001"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/restores", strings.NewReader(`{"snapshotId":"snap-001","paths":["dir/alpha.txt"],"destinationRoot":"/tmp/restore","alternatePath":"restored/alpha.txt"}`))
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("restore code = %d body=%s", ok.Code, ok.Body.String())
	}
	body := ok.Body.String()
	for _, want := range []string{`"snapshotId":"snap-001"`, `"folderId":"docs"`, `"totalFiles":2`, `"restoredFiles":1`, `"restoredBytes":5`, `"skippedFiles":1`, `"remainingFiles":0`} {
		if !strings.Contains(body, want) {
			t.Fatalf("restore response missing %s: %s", want, body)
		}
	}
	state := server.CurrentState()
	if state.Backup.LastRestore == nil || state.Backup.LastRestore.SnapshotID != "snap-001" || state.Backup.LastRestore.TotalFiles != 2 || state.Backup.LastRestore.RestoredFiles != 1 || state.Backup.LastRestore.SkippedFiles != 1 || state.Backup.LastRestore.RemainingFiles != 0 {
		t.Fatalf("restore status not updated: %+v", state.Backup)
	}
	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	eventsReq.Header.Set("X-FSE-API-Key", "secret")
	events := httptest.NewRecorder()
	server.Router().ServeHTTP(events, eventsReq)
	if !strings.Contains(events.Body.String(), "snapshot.restore.finished") || !strings.Contains(events.Body.String(), "files=2") || !strings.Contains(events.Body.String(), "remaining=0") {
		t.Fatalf("restore event not published: %s", events.Body.String())
	}
}

func TestRestoreEndpointRejectsDatabaseReversionThroughOrdinaryRestore(t *testing.T) {
	server := NewServer(State{NodeName: "node-a", Backup: BackupState{Enabled: true}}, "secret")
	called := false
	server.SetRestoreHandler(func(ctx context.Context, req RestoreRequest) (RestoreResponse, error) {
		called = true
		return RestoreResponse{}, nil
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/restores", strings.NewReader(`{"snapshotId":"snap-001","revertDatabase":true}`))
	req.Header.Set("X-FSE-API-Key", "secret")
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("restore reversion code = %d body=%s", w.Code, w.Body.String())
	}
	if called {
		t.Fatalf("restore handler must not run for database reversion requests")
	}
	if !strings.Contains(w.Body.String(), "database reversion requires a dedicated rollback flow") {
		t.Fatalf("restore reversion error should explain rollback boundary: %s", w.Body.String())
	}
}

func TestPeerCommandEndpointRequiresPostAndRunsHandler(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	server.SetPeerCommandHandler(func(ctx context.Context, req PeerCommandRequest) (PeerCommandResponse, error) {
		if req.Action != "add" || req.ID != "peer-a" || req.Endpoint != "manual:http://127.0.0.1:22000" {
			t.Fatalf("unexpected peer command request: %+v", req)
		}
		return PeerCommandResponse{Action: req.Action, ID: req.ID, Status: "accepted", Message: "peer add accepted"}, nil
	})

	badMethod := httptest.NewRecorder()
	badMethodReq := httptest.NewRequest(http.MethodGet, "/v1/peer-command", nil)
	badMethodReq.Header.Set("X-FSE-API-Key", "secret")
	server.Router().ServeHTTP(badMethod, badMethodReq)
	if badMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("bad method code = %d", badMethod.Code)
	}

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/peer-command", strings.NewReader(`{"action":"add","id":"peer-a"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/peer-command", strings.NewReader(`{"action":"add","id":"peer-a","endpoint":"manual:http://127.0.0.1:22000"}`))
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("peer command code = %d body=%s", ok.Code, ok.Body.String())
	}
	body := ok.Body.String()
	for _, want := range []string{`"action":"add"`, `"id":"peer-a"`, `"status":"accepted"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("peer command response missing %s: %s", want, body)
		}
	}
	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	eventsReq.Header.Set("X-FSE-API-Key", "secret")
	events := httptest.NewRecorder()
	server.Router().ServeHTTP(events, eventsReq)
	if !strings.Contains(events.Body.String(), "peer.command.finished") || strings.Contains(events.Body.String(), "127.0.0.1:22000") {
		t.Fatalf("peer command event missing or leaked endpoint: %s", events.Body.String())
	}
}

func TestFolderCommandEndpointRequiresPostAndRunsHandler(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	server.SetFolderCommandHandler(func(ctx context.Context, req FolderCommandRequest) (FolderCommandResponse, error) {
		if req.Action != "update" || req.ID != "docs" || req.Path != "/srv/docs" || req.Mode != "sendonly" {
			t.Fatalf("unexpected folder command request: %+v", req)
		}
		return FolderCommandResponse{Action: req.Action, ID: req.ID, Status: "accepted", Message: "folder update accepted"}, nil
	})

	badMethod := httptest.NewRecorder()
	badMethodReq := httptest.NewRequest(http.MethodGet, "/v1/folder-command", nil)
	badMethodReq.Header.Set("X-FSE-API-Key", "secret")
	server.Router().ServeHTTP(badMethod, badMethodReq)
	if badMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("bad method code = %d", badMethod.Code)
	}

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/folder-command", strings.NewReader(`{"action":"update","id":"docs"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/folder-command", strings.NewReader(`{"action":"update","id":"docs","path":"/srv/docs","mode":"sendonly"}`))
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("folder command code = %d body=%s", ok.Code, ok.Body.String())
	}
	body := ok.Body.String()
	for _, want := range []string{`"action":"update"`, `"id":"docs"`, `"status":"accepted"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("folder command response missing %s: %s", want, body)
		}
	}
	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	eventsReq.Header.Set("X-FSE-API-Key", "secret")
	events := httptest.NewRecorder()
	server.Router().ServeHTTP(events, eventsReq)
	if !strings.Contains(events.Body.String(), "folder.command.finished") || strings.Contains(events.Body.String(), "/srv/docs") {
		t.Fatalf("folder command event missing or leaked path: %s", events.Body.String())
	}
}

func TestServiceCommandEndpointRequiresPostAndRunsHandler(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	server.SetServiceCommandHandler(func(ctx context.Context, req ServiceCommandRequest) (ServiceCommandResponse, error) {
		if req.Action != "restart" || req.Platform != "systemd" || req.ServiceName != "fse" || req.Domain != "system" {
			t.Fatalf("unexpected service command request: %+v", req)
		}
		return ServiceCommandResponse{Action: req.Action, Platform: req.Platform, ServiceName: req.ServiceName, Status: "accepted", Message: "review service restart", Handoff: "systemctl restart fse"}, nil
	})

	badMethod := httptest.NewRecorder()
	badMethodReq := httptest.NewRequest(http.MethodGet, "/v1/service-command", nil)
	badMethodReq.Header.Set("X-FSE-API-Key", "secret")
	server.Router().ServeHTTP(badMethod, badMethodReq)
	if badMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("bad method code = %d", badMethod.Code)
	}

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/service-command", strings.NewReader(`{"action":"restart","platform":"systemd","serviceName":"fse"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/service-command", strings.NewReader(`{"action":"restart","platform":"systemd","serviceName":"fse","domain":"system"}`))
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("service command code = %d body=%s", ok.Code, ok.Body.String())
	}
	body := ok.Body.String()
	for _, want := range []string{`"action":"restart"`, `"platform":"systemd"`, `"serviceName":"fse"`, `"status":"accepted"`, `"handoff":"systemctl restart fse"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("service command response missing %s: %s", want, body)
		}
	}
	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	eventsReq.Header.Set("X-FSE-API-Key", "secret")
	events := httptest.NewRecorder()
	server.Router().ServeHTTP(events, eventsReq)
	if !strings.Contains(events.Body.String(), "service.command.finished") || strings.Contains(events.Body.String(), "systemctl restart fse") {
		t.Fatalf("service command event missing or leaked handoff: %s", events.Body.String())
	}
}

func TestTransferCommandEndpointRequiresPostAndRunsHandler(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	server.SetTransferCommandHandler(func(ctx context.Context, req TransferCommandRequest) (TransferCommandResponse, error) {
		if req.Action != "pause" || req.FolderID != "docs" || req.PeerID != "peer-a" {
			t.Fatalf("unexpected transfer command request: %+v", req)
		}
		return TransferCommandResponse{Action: req.Action, FolderID: req.FolderID, PeerID: req.PeerID, Status: "accepted", Message: "transfer pause accepted"}, nil
	})

	badMethod := httptest.NewRecorder()
	badMethodReq := httptest.NewRequest(http.MethodGet, "/v1/transfer-command", nil)
	badMethodReq.Header.Set("X-FSE-API-Key", "secret")
	server.Router().ServeHTTP(badMethod, badMethodReq)
	if badMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("bad method code = %d", badMethod.Code)
	}

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/transfer-command", strings.NewReader(`{"action":"pause","folderID":"docs"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/transfer-command", strings.NewReader(`{"action":"pause","folderID":"docs","peerID":"peer-a"}`))
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("transfer command code = %d body=%s", ok.Code, ok.Body.String())
	}
	body := ok.Body.String()
	for _, want := range []string{`"action":"pause"`, `"folderID":"docs"`, `"peerID":"peer-a"`, `"status":"accepted"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("transfer command response missing %s: %s", want, body)
		}
	}
	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	eventsReq.Header.Set("X-FSE-API-Key", "secret")
	events := httptest.NewRecorder()
	server.Router().ServeHTTP(events, eventsReq)
	if !strings.Contains(events.Body.String(), "transfer.command.finished") {
		t.Fatalf("transfer command event missing: %s", events.Body.String())
	}
}

func TestDiscoveryCommandEndpointRequiresPostAndRunsHandler(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	server.SetDiscoveryCommandHandler(func(ctx context.Context, req DiscoveryCommandRequest) (DiscoveryCommandResponse, error) {
		if req.Action != "update" || !req.Disabled || req.DHT || req.Local || req.DHTNamespace != "fse-test" || len(req.DHTBootstrapPeers) != 1 || req.DHTBootstrapPeers[0] != "/dnsaddr/bootstrap.libp2p.io" || len(req.NetworkHints.LocalContainerGatewayIPs) != 1 || req.NetworkHints.LocalContainerGatewayIPs[0] != "172.17.0.1" || len(req.NetworkHints.LocalCIDRs) != 1 || req.NetworkHints.LocalCIDRs[0] != "172.18.0.0/16" || len(req.NetworkHints.PublishedPortMappings) != 1 || req.NetworkHints.PublishedPortMappings[0].HostIP != "172.18.0.1" || req.NetworkHints.PublishedPortMappings[0].HostPort != 32200 {
			t.Fatalf("unexpected discovery command request: %+v", req)
		}
		return DiscoveryCommandResponse{Action: req.Action, Status: "accepted", Message: "discovery update accepted"}, nil
	})

	badMethod := httptest.NewRecorder()
	badMethodReq := httptest.NewRequest(http.MethodGet, "/v1/discovery-command", nil)
	badMethodReq.Header.Set("X-FSE-API-Key", "secret")
	server.Router().ServeHTTP(badMethod, badMethodReq)
	if badMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("bad method code = %d", badMethod.Code)
	}

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/v1/discovery-command", strings.NewReader(`{"action":"update"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/discovery-command", strings.NewReader(`{"action":"update","disabled":true,"dht":false,"local":false,"dhtNamespace":"fse-test","dhtBootstrapPeers":["/dnsaddr/bootstrap.libp2p.io"],"networkHints":{"localContainerGatewayIPs":["172.17.0.1"],"localCIDRs":["172.18.0.0/16"],"publishedPortMappings":[{"hostIP":"172.18.0.1","hostPort":32200,"containerIP":"172.18.0.5","containerPort":22000}]}}`))
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("discovery command code = %d body=%s", ok.Code, ok.Body.String())
	}
	body := ok.Body.String()
	for _, want := range []string{`"action":"update"`, `"status":"accepted"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("discovery command response missing %s: %s", want, body)
		}
	}
	eventsReq := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	eventsReq.Header.Set("X-FSE-API-Key", "secret")
	events := httptest.NewRecorder()
	server.Router().ServeHTTP(events, eventsReq)
	if !strings.Contains(events.Body.String(), "discovery.command.finished") || strings.Contains(events.Body.String(), "bootstrap.libp2p.io") {
		t.Fatalf("discovery command event missing or leaked discovery details: %s", events.Body.String())
	}
}

func TestLogsEndpointReturnsRecentEventsWithoutSSE(t *testing.T) {
	server := NewServer(State{NodeName: "node-a"}, "secret")
	server.Publish(Event{Type: "daemon.started", Message: "started"})
	server.Publish(Event{Type: "peer.sync.finished", PeerID: "peer-a", Message: "done"})

	badMethod := httptest.NewRecorder()
	badMethodReq := httptest.NewRequest(http.MethodPost, "/v1/logs", nil)
	badMethodReq.Header.Set("X-FSE-API-Key", "secret")
	server.Router().ServeHTTP(badMethod, badMethodReq)
	if badMethod.Code != http.StatusMethodNotAllowed {
		t.Fatalf("bad method code = %d", badMethod.Code)
	}

	unauthorized := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/logs", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/logs?limit=1", nil)
	req.Header.Set("X-FSE-API-Key", "secret")
	ok := httptest.NewRecorder()
	server.Router().ServeHTTP(ok, req)
	if ok.Code != http.StatusOK {
		t.Fatalf("logs code = %d body=%s", ok.Code, ok.Body.String())
	}
	body := ok.Body.String()
	if !strings.Contains(body, `"entries"`) || !strings.Contains(body, `"type":"peer.sync.finished"`) || strings.Contains(body, `"daemon.started"`) {
		t.Fatalf("logs response did not return only the most recent limited event: %s", body)
	}
}

func TestFoldersAndPeersExposeRuntimeState(t *testing.T) {
	server := NewServer(State{
		NodeName: "node-a",
		FoldersState: []FolderState{{
			ID:     "docs",
			Path:   "/data/docs",
			Mode:   "sendrecv",
			Status: "idle",
			Index: FolderIndexState{
				Mode:                   "lazy-hashing",
				TotalFiles:             3,
				UnknownFiles:           1,
				UnverifiedSeedFiles:    1,
				QueuedHashJobs:         2,
				DateCorrectionsPending: 1,
				ProvisionalReadOnly:    true,
			},
			Sync:     FolderSyncState{LocalCursor: 4, LocalStateHash: "hash-a", DeferredDeletes: 2, ReadyDeferredDeletes: 1, MetadataCatchupPending: true},
			Warnings: FolderWarningState{InaccessibleFiles: 1, PendingLockedApplies: 2, Recent: []FolderWarning{{Kind: "inaccessible", Path: "locked.txt", Message: "open failed"}}},
		}},
		PeersState: []PeerState{{ID: "portable-peer", Status: "connected", Endpoint: "manual:10.0.0.2:22000"}},
	}, "secret")

	for _, path := range []string{"/v1/folders", "/v1/peers"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-FSE-API-Key", "secret")
		w := httptest.NewRecorder()
		server.Router().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s code = %d body=%s", path, w.Code, w.Body.String())
		}
		if path == "/v1/folders" {
			body := w.Body.String()
			if !strings.Contains(body, `"provisionalReadOnly":true`) || !strings.Contains(body, `"dateCorrectionsPending":1`) {
				t.Fatalf("folder index state missing from body: %s", body)
			}
			if !strings.Contains(body, `"warnings"`) || !strings.Contains(body, `"inaccessibleFiles":1`) || !strings.Contains(body, `"pendingLockedApplies":2`) || !strings.Contains(body, `"locked.txt"`) {
				t.Fatalf("folder warning state missing from body: %s", body)
			}
		}
	}
}
