import type { NativeDesktopShell } from './nativeShell';

export type DaemonTrayOpenRequest = {
  source: 'daemon-tray-double-click' | 'daemon-tray-menu' | 'uri-handler' | 'launch-argument';
  uri?: 'fse-desktop://open';
  argv?: string[];
  boundary: 'separate GUI process';
  daemonBoundary: 'no direct daemon process launch';
};

export function isDaemonTrayOpenRequest(request: DaemonTrayOpenRequest): boolean {
  if (request.uri === 'fse-desktop://open') {
    return true;
  }
  return (request.argv ?? []).some((arg) => arg === '--open-from-tray' || arg === 'fse-desktop://open');
}

export async function handleDaemonTrayOpenRequest(
  request: DaemonTrayOpenRequest,
  shell: Pick<NativeDesktopShell, 'showMainWindowFromDaemonTray'>
): Promise<boolean> {
  if (!isDaemonTrayOpenRequest(request)) {
    return false;
  }
  await shell.showMainWindowFromDaemonTray();
  return true;
}
