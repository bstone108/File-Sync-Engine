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
  SaveRemoteInstanceRegistry(registry: unknown): Promise<unknown>;
  StoreRemoteInstanceCredential(record: unknown, secret: unknown): Promise<unknown>;
  DeleteRemoteInstanceCredential(credentialRef: string): Promise<void>;
};

declare global {
  interface Window {
    go?: { main?: { App?: WailsAppBindings } };
  }
}

function unavailable(operation: string): never {
  throw new Error(`${operation} is not implemented by this native desktop build`);
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
    saveRemoteInstanceRegistry: (registry) => app.SaveRemoteInstanceRegistry(registry) as ReturnType<NativeDesktopShell['saveRemoteInstanceRegistry']>,
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
    readBundledEngineResourceManifest: async () => unavailable('Bundled engine manifest inspection'),
    observeBundledEngineResources: async () => unavailable('Bundled engine resource inspection'),
    getLocalLifecycleSettings: async () => unavailable('Installed service lifecycle control'),
    getFirstLaunchDaemonRegistrationStatus: async () => unavailable('Service registration status'),
    getDaemonTrayStatus: async () => unavailable('Daemon tray status'),
    getDaemonStartupIntegrationStatus: async () => unavailable('Startup integration status'),
    openGuiFromDaemonTray: async () => unavailable('Daemon tray open'),
    showMainWindowFromDaemonTray: async () => unavailable('Daemon tray window focus'),
    storeRemoteInstanceCredential: (record, secret) => app.StoreRemoteInstanceCredential(record, secret) as ReturnType<NativeDesktopShell['storeRemoteInstanceCredential']>,
    deleteRemoteInstanceCredential: (credentialRef) => app.DeleteRemoteInstanceCredential(credentialRef)
  };
  return true;
}
