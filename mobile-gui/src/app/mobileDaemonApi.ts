export type MobileDaemonConnectionSettings = {
  readonly apiBaseURL: string;
  readonly credentialRef: string;
};

export type MobileCredentialResolver = (credentialRef: string) => Promise<string>;

export type MobileDaemonAPIRequestOptions = {
  readonly resolveCredential: MobileCredentialResolver;
  readonly fetchImpl?: typeof fetch;
};

export type MobileDaemonStatus = {
  readonly nodeName?: string;
  readonly startedAt?: string;
  readonly folders?: readonly unknown[];
  readonly peers?: readonly unknown[];
  readonly maintenance?: unknown;
  readonly backup?: unknown;
};

export type MobileDaemonFoldersResponse = {
  readonly folders?: readonly unknown[];
};

export type MobileDaemonPeersResponse = {
  readonly peers?: readonly unknown[];
};

export type MobileRecentLogsResponse = {
  readonly records?: readonly unknown[];
};

export type MobileBackupJobsResponse = {
  readonly jobs?: readonly unknown[];
};

export type MobileMeshSettingsResponse = {
  readonly documents?: readonly unknown[];
  readonly pendingChanges?: readonly unknown[];
};

export type MobileDaemonConfig = Record<string, unknown>;
export type MobileDaemonConfigPatch = Record<string, unknown>;

export type MobilePeerCommandRequest = {
  readonly action: 'add' | 'remove' | 'update';
  readonly id?: string;
  readonly endpoint?: string;
  readonly credentialRef?: string;
  readonly [key: string]: unknown;
};

export type MobileFolderCommandRequest = {
  readonly action: 'add' | 'remove' | 'update';
  readonly id?: string;
  readonly path?: string;
  readonly mode?: string;
  readonly [key: string]: unknown;
};

export type MobileDiscoveryCommandRequest = {
  readonly action: 'update' | 'status';
  readonly disabled?: boolean;
  readonly dht?: boolean;
  readonly local?: boolean;
  readonly [key: string]: unknown;
};

export type MobileTransferCommandRequest = {
  readonly action: 'pause' | 'resume' | 'cancel';
  readonly folderID?: string;
  readonly peerID?: string;
};

export type MobileMaintenanceScrubRequest = {
  readonly folderId?: string;
};

export type MobileCommandResponse = {
  readonly ok?: boolean;
  readonly status?: string;
  readonly message?: string;
  readonly [key: string]: unknown;
};

export type MobileMeshSettingsCommandRequest = {
  readonly targetNodeID: string;
  readonly idempotencyKey: string;
  readonly patch: Record<string, unknown>;
};

function trimBaseURL(settings: MobileDaemonConnectionSettings): string {
  return settings.apiBaseURL.replace(/\/+$/, '');
}

function jsonBody(body: unknown): string {
  return JSON.stringify(body ?? {});
}

async function mobileDaemonAPIRequest<T>(
  settings: MobileDaemonConnectionSettings,
  options: MobileDaemonAPIRequestOptions,
  path: string,
  init: RequestInit = {}
): Promise<T> {
  const secret = await options.resolveCredential(settings.credentialRef);
  const headers = new Headers(init.headers);
  headers.set('X-API-Key', secret);
  headers.set('Accept', 'application/json');
  if (init.body !== undefined && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }
  const fetcher = options.fetchImpl ?? fetch;
  const response = await fetcher(`${trimBaseURL(settings)}${path}`, { ...init, headers });
  if (!response.ok) {
    throw new Error(`mobile daemon API request ${path} failed: ${response.status}`);
  }
  return await response.json() as T;
}

export async function fetchMobileDaemonStatus(
  settings: MobileDaemonConnectionSettings,
  options: MobileDaemonAPIRequestOptions
): Promise<MobileDaemonStatus> {
  return await mobileDaemonAPIRequest<MobileDaemonStatus>(settings, options, '/v1/status');
}

export async function fetchMobileDaemonFolders(
  settings: MobileDaemonConnectionSettings,
  options: MobileDaemonAPIRequestOptions
): Promise<MobileDaemonFoldersResponse> {
  return await mobileDaemonAPIRequest<MobileDaemonFoldersResponse>(settings, options, '/v1/folders');
}

export async function fetchMobileDaemonPeers(
  settings: MobileDaemonConnectionSettings,
  options: MobileDaemonAPIRequestOptions
): Promise<MobileDaemonPeersResponse> {
  return await mobileDaemonAPIRequest<MobileDaemonPeersResponse>(settings, options, '/v1/peers');
}

export async function readMobileDaemonConfig(
  settings: MobileDaemonConnectionSettings,
  options: MobileDaemonAPIRequestOptions
): Promise<MobileDaemonConfig> {
  return await mobileDaemonAPIRequest<MobileDaemonConfig>(settings, options, '/v1/config');
}

export async function patchMobileDaemonConfig(
  settings: MobileDaemonConnectionSettings,
  options: MobileDaemonAPIRequestOptions,
  patch: MobileDaemonConfigPatch
): Promise<MobileCommandResponse> {
  return await mobileDaemonAPIRequest<MobileCommandResponse>(settings, options, '/v1/config', {
    method: 'PATCH',
    body: jsonBody(patch),
  });
}

export async function sendMobilePeerCommand(
  settings: MobileDaemonConnectionSettings,
  options: MobileDaemonAPIRequestOptions,
  command: MobilePeerCommandRequest
): Promise<MobileCommandResponse> {
  return await mobileDaemonAPIRequest<MobileCommandResponse>(settings, options, '/v1/peer-command', {
    method: 'POST',
    body: jsonBody(command),
  });
}

export async function sendMobileFolderCommand(
  settings: MobileDaemonConnectionSettings,
  options: MobileDaemonAPIRequestOptions,
  command: MobileFolderCommandRequest
): Promise<MobileCommandResponse> {
  return await mobileDaemonAPIRequest<MobileCommandResponse>(settings, options, '/v1/folder-command', {
    method: 'POST',
    body: jsonBody(command),
  });
}

export async function sendMobileDiscoveryCommand(
  settings: MobileDaemonConnectionSettings,
  options: MobileDaemonAPIRequestOptions,
  command: MobileDiscoveryCommandRequest
): Promise<MobileCommandResponse> {
  return await mobileDaemonAPIRequest<MobileCommandResponse>(settings, options, '/v1/discovery-command', {
    method: 'POST',
    body: jsonBody(command),
  });
}

export async function sendMobileTransferCommand(
  settings: MobileDaemonConnectionSettings,
  options: MobileDaemonAPIRequestOptions,
  command: MobileTransferCommandRequest
): Promise<MobileCommandResponse> {
  return await mobileDaemonAPIRequest<MobileCommandResponse>(settings, options, '/v1/transfer-command', {
    method: 'POST',
    body: jsonBody(command),
  });
}

export async function fetchMobileRecentLogs(
  settings: MobileDaemonConnectionSettings,
  options: MobileDaemonAPIRequestOptions
): Promise<MobileRecentLogsResponse> {
  return await mobileDaemonAPIRequest<MobileRecentLogsResponse>(settings, options, '/v1/logs');
}

export async function runMobileMaintenanceScrub(
  settings: MobileDaemonConnectionSettings,
  options: MobileDaemonAPIRequestOptions,
  request: MobileMaintenanceScrubRequest = {}
): Promise<MobileCommandResponse> {
  return await mobileDaemonAPIRequest<MobileCommandResponse>(settings, options, '/v1/maintenance/scrub', {
    method: 'POST',
    body: jsonBody(request),
  });
}

export async function fetchMobileBackupJobs(
  settings: MobileDaemonConnectionSettings,
  options: MobileDaemonAPIRequestOptions
): Promise<MobileBackupJobsResponse> {
  return await mobileDaemonAPIRequest<MobileBackupJobsResponse>(settings, options, '/v1/backup/jobs');
}

export async function fetchMobileMeshSettings(
  settings: MobileDaemonConnectionSettings,
  options: MobileDaemonAPIRequestOptions,
  nodeID?: string
): Promise<MobileMeshSettingsResponse> {
  const query = nodeID ? `?nodeId=${encodeURIComponent(nodeID)}` : '';
  return await mobileDaemonAPIRequest<MobileMeshSettingsResponse>(settings, options, `/v1/mesh/settings${query}`);
}

export async function sendMobileMeshSettingsCommand(
  settings: MobileDaemonConnectionSettings,
  options: MobileDaemonAPIRequestOptions,
  command: MobileMeshSettingsCommandRequest
): Promise<MobileCommandResponse> {
  return await mobileDaemonAPIRequest<MobileCommandResponse>(settings, options, '/v1/mesh/settings-command', {
    method: 'POST',
    body: jsonBody(command),
  });
}
