package maintenancecli

import (
	"fmt"

	"filesyncengine/internal/api"
	"filesyncengine/internal/cli"
	"filesyncengine/internal/clioutput"
	"filesyncengine/internal/maintenancecontrol"
)

type Runners struct {
	Scrub       func(cli.Options) ([]maintenancecontrol.ScrubResult, error)
	BackupScrub func(cli.Options) (api.BackupScrubResponse, error)
}

func Run(opts cli.Options, runners Runners) (string, error) {
	switch opts.Action {
	case cli.ActionScrub:
		if runners.Scrub == nil {
			return "", fmt.Errorf("maintenance scrub runner not configured")
		}
		results, err := runners.Scrub(opts)
		if err != nil {
			return "", err
		}
		return clioutput.MaintenanceScrubOutput(results), nil
	case cli.ActionBackupScrub:
		if runners.BackupScrub == nil {
			return "", fmt.Errorf("maintenance backup-scrub runner not configured")
		}
		result, err := runners.BackupScrub(opts)
		if err != nil {
			return "", err
		}
		return clioutput.BackupScrubOutput(result), nil
	default:
		return "", fmt.Errorf("maintenance action %s not implemented", opts.Action)
	}
}
