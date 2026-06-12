import {
  controlVerifiedBundledDaemonLifecycle,
  type BundledDaemonLifecycleSettings,
  type BundledEngineRuntimeGate
} from './bundledEngine';
import type { DaemonStartupIntegrationStatus, DaemonTrayStatus, NativeDesktopShell } from './nativeShell';

export type MacOSStartupTrayState = {
  platform: 'launchd';
  launchdLabel: string;
  appName: string;
  guiOpenURI: 'fse-desktop://open';
  startup: DaemonStartupIntegrationStatus;
  tray: DaemonTrayStatus;
  registerURLSchemeCommand: string;
  openGuiCommand: string;
  serviceManager: 'LaunchAgent/LaunchDaemon';
  trayOwner: 'menu bar status item';
  boundary: 'no direct daemon process launch';
};

export type MacOSStartupTrayBridgeOptions = {
  shell: NativeDesktopShell;
  gate: BundledEngineRuntimeGate;
  lifecycleSettings: BundledDaemonLifecycleSettings;
  appName?: string;
  bundleIdentifier?: string;
};

export function renderMacOSURLSchemeRegistrationCommand(
  appName: string,
  bundleIdentifier = 'com.filesyncengine.desktop'
): string {
  const safeAppName = appName.replace(/'/g, "'\\''");
  const safeBundleIdentifier = bundleIdentifier.replace(/'/g, "'\\''");
  return [
    "# Review before running on macOS. Register the separate GUI app as the fse-desktop URL handler.",
    `APP_NAME='${safeAppName}'`,
    `BUNDLE_ID='${safeBundleIdentifier}'`,
    "URI_SCHEME='fse-desktop'",
    "echo \"Ensure $APP_NAME Info.plist declares CFBundleURLSchemes=$URI_SCHEME for $BUNDLE_ID\"",
    "echo \"Run: /System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f /Applications/$APP_NAME.app\""
  ].join('\n');
}

export function renderMacOSOpenGuiCommand(): string {
  return "open 'fse-desktop://open'";
}

export async function buildMacOSStartupTrayBridge(
  options: MacOSStartupTrayBridgeOptions
): Promise<MacOSStartupTrayState> {
  const { shell, lifecycleSettings } = options;
  const settings: BundledDaemonLifecycleSettings = {
    ...lifecycleSettings,
    platform: 'launchd'
  };
  const [startup, tray] = await Promise.all([
    shell.getDaemonStartupIntegrationStatus(),
    shell.getDaemonTrayStatus()
  ]);

  return {
    platform: 'launchd',
    launchdLabel: settings.serviceName,
    appName: options.appName ?? 'File Synchronization Engine',
    guiOpenURI: 'fse-desktop://open',
    startup,
    tray,
    registerURLSchemeCommand: renderMacOSURLSchemeRegistrationCommand(
      options.appName ?? 'File Synchronization Engine',
      options.bundleIdentifier
    ),
    openGuiCommand: renderMacOSOpenGuiCommand(),
    serviceManager: 'LaunchAgent/LaunchDaemon',
    trayOwner: 'menu bar status item',
    boundary: 'no direct daemon process launch'
  };
}

export async function openGuiFromDaemonTray(shell: NativeDesktopShell): Promise<void> {
  await shell.openGuiFromDaemonTray();
}

export async function requestMacOSDaemonServiceStatus(
  gate: BundledEngineRuntimeGate,
  lifecycleSettings: BundledDaemonLifecycleSettings
): Promise<Response> {
  return await controlVerifiedBundledDaemonLifecycle(
    gate,
    { ...lifecycleSettings, platform: 'launchd' },
    'status'
  );
}
