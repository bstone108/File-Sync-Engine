package metadatacli

import (
	"fmt"

	"filesyncengine/internal/cli"
	"filesyncengine/internal/clioutput"
	"filesyncengine/internal/metadataops"
	"filesyncengine/internal/state"
)

type Runners struct {
	Compact             func(cli.Options) ([]state.MetadataCompactionResult, error)
	CompactionStatePath func() string
	ImportJSON          func(cli.Options) (metadataops.Result, error)
	SplitBadger         func(cli.Options) (metadataops.Result, error)
}

func Run(opts cli.Options, runners Runners) (string, error) {
	switch opts.Action {
	case cli.ActionCompact:
		if runners.Compact == nil {
			return "", fmt.Errorf("metadata compact runner not configured")
		}
		results, err := runners.Compact(opts)
		if err != nil {
			return "", err
		}
		statePath := ""
		if runners.CompactionStatePath != nil {
			statePath = runners.CompactionStatePath()
		}
		return clioutput.MetadataCompactionOutput(results, statePath), nil
	case cli.ActionImportJSON:
		if runners.ImportJSON == nil {
			return "", fmt.Errorf("metadata import-json runner not configured")
		}
		result, err := runners.ImportJSON(opts)
		if err != nil {
			return "", err
		}
		return clioutput.MetadataImportOutput("import-json", result), nil
	case cli.ActionSplitBadger:
		if runners.SplitBadger == nil {
			return "", fmt.Errorf("metadata split-badger runner not configured")
		}
		result, err := runners.SplitBadger(opts)
		if err != nil {
			return "", err
		}
		return clioutput.MetadataImportOutput("split-badger", result), nil
	default:
		return "", fmt.Errorf("metadata action %s not implemented", opts.Action)
	}
}
