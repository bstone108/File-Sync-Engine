import type { NativeDesktopShell } from './nativeShell';

type WailsAppBindings = {
  RequestGUIOwnedNonServiceDaemonLaunch(request: unknown): Promise<unknown>;
  AdoptGUIOwnedNonServiceDaemon(sessionID: string): Promise<unknown>;
  GetGUIOwnedNonServiceDaemonSession(): Promise<unknown>;
  GetGUIOwnedNonServiceDaemonState(): Promise<unknown>;
  StopGUIOwnedNonServiceDaemonThroughAPI(sessionID: string): Promise<unknown>;
  DiscoverLocalDaemon(): Promise<unknown>;
  ControlLocalDaemon(request: unknown): Promise<unknown>;
  DaemonAPIRequest(request: unknown): Promise<unknown>;
  InspectBundledEngineResources(): Promise<unknown>;
  GetDesktopPreferences(): Promise<unknown>;
  SaveDesktopPreferences(preferences: unknown): Promise<unknown>;
  GetRemoteInstanceRegistry(): Promise<unknown>;
  SelectRemoteInstance(request: unknown): Promise<unknown>;
  OnboardRemoteInstance(request: unknown): Promise<unknown>;
  UpdateRemoteInstance(request: unknown): Promise<unknown>;
  RemoveRemoteInstance(request: unknown): Promise<unknown>;
  StoreRemoteInstanceCredential(record: unknown, secret: unknown): Promise<unknown>;
  DeleteRemoteInstanceCredential(credentialRef: string): Promise<void>;
};

declare global {
  interface Window {
    go?: { main?: { App?: WailsAppBindings } };
  }
}

function unsupported(operation: string): never {
  throw new Error(`${operation} is unavailable in this build; use the local engine lifecycle controls instead.`);
}

export function installWailsNativeShellBridge(): boolean {
  if (window.fseDesktopShell) return true;
  const app = window.go?.main?.App;
  if (!app) return false;

  window.fseDesktopShell = {
    inspectBundledEngineResources: () => app.InspectBundledEngineResources() as ReturnType<NativeDesktopShell['inspectBundledEngineResources']>,
    getDesktopPreferences: () => app.GetDesktopPreferences() as ReturnType<NativeDesktopShell['getDesktopPreferences']>,
    saveDesktopPreferences: (preferences) => app.SaveDesktopPreferences(preferences) as ReturnType<NativeDesktopShell['saveDesktopPreferences']>,
    getRemoteInstanceRegistry: () => app.GetRemoteInstanceRegistry() as ReturnType<NativeDesktopShell['getRemoteInstanceRegistry']>,
    selectRemoteInstance: (request) => app.SelectRemoteInstance(request) as ReturnType<NativeDesktopShell['selectRemoteInstance']>,
    onboardRemoteInstance: (request) => app.OnboardRemoteInstance(request) as ReturnType<NativeDesktopShell['onboardRemoteInstance']>,
    updateRemoteInstance: (request) => app.UpdateRemoteInstance(request) as ReturnType<NativeDesktopShell['updateRemoteInstance']>,
    removeRemoteInstance: (request) => app.RemoveRemoteInstance(request) as ReturnType<NativeDesktopShell['removeRemoteInstance']>,
    discoverLocalDaemon: () => app.DiscoverLocalDaemon() as ReturnType<NativeDesktopShell['discoverLocalDaemon']>,
    controlLocalDaemon: (request) => app.ControlLocalDaemon(request) as ReturnType<NativeDesktopShell['controlLocalDaemon']>,
    daemonAPIRequest: (request) => app.DaemonAPIRequest(request) as ReturnType<NativeDesktopShell['daemonAPIRequest']>,
    requestGUIOwnedNonServiceDaemonLaunch: (request) =>
      app.RequestGUIOwnedNonServiceDaemonLaunch(request) as ReturnType<NativeDesktopShell['requestGUIOwnedNonServiceDaemonLaunch']>,
    adoptGUIOwnedNonServiceDaemon: (sessionID) =>
      app.AdoptGUIOwnedNonServiceDaemon(sessionID) as ReturnType<NativeDesktopShell['adoptGUIOwnedNonServiceDaemon']>,
    getGUIOwnedNonServiceDaemonSession: () =>
      app.GetGUIOwnedNonServiceDaemonSession() as ReturnType<NativeDesktopShell['getGUIOwnedNonServiceDaemonSession']>,
    getGUIOwnedNonServiceDaemonState: () =>
      app.GetGUIOwnedNonServiceDaemonState() as ReturnType<NativeDesktopShell['getGUIOwnedNonServiceDaemonState']>,
    stopGUIOwnedNonServiceDaemonThroughAPI: (sessionID) =>
      app.StopGUIOwnedNonServiceDaemonThroughAPI(sessionID) as ReturnType<NativeDesktopShell['stopGUIOwnedNonServiceDaemonThroughAPI']>,
    readBundledEngineResourceManifest: () => unsupported('Legacy bundled manifest workflow'),
    observeBundledEngineResources: () => unsupported('Legacy bundled manifest workflow'),
    getLocalLifecycleSettings: () => unsupported('Legacy service setup workflow'),
    getFirstLaunchDaemonRegistrationStatus: () => unsupported('Legacy service setup workflow'),
    getDaemonTrayStatus: () => unsupported('Daemon tray integration'),
    getDaemonStartupIntegrationStatus: () => unsupported('Daemon startup integration'),
    openGuiFromDaemonTray: () => unsupported('Daemon tray integration'),
    showMainWindowFromDaemonTray: () => unsupported('Daemon tray integration'),
    storeRemoteInstanceCredential: (record, secret) => app.StoreRemoteInstanceCredential(record, secret) as ReturnType<NativeDesktopShell['storeRemoteInstanceCredential']>,
    deleteRemoteInstanceCredential: (credentialRef) => app.DeleteRemoteInstanceCredential(credentialRef)
  };
  return true;
}
