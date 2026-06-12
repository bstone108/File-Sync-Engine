package snapshotcli

import (
	"fmt"

	"filesyncengine/internal/backup"
	"filesyncengine/internal/cli"
	"filesyncengine/internal/clioutput"
	"filesyncengine/internal/state"
)

type Runners struct {
	List        func(cli.Options) ([]state.SnapshotMarker, error)
	RestorePlan func(cli.Options) (backup.RestorePlan, error)
	Restore     func(cli.Options) (backup.RestoreResult, error)
	Retention   func(cli.Options) (backup.SnapshotRetentionPlan, error)
	Marker      func(cli.Options) (state.SnapshotMarker, error)
}

func Run(opts cli.Options, runners Runners) (string, error) {
	switch opts.Action {
	case cli.ActionList:
		if runners.List == nil {
			return "", fmt.Errorf("snapshot list runner not configured")
		}
		markers, err := runners.List(opts)
		if err != nil {
			return "", err
		}
		return clioutput.SnapshotMarkersOutput(markers), nil
	case cli.ActionRestorePlan:
		if runners.RestorePlan == nil {
			return "", fmt.Errorf("snapshot restore-plan runner not configured")
		}
		plan, err := runners.RestorePlan(opts)
		if err != nil {
			return "", err
		}
		return clioutput.RestorePlanOutput(plan), nil
	case cli.ActionRestore:
		if runners.Restore == nil {
			return "", fmt.Errorf("snapshot restore runner not configured")
		}
		result, err := runners.Restore(opts)
		if err != nil {
			return "", err
		}
		return clioutput.RestoreResultOutput(result), nil
	case cli.ActionRetention:
		if runners.Retention == nil {
			return "", fmt.Errorf("snapshot retention runner not configured")
		}
		result, err := runners.Retention(opts)
		if err != nil {
			return "", err
		}
		return clioutput.SnapshotRetentionOutput(result), nil
	case cli.ActionCreate, cli.ActionShow, cli.ActionPin, cli.ActionDeprecate, cli.ActionDelete:
		if runners.Marker == nil {
			return "", fmt.Errorf("snapshot marker runner not configured")
		}
		marker, err := runners.Marker(opts)
		if err != nil {
			return "", err
		}
		if opts.Action == cli.ActionDelete {
			return clioutput.SnapshotDeletedOutput(marker.ID), nil
		}
		return clioutput.SnapshotMarkersOutput([]state.SnapshotMarker{marker}), nil
	default:
		return "", fmt.Errorf("snapshot action %s not implemented", opts.Action)
	}
}
