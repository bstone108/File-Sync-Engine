package clicontrol

import (
	"fmt"
	"strings"

	"filesyncengine/internal/cli"
	"filesyncengine/internal/config"
	"filesyncengine/internal/configops"
)

// HandleConfig applies scriptable config CLI actions and returns printable output.
func HandleConfig(opts cli.Options, configPath string) (string, error) {
	switch opts.Action {
	case cli.ActionInit:
		if _, _, err := config.EnsureFile(configPath); err != nil {
			return "", err
		}
		return fmt.Sprintf("config ready: %s\n", configPath), nil
	case cli.ActionShow:
		cfg, err := config.LoadFile(configPath)
		if err != nil {
			return "", err
		}
		data, err := config.RedactedJSON(cfg)
		if err != nil {
			return "", err
		}
		return string(data), nil
	default:
		return "", fmt.Errorf("config action %s not implemented yet", opts.Action)
	}
}

// HandlePeer applies scriptable peer CLI actions and returns the text that should
// be printed by the command wrapper. Keeping mutation/listing here lets cmd/fse
// stay a thin process/exit-code boundary.
func HandlePeer(opts cli.Options, configPath string) (string, error) {
	switch opts.Action {
	case cli.ActionAdd:
		if err := configops.AddPeer(configPath, opts.ID, opts.Endpoint); err != nil {
			return "", err
		}
		return fmt.Sprintf("peer added: %s\n", opts.ID), nil
	case cli.ActionRemove:
		if err := configops.RemovePeer(configPath, opts.ID); err != nil {
			return "", err
		}
		return fmt.Sprintf("peer removed: %s\n", opts.ID), nil
	case cli.ActionUpdate:
		if err := configops.UpdatePeer(configPath, opts.ID, opts.Endpoint); err != nil {
			return "", err
		}
		return fmt.Sprintf("peer updated: %s\n", opts.ID), nil
	case cli.ActionList:
		cfg, err := config.LoadFile(configPath)
		if err != nil {
			return "", err
		}
		var builder strings.Builder
		for _, peer := range cfg.Peers {
			fmt.Fprintln(&builder, peer.ID)
		}
		return builder.String(), nil
	default:
		return "", fmt.Errorf("peer action %s not implemented yet", opts.Action)
	}
}

// HandleFolder applies scriptable folder CLI actions and returns the text that
// should be printed by the command wrapper.
func HandleFolder(opts cli.Options, configPath string) (string, error) {
	switch opts.Action {
	case cli.ActionAdd:
		if err := configops.AddFolder(configPath, opts.ID, opts.Path, opts.Mode); err != nil {
			return "", err
		}
		return fmt.Sprintf("folder added: %s\n", opts.ID), nil
	case cli.ActionRemove:
		if err := configops.RemoveFolder(configPath, opts.ID); err != nil {
			return "", err
		}
		return fmt.Sprintf("folder removed: %s\n", opts.ID), nil
	case cli.ActionUpdate:
		if err := configops.UpdateFolder(configPath, opts.ID, opts.Path, opts.Mode); err != nil {
			return "", err
		}
		return fmt.Sprintf("folder updated: %s\n", opts.ID), nil
	case cli.ActionList:
		cfg, err := config.LoadFile(configPath)
		if err != nil {
			return "", err
		}
		var builder strings.Builder
		for _, folder := range cfg.Folders {
			fmt.Fprintf(&builder, "%s\t%s\t%s\n", folder.ID, folder.Mode, folder.Path)
		}
		return builder.String(), nil
	default:
		return "", fmt.Errorf("folder action %s not implemented yet", opts.Action)
	}
}
