package service

import (
	"fmt"
	"html"
	"path/filepath"
	"strings"
)

// Platform identifies the operating-system service manager helper to render.
type Platform string

const (
	PlatformSystemd Platform = "systemd"
	PlatformLaunchd Platform = "launchd"
	PlatformWindows Platform = "windows"
)

// RenderOptions describes a service helper template. BinaryPath and ConfigPath
// are required so generated helpers never guess local install locations.
type RenderOptions struct {
	Platform    Platform
	BinaryPath  string
	ConfigPath  string
	User        string
	Label       string
	ServiceName string
}

// SystemdInstallOptions describes the documented package-manager handoff for a
// systemd unit. The returned shell snippet is reviewable text; callers decide
// whether and how to run the privileged commands.
type SystemdInstallOptions struct {
	UnitPath   string
	BinaryPath string
	ConfigPath string
	User       string
}

// LaunchdInstallOptions describes the documented handoff for macOS launchd
// agents/daemons. The returned shell snippet is reviewable text; callers decide
// whether and how to run the privileged commands.
type LaunchdInstallOptions struct {
	PlistPath  string
	BinaryPath string
	ConfigPath string
	Label      string
	Domain     string
}

// WindowsInstallOptions describes the documented handoff for Windows Service
// Control Manager operations. The returned PowerShell snippet is reviewable text;
// callers decide whether and how to run the privileged commands.
type WindowsInstallOptions struct {
	ServiceName string
	BinaryPath  string
	ConfigPath  string
}

// ControlAction identifies a platform service-manager lifecycle operation.
type ControlAction string

const (
	ControlStatus  ControlAction = "status"
	ControlStart   ControlAction = "start"
	ControlStop    ControlAction = "stop"
	ControlRestart ControlAction = "restart"
)

// ControlOptions describes a reviewable platform service-manager status/control
// handoff. The helper renders commands only; it does not execute privileged
// service-manager operations or hide platform-owned policy.
type ControlOptions struct {
	Platform    Platform
	ServiceName string
	Domain      string
	Action      ControlAction
}

// Render returns a script or service definition that installs/runs fse with an
// explicit config path. It does not include secrets and does not perform any OS
// mutation by itself.
func Render(opts RenderOptions) (string, error) {
	if opts.BinaryPath == "" {
		return "", fmt.Errorf("binary path required")
	}
	if opts.ConfigPath == "" {
		return "", fmt.Errorf("config path required")
	}
	switch opts.Platform {
	case PlatformSystemd:
		return renderSystemd(opts), nil
	case PlatformLaunchd:
		return renderLaunchd(opts), nil
	case PlatformWindows:
		return renderWindows(opts), nil
	default:
		return "", fmt.Errorf("unsupported service platform %q", opts.Platform)
	}
}

func renderSystemd(opts RenderOptions) string {
	userLine := ""
	if opts.User != "" {
		userLine = fmt.Sprintf("User=%s\n", systemdEscape(opts.User))
	}
	return fmt.Sprintf(`[Unit]
Description=File Synchronization Engine daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
%sExecStart=%s start %s
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
`, userLine, systemdEscape(opts.BinaryPath), systemdEscape(opts.ConfigPath))
}

// SystemdInstallHandoff renders a reviewable shell handoff for packagers or
// administrators who want to install the rendered unit with systemctl. The
// helper does not execute anything and does not embed API keys or other secrets.
func SystemdInstallHandoff(opts SystemdInstallOptions) (string, error) {
	if opts.UnitPath == "" {
		return "", fmt.Errorf("unit path required")
	}
	unit, err := Render(RenderOptions{Platform: PlatformSystemd, BinaryPath: opts.BinaryPath, ConfigPath: opts.ConfigPath, User: opts.User})
	if err != nil {
		return "", err
	}
	unitDir := filepath.Dir(opts.UnitPath)
	unitName := filepath.Base(opts.UnitPath)
	return fmt.Sprintf(`# Review before running as root. This snippet installs a systemd unit for fse.
install -d -m 0755 %s
cat > %s <<'FSE_SYSTEMD_UNIT'
%sFSE_SYSTEMD_UNIT
systemctl daemon-reload
systemctl enable --now %s

# To uninstall later:
# systemctl disable --now %s
# rm -f %s
# systemctl daemon-reload
`, systemdEscape(unitDir), systemdEscape(opts.UnitPath), unit, systemdEscape(unitName), systemdEscape(unitName), systemdEscape(opts.UnitPath)), nil
}

func renderLaunchd(opts RenderOptions) string {
	label := launchdLabel(opts.Label)
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>start</string>
		<string>%s</string>
	</array>
	<key>KeepAlive</key>
	<true/>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`, xmlEscape(label), xmlEscape(opts.BinaryPath), xmlEscape(opts.ConfigPath))
}

// LaunchdInstallHandoff renders a reviewable macOS launchd install/load and
// uninstall/unload handoff. It writes no files and runs no commands itself.
func LaunchdInstallHandoff(opts LaunchdInstallOptions) (string, error) {
	if opts.PlistPath == "" {
		return "", fmt.Errorf("plist path required")
	}
	plist, err := Render(RenderOptions{Platform: PlatformLaunchd, BinaryPath: opts.BinaryPath, ConfigPath: opts.ConfigPath, Label: opts.Label})
	if err != nil {
		return "", err
	}
	domain := opts.Domain
	if domain == "" {
		domain = "gui/$(id -u)"
	}
	if domain != "system" && !strings.HasPrefix(domain, "gui/") {
		return "", fmt.Errorf("launchd domain must be system or gui/<uid>")
	}
	label := launchdLabel(opts.Label)
	plistDir := filepath.Dir(opts.PlistPath)
	return fmt.Sprintf(`# Review before running on macOS. This snippet installs and loads a launchd definition for fse.
install -d -m 0755 %s
cat > %s <<'FSE_LAUNCHD_PLIST'
%sFSE_LAUNCHD_PLIST
launchctl bootstrap %s %s
launchctl enable %s/%s
launchctl kickstart -k %s/%s

# To unload and uninstall later:
# launchctl bootout %s %s
# rm -f %s
`, systemdEscape(plistDir), systemdEscape(opts.PlistPath), plist, systemdEscape(domain), systemdEscape(opts.PlistPath), systemdEscape(domain), systemdEscape(label), systemdEscape(domain), systemdEscape(label), systemdEscape(domain), systemdEscape(opts.PlistPath), systemdEscape(opts.PlistPath)), nil
}

func launchdLabel(label string) string {
	if label == "" {
		return "com.filesyncengine.fse"
	}
	return label
}

func renderWindows(opts RenderOptions) string {
	serviceName := opts.ServiceName
	if serviceName == "" {
		serviceName = "FSE"
	}
	binaryName := fmt.Sprintf(`\"%s\" start \"%s\"`, psSingleQuotedContent(opts.BinaryPath), psSingleQuotedContent(opts.ConfigPath))
	quotedName := psQuote(serviceName)
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$binaryName = '%s'
if (Get-Service -Name %s -ErrorAction SilentlyContinue) {
    Set-Service -Name %s -StartupType Automatic
} else {
    New-Service -Name %s -DisplayName 'File Synchronization Engine' -BinaryPathName $binaryName -StartupType Automatic
}
Set-Service -Name %s -StartupType Automatic
Start-Service -Name %s
`, binaryName, quotedName, quotedName, quotedName, quotedName, quotedName)
}

// WindowsInstallHandoff renders a reviewable elevated PowerShell handoff for
// installing, starting, stopping, and uninstalling the Windows service. It writes
// no files, runs no commands, and embeds no API keys or peer credentials.
func WindowsInstallHandoff(opts WindowsInstallOptions) (string, error) {
	if opts.BinaryPath == "" {
		return "", fmt.Errorf("binary path required")
	}
	if opts.ConfigPath == "" {
		return "", fmt.Errorf("config path required")
	}
	serviceName := opts.ServiceName
	if serviceName == "" {
		serviceName = "FSE"
	}
	return fmt.Sprintf(`#requires -RunAsAdministrator
# Review before running in an elevated PowerShell session. This snippet installs and starts the fse Windows service.
$ErrorActionPreference = 'Stop'
$serviceName = %s
$binaryPath = %s
$configPath = %s
$binaryName = '"' + $binaryPath + '" start "' + $configPath + '"'

if (Get-Service -Name $serviceName -ErrorAction SilentlyContinue) {
    Set-Service -Name $serviceName -StartupType Automatic
} else {
    New-Service -Name $serviceName -DisplayName 'File Synchronization Engine' -BinaryPathName $binaryName -StartupType Automatic
}
Set-Service -Name $serviceName -StartupType Automatic
Start-Service -Name $serviceName

# To stop, uninstall, and remove the service later from an elevated PowerShell session:
# Stop-Service -Name $serviceName
# sc.exe delete $serviceName
`, psQuote(serviceName), psQuote(opts.BinaryPath), psQuote(opts.ConfigPath)), nil
}

// ControlHandoff renders reviewable platform-owned lifecycle commands for an
// already installed service. It intentionally does not execute systemctl,
// launchctl, or Service Control Manager commands.
func ControlHandoff(opts ControlOptions) (string, error) {
	if opts.ServiceName == "" {
		return "", fmt.Errorf("service name required")
	}
	if opts.Action == "" {
		opts.Action = ControlStatus
	}
	if !validControlAction(opts.Action) {
		return "", fmt.Errorf("unsupported control action %q", opts.Action)
	}
	switch opts.Platform {
	case PlatformSystemd:
		return renderSystemdControl(opts), nil
	case PlatformLaunchd:
		return renderLaunchdControl(opts)
	case PlatformWindows:
		return renderWindowsControl(opts), nil
	default:
		return "", fmt.Errorf("unsupported service platform %q", opts.Platform)
	}
}

func validControlAction(action ControlAction) bool {
	switch action {
	case ControlStatus, ControlStart, ControlStop, ControlRestart:
		return true
	default:
		return false
	}
}

func renderSystemdControl(opts ControlOptions) string {
	name := systemdEscape(opts.ServiceName)
	command := "status"
	switch opts.Action {
	case ControlStart:
		command = "start"
	case ControlStop:
		command = "stop"
	case ControlRestart:
		command = "restart"
	}
	return fmt.Sprintf(`# Review before running. This uses the platform service manager and does not replace distro/package policy.
systemctl status %s
systemctl %s %s
`, name, command, name)
}

func renderLaunchdControl(opts ControlOptions) (string, error) {
	domain := opts.Domain
	if domain == "" {
		domain = "gui/$(id -u)"
	}
	if domain != "system" && !strings.HasPrefix(domain, "gui/") {
		return "", fmt.Errorf("launchd domain must be system or gui/<uid>")
	}
	service := systemdEscape(domain + "/" + opts.ServiceName)
	commands := []string{fmt.Sprintf("launchctl print %s", service)}
	switch opts.Action {
	case ControlStart:
		commands = append(commands, fmt.Sprintf("launchctl kickstart %s", service))
	case ControlStop:
		commands = append(commands, fmt.Sprintf("launchctl kill TERM %s", service))
	case ControlRestart:
		commands = append(commands, fmt.Sprintf("launchctl kickstart -k %s", service))
	}
	return fmt.Sprintf(`# Review before running. This uses the platform launchd service manager domains/labels directly and does not install or unload definitions.
%s
`, strings.Join(commands, "\n")), nil
}

func renderWindowsControl(opts ControlOptions) string {
	name := psQuote(opts.ServiceName)
	command := fmt.Sprintf("Get-Service -Name %s", name)
	switch opts.Action {
	case ControlStart:
		command = fmt.Sprintf("Start-Service -Name %s", name)
	case ControlStop:
		command = fmt.Sprintf("Stop-Service -Name %s", name)
	case ControlRestart:
		command = fmt.Sprintf("Restart-Service -Name %s", name)
	}
	return fmt.Sprintf(`# Review before running in an elevated PowerShell session when required. This uses Windows Service Control Manager policy.
Get-Service -Name %s
%s
`, name, command)
}

func systemdEscape(value string) string {
	if !strings.ContainsAny(value, " \t\n\"'\\") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func xmlEscape(value string) string {
	return html.EscapeString(value)
}

func psQuote(value string) string {
	return "'" + psSingleQuotedContent(value) + "'"
}

func psSingleQuotedContent(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}
