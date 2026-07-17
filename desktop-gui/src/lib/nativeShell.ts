import {
  adoptGUIOwnedNonServiceDaemon as adoptGUIOwnedNonServiceDaemonThroughBridge,
  requestGUIOwnedNonServiceDaemonLaunch as requestGUIOwnedNonServiceDaemonLaunchThroughBridge,
  stopGUIOwnedNonServiceDaemonThroughAPI as stopGUIOwnedNonServiceDaemonThroughAPIBridge,
  type GUIManagedNonServiceDaemonSession,
  type DaemonRuntimeState,
  type GUIOwnedNonServiceDaemonBridge,
  type GUIOwnedNonServiceDaemonLaunchRequest
} from './firstLaunch';
import {
  deleteRemoteInstanceCredential as deleteCredentialThroughBridge,
  storeRemoteInstanceCredential as storeCredentialThroughBridge,
  type RemoteInstanceCredentialRecord,
  type RemoteInstanceCredentialSecret
} from './credentialVault';

export type DaemonTrayStatus = { daemonOwnedTray: boolean; visible: boolean; state: 'unknown' | 'running' | 'stopped' | 'degraded'; message: string };
export type DaemonStartupIntegrationStatus = { startupEnabled: boolean; platform: 'systemd' | 'launchd' | 'windows'; serviceName: string; message: string };

export type LocalDaemonRuntimeState = {
  connectionState: string;
  nodeName?: string;
  pid: number;
  sessionID?: string;
  source?: string;
  kind?: string;
  manager?: string;
  serviceName?: string;
  apiBaseURL?: string;
  credentialRef?: string;
  message: string;
};

export type NativeDaemonAPIRequest = {
  apiBaseURL: string;
  credentialRef: string;
  method: string;
  path: string;
  body?: unknown;
};

export type NativeDaemonAPIResponse = { status: number; body: unknown };

export type BundledEngineInspection = {
  version: string;
  verified: boolean;
  message: string;
  entries: Array<{ target: string; relativePath: string; expectedExecutable: string; expectedVersion: string; expectedSHA256: string; exists: boolean; sha256?: string; verified: boolean; message: string }>;
};

export type DesktopPreferences = {
  theme: 'system' | 'light' | 'dark';
  density: 'comfortable' | 'compact';
  minimizeToTray: boolean;
  notificationsEnabled: boolean;
};

export type RemoteInstanceRegistryEntry = {
  id: string;
  label: string;
  apiBaseURL: string;
  credentialRef: string;
  source: 'api-endpoint-key';
  connectionState: 'offline' | 'connecting' | 'online' | 'failed';
  revision: number;
};

export type RemoteInstanceRegistry = { selectedInstanceID?: string; instances: RemoteInstanceRegistryEntry[]; credentialCleanupPending?: string[] };
export type RemoteInstanceUpdateRequest = { id: string; expectedCredentialRef: string; expectedRevision: number; label: string; apiBaseURL: string };
export type RemoteInstanceRemovalRequest = { id: string; expectedCredentialRef: string; expectedRevision: number; confirmLabel: string };
export type RemoteInstanceSelectionRequest = { instanceID: string; expectedSelectedInstanceID: string };
export type RemoteInstanceOnboardingRequest = { entry: RemoteInstanceRegistryEntry; secretValue: string };

export type NativeDesktopShell = {
  inspectBundledEngineResources(): Promise<BundledEngineInspection>;
  getDesktopPreferences(): Promise<DesktopPreferences>;
  saveDesktopPreferences(preferences: DesktopPreferences): Promise<DesktopPreferences>;
  getRemoteInstanceRegistry(): Promise<RemoteInstanceRegistry>;
  selectRemoteInstance(request: RemoteInstanceSelectionRequest): Promise<RemoteInstanceRegistry>;
  onboardRemoteInstance(request: RemoteInstanceOnboardingRequest): Promise<RemoteInstanceRegistry>;
  updateRemoteInstance(request: RemoteInstanceUpdateRequest): Promise<RemoteInstanceRegistry>;
  removeRemoteInstance(request: RemoteInstanceRemovalRequest): Promise<RemoteInstanceRegistry>;
  readBundledEngineResourceManifest(): Promise<unknown>;
  observeBundledEngineResources(): Promise<unknown[]>;
  getLocalLifecycleSettings(): Promise<unknown>;
  getFirstLaunchDaemonRegistrationStatus(): Promise<unknown>;
  getDaemonTrayStatus(): Promise<DaemonTrayStatus>;
  getDaemonStartupIntegrationStatus(): Promise<DaemonStartupIntegrationStatus>;

  discoverLocalDaemon(): Promise<LocalDaemonRuntimeState>;
  controlLocalDaemon(request: { action: 'status' | 'start' | 'stop' | 'restart'; source?: string }): Promise<LocalDaemonRuntimeState>;
  daemonAPIRequest(request: NativeDaemonAPIRequest): Promise<NativeDaemonAPIResponse>;
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



export async function discoverLocalDaemon(shell = getNativeDesktopShell()): Promise<LocalDaemonRuntimeState> {
  return await shell.discoverLocalDaemon();
}

export async function controlLocalDaemon(
  action: 'status' | 'start' | 'stop' | 'restart',
  source?: string,
  shell = getNativeDesktopShell()
): Promise<LocalDaemonRuntimeState> {
  return await shell.controlLocalDaemon({ action, source });
}


export async function storeRemoteInstanceCredential(
  record: RemoteInstanceCredentialRecord,
  secret: RemoteInstanceCredentialSecret,
  shell = getNativeDesktopShell()
): Promise<RemoteInstanceCredentialRecord> {
  return await storeCredentialThroughBridge(shell, record, secret);
}

export async function deleteRemoteInstanceCredential(credentialRef: string, shell = getNativeDesktopShell()): Promise<void> {
  await deleteCredentialThroughBridge(shell, credentialRef);
}
