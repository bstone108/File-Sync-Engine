export type DaemonConnectionSettings = {
  apiBaseURL: string;
  apiKey?: string;
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

function baseURL(settings: DaemonConnectionSettings): string {
  return settings.apiBaseURL.replace(/\/+$/, '');
}

async function daemonAPIRequest<T>(
  settings: DaemonConnectionSettings,
  path: string,
  init: RequestInit = {}
): Promise<T> {
  const nativeShell = typeof window === 'undefined' ? undefined : window.fseDesktopShell;
  if (settings.credentialRef && nativeShell) {
    const response = await nativeShell.daemonAPIRequest({
      apiBaseURL: settings.apiBaseURL,
      credentialRef: settings.credentialRef,
      method: init.method ?? 'GET',
      path,
      body: init.body === undefined ? undefined : JSON.parse(String(init.body))
    });
    return (typeof response.body === 'string' ? JSON.parse(response.body) : response.body) as T;
  }
  if (!settings.apiKey) {
    throw new Error('No native credential reference or API key is available for the selected daemon.');
  }
  const headers = new Headers(init.headers);
  headers.set('X-FSE-API-Key', settings.apiKey);
  headers.set('Accept', 'application/json');
  if (init.body !== undefined && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }
  const response = await fetch(`${baseURL(settings)}${path}`, { ...init, headers });
  if (!response.ok) {
    throw new Error(`daemon API request ${path} failed: ${response.status}`);
  }
  return await response.json() as T;
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
