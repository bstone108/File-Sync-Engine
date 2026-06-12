package validatecontrol

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateConfigLoadsAndValidatesConfigFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"nodeName":"node-a","folders":[{"id":"docs","path":"./docs","mode":"sendrecv"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := ValidateConfig(configPath)
	if err != nil {
		t.Fatalf("ValidateConfig returned error: %v", err)
	}
	if result.ConfigPath != configPath {
		t.Fatalf("ConfigPath = %q, want %q", result.ConfigPath, configPath)
	}
}

func TestValidateConfigReturnsConfigValidationErrors(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"nodeName":"node-a","folders":[{"id":"docs","path":"./docs","mode":"sendrecv"},{"id":"docs","path":"./other","mode":"sendrecv"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := ValidateConfig(configPath)
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if !strings.Contains(err.Error(), "duplicate folder id") {
		t.Fatalf("error = %q, want duplicate folder id", err.Error())
	}
}
