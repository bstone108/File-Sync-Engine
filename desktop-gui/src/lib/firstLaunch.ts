import {
  controlVerifiedBundledDaemonLifecycle,
  type BundledDaemonLifecycleSettings,
  type BundledEngineRuntimeGate
} from './bundledEngine';

export type FirstLaunchDaemonRegistrationStatus = {
  registered: boolean;
  registrationRequired: boolean;
  platform: BundledDaemonLifecycleSettings['platform'];
  serviceName: string;
  startupConfigured: boolean;
  message: string;
};

export type FirstLaunchDaemonRegistrationChoice = {
  configureStartup: boolean;
};

export type FirstLaunchDaemonRegistrationResult = {
  registered: boolean;
  startupConfigured: boolean;
  message: string;
};

export type GUIManagedNonServiceDaemonSession = {
  sessionID: string;
  pid: number;
  kind?: 'service' | 'portable' | string;
  manager?: string;
  serviceName?: string;
  encryptedApiBaseURL: `https://${string}`;
  credentialRef: string;
  configPath?: string;
  statePath?: string;
  sessionMode: 'persistent-user-daemon' | 'temporary-session-only' | 'installed-service';
  launchedAt?: string;
  reconnectOnNextLaunch?: boolean;
  message: string;
  connectionState: 'starting' | 'running' | 'stopped' | 'unreachable' | string;
  nodeName?: string;
};

export type DaemonRuntimeState = {
  connectionState: 'running' | 'stopped' | 'unreachable' | string;
  nodeName?: string;
  pid: number;
  sessionID: string;
  message: string;
};

export type GUIOwnedNonServiceDaemonLaunchRequest = {
  sessionMode: GUIManagedNonServiceDaemonSession['sessionMode'];
  preferExistingReachableDaemon: boolean;
};

export type GUIOwnedNonServiceDaemonBridge = {
  requestGUIOwnedNonServiceDaemonLaunch(
    request: GUIOwnedNonServiceDaemonLaunchRequest
  ): Promise<GUIManagedNonServiceDaemonSession>;
  adoptGUIOwnedNonServiceDaemon(sessionID: string): Promise<GUIManagedNonServiceDaemonSession>;
  getGUIOwnedNonServiceDaemonSession(): Promise<GUIManagedNonServiceDaemonSession | null>;
  stopGUIOwnedNonServiceDaemonThroughAPI(sessionID: string): Promise<GUIManagedNonServiceDaemonSession>;
};

export async function installBundledDaemonForCurrentOS(
  gate: BundledEngineRuntimeGate,
  settings: BundledDaemonLifecycleSettings,
  choice: FirstLaunchDaemonRegistrationChoice
): Promise<FirstLaunchDaemonRegistrationResult> {
  if (!gate.bundleVerified) {
    throw new Error(`bundled engine verification failed: ${gate.reason}`);
  }

  // The daemon service-command endpoint is the encrypted API handoff for service managers.
  // This frontend contract never installs or starts a process directly; native/platform code owns reviewable handoff details.
  // API path intentionally remains /v1/service-command through controlVerifiedBundledDaemonLifecycle.
  const response = await controlVerifiedBundledDaemonLifecycle(gate, settings, 'start');
  if (!response.ok) {
    throw new Error(`bundled daemon service registration request failed: ${response.status}`);
  }

  return {
    registered: true,
    startupConfigured: choice.configureStartup,
    message: choice.configureStartup
      ? 'Bundled daemon registration requested with startup/login/start-at-boot configuration.'
      : 'Bundled daemon registration requested without automatic startup.'
  };
}

export function promptForStartupAtLogin(status: FirstLaunchDaemonRegistrationStatus): boolean {
  return status.registrationRequired && !status.startupConfigured;
}

export async function requestGUIOwnedNonServiceDaemonLaunch(
  gate: BundledEngineRuntimeGate,
  bridge: GUIOwnedNonServiceDaemonBridge,
  request: GUIOwnedNonServiceDaemonLaunchRequest
): Promise<GUIManagedNonServiceDaemonSession> {
  if (!gate.bundleVerified) {
    throw new Error(`bundled engine verification failed: ${gate.reason}`);
  }
  return await bridge.requestGUIOwnedNonServiceDaemonLaunch(request);
}

export async function adoptGUIOwnedNonServiceDaemon(
  bridge: GUIOwnedNonServiceDaemonBridge,
  sessionID: string
): Promise<GUIManagedNonServiceDaemonSession> {
  return await bridge.adoptGUIOwnedNonServiceDaemon(sessionID);
}

export async function stopGUIOwnedNonServiceDaemonThroughAPI(
  bridge: GUIOwnedNonServiceDaemonBridge,
  sessionID: string
): Promise<GUIManagedNonServiceDaemonSession> {
  return await bridge.stopGUIOwnedNonServiceDaemonThroughAPI(sessionID);
}
