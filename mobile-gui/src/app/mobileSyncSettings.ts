export type MobileSyncNetworkType = 'wifi' | 'ethernet' | 'cellular' | 'offline' | 'unknown';

export interface MobileSyncNetworkPolicy {
  readonly cellularSyncDisabled: boolean;
  readonly blockedScheduleReason: 'blocked-by-cellular-policy' | 'network-allowed';
  readonly userMessage: string;
}

export function shouldBlockMobileSyncForNetwork(cellularSyncDisabled: boolean, networkType: MobileSyncNetworkType): boolean {
  return cellularSyncDisabled && networkType === 'cellular';
}

export function buildMobileSyncNetworkPolicy(cellularSyncDisabled: boolean, networkType: MobileSyncNetworkType): MobileSyncNetworkPolicy {
  const blocked = shouldBlockMobileSyncForNetwork(cellularSyncDisabled, networkType);
  return {
    cellularSyncDisabled,
    blockedScheduleReason: blocked ? 'blocked-by-cellular-policy' : 'network-allowed',
    userMessage: blocked
      ? 'Mobile cellular sync is disabled by user settings.'
      : 'Wi-Fi and Ethernet remain allowed when available.',
  };
}
