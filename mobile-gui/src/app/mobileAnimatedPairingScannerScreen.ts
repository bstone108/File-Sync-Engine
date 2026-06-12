import type { MobilePlatform } from './mobileAppContract';
import type { MobileAnimatedPairingScannerState } from './mobileAnimatedPairingScanner';

export type MobileAnimatedPairingScannerScreenStatus = 'waiting-for-camera' | 'collecting' | 'ready-to-verify' | 'complete' | 'error';

export type MobileAnimatedPairingScannerScreenModel = {
  readonly platform: MobilePlatform;
  readonly title: string;
  readonly status: MobileAnimatedPairingScannerScreenStatus;
  readonly guidance: 'Keep phone pointed at screen until pairing is complete.';
  readonly progressText: string;
  readonly progressPercent: number;
  readonly collectedFrameCount: number;
  readonly requiredFrameCount: number;
  readonly totalFrameCount: number;
  readonly missingFrameIndexes: readonly number[];
  readonly duplicateFrameCount: number;
  readonly secureLocalStorageTarget: MobileAnimatedPairingScannerState['secureLocalStorageTarget'];
  readonly primaryActionLabel: string;
  readonly secondaryActionLabel: string;
  readonly completionMessage?: 'Pairing is complete; connectivity and authorization will continue in the background.';
  readonly errorMessage?: string;
};

export interface MobileAnimatedPairingScannerScreenOptions {
  readonly waitingForCameraPermission?: boolean;
  readonly errorMessage?: string;
  readonly importCompleted?: boolean;
}

export function buildMobileAnimatedPairingScannerScreen(
  platform: MobilePlatform,
  scannerState: MobileAnimatedPairingScannerState,
  options: MobileAnimatedPairingScannerScreenOptions = {}
): MobileAnimatedPairingScannerScreenModel {
  const progress = scannerState.progress;
  const totalForPercent = Math.max(progress.requiredFrameCount, 1);
  const progressPercent = clampPercent(Math.round((progress.collectedFrameCount / totalForPercent) * 100));
  const status = screenStatus(scannerState, options);
  const completionMessage = status === 'complete'
    ? 'Pairing is complete; connectivity and authorization will continue in the background.'
    : undefined;

  return {
    platform,
    title: platform === 'android' ? 'Scan animated pairing code' : 'Scan animated pairing code',
    status,
    guidance: 'Keep phone pointed at screen until pairing is complete.',
    progressText: progressTextForStatus(status, scannerState, options.errorMessage),
    progressPercent,
    collectedFrameCount: progress.collectedFrameCount,
    requiredFrameCount: progress.requiredFrameCount,
    totalFrameCount: progress.totalFrameCount,
    missingFrameIndexes: progress.missingFrameIndexes,
    duplicateFrameCount: progress.duplicateFrameCount,
    secureLocalStorageTarget: scannerState.secureLocalStorageTarget,
    primaryActionLabel: primaryActionLabel(status),
    secondaryActionLabel: 'Cancel scanning',
    completionMessage,
    errorMessage: options.errorMessage,
  };
}

function screenStatus(
  scannerState: MobileAnimatedPairingScannerState,
  options: MobileAnimatedPairingScannerScreenOptions
): MobileAnimatedPairingScannerScreenStatus {
  if (options.errorMessage) {
    return 'error';
  }
  if (options.importCompleted || scannerState.progress.status === 'verified') {
    return 'complete';
  }
  if (options.waitingForCameraPermission) {
    return 'waiting-for-camera';
  }
  if (scannerState.progress.status === 'ready-to-verify') {
    return 'ready-to-verify';
  }
  return 'collecting';
}

function progressTextForStatus(
  status: MobileAnimatedPairingScannerScreenStatus,
  scannerState: MobileAnimatedPairingScannerState,
  errorMessage?: string
): string {
  if (status === 'waiting-for-camera') {
    return 'Camera permission is needed before animated pairing frames can be collected.';
  }
  if (status === 'error') {
    return errorMessage ?? 'Animated pairing scan could not continue.';
  }
  if (status === 'complete') {
    return 'Pairing is complete; connectivity and authorization will continue in the background.';
  }
  if (status === 'ready-to-verify') {
    return `${scannerState.progress.progressLabel} Verify the complete payload, then store the accepted pairing material in ${scannerState.secureLocalStorageTarget}.`;
  }
  return scannerState.progress.progressLabel;
}

function primaryActionLabel(status: MobileAnimatedPairingScannerScreenStatus): string {
  switch (status) {
    case 'waiting-for-camera':
      return 'Grant camera access';
    case 'ready-to-verify':
      return 'Verify and finish pairing';
    case 'complete':
      return 'Done';
    case 'error':
      return 'Try again';
    case 'collecting':
    default:
      return 'Keep scanning';
  }
}

function clampPercent(value: number): number {
  if (value < 0) {
    return 0;
  }
  if (value > 100) {
    return 100;
  }
  return value;
}
