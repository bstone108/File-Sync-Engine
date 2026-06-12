package service

import (
	"strings"
	"testing"
)

func TestRenderSystemdUnitUsesExplicitBinaryAndConfigPath(t *testing.T) {
	unit, err := Render(RenderOptions{Platform: PlatformSystemd, BinaryPath: "/usr/local/bin/fse", ConfigPath: "/etc/fse/config.json", User: "fse"})
	if err != nil {
		t.Fatalf("Render systemd: %v", err)
	}
	for _, want := range []string{
		"[Unit]",
		"Description=File Synchronization Engine daemon",
		"ExecStart=/usr/local/bin/fse start /etc/fse/config.json",
		"User=fse",
		"Restart=on-failure",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("systemd unit missing %q:\n%s", want, unit)
		}
	}
	if strings.Contains(unit, "apiKey") || strings.Contains(unit, "APIKey") {
		t.Fatalf("service helper should not render secrets: %s", unit)
	}
}

func TestRenderLaunchdPlistUsesProgramArguments(t *testing.T) {
	plist, err := Render(RenderOptions{Platform: PlatformLaunchd, BinaryPath: "/Applications/FSE/fse", ConfigPath: "/Users/me/Library/Application Support/fse/config.json", Label: "com.example.fse"})
	if err != nil {
		t.Fatalf("Render launchd: %v", err)
	}
	for _, want := range []string{
		"<key>Label</key>",
		"<string>com.example.fse</string>",
		"<string>/Applications/FSE/fse</string>",
		"<string>start</string>",
		"<string>/Users/me/Library/Application Support/fse/config.json</string>",
		"<key>KeepAlive</key>",
	} {
		if !strings.Contains(plist, want) {
			t.Fatalf("launchd plist missing %q:\n%s", want, plist)
		}
	}
}

func TestRenderWindowsPowerShellInstallScriptUsesServiceArguments(t *testing.T) {
	script, err := Render(RenderOptions{Platform: PlatformWindows, BinaryPath: `C:\Program Files\FSE\fse.exe`, ConfigPath: `C:\ProgramData\FSE\config.json`, ServiceName: "FSE"})
	if err != nil {
		t.Fatalf("Render windows: %v", err)
	}
	for _, want := range []string{
		"New-Service",
		"-Name 'FSE'",
		`\"C:\Program Files\FSE\fse.exe\" start \"C:\ProgramData\FSE\config.json\"`,
		"Set-Service -Name 'FSE' -StartupType Automatic",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("windows helper missing %q:\n%s", want, script)
		}
	}
}

func TestSystemdInstallHandoffCommandsAreReviewableAndExplicit(t *testing.T) {
	plan, err := SystemdInstallHandoff(SystemdInstallOptions{UnitPath: "/etc/systemd/system/fse.service", BinaryPath: "/usr/local/bin/fse", ConfigPath: "/etc/fse/config.json", User: "fse"})
	if err != nil {
		t.Fatalf("SystemdInstallHandoff: %v", err)
	}
	for _, want := range []string{
		"install -d -m 0755 /etc/systemd/system",
		"cat > /etc/systemd/system/fse.service <<'FSE_SYSTEMD_UNIT'",
		"ExecStart=/usr/local/bin/fse start /etc/fse/config.json",
		"User=fse",
		"systemctl daemon-reload",
		"systemctl enable --now fse.service",
		"systemctl disable --now fse.service",
	} {
		if !strings.Contains(plan, want) {
			t.Fatalf("systemd handoff missing %q:\n%s", want, plan)
		}
	}
	if strings.Contains(plan, "apiKey") || strings.Contains(plan, "APIKey") {
		t.Fatalf("systemd handoff should not render secrets: %s", plan)
	}
}

func TestLaunchdInstallHandoffCommandsAreReviewableAndExplicit(t *testing.T) {
	plan, err := LaunchdInstallHandoff(LaunchdInstallOptions{PlistPath: "/Library/LaunchDaemons/com.example.fse.plist", BinaryPath: "/Applications/FSE/fse", ConfigPath: "/Library/Application Support/FSE/config.json", Label: "com.example.fse", Domain: "system"})
	if err != nil {
		t.Fatalf("LaunchdInstallHandoff: %v", err)
	}
	for _, want := range []string{
		"install -d -m 0755 /Library/LaunchDaemons",
		"cat > /Library/LaunchDaemons/com.example.fse.plist <<'FSE_LAUNCHD_PLIST'",
		"<string>com.example.fse</string>",
		"<string>/Applications/FSE/fse</string>",
		"<string>/Library/Application Support/FSE/config.json</string>",
		"launchctl bootstrap system /Library/LaunchDaemons/com.example.fse.plist",
		"launchctl enable system/com.example.fse",
		"launchctl kickstart -k system/com.example.fse",
		"launchctl bootout system /Library/LaunchDaemons/com.example.fse.plist",
	} {
		if !strings.Contains(plan, want) {
			t.Fatalf("launchd handoff missing %q:\n%s", want, plan)
		}
	}
	if strings.Contains(plan, "apiKey") || strings.Contains(plan, "APIKey") {
		t.Fatalf("launchd handoff should not render secrets: %s", plan)
	}
}

func TestWindowsInstallHandoffCommandsAreReviewableExplicitAndSafelyQuoted(t *testing.T) {
	plan, err := WindowsInstallHandoff(WindowsInstallOptions{ServiceName: "FSE Agent", BinaryPath: `C:\Program Files\FSE\fse.exe`, ConfigPath: `C:\ProgramData\FSE\Bob's Config.json`})
	if err != nil {
		t.Fatalf("WindowsInstallHandoff: %v", err)
	}
	for _, want := range []string{
		"#requires -RunAsAdministrator",
		"Review before running in an elevated PowerShell session",
		"$serviceName = 'FSE Agent'",
		`$binaryPath = 'C:\Program Files\FSE\fse.exe'`,
		`$configPath = 'C:\ProgramData\FSE\Bob''s Config.json'`,
		`$binaryName = '"' + $binaryPath + '" start "' + $configPath + '"'`,
		"New-Service -Name $serviceName -DisplayName 'File Synchronization Engine' -BinaryPathName $binaryName -StartupType Automatic",
		"Start-Service -Name $serviceName",
		"Stop-Service -Name $serviceName",
		"sc.exe delete $serviceName",
	} {
		if !strings.Contains(plan, want) {
			t.Fatalf("windows handoff missing %q:\n%s", want, plan)
		}
	}
	if strings.Contains(plan, "apiKey") || strings.Contains(plan, "APIKey") {
		t.Fatalf("windows handoff should not render secrets: %s", plan)
	}
}

func TestControlHandoffRendersPlatformOwnedStatusAndControlCommands(t *testing.T) {
	systemd, err := ControlHandoff(ControlOptions{Platform: PlatformSystemd, ServiceName: "fse.service", Action: ControlStatus})
	if err != nil {
		t.Fatalf("ControlHandoff systemd: %v", err)
	}
	if !strings.Contains(systemd, "systemctl status fse.service") || !strings.Contains(systemd, "platform service manager") {
		t.Fatalf("systemd control handoff missing status command/boundary:\n%s", systemd)
	}

	launchd, err := ControlHandoff(ControlOptions{Platform: PlatformLaunchd, ServiceName: "com.example.fse", Domain: "system", Action: ControlRestart})
	if err != nil {
		t.Fatalf("ControlHandoff launchd: %v", err)
	}
	for _, want := range []string{"launchctl print system/com.example.fse", "launchctl kickstart -k system/com.example.fse"} {
		if !strings.Contains(launchd, want) {
			t.Fatalf("launchd control handoff missing %q:\n%s", want, launchd)
		}
	}

	windows, err := ControlHandoff(ControlOptions{Platform: PlatformWindows, ServiceName: "FSE Agent", Action: ControlStop})
	if err != nil {
		t.Fatalf("ControlHandoff windows: %v", err)
	}
	if !strings.Contains(windows, "Get-Service -Name 'FSE Agent'") || !strings.Contains(windows, "Stop-Service -Name 'FSE Agent'") {
		t.Fatalf("windows control handoff missing status/stop commands:\n%s", windows)
	}
}

func TestControlHandoffRejectsAmbiguousServicePolicy(t *testing.T) {
	if _, err := ControlHandoff(ControlOptions{Platform: PlatformSystemd, Action: ControlStatus}); err == nil {
		t.Fatalf("missing service name should fail")
	}
	if _, err := ControlHandoff(ControlOptions{Platform: PlatformLaunchd, ServiceName: "com.example.fse", Domain: "session", Action: ControlStatus}); err == nil {
		t.Fatalf("invalid launchd domain should fail")
	}
	if _, err := ControlHandoff(ControlOptions{Platform: PlatformWindows, ServiceName: "FSE", Action: ControlAction("reload")}); err == nil {
		t.Fatalf("unsupported control action should fail")
	}
}

func TestRenderRejectsMissingRequiredPaths(t *testing.T) {
	if _, err := Render(RenderOptions{Platform: PlatformSystemd, ConfigPath: "/etc/fse/config.json"}); err == nil {
		t.Fatalf("missing binary path should fail")
	}
	if _, err := Render(RenderOptions{Platform: PlatformSystemd, BinaryPath: "/usr/local/bin/fse"}); err == nil {
		t.Fatalf("missing config path should fail")
	}
	if _, err := Render(RenderOptions{Platform: Platform("plan9"), BinaryPath: "/bin/fse", ConfigPath: "/etc/fse/config.json"}); err == nil {
		t.Fatalf("unsupported platform should fail")
	}
	if _, err := SystemdInstallHandoff(SystemdInstallOptions{BinaryPath: "/usr/local/bin/fse", ConfigPath: "/etc/fse/config.json"}); err == nil {
		t.Fatalf("missing unit path should fail")
	}
}
