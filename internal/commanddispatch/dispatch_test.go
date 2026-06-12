package commanddispatch

import (
	"reflect"
	"strings"
	"testing"

	"filesyncengine/internal/cli"
)

func TestDispatchRoutesEveryTopLevelCommandToConfiguredRunner(t *testing.T) {
	var calls []string
	runners := Runners{
		Start:              func() { calls = append(calls, "start") },
		Stop:               func() { calls = append(calls, "stop") },
		Status:             func() { calls = append(calls, "status") },
		Validate:           func() { calls = append(calls, "validate") },
		Scan:               func() { calls = append(calls, "scan") },
		Config:             func() { calls = append(calls, "config") },
		Peer:               func() { calls = append(calls, "peer") },
		Folder:             func() { calls = append(calls, "folder") },
		Stream:             func() { calls = append(calls, "stream") },
		Metadata:           func() { calls = append(calls, "metadata") },
		Maintenance:        func() { calls = append(calls, "maintenance") },
		Snapshot:           func() { calls = append(calls, "snapshot") },
		Service:            func() { calls = append(calls, "service") },
		WebGUI:             func() { calls = append(calls, "web-gui") },
		Identity:           func() { calls = append(calls, "identity") },
		ContainerBootstrap: func() { calls = append(calls, "container-bootstrap") },
	}

	for _, command := range []cli.Command{
		cli.CommandStart,
		cli.CommandStop,
		cli.CommandStatus,
		cli.CommandValidate,
		cli.CommandScan,
		cli.CommandConfig,
		cli.CommandPeer,
		cli.CommandFolder,
		cli.CommandStream,
		cli.CommandMetadata,
		cli.CommandMaintenance,
		cli.CommandSnapshot,
		cli.CommandService,
		cli.CommandWebGUI,
		cli.CommandIdentity,
		cli.CommandContainerBootstrap,
	} {
		Dispatch(cli.Options{Command: command}, runners)
	}

	want := []string{"start", "stop", "status", "validate", "scan", "config", "peer", "folder", "stream", "metadata", "maintenance", "snapshot", "service", "web-gui", "identity", "container-bootstrap"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("dispatch calls = %#v, want %#v", calls, want)
	}
}

func TestUsageMentionsAllTopLevelCommands(t *testing.T) {
	for _, command := range []string{"start", "stop", "status", "validate", "scan", "maintenance", "metadata", "snapshot", "service", "web-gui", "identity", "container-bootstrap", "config", "peer", "folder", "stream"} {
		if !strings.Contains(Usage, command) {
			t.Fatalf("usage %q does not mention %q", Usage, command)
		}
	}
}
