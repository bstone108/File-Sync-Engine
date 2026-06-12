import type {
  MobileDaemonConnectionSettings,
  MobileDaemonAPIRequestOptions,
  MobileDiscoveryCommandRequest,
  MobileFolderCommandRequest,
  MobileMeshSettingsCommandRequest,
  MobilePeerCommandRequest,
  MobileTransferCommandRequest,
} from './mobileDaemonApi';
import {
  fetchMobileBackupJobs,
  fetchMobileDaemonFolders,
  fetchMobileDaemonPeers,
  fetchMobileDaemonStatus,
  fetchMobileMeshSettings,
  fetchMobileRecentLogs,
  patchMobileDaemonConfig,
  readMobileDaemonConfig,
  runMobileMaintenanceScrub,
  sendMobileDiscoveryCommand,
  sendMobileFolderCommand,
  sendMobileMeshSettingsCommand,
  sendMobilePeerCommand,
  sendMobileTransferCommand,
} from './mobileDaemonApi';
import type { MobileViewID } from './mobileAppContract';

export const mobileMeshSettingsScreenEndpoints = ['/v1/mesh/settings', '/v1/mesh/settings-command'] as const;

export type MobileOperationalScreenID = Extract<
  MobileViewID,
  | 'overview'
  | 'folders'
  | 'peers-identity'
  | 'transfers'
  | 'warnings-logs'
  | 'maintenance-backups'
  | 'daemon-settings'
>;

export type MobileOperationalScreenBinding = {
  readonly id: MobileOperationalScreenID;
  readonly label: string;
  readonly load: () => Promise<unknown>;
  readonly commands?: readonly string[];
};

export type MobileOperationalScreenCommandInput = {
  readonly peer?: MobilePeerCommandRequest;
  readonly folder?: MobileFolderCommandRequest;
  readonly discovery?: MobileDiscoveryCommandRequest;
  readonly transfer?: MobileTransferCommandRequest;
  readonly configPatch?: Record<string, unknown>;
  readonly maintenanceFolderId?: string;
  readonly meshNodeID?: string;
  readonly meshSettings?: MobileMeshSettingsCommandRequest;
};

export function buildMobileOperationalScreenBindings(
  settings: MobileDaemonConnectionSettings,
  options: MobileDaemonAPIRequestOptions,
  input: MobileOperationalScreenCommandInput = {}
): readonly MobileOperationalScreenBinding[] {
  return [
    {
      id: 'overview',
      label: 'Overview',
      load: () => fetchMobileDaemonStatus(settings, options),
    },
    {
      id: 'folders',
      label: 'Folders',
      load: () => fetchMobileDaemonFolders(settings, options),
      commands: ['folder add', 'folder update', 'folder remove'],
    },
    {
      id: 'peers-identity',
      label: 'Peers & identity',
      load: async () => ({
        peers: await fetchMobileDaemonPeers(settings, options),
        meshSettings: await fetchMobileMeshSettings(settings, options, input.meshNodeID),
      }),
      commands: ['peer add', 'peer update', 'peer remove', 'mesh settings read', 'mesh settings command'],
    },
    {
      id: 'transfers',
      label: 'Transfers',
      load: () => input.transfer
        ? sendMobileTransferCommand(settings, options, input.transfer)
        : fetchMobileDaemonStatus(settings, options),
      commands: ['transfer pause', 'transfer resume', 'transfer cancel'],
    },
    {
      id: 'warnings-logs',
      label: 'Warnings & logs',
      load: () => fetchMobileRecentLogs(settings, options),
    },
    {
      id: 'maintenance-backups',
      label: 'Maintenance & backups',
      load: async () => ({
        maintenance: await runMobileMaintenanceScrub(settings, options, { folderId: input.maintenanceFolderId }),
        backupJobs: await fetchMobileBackupJobs(settings, options),
      }),
      commands: ['maintenance scrub', 'backup jobs'],
    },
    {
      id: 'daemon-settings',
      label: 'Daemon settings',
      load: () => input.configPatch
        ? patchMobileDaemonConfig(settings, options, input.configPatch)
        : readMobileDaemonConfig(settings, options),
      commands: ['config read', 'config patch', 'discovery update'],
    },
  ];
}

export async function runMobileOperationalScreenCommand(
  settings: MobileDaemonConnectionSettings,
  options: MobileDaemonAPIRequestOptions,
  input: MobileOperationalScreenCommandInput
): Promise<unknown> {
  if (input.peer) {
    return await sendMobilePeerCommand(settings, options, input.peer);
  }
  if (input.folder) {
    return await sendMobileFolderCommand(settings, options, input.folder);
  }
  if (input.discovery) {
    return await sendMobileDiscoveryCommand(settings, options, input.discovery);
  }
  if (input.transfer) {
    return await sendMobileTransferCommand(settings, options, input.transfer);
  }
  if (input.configPatch) {
    return await patchMobileDaemonConfig(settings, options, input.configPatch);
  }
  if (input.meshSettings) {
    return await sendMobileMeshSettingsCommand(settings, options, input.meshSettings);
  }
  return await fetchMobileDaemonStatus(settings, options);
}
