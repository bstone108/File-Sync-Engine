import {
  addAnimatedPairingFrameToScan,
  assembleAnimatedPairingFrames,
  createAnimatedPairingScanState,
  type AnimatedPairingFrame,
  type AnimatedPairingScanState,
  type IdentityPairingPackage
} from './identityPairing';

export type DesktopAnimatedPairingCameraPermissionState = 'unknown' | 'prompting' | 'granted' | 'denied' | 'unavailable';

export interface DesktopAnimatedPairingCameraCaptureState {
  cameraPermission: DesktopAnimatedPairingCameraPermissionState;
  decodedFrameCount: number;
  rejectedFrameCount: number;
  scannerState: AnimatedPairingScanState;
  lastError?: string;
}

export type DesktopAnimatedPairingScannerScreenModel = {
  title: 'Desktop animated pairing camera scanner';
  cameraPermission: DesktopAnimatedPairingCameraPermissionState;
  cameraPermissionMessage: string;
  collectedFrameCount: number;
  requiredFrameCount: number;
  totalFrameCount: number;
  duplicateFrameCount: number;
  decodedFrameCount: number;
  rejectedFrameCount: number;
  progressPercent: number;
  missingFrameIndexes: number[];
  guidance: string;
  readyForImport: boolean;
  completionMessage: string;
  errorMessage: string;
};

export function buildDesktopAnimatedPairingCameraCaptureState(input: Partial<DesktopAnimatedPairingCameraCaptureState> = {}): DesktopAnimatedPairingCameraCaptureState {
  return {
    cameraPermission: input.cameraPermission ?? 'unknown',
    decodedFrameCount: input.decodedFrameCount ?? 0,
    rejectedFrameCount: input.rejectedFrameCount ?? 0,
    scannerState: input.scannerState ?? createAnimatedPairingScanState(),
    lastError: input.lastError
  };
}

export function handleDesktopAnimatedPairingCameraFrame(state: DesktopAnimatedPairingCameraCaptureState, frameText: string): DesktopAnimatedPairingCameraCaptureState {
  try {
    const parsed = JSON.parse(frameText) as AnimatedPairingFrame;
    return {
      ...state,
      cameraPermission: state.cameraPermission === 'unknown' ? 'granted' : state.cameraPermission,
      decodedFrameCount: state.decodedFrameCount + 1,
      scannerState: addAnimatedPairingFrameToScan(state.scannerState, parsed),
      lastError: undefined
    };
  } catch (error) {
    return {
      ...state,
      rejectedFrameCount: state.rejectedFrameCount + 1,
      lastError: error instanceof Error ? error.message : String(error)
    };
  }
}

export async function completeDesktopAnimatedPairingCameraImport(state: DesktopAnimatedPairingCameraCaptureState): Promise<IdentityPairingPackage> {
  // complete-payload verification before daemon-owned import: the desktop camera scanner
  // must reconstruct and checksum-verify the full identity package locally before sending
  // the parsed package to the daemon-owned identity import endpoint.
  if (state.scannerState.progress.status === 'collecting') {
    throw new Error('animated pairing camera scanner needs more unique frames before import');
  }
  return assembleAnimatedPairingFrames(state.scannerState.frames);
}

export function buildDesktopAnimatedPairingScannerScreen(state: DesktopAnimatedPairingCameraCaptureState): DesktopAnimatedPairingScannerScreenModel {
  const progress = state.scannerState.progress;
  const required = Math.max(1, progress.requiredFrameCount);
  const progressPercent = Math.min(100, Math.round((progress.collectedFrameCount / required) * 100));
  const readyForImport = progress.status === 'ready' || progress.status === 'complete';
  return {
    title: 'Desktop animated pairing camera scanner',
    cameraPermission: state.cameraPermission,
    cameraPermissionMessage: desktopCameraPermissionMessage(state.cameraPermission),
    collectedFrameCount: progress.collectedFrameCount,
    requiredFrameCount: progress.requiredFrameCount,
    totalFrameCount: progress.totalFrameCount,
    duplicateFrameCount: progress.duplicateFrameCount,
    decodedFrameCount: state.decodedFrameCount,
    rejectedFrameCount: state.rejectedFrameCount,
    progressPercent,
    missingFrameIndexes: progress.missingFrameIndexes,
    guidance: readyForImport
      ? 'Enough frames are available for complete-payload verification before daemon-owned import.'
      : 'Keep camera pointed at the animated code until pairing is complete.',
    readyForImport,
    completionMessage: readyForImport
      ? 'Pairing payload is ready to verify and import; connectivity and authorization can continue silently after import.'
      : '',
    errorMessage: state.lastError ?? ''
  };
}

function desktopCameraPermissionMessage(permission: DesktopAnimatedPairingCameraPermissionState): string {
  switch (permission) {
    case 'granted':
      return 'Camera permission granted; scan animated pairing frames until complete.';
    case 'prompting':
      return 'Requesting camera permission for the desktop animated pairing camera scanner.';
    case 'denied':
      return 'Camera permission denied; paste or upload an identity file instead.';
    case 'unavailable':
      return 'Camera unavailable; use copyable text, downloaded identity file, or QR fallback instead.';
    default:
      return 'Camera permission has not been requested yet.';
  }
}
