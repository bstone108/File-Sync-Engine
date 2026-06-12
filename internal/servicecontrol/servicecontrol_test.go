package servicecontrol

import (
	"strings"
	"testing"

	"filesyncengine/internal/cli"
)

func TestHandleServiceRenderUsesCLIOptionsAndResolvedConfigPath(t *testing.T) {
	definition, err := Handle(cli.Options{Command: cli.CommandService, Action: cli.ActionRender, Platform: "systemd", Path: "/usr/local/bin/fse", User: "fse"}, "/etc/fse/config.json")
	if err != nil {
		t.Fatalf("Handle service render: %v", err)
	}
	for _, want := range []string{"ExecStart=/usr/local/bin/fse start /etc/fse/config.json", "User=fse", "Restart=on-failure"} {
		if !strings.Contains(definition, want) {
			t.Fatalf("service definition missing %q:\n%s", want, definition)
		}
	}
}

func TestHandleServiceControlUsesCLIOptions(t *testing.T) {
	plan, err := Handle(cli.Options{Command: cli.CommandService, Action: cli.ActionRestart, Platform: "launchd", ID: "com.example.fse", Domain: "system"}, "/etc/fse/config.json")
	if err != nil {
		t.Fatalf("Handle service control: %v", err)
	}
	for _, want := range []string{"launchctl print system/com.example.fse", "launchctl kickstart -k system/com.example.fse", "platform"} {
		if !strings.Contains(plan, want) {
			t.Fatalf("service control plan missing %q:\n%s", want, plan)
		}
	}
}
