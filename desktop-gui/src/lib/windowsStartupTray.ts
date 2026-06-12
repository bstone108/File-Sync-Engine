import {
  controlVerifiedBundledDaemonLifecycle,
  type BundledDaemonLifecycleSettings,
  type BundledEngineRuntimeGate
} from './bundledEngine';
import type { DaemonStartupIntegrationStatus, DaemonTrayStatus, NativeDesktopShell } from './nativeShell';

export type WindowsStartupTrayState = {
  platform: 'windows';
  serviceName: string;
  appName: string;
  guiOpenURI: 'fse-desktop://open';
  startup: DaemonStartupIntegrationStatus;
  tray: DaemonTrayStatus;
  registerProtocolHandlerCommand: string;
  openGuiCommand: string;
  serviceManager: 'Service Control Manager';
  boundary: 'no direct daemon process launch';
};

export type WindowsStartupTrayBridgeOptions = {
  shell: NativeDesktopShell;
  gate: BundledEngineRuntimeGate;
  lifecycleSettings: BundledDaemonLifecycleSettings;
  appName?: string;
};

export function renderWindowsProtocolHandlerCommand(appName: string, exePath: string): string {
  const safeAppName = appName.replace(/'/g, "''");
  const safeExePath = exePath.replace(/'/g, "''");
  return [
    "$ErrorActionPreference = 'Stop'",
    `$appName = '${safeAppName}'`,
    "$protocol = 'fse-desktop'",
    `$guiExe = '${safeExePath}'`,
    "$base = \"HKCU:\\Software\\Classes\\$protocol\"",
    "New-Item -Path $base -Force | Out-Null",
    "New-ItemProperty -Path $base -Name '(default)' -Value ('URL:' + $appName) -Force | Out-Null",
    "New-ItemProperty -Path $base -Name 'URL Protocol' -Value '' -Force | Out-Null",
    "New-Item -Path \"$base\\shell\\open\\command\" -Force | Out-Null",
    "New-ItemProperty -Path \"$base\\shell\\open\\command\" -Name '(default)' -Value ('\"' + $guiExe + '\" --open-from-tray') -Force | Out-Null"
  ].join('\n');
}

export function renderWindowsOpenGuiCommand(): string {
  return "Start-Process 'fse-desktop://open'";
}

export async function buildWindowsStartupTrayBridge(
  options: WindowsStartupTrayBridgeOptions
): Promise<WindowsStartupTrayState> {
  const { shell, gate, lifecycleSettings } = options;
  const settings: BundledDaemonLifecycleSettings = {
    ...lifecycleSettings,
    platform: 'windows'
  };
  const [startup, tray] = await Promise.all([
    shell.getDaemonStartupIntegrationStatus(),
    shell.getDaemonTrayStatus()
  ]);

  return {
    platform: 'windows',
    serviceName: settings.serviceName,
    appName: options.appName ?? 'File Synchronization Engine',
    guiOpenURI: 'fse-desktop://open',
    startup,
    tray,
    registerProtocolHandlerCommand: renderWindowsProtocolHandlerCommand(
      options.appName ?? 'File Synchronization Engine',
      gate.expected.executable
    ),
    openGuiCommand: renderWindowsOpenGuiCommand(),
    serviceManager: 'Service Control Manager',
    boundary: 'no direct daemon process launch'
  };
}

export async function openGuiFromDaemonTray(shell: NativeDesktopShell): Promise<void> {
  await shell.openGuiFromDaemonTray();
}

export async function requestWindowsDaemonServiceStatus(
  gate: BundledEngineRuntimeGate,
  lifecycleSettings: BundledDaemonLifecycleSettings
): Promise<Response> {
  return await controlVerifiedBundledDaemonLifecycle(
    gate,
    { ...lifecycleSettings, platform: 'windows' },
    'status'
  );
}
