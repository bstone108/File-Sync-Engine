import type { MobilePlatform, MobilePairingSecureStorageTarget } from './mobileAppContract';

export type MobileRemoteInstanceOnboardingSource =
  | 'direct-api-endpoint-key'
  | 'pasted-pairing-code'
  | 'uploaded-identity-file'
  | 'scanned-animated-pairing-code'
  | 'shared-identity-file';

export type MobileRemoteInstanceConnectionState =
  | 'discovered'
  | 'connecting'
  | 'paired'
  | 'offline'
  | 'failed'
  | 'revoked';

export interface MobileRemoteInstanceCandidateInput {
  readonly platform: MobilePlatform;
  readonly source: MobileRemoteInstanceOnboardingSource;
  readonly instanceID?: string;
  readonly displayName?: string;
  readonly endpoint?: string;
  readonly credentialRef?: string;
  readonly peerIDCode?: string;
  readonly reachable?: boolean;
  readonly statusDetail?: string;
}

export interface MobileRemoteInstanceRecord {
  readonly id: string;
  readonly displayName: string;
  readonly source: MobileRemoteInstanceOnboardingSource;
  readonly endpoint?: string;
  readonly credentialRef?: string;
  readonly peerIDCode?: string;
  readonly reachable: boolean;
  readonly connectionState: MobileRemoteInstanceConnectionState;
  readonly secureStorageTarget: MobilePairingSecureStorageTarget;
  readonly progressiveStatusHydration: true;
  readonly statusSummary: string;
}

export interface MobileLoadedSharedIdentityDiscoverySnapshot {
  readonly identityID: string;
  readonly peers: readonly MobileSameIdentityDiscoveredPeer[];
}

export interface MobileSameIdentityDiscoveredPeer {
  readonly peerIDCode: string;
  readonly displayName?: string;
  readonly endpoint?: string;
  readonly reachable: boolean;
  readonly credentialRef?: string;
  readonly statusDetail?: string;
}

export const mobileRemoteInstanceOnboardingSources: readonly MobileRemoteInstanceOnboardingSource[] = [
  'direct-api-endpoint-key',
  'pasted-pairing-code',
  'uploaded-identity-file',
  'scanned-animated-pairing-code',
  'shared-identity-file',
];

export const mobileRemoteInstanceSecretBoundary = 'raw API keys must be stored only through the platform secure credential store';

export function mobileSecureStorageTarget(platform: MobilePlatform): MobilePairingSecureStorageTarget {
  return platform === 'android' ? 'android-keystore' : 'ios-keychain';
}

export function buildMobileRemoteInstanceCandidate(input: MobileRemoteInstanceCandidateInput): MobileRemoteInstanceRecord {
  const peerIDCode = input.peerIDCode?.trim();
  const endpoint = input.endpoint?.trim();
  const instanceID = input.instanceID?.trim() || peerIDCode || endpoint || `${input.source}-pending`;
  const displayName = input.displayName?.trim() || (peerIDCode ? `Peer ${peerIDCode}` : 'Remote daemon');
  const reachable = input.reachable ?? Boolean(endpoint);
  const connectionState: MobileRemoteInstanceConnectionState = reachable ? 'connecting' : 'discovered';
  return {
    id: `mobile-remote:${instanceID}`,
    displayName,
    source: input.source,
    endpoint: endpoint || undefined,
    credentialRef: input.credentialRef,
    peerIDCode: peerIDCode || undefined,
    reachable,
    connectionState,
    secureStorageTarget: mobileSecureStorageTarget(input.platform),
    progressiveStatusHydration: true,
    statusSummary: input.statusDetail ?? 'Mobile remote instance onboarding keeps only endpoint/peer metadata and credentialRef in app state; details progressively hydrate as pairing, discovery, and encrypted API negotiation complete.',
  };
}

export function autoPopulateMobileSameIdentityInstances(
  platform: MobilePlatform,
  snapshot: MobileLoadedSharedIdentityDiscoverySnapshot,
  current: readonly MobileRemoteInstanceRecord[] = []
): readonly MobileRemoteInstanceRecord[] {
  const byID = new Map<string, MobileRemoteInstanceRecord>();
  for (const existing of current) {
    byID.set(existing.id, existing);
  }
  for (const peer of snapshot.peers) {
    const discovered = buildMobileRemoteInstanceCandidate({
      platform,
      source: 'shared-identity-file',
      peerIDCode: peer.peerIDCode,
      displayName: peer.displayName,
      endpoint: peer.endpoint,
      credentialRef: peer.credentialRef,
      reachable: peer.reachable,
      statusDetail: peer.statusDetail ?? `Discovered same-identity peer ${peer.peerIDCode}; progressively hydrates status, folders, capabilities, and rates as the encrypted identity mesh negotiates.`,
    });
    byID.set(discovered.id, discovered);
  }
  return [...byID.values()].sort((a, b) => a.displayName.localeCompare(b.displayName) || a.id.localeCompare(b.id));
}
