export type ManagedDaemonInstanceKind = 'local' | 'remote';
export type ManagedDaemonConnectionState = 'offline' | 'connecting' | 'online' | 'failed' | 'paired' | 'revoked';
export type PeerPairingNegotiationState = 'discovered' | 'connecting' | 'negotiating-identity' | 'exchanging-keys' | 'waiting-on-relay' | 'direct-connection-established' | 'paired' | 'failed' | 'revoked-identity' | 'offline';
export type RemoteInstanceOnboardingSource = 'api-endpoint-key' | 'pasted-pairing-code' | 'imported-identity-file' | 'scanned-animated-code' | 'loaded-shared-identity';

export type DiscoveredIdentityPeer = {
  peerIDCode: string;
  label?: string;
  endpoint?: string;
  connectionState?: ManagedDaemonConnectionState;
  capabilities?: string[];
  folders?: string[];
  receiveRemainingBytes?: number;
  sendRemainingBytes?: number;
  averageReceiveBytesPerSecond?: number;
  averageSendBytesPerSecond?: number;
  pairingState?: PeerPairingNegotiationState;
  pairingDetail?: string;
  lastSeenAt?: string;
};

export type LoadedSharedIdentityDiscoverySnapshot = {
  sharedIdentityID: string;
  discoveredPeers: DiscoveredIdentityPeer[];
};

export type ManagedDaemonInstance = {
  id: string;
  kind: 'local' | 'remote';
  label: string;
  apiBaseURL: string;
  credentialRef?: string;
  revision?: number;
  onboardingSource?: RemoteInstanceOnboardingSource;
  group?: string;
  connectionState: ManagedDaemonConnectionState;
  statusSummary: string;
  receiveRemainingBytes?: number;
  sendRemainingBytes?: number;
  averageReceiveBytesPerSecond?: number;
  averageSendBytesPerSecond?: number;
  pairingState?: PeerPairingNegotiationState;
  pairingDetail?: string;
};

export type ManagedDaemonTransferStatusMetrics = {
  label: string;
  value: string;
};

export type RemoteInstanceOnboardingInput = {
  source: RemoteInstanceOnboardingSource;
  label?: string;
  endpoint?: string;
  credentialRef?: string;
  pairingCode?: string;
  identityFileText?: string;
  animatedCodeSummary?: string;
  sharedIdentityID?: string;
};

export type RemoteMeshSettingsDocument = {
  nodeID: string;
  revision: number;
  updatedAt: string;
  redactedSettingsSummary: string;
  ownerNote: 'node owns exactly one canonical settings document';
  configBoundary: 'config file as an editable local import/export surface';
};

export type RemoteMeshPendingSettingsChange = {
  targetNodeID: string;
  idempotencyKey: string;
  queuedAt: string;
  status: 'pending' | 'replicating' | 'applied' | 'failed' | 'acknowledged';
  nonSecretPatchSummary: string;
  deliveryNote: 'durable per-target pending command records';
};

export type OfflineRemoteSettingsEdit = {
  targetNodeID: string;
  selectedHostLabel: string;
  canQueueWhileOffline: true;
  pendingChange: RemoteMeshPendingSettingsChange;
  offlineDeliveryNote: 'offline edits remain durable local pending changes until the owner node receives, validates, applies, and acknowledges them';
};

const localInstanceID = 'local-bundled-engine';
const localInstancePinnedFirstNote = 'local instance pinned first';

export function defaultLocalDaemonInstance(): ManagedDaemonInstance {
  return {
    id: localInstanceID,
    kind: 'local',
    label: 'Local bundled engine',
    apiBaseURL: 'https://127.0.0.1:22420',
    credentialRef: 'desktop-vault:local-api-key',
    connectionState: 'offline',
    statusSummary: `${localInstancePinnedFirstNote}; opens by default and scopes controls to this selected host scope.`
  };
}

export function ensureLocalInstanceFirst(instances: ManagedDaemonInstance[]): ManagedDaemonInstance[] {
  const local = instances.find((instance) => instance.kind === 'local') ?? defaultLocalDaemonInstance();
  const remotes = instances
    .filter((instance) => instance.id !== local.id && instance.kind === 'remote')
    .sort((left, right) => left.label.localeCompare(right.label));
  return [local, ...remotes];
}

export function addRemoteDaemonInstance(
  instances: ManagedDaemonInstance[],
  remote: Omit<ManagedDaemonInstance, 'kind' | 'connectionState' | 'statusSummary'> & Partial<Pick<ManagedDaemonInstance, 'connectionState' | 'statusSummary'>>
): ManagedDaemonInstance[] {
  const nextRemote: ManagedDaemonInstance = {
    ...remote,
    kind: 'remote',
    connectionState: remote.connectionState ?? 'offline',
    statusSummary: remote.statusSummary ?? 'Remote daemon API endpoint saved; connect or pair before sending selected host scope commands.'
  };
  return ensureLocalInstanceFirst([...instances.filter((instance) => instance.id !== nextRemote.id), nextRemote]);
}

export function buildRemoteMeshSettingsDocument(input: {
  nodeID: string;
  revision?: number;
  updatedAt?: string;
  redactedSettingsSummary?: string;
}): RemoteMeshSettingsDocument {
  return {
    nodeID: requireRemoteOnboardingValue(input.nodeID, 'remote mesh node ID'),
    revision: input.revision ?? 1,
    updatedAt: input.updatedAt ?? 'pending replication timestamp',
    redactedSettingsSummary: input.redactedSettingsSummary ?? 'offline instances can be inspected or queued for later adoption through the trusted eventually-consistent mesh',
    ownerNote: 'node owns exactly one canonical settings document',
    configBoundary: 'config file as an editable local import/export surface'
  };
}

export function queueRemoteMeshSettingsChange(input: {
  targetNodeID: string;
  nonSecretPatchSummary: string;
  idempotencyKey?: string;
  queuedAt?: string;
}): RemoteMeshPendingSettingsChange {
  const targetNodeID = requireRemoteOnboardingValue(input.targetNodeID, 'remote mesh target node ID');
  const patchSummary = requireRemoteOnboardingValue(input.nonSecretPatchSummary, 'non-secret remote settings patch summary');
  return {
    targetNodeID,
    idempotencyKey: input.idempotencyKey?.trim() || `settings-change:${targetNodeID}:${patchSummary}`.toLowerCase().replace(/[^a-z0-9_.:-]/g, '-').replace(/-+/g, '-').slice(0, 128),
    queuedAt: input.queuedAt ?? 'pending replication timestamp',
    status: 'pending',
    nonSecretPatchSummary: patchSummary,
    deliveryNote: 'durable per-target pending command records'
  };
}

export function buildOfflineRemoteSettingsEdit(input: {
  targetNodeID: string;
  selectedHostLabel: string;
  nonSecretPatchSummary: string;
  idempotencyKey?: string;
  queuedAt?: string;
}): OfflineRemoteSettingsEdit {
  const targetNodeID = requireRemoteOnboardingValue(input.targetNodeID, 'offline remote settings target node ID');
  return {
    targetNodeID,
    selectedHostLabel: requireRemoteOnboardingValue(input.selectedHostLabel, 'selected offline host label'),
    canQueueWhileOffline: true,
    pendingChange: queueRemoteMeshSettingsChange({
      targetNodeID,
      nonSecretPatchSummary: input.nonSecretPatchSummary,
      idempotencyKey: input.idempotencyKey,
      queuedAt: input.queuedAt
    }),
    offlineDeliveryNote: 'offline edits remain durable local pending changes until the owner node receives, validates, applies, and acknowledges them'
  };
}

export function buildDiscoveredIdentityPeersFromLoadedSharedIdentity(
  snapshot: LoadedSharedIdentityDiscoverySnapshot
): DiscoveredIdentityPeer[] {
  const sharedIdentityID = requireRemoteOnboardingValue(snapshot.sharedIdentityID, 'loaded shared identity ID');
  return snapshot.discoveredPeers.map((peer) => ({
    ...peer,
    label: peer.label?.trim() || `Same-identity peer ${peer.peerIDCode}`,
    pairingState: peer.pairingState ?? 'discovered',
    pairingDetail: peer.pairingDetail ?? `Automatically discovered through loaded shared identity ${sharedIdentityID}; pairing/negotiation will progressively hydrate status, capabilities, folders, rates, and endpoint data.`,
    lastSeenAt: peer.lastSeenAt ?? 'loaded shared identity discovery snapshot'
  }));
}

export function upsertDiscoveredIdentityPeerInstances(
  instances: ManagedDaemonInstance[],
  discoveredPeers: DiscoveredIdentityPeer[]
): ManagedDaemonInstance[] {
  const withDiscovered = discoveredPeers.reduce<ManagedDaemonInstance[]>((current, peer) => {
    const peerIDCode = requireRemoteOnboardingValue(peer.peerIDCode, 'unique peer ID/code');
    const endpoint = peer.endpoint?.trim() || `identity-peer://${peerIDCode}`;
    const label = (peer.label?.trim() || `Peer ID/code ${peerIDCode}`).slice(0, 96);
    const capabilitySummary = (peer.capabilities ?? []).join(', ') || 'capabilities pending';
    const folderSummary = (peer.folders ?? []).join(', ') || 'folders pending';
    const discovered: ManagedDaemonInstance = {
      id: remoteInstanceID('loaded-shared-identity', endpoint, peerIDCode),
      kind: 'remote',
      label,
      apiBaseURL: endpoint,
      group: 'Remote daemon instances',
      connectionState: peer.connectionState ?? 'connecting',
      receiveRemainingBytes: peer.receiveRemainingBytes,
      sendRemainingBytes: peer.sendRemainingBytes,
      averageReceiveBytesPerSecond: peer.averageReceiveBytesPerSecond,
      averageSendBytesPerSecond: peer.averageSendBytesPerSecond,
      pairingState: peer.pairingState ?? connectionStateToPairingState(peer.connectionState ?? 'connecting'),
      pairingDetail: peer.pairingDetail ?? 'Pairing/negotiation status will update through discovered, connecting, negotiating identity, exchanging keys, waiting on relay/mesh hop, direct connection established, paired, failed, revoked identity, and offline states as the identity handshake progresses.',
      statusSummary: `Discovered same-identity peer from unique peer ID/code ${peerIDCode}; progressive status hydration will fill in name, status, capabilities (${capabilitySummary}), folders (${folderSummary}), rates, and other metadata as negotiation completes. Last seen: ${peer.lastSeenAt ?? 'pending'}.`
    };
    return ensureLocalInstanceFirst([...current.filter((instance) => instance.id !== discovered.id), discovered]);
  }, instances);
  return ensureLocalInstanceFirst(withDiscovered);
}

export function removeDaemonInstance(instances: ManagedDaemonInstance[], id: string): ManagedDaemonInstance[] {
  return ensureLocalInstanceFirst(instances.filter((instance) => instance.kind === 'local' || instance.id !== id));
}

export function buildRemoteInstanceOnboardingCandidate(input: RemoteInstanceOnboardingInput): Omit<ManagedDaemonInstance, 'kind' | 'connectionState' | 'statusSummary'> & Partial<Pick<ManagedDaemonInstance, 'connectionState' | 'statusSummary'>> {
  const endpoint = requireRemoteOnboardingValue(input.endpoint, 'remote API endpoint');
  const label = (input.label?.trim() || endpoint).slice(0, 96);
  const id = remoteInstanceID(input.source, endpoint, input.sharedIdentityID || input.pairingCode || input.identityFileText || input.animatedCodeSummary || label);
  const credentialRef = requireRemoteOnboardingValue(input.credentialRef, 'remote credentialRef');
  const sourceNotes: Record<RemoteInstanceOnboardingSource, string> = {
    'api-endpoint-key': 'Direct API endpoint/key onboarding saved the endpoint and credentialRef; raw API key material stays in the native credential vault.',
    'pasted-pairing-code': 'Pasted pairing code onboarding saved a pending paired remote instance while daemon-owned import/authorization completes.',
    'imported-identity-file': 'Imported identity file onboarding saved a pending paired remote instance while daemon-owned import/authorization completes.',
    'scanned-animated-code': 'Scanned animated code onboarding saved a pending paired remote instance after complete-payload verification.',
    'loaded-shared-identity': 'Loaded shared identity onboarding saved a discovered same-identity remote instance for progressive status hydration.'
  };
  return {
    id,
    label,
    apiBaseURL: endpoint,
    credentialRef,
    onboardingSource: input.source,
    group: 'Remote daemon instances',
    connectionState: input.source === 'api-endpoint-key' ? 'offline' : 'connecting',
    statusSummary: `${sourceNotes[input.source]} Remote onboarding stores raw API key material only in the native credential vault; the registry keeps endpoint, source, and credentialRef metadata.`
  };
}

function requireRemoteOnboardingValue(value: string | undefined, label: string): string {
  const trimmed = value?.trim() ?? '';
  if (!trimmed) {
    throw new Error(`${label} is required for remote instance onboarding`);
  }
  return trimmed;
}

function remoteInstanceID(source: RemoteInstanceOnboardingSource, endpoint: string, seed: string): string {
  const safe = `${source}:${endpoint}:${seed}`.toLowerCase().replace(/[^a-z0-9_.:-]/g, '-').replace(/-+/g, '-').slice(0, 96);
  return `remote-${safe || source}`;
}

export function groupDaemonInstances(instances: ManagedDaemonInstance[]): Record<string, ManagedDaemonInstance[]> {
  return ensureLocalInstanceFirst(instances).reduce<Record<string, ManagedDaemonInstance[]>>((groups, instance) => {
    const key = instance.kind === 'local' ? 'Local bundled engine' : (instance.group || 'Remote daemon instances');
    groups[key] = [...(groups[key] ?? []), instance];
    return groups;
  }, {});
}

export function defaultExpandedInstanceGroups(instances: ManagedDaemonInstance[]): Record<string, boolean> {
  return Object.keys(groupDaemonInstances(instances)).reduce<Record<string, boolean>>((expanded, groupName) => {
    expanded[groupName] = true;
    return expanded;
  }, {});
}

export function formatConnectionStateLabel(state: ManagedDaemonConnectionState): string {
  const labels: Record<ManagedDaemonConnectionState, string> = {
    offline: 'Offline',
    connecting: 'Connecting',
    online: 'Online',
    failed: 'Failed',
    paired: 'Paired',
    revoked: 'Revoked identity'
  };
  return labels[state];
}

export function formatPeerPairingNegotiationStateLabel(state: PeerPairingNegotiationState | undefined): string {
  const labels: Record<PeerPairingNegotiationState, string> = {
    discovered: 'Discovered',
    connecting: 'Connecting',
    'negotiating-identity': 'Negotiating identity',
    'exchanging-keys': 'Exchanging keys',
    'waiting-on-relay': 'Waiting on relay/mesh hop',
    'direct-connection-established': 'Direct connection established',
    paired: 'Paired',
    failed: 'Failed',
    'revoked-identity': 'Revoked identity',
    offline: 'Offline'
  };
  return labels[state ?? 'discovered'];
}

export function buildPeerPairingStatusLine(instance: ManagedDaemonInstance): string {
  const state = instance.pairingState ?? connectionStateToPairingState(instance.connectionState);
  const label = formatPeerPairingNegotiationStateLabel(state);
  return instance.pairingDetail ? `${label}: ${instance.pairingDetail}` : label;
}

function connectionStateToPairingState(state: ManagedDaemonConnectionState): PeerPairingNegotiationState {
  const states: Record<ManagedDaemonConnectionState, PeerPairingNegotiationState> = {
    offline: 'offline',
    connecting: 'connecting',
    online: 'direct-connection-established',
    failed: 'failed',
    paired: 'paired',
    revoked: 'revoked-identity'
  };
  return states[state];
}

export function formatTransferByteSummary(bytes: number | undefined): string {
  if (bytes === undefined) {
    return 'unknown';
  }
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KiB`;
  }
  if (bytes < 1024 * 1024 * 1024) {
    return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
  }
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GiB`;
}

export function buildReadableHostStatusMetrics(instance: ManagedDaemonInstance): ManagedDaemonTransferStatusMetrics[] {
  return [
    { label: 'Online/offline', value: formatConnectionStateLabel(instance.connectionState) },
    { label: 'Receive remaining', value: formatTransferByteSummary(instance.receiveRemainingBytes) },
    { label: 'Send remaining', value: formatTransferByteSummary(instance.sendRemainingBytes) },
    { label: 'Average receive rate', value: `${formatTransferByteSummary(instance.averageReceiveBytesPerSecond)}/s` },
    { label: 'Average send rate', value: `${formatTransferByteSummary(instance.averageSendBytesPerSecond)}/s` }
  ];
}
