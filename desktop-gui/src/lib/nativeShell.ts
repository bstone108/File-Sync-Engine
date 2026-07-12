import {
  controlVerifiedBundledDaemonLifecycle,
  verifyBundledEngineResourceManifest,
  type BundledDaemonLifecycleAction,
  type BundledDaemonLifecycleSettings,
  type BundledEngineResourceManifest,
  type BundledEngineResourceObservation,
  type BundledEngineRuntimeGate
} from './bundledEngine';
import {
  installBundledDaemonForCurrentOS as requestBundledDaemonInstall,
  adoptGUIOwnedNonServiceDaemon as adoptGUIOwnedNonServiceDaemonThroughBridge,
  requestGUIOwnedNonServiceDaemonLaunch as requestGUIOwnedNonServiceDaemonLaunchThroughBridge,
  stopGUIOwnedNonServiceDaemonThroughAPI as stopGUIOwnedNonServiceDaemonThroughAPIBridge,
  promptForStartupAtLogin,
  type FirstLaunchDaemonRegistrationChoice,
  type FirstLaunchDaemonRegistrationResult,
  type FirstLaunchDaemonRegistrationStatus,
  type GUIManagedNonServiceDaemonSession,
  type DaemonRuntimeState,
  type GUIOwnedNonServiceDaemonBridge,
  type GUIOwnedNonServiceDaemonLaunchRequest
} from './firstLaunch';
import {
  deleteRemoteInstanceCredential as deleteCredentialThroughBridge,
  resolveRemoteInstanceCredential as resolveCredentialThroughBridge,
  storeRemoteInstanceCredential as storeCredentialThroughBridge,
  type RemoteInstanceCredentialRecord,
  type RemoteInstanceCredentialSecret
} from './credentialVault';

export type DaemonTrayStatus = {
  daemonOwnedTray: boolean;
  visible: boolean;
  state: 'unknown' | 'running' | 'stopped' | 'degraded';
  message: string;
};

export type DaemonStartupIntegrationStatus = {
  startupEnabled: boolean;
  platform: BundledDaemonLifecycleSettings['platform'];
  serviceName: string;
  message: string;
};

export type NativeDesktopShell = {
  readBundledEngineResourceManifest(): Promise<BundledEngineResourceManifest>;
  observeBundledEngineResources(): Promise<BundledEngineResourceObservation[]>;
  getLocalLifecycleSettings(): Promise<BundledDaemonLifecycleSettings>;
  getFirstLaunchDaemonRegistrationStatus(): Promise<FirstLaunchDaemonRegistrationStatus>;
  getDaemonTrayStatus(): Promise<DaemonTrayStatus>;
  getDaemonStartupIntegrationStatus(): Promise<DaemonStartupIntegrationStatus>;
  requestGUIOwnedNonServiceDaemonLaunch(
    request: GUIOwnedNonServiceDaemonLaunchRequest
  ): Promise<GUIManagedNonServiceDaemonSession>;
  adoptGUIOwnedNonServiceDaemon(sessionID: string): Promise<GUIManagedNonServiceDaemonSession>;
  getGUIOwnedNonServiceDaemonSession(): Promise<GUIManagedNonServiceDaemonSession | null>;
  getGUIOwnedNonServiceDaemonState(): Promise<DaemonRuntimeState>;
  stopGUIOwnedNonServiceDaemonThroughAPI(sessionID: string): Promise<GUIManagedNonServiceDaemonSession>;
  openGuiFromDaemonTray(): Promise<void>;
  showMainWindowFromDaemonTray(): Promise<void>;
  storeRemoteInstanceCredential(record: RemoteInstanceCredentialRecord, secret: RemoteInstanceCredentialSecret): Promise<RemoteInstanceCredentialRecord>;
  resolveRemoteInstanceCredential(credentialRef: string): Promise<RemoteInstanceCredentialSecret>;
  deleteRemoteInstanceCredential(credentialRef: string): Promise<void>;
};

declare global {
  interface Window {
    fseDesktopShell?: NativeDesktopShell;
  }
}

export function getNativeDesktopShell(): NativeDesktopShell {
  if (!window.fseDesktopShell) {
    throw new Error('native desktop shell is not available');
  }
  return window.fseDesktopShell;
}

export async function loadBundledDaemonGate(shell = getNativeDesktopShell()): Promise<BundledEngineRuntimeGate> {
  const [manifest, observations] = await Promise.all([
    shell.readBundledEngineResourceManifest(),
    shell.observeBundledEngineResources()
  ]);
  return verifyBundledEngineResourceManifest(manifest, observations);
}

export async function runBundledDaemonLifecycle(
  action: BundledDaemonLifecycleAction,
  shell = getNativeDesktopShell()
): Promise<Response> {
  const [gate, settings] = await Promise.all([
    loadBundledDaemonGate(shell),
    shell.getLocalLifecycleSettings()
  ]);
  return await controlVerifiedBundledDaemonLifecycle(gate, settings, action);
}

export async function getFirstLaunchDaemonRegistrationStatus(
  shell = getNativeDesktopShell()
): Promise<FirstLaunchDaemonRegistrationStatus> {
  return await shell.getFirstLaunchDaemonRegistrationStatus();
}

export async function installBundledDaemonForCurrentOS(
  choice: FirstLaunchDaemonRegistrationChoice,
  shell = getNativeDesktopShell()
): Promise<FirstLaunchDaemonRegistrationResult> {
  const [gate, settings] = await Promise.all([
    loadBundledDaemonGate(shell),
    shell.getLocalLifecycleSettings()
  ]);
  return await requestBundledDaemonInstall(gate, settings, choice);
}

export async function requestGUIOwnedNonServiceDaemonLaunch(
  request: GUIOwnedNonServiceDaemonLaunchRequest,
  shell = getNativeDesktopShell()
): Promise<GUIManagedNonServiceDaemonSession> {
  return await shell.requestGUIOwnedNonServiceDaemonLaunch(request);
}

export async function adoptGUIOwnedNonServiceDaemon(
  sessionID: string,
  shell = getNativeDesktopShell()
): Promise<GUIManagedNonServiceDaemonSession> {
  return await adoptGUIOwnedNonServiceDaemonThroughBridge(shell as GUIOwnedNonServiceDaemonBridge, sessionID);
}

export async function getGUIOwnedNonServiceDaemonSession(
  shell = getNativeDesktopShell()
): Promise<GUIManagedNonServiceDaemonSession | null> {
  return await shell.getGUIOwnedNonServiceDaemonSession();
}

export async function getGUIOwnedNonServiceDaemonState(
  shell = getNativeDesktopShell()
): Promise<DaemonRuntimeState> {
  return await shell.getGUIOwnedNonServiceDaemonState();
}

export async function stopGUIOwnedNonServiceDaemonThroughAPI(
  sessionID: string,
  shell = getNativeDesktopShell()
): Promise<GUIManagedNonServiceDaemonSession> {
  return await stopGUIOwnedNonServiceDaemonThroughAPIBridge(shell as GUIOwnedNonServiceDaemonBridge, sessionID);
}

export async function getDaemonTrayStatus(shell = getNativeDesktopShell()): Promise<DaemonTrayStatus> {
  return await shell.getDaemonTrayStatus();
}

export async function getDaemonStartupIntegrationStatus(
  shell = getNativeDesktopShell()
): Promise<DaemonStartupIntegrationStatus> {
  return await shell.getDaemonStartupIntegrationStatus();
}

export async function openGuiFromDaemonTray(shell = getNativeDesktopShell()): Promise<void> {
  await shell.openGuiFromDaemonTray();
}

export async function showMainWindowFromDaemonTray(shell = getNativeDesktopShell()): Promise<void> {
  await shell.showMainWindowFromDaemonTray();
}

export async function storeRemoteInstanceCredential(
  record: RemoteInstanceCredentialRecord,
  secret: RemoteInstanceCredentialSecret,
  shell = getNativeDesktopShell()
): Promise<RemoteInstanceCredentialRecord> {
  return await storeCredentialThroughBridge(shell, record, secret);
}

export async function resolveRemoteInstanceCredential(
  credentialRef: string,
  shell = getNativeDesktopShell()
): Promise<RemoteInstanceCredentialSecret> {
  return await resolveCredentialThroughBridge(shell, credentialRef);
}

export async function deleteRemoteInstanceCredential(credentialRef: string, shell = getNativeDesktopShell()): Promise<void> {
  await deleteCredentialThroughBridge(shell, credentialRef);
}

export { promptForStartupAtLogin };
