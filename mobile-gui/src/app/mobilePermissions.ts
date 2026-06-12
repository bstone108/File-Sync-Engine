export type MobilePermissionPlatform = 'android' | 'ios';

export type MobilePlatformPermissionID =
  | 'android-notifications'
  | 'android-manage-external-storage'
  | 'android-nearby-wifi-devices'
  | 'android-location-for-wifi-ssid'
  | 'android-foreground-service-data-sync'
  | 'android-ignore-battery-optimizations'
  | 'ios-user-notifications'
  | 'ios-local-network'
  | 'ios-background-app-refresh'
  | 'ios-background-processing'
  | 'ios-background-url-session'
  | 'ios-location-when-in-use-for-network-state';

export interface MobilePermissionPlanOptions {
  readonly platform: MobilePermissionPlatform;
  readonly localFolderSyncEnabled: boolean;
  readonly remoteInstanceManagementEnabled: boolean;
  readonly discoveryEnabled: boolean;
  readonly transferNotificationsEnabled: boolean;
  readonly continuousBackgroundSyncEnabled: boolean;
  readonly backgroundSyncEnabled: boolean;
  readonly requireLocationForWifiNetworkState: boolean;
}

export interface MobilePermissionRequirement {
  readonly id: MobilePlatformPermissionID;
  readonly platform: MobilePermissionPlatform;
  readonly reason: string;
  readonly userFacingExplanation: string;
  readonly requiredBeforeScheduling: boolean;
}

export interface MobilePermissionPlan {
  readonly platform: MobilePermissionPlatform;
  readonly permissions: readonly MobilePermissionRequirement[];
  readonly requestOrder: readonly MobilePlatformPermissionID[];
  readonly notes: readonly string[];
}

export function buildMobilePermissionPlan(options: MobilePermissionPlanOptions): MobilePermissionPlan {
  const permissions = options.platform === 'android'
    ? buildAndroidPermissionRequirements(options)
    : buildIOSPermissionRequirements(options);
  return {
    platform: options.platform,
    permissions,
    requestOrder: permissions.map((permission) => permission.id),
    notes: [
      'Request only permissions required by configured sync options.',
      ...(options.requireLocationForWifiNetworkState ? ['Location permission is requested only when the platform requires it to detect Wi-Fi/network state.'] : []),
      ...(options.continuousBackgroundSyncEnabled ? ['Continuous background sync requires visible background capability and user-facing degraded-status handling.'] : []),
    ],
  };
}

function buildAndroidPermissionRequirements(options: MobilePermissionPlanOptions): MobilePermissionRequirement[] {
  const permissions: MobilePermissionRequirement[] = [];
  if (options.localFolderSyncEnabled) {
    permissions.push({
      id: 'android-manage-external-storage',
      platform: 'android',
      reason: 'local-folder-sync',
      userFacingExplanation: 'Allow access to selected folders so the local engine can synchronize the files you choose.',
      requiredBeforeScheduling: true,
    });
  }
  if (options.discoveryEnabled || options.remoteInstanceManagementEnabled) {
    permissions.push({
      id: 'android-nearby-wifi-devices',
      platform: 'android',
      reason: 'local-network-discovery',
      userFacingExplanation: 'Allow nearby Wi-Fi device discovery so local peers and managed daemons can be found on trusted networks.',
      requiredBeforeScheduling: false,
    });
  }
  if (options.requireLocationForWifiNetworkState) {
    permissions.push({
      id: 'android-location-for-wifi-ssid',
      platform: 'android',
      reason: 'wifi-network-state',
      userFacingExplanation: 'Allow location only when Android requires it to read Wi-Fi/network state for sync policy decisions.',
      requiredBeforeScheduling: false,
    });
  }
  if (options.transferNotificationsEnabled || options.continuousBackgroundSyncEnabled) {
    permissions.push({
      id: 'android-notifications',
      platform: 'android',
      reason: 'user-visible-sync-status',
      userFacingExplanation: 'Allow notifications so active sync can show foreground status, pause/stop controls, and warnings.',
      requiredBeforeScheduling: options.continuousBackgroundSyncEnabled,
    });
  }
  if (options.backgroundSyncEnabled || options.continuousBackgroundSyncEnabled) {
    permissions.push({
      id: 'android-foreground-service-data-sync',
      platform: 'android',
      reason: 'foreground-data-sync-service',
      userFacingExplanation: 'Allow foreground data sync service operation for user-visible long-running transfers.',
      requiredBeforeScheduling: true,
    });
  }
  if (options.continuousBackgroundSyncEnabled) {
    permissions.push({
      id: 'android-ignore-battery-optimizations',
      platform: 'android',
      reason: 'reliable-continuous-background-sync',
      userFacingExplanation: 'Allow the app to request battery optimization exemption only when continuous background sync is enabled.',
      requiredBeforeScheduling: false,
    });
  }
  return permissions;
}

function buildIOSPermissionRequirements(options: MobilePermissionPlanOptions): MobilePermissionRequirement[] {
  const permissions: MobilePermissionRequirement[] = [];
  if (options.transferNotificationsEnabled || options.backgroundSyncEnabled) {
    permissions.push({
      id: 'ios-user-notifications',
      platform: 'ios',
      reason: 'user-visible-sync-status-and-wakeups',
      userFacingExplanation: 'Allow notifications for sync status, warnings, and permitted push-assisted wake flows.',
      requiredBeforeScheduling: false,
    });
  }
  if (options.discoveryEnabled || options.remoteInstanceManagementEnabled) {
    permissions.push({
      id: 'ios-local-network',
      platform: 'ios',
      reason: 'local-network-discovery',
      userFacingExplanation: 'Allow local network access so the app can discover and manage daemons on trusted networks.',
      requiredBeforeScheduling: false,
    });
  }
  if (options.backgroundSyncEnabled || options.continuousBackgroundSyncEnabled) {
    permissions.push({
      id: 'ios-background-app-refresh',
      platform: 'ios',
      reason: 'background-refresh-opportunity',
      userFacingExplanation: 'Enable Background App Refresh so iOS can offer documented sync wake opportunities.',
      requiredBeforeScheduling: false,
    });
    permissions.push({
      id: 'ios-background-processing',
      platform: 'ios',
      reason: 'background-processing-opportunity',
      userFacingExplanation: 'Allow background processing tasks where iOS policy permits longer maintenance or sync work.',
      requiredBeforeScheduling: false,
    });
    permissions.push({
      id: 'ios-background-url-session',
      platform: 'ios',
      reason: 'resumable-background-transfer',
      userFacingExplanation: 'Allow background URLSession transfers for resumable chunks where iOS policy permits them.',
      requiredBeforeScheduling: false,
    });
  }
  if (options.requireLocationForWifiNetworkState) {
    permissions.push({
      id: 'ios-location-when-in-use-for-network-state',
      platform: 'ios',
      reason: 'wifi-network-state',
      userFacingExplanation: 'Request When In Use location only if iOS requires it to identify Wi-Fi/network state for sync policy.',
      requiredBeforeScheduling: false,
    });
  }
  return permissions;
}
