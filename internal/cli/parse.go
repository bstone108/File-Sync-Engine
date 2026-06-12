package cli

import (
	"fmt"
	"strconv"
)

type Command string

type Action string

const (
	CommandStart              Command = "start"
	CommandStop               Command = "stop"
	CommandStatus             Command = "status"
	CommandValidate           Command = "validate"
	CommandScan               Command = "scan"
	CommandConfig             Command = "config"
	CommandPeer               Command = "peer"
	CommandFolder             Command = "folder"
	CommandStream             Command = "stream"
	CommandMetadata           Command = "metadata"
	CommandMaintenance        Command = "maintenance"
	CommandService            Command = "service"
	CommandSnapshot           Command = "snapshot"
	CommandWebGUI             Command = "web-gui"
	CommandIdentity           Command = "identity"
	CommandContainerBootstrap Command = "container-bootstrap"
)

const (
	ActionAdd         Action = "add"
	ActionRemove      Action = "remove"
	ActionList        Action = "list"
	ActionUpdate      Action = "update"
	ActionInit        Action = "init"
	ActionShow        Action = "show"
	ActionCreate      Action = "create"
	ActionDelete      Action = "delete"
	ActionRestorePlan Action = "restore-plan"
	ActionRestore     Action = "restore"
	ActionRetention   Action = "retention"
	ActionPin         Action = "pin"
	ActionDeprecate   Action = "deprecate"
	ActionServe       Action = "serve"
	ActionPull        Action = "pull"
	ActionCompact     Action = "compact"
	ActionImportJSON  Action = "import-json"
	ActionSplitBadger Action = "split-badger"
	ActionScrub       Action = "scrub"
	ActionBackupScrub Action = "backup-scrub"
	ActionRender      Action = "render"
	ActionStatus      Action = "status"
	ActionStart       Action = "start"
	ActionStop        Action = "stop"
	ActionRestart     Action = "restart"
	ActionInstall     Action = "install"
	ActionExport      Action = "export"
	ActionImport      Action = "import"
)

type Options struct {
	Command     Command
	Action      Action
	ConfigPath  string
	ID          string
	Path        string
	Mode        string
	Endpoint    string
	Platform    string
	User        string
	Domain      string
	Destination string
	Paths       []string
	KeepLast    int
}

func Parse(args []string) (Options, error) {
	if len(args) < 1 {
		return Options{}, fmt.Errorf("command required")
	}
	switch Command(args[0]) {
	case CommandStart, CommandStop, CommandStatus, CommandValidate:
		return parseCore(Command(args[0]), args[1:])
	case CommandScan:
		return parseScan(args[1:])
	case CommandConfig:
		return parseConfig(args[1:])
	case CommandPeer:
		return parsePeer(args[1:])
	case CommandFolder:
		return parseFolder(args[1:])
	case CommandStream:
		return parseStream(args[1:])
	case CommandMetadata:
		return parseMetadata(args[1:])
	case CommandMaintenance:
		return parseMaintenance(args[1:])
	case CommandService:
		return parseService(args[1:])
	case CommandSnapshot:
		return parseSnapshot(args[1:])
	case CommandWebGUI:
		return parseWebGUI(args[1:])
	case CommandIdentity:
		return parseIdentity(args[1:])
	case CommandContainerBootstrap:
		return parseCore(CommandContainerBootstrap, args[1:])
	default:
		return Options{}, fmt.Errorf("unknown command %q", args[0])
	}
}

func parseWebGUI(args []string) (Options, error) {
	if len(args) < 1 {
		return Options{}, fmt.Errorf("web-gui action required")
	}
	action := Action(args[0])
	if action != ActionStatus && action != ActionInstall && action != ActionUpdate && action != ActionStart && action != ActionStop {
		return Options{}, fmt.Errorf("unsupported web-gui action %q", action)
	}
	if len(args) > 2 {
		return Options{}, fmt.Errorf("too many web-gui arguments")
	}
	opts := Options{Command: CommandWebGUI, Action: action}
	if len(args) == 2 {
		opts.ConfigPath = args[1]
	}
	return opts, nil
}

func parseIdentity(args []string) (Options, error) {
	if len(args) < 1 {
		return Options{}, fmt.Errorf("identity action required")
	}
	action := Action(args[0])
	if action != ActionExport && action != ActionImport {
		return Options{}, fmt.Errorf("unsupported identity action %q", action)
	}
	opts := Options{Command: CommandIdentity, Action: action}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--group":
			if action != ActionExport {
				return Options{}, fmt.Errorf("identity %s does not support --group", action)
			}
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("identity export --group requires value")
			}
			opts.ID = args[i+1]
			i++
		case "--package":
			if action != ActionImport {
				return Options{}, fmt.Errorf("identity %s does not support --package", action)
			}
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("identity import --package requires path")
			}
			opts.Path = args[i+1]
			i++
		default:
			if opts.ConfigPath == "" {
				opts.ConfigPath = args[i]
				continue
			}
			return Options{}, fmt.Errorf("unexpected identity argument %q", args[i])
		}
	}
	if action == ActionImport && opts.Path == "" {
		return Options{}, fmt.Errorf("identity import --package is required")
	}
	return opts, nil
}

func parseSnapshot(args []string) (Options, error) {
	if len(args) < 1 {
		return Options{}, fmt.Errorf("snapshot action required")
	}
	action := Action(args[0])
	if action != ActionCreate && action != ActionList && action != ActionShow && action != ActionPin && action != ActionDeprecate && action != ActionDelete && action != ActionRestorePlan && action != ActionRestore && action != ActionRetention {
		return Options{}, fmt.Errorf("unsupported snapshot action %q", action)
	}
	opts := Options{Command: CommandSnapshot, Action: action}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--folder":
			if action != ActionCreate && action != ActionList {
				return Options{}, fmt.Errorf("snapshot %s does not support --folder", action)
			}
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("snapshot %s --folder requires folder id", action)
			}
			opts.ID = args[i+1]
			i++
		case "--description":
			if action != ActionCreate {
				return Options{}, fmt.Errorf("snapshot %s does not support --description", action)
			}
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("snapshot create --description requires value")
			}
			opts.Mode = args[i+1]
			i++
		case "--snapshot":
			if action != ActionRestorePlan && action != ActionRestore {
				return Options{}, fmt.Errorf("snapshot %s does not support --snapshot", action)
			}
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("snapshot %s --snapshot requires id", action)
			}
			opts.ID = args[i+1]
			i++
		case "--path":
			if action != ActionRestorePlan && action != ActionRestore {
				return Options{}, fmt.Errorf("snapshot %s does not support --path", action)
			}
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("snapshot %s --path requires value", action)
			}
			opts.Paths = append(opts.Paths, args[i+1])
			i++
		case "--destination":
			if action != ActionRestorePlan && action != ActionRestore {
				return Options{}, fmt.Errorf("snapshot %s does not support --destination", action)
			}
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("snapshot %s --destination requires path", action)
			}
			opts.Destination = args[i+1]
			i++
		case "--alternate":
			if action != ActionRestorePlan && action != ActionRestore {
				return Options{}, fmt.Errorf("snapshot %s does not support --alternate", action)
			}
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("snapshot %s --alternate requires path", action)
			}
			opts.Path = args[i+1]
			i++
		case "--revert-database":
			return Options{}, fmt.Errorf("database reversion requires a dedicated rollback flow; snapshot %s only restores files", action)
		case "--keep-last":
			if action != ActionRetention {
				return Options{}, fmt.Errorf("snapshot %s does not support --keep-last", action)
			}
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("snapshot retention --keep-last requires value")
			}
			keepLast, err := strconv.Atoi(args[i+1])
			if err != nil || keepLast < 1 {
				return Options{}, fmt.Errorf("snapshot retention --keep-last must be at least 1")
			}
			opts.KeepLast = keepLast
			i++
		default:
			if action == ActionShow || action == ActionPin || action == ActionDeprecate || action == ActionDelete {
				if opts.ID == "" {
					opts.ID = args[i]
					continue
				}
			}
			if opts.ConfigPath == "" {
				opts.ConfigPath = args[i]
				continue
			}
			return Options{}, fmt.Errorf("unexpected snapshot argument %q", args[i])
		}
	}
	if action == ActionCreate && opts.ID == "" {
		return Options{}, fmt.Errorf("snapshot create --folder is required")
	}
	if (action == ActionShow || action == ActionPin || action == ActionDeprecate || action == ActionDelete) && opts.ID == "" {
		return Options{}, fmt.Errorf("snapshot %s id is required", action)
	}
	if (action == ActionRestorePlan || action == ActionRestore) && opts.ID == "" {
		return Options{}, fmt.Errorf("snapshot %s --snapshot is required", action)
	}
	if action == ActionRetention && opts.KeepLast < 1 {
		return Options{}, fmt.Errorf("snapshot retention --keep-last is required")
	}
	return opts, nil
}

func parseService(args []string) (Options, error) {
	if len(args) < 1 {
		return Options{}, fmt.Errorf("service action required")
	}
	action := Action(args[0])
	if action != ActionRender && action != ActionStatus && action != ActionStart && action != ActionStop && action != ActionRestart {
		return Options{}, fmt.Errorf("unsupported service action %q", action)
	}
	opts := Options{Command: CommandService, Action: action}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--platform":
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("service render --platform requires value")
			}
			opts.Platform = args[i+1]
			i++
		case "--binary":
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("service render --binary requires path")
			}
			opts.Path = args[i+1]
			i++
		case "--user":
			if action != ActionRender {
				return Options{}, fmt.Errorf("service %s does not support --user", action)
			}
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("service render --user requires value")
			}
			opts.User = args[i+1]
			i++
		case "--name":
			if action == ActionRender {
				return Options{}, fmt.Errorf("service render does not support --name")
			}
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("service %s --name requires value", action)
			}
			opts.ID = args[i+1]
			i++
		case "--domain":
			if action == ActionRender {
				return Options{}, fmt.Errorf("service render does not support --domain")
			}
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("service %s --domain requires value", action)
			}
			opts.Domain = args[i+1]
			i++
		default:
			if opts.ConfigPath == "" {
				opts.ConfigPath = args[i]
				continue
			}
			return Options{}, fmt.Errorf("unexpected service argument %q", args[i])
		}
	}
	if opts.Platform == "" {
		return Options{}, fmt.Errorf("service %s --platform is required", action)
	}
	if action == ActionRender && opts.Path == "" {
		return Options{}, fmt.Errorf("service render --binary is required")
	}
	if action != ActionRender && opts.ID == "" {
		return Options{}, fmt.Errorf("service %s --name is required", action)
	}
	return opts, nil
}

func parseMaintenance(args []string) (Options, error) {
	if len(args) < 1 {
		return Options{}, fmt.Errorf("maintenance action required")
	}
	action := Action(args[0])
	if action != ActionScrub && action != ActionBackupScrub {
		return Options{}, fmt.Errorf("unsupported maintenance action %q", action)
	}
	opts := Options{Command: CommandMaintenance, Action: action}
	for i := 1; i < len(args); i++ {
		if args[i] == "--folder" {
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("maintenance scrub --folder requires folder id")
			}
			opts.ID = args[i+1]
			i++
			continue
		}
		if opts.ConfigPath == "" {
			opts.ConfigPath = args[i]
			continue
		}
		return Options{}, fmt.Errorf("unexpected maintenance argument %q", args[i])
	}
	return opts, nil
}

func parseMetadata(args []string) (Options, error) {
	if len(args) < 1 {
		return Options{}, fmt.Errorf("metadata action required")
	}
	action := Action(args[0])
	if action != ActionCompact && action != ActionImportJSON && action != ActionSplitBadger {
		return Options{}, fmt.Errorf("unsupported metadata action %q", action)
	}
	opts := Options{Command: CommandMetadata, Action: action}
	for i := 1; i < len(args); i++ {
		if args[i] == "--source" {
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("metadata %s --source requires path", action)
			}
			opts.Path = args[i+1]
			i++
			continue
		}
		if args[i] == "--folder" {
			if action != ActionCompact {
				return Options{}, fmt.Errorf("metadata %s does not support --folder", action)
			}
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("metadata compact --folder requires folder id")
			}
			opts.ID = args[i+1]
			i++
			continue
		}
		if opts.ConfigPath == "" {
			opts.ConfigPath = args[i]
			continue
		}
		return Options{}, fmt.Errorf("unexpected metadata argument %q", args[i])
	}
	if (action == ActionImportJSON || action == ActionSplitBadger) && opts.Path == "" {
		return Options{}, fmt.Errorf("metadata %s --source is required", action)
	}
	return opts, nil
}

func parseStream(args []string) (Options, error) {
	if len(args) < 1 {
		return Options{}, fmt.Errorf("stream action required")
	}
	action := Action(args[0])
	opts := Options{Command: CommandStream, Action: action}
	switch action {
	case ActionServe:
		if len(args) < 2 {
			return Options{}, fmt.Errorf("stream serve folder id required")
		}
		if len(args) > 3 {
			return Options{}, fmt.Errorf("too many stream serve arguments")
		}
		opts.ID = args[1]
		if len(args) == 3 {
			opts.ConfigPath = args[2]
		}
		return opts, nil
	case ActionPull:
		if len(args) < 3 {
			return Options{}, fmt.Errorf("stream pull requires folder id and local path")
		}
		if len(args) > 4 {
			return Options{}, fmt.Errorf("too many stream pull arguments")
		}
		opts.ID = args[1]
		opts.Path = args[2]
		if len(args) == 4 {
			opts.ConfigPath = args[3]
		}
		return opts, nil
	default:
		return Options{}, fmt.Errorf("unsupported stream action %q", action)
	}
}

func parseCore(cmd Command, args []string) (Options, error) {
	if len(args) > 1 {
		return Options{}, fmt.Errorf("too many arguments")
	}
	opts := Options{Command: cmd}
	if len(args) == 1 {
		opts.ConfigPath = args[0]
	}
	return opts, nil
}

func parseScan(args []string) (Options, error) {
	opts := Options{Command: CommandScan}
	for i := 0; i < len(args); i++ {
		if args[i] == "--folder" {
			if i+1 >= len(args) {
				return Options{}, fmt.Errorf("scan --folder requires folder id")
			}
			opts.ID = args[i+1]
			i++
			continue
		}
		if opts.ConfigPath == "" {
			opts.ConfigPath = args[i]
			continue
		}
		return Options{}, fmt.Errorf("unexpected scan argument %q", args[i])
	}
	return opts, nil
}

func parseConfig(args []string) (Options, error) {
	if len(args) < 1 {
		return Options{}, fmt.Errorf("config action required")
	}
	action := Action(args[0])
	if action != ActionInit && action != ActionShow {
		return Options{}, fmt.Errorf("unsupported config action %q", args[0])
	}
	opts := Options{Command: CommandConfig, Action: action}
	if len(args) > 2 {
		return Options{}, fmt.Errorf("too many config arguments")
	}
	if len(args) == 2 {
		opts.ConfigPath = args[1]
	}
	return opts, nil
}

func parsePeer(args []string) (Options, error) {
	if len(args) < 1 {
		return Options{}, fmt.Errorf("peer action required")
	}
	action := Action(args[0])
	opts := Options{Command: CommandPeer, Action: action}
	args = args[1:]
	if action == ActionList {
		if len(args) == 1 {
			opts.ConfigPath = args[0]
		} else if len(args) > 1 {
			return Options{}, fmt.Errorf("too many peer list arguments")
		}
		return opts, nil
	}
	if action != ActionAdd && action != ActionRemove && action != ActionUpdate {
		return Options{}, fmt.Errorf("unsupported peer action %q", action)
	}
	if len(args) < 1 {
		return Options{}, fmt.Errorf("peer id required")
	}
	opts.ID = args[0]
	for i := 1; i < len(args); i++ {
		if args[i] == "--endpoint" && i+1 < len(args) {
			opts.Endpoint = args[i+1]
			i++
			continue
		}
		if opts.ConfigPath == "" {
			opts.ConfigPath = args[i]
		} else {
			return Options{}, fmt.Errorf("unexpected peer argument %q", args[i])
		}
	}
	return opts, nil
}

func parseFolder(args []string) (Options, error) {
	if len(args) < 1 {
		return Options{}, fmt.Errorf("folder action required")
	}
	action := Action(args[0])
	opts := Options{Command: CommandFolder, Action: action}
	args = args[1:]
	if action == ActionList {
		if len(args) == 1 {
			opts.ConfigPath = args[0]
		} else if len(args) > 1 {
			return Options{}, fmt.Errorf("too many folder list arguments")
		}
		return opts, nil
	}
	if action != ActionAdd && action != ActionRemove && action != ActionUpdate {
		return Options{}, fmt.Errorf("unsupported folder action %q", action)
	}
	if len(args) < 1 {
		return Options{}, fmt.Errorf("folder id required")
	}
	opts.ID = args[0]
	if action == ActionAdd || action == ActionUpdate {
		if len(args) < 2 {
			return Options{}, fmt.Errorf("folder path required")
		}
		opts.Path = args[1]
		args = args[2:]
	} else {
		args = args[1:]
	}
	for i := 0; i < len(args); i++ {
		if args[i] == "--mode" && i+1 < len(args) {
			opts.Mode = args[i+1]
			i++
			continue
		}
		if opts.ConfigPath == "" {
			opts.ConfigPath = args[i]
		} else {
			return Options{}, fmt.Errorf("unexpected folder argument %q", args[i])
		}
	}
	return opts, nil
}
