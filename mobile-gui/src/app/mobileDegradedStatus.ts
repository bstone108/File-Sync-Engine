export type MobileDegradedSyncCause =
  | 'ios-policy'
  | 'battery-state'
  | 'user-settings'
  | 'notification-permission'
  | 'network-state'
  | 'background-refresh';

export interface MobileDegradedSyncReason {
  readonly cause: MobileDegradedSyncCause;
  readonly message: string;
  readonly actionRequired: boolean;
}

export interface MobileDegradedSyncStatus {
  readonly visible: boolean;
  readonly headline: string;
  readonly reasons: readonly MobileDegradedSyncReason[];
  readonly summary: string;
}

export function buildMobileDegradedSyncStatus(reasons: readonly MobileDegradedSyncReason[]): MobileDegradedSyncStatus {
  return {
    visible: reasons.length > 0,
    headline: reasons.length > 0 ? 'Mobile sync is degraded' : 'Mobile sync is ready',
    reasons,
    summary: reasons.length > 0
      ? 'continuous daemon-style syncing is degraded until the listed mobile platform limits are resolved.'
      : 'Mobile background sync has no known degraded conditions.',
  };
}
