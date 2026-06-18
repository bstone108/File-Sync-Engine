import {
  controlVerifiedBundledDaemonLifecycle,
  type BundledDaemonLifecycleSettings,
  type BundledEngineRuntimeGate
} from './bundledEngine';
import type { DaemonStartupIntegrationStatus, DaemonTrayStatus, NativeDesktopShell } from './nativeShell';

export type LinuxStartupTrayState = {
  platform: 'systemd';
  serviceName: string;
  appName: string;
  guiOpenURI: 'fse-desktop://open';
  startup: DaemonStartupIntegrationStatus;
  tray: DaemonTrayStatus;
  registerDesktopEntryCommand: string;
  openGuiCommand: string;
  serviceManager: 'systemd user/system service';
  trayOwner: 'StatusNotifier/AppIndicator';
  boundary: 'no direct daemon process launch';
};

export type LinuxStartupTrayBridgeOptions = {
  shell: NativeDesktopShell;
  gate: BundledEngineRuntimeGate;
  lifecycleSettings: BundledDaemonLifecycleSettings;
  appName?: string;
  guiExecutablePath?: string;
  desktopFileName?: string;
};

export function renderLinuxDesktopEntryCommand(
  appName: string,
  executablePath: string,
  desktopFileName = 'fse-desktop.desktop'
): string {
  const safeAppName = appName.replace(/'/g, "'\\''");
  const safeExecutablePath = executablePath.replace(/'/g, "'\\''");
  const safeDesktopFileName = desktopFileName.replace(/'/g, "'\\''");
  return [
    '# Review before running on Linux. Register the separate GUI app as the fse-desktop URI handler.',
    `APP_NAME='${safeAppName}'`,
    `GUI_EXE='${safeExecutablePath}'`,
    `DESKTOP_FILE='${safeDesktopFileName}'`,
    "INSTALL_DIR=\"${XDG_DATA_HOME:-$HOME/.local/share}/applications\"",
    'mkdir -p "$INSTALL_DIR"',
    'cat > "$INSTALL_DIR/$DESKTOP_FILE" <<EOF',
    '[Desktop Entry]',
    'Type=Application',
    'Name=$APP_NAME',
    'Exec=$GUI_EXE --open-from-tray %u',
    'Terminal=false',
    'MimeType=x-scheme-handler/fse-desktop;',
    'NoDisplay=true',
    'EOF',
    'xdg-mime default "$DESKTOP_FILE" x-scheme-handler/fse-desktop',
    'update-desktop-database "$INSTALL_DIR" >/dev/null 2>&1 || true'
  ].join('\n');
}

export function renderLinuxOpenGuiCommand(): string {
  return "xdg-open 'fse-desktop://open'";
}

export async function buildLinuxStartupTrayBridge(
  options: LinuxStartupTrayBridgeOptions
): Promise<LinuxStartupTrayState> {
  const { shell, gate, lifecycleSettings } = options;
  const settings: BundledDaemonLifecycleSettings = {
    ...lifecycleSettings,
    platform: 'systemd'
  };
  const [startup, tray] = await Promise.all([
    shell.getDaemonStartupIntegrationStatus(),
    shell.getDaemonTrayStatus()
  ]);

  return {
    platform: 'systemd',
    serviceName: settings.serviceName,
    appName: options.appName ?? 'File Synchronization Engine',
    guiOpenURI: 'fse-desktop://open',
    startup,
    tray,
    registerDesktopEntryCommand: renderLinuxDesktopEntryCommand(
      options.appName ?? 'File Synchronization Engine',
      options.guiExecutablePath ?? 'fse-desktop',
      options.desktopFileName
    ),
    openGuiCommand: renderLinuxOpenGuiCommand(),
    serviceManager: 'systemd user/system service',
    trayOwner: 'StatusNotifier/AppIndicator',
    boundary: 'no direct daemon process launch'
  };
}

export async function openGuiFromDaemonTray(shell: NativeDesktopShell): Promise<void> {
  await shell.openGuiFromDaemonTray();
}

export async function requestLinuxDaemonServiceStatus(
  gate: BundledEngineRuntimeGate,
  lifecycleSettings: BundledDaemonLifecycleSettings
): Promise<Response> {
  return await controlVerifiedBundledDaemonLifecycle(
    gate,
    { ...lifecycleSettings, platform: 'systemd' },
    'status'
  );
}
