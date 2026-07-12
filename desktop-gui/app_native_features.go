package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
