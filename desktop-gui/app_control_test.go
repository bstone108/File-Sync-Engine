package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemoteCredentialVaultStoresMetadataWithoutReturningSecret(t *testing.T) {
	var storedService, storedAccount, storedSecret string
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		platform: "linux",
		credentialVaultSet: func(service, account, secret string) error {
			storedService, storedAccount, storedSecret = service, account, secret
			return nil
		},
	}
	record := RemoteInstanceCredentialRecord{
		CredentialRef: "desktop-vault:remote:home-nas",
		InstanceID:    "remote-home-nas",
		Label:         "Home NAS",
		CreatedAt:     time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		UpdatedAt:     time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}
	got, err := app.StoreRemoteInstanceCredential(record, RemoteInstanceCredentialSecret{CredentialRef: record.CredentialRef, SecretValue: "remote-api-secret"})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if got.Platform != "linux" || got.CredentialRef != record.CredentialRef || storedService != remoteCredentialVaultService || storedAccount != record.CredentialRef || storedSecret != "remote-api-secret" {
		t.Fatalf("record=%#v service=%q account=%q secret=%q", got, storedService, storedAccount, storedSecret)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "remote-api-secret") {
		t.Fatalf("metadata leaked secret: %s", encoded)
	}
}

func TestRemoteCredentialVaultRejectsMalformedReferenceBeforeBackendCall(t *testing.T) {
	calls := 0
	app := NewApp()
	app.desktop = &desktopNativeRuntime{credentialVaultSet: func(string, string, string) error { calls++; return nil }}
	_, err := app.StoreRemoteInstanceCredential(
		RemoteInstanceCredentialRecord{CredentialRef: "desktop-vault:local-api-key", InstanceID: "remote-a", Label: "Remote A"},
		RemoteInstanceCredentialSecret{CredentialRef: "desktop-vault:local-api-key", SecretValue: "secret"},
	)
	if err == nil || calls != 0 {
		t.Fatalf("expected fail-closed validation, err=%v calls=%d", err, calls)
	}
}

func TestDaemonAPIProxyResolvesRemoteCredentialInsideNativeBoundary(t *testing.T) {
	var auth string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("X-FSE-API-Key")
		_ = json.NewEncoder(w).Encode(map[string]any{"nodeName": "remote-node"})
	}))
	defer server.Close()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		apiClient: server.Client(),
		credentialVaultGet: func(service, account string) (string, error) {
			if service != remoteCredentialVaultService || account != "desktop-vault:remote:home-nas" {
				t.Fatalf("service=%q account=%q", service, account)
			}
			return "vault-api-secret", nil
		},
	}
	response, err := app.DaemonAPIRequest(NativeDaemonAPIRequest{APIBaseURL: server.URL, CredentialRef: "desktop-vault:remote:home-nas", Method: "GET", Path: "/v1/status"})
	if err != nil {
		t.Fatalf("proxy: %v", err)
	}
	if auth != "vault-api-secret" || strings.Contains(string(response.Body), "vault-api-secret") {
		t.Fatalf("auth=%q body=%s", auth, response.Body)
	}
}

func TestDeleteRemoteCredentialUsesVaultAndTreatsMissingAsSuccess(t *testing.T) {
	deleted := ""
	app := NewApp()
	app.desktop = &desktopNativeRuntime{credentialVaultDelete: func(service, account string) error {
		deleted = service + ":" + account
		return errCredentialNotFound
	}}
	if err := app.DeleteRemoteInstanceCredential("desktop-vault:remote:home-nas"); err != nil {
		t.Fatalf("delete missing credential: %v", err)
	}
	if deleted != remoteCredentialVaultService+":desktop-vault:remote:home-nas" {
		t.Fatalf("deleted=%q", deleted)
	}
}

func TestUpdateRemoteInstancePreservesCredentialReference(t *testing.T) {
	tmp := t.TempDir()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp}
	original := RemoteInstanceRegistry{SelectedInstanceID: "remote-a", Instances: []RemoteInstanceRegistryEntry{{ID: "remote-a", Label: "Old label", APIBaseURL: "https://old.example", CredentialRef: "desktop-vault:remote:a", Source: "api-endpoint-key", ConnectionState: "online"}}}
	if _, err := app.saveRemoteInstanceRegistryForTest(original); err != nil {
		t.Fatal(err)
	}
	opened, _ := app.GetRemoteInstanceRegistry()
	updated, err := app.UpdateRemoteInstance(RemoteInstanceUpdateRequest{ID: "remote-a", ExpectedCredentialRef: "desktop-vault:remote:a", ExpectedRevision: opened.Instances[0].Revision, Label: "New label", APIBaseURL: "https://new.example"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(updated.Instances) != 1 || updated.Instances[0].CredentialRef != "desktop-vault:remote:a" || updated.Instances[0].Label != "New label" || updated.Instances[0].APIBaseURL != "https://new.example" || updated.Instances[0].ConnectionState != "offline" {
		t.Fatalf("updated registry = %#v", updated)
	}
}

func TestCompetingEditsToSameRemoteHostRejectStaleRevisionWithoutLostUpdate(t *testing.T) {
	tmp := t.TempDir()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp}
	if _, err := app.saveRemoteInstanceRegistryForTest(RemoteInstanceRegistry{Instances: []RemoteInstanceRegistryEntry{{ID: "remote-a", Label: "Original", APIBaseURL: "https://original.example", CredentialRef: "desktop-vault:remote:a", Source: "api-endpoint-key", ConnectionState: "offline"}}}); err != nil {
		t.Fatal(err)
	}
	opened, err := app.GetRemoteInstanceRegistry()
	if err != nil || opened.Instances[0].Revision == 0 {
		t.Fatalf("opened registry = %#v, %v", opened, err)
	}
	first, err := app.UpdateRemoteInstance(RemoteInstanceUpdateRequest{ID: "remote-a", ExpectedCredentialRef: "desktop-vault:remote:a", ExpectedRevision: opened.Instances[0].Revision, Label: "First", APIBaseURL: "https://first.example"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.UpdateRemoteInstance(RemoteInstanceUpdateRequest{ID: "remote-a", ExpectedCredentialRef: "desktop-vault:remote:a", ExpectedRevision: opened.Instances[0].Revision, Label: "Second", APIBaseURL: "https://second.example"}); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Fatalf("expected stale revision rejection, got %v", err)
	}
	loaded, _ := app.GetRemoteInstanceRegistry()
	if loaded.Instances[0].Revision != first.Instances[0].Revision || loaded.Instances[0].Label != "First" {
		t.Fatalf("stale edit overwrote first edit: %#v", loaded)
	}
}

func TestUpdateRemoteInstanceRejectsStaleCredentialReferenceWithoutMutation(t *testing.T) {
	tmp := t.TempDir()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp}
	original := RemoteInstanceRegistry{Instances: []RemoteInstanceRegistryEntry{{ID: "remote-a", Label: "Old label", APIBaseURL: "https://old.example", CredentialRef: "desktop-vault:remote:a", Source: "api-endpoint-key", ConnectionState: "offline"}}}
	if _, err := app.saveRemoteInstanceRegistryForTest(original); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UpdateRemoteInstance(RemoteInstanceUpdateRequest{ID: "remote-a", ExpectedCredentialRef: "desktop-vault:remote:stale", ExpectedRevision: 1, Label: "Changed", APIBaseURL: "https://changed.example"}); err == nil {
		t.Fatal("expected stale credential reference rejection")
	}
	loaded, err := app.GetRemoteInstanceRegistry()
	if err != nil || loaded.Instances[0].Label != "Old label" {
		t.Fatalf("registry mutated after stale update: %#v, %v", loaded, err)
	}
}

func TestConcurrentRemoteInstanceUpdatesDoNotLoseEitherChange(t *testing.T) {
	tmp := t.TempDir()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp}
	original := RemoteInstanceRegistry{Instances: []RemoteInstanceRegistryEntry{
		{ID: "remote-a", Label: "Old A", APIBaseURL: "https://a.example", CredentialRef: "desktop-vault:remote:a", Source: "api-endpoint-key", ConnectionState: "offline"},
		{ID: "remote-b", Label: "Old B", APIBaseURL: "https://b.example", CredentialRef: "desktop-vault:remote:b", Source: "api-endpoint-key", ConnectionState: "offline"},
	}}
	if _, err := app.saveRemoteInstanceRegistryForTest(original); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, request := range []RemoteInstanceUpdateRequest{
		{ID: "remote-a", ExpectedCredentialRef: "desktop-vault:remote:a", ExpectedRevision: 1, Label: "New A", APIBaseURL: "https://a-new.example"},
		{ID: "remote-b", ExpectedCredentialRef: "desktop-vault:remote:b", ExpectedRevision: 1, Label: "New B", APIBaseURL: "https://b-new.example"},
	} {
		request := request
		go func() {
			<-start
			_, err := app.UpdateRemoteInstance(request)
			errs <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := app.GetRemoteInstanceRegistry()
	if err != nil || loaded.Instances[0].Label != "New A" || loaded.Instances[1].Label != "New B" {
		t.Fatalf("concurrent update lost: %#v, %v", loaded, err)
	}
}

func TestRemoveRemoteInstancePersistsBeforeCredentialDeletion(t *testing.T) {
	tmp := t.TempDir()
	deletedAfterPersist := false
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp}
	original := RemoteInstanceRegistry{SelectedInstanceID: "remote-a", Instances: []RemoteInstanceRegistryEntry{{ID: "remote-a", Label: "Remote A", APIBaseURL: "https://remote.example", CredentialRef: "desktop-vault:remote:a", Source: "api-endpoint-key", ConnectionState: "online"}}}
	if _, err := app.saveRemoteInstanceRegistryForTest(original); err != nil {
		t.Fatal(err)
	}
	app.desktop.credentialVaultDelete = func(_, _ string) error {
		data, err := os.ReadFile(filepath.Join(tmp, "remote-instances.json"))
		var loaded RemoteInstanceRegistry
		if err == nil {
			err = json.Unmarshal(data, &loaded)
		}
		deletedAfterPersist = err == nil && len(loaded.Instances) == 0 && loaded.SelectedInstanceID == ""
		return nil
	}
	opened, _ := app.GetRemoteInstanceRegistry()
	removed, err := app.RemoveRemoteInstance(RemoteInstanceRemovalRequest{ID: "remote-a", ExpectedCredentialRef: "desktop-vault:remote:a", ExpectedRevision: opened.Instances[0].Revision, ConfirmLabel: "Remote A"})
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !deletedAfterPersist || len(removed.Instances) != 0 {
		t.Fatalf("removed=%#v deletedAfterPersist=%v", removed, deletedAfterPersist)
	}
}

func TestRemoveRemoteInstanceLeavesCleanupTombstoneWhenCredentialDeletionIsUncertain(t *testing.T) {
	tmp := t.TempDir()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp, credentialVaultDelete: func(_, _ string) error { return errors.New("partial Secret Service deletion") }}
	original := RemoteInstanceRegistry{SelectedInstanceID: "remote-a", Instances: []RemoteInstanceRegistryEntry{{ID: "remote-a", Label: "Remote A", APIBaseURL: "https://remote.example", CredentialRef: "desktop-vault:remote:a", Source: "api-endpoint-key", ConnectionState: "online"}}}
	if _, err := app.saveRemoteInstanceRegistryForTest(original); err != nil {
		t.Fatal(err)
	}
	opened, _ := app.GetRemoteInstanceRegistry()
	removed, err := app.RemoveRemoteInstance(RemoteInstanceRemovalRequest{ID: "remote-a", ExpectedCredentialRef: "desktop-vault:remote:a", ExpectedRevision: opened.Instances[0].Revision, ConfirmLabel: "Remote A"})
	if err != nil {
		t.Fatalf("uncertain cleanup must preserve safe host removal: %v", err)
	}
	loaded, err := app.GetRemoteInstanceRegistry()
	if err != nil || len(loaded.Instances) != 0 || loaded.SelectedInstanceID != "" || len(loaded.CredentialCleanupPending) != 1 || loaded.CredentialCleanupPending[0] != "desktop-vault:remote:a" {
		t.Fatalf("unsafe cleanup state: removed=%#v loaded=%#v err=%v", removed, loaded, err)
	}
}

func TestReconcileRemoteCredentialCleanupRetriesFailuresAndClearsOnlyAfterSuccess(t *testing.T) {
	tmp := t.TempDir()
	attempts := 0
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp, credentialVaultDelete: func(_, account string) error {
		attempts++
		if account != "desktop-vault:remote:a" {
			t.Fatalf("unexpected account %q", account)
		}
		if attempts == 1 {
			return errors.New("one matching Secret Service item remains")
		}
		return nil
	}}
	if _, err := app.saveRemoteInstanceRegistryForTest(RemoteInstanceRegistry{CredentialCleanupPending: []string{"desktop-vault:remote:a"}, Instances: []RemoteInstanceRegistryEntry{}}); err != nil {
		t.Fatal(err)
	}
	first, err := app.ReconcileRemoteInstanceCredentialCleanup()
	if err == nil || len(first.CredentialCleanupPending) != 1 || attempts != 1 {
		t.Fatalf("first reconciliation must retain tombstone: registry=%#v attempts=%d err=%v", first, attempts, err)
	}
	second, err := app.ReconcileRemoteInstanceCredentialCleanup()
	if err != nil || len(second.CredentialCleanupPending) != 0 || attempts != 2 {
		t.Fatalf("second reconciliation must clear verified cleanup: registry=%#v attempts=%d err=%v", second, attempts, err)
	}
	loaded, loadErr := app.GetRemoteInstanceRegistry()
	if loadErr != nil || len(loaded.CredentialCleanupPending) != 0 || len(loaded.Instances) != 0 {
		t.Fatalf("reconciled registry was not durable/non-actionable: %#v, %v", loaded, loadErr)
	}
}

func TestReconcileRemoteCredentialCleanupAggregatesFailuresAndKeepsEveryFailedTombstone(t *testing.T) {
	tmp := t.TempDir()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp, credentialVaultDelete: func(_, account string) error { return fmt.Errorf("cannot delete %s", account) }}
	refs := []string{"desktop-vault:remote:a", "desktop-vault:remote:b"}
	if _, err := app.saveRemoteInstanceRegistryForTest(RemoteInstanceRegistry{CredentialCleanupPending: refs, Instances: []RemoteInstanceRegistryEntry{}}); err != nil {
		t.Fatal(err)
	}
	result, err := app.ReconcileRemoteInstanceCredentialCleanup()
	if err == nil || !strings.Contains(err.Error(), refs[0]) || !strings.Contains(err.Error(), refs[1]) || len(result.CredentialCleanupPending) != 2 {
		t.Fatalf("cleanup failures were not safely aggregated: %#v, %v", result, err)
	}
}

func TestReconcileRemoteCredentialCleanupReportsTombstoneClearFailureWithoutLosingRetry(t *testing.T) {
	tmp := t.TempDir()
	writes := 0
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp, credentialVaultDelete: func(_, _ string) error { return nil }, remoteRegistryWrite: func(path string, data []byte) error {
		writes++
		if writes == 2 {
			return errors.New("disk full")
		}
		return atomicWriteRemoteInstanceRegistry(path, data)
	}}
	if _, err := app.saveRemoteInstanceRegistryForTest(RemoteInstanceRegistry{CredentialCleanupPending: []string{"desktop-vault:remote:a"}, Instances: []RemoteInstanceRegistryEntry{}}); err != nil {
		t.Fatal(err)
	}
	result, err := app.ReconcileRemoteInstanceCredentialCleanup()
	if err == nil || !strings.Contains(err.Error(), "persist") || len(result.CredentialCleanupPending) != 1 {
		t.Fatalf("unsafe tombstone-clear result: %#v, %v", result, err)
	}
	loaded, loadErr := app.GetRemoteInstanceRegistry()
	if loadErr != nil || len(loaded.CredentialCleanupPending) != 1 {
		t.Fatalf("retry tombstone was lost: %#v, %v", loaded, loadErr)
	}
}

func TestStartupRetriesDurableRemoteCredentialCleanup(t *testing.T) {
	tmp := t.TempDir()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp, credentialVaultDelete: func(_, _ string) error { return nil }}
	if _, err := app.saveRemoteInstanceRegistryForTest(RemoteInstanceRegistry{CredentialCleanupPending: []string{"desktop-vault:remote:a"}, Instances: []RemoteInstanceRegistryEntry{}}); err != nil {
		t.Fatal(err)
	}
	app.startup(nil)
	loaded, err := app.GetRemoteInstanceRegistry()
	if err != nil || len(loaded.CredentialCleanupPending) != 0 {
		t.Fatalf("startup did not reconcile cleanup: %#v, %v", loaded, err)
	}
}

func TestRemoveRemoteInstanceDoesNotDeleteCredentialWhenDurableTombstoneWriteFails(t *testing.T) {
	tmp := t.TempDir()
	writes := 0
	deleted := false
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		stateRoot:             tmp,
		credentialVaultDelete: func(_, _ string) error { deleted = true; return nil },
		remoteRegistryWrite: func(path string, data []byte) error {
			writes++
			if writes == 2 {
				return errors.New("tombstone disk failure")
			}
			return atomicWriteRemoteInstanceRegistry(path, data)
		},
	}
	if _, err := app.saveRemoteInstanceRegistryForTest(RemoteInstanceRegistry{SelectedInstanceID: "remote-a", Instances: []RemoteInstanceRegistryEntry{{ID: "remote-a", Label: "Remote A", APIBaseURL: "https://remote.example", CredentialRef: "desktop-vault:remote:a", Source: "api-endpoint-key", ConnectionState: "online"}}}); err != nil {
		t.Fatal(err)
	}
	opened, _ := app.GetRemoteInstanceRegistry()
	_, removeErr := app.RemoveRemoteInstance(RemoteInstanceRemovalRequest{ID: "remote-a", ExpectedCredentialRef: "desktop-vault:remote:a", ExpectedRevision: opened.Instances[0].Revision, ConfirmLabel: "Remote A"})
	if removeErr == nil || deleted {
		t.Fatalf("expected pre-deletion tombstone persistence error, err=%v deleted=%v", removeErr, deleted)
	}
	loaded, err := app.GetRemoteInstanceRegistry()
	if err != nil || len(loaded.Instances) != 1 || loaded.SelectedInstanceID != "remote-a" || len(loaded.CredentialCleanupPending) != 0 {
		t.Fatalf("failed removal changed authoritative host state: %#v, %v", loaded, err)
	}
}

func TestOnboardRemoteInstanceKeepsDurableCleanupTombstoneWhenRegistryCommitAndCredentialRollbackFail(t *testing.T) {
	tmp := t.TempDir()
	writes := 0
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		stateRoot:             tmp,
		credentialVaultSet:    func(_, _, _ string) error { return nil },
		credentialVaultDelete: func(_, _ string) error { return errors.New("partial Secret Service deletion") },
		remoteRegistryWrite: func(path string, data []byte) error {
			writes++
			if writes == 2 {
				return errors.New("host registry commit failed")
			}
			return atomicWriteRemoteInstanceRegistry(path, data)
		},
	}
	result, onboardErr := app.OnboardRemoteInstance(RemoteInstanceOnboardingRequest{Entry: RemoteInstanceRegistryEntry{ID: "remote-a", Label: "Remote A", APIBaseURL: "https://remote.example", CredentialRef: "desktop-vault:remote:a", Source: "api-endpoint-key", ConnectionState: "offline"}, SecretValue: "native-only-secret"})
	if onboardErr == nil || len(result.Instances) != 0 || len(result.CredentialCleanupPending) != 1 || result.CredentialCleanupPending[0] != "desktop-vault:remote:a" {
		t.Fatalf("onboarding rollback lost durable cleanup reference after %v: %#v", onboardErr, result)
	}
	loaded, err := app.GetRemoteInstanceRegistry()
	if err != nil || len(loaded.Instances) != 0 || len(loaded.CredentialCleanupPending) != 1 {
		t.Fatalf("cleanup tombstone was not durable: %#v, %v", loaded, err)
	}
}

func TestOnboardRemoteInstanceRejectsCredentialReferenceAlreadyOwnedOrPendingBeforeVaultAccess(t *testing.T) {
	for _, test := range []struct {
		name     string
		registry RemoteInstanceRegistry
	}{
		{
			name: "active instance owns reference",
			registry: RemoteInstanceRegistry{Instances: []RemoteInstanceRegistryEntry{{
				ID: "remote-a", Label: "Remote A", APIBaseURL: "https://a.example", CredentialRef: "desktop-vault:remote:shared", Source: "api-endpoint-key", ConnectionState: "offline",
			}}},
		},
		{
			name:     "cleanup tombstone owns reference",
			registry: RemoteInstanceRegistry{Instances: []RemoteInstanceRegistryEntry{}, CredentialCleanupPending: []string{"desktop-vault:remote:shared"}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tmp := t.TempDir()
			vaultCalls := 0
			app := NewApp()
			app.desktop = &desktopNativeRuntime{stateRoot: tmp, credentialVaultSet: func(_, _, _ string) error { vaultCalls++; return nil }, credentialVaultDelete: func(_, _ string) error { vaultCalls++; return nil }}
			if _, err := app.saveRemoteInstanceRegistryForTest(test.registry); err != nil {
				t.Fatal(err)
			}
			_, err := app.OnboardRemoteInstance(RemoteInstanceOnboardingRequest{Entry: RemoteInstanceRegistryEntry{ID: "remote-b", Label: "Remote B", APIBaseURL: "https://b.example", CredentialRef: "desktop-vault:remote:shared", Source: "api-endpoint-key", ConnectionState: "offline"}, SecretValue: "must-not-be-stored"})
			if err == nil || vaultCalls != 0 {
				t.Fatalf("expected duplicate credential reference rejection before vault access, err=%v vaultCalls=%d", err, vaultCalls)
			}
		})
	}
}

func TestReconcileRemoteCredentialCleanupRejectsActiveTombstoneOverlapBeforeVaultDeletion(t *testing.T) {
	tmp := t.TempDir()
	deleted := false
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp, credentialVaultDelete: func(_, _ string) error { deleted = true; return nil }}
	registry := RemoteInstanceRegistry{
		Instances:                []RemoteInstanceRegistryEntry{{ID: "remote-a", Label: "Remote A", APIBaseURL: "https://a.example", CredentialRef: "desktop-vault:remote:shared", Source: "api-endpoint-key", ConnectionState: "offline", Revision: 1}},
		CredentialCleanupPending: []string{"desktop-vault:remote:shared"},
	}
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "remote-instances.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ReconcileRemoteInstanceCredentialCleanup(); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("expected invalid overlapping registry rejection, got %v", err)
	}
	if deleted {
		t.Fatal("active credential was deleted by overlapping cleanup tombstone")
	}
}

func TestSelectionCASCannotOverwriteConcurrentEditOrRemoval(t *testing.T) {
	tmp := t.TempDir()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp, credentialVaultDelete: func(_, _ string) error { return nil }}
	if _, err := app.saveRemoteInstanceRegistryForTest(RemoteInstanceRegistry{SelectedInstanceID: "remote-a", Instances: []RemoteInstanceRegistryEntry{{ID: "remote-a", Label: "A", APIBaseURL: "https://a.example", CredentialRef: "desktop-vault:remote:a", Source: "api-endpoint-key", ConnectionState: "offline"}, {ID: "remote-b", Label: "B", APIBaseURL: "https://b.example", CredentialRef: "desktop-vault:remote:b", Source: "api-endpoint-key", ConnectionState: "offline"}}}); err != nil {
		t.Fatal(err)
	}
	opened, _ := app.GetRemoteInstanceRegistry()
	if _, err := app.UpdateRemoteInstance(RemoteInstanceUpdateRequest{ID: "remote-b", ExpectedCredentialRef: "desktop-vault:remote:b", ExpectedRevision: 1, Label: "B edited", APIBaseURL: "https://b-new.example"}); err != nil {
		t.Fatal(err)
	}
	selected, err := app.SelectRemoteInstance(RemoteInstanceSelectionRequest{InstanceID: "remote-b", ExpectedSelectedInstanceID: opened.SelectedInstanceID})
	if err != nil || selected.Instances[1].Label != "B edited" || selected.Instances[1].Revision != 2 {
		t.Fatalf("selection overwrote edit: %#v, %v", selected, err)
	}
	if _, err := app.RemoveRemoteInstance(RemoteInstanceRemovalRequest{ID: "remote-b", ExpectedCredentialRef: "desktop-vault:remote:b", ExpectedRevision: 2, ConfirmLabel: "B edited"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SelectRemoteInstance(RemoteInstanceSelectionRequest{InstanceID: "remote-b", ExpectedSelectedInstanceID: "remote-b"}); err == nil {
		t.Fatal("stale selection resurrected removed host")
	}
	loaded, _ := app.GetRemoteInstanceRegistry()
	if len(loaded.Instances) != 1 || loaded.Instances[0].ID != "remote-a" {
		t.Fatalf("removed host was restored: %#v", loaded)
	}
}

func TestRemoveRemoteInstanceRejectsStaleRevisionBeforeCredentialDeletion(t *testing.T) {
	tmp := t.TempDir()
	deleted := false
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp, credentialVaultDelete: func(_, _ string) error { deleted = true; return nil }}
	if _, err := app.saveRemoteInstanceRegistryForTest(RemoteInstanceRegistry{Instances: []RemoteInstanceRegistryEntry{{ID: "remote-a", Label: "Original", APIBaseURL: "https://original.example", CredentialRef: "desktop-vault:remote:a", Source: "api-endpoint-key", ConnectionState: "offline"}}}); err != nil {
		t.Fatal(err)
	}
	opened, _ := app.GetRemoteInstanceRegistry()
	if _, err := app.UpdateRemoteInstance(RemoteInstanceUpdateRequest{ID: "remote-a", ExpectedCredentialRef: "desktop-vault:remote:a", ExpectedRevision: opened.Instances[0].Revision, Label: "Changed", APIBaseURL: "https://changed.example"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.RemoveRemoteInstance(RemoteInstanceRemovalRequest{ID: "remote-a", ExpectedCredentialRef: "desktop-vault:remote:a", ExpectedRevision: opened.Instances[0].Revision, ConfirmLabel: "Changed"}); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Fatalf("expected stale removal rejection, got %v", err)
	}
	if deleted {
		t.Fatal("credential deletion ran for stale removal form")
	}
}

func TestRemoveRemoteInstanceRequiresExactLabelConfirmation(t *testing.T) {
	tmp := t.TempDir()
	deleted := false
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp, credentialVaultDelete: func(_, _ string) error { deleted = true; return nil }}
	if _, err := app.saveRemoteInstanceRegistryForTest(RemoteInstanceRegistry{Instances: []RemoteInstanceRegistryEntry{{ID: "remote-a", Label: "Remote A", APIBaseURL: "https://remote.example", CredentialRef: "desktop-vault:remote:a", Source: "api-endpoint-key", ConnectionState: "offline"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.RemoveRemoteInstance(RemoteInstanceRemovalRequest{ID: "remote-a", ExpectedCredentialRef: "desktop-vault:remote:a", ConfirmLabel: "remote a"}); err == nil {
		t.Fatal("expected exact-label confirmation rejection")
	}
	if deleted {
		t.Fatal("credential deletion ran without exact confirmation")
	}
}

func TestRemoteInstanceRegistryPersistsOnlyValidatedNonSecretMetadata(t *testing.T) {
	tmp := t.TempDir()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp}
	want := RemoteInstanceRegistry{
		SelectedInstanceID: "remote-home-nas",
		Instances: []RemoteInstanceRegistryEntry{{
			ID: "remote-home-nas", Label: "Home NAS", APIBaseURL: "https://nas.example:22420",
			CredentialRef: "desktop-vault:remote:home-nas", Source: "api-endpoint-key",
			ConnectionState: "offline",
		}},
	}
	if got, err := app.saveRemoteInstanceRegistryForTest(want); err != nil || len(got.Instances) != 1 {
		t.Fatalf("save = %#v, %v", got, err)
	}
	reloaded := NewApp()
	reloaded.desktop = &desktopNativeRuntime{stateRoot: tmp}
	got, err := reloaded.GetRemoteInstanceRegistry()
	if err != nil || got.SelectedInstanceID != want.SelectedInstanceID || len(got.Instances) != 1 || got.Instances[0] != want.Instances[0] {
		t.Fatalf("reload = %#v, %v", got, err)
	}
	path := filepath.Join(tmp, "remote-instances.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "apiKey") || strings.Contains(string(data), "secret") {
		t.Fatalf("registry contains secret-shaped data: %s", data)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("registry permissions = %v, %v", info, err)
	}
}

func TestRemoteInstanceRegistryRejectsUnsafeMetadataWithoutReplacingSavedState(t *testing.T) {
	tmp := t.TempDir()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp}
	want := RemoteInstanceRegistry{Instances: []RemoteInstanceRegistryEntry{{ID: "remote-a", Label: "Remote A", APIBaseURL: "https://remote.example", CredentialRef: "desktop-vault:remote:a", Source: "api-endpoint-key", ConnectionState: "offline"}}}
	if _, err := app.saveRemoteInstanceRegistryForTest(want); err != nil {
		t.Fatal(err)
	}
	bad := []RemoteInstanceRegistry{
		{Instances: []RemoteInstanceRegistryEntry{{ID: "remote-b", Label: "Remote B", APIBaseURL: "http://remote.example", CredentialRef: "desktop-vault:remote:b", Source: "api-endpoint-key", ConnectionState: "offline"}}},
		{Instances: []RemoteInstanceRegistryEntry{{ID: "remote-b", Label: "Remote B", APIBaseURL: "https://user:pass@remote.example", CredentialRef: "desktop-vault:remote:b", Source: "api-endpoint-key", ConnectionState: "offline"}}},
		{Instances: []RemoteInstanceRegistryEntry{{ID: "remote-b", Label: "Remote B", APIBaseURL: "https://remote.example?apiKey=must-not-persist", CredentialRef: "desktop-vault:remote:b", Source: "api-endpoint-key", ConnectionState: "offline"}}},
		{Instances: []RemoteInstanceRegistryEntry{{ID: "remote-b", Label: "Remote B", APIBaseURL: "https://remote.example", CredentialRef: "desktop-vault:remote:b", Source: "pasted-pairing-code", ConnectionState: "offline"}}},
	}
	for _, registry := range bad {
		if _, err := app.saveRemoteInstanceRegistryForTest(registry); err == nil {
			t.Fatalf("expected rejection for %#v", registry)
		}
	}
	if got, err := app.GetRemoteInstanceRegistry(); err != nil || len(got.Instances) != 1 || got.Instances[0] != want.Instances[0] {
		t.Fatalf("saved state changed: %#v, %v", got, err)
	}
}

func TestRemoteInstanceRegistryRejectsStoredUnknownSecretFields(t *testing.T) {
	tmp := t.TempDir()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp}
	data := `{"instances":[{"id":"remote-a","label":"Remote A","apiBaseURL":"https://remote.example","credentialRef":"desktop-vault:remote:a","source":"api-endpoint-key","connectionState":"offline","apiKey":"must-not-survive"}]}`
	if err := os.WriteFile(filepath.Join(tmp, "remote-instances.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.GetRemoteInstanceRegistry(); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown secret-shaped field rejection, got %v", err)
	}
}

func TestBundledManifestInspectionReadsAndHashesPackagedResources(t *testing.T) {
	tmp := t.TempDir()
	enginePath := filepath.Join(tmp, "linux", "amd64", "fse")
	if err := os.MkdirAll(filepath.Dir(enginePath), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("engine-binary")
	if err := os.WriteFile(enginePath, payload, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	manifest := map[string]any{"version": "1.2.3", "entries": []map[string]any{
		{"target": "linux-amd64", "relativePath": "linux/amd64/fse", "expectedExecutable": "fse", "expectedVersion": "1.2.3", "expectedSHA256": fmt.Sprintf("%x", sum)},
		{"target": "windows-amd64", "relativePath": "windows/amd64/fse.exe", "expectedExecutable": "fse.exe", "expectedVersion": "1.2.3", "expectedSHA256": strings.Repeat("0", 64)},
	}}
	manifestBytes, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(tmp, "manifest.json"), manifestBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.desktop = &desktopNativeRuntime{resourceRoot: tmp}
	got, err := app.InspectBundledEngineResources()
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !got.Verified || got.Version != "1.2.3" || len(got.Entries) != 2 || !got.Entries[0].Exists || !got.Entries[0].Verified || got.Entries[1].Exists {
		t.Fatalf("inspection = %#v", got)
	}
}

func TestBundledManifestInspectionRejectsEscapingResourcePath(t *testing.T) {
	tmp := t.TempDir()
	manifest := `{"version":"1","entries":[{"target":"linux-amd64","relativePath":"../outside","expectedExecutable":"fse","expectedVersion":"1","expectedSHA256":"00"}]}`
	if err := os.WriteFile(filepath.Join(tmp, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.desktop = &desktopNativeRuntime{resourceRoot: tmp}
	if _, err := app.InspectBundledEngineResources(); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("expected unsafe path rejection, got %v", err)
	}
}

func TestDesktopPreferencesPersistThroughNativeBoundary(t *testing.T) {
	tmp := t.TempDir()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp}
	want := DesktopPreferences{Theme: "dark", Density: "compact", MinimizeToTray: true, NotificationsEnabled: false}
	if got, err := app.SaveDesktopPreferences(want); err != nil || got != want {
		t.Fatalf("save = %#v, %v", got, err)
	}
	reloaded := NewApp()
	reloaded.desktop = &desktopNativeRuntime{stateRoot: tmp}
	if got, err := reloaded.GetDesktopPreferences(); err != nil || got != want {
		t.Fatalf("reload = %#v, %v", got, err)
	}
	info, err := os.Stat(filepath.Join(tmp, "desktop-preferences.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestDesktopPreferencesRejectUnsupportedValuesWithoutReplacingSavedState(t *testing.T) {
	tmp := t.TempDir()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{stateRoot: tmp}
	want := DesktopPreferences{Theme: "system", Density: "comfortable", NotificationsEnabled: true}
	if _, err := app.SaveDesktopPreferences(want); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SaveDesktopPreferences(DesktopPreferences{Theme: "script:bad", Density: "compact"}); err == nil {
		t.Fatal("expected validation error")
	}
	if got, err := app.GetDesktopPreferences(); err != nil || got != want {
		t.Fatalf("saved state changed: %#v, %v", got, err)
	}
}

func TestRequestGUIOwnedNonServiceDaemonLaunchPrefersReachableInstalledService(t *testing.T) {
	launches := 0
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		platform:          "linux",
		serviceCandidates: []localDaemonCandidate{{ID: "systemd-user:fse", Kind: "service", Manager: "systemd-user", ServiceName: "fse", APIBaseURL: "https://127.0.0.1:22420", CredentialRef: "config://key"}},
		probeCandidate: func(candidate localDaemonCandidate) (DaemonRuntimeState, error) {
			return DaemonRuntimeState{ConnectionState: "running", NodeName: "service-node"}, nil
		},
		launcher: func(string, []string, []string) (int, error) { launches++; return 42, nil },
	}
	got, err := app.RequestGUIOwnedNonServiceDaemonLaunch(GUIOwnedNonServiceDaemonLaunchRequest{PreferExistingReachableDaemon: true})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if launches != 0 || got.Kind != "service" || got.Manager != "systemd-user" || got.SessionID != "systemd-user:fse" {
		t.Fatalf("got %#v after %d portable launches", got, launches)
	}
}

func TestRequestGUIOwnedNonServiceDaemonLaunchStartsConfiguredServiceBeforePortableFallback(t *testing.T) {
	startedService := false
	portableLaunches := 0
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		platform: "linux",
		serviceCandidates: []localDaemonCandidate{{
			ID: "systemd-user:fse", Kind: "service", Manager: "systemd-user", ServiceName: "fse",
			APIBaseURL: "https://127.0.0.1:22420", CredentialRef: "config://key",
		}},
		commandRunner: func(name string, args ...string) ([]byte, error) {
			if name != "systemctl" || strings.Join(args, " ") != "--user start fse" {
				t.Fatalf("manager command = %s %s", name, strings.Join(args, " "))
			}
			startedService = true
			return []byte("started"), nil
		},
		probeCandidate: func(localDaemonCandidate) (DaemonRuntimeState, error) {
			if !startedService {
				return DaemonRuntimeState{}, errors.New("connection refused")
			}
			return DaemonRuntimeState{ConnectionState: "running", NodeName: "installed-service"}, nil
		},
		launcher: func(string, []string, []string) (int, error) {
			portableLaunches++
			return 99, nil
		},
	}

	got, err := app.RequestGUIOwnedNonServiceDaemonLaunch(GUIOwnedNonServiceDaemonLaunchRequest{PreferExistingReachableDaemon: true})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if !startedService || portableLaunches != 0 {
		t.Fatalf("startedService=%v portableLaunches=%d", startedService, portableLaunches)
	}
	if got.Kind != "service" || got.Manager != "systemd-user" || got.SessionID != "systemd-user:fse" || got.ConnectionState != "running" || got.NodeName != "installed-service" {
		t.Fatalf("service session = %#v", got)
	}
}

func TestDiscoverLocalDaemonPrefersReachableServiceOverPortableSession(t *testing.T) {
	tmp := t.TempDir()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		stateRoot: tmp,
		platform:  "linux",
		serviceCandidates: []localDaemonCandidate{
			{ID: "systemd-user:fse", Kind: "service", Manager: "systemd-user", ServiceName: "fse", APIBaseURL: "https://127.0.0.1:22420", CredentialRef: "file://service-key", StatePath: tmp},
		},
		probeCandidate: func(candidate localDaemonCandidate) (DaemonRuntimeState, error) {
			if candidate.Kind == "service" {
				return DaemonRuntimeState{ConnectionState: "running", NodeName: "installed", Source: candidate.ID, Manager: candidate.Manager, ServiceName: candidate.ServiceName}, nil
			}
			return DaemonRuntimeState{}, errors.New("unexpected portable probe")
		},
	}
	if err := app.desktop.saveSession(GUIManagedNonServiceDaemonSession{SessionID: "portable", PID: 99}); err != nil {
		t.Fatal(err)
	}

	got, err := app.DiscoverLocalDaemon()
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if got.Source != "systemd-user:fse" || got.NodeName != "installed" || got.Manager != "systemd-user" {
		t.Fatalf("discovered %#v, want reachable service", got)
	}
}

func TestControlLocalDaemonRunsPlatformManagerAndReturnsRefreshedStatus(t *testing.T) {
	var commands [][]string
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		platform:          "linux",
		serviceCandidates: []localDaemonCandidate{{ID: "systemd-user:fse", Kind: "service", Manager: "systemd-user", ServiceName: "fse"}},
		commandRunner: func(name string, args ...string) ([]byte, error) {
			commands = append(commands, append([]string{name}, args...))
			return []byte("active\n"), nil
		},
		probeCandidate: func(candidate localDaemonCandidate) (DaemonRuntimeState, error) {
			return DaemonRuntimeState{ConnectionState: "running", Source: candidate.ID, Manager: candidate.Manager, ServiceName: candidate.ServiceName}, nil
		},
	}

	got, err := app.ControlLocalDaemon(LocalDaemonControlRequest{Action: "restart", Source: "systemd-user:fse"})
	if err != nil {
		t.Fatalf("control: %v", err)
	}
	if len(commands) != 1 || strings.Join(commands[0], " ") != "systemctl --user restart fse" {
		t.Fatalf("commands = %#v", commands)
	}
	if got.ConnectionState != "running" {
		t.Fatalf("status = %#v", got)
	}
}

func TestControlLocalDaemonReportsActionableManagerFailure(t *testing.T) {
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		platform:          "linux",
		serviceCandidates: []localDaemonCandidate{{ID: "systemd:fse", Kind: "service", Manager: "systemd", ServiceName: "fse"}},
		commandRunner: func(name string, args ...string) ([]byte, error) {
			return []byte("Access denied"), errors.New("exit status 1")
		},
	}
	_, err := app.ControlLocalDaemon(LocalDaemonControlRequest{Action: "start", Source: "systemd:fse"})
	if err == nil || !strings.Contains(err.Error(), "systemctl start fse") || !strings.Contains(err.Error(), "Access denied") {
		t.Fatalf("expected actionable manager failure, got %v", err)
	}
}

func TestDaemonAPIProxyUsesNativeCredentialAndCorrectHeaderWithoutReturningSecret(t *testing.T) {
	var header string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Get("X-FSE-API-Key")
		if r.URL.Path != "/v1/folders" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"folders": []any{map[string]any{"id": "docs"}}})
	}))
	defer server.Close()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{
		apiClient: server.Client(),
		credentialResolver: func(ref string) (string, error) {
			if ref != "native://local" {
				t.Fatalf("ref = %q", ref)
			}
			return "native-secret", nil
		},
	}
	response, err := app.DaemonAPIRequest(NativeDaemonAPIRequest{APIBaseURL: server.URL, CredentialRef: "native://local", Method: "GET", Path: "/v1/folders"})
	if err != nil {
		t.Fatalf("proxy: %v", err)
	}
	if header != "native-secret" {
		t.Fatalf("auth header = %q", header)
	}
	if strings.Contains(string(response.Body), "native-secret") {
		t.Fatalf("proxy leaked credential: %s", response.Body)
	}
}

func TestDaemonAPIProxyRestrictsMethodPathAndBody(t *testing.T) {
	app := NewApp()
	for _, request := range []NativeDaemonAPIRequest{
		{APIBaseURL: "https://127.0.0.1", CredentialRef: "x", Method: "DELETE", Path: "/v1/config"},
		{APIBaseURL: "https://127.0.0.1", CredentialRef: "x", Method: "GET", Path: "/v1/folder-file"},
		{APIBaseURL: "https://127.0.0.1", CredentialRef: "x", Method: "POST", Path: "/v1/peer-command", Body: make([]byte, maxNativeProxyBodyBytes+1)},
	} {
		if _, err := app.DaemonAPIRequest(request); err == nil {
			t.Fatalf("request should be rejected: %#v", request)
		}
	}
}

func TestLoadConfiguredServiceCandidateReadsJSONCWithoutExposingAPIKey(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.jsonc")
	if err := os.WriteFile(configPath, []byte(`{
		// service config
		"nodeName":"service-node",
		"api":{"listen":"127.0.0.1:22420","apiKey":"super-secret","encryption":{"mode":"manual-tls","certFile":"/tmp/api.crt"}}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	candidate, err := loadConfiguredServiceCandidate("systemd-user:fse", "systemd-user", "fse", configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	encoded, _ := json.Marshal(candidate)
	if candidate.APIBaseURL != "https://127.0.0.1:22420" || !strings.HasPrefix(candidate.CredentialRef, "config://") {
		t.Fatalf("candidate = %#v", candidate)
	}
	if strings.Contains(string(encoded), "super-secret") {
		t.Fatalf("candidate leaked secret: %s", encoded)
	}
}

func TestDaemonAPIProxyPreservesDaemonErrorMessage(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"folder id is required"}`)
	}))
	defer server.Close()
	app := NewApp()
	app.desktop = &desktopNativeRuntime{apiClient: server.Client(), credentialResolver: func(string) (string, error) { return "secret", nil }}
	_, err := app.DaemonAPIRequest(NativeDaemonAPIRequest{APIBaseURL: server.URL, CredentialRef: "native://local", Method: "POST", Path: "/v1/folder-command", Body: []byte(`{}`)})
	if err == nil || !strings.Contains(err.Error(), "folder id is required") {
		t.Fatalf("error = %v", err)
	}
}
