package clicontrol

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"filesyncengine/internal/cli"
	"filesyncengine/internal/config"
)

func TestHandlePeerMutationsAndListReturnScriptableOutput(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.jsonc")
	if _, _, err := config.EnsureFile(configPath); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := HandlePeer(cli.Options{Action: cli.ActionAdd, ID: "peer-a", Endpoint: "pipe:stdio"}, configPath)
	if err != nil {
		t.Fatalf("add peer: %v", err)
	}
	if strings.TrimSpace(out) != "peer added: peer-a" {
		t.Fatalf("unexpected add output %q", out)
	}

	out, err = HandlePeer(cli.Options{Action: cli.ActionUpdate, ID: "peer-a", Endpoint: "pipe:updated"}, configPath)
	if err != nil {
		t.Fatalf("update peer: %v", err)
	}
	if strings.TrimSpace(out) != "peer updated: peer-a" {
		t.Fatalf("unexpected update output %q", out)
	}

	out, err = HandlePeer(cli.Options{Action: cli.ActionList}, configPath)
	if err != nil {
		t.Fatalf("list peer: %v", err)
	}
	if !strings.Contains(out, "peer-a\n") {
		t.Fatalf("expected peer-a in list output %q", out)
	}

	out, err = HandlePeer(cli.Options{Action: cli.ActionRemove, ID: "peer-a"}, configPath)
	if err != nil {
		t.Fatalf("remove peer: %v", err)
	}
	if strings.TrimSpace(out) != "peer removed: peer-a" {
		t.Fatalf("unexpected remove output %q", out)
	}
}

func TestHandleFolderMutationsAndListReturnScriptableOutput(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.jsonc")
	if _, _, err := config.EnsureFile(configPath); err != nil {
		t.Fatalf("write config: %v", err)
	}

	folderPath := filepath.Join(dir, "share")
	out, err := HandleFolder(cli.Options{Action: cli.ActionAdd, ID: "unique-docs", Path: folderPath, Mode: "sendrecv"}, configPath)
	if err != nil {
		t.Fatalf("add folder: %v", err)
	}
	if strings.TrimSpace(out) != "folder added: unique-docs" {
		t.Fatalf("unexpected add output %q", out)
	}

	out, err = HandleFolder(cli.Options{Action: cli.ActionUpdate, ID: "unique-docs", Path: folderPath, Mode: "sendonly"}, configPath)
	if err != nil {
		t.Fatalf("update folder: %v", err)
	}
	if strings.TrimSpace(out) != "folder updated: unique-docs" {
		t.Fatalf("unexpected update output %q", out)
	}

	out, err = HandleFolder(cli.Options{Action: cli.ActionList}, configPath)
	if err != nil {
		t.Fatalf("list folder: %v", err)
	}
	want := "unique-docs	sendonly	" + folderPath
	if !strings.Contains(out, want+"\n") {
		t.Fatalf("expected %q in list output %q", want, out)
	}

	out, err = HandleFolder(cli.Options{Action: cli.ActionRemove, ID: "unique-docs"}, configPath)
	if err != nil {
		t.Fatalf("remove folder: %v", err)
	}
	if strings.TrimSpace(out) != "folder removed: unique-docs" {
		t.Fatalf("unexpected remove output %q", out)
	}
}

func TestHandleConfigInitAndShowReturnScriptableOutput(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.jsonc")

	out, err := HandleConfig(cli.Options{Action: cli.ActionInit}, configPath)
	if err != nil {
		t.Fatalf("init config: %v", err)
	}
	if strings.TrimSpace(out) != "config ready: "+configPath {
		t.Fatalf("unexpected init output %q", out)
	}

	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatalf("load generated config: %v", err)
	}
	cfg.API.Key = "super-secret-api-key"
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("encode config with secret: %v", err)
	}
	if err := config.WriteFileAtomic(configPath, append(encoded, '\n'), 0o600); err != nil {
		t.Fatalf("write config with secret: %v", err)
	}

	out, err = HandleConfig(cli.Options{Action: cli.ActionShow}, configPath)
	if err != nil {
		t.Fatalf("show config: %v", err)
	}
	if strings.Contains(out, "super-secret-api-key") {
		t.Fatalf("config show leaked API key in %q", out)
	}
	if !strings.Contains(out, "REDACTED") {
		t.Fatalf("config show did not redact secrets in %q", out)
	}
}

func TestHandlePeerFolderAndConfigRejectUnsupportedActions(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.jsonc")
	if _, _, err := config.EnsureFile(configPath); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := HandlePeer(cli.Options{Action: cli.ActionStart}, configPath); err == nil || !strings.Contains(err.Error(), "peer action start not implemented") {
		t.Fatalf("expected peer unsupported action error, got %v", err)
	}
	if _, err := HandleFolder(cli.Options{Action: cli.ActionStart}, configPath); err == nil || !strings.Contains(err.Error(), "folder action start not implemented") {
		t.Fatalf("expected folder unsupported action error, got %v", err)
	}
	if _, err := HandleConfig(cli.Options{Action: cli.ActionStart}, configPath); err == nil || !strings.Contains(err.Error(), "config action start not implemented") {
		t.Fatalf("expected config unsupported action error, got %v", err)
	}
}
