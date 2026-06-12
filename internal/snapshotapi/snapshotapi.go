package snapshotapi

import (
	"time"

	"filesyncengine/internal/api"
	"filesyncengine/internal/apicontrol"
	"filesyncengine/internal/backup"
	"filesyncengine/internal/cli"
	"filesyncengine/internal/config"
	"filesyncengine/internal/snapshotcontrol"
	"filesyncengine/internal/state"
)

type Clock func() time.Time

func MarkerResponse(req api.SnapshotRequest, cfg config.Config, store state.JSONStore, configPath string, now Clock) (api.SnapshotResponse, error) {
	if req.Action == string(cli.ActionList) {
		markers, err := store.ListSnapshotMarkers(req.FolderID)
		if err != nil {
			return api.SnapshotResponse{}, err
		}
		return apicontrol.SnapshotMarkersResponse(markers), nil
	}
	markerID := req.ID
	description := req.Description
	if req.Action == string(cli.ActionCreate) {
		markerID = req.FolderID
	}
	marker, err := snapshotcontrol.RunMarkerAction(cli.Action(req.Action), cfg, store, markerID, description, configPath, now().UTC())
	if err != nil {
		return api.SnapshotResponse{}, err
	}
	return apicontrol.SnapshotMarkersResponse([]state.SnapshotMarker{marker}), nil
}

func RestorePlanResponse(req api.RestorePlanRequest, cfg config.Config, store state.JSONStore, configPath string) (api.RestorePlanResponse, error) {
	plan, err := snapshotcontrol.PlanRestore(cfg, store, configPath, req.SnapshotID, req.Paths, req.DestinationRoot, req.AlternatePath)
	if err != nil {
		return api.RestorePlanResponse{}, err
	}
	return apicontrol.RestorePlanResponse(plan), nil
}

func RestoreResponse(req api.RestoreRequest, cfg config.Config, store state.JSONStore, configPath string, now Clock) (api.RestoreResponse, error) {
	started := now().UTC()
	result, err := snapshotcontrol.ExecuteRestore(cfg, store, configPath, req.SnapshotID, req.Paths, req.DestinationRoot, req.AlternatePath)
	if err != nil {
		return api.RestoreResponse{}, err
	}
	response := apicontrol.RestoreResponse(result)
	response.StartedAt = started
	response.FinishedAt = now().UTC()
	return response, nil
}

func RetentionResponse(req api.SnapshotRetentionRequest, cfg config.Config, store state.JSONStore, configPath string, now Clock) (api.SnapshotRetentionResponse, error) {
	started := now().UTC()
	plan, err := backup.ExecuteSnapshotRetention(backup.SnapshotRetentionOptions{Store: store, KeepLast: req.KeepLast, ArchiveRoot: snapshotcontrol.ArchivePath(cfg, configPath)})
	if err != nil {
		return api.SnapshotRetentionResponse{}, err
	}
	response := apicontrol.SnapshotRetentionResponse(plan)
	response.StartedAt = started
	response.FinishedAt = now().UTC()
	return response, nil
}
