import type { MobilePairingSecureStorageTarget, MobilePlatform } from './mobileAppContract';

export type MobileAnimatedPairingFrame = {
  readonly version: 'fse-animated-pairing-v1';
  readonly sessionId: string;
  readonly frameIndex: number;
  readonly frameCount: number;
  readonly payloadSha256: string;
  readonly payloadByteLength: number;
  readonly fragmentBase64: string;
  readonly frameKind: 'data' | 'parity';
  readonly dataFrameCount: number;
  readonly parityFrameCount: number;
};

export type MobileAnimatedPairingScanProgress = {
  readonly status: 'collecting' | 'ready-to-verify' | 'verified';
  readonly collectedFrameCount: number;
  readonly requiredFrameCount: number;
  readonly totalFrameCount: number;
  readonly duplicateFrameCount: number;
  readonly collectedFrameIndexes: readonly number[];
  readonly missingFrameIndexes: readonly number[];
  readonly progressLabel: string;
  readonly guidance: 'Keep phone pointed at screen until pairing is complete.';
};

export type MobileAnimatedPairingScannerState = {
  readonly platform: MobilePlatform;
  readonly frames: readonly MobileAnimatedPairingFrame[];
  readonly progress: MobileAnimatedPairingScanProgress;
  readonly completePayloadVerificationRequired: true;
  readonly secureLocalStorageTarget: MobilePairingSecureStorageTarget;
};

export type MobileAnimatedPairingImportResult = {
  readonly readyForDaemonImport: boolean;
  readonly completePayloadVerification: 'required-before-import';
  readonly secureLocalStorageTarget: MobilePairingSecureStorageTarget;
  readonly progress: MobileAnimatedPairingScanProgress;
  readonly redactedPayloadReference?: string;
};

export function createMobileAnimatedPairingScannerState(platform: MobilePlatform): MobileAnimatedPairingScannerState {
  return buildMobileAnimatedPairingScannerState(platform, [], 0);
}

export function addMobileAnimatedPairingFrame(
  state: MobileAnimatedPairingScannerState,
  frame: MobileAnimatedPairingFrame
): MobileAnimatedPairingScannerState {
  validateMobileAnimatedPairingFrame(frame, state.frames[0] ?? frame);
  if (state.frames.some((existing) => existing.frameIndex === frame.frameIndex)) {
    return buildMobileAnimatedPairingScannerState(state.platform, state.frames, state.progress.duplicateFrameCount + 1);
  }
  return buildMobileAnimatedPairingScannerState(state.platform, [...state.frames, frame], state.progress.duplicateFrameCount);
}

export function completeMobileAnimatedPairingImport(state: MobileAnimatedPairingScannerState): MobileAnimatedPairingImportResult {
  const readyForDaemonImport = state.progress.status !== 'collecting';
  return {
    readyForDaemonImport,
    completePayloadVerification: 'required-before-import',
    secureLocalStorageTarget: state.secureLocalStorageTarget,
    progress: state.progress,
    redactedPayloadReference: readyForDaemonImport ? `verified-session:${state.frames[0]?.sessionId ?? 'unknown'}` : undefined,
  };
}

function buildMobileAnimatedPairingScannerState(
  platform: MobilePlatform,
  frames: readonly MobileAnimatedPairingFrame[],
  duplicateFrameCount: number
): MobileAnimatedPairingScannerState {
  const ordered = [...frames].sort((left, right) => left.frameIndex - right.frameIndex);
  const first = ordered[0];
  const collectedFrameIndexes = ordered.map((frame) => frame.frameIndex);
  const collected = new Set(collectedFrameIndexes);
  const totalFrameCount = first?.frameCount ?? 0;
  const requiredFrameCount = first?.dataFrameCount ?? 1;
  const missingFrameIndexes = Array.from({ length: totalFrameCount }, (_, index) => index).filter((index) => !collected.has(index));
  const status: MobileAnimatedPairingScanProgress['status'] = first && ordered.length >= first.dataFrameCount
    ? 'ready-to-verify'
    : 'collecting';
  const needed = Math.max(0, requiredFrameCount - ordered.length);
  return {
    platform,
    frames: ordered,
    completePayloadVerificationRequired: true,
    secureLocalStorageTarget: platform === 'android' ? 'android-keystore' : 'ios-keychain',
    progress: {
      status,
      collectedFrameCount: ordered.length,
      requiredFrameCount,
      totalFrameCount,
      duplicateFrameCount,
      collectedFrameIndexes,
      missingFrameIndexes,
      progressLabel: status === 'collecting'
        ? `Keep phone pointed at screen until pairing is complete. Collected ${ordered.length} of ${requiredFrameCount} required unique frames; need ${needed} more.`
        : `Collected ${ordered.length} of ${totalFrameCount} frames; enough unique frames are available for complete-payload verification.`,
      guidance: 'Keep phone pointed at screen until pairing is complete.',
    },
  };
}

function validateMobileAnimatedPairingFrame(frame: MobileAnimatedPairingFrame, first: MobileAnimatedPairingFrame): void {
  if (frame.version !== 'fse-animated-pairing-v1') {
    throw new Error('mobile animated pairing frame version is unsupported');
  }
  if (
    frame.sessionId !== first.sessionId ||
    frame.frameCount !== first.frameCount ||
    frame.payloadSha256 !== first.payloadSha256 ||
    frame.payloadByteLength !== first.payloadByteLength ||
    frame.dataFrameCount !== first.dataFrameCount ||
    frame.parityFrameCount !== first.parityFrameCount
  ) {
    throw new Error('mobile animated pairing frames do not belong to the same session');
  }
  if (frame.frameCount !== frame.dataFrameCount + frame.parityFrameCount) {
    throw new Error('mobile animated pairing frame count must equal data plus parity frames');
  }
  if (!Number.isInteger(frame.frameIndex) || frame.frameIndex < 0 || frame.frameIndex >= frame.frameCount) {
    throw new Error('mobile animated pairing frame index is out of range');
  }
  const expectedKind = frame.frameIndex < frame.dataFrameCount ? 'data' : 'parity';
  if (frame.frameKind !== expectedKind) {
    throw new Error('mobile animated pairing frame kind does not match its index');
  }
  if (frame.fragmentBase64.trim() === '') {
    throw new Error('mobile animated pairing frame fragment is required');
  }
}
