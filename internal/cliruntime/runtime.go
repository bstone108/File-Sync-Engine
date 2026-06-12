package cliruntime

import (
	"filesyncengine/internal/cli"
	"filesyncengine/internal/commanddispatch"
	"filesyncengine/internal/configbootstrap"
)

// Runners are process-boundary command handlers. Run parses CLI args, resolves or
// creates the active config, then dispatches exactly one matching runner.
type Runners struct {
	Start              func(configPath string)
	Stop               func(configPath string)
	Status             func(configPath string)
	Validate           func(configPath string)
	Scan               func(opts cli.Options, configPath string)
	Config             func(opts cli.Options, configPath string)
	Peer               func(opts cli.Options, configPath string)
	Folder             func(opts cli.Options, configPath string)
	Stream             func(opts cli.Options, configPath string)
	Metadata           func(opts cli.Options, configPath string)
	Maintenance        func(opts cli.Options, configPath string)
	Snapshot           func(opts cli.Options, configPath string)
	Service            func(opts cli.Options, configPath string)
	WebGUI             func(opts cli.Options, configPath string)
	Identity           func(opts cli.Options, configPath string)
	ContainerBootstrap func(configPath string)
}

func Run(args []string, executablePath string, runners Runners) error {
	opts, err := cli.Parse(args)
	if err != nil {
		return err
	}
	configPath, err := configbootstrap.ResolveOrCreate(opts.ConfigPath, executablePath)
	if err != nil {
		return err
	}
	commanddispatch.Dispatch(opts, commanddispatch.Runners{
		Start:              func() { callPath(runners.Start, configPath) },
		Stop:               func() { callPath(runners.Stop, configPath) },
		Status:             func() { callPath(runners.Status, configPath) },
		Validate:           func() { callPath(runners.Validate, configPath) },
		Scan:               func() { callOptionsPath(runners.Scan, opts, configPath) },
		Config:             func() { callOptionsPath(runners.Config, opts, configPath) },
		Peer:               func() { callOptionsPath(runners.Peer, opts, configPath) },
		Folder:             func() { callOptionsPath(runners.Folder, opts, configPath) },
		Stream:             func() { callOptionsPath(runners.Stream, opts, configPath) },
		Metadata:           func() { callOptionsPath(runners.Metadata, opts, configPath) },
		Maintenance:        func() { callOptionsPath(runners.Maintenance, opts, configPath) },
		Snapshot:           func() { callOptionsPath(runners.Snapshot, opts, configPath) },
		Service:            func() { callOptionsPath(runners.Service, opts, configPath) },
		WebGUI:             func() { callOptionsPath(runners.WebGUI, opts, configPath) },
		Identity:           func() { callOptionsPath(runners.Identity, opts, configPath) },
		ContainerBootstrap: func() { callPath(runners.ContainerBootstrap, configPath) },
	})
	return nil
}

func callPath(fn func(string), configPath string) {
	if fn != nil {
		fn(configPath)
	}
}

func callOptionsPath(fn func(cli.Options, string), opts cli.Options, configPath string) {
	if fn != nil {
		fn(opts, configPath)
	}
}
