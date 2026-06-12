package streamcontrol

import (
	"context"
	"fmt"
	"io"

	"filesyncengine/internal/cli"
	"filesyncengine/internal/config"
	"filesyncengine/internal/discovery"
	"filesyncengine/internal/streamsync"
	"filesyncengine/internal/transport"
)

// RunOptions contains the runtime dependencies for the scriptable stream bridge.
// The caller owns loading config and deciding how to report returned summaries.
type RunOptions struct {
	Config config.Config
	CLI    cli.Options
	In     io.Reader
	Out    io.Writer
}

// LoadConfigFunc loads a stream command's active configuration.
type LoadConfigFunc func(path string) (config.Config, error)

// Runner executes an already configured stream action.
type Runner func(RunOptions) (RunResult, error)

// ConfiguredOptions contains dependencies for loading config and running a stream action.
type ConfiguredOptions struct {
	ConfigPath string
	CLI        cli.Options
	In         io.Reader
	Out        io.Writer
	LoadConfig LoadConfigFunc
	Run        Runner
}

// RunConfigured loads the active config, then executes the stream action with caller-supplied pipes.
func RunConfigured(opts ConfiguredOptions) (RunResult, error) {
	load := opts.LoadConfig
	if load == nil {
		load = config.LoadFile
	}
	run := opts.Run
	if run == nil {
		run = Run
	}
	cfg, err := load(opts.ConfigPath)
	if err != nil {
		return RunResult{}, err
	}
	return run(RunOptions{Config: cfg, CLI: opts.CLI, In: opts.In, Out: opts.Out})
}

// RunResult summarizes a completed stream action for CLI/API callers.
type RunResult struct {
	Pull *streamsync.PullResult
}

// Run executes a prototype stream serve/pull action over caller-supplied pipes.
func Run(opts RunOptions) (RunResult, error) {
	stream := transport.NewPipeStream(opts.In, opts.Out)
	switch opts.CLI.Action {
	case cli.ActionServe:
		folder, ok := findFolder(opts.Config, opts.CLI.ID)
		if !ok {
			return RunResult{}, fmt.Errorf("folder %q not found", opts.CLI.ID)
		}
		server := streamsync.NewServer(streamsync.ServerConfig{
			NodeID:         opts.Config.NodeName,
			BlockSize:      folder.BlockSize,
			Folders:        map[string]string{folder.ID: folder.Path},
			Transfer:       opts.Config.Transfer,
			IdentityGroups: discovery.IdentityGroupStatesFromConfig(opts.Config),
		})
		return RunResult{}, server.Serve(context.Background(), stream)
	case cli.ActionPull:
		result, err := streamsync.PullFolder(context.Background(), stream, streamsync.PullOptions{
			NodeID:         opts.Config.NodeName,
			FolderID:       opts.CLI.ID,
			LocalRoot:      opts.CLI.Path,
			Transfer:       opts.Config.Transfer,
			IdentityGroups: discovery.IdentityGroupStatesFromConfig(opts.Config),
		})
		if err != nil {
			return RunResult{}, err
		}
		return RunResult{Pull: &result}, nil
	default:
		return RunResult{}, fmt.Errorf("stream action %s not implemented", opts.CLI.Action)
	}
}

func findFolder(cfg config.Config, folderID string) (config.FolderConfig, bool) {
	for _, folder := range cfg.Folders {
		if folder.ID == folderID {
			return folder, true
		}
	}
	return config.FolderConfig{}, false
}
