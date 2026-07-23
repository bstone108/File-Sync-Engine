export type DaemonConnectionSettings = {
  apiBaseURL: string;
  credentialRef?: string;
};

export type DaemonStatus = {
  nodeName?: string;
  startedAt?: string;
  status?: string;
  folders?: number;
  peers?: number;
  maintenance?: Record<string, unknown>;
  backup?: Record<string, unknown>;
};

export type DaemonFolder = { id: string; path?: string; mode?: string; status?: string; [key: string]: unknown };
export type DaemonPeer = { id: string; endpoint?: string; status?: string; [key: string]: unknown };

export type DaemonConfig = Record<string, unknown>;
export type DaemonConfigPatch = Record<string, unknown>;

export type APITrustStatus = {
  mode: string;
  tlsEnabled: boolean;
  tlsRequired: boolean;
  certificateSha256?: string;
  trustedCertificateSha256?: string;
  trustedCertificateConfigured: boolean;
  trustedCertificateMatches: boolean;
  message?: string;
};

export type APITrustCommandRequest = {
  action: 'pin-active-certificate';
};

export type APITrustCommandResponse = APITrustStatus & {
  action: 'pin-active-certificate';
  status: string;
};

export type IdentityPairingPackage = {
  version: string;
  nodeName?: string;
  discoveryId: string;
  groupId: string;
  bootstrapProofKey: string;
  bootstrapEncryptionLevel: number;
  defaultPeerEncryptionLevel: number;
};

export type IdentityPairingImportResponse = {
  status: string;
  message?: string;
  groupId: string;
  remoteDiscoveryId: string;
  introductionEncryptionLevel: number;
  peerPairEncryptionLevel: number;
  requiresDedicatedPeerPairKey: boolean;
  usesBootstrapKeyForTraffic: boolean;
  pairId?: string;
  keyId?: string;
};

export type PeerCommandRequest = {
  action: 'add' | 'remove' | 'update';
  id?: string;
  endpoint?: string;
  apiKey?: string;
  [key: string]: unknown;
};

export type FolderCommandRequest = {
  action: 'add' | 'remove' | 'update';
  id?: string;
  path?: string;
  mode?: string;
  [key: string]: unknown;
};

export type DiscoveryCommandRequest = {
  action: 'update' | 'status';
  disabled?: boolean;
  dht?: boolean;
  local?: boolean;
  dhtNamespace?: string;
  dhtBootstrapPeers?: string[];
  [key: string]: unknown;
};

export type TransferCommandRequest = {
  action: 'pause' | 'resume' | 'cancel';
  folderID?: string;
  peerID?: string;
};

export type MaintenanceScrubRequest = {
  folderId?: string;
};

export type WebGUICommandRequest = {
  action: 'status' | 'install' | 'update' | 'start' | 'stop';
};

export type MeshSettingsDocument = {
  nodeId?: string;
  nodeID?: string;
  revision?: number;
  updatedAt?: string;
  settings?: Record<string, unknown>;
};

export type FilesystemBrowseEntry = {
  name: string;
  path: string;
  type: 'directory';
  readable: boolean;
};

export type FilesystemBrowseResponse = {
  path: string;
  entries: FilesystemBrowseEntry[];
};

export type MeshSettingsResponse = {
  documents: MeshSettingsDocument[];
};

export type MeshSettingsCommandRequest = {
  action: 'queue';
  targetNodeId: string;
  originNodeId: string;
  idempotencyKey: string;
  settingsPatch: Record<string, unknown>;
};

export type MeshSettingsCommandResponse = {
  status: 'queued' | 'pending' | 'acked' | 'failed' | string;
  targetNodeId?: string;
  idempotencyKey?: string;
  message?: string;
};

export type CommandResponse = {
  ok?: boolean;
  status?: string;
  message?: string;
  [key: string]: unknown;
};

export type DaemonEvent = {
  type: string;
  time: string;
  folderID?: string;
  peerID?: string;
  path?: string;
  message?: string;
  progress?: Record<string, number>;
};

export type DaemonLogsResponse = { entries: DaemonEvent[] };
export type DaemonTransferItem = {
  folderId: string;
  peerId?: string;
  status: 'active' | 'completed' | 'failed' | 'paused' | 'cancelled' | string;
  startedAt?: string;
  finishedAt?: string;
  eventType: string;
  message?: string;
};
export type DaemonTransferReadModel = {
  active: DaemonTransferItem[];
  history: DaemonTransferItem[];
  liveRatesAvailable: boolean;
  byteProgressAvailable: boolean;
};
export type SnapshotMarker = {
  id: string;
  folderId: string;
  cursor: number;
  description?: string;
  pinned?: boolean;
  deprecated?: boolean;
  [key: string]: unknown;
};
export type SnapshotResponse = { markers: SnapshotMarker[] };
export type RestorePlanResponse = {
  snapshotId: string;
  folderId: string;
  destination: string;
  files: Array<{ path: string; destinationPath: string; size: number; missingBlocks?: unknown[] }>;
  [key: string]: unknown;
};
export type RestoreResponse = { jobId?: string; totalFiles?: number; restoredFiles?: number; remainingFiles?: number; [key: string]: unknown };
export type SnapshotRetentionResponse = { jobId?: string; deprecatedSnapshots?: number; deletedSnapshots?: number; sweepEligibleBlocks?: number; [key: string]: unknown };
export type BackupScrubResponse = { archive?: Record<string, unknown>; checkpoints?: Record<string, unknown>; repairPlan?: Record<string, unknown>; [key: string]: unknown };
export type BackupJobsResponse = { restoreJobs: unknown[]; retentionJobs: unknown[]; repairJobs: unknown[] };

async function daemonAPIRequest<T>(
  settings: DaemonConnectionSettings,
  path: string,
  init: RequestInit = {}
): Promise<T> {
  const nativeShell = typeof window === 'undefined' ? undefined : window.fseDesktopShell;
  if (!nativeShell || !settings.credentialRef) {
    throw new Error('Native desktop daemon bridge is unavailable or no native credential is configured for the selected daemon. Start the packaged desktop application and connect through its local engine controls.');
  }
  const response = await nativeShell.daemonAPIRequest({
    apiBaseURL: settings.apiBaseURL,
    credentialRef: settings.credentialRef,
    method: init.method ?? 'GET',
    path,
    body: init.body === undefined ? undefined : JSON.parse(String(init.body))
  });
  return (typeof response.body === 'string' ? JSON.parse(response.body) : response.body) as T;
}

function jsonBody(body: unknown): string {
  return JSON.stringify(body ?? {});
}

export async function fetchDaemonStatus(settings: DaemonConnectionSettings): Promise<DaemonStatus> {
  return await daemonAPIRequest<DaemonStatus>(settings, '/v1/status');
}

export async function fetchDaemonFolders(settings: DaemonConnectionSettings): Promise<DaemonFolder[]> {
  return await daemonAPIRequest<DaemonFolder[]>(settings, '/v1/folders');
}

export async function fetchDaemonPeers(settings: DaemonConnectionSettings): Promise<DaemonPeer[]> {
  return await daemonAPIRequest<DaemonPeer[]>(settings, '/v1/peers');
}

export async function readDaemonConfig(settings: DaemonConnectionSettings): Promise<DaemonConfig> {
  return await daemonAPIRequest<DaemonConfig>(settings, '/v1/config');
}

export async function fetchAPITrustStatus(settings: DaemonConnectionSettings): Promise<APITrustStatus> {
  return await daemonAPIRequest<APITrustStatus>(settings, '/v1/api/trust');
}

export async function pinActiveAPICertificate(settings: DaemonConnectionSettings): Promise<APITrustCommandResponse> {
  const command: APITrustCommandRequest = { action: 'pin-active-certificate' };
  return await daemonAPIRequest<APITrustCommandResponse>(settings, '/v1/api/trust-command', {
    method: 'POST',
    body: jsonBody(command)
  });
}

export async function patchDaemonConfig(
  settings: DaemonConnectionSettings,
  patch: DaemonConfigPatch
): Promise<CommandResponse> {
  return await daemonAPIRequest<CommandResponse>(settings, '/v1/config', {
    method: 'PATCH',
    body: jsonBody(patch)
  });
}

export async function generateIdentityPairingPackage(
  settings: DaemonConnectionSettings,
  groupID: string
): Promise<IdentityPairingPackage> {
  return await daemonAPIRequest<IdentityPairingPackage>(settings, '/v1/identity-package', {
    method: 'POST',
    body: jsonBody({ groupId: groupID })
  });
}

export async function importIdentityPairingPackage(
  settings: DaemonConnectionSettings,
  identityPackage: IdentityPairingPackage
): Promise<IdentityPairingImportResponse> {
  return await daemonAPIRequest<IdentityPairingImportResponse>(settings, '/v1/identity-import', {
    method: 'POST',
    body: jsonBody({ package: identityPackage })
  });
}

export async function sendPeerCommand(
  settings: DaemonConnectionSettings,
  command: PeerCommandRequest
): Promise<CommandResponse> {
  return await daemonAPIRequest<CommandResponse>(settings, '/v1/peer-command', {
    method: 'POST',
    body: jsonBody(command)
  });
}

export async function sendFolderCommand(
  settings: DaemonConnectionSettings,
  command: FolderCommandRequest
): Promise<CommandResponse> {
  return await daemonAPIRequest<CommandResponse>(settings, '/v1/folder-command', {
    method: 'POST',
    body: jsonBody(command)
  });
}

export async function sendDiscoveryCommand(
  settings: DaemonConnectionSettings,
  command: DiscoveryCommandRequest
): Promise<CommandResponse> {
  return await daemonAPIRequest<CommandResponse>(settings, '/v1/discovery-command', {
    method: 'POST',
    body: jsonBody(command)
  });
}

export async function sendTransferCommand(
  settings: DaemonConnectionSettings,
  command: TransferCommandRequest
): Promise<CommandResponse> {
  return await daemonAPIRequest<CommandResponse>(settings, '/v1/transfer-command', {
    method: 'POST',
    body: jsonBody(command)
  });
}

export async function runMaintenanceScrub(
  settings: DaemonConnectionSettings,
  request: MaintenanceScrubRequest = {}
): Promise<CommandResponse> {
  return await daemonAPIRequest<CommandResponse>(settings, '/v1/maintenance/scrub', {
    method: 'POST',
    body: jsonBody(request)
  });
}

export async function sendWebGUICommand(
  settings: DaemonConnectionSettings,
  command: WebGUICommandRequest
): Promise<CommandResponse> {
  return await daemonAPIRequest<CommandResponse>(settings, '/v1/web-gui-command', {
    method: 'POST',
    body: jsonBody(command)
  });
}

export async function fetchRemoteMeshSettings(
  settings: DaemonConnectionSettings,
  nodeID?: string
): Promise<MeshSettingsResponse> {
  const query = nodeID?.trim() ? `?nodeId=${encodeURIComponent(nodeID.trim())}` : '';
  return await daemonAPIRequest<MeshSettingsResponse>(settings, `/v1/mesh/settings${query}`);
}

export async function browseFilesystemDirectories(
  settings: DaemonConnectionSettings,
  path = ''
): Promise<FilesystemBrowseResponse> {
  const query = path.trim() ? `?path=${encodeURIComponent(path.trim())}` : '';
  return await daemonAPIRequest<FilesystemBrowseResponse>(settings, `/v1/filesystem/browse${query}`);
}

export async function queueRemoteMeshSettingsCommand(
  settings: DaemonConnectionSettings,
  command: MeshSettingsCommandRequest
): Promise<MeshSettingsCommandResponse> {
  return await daemonAPIRequest<MeshSettingsCommandResponse>(settings, '/v1/mesh/settings-command', {
    method: 'POST',
    body: jsonBody(command)
  });
}

export async function fetchDaemonLogs(settings: DaemonConnectionSettings): Promise<DaemonLogsResponse> {
  return await daemonAPIRequest<DaemonLogsResponse>(settings, '/v1/logs');
}

export async function fetchDaemonTransfers(settings: DaemonConnectionSettings, limit = 50): Promise<DaemonTransferReadModel> {
  return await daemonAPIRequest<DaemonTransferReadModel>(settings, `/v1/transfers?limit=${encodeURIComponent(String(limit))}`);
}

export async function sendSnapshotCommand(
  settings: DaemonConnectionSettings,
  command: { action: 'list' | 'create' | 'pin' | 'deprecate' | 'delete'; id?: string; folderId?: string; description?: string }
): Promise<SnapshotResponse> {
  return await daemonAPIRequest<SnapshotResponse>(settings, '/v1/snapshots', { method: 'POST', body: jsonBody(command) });
}

export async function planSnapshotRestore(
  settings: DaemonConnectionSettings,
  request: { snapshotId: string; paths?: string[]; destinationRoot?: string; alternatePath?: string }
): Promise<RestorePlanResponse> {
  return await daemonAPIRequest<RestorePlanResponse>(settings, '/v1/restore-plans', { method: 'POST', body: jsonBody(request) });
}

export async function runSnapshotRestore(
  settings: DaemonConnectionSettings,
  request: { snapshotId: string; paths?: string[]; destinationRoot?: string; alternatePath?: string; revertDatabase?: boolean }
): Promise<RestoreResponse> {
  return await daemonAPIRequest<RestoreResponse>(settings, '/v1/restores', { method: 'POST', body: jsonBody(request) });
}

export async function runSnapshotRetention(settings: DaemonConnectionSettings, keepLast: number): Promise<SnapshotRetentionResponse> {
  return await daemonAPIRequest<SnapshotRetentionResponse>(settings, '/v1/snapshot-retention', { method: 'POST', body: jsonBody({ keepLast }) });
}

export async function runBackupScrub(settings: DaemonConnectionSettings): Promise<BackupScrubResponse> {
  return await daemonAPIRequest<BackupScrubResponse>(settings, '/v1/backup/scrub', { method: 'POST', body: jsonBody({}) });
}

export async function fetchBackupJobs(settings: DaemonConnectionSettings, snapshotId = ''): Promise<BackupJobsResponse> {
  const query = snapshotId.trim() ? `?snapshotId=${encodeURIComponent(snapshotId.trim())}` : '';
  return await daemonAPIRequest<BackupJobsResponse>(settings, `/v1/backup/jobs${query}`);
}
