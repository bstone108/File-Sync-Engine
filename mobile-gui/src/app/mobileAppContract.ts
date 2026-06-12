export type MobilePlatform = 'android' | 'ios';

export type MobileViewID =
  | 'overview'
  | 'folders'
  | 'peers-identity'
  | 'transfers'
  | 'warnings-logs'
  | 'maintenance-backups'
  | 'daemon-settings'
  | 'mobile-app-settings'
  | 'help-details';

export interface MobileFeatureParityContract {
  readonly platform: MobilePlatform;
  readonly localEncryptedAPIOnly: true;
  readonly remoteManagementParity: true;
  readonly identityPairingImportExport: true;
  readonly animatedPairingScanner: true;
  readonly secureCredentialStoreRequired: true;
  readonly cellularSyncDisableSetting: true;
  readonly degradedStatusRequired: true;
  readonly mobileOperationalAPIClient: true;
}

export const mobileOperationalAPIClientScope = 'folders, peers, discovery, transfers, warnings/logs, maintenance, backups, and settings';

export interface MobileNavigationItem {
  readonly id: MobileViewID;
  readonly label: string;
  readonly selectedHostScoped: boolean;
}

export type MobilePairingSecureStorageTarget = 'android-keystore' | 'ios-keychain';

export type MobileIdentityPackageExportPresentation = {
  readonly copyableText: true;
  readonly downloadableIdentityFile: true;
  readonly shareSheetPayload: true;
  readonly qrFallbackPayload: true;
  readonly animatedPairingFrames: true;
};

export type MobileIdentityPackageImportSource =
  | 'pastedText'
  | 'uploadedIdentityFile'
  | 'scannedAnimatedCode'
  | 'sharedIdentityFile';

export interface MobileIdentityImportReadiness {
  readonly source: MobileIdentityPackageImportSource;
  readonly readyForDaemonImport: boolean;
  readonly requiresEncryptedAPI: true;
  readonly secureLocalStorageAfterImport: MobilePairingSecureStorageTarget;
  readonly progressMessage: string;
}

export function buildMobileIdentityPairingActions(platform: MobilePlatform): MobileIdentityPackageExportPresentation & { readonly secureLocalStorageAfterImport: MobilePairingSecureStorageTarget } {
  return {
    copyableText: true,
    downloadableIdentityFile: true,
    shareSheetPayload: true,
    qrFallbackPayload: true,
    animatedPairingFrames: true,
    secureLocalStorageAfterImport: platform === 'android' ? 'android-keystore' : 'ios-keychain',
  };
}

export function parseMobileIdentityImportReadiness(source: MobileIdentityPackageImportSource, platform: MobilePlatform, payload: string): MobileIdentityImportReadiness {
  return {
    source,
    readyForDaemonImport: payload.trim().length > 0,
    requiresEncryptedAPI: true,
    secureLocalStorageAfterImport: platform === 'android' ? 'android-keystore' : 'ios-keychain',
    progressMessage: payload.trim().length > 0 ? 'Ready for daemon-owned identity import.' : 'Waiting for pairing payload.',
  };
}

export const mobileFeatureParityContracts: readonly MobileFeatureParityContract[] = [
  {
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
  {
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
];

export function buildMobileNavigationModel(): readonly MobileNavigationItem[] {
  return [
    { id: 'overview', label: 'Overview', selectedHostScoped: true },
    { id: 'folders', label: 'Folders', selectedHostScoped: true },
    { id: 'peers-identity', label: 'Peers & identity', selectedHostScoped: true },
    { id: 'transfers', label: 'Transfers', selectedHostScoped: true },
    { id: 'warnings-logs', label: 'Warnings & logs', selectedHostScoped: true },
    { id: 'maintenance-backups', label: 'Maintenance & backups', selectedHostScoped: true },
    { id: 'daemon-settings', label: 'Daemon settings', selectedHostScoped: true },
    { id: 'mobile-app-settings', label: 'Mobile app settings', selectedHostScoped: false },
    { id: 'help-details', label: 'Help & details', selectedHostScoped: false },
  ];
}
