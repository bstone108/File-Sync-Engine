package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type desktopAppUpdateState struct {
	LastCheckAt             string `json:"lastCheckAt,omitempty"`
	NotifiedAppImageVersion string `json:"notifiedAppImageVersion,omitempty"`
	PostponedVersion        string `json:"postponedVersion,omitempty"`
	StagedPath              string `json:"stagedPath,omitempty"`
	StagedVersion           string `json:"stagedVersion,omitempty"`
	StagedAssetName         string `json:"stagedAssetName,omitempty"`
	StagedSHA256            string `json:"stagedSHA256,omitempty"`
}

func (a *App) loadDesktopAppUpdateState() desktopAppUpdateState {
	path, err := a.desktopAppUpdateStatePath()
	if err != nil {
		return desktopAppUpdateState{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return desktopAppUpdateState{}
	}
	var state desktopAppUpdateState
	if err := json.Unmarshal(data, &state); err != nil {
		return desktopAppUpdateState{}
	}
	return state
}

func (a *App) saveDesktopAppUpdateState(state desktopAppUpdateState) error {
	path, err := a.desktopAppUpdateStatePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data, 0o600)
}

func (a *App) desktopAppUpdateStatePath() (string, error) {
	root, err := a.desktopRuntime().ensureStateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "app-update-state.json"), nil
}

func parseUpdateTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}
