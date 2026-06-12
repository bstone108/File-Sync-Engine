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

export type AndroidBackgroundCapabilityStatus = 'ready' | 'degraded' | 'blocked';

export interface AndroidBackgroundCapability {
  readonly status: AndroidBackgroundCapabilityStatus;
  readonly foregroundServiceRequired: true;
  readonly workManagerDeferredSyncRequired: true;
  readonly batteryOptimizationExemptionPrompt: boolean;
  readonly notificationPermissionRequired: boolean;
  readonly cellularSyncDisabled: boolean;
  readonly degradedStatusReasons: readonly string[];
}

export type AndroidMobileShellState = {
  readonly platform: 'android';
  readonly selectedHostID: string;
  readonly activeView: MobileViewID;
  readonly navigation: ReturnType<typeof buildMobileNavigationModel>;
  readonly featureParity: MobileFeatureParityContract;
  readonly backgroundCapability: AndroidBackgroundCapability;
  readonly degradedSyncStatus: MobileDegradedSyncStatus;
  readonly permissionPlan: MobilePermissionPlan;
  readonly remoteInstanceOnboardingSources: readonly MobileRemoteInstanceOnboardingSource[];
  readonly discoveredSameIdentityInstances: readonly MobileRemoteInstanceRecord[];
  readonly mobileMeshRelayStatus: MobileMeshRelayRoute;
  readonly secureCredentialStore: 'android-keystore';
};

export const androidMobileShellViewIDs: readonly MobileViewID[] = [
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

export const androidPermissionPlanKeyRequirements = [
  'android-location-for-wifi-ssid',
  'android-ignore-battery-optimizations',
] as const;

export const androidRemoteInstanceOnboardingSourceRequirements = [
  'direct-api-endpoint-key',
  'pasted-pairing-code',
  'scanned-animated-pairing-code',
  'shared-identity-file',
] as const;

export const androidMobileMeshRelayRouteKinds = ['direct-encrypted-api', 'identity-mesh-relay', 'offline-queued'] as const;

export interface AndroidMobileShellOptions {
  readonly selectedHostID?: string;
  readonly activeView?: MobileViewID;
  readonly notificationsGranted?: boolean;
  readonly batteryOptimizationExempt?: boolean;
  readonly cellularSyncDisabled?: boolean;
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

export type AndroidBackgroundSyncWorkKind =
  | 'active-folder-sync'
  | 'metadata-catchup'
  | 'small-pending-transfer'
  | 'scrub-repair-check'
  | 'retry-after-connectivity';

export type AndroidBackgroundSyncSchedule = 'foreground-service' | 'workmanager-deferred' | 'blocked-by-cellular-policy' | 'blocked-by-permission';

export type AndroidBatteryOptimizationExemptionState = 'exempt' | 'prompt-needed' | 'user-declined' | 'os-refused' | 'not-needed';
export type AndroidBatteryOptimizationPromptReason = 'continuous-background-sync';
export type AndroidBatteryOptimizationPromptAction = 'request-action-manage-ignore-battery-optimizations' | 'show-degraded-status-only';

export interface AndroidBatteryOptimizationExemptionPrompt {
  readonly state: AndroidBatteryOptimizationExemptionState;
  readonly reason: AndroidBatteryOptimizationPromptReason;
  readonly action: AndroidBatteryOptimizationPromptAction;
  readonly shouldRequestExemption: boolean;
  readonly explanation: string;
  readonly degradedStatusReason?: string;
}

export type AndroidNetworkType = 'wifi' | 'ethernet' | 'cellular' | 'offline' | 'unknown';

export interface AndroidBackgroundSyncRequest {
  readonly workKind: AndroidBackgroundSyncWorkKind;
  readonly networkType: AndroidNetworkType;
  readonly metered: boolean;
  readonly cellularSyncDisabled: boolean;
  readonly notificationsGranted: boolean;
  readonly batteryOptimizationExempt: boolean;
  readonly chargingRequired?: boolean;
}

export type AndroidBackgroundSyncDecision = {
  readonly schedule: AndroidBackgroundSyncSchedule;
  readonly workKind: AndroidBackgroundSyncWorkKind;
  readonly networkPolicy: MobileSyncNetworkPolicy;
  readonly requiresUserVisibleNotification: boolean;
  readonly requiresBatteryOptimizationExemptionPrompt: boolean;
  readonly chargingRequired: boolean;
  readonly durableCheckpointRequired: true;
  readonly degradedStatusReasons: readonly string[];
};

export function buildAndroidBatteryOptimizationExemptionPrompt(options: {
  readonly continuousBackgroundSyncEnabled: boolean;
  readonly batteryOptimizationExempt: boolean;
  readonly userDeclined?: boolean;
  readonly osRefused?: boolean;
}): AndroidBatteryOptimizationExemptionPrompt {
  const explanation = options.continuousBackgroundSyncEnabled
    ? 'Battery optimization exemption is recommended only when continuous/background sync is enabled. Explain that Android may pause sync while the app is idle unless battery optimization is exempted.'
    : 'Battery optimization exemption is recommended only when continuous/background sync is enabled.';
  if (!options.continuousBackgroundSyncEnabled) {
    return {
      state: 'not-needed',
      reason: 'continuous-background-sync',
      action: 'show-degraded-status-only',
      shouldRequestExemption: false,
      explanation,
    };
  }
  if (options.batteryOptimizationExempt) {
    return {
      state: 'exempt',
      reason: 'continuous-background-sync',
      action: 'show-degraded-status-only',
      shouldRequestExemption: false,
      explanation,
    };
  }
  const state: AndroidBatteryOptimizationExemptionState = options.osRefused
    ? 'os-refused'
    : options.userDeclined
      ? 'user-declined'
      : 'prompt-needed';
  return {
    state,
    reason: 'continuous-background-sync',
    action: state === 'prompt-needed' ? 'request-action-manage-ignore-battery-optimizations' : 'show-degraded-status-only',
    shouldRequestExemption: state === 'prompt-needed',
    explanation,
    degradedStatusReason: state === 'prompt-needed'
      ? 'Android battery optimization exemption should be requested before continuous background sync is enabled.'
      : 'Battery optimization exemption was not granted; background sync remains degraded until the OS and user allow it.',
  };
}

export function buildAndroidBackgroundSyncDecision(request: AndroidBackgroundSyncRequest): AndroidBackgroundSyncDecision {
  const networkPolicy = buildMobileSyncNetworkPolicy(request.cellularSyncDisabled, request.networkType);
  const cellularBlocked = shouldBlockMobileSyncForNetwork(request.cellularSyncDisabled, request.networkType);
  const permissionBlocked = request.workKind === 'active-folder-sync' && !request.notificationsGranted;
  const schedule: AndroidBackgroundSyncSchedule = cellularBlocked
    ? 'blocked-by-cellular-policy'
    : permissionBlocked
      ? 'blocked-by-permission'
      : request.workKind === 'active-folder-sync'
        ? 'foreground-service'
        : 'workmanager-deferred';
  const degradedStatusReasons = [
    ...(cellularBlocked ? ['Cellular sync disable policy blocks mobile network work before scheduling.'] : []),
    ...(permissionBlocked ? ['Notification permission is required before active foreground sync can start.'] : []),
    ...(request.metered && !cellularBlocked ? ['Metered network detected; defer non-urgent work when policy requires it.'] : []),
    ...(!request.batteryOptimizationExempt ? ['Battery optimization exemption may be needed for reliable background sync.'] : []),
  ];

  return {
    schedule,
    workKind: request.workKind,
    networkPolicy,
    requiresUserVisibleNotification: request.workKind === 'active-folder-sync',
    requiresBatteryOptimizationExemptionPrompt: !request.batteryOptimizationExempt,
    chargingRequired: request.chargingRequired ?? false,
    durableCheckpointRequired: true,
    degradedStatusReasons,
  };
}

export type AndroidAnimatedPairingCameraPermissionState = 'unknown' | 'prompt-required' | 'granted' | 'denied';

export interface AndroidAnimatedPairingCameraCaptureState {
  readonly cameraPermission: AndroidAnimatedPairingCameraPermissionState;
  readonly cameraPreviewActive: boolean;
  readonly decodedFrameCount: number;
  readonly rejectedFrameCount: number;
  readonly scannerState: MobileAnimatedPairingScannerState;
  readonly scannerScreen: MobileAnimatedPairingScannerScreenModel;
  readonly progressText: string;
  readonly completePayloadVerification: 'required-before-import';
  readonly secureLocalStorageTarget: 'android-keystore';
}

export interface AndroidAnimatedPairingCameraCaptureOptions {
  readonly cameraPermission?: AndroidAnimatedPairingCameraPermissionState;
  readonly cameraPreviewActive?: boolean;
  readonly decodedFrameCount?: number;
  readonly rejectedFrameCount?: number;
  readonly scannerState?: MobileAnimatedPairingScannerState;
}

export const androidAnimatedPairingCameraGuidance = 'Keep phone pointed at screen until pairing is complete.';
export const androidAnimatedPairingVerificationPolicy = 'complete-payload verification before daemon import';

export function buildAndroidAnimatedPairingCameraCaptureState(
  options: AndroidAnimatedPairingCameraCaptureOptions = {}
): AndroidAnimatedPairingCameraCaptureState {
  const scannerState = options.scannerState ?? createMobileAnimatedPairingScannerState('android');
  const cameraPermission = options.cameraPermission ?? 'prompt-required';
  const scannerScreen = buildMobileAnimatedPairingScannerScreen('android', scannerState, {
    waitingForCameraPermission: cameraPermission !== 'granted',
  });
  return {
    cameraPermission,
    cameraPreviewActive: options.cameraPreviewActive ?? cameraPermission === 'granted',
    decodedFrameCount: options.decodedFrameCount ?? scannerState.progress.collectedFrameCount,
    rejectedFrameCount: options.rejectedFrameCount ?? 0,
    scannerState,
    scannerScreen,
    progressText: scannerScreen.progressText,
    completePayloadVerification: 'required-before-import',
    secureLocalStorageTarget: 'android-keystore',
  };
}

export function handleAndroidAnimatedPairingCameraFrame(
  state: AndroidAnimatedPairingCameraCaptureState,
  frame: MobileAnimatedPairingFrame
): AndroidAnimatedPairingCameraCaptureState {
  if (state.cameraPermission !== 'granted' || !state.cameraPreviewActive) {
    return buildAndroidAnimatedPairingCameraCaptureState({
      ...state,
      rejectedFrameCount: state.rejectedFrameCount + 1,
    });
  }
  return buildAndroidAnimatedPairingCameraCaptureState({
    ...state,
    scannerState: addMobileAnimatedPairingFrame(state.scannerState, frame),
    decodedFrameCount: state.decodedFrameCount + 1,
  });
}

export function buildAndroidMobileShellState(options: AndroidMobileShellOptions = {}): AndroidMobileShellState {
  const degradedReasons: MobileDegradedSyncReason[] = [
    ...(options.notificationsGranted === false ? [{ cause: 'notification-permission' as const, message: 'Notification permission is required for foreground sync status.', actionRequired: true }] : []),
    ...(options.batteryOptimizationExempt === false ? [{ cause: 'battery-state' as const, message: 'Battery optimization exemption is not granted for continuous background sync.', actionRequired: true }] : []),
    ...(options.cellularSyncDisabled === true ? [{ cause: 'user-settings' as const, message: 'Mobile cellular sync is disabled by user settings.', actionRequired: false }] : []),
    ...(options.degradedStatusReasons ?? []).map((message) => ({ cause: 'network-state' as const, message, actionRequired: false })),
  ];
  const degradedStatusReasons = degradedReasons.map((reason) => reason.message);
  const degradedSyncStatus = buildMobileDegradedSyncStatus(degradedReasons);
  const permissionPlan = buildMobilePermissionPlan({
    platform: 'android',
    localFolderSyncEnabled: options.localFolderSyncEnabled ?? true,
    remoteInstanceManagementEnabled: options.remoteInstanceManagementEnabled ?? true,
    discoveryEnabled: options.discoveryEnabled ?? true,
    transferNotificationsEnabled: options.transferNotificationsEnabled ?? true,
    continuousBackgroundSyncEnabled: options.continuousBackgroundSyncEnabled ?? options.batteryOptimizationExempt !== true,
    backgroundSyncEnabled: options.backgroundSyncEnabled ?? true,
    requireLocationForWifiNetworkState: options.requireLocationForWifiNetworkState ?? false,
  });

  return {
    platform: 'android',
    selectedHostID: options.selectedHostID ?? 'local',
    activeView: options.activeView ?? 'overview',
    navigation: buildMobileNavigationModel(),
    featureParity: {
      platform: 'android',
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
      foregroundServiceRequired: true,
      workManagerDeferredSyncRequired: true,
      batteryOptimizationExemptionPrompt: options.batteryOptimizationExempt !== true,
      notificationPermissionRequired: options.notificationsGranted !== true,
      cellularSyncDisabled: options.cellularSyncDisabled ?? false,
      degradedStatusReasons,
    },
    degradedSyncStatus,
    permissionPlan,
    remoteInstanceOnboardingSources: mobileRemoteInstanceOnboardingSources,
    discoveredSameIdentityInstances: options.sameIdentityDiscoverySnapshot
      ? autoPopulateMobileSameIdentityInstances('android', options.sameIdentityDiscoverySnapshot)
      : [],
    mobileMeshRelayStatus: buildMobileMeshRelayRoute({
      selectedHostID: options.selectedHostID ?? 'local',
      directReachable: options.selectedHostDirectReachable ?? true,
      relayReachable: options.selectedHostRelayReachable ?? false,
      relayPeerIDs: options.selectedHostRelayPeerIDs ?? [],
    }),
    secureCredentialStore: 'android-keystore',
  };
}
