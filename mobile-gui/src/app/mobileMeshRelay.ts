export type MobileMeshRelayRouteKind = 'direct-encrypted-api' | 'identity-mesh-relay' | 'offline-queued';
export type MobileMeshPendingStatus = 'pending' | 'applied' | 'failed' | 'acknowledged';

export interface MobileMeshRelayRouteInput {
  readonly selectedHostID: string;
  readonly directReachable: boolean;
  readonly relayReachable: boolean;
  readonly relayPeerIDs?: readonly string[];
  readonly statusDetail?: string;
}

export interface MobileMeshRelayRoute {
  readonly selectedHostID: string;
  readonly routeKind: MobileMeshRelayRouteKind;
  readonly relayPeerIDs: readonly string[];
  readonly durablePendingChangeRequired: true;
  readonly eventuallyConsistentDelivery: true;
  readonly authenticationRequired: true;
  readonly authorizationRequired: true;
  readonly acknowledgementRequired: true;
  readonly statusSummary: string;
}

export interface MobileOfflineSettingsEditInput {
  readonly targetNodeID: string;
  readonly originNodeID: string;
  readonly idempotencyKey: string;
  readonly patch: Record<string, unknown>;
  readonly relayRoute: MobileMeshRelayRoute;
}

export interface MobileOfflineSettingsEdit {
  readonly targetNodeID: string;
  readonly originNodeID: string;
  readonly idempotencyKey: string;
  readonly patch: Record<string, unknown>;
  readonly routeKind: MobileMeshRelayRouteKind;
  readonly status: MobileMeshPendingStatus;
  readonly statusModel: 'pending/applied/failed/acknowledged';
  readonly durablePendingChangeRequired: true;
  readonly eventuallyConsistentDelivery: true;
  readonly authenticationRequired: true;
  readonly authorizationRequired: true;
  readonly acknowledgementRequired: true;
  readonly statusSummary: string;
}

export function buildMobileMeshRelayRoute(input: MobileMeshRelayRouteInput): MobileMeshRelayRoute {
  const routeKind: MobileMeshRelayRouteKind = input.directReachable
    ? 'direct-encrypted-api'
    : input.relayReachable
      ? 'identity-mesh-relay'
      : 'offline-queued';
  const statusSummary = input.statusDetail ?? (
    routeKind === 'direct-encrypted-api'
      ? 'Direct encrypted API route is available for live mobile management.'
      : routeKind === 'identity-mesh-relay'
        ? 'Identity mesh relay route can inspect cached status and deliver settings commands through reachable peers; delivery is eventually consistent.'
        : 'Selected node is offline; mobile edits must be stored as durable pending changes until an identity-linked route becomes available.'
  );
  return {
    selectedHostID: input.selectedHostID,
    routeKind,
    relayPeerIDs: input.relayPeerIDs ?? [],
    durablePendingChangeRequired: true,
    eventuallyConsistentDelivery: true,
    authenticationRequired: true,
    authorizationRequired: true,
    acknowledgementRequired: true,
    statusSummary,
  };
}

export function buildMobileOfflineSettingsEdit(input: MobileOfflineSettingsEditInput): MobileOfflineSettingsEdit {
  return {
    targetNodeID: input.targetNodeID,
    originNodeID: input.originNodeID,
    idempotencyKey: input.idempotencyKey,
    patch: input.patch,
    routeKind: input.relayRoute.routeKind,
    status: 'pending',
    statusModel: 'pending/applied/failed/acknowledged',
    durablePendingChangeRequired: true,
    eventuallyConsistentDelivery: true,
    authenticationRequired: true,
    authorizationRequired: true,
    acknowledgementRequired: true,
    statusSummary: 'Mobile identity-linked settings edits remain pending until the owner node validates, applies, and acknowledges them through the trusted mesh.',
  };
}
