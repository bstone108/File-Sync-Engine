package commanddispatch

import "filesyncengine/internal/cli"

const Usage = "usage: fse start|stop|status|validate [config-path], fse scan [--folder id] [config-path], fse maintenance scrub [--folder id] [config-path], fse maintenance backup-scrub [config-path], fse metadata compact [--folder id] [config-path], fse metadata import-json --source <state.json> [config-path], fse metadata split-badger --source <metadata.badger> [config-path], fse snapshot create|list|show|pin|deprecate|delete ..., fse service render --platform systemd|launchd|windows --binary <path> [--user user] [config-path], fse service status|start|stop|restart --platform systemd|launchd|windows --name <service> [--domain system|gui/uid] [config-path], fse web-gui status|install|update|start|stop [config-path], fse identity export [--group id] [config-path], fse identity import --package <identity.json> [config-path], fse container-bootstrap [config-path], fse config init|show [path], fse peer ..., fse folder ..., fse stream serve|pull ..."

type Runners struct {
	Start              func()
	Stop               func()
	Status             func()
	Validate           func()
	Scan               func()
	Config             func()
	Peer               func()
	Folder             func()
	Stream             func()
	Metadata           func()
	Maintenance        func()
	Snapshot           func()
	Service            func()
	WebGUI             func()
	Identity           func()
	ContainerBootstrap func()
}

func Dispatch(opts cli.Options, runners Runners) {
	switch opts.Command {
	case cli.CommandStop:
		runners.Stop()
	case cli.CommandStatus:
		runners.Status()
	case cli.CommandValidate:
		runners.Validate()
	case cli.CommandScan:
		runners.Scan()
	case cli.CommandConfig:
		runners.Config()
	case cli.CommandPeer:
		runners.Peer()
	case cli.CommandFolder:
		runners.Folder()
	case cli.CommandStream:
		runners.Stream()
	case cli.CommandMetadata:
		runners.Metadata()
	case cli.CommandMaintenance:
		runners.Maintenance()
	case cli.CommandSnapshot:
		runners.Snapshot()
	case cli.CommandService:
		runners.Service()
	case cli.CommandWebGUI:
		runners.WebGUI()
	case cli.CommandIdentity:
		runners.Identity()
	case cli.CommandContainerBootstrap:
		runners.ContainerBootstrap()
	case cli.CommandStart:
		runners.Start()
	}
}
