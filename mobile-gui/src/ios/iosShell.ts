import { buildMobileNavigationModel, type MobileFeatureParityContract, type MobileViewID } from '../app/mobileAppContract';
import {
  addMobileAnimatedPairingFrame,
  createMobileAnimatedPairingScannerState,
  type MobileAnimatedPairingFrame,
  type MobileAnimatedPairingScannerState,
} from '../app/mobileAnimatedPairingScanner';
import {
  buildMobileAnimatedPairingScannerScreen,
  type MobileAnimatedPairingScannerScreenModel,
} from '../app/mobileAnimatedPairingScannerScreen';
import {
  buildMobileDegradedSyncStatus,
  type MobileDegradedSyncReason,
  type MobileDegradedSyncStatus,
} from '../app/mobileDegradedStatus';
import {
  buildMobileSyncNetworkPolicy,
  shouldBlockMobileSyncForNetwork,
  type MobileSyncNetworkPolicy,
} from '../app/mobileSyncSettings';
import {
  buildMobilePermissionPlan,
  type MobilePermissionPlan,
} from '../app/mobilePermissions';
import {
  autoPopulateMobileSameIdentityInstances,
  mobileRemoteInstanceOnboardingSources,
  type MobileRemoteInstanceOnboardingSource,
  type MobileRemoteInstanceRecord,
} from '../app/mobileRemoteInstances';
import {
  buildMobileMeshRelayRoute,
  type MobileMeshRelayRoute,
} from '../app/mobileMeshRelay';

export type IOSBackgroundCapabilityStatus = 'ready' | 'degraded' | 'blocked';

export interface IOSBackgroundCapability {
  readonly status: IOSBackgroundCapabilityStatus;
  readonly backgroundTasksRequired: true;
  readonly backgroundURLSessionRequired: true;
  readonly silentPushWakeSupported: boolean;
  readonly shortWakeWindowCheckpointing: true;
  readonly cellularSyncDisabled: boolean;
  readonly degradedStatusReasons: readonly string[];
}

export type IOSMobileShellState = {
  readonly platform: 'ios';
  readonly selectedHostID: string;
  readonly activeView: MobileViewID;
  readonly navigation: ReturnType<typeof buildMobileNavigationModel>;
  readonly featureParity: MobileFeatureParityContract;
  readonly backgroundCapability: IOSBackgroundCapability;
  readonly degradedSyncStatus: MobileDegradedSyncStatus;
  readonly permissionPlan: MobilePermissionPlan;
  readonly remoteInstanceOnboardingSources: readonly MobileRemoteInstanceOnboardingSource[];
  readonly discoveredSameIdentityInstances: readonly MobileRemoteInstanceRecord[];
  readonly mobileMeshRelayStatus: MobileMeshRelayRoute;
  readonly secureCredentialStore: 'ios-keychain';
};

export const iosMobileShellViewIDs: readonly MobileViewID[] = [
  'overview',
  'folders',
  'peers-identity',
  'transfers',
  'warnings-logs',
  'maintenance-backups',
  'daemon-settings',
  'mobile-app-settings',
  'help-details',
];

export const iosPermissionPlanKeyRequirements = [
  'ios-location-when-in-use-for-network-state',
  'ios-background-processing',
] as const;

export const iosRemoteInstanceOnboardingSourceRequirements = [
  'direct-api-endpoint-key',
  'pasted-pairing-code',
  'scanned-animated-pairing-code',
  'shared-identity-file',
] as const;

export const iosMobileMeshRelayRouteKinds = ['direct-encrypted-api', 'identity-mesh-relay', 'offline-queued'] as const;

export interface IOSMobileShellOptions {
  readonly selectedHostID?: string;
  readonly activeView?: MobileViewID;
  readonly backgroundRefreshEnabled?: boolean;
  readonly notificationsGranted?: boolean;
  readonly lowPowerModeEnabled?: boolean;
  readonly cellularSyncDisabled?: boolean;
  readonly silentPushWakeSupported?: boolean;
  readonly localFolderSyncEnabled?: boolean;
  readonly remoteInstanceManagementEnabled?: boolean;
  readonly discoveryEnabled?: boolean;
  readonly transferNotificationsEnabled?: boolean;
  readonly continuousBackgroundSyncEnabled?: boolean;
  readonly backgroundSyncEnabled?: boolean;
  readonly requireLocationForWifiNetworkState?: boolean;
  readonly degradedStatusReasons?: readonly string[];
  readonly sameIdentityDiscoverySnapshot?: Parameters<typeof autoPopulateMobileSameIdentityInstances>[1];
  readonly selectedHostDirectReachable?: boolean;
  readonly selectedHostRelayReachable?: boolean;
  readonly selectedHostRelayPeerIDs?: readonly string[];
}

export type IOSBackgroundSyncWorkKind =
  | 'metadata-catchup'
  | 'small-pending-transfer'
  | 'resumable-block-chunk'
  | 'checkpoint-flush'
  | 'retry-after-connectivity';

export type IOSBackgroundSyncSchedule =
  | 'background-app-refresh'
  | 'background-task'
  | 'background-url-session'
  | 'silent-push-wake'
  | 'foreground-only'
  | 'blocked-by-cellular-policy'
  | 'blocked-by-platform-policy';

export type IOSNetworkType = 'wifi' | 'ethernet' | 'cellular' | 'offline' | 'unknown';

export interface IOSBackgroundSyncRequest {
  readonly workKind: IOSBackgroundSyncWorkKind;
  readonly networkType: IOSNetworkType;
  readonly cellularSyncDisabled: boolean;
  readonly backgroundRefreshEnabled: boolean;
  readonly backgroundTasksAvailable: boolean;
  readonly backgroundURLSessionAvailable: boolean;
  readonly silentPushWakeAvailable?: boolean;
  readonly appForeground: boolean;
  readonly remainingWakeBudgetSeconds?: number;
}

export type IOSShortWakeWindowPlan = {
  readonly orderedWork: readonly IOSBackgroundSyncWorkKind[];
  readonly remainingWakeBudgetSeconds: number;
  readonly exitDeadlineSeconds: number;
  readonly durableCheckpointBeforeExit: true;
  readonly rescheduleNextWakeBeforeExit: true;
  readonly quickExitWhenBudgetExhausted: true;
};

export type IOSBackgroundSyncDecision = {
  readonly schedule: IOSBackgroundSyncSchedule;
  readonly workKind: IOSBackgroundSyncWorkKind;
  readonly networkPolicy: MobileSyncNetworkPolicy;
  readonly durableCheckpointRequired: true;
  readonly rescheduleBeforeSuspension: true;
  readonly shortWakeWindowPriority: readonly IOSBackgroundSyncWorkKind[];
  readonly shortWakeWindowPlan: IOSShortWakeWindowPlan;
  readonly degradedStatusReasons: readonly string[];
};

export function buildIOSShortWakeWindowPlan(remainingWakeBudgetSeconds: number): IOSShortWakeWindowPlan {
  const safeRemaining = Math.max(0, Math.floor(remainingWakeBudgetSeconds));
  const exitDeadlineSeconds = Math.max(0, safeRemaining - 5);
  return {
    orderedWork: ['metadata-catchup', 'small-pending-transfer', 'resumable-block-chunk', 'checkpoint-flush', 'retry-after-connectivity'],
    remainingWakeBudgetSeconds: safeRemaining,
    exitDeadlineSeconds,
    durableCheckpointBeforeExit: true,
    rescheduleNextWakeBeforeExit: true,
    quickExitWhenBudgetExhausted: true,
  };
}

export function buildIOSBackgroundSyncDecision(request: IOSBackgroundSyncRequest): IOSBackgroundSyncDecision {
  const networkPolicy = buildMobileSyncNetworkPolicy(request.cellularSyncDisabled, request.networkType);
  const cellularBlocked = shouldBlockMobileSyncForNetwork(request.cellularSyncDisabled, request.networkType);
  const platformBlocked = !request.appForeground && !request.backgroundRefreshEnabled && !request.backgroundTasksAvailable && !request.backgroundURLSessionAvailable && !request.silentPushWakeAvailable;
  const schedule: IOSBackgroundSyncSchedule = cellularBlocked
    ? 'blocked-by-cellular-policy'
    : platformBlocked
      ? 'blocked-by-platform-policy'
      : request.appForeground
        ? 'foreground-only'
        : request.workKind === 'resumable-block-chunk' && request.backgroundURLSessionAvailable
          ? 'background-url-session'
          : request.silentPushWakeAvailable
            ? 'silent-push-wake'
            : request.backgroundTasksAvailable
              ? 'background-task'
              : 'background-app-refresh';
  const degradedStatusReasons = [
    ...(cellularBlocked ? ['Cellular sync disable policy blocks iOS mobile network work before scheduling.'] : []),
    ...(platformBlocked ? ['No documented iOS wake/work opportunity is currently available; sync remains foreground-only until policy state changes.'] : []),
    ...(!request.backgroundRefreshEnabled ? ['Background App Refresh is disabled; permitted background sync opportunities are reduced.'] : []),
  ];

  return {
    schedule,
    workKind: request.workKind,
    networkPolicy,
    durableCheckpointRequired: true,
    rescheduleBeforeSuspension: true,
    shortWakeWindowPriority: ['metadata-catchup', 'small-pending-transfer', 'resumable-block-chunk', 'checkpoint-flush', 'retry-after-connectivity'],
    shortWakeWindowPlan: buildIOSShortWakeWindowPlan(request.remainingWakeBudgetSeconds ?? 30),
    degradedStatusReasons,
  };
}

export type IOSAnimatedPairingCameraAuthorizationState = 'unknown' | 'prompt-required' | 'authorized' | 'denied' | 'restricted';

export interface IOSAnimatedPairingCameraCaptureState {
  readonly cameraAuthorization: IOSAnimatedPairingCameraAuthorizationState;
  readonly cameraSessionActive: boolean;
  readonly avFoundationCaptureSessionRequired: true;
  readonly decodedFrameCount: number;
  readonly rejectedFrameCount: number;
  readonly scannerState: MobileAnimatedPairingScannerState;
  readonly scannerScreen: MobileAnimatedPairingScannerScreenModel;
  readonly progressText: string;
  readonly completePayloadVerification: 'required-before-import';
  readonly secureLocalStorageTarget: 'ios-keychain';
}

export interface IOSAnimatedPairingCameraCaptureOptions {
  readonly cameraAuthorization?: IOSAnimatedPairingCameraAuthorizationState;
  readonly cameraSessionActive?: boolean;
  readonly decodedFrameCount?: number;
  readonly rejectedFrameCount?: number;
  readonly scannerState?: MobileAnimatedPairingScannerState;
}

export const iosAnimatedPairingCameraGuidance = 'Keep phone pointed at screen until pairing is complete.';
export const iosAnimatedPairingVerificationPolicy = 'complete-payload verification before daemon import';

export function buildIOSAnimatedPairingCameraCaptureState(
  options: IOSAnimatedPairingCameraCaptureOptions = {}
): IOSAnimatedPairingCameraCaptureState {
  const scannerState = options.scannerState ?? createMobileAnimatedPairingScannerState('ios');
  const cameraAuthorization = options.cameraAuthorization ?? 'prompt-required';
  const scannerScreen = buildMobileAnimatedPairingScannerScreen('ios', scannerState, {
    waitingForCameraPermission: cameraAuthorization !== 'authorized',
  });
  return {
    cameraAuthorization,
    cameraSessionActive: options.cameraSessionActive ?? cameraAuthorization === 'authorized',
    avFoundationCaptureSessionRequired: true,
    decodedFrameCount: options.decodedFrameCount ?? scannerState.progress.collectedFrameCount,
    rejectedFrameCount: options.rejectedFrameCount ?? 0,
    scannerState,
    scannerScreen,
    progressText: scannerScreen.progressText,
    completePayloadVerification: 'required-before-import',
    secureLocalStorageTarget: 'ios-keychain',
  };
}

export function handleIOSAnimatedPairingCameraFrame(
  state: IOSAnimatedPairingCameraCaptureState,
  frame: MobileAnimatedPairingFrame
): IOSAnimatedPairingCameraCaptureState {
  if (state.cameraAuthorization !== 'authorized' || !state.cameraSessionActive) {
    return buildIOSAnimatedPairingCameraCaptureState({
      ...state,
      rejectedFrameCount: state.rejectedFrameCount + 1,
    });
  }
  return buildIOSAnimatedPairingCameraCaptureState({
    ...state,
    scannerState: addMobileAnimatedPairingFrame(state.scannerState, frame),
    decodedFrameCount: state.decodedFrameCount + 1,
  });
}

export function buildIOSMobileShellState(options: IOSMobileShellOptions = {}): IOSMobileShellState {
  const degradedReasons: MobileDegradedSyncReason[] = [
    ...(options.backgroundRefreshEnabled === false ? [{ cause: 'background-refresh' as const, message: 'Background App Refresh is disabled; iOS sync will run only during foreground or permitted wake windows.', actionRequired: true }] : []),
    ...(options.notificationsGranted === false ? [{ cause: 'notification-permission' as const, message: 'Notification permission is not granted for user-visible sync status and push-assisted wakeups.', actionRequired: true }] : []),
    ...(options.lowPowerModeEnabled === true ? [{ cause: 'battery-state' as const, message: 'Low Power Mode may defer background tasks and network work.', actionRequired: false }] : []),
    ...(options.cellularSyncDisabled === true ? [{ cause: 'user-settings' as const, message: 'Mobile cellular sync is disabled by user settings.', actionRequired: false }] : []),
    ...((options.silentPushWakeSupported === false || options.silentPushWakeSupported === undefined) ? [{ cause: 'ios-policy' as const, message: 'Silent push wake support is unavailable; iOS background sync may be opportunistic.', actionRequired: false }] : []),
    ...(options.degradedStatusReasons ?? []).map((message) => ({ cause: 'network-state' as const, message, actionRequired: false })),
  ];
  const degradedStatusReasons = degradedReasons.map((reason) => reason.message);
  const degradedSyncStatus = buildMobileDegradedSyncStatus(degradedReasons);
  const permissionPlan = buildMobilePermissionPlan({
    platform: 'ios',
    localFolderSyncEnabled: options.localFolderSyncEnabled ?? false,
    remoteInstanceManagementEnabled: options.remoteInstanceManagementEnabled ?? true,
    discoveryEnabled: options.discoveryEnabled ?? true,
    transferNotificationsEnabled: options.transferNotificationsEnabled ?? true,
    continuousBackgroundSyncEnabled: options.continuousBackgroundSyncEnabled ?? false,
    backgroundSyncEnabled: options.backgroundSyncEnabled ?? true,
    requireLocationForWifiNetworkState: options.requireLocationForWifiNetworkState ?? false,
  });

  return {
    platform: 'ios',
    selectedHostID: options.selectedHostID ?? 'local',
    activeView: options.activeView ?? 'overview',
    navigation: buildMobileNavigationModel(),
    featureParity: {
      platform: 'ios',
      localEncryptedAPIOnly: true,
      remoteManagementParity: true,
      identityPairingImportExport: true,
      animatedPairingScanner: true,
      secureCredentialStoreRequired: true,
      cellularSyncDisableSetting: true,
      degradedStatusRequired: true,
      mobileOperationalAPIClient: true,
    },
    backgroundCapability: {
      status: degradedStatusReasons.length > 0 ? 'degraded' : 'ready',
      backgroundTasksRequired: true,
      backgroundURLSessionRequired: true,
      silentPushWakeSupported: options.silentPushWakeSupported ?? false,
      shortWakeWindowCheckpointing: true,
      cellularSyncDisabled: options.cellularSyncDisabled ?? false,
      degradedStatusReasons,
    },
    degradedSyncStatus,
    permissionPlan,
    remoteInstanceOnboardingSources: mobileRemoteInstanceOnboardingSources,
    discoveredSameIdentityInstances: options.sameIdentityDiscoverySnapshot
      ? autoPopulateMobileSameIdentityInstances('ios', options.sameIdentityDiscoverySnapshot)
      : [],
    mobileMeshRelayStatus: buildMobileMeshRelayRoute({
      selectedHostID: options.selectedHostID ?? 'local',
      directReachable: options.selectedHostDirectReachable ?? true,
      relayReachable: options.selectedHostRelayReachable ?? false,
      relayPeerIDs: options.selectedHostRelayPeerIDs ?? [],
    }),
    secureCredentialStore: 'ios-keychain',
  };
}
