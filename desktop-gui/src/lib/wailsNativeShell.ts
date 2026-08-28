import {
  AdoptGUIOwnedNonServiceDaemon,
  CheckDesktopAppUpdate,
  ControlLocalDaemon,
  DaemonAPIRequest,
  DeleteRemoteInstanceCredential,
  DiscoverLocalDaemon,
  GetDesktopAppUpdateStatus,
  GetDesktopPreferences,
  GetGUIOwnedNonServiceDaemonSession,
  GetGUIOwnedNonServiceDaemonState,
  GetRemoteInstanceRegistry,
  InspectBundledEngineResources,
  OnboardRemoteInstance,
  PostponeDesktopAppUpdate,
  RemoveRemoteInstance,
  RequestGUIOwnedNonServiceDaemonLaunch,
  RestartDesktopAppUpdate,
  SaveDesktopPreferences,
  SelectRemoteInstance,
  StopGUIOwnedNonServiceDaemonThroughAPI,
  StoreRemoteInstanceCredential,
  UpdateRemoteInstance
} from "../../wailsjs/go/main/App";
import type { NativeDesktopShell } from './nativeShell';

function unsupported(operation: string): never {
  throw new Error(`${operation} is unavailable in this build; use the local engine lifecycle controls instead.`);
}

export function installWailsNativeShellBridge(): boolean {
  if (window.fseDesktopShell) return true;

  window.fseDesktopShell = {
    inspectBundledEngineResources: () => InspectBundledEngineResources() as ReturnType<NativeDesktopShell['inspectBundledEngineResources']>,
    getDesktopPreferences: () => GetDesktopPreferences() as ReturnType<NativeDesktopShell['getDesktopPreferences']>,
    saveDesktopPreferences: (preferences) => SaveDesktopPreferences(preferences) as ReturnType<NativeDesktopShell['saveDesktopPreferences']>,
    getRemoteInstanceRegistry: () => GetRemoteInstanceRegistry() as ReturnType<NativeDesktopShell['getRemoteInstanceRegistry']>,
    selectRemoteInstance: (request) => SelectRemoteInstance(request) as ReturnType<NativeDesktopShell['selectRemoteInstance']>,
    onboardRemoteInstance: (request) => OnboardRemoteInstance(request as unknown as Parameters<typeof OnboardRemoteInstance>[0]) as ReturnType<NativeDesktopShell['onboardRemoteInstance']>,
    updateRemoteInstance: (request) => UpdateRemoteInstance(request) as ReturnType<NativeDesktopShell['updateRemoteInstance']>,
    removeRemoteInstance: (request) => RemoveRemoteInstance(request) as ReturnType<NativeDesktopShell['removeRemoteInstance']>,
    discoverLocalDaemon: () => DiscoverLocalDaemon() as ReturnType<NativeDesktopShell['discoverLocalDaemon']>,
    controlLocalDaemon: (request) => ControlLocalDaemon(request) as ReturnType<NativeDesktopShell['controlLocalDaemon']>,
    daemonAPIRequest: (request) => DaemonAPIRequest(request as unknown as Parameters<typeof DaemonAPIRequest>[0]) as ReturnType<NativeDesktopShell['daemonAPIRequest']>,
    requestGUIOwnedNonServiceDaemonLaunch: (request) =>
      RequestGUIOwnedNonServiceDaemonLaunch(request) as ReturnType<NativeDesktopShell['requestGUIOwnedNonServiceDaemonLaunch']>,
    adoptGUIOwnedNonServiceDaemon: (sessionID) =>
      AdoptGUIOwnedNonServiceDaemon(sessionID) as ReturnType<NativeDesktopShell['adoptGUIOwnedNonServiceDaemon']>,
    getGUIOwnedNonServiceDaemonSession: () =>
      GetGUIOwnedNonServiceDaemonSession() as ReturnType<NativeDesktopShell['getGUIOwnedNonServiceDaemonSession']>,
    getGUIOwnedNonServiceDaemonState: () =>
      GetGUIOwnedNonServiceDaemonState() as ReturnType<NativeDesktopShell['getGUIOwnedNonServiceDaemonState']>,
    stopGUIOwnedNonServiceDaemonThroughAPI: (sessionID) =>
      StopGUIOwnedNonServiceDaemonThroughAPI(sessionID) as ReturnType<NativeDesktopShell['stopGUIOwnedNonServiceDaemonThroughAPI']>,
    readBundledEngineResourceManifest: () => unsupported('Legacy bundled manifest workflow'),
    observeBundledEngineResources: () => unsupported('Legacy bundled manifest workflow'),
    getLocalLifecycleSettings: () => unsupported('Legacy service setup workflow'),
    getFirstLaunchDaemonRegistrationStatus: () => unsupported('Legacy service setup workflow'),
    getDaemonTrayStatus: () => unsupported('Daemon tray integration'),
    getDaemonStartupIntegrationStatus: () => unsupported('Daemon startup integration'),
    openGuiFromDaemonTray: () => unsupported('Daemon tray integration'),
    showMainWindowFromDaemonTray: () => unsupported('Daemon tray integration'),
    storeRemoteInstanceCredential: (record, secret) => StoreRemoteInstanceCredential(record, secret) as ReturnType<NativeDesktopShell['storeRemoteInstanceCredential']>,
    deleteRemoteInstanceCredential: (credentialRef) => DeleteRemoteInstanceCredential(credentialRef),
    getDesktopAppUpdateStatus: () => GetDesktopAppUpdateStatus() as ReturnType<NativeDesktopShell['getDesktopAppUpdateStatus']>,
    checkDesktopAppUpdate: () => CheckDesktopAppUpdate() as ReturnType<NativeDesktopShell['checkDesktopAppUpdate']>,
    restartDesktopAppUpdate: () => RestartDesktopAppUpdate() as ReturnType<NativeDesktopShell['restartDesktopAppUpdate']>,
    postponeDesktopAppUpdate: () => PostponeDesktopAppUpdate() as ReturnType<NativeDesktopShell['postponeDesktopAppUpdate']>
  };
  return true;
}
