package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const remoteCredentialVaultService = "File Synchronization Engine Desktop"

var errCredentialNotFound = errors.New("credential not found")

type RemoteInstanceCredentialRecord struct {
	CredentialRef string `json:"credentialRef"`
	Platform      string `json:"platform"`
	InstanceID    string `json:"instanceID"`
	Label         string `json:"label"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

type RemoteInstanceCredentialSecret struct {
	CredentialRef string `json:"credentialRef"`
	SecretValue   string `json:"secretValue"`
}

type RemoteInstanceRegistryEntry struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	APIBaseURL      string `json:"apiBaseURL"`
	CredentialRef   string `json:"credentialRef"`
	Source          string `json:"source"`
	ConnectionState string `json:"connectionState"`
	Revision        uint64 `json:"revision"`
}

type RemoteInstanceRegistry struct {
	SelectedInstanceID       string                        `json:"selectedInstanceID,omitempty"`
	Instances                []RemoteInstanceRegistryEntry `json:"instances"`
	CredentialCleanupPending []string                      `json:"credentialCleanupPending,omitempty"`
}

type RemoteInstanceUpdateRequest struct {
	ID                    string `json:"id"`
	ExpectedCredentialRef string `json:"expectedCredentialRef"`
	ExpectedRevision      uint64 `json:"expectedRevision"`
	Label                 string `json:"label"`
	APIBaseURL            string `json:"apiBaseURL"`
}

type RemoteInstanceRemovalRequest struct {
	ID                    string `json:"id"`
	ExpectedCredentialRef string `json:"expectedCredentialRef"`
	ExpectedRevision      uint64 `json:"expectedRevision"`
	ConfirmLabel          string `json:"confirmLabel"`
}

type RemoteInstanceSelectionRequest struct {
	InstanceID                 string `json:"instanceID"`
	ExpectedSelectedInstanceID string `json:"expectedSelectedInstanceID"`
}

type RemoteInstanceOnboardingRequest struct {
	Entry       RemoteInstanceRegistryEntry `json:"entry"`
	SecretValue string                      `json:"secretValue"`
}

func (a *App) GetRemoteInstanceRegistry() (RemoteInstanceRegistry, error) {
	a.remoteInstanceRegistry.Lock()
	defer a.remoteInstanceRegistry.Unlock()
	return a.getRemoteInstanceRegistry()
}

func (a *App) getRemoteInstanceRegistry() (RemoteInstanceRegistry, error) {
	path, err := a.remoteInstanceRegistryPath()
	if err != nil {
		return RemoteInstanceRegistry{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return RemoteInstanceRegistry{Instances: []RemoteInstanceRegistryEntry{}}, nil
	}
	if err != nil {
		return RemoteInstanceRegistry{}, fmt.Errorf("read remote instance registry: %w", err)
	}
	var registry RemoteInstanceRegistry
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&registry); err != nil {
		return RemoteInstanceRegistry{}, fmt.Errorf("parse remote instance registry: %w", err)
	}
	for index := range registry.Instances {
		if registry.Instances[index].Revision == 0 {
			registry.Instances[index].Revision = 1
		}
	}
	if err := validateRemoteInstanceRegistry(registry); err != nil {
		return RemoteInstanceRegistry{}, fmt.Errorf("stored remote instance registry is invalid: %w", err)
	}
	return registry, nil
}

func (a *App) saveRemoteInstanceRegistryForTest(registry RemoteInstanceRegistry) (RemoteInstanceRegistry, error) {
	a.remoteInstanceRegistry.Lock()
	defer a.remoteInstanceRegistry.Unlock()
	return a.saveRemoteInstanceRegistry(registry)
}

func (a *App) saveRemoteInstanceRegistry(registry RemoteInstanceRegistry) (RemoteInstanceRegistry, error) {
	for index := range registry.Instances {
		if registry.Instances[index].Revision == 0 {
			registry.Instances[index].Revision = 1
		}
	}
	if err := validateRemoteInstanceRegistry(registry); err != nil {
		return RemoteInstanceRegistry{}, err
	}
	path, err := a.remoteInstanceRegistryPath()
	if err != nil {
		return RemoteInstanceRegistry{}, err
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return RemoteInstanceRegistry{}, err
	}
	write := a.desktopRuntime().remoteRegistryWrite
	if write == nil {
		write = atomicWriteRemoteInstanceRegistry
	}
	if err := write(path, data); err != nil {
		return RemoteInstanceRegistry{}, fmt.Errorf("save remote instance registry: %w", err)
	}
	return registry, nil
}

func (a *App) SelectRemoteInstance(request RemoteInstanceSelectionRequest) (RemoteInstanceRegistry, error) {
	a.remoteInstanceRegistry.Lock()
	defer a.remoteInstanceRegistry.Unlock()
	registry, err := a.getRemoteInstanceRegistry()
	if err != nil {
		return RemoteInstanceRegistry{}, err
	}
	if registry.SelectedInstanceID != request.ExpectedSelectedInstanceID {
		return RemoteInstanceRegistry{}, errors.New("remote host selection changed; reload before selecting")
	}
	if request.InstanceID != "" {
		found := false
		for _, entry := range registry.Instances {
			if entry.ID == request.InstanceID {
				found = true
				break
			}
		}
		if !found {
			return RemoteInstanceRegistry{}, fmt.Errorf("remote instance %q does not exist", request.InstanceID)
		}
	}
	registry.SelectedInstanceID = request.InstanceID
	return a.saveRemoteInstanceRegistry(registry)
}

func (a *App) OnboardRemoteInstance(request RemoteInstanceOnboardingRequest) (RemoteInstanceRegistry, error) {
	a.remoteInstanceRegistry.Lock()
	defer a.remoteInstanceRegistry.Unlock()
	registry, err := a.getRemoteInstanceRegistry()
	if err != nil {
		return RemoteInstanceRegistry{}, err
	}
	entry := request.Entry
	if entry.Revision == 0 {
		entry.Revision = 1
	}
	if err := validateRemoteInstanceRegistry(RemoteInstanceRegistry{Instances: []RemoteInstanceRegistryEntry{entry}}); err != nil {
		return registry, err
	}
	for _, existing := range registry.Instances {
		if existing.ID == entry.ID {
			return registry, fmt.Errorf("remote instance %q already exists", entry.ID)
		}
	}
	candidateRegistry := registry
	candidateRegistry.Instances = append(append([]RemoteInstanceRegistryEntry{}, registry.Instances...), entry)
	if err := validateRemoteInstanceRegistry(candidateRegistry); err != nil {
		return registry, err
	}
	if strings.TrimSpace(request.SecretValue) == "" {
		return registry, errors.New("remote API credential is required")
	}
	pending := registry
	pending.CredentialCleanupPending = appendUniqueCredentialRef(pending.CredentialCleanupPending, entry.CredentialRef)
	if _, err := a.saveRemoteInstanceRegistry(pending); err != nil {
		return registry, err
	}
	record := RemoteInstanceCredentialRecord{CredentialRef: entry.CredentialRef, InstanceID: entry.ID, Label: entry.Label}
	if _, err := a.StoreRemoteInstanceCredential(record, RemoteInstanceCredentialSecret{CredentialRef: entry.CredentialRef, SecretValue: request.SecretValue}); err != nil {
		return pending, err
	}
	next := pending
	next.CredentialCleanupPending = removeCredentialRef(next.CredentialCleanupPending, entry.CredentialRef)
	next.Instances = append(next.Instances, entry)
	next.SelectedInstanceID = entry.ID
	saved, commitErr := a.saveRemoteInstanceRegistry(next)
	if commitErr == nil {
		return saved, nil
	}
	if cleanupErr := a.DeleteRemoteInstanceCredential(entry.CredentialRef); cleanupErr == nil {
		clean := pending
		clean.CredentialCleanupPending = removeCredentialRef(clean.CredentialCleanupPending, entry.CredentialRef)
		if cleaned, cleanErr := a.saveRemoteInstanceRegistry(clean); cleanErr == nil {
			return cleaned, commitErr
		}
	}
	return pending, commitErr
}

func appendUniqueCredentialRef(refs []string, ref string) []string {
	for _, existing := range refs {
		if existing == ref {
			return refs
		}
	}
	return append(refs, ref)
}

func removeCredentialRef(refs []string, ref string) []string {
	result := make([]string, 0, len(refs))
	for _, existing := range refs {
		if existing != ref {
			result = append(result, existing)
		}
	}
	return result
}

func (a *App) UpdateRemoteInstance(request RemoteInstanceUpdateRequest) (RemoteInstanceRegistry, error) {
	a.remoteInstanceRegistry.Lock()
	defer a.remoteInstanceRegistry.Unlock()
	registry, err := a.getRemoteInstanceRegistry()
	if err != nil {
		return RemoteInstanceRegistry{}, err
	}
	for index, entry := range registry.Instances {
		if entry.ID != request.ID {
			continue
		}
		if request.ExpectedCredentialRef == "" || request.ExpectedCredentialRef != entry.CredentialRef || request.ExpectedRevision == 0 || request.ExpectedRevision != entry.Revision {
			return RemoteInstanceRegistry{}, errors.New("remote host changed since this edit was opened; reload before editing")
		}
		entry.Label = strings.TrimSpace(request.Label)
		entry.APIBaseURL = strings.TrimSpace(request.APIBaseURL)
		entry.ConnectionState = "offline"
		entry.Revision++
		registry.Instances[index] = entry
		return a.saveRemoteInstanceRegistry(registry)
	}
	return RemoteInstanceRegistry{}, fmt.Errorf("remote instance %q does not exist", request.ID)
}

func (a *App) RemoveRemoteInstance(request RemoteInstanceRemovalRequest) (RemoteInstanceRegistry, error) {
	a.remoteInstanceRegistry.Lock()
	defer a.remoteInstanceRegistry.Unlock()
	registry, err := a.getRemoteInstanceRegistry()
	if err != nil {
		return RemoteInstanceRegistry{}, err
	}
	index := -1
	var removed RemoteInstanceRegistryEntry
	for candidateIndex, entry := range registry.Instances {
		if entry.ID == request.ID {
			index, removed = candidateIndex, entry
			break
		}
	}
	if index < 0 {
		return RemoteInstanceRegistry{}, fmt.Errorf("remote instance %q does not exist", request.ID)
	}
	if request.ConfirmLabel != removed.Label {
		return RemoteInstanceRegistry{}, errors.New("type the remote host label exactly to confirm removal")
	}
	if request.ExpectedCredentialRef == "" || request.ExpectedCredentialRef != removed.CredentialRef || request.ExpectedRevision == 0 || request.ExpectedRevision != removed.Revision {
		return RemoteInstanceRegistry{}, errors.New("remote host changed since removal was opened; reload before removing")
	}
	next := RemoteInstanceRegistry{SelectedInstanceID: registry.SelectedInstanceID, Instances: append([]RemoteInstanceRegistryEntry{}, registry.Instances[:index]...), CredentialCleanupPending: append([]string{}, registry.CredentialCleanupPending...)}
	next.Instances = append(next.Instances, registry.Instances[index+1:]...)
	if next.SelectedInstanceID == removed.ID {
		next.SelectedInstanceID = ""
	}
	next.CredentialCleanupPending = appendUniqueCredentialRef(next.CredentialCleanupPending, removed.CredentialRef)
	if _, err := a.saveRemoteInstanceRegistry(next); err != nil {
		return RemoteInstanceRegistry{}, err
	}
	if err := a.DeleteRemoteInstanceCredential(removed.CredentialRef); err != nil {
		return next, nil
	}
	clean := next
	clean.CredentialCleanupPending = removeCredentialRef(clean.CredentialCleanupPending, removed.CredentialRef)
	saved, err := a.saveRemoteInstanceRegistry(clean)
	if err != nil {
		return next, fmt.Errorf("credential was deleted but cleanup tombstone could not be cleared; retry reconciliation: %w", err)
	}
	return saved, nil
}

// ReconcileRemoteInstanceCredentialCleanup retries durable native-vault cleanup.
// A tombstone is removed only when the vault backend confirms no matching item remains.
func (a *App) ReconcileRemoteInstanceCredentialCleanup() (RemoteInstanceRegistry, error) {
	a.remoteInstanceRegistry.Lock()
	defer a.remoteInstanceRegistry.Unlock()
	registry, err := a.getRemoteInstanceRegistry()
	if err != nil {
		return RemoteInstanceRegistry{}, err
	}
	remaining := append([]string{}, registry.CredentialCleanupPending...)
	var failures []error
	for _, ref := range registry.CredentialCleanupPending {
		if err := a.DeleteRemoteInstanceCredential(ref); err != nil {
			failures = append(failures, fmt.Errorf("credential cleanup %s: %w", ref, err))
			continue
		}
		remaining = removeCredentialRef(remaining, ref)
	}
	if len(remaining) != len(registry.CredentialCleanupPending) {
		clean := registry
		clean.CredentialCleanupPending = remaining
		saved, saveErr := a.saveRemoteInstanceRegistry(clean)
		if saveErr != nil {
			return registry, errors.Join(errors.Join(failures...), fmt.Errorf("persist credential cleanup reconciliation: %w", saveErr))
		}
		registry = saved
	}
	return registry, errors.Join(failures...)
}

func atomicWriteRemoteInstanceRegistry(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".remote-instances-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (a *App) remoteInstanceRegistryPath() (string, error) {
	root, err := a.desktopRuntime().ensureStateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "remote-instances.json"), nil
}

func validateRemoteInstanceRegistry(registry RemoteInstanceRegistry) error {
	if len(registry.Instances) > 128 {
		return errors.New("remote instance registry exceeds 128 entries")
	}
	seen := make(map[string]bool, len(registry.Instances))
	credentialOwners := make(map[string]string, len(registry.Instances)+len(registry.CredentialCleanupPending))
	for _, ref := range registry.CredentialCleanupPending {
		if err := validateRemoteCredentialRef(ref); err != nil {
			return fmt.Errorf("invalid credential cleanup tombstone: %w", err)
		}
		if owner, exists := credentialOwners[ref]; exists {
			return fmt.Errorf("credential reference %q is not globally unique; already used by %s", ref, owner)
		}
		credentialOwners[ref] = "credential cleanup tombstone"
	}
	for _, entry := range registry.Instances {
		if entry.Source != "api-endpoint-key" {
			return fmt.Errorf("remote instance %q has unsupported onboarding source", entry.ID)
		}
		if entry.ConnectionState != "offline" && entry.ConnectionState != "connecting" && entry.ConnectionState != "online" && entry.ConnectionState != "failed" {
			return fmt.Errorf("remote instance %q has unsupported connection state", entry.ID)
		}
		if strings.TrimSpace(entry.ID) == "" || len(entry.ID) > 128 || strings.TrimSpace(entry.Label) == "" || len(entry.Label) > 96 {
			return errors.New("remote instance ID and label are required and must fit registry limits")
		}
		if seen[entry.ID] {
			return fmt.Errorf("duplicate remote instance ID %q", entry.ID)
		}
		seen[entry.ID] = true
		if err := validateRemoteCredentialRef(entry.CredentialRef); err != nil {
			return err
		}
		if owner, exists := credentialOwners[entry.CredentialRef]; exists {
			return fmt.Errorf("credential reference %q is not globally unique; remote instance %q overlaps %s", entry.CredentialRef, entry.ID, owner)
		}
		credentialOwners[entry.CredentialRef] = fmt.Sprintf("remote instance %q", entry.ID)
		endpoint, err := url.Parse(entry.APIBaseURL)
		if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
			return fmt.Errorf("remote instance %q requires an HTTPS API endpoint without embedded credentials, query parameters, or fragments", entry.ID)
		}
	}
	if registry.SelectedInstanceID != "" && !seen[registry.SelectedInstanceID] {
		return errors.New("selected remote instance does not exist in registry")
	}
	return nil
}

func (a *App) StoreRemoteInstanceCredential(record RemoteInstanceCredentialRecord, secret RemoteInstanceCredentialSecret) (RemoteInstanceCredentialRecord, error) {
	if err := validateRemoteCredentialRef(record.CredentialRef); err != nil {
		return RemoteInstanceCredentialRecord{}, err
	}
	if record.CredentialRef != secret.CredentialRef || strings.TrimSpace(secret.SecretValue) == "" {
		return RemoteInstanceCredentialRecord{}, errors.New("matching credential reference and non-empty secret are required")
	}
	if strings.TrimSpace(record.InstanceID) == "" || strings.TrimSpace(record.Label) == "" {
		return RemoteInstanceCredentialRecord{}, errors.New("remote instance ID and label are required")
	}
	rt := a.desktopRuntime()
	set := rt.credentialVaultSet
	if set == nil {
		set = nativeCredentialVaultSet
	}
	if err := set(remoteCredentialVaultService, record.CredentialRef, secret.SecretValue); err != nil {
		return RemoteInstanceCredentialRecord{}, fmt.Errorf("store remote API credential in native vault: %w", err)
	}
	record.Platform = rt.platform
	if record.Platform == "" {
		record.Platform = runtimeTargetOS()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if record.CreatedAt == "" {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	return record, nil
}

func (a *App) DeleteRemoteInstanceCredential(credentialRef string) error {
	if err := validateRemoteCredentialRef(credentialRef); err != nil {
		return err
	}
	rt := a.desktopRuntime()
	remove := rt.credentialVaultDelete
	if remove == nil {
		remove = nativeCredentialVaultDelete
	}
	if err := remove(remoteCredentialVaultService, credentialRef); err != nil && !errors.Is(err, errCredentialNotFound) {
		return fmt.Errorf("delete remote API credential from native vault: %w", err)
	}
	return nil
}

func validateRemoteCredentialRef(ref string) error {
	const prefix = "desktop-vault:remote:"
	if !strings.HasPrefix(ref, prefix) || len(ref) <= len(prefix) || len(ref) > 160 {
		return errors.New("remote credential reference must use desktop-vault:remote:<id>")
	}
	for _, r := range strings.TrimPrefix(ref, prefix) {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && !strings.ContainsRune("_.:-", r) {
			return errors.New("remote credential reference contains unsupported characters")
		}
	}
	return nil
}

type BundledEngineManifestEntry struct {
	Target             string `json:"target"`
	RelativePath       string `json:"relativePath"`
	ExpectedExecutable string `json:"expectedExecutable"`
	ExpectedVersion    string `json:"expectedVersion"`
	ExpectedSHA256     string `json:"expectedSHA256"`
}

type BundledEngineInspectionEntry struct {
	BundledEngineManifestEntry
	Exists   bool   `json:"exists"`
	SHA256   string `json:"sha256,omitempty"`
	Verified bool   `json:"verified"`
	Message  string `json:"message"`
}

type BundledEngineInspection struct {
	Version  string                         `json:"version"`
	Verified bool                           `json:"verified"`
	Entries  []BundledEngineInspectionEntry `json:"entries"`
	Message  string                         `json:"message"`
}

type DesktopPreferences struct {
	Theme                string `json:"theme"`
	Density              string `json:"density"`
	MinimizeToTray       bool   `json:"minimizeToTray"`
	NotificationsEnabled bool   `json:"notificationsEnabled"`
}

func defaultDesktopPreferences() DesktopPreferences {
	return DesktopPreferences{Theme: "system", Density: "comfortable", NotificationsEnabled: true}
}

func (a *App) InspectBundledEngineResources() (BundledEngineInspection, error) {
	root := a.desktopRuntime().resourceRoot
	if root == "" {
		exe, err := os.Executable()
		if err != nil {
			return BundledEngineInspection{}, err
		}
		root = filepath.Join(filepath.Dir(exe), "resources", "engine")
	}
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return BundledEngineInspection{}, fmt.Errorf("read bundled engine manifest: %w", err)
	}
	var manifest struct {
		Version string                       `json:"version"`
		Entries []BundledEngineManifestEntry `json:"entries"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return BundledEngineInspection{}, fmt.Errorf("parse bundled engine manifest: %w", err)
	}
	if strings.TrimSpace(manifest.Version) == "" || len(manifest.Entries) == 0 {
		return BundledEngineInspection{}, errors.New("bundled engine manifest has no version or entries")
	}
	result := BundledEngineInspection{Version: manifest.Version, Verified: true}
	currentTarget := runtimeTargetOS() + "-" + runtimeTargetArch()
	currentTargetFound := false
	for _, entry := range manifest.Entries {
		observed, err := inspectBundledEngineEntry(root, manifest.Version, entry)
		if err != nil {
			return BundledEngineInspection{}, err
		}
		if entry.Target == currentTarget {
			currentTargetFound = true
			if !observed.Verified {
				result.Verified = false
			}
		} else if observed.Exists && !observed.Verified {
			result.Verified = false
		}
		result.Entries = append(result.Entries, observed)
	}
	if !currentTargetFound {
		result.Verified = false
		result.Message = fmt.Sprintf("Manifest has no entry for this desktop target (%s).", currentTarget)
	} else if result.Verified {
		result.Message = "The engine for this desktop target and every other packaged engine file match the manifest SHA-256 digests."
	} else {
		result.Message = "The current-target engine is missing or one or more packaged engine resources do not match the manifest."
	}
	return result, nil
}

func inspectBundledEngineEntry(root, manifestVersion string, entry BundledEngineManifestEntry) (BundledEngineInspectionEntry, error) {
	result := BundledEngineInspectionEntry{BundledEngineManifestEntry: entry}
	clean := filepath.Clean(filepath.FromSlash(entry.RelativePath))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return result, fmt.Errorf("unsafe bundled engine path %q", entry.RelativePath)
	}
	path := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return result, fmt.Errorf("unsafe bundled engine path %q", entry.RelativePath)
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		result.Message = "Packaged resource is absent from this target-specific application bundle."
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("open bundled engine %q: %w", entry.Target, err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return result, fmt.Errorf("hash bundled engine %q: %w", entry.Target, err)
	}
	result.Exists = true
	result.SHA256 = hex.EncodeToString(hash.Sum(nil))
	result.Verified = entry.ExpectedVersion == manifestVersion && strings.EqualFold(result.SHA256, entry.ExpectedSHA256) && filepath.Base(path) == entry.ExpectedExecutable
	if result.Verified {
		result.Message = "Packaged resource matches manifest version, executable name, and SHA-256 digest."
	} else {
		result.Message = "Packaged resource does not match its manifest metadata."
	}
	return result, nil
}

func (a *App) GetDesktopPreferences() (DesktopPreferences, error) {
	path, err := a.desktopPreferencesPath()
	if err != nil {
		return DesktopPreferences{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultDesktopPreferences(), nil
	}
	if err != nil {
		return DesktopPreferences{}, err
	}
	var preferences DesktopPreferences
	if err := json.Unmarshal(data, &preferences); err != nil {
		return DesktopPreferences{}, fmt.Errorf("read desktop preferences: %w", err)
	}
	if err := validateDesktopPreferences(preferences); err != nil {
		return DesktopPreferences{}, fmt.Errorf("stored desktop preferences are invalid: %w", err)
	}
	return preferences, nil
}

func (a *App) SaveDesktopPreferences(preferences DesktopPreferences) (DesktopPreferences, error) {
	if err := validateDesktopPreferences(preferences); err != nil {
		return DesktopPreferences{}, err
	}
	path, err := a.desktopPreferencesPath()
	if err != nil {
		return DesktopPreferences{}, err
	}
	data, err := json.MarshalIndent(preferences, "", "  ")
	if err != nil {
		return DesktopPreferences{}, err
	}
	if err := atomicWriteFile(path, data, 0o600); err != nil {
		return DesktopPreferences{}, err
	}
	return preferences, nil
}

func (a *App) desktopPreferencesPath() (string, error) {
	root, err := a.desktopRuntime().ensureStateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "desktop-preferences.json"), nil
}

func validateDesktopPreferences(preferences DesktopPreferences) error {
	if preferences.Theme != "system" && preferences.Theme != "light" && preferences.Theme != "dark" {
		return fmt.Errorf("unsupported desktop theme %q", preferences.Theme)
	}
	if preferences.Density != "comfortable" && preferences.Density != "compact" {
		return fmt.Errorf("unsupported desktop density %q", preferences.Density)
	}
	return nil
}
