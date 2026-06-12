export type IdentityPairingPackage = {
  version: string;
  nodeName?: string;
  discoveryId: string;
  groupId: string;
  bootstrapProofKey: string;
  bootstrapEncryptionLevel: number;
  defaultPeerEncryptionLevel: number;
};

export type IdentityPairingDownload = {
  filename: string;
  mimeType: 'application/json';
  text: string;
};

export type IdentityPairingQRFallbackPayload = {
  format: 'fse-identity-qr-fallback-v1';
  mimeType: 'application/json';
  text: string;
  dataURL: string;
  securityNote: string;
};

export type IdentityPairingQRImageModel = {
  sourceURI: string;
  payloadDataURL: string;
  moduleGrid: boolean[][];
  moduleSizePixels: number;
  quietZoneModules: number;
  altText: string;
  guidance: string;
};

export type AnimatedPairingCodeDescriptor = {
  label: string;
  mode: 'animated visual code';
  qrFallback: 'QR code fallback';
  framePlan: string;
  securityNote: string;
};

export type AnimatedPairingFrame = {
  version: 'fse-animated-pairing-v1';
  sessionId: string;
  frameIndex: number;
  frameCount: number;
  payloadSha256: string;
  fragmentBase64: string;
  frameKind: 'data' | 'parity';
  dataFrameCount: number;
  parityFrameCount: number;
  payloadByteLength: number;
};

export type AnimatedPairingDensityProfile = {
  maxPayloadBytesPerFrame: number;
  minFrameDisplayMilliseconds: number;
  minimumVisualModuleSize: 'large-camera-friendly';
  guidance: string;
};

export type AnimatedPairingScanProgress = {
  status: 'collecting' | 'ready' | 'complete';
  collectedFrameCount: number;
  requiredFrameCount: number;
  totalFrameCount: number;
  duplicateFrameCount: number;
  collectedFrameIndexes: number[];
  missingFrameIndexes: number[];
  message: string;
};

export type AnimatedPairingScanState = {
  frames: AnimatedPairingFrame[];
  duplicateFrameCount: number;
  progress: AnimatedPairingScanProgress;
};

const identityPairingFilePrefix = 'fse-identity-';
const animatedPairingFrameBytes = 64;
const animatedPairingParityTarget = 0.3;

export const animatedPairingConservativeDensityProfile: AnimatedPairingDensityProfile = {
  maxPayloadBytesPerFrame: 64,
  minFrameDisplayMilliseconds: 180,
  minimumVisualModuleSize: 'large-camera-friendly',
  guidance: 'Use conservative animated pairing density for weak cameras and shaky hands: prefer larger modules, slower frames, and at most 64 bytes per frame until real camera testing proves a denser profile is reliable.'
};

export function exportIdentityPackageAsCopyableText(pkg: IdentityPairingPackage): string {
  // copyable text: stable pretty JSON so users can paste it into another GUI.
  return JSON.stringify(pkg, null, 2);
}

export function buildIdentityPackageDownload(pkg: IdentityPairingPackage): IdentityPairingDownload {
  const safeGroup = sanitizeFilenamePart(pkg.groupId || 'identity');
  const safeDiscovery = sanitizeFilenamePart(pkg.discoveryId || 'peer');
  return {
    filename: `${identityPairingFilePrefix}${safeGroup}-${safeDiscovery}.json`,
    mimeType: 'application/json',
    // downloadable identity file: same payload as copyable text, never daemon API/private keys.
    text: exportIdentityPackageAsCopyableText(pkg)
  };
}

export function buildIdentityPairingQRFallbackPayload(pkg: IdentityPairingPackage): IdentityPairingQRFallbackPayload {
  const text = exportIdentityPackageAsCopyableText(pkg);
  return {
    format: 'fse-identity-qr-fallback-v1',
    mimeType: 'application/json',
    text,
    // QR fallback payload: GUI renderers can hand this data URL to a native/browser QR renderer
    // without routing the identity package through generic logs or daemon status.
    dataURL: `data:application/json;charset=utf-8,${encodeURIComponent(text)}`,
    securityNote: 'QR fallback payload contains the same identity bootstrap secret as the downloadable identity file; show it only in the pairing view and never in generic logs/status.'
  };
}

export function buildIdentityPairingQRImageModel(pkg: IdentityPairingPackage, moduleSizePixels = 8): IdentityPairingQRImageModel {
  const payload = buildIdentityPairingQRFallbackPayload(pkg);
  return {
    sourceURI: `qr://fse-identity/${encodeURIComponent(pkg.groupId)}/${encodeURIComponent(pkg.discoveryId)}`,
    payloadDataURL: payload.dataURL,
    moduleGrid: buildDeterministicQRPreviewGrid(payload.text),
    moduleSizePixels,
    quietZoneModules: 4,
    altText: `QR fallback image for identity group ${pkg.groupId}`,
    guidance: 'QR fallback image model is for a native/browser QR renderer in the pairing view only; Show only in the pairing view and never in generic logs/status.'
  };
}

export function parseImportedIdentityPackageText(text: string): IdentityPairingPackage {
  const parsed = JSON.parse(text) as Partial<IdentityPairingPackage>;
  for (const field of ['version', 'discoveryId', 'groupId', 'bootstrapProofKey'] as const) {
    if (typeof parsed[field] !== 'string' || parsed[field]?.trim() === '') {
      throw new Error(`identity package ${field} is required`);
    }
  }
  if (typeof parsed.bootstrapEncryptionLevel !== 'number' || typeof parsed.defaultPeerEncryptionLevel !== 'number') {
    throw new Error('identity package encryption levels are required');
  }
  return parsed as IdentityPairingPackage;
}

export function animatedPairingCodeDescriptor(pkg: IdentityPairingPackage): AnimatedPairingCodeDescriptor {
  return {
    label: `Animated pairing code for ${pkg.groupId}`,
    mode: 'animated visual code',
    qrFallback: 'QR code fallback',
    framePlan: 'Animated visual code frames carry ordered fragments as privacy-preserving animated pairing fragments plus 30% total-frame parity with frameKind, frameIndex, frameCount, sessionId, payloadByteLength, and payloadSha256 verification metadata. The first implementation uses conservative animated pairing density: at most 64 bytes per frame, large-camera-friendly modules, and slow enough frame timing for weak cameras and shaky hands.',
    securityNote: 'A single frame is not a complete identity secret; single captured frame cannot reveal a contiguous identity payload chunk because every displayed shard is a recoverable linear mix rather than raw payload bytes. Scan and verify enough ordered/parity fragments before daemon-owned identity import. QR code fallback remains available while visual scanner UI and mobile camera support mature.'
  };
}

export async function buildAnimatedPairingFrames(pkg: IdentityPairingPackage, bytesPerFrame = animatedPairingFrameBytes): Promise<AnimatedPairingFrame[]> {
  if (!Number.isInteger(bytesPerFrame) || bytesPerFrame < 32) {
    throw new Error('animated pairing bytesPerFrame must be an integer of at least 32 bytes');
  }
  if (bytesPerFrame > animatedPairingConservativeDensityProfile.maxPayloadBytesPerFrame) {
    throw new Error('reject dense animated pairing frames: keep payload fragments at 64 bytes per frame or less for weak cameras and shaky hands');
  }
  const payload = exportIdentityPackageAsCopyableText(pkg);
  const payloadBytes = new TextEncoder().encode(payload);
  const payloadSha256 = await sha256Hex(payloadBytes);
  const sessionId = await sha256Hex(new TextEncoder().encode(`${payloadSha256}:${payloadBytes.length}:fse-animated-pairing-v1`));
  const dataFrameCount = Math.max(1, Math.ceil(payloadBytes.length / bytesPerFrame));
  if (dataFrameCount > 255) {
    throw new Error('animated pairing payload is too large for v1 parity coding');
  }
  const parityFrameCount = Math.max(1, Math.ceil((dataFrameCount * animatedPairingParityTarget) / (1 - animatedPairingParityTarget)));
  const frameCount = dataFrameCount + parityFrameCount;
  const dataShards = buildDataShards(payloadBytes, dataFrameCount, bytesPerFrame);
  const frames: AnimatedPairingFrame[] = [];
  for (let dataIndex = 0; dataIndex < dataFrameCount; dataIndex += 1) {
    frames.push(animatedPairingFrame({
      sessionId: sessionId.slice(0, 24),
      frameIndex: dataIndex,
      frameCount,
      payloadSha256,
      frameKind: 'data',
      dataFrameCount,
      parityFrameCount,
      payloadByteLength: payloadBytes.length,
      fragment: buildPrivacyPreservingAnimatedPairingShard(dataShards, dataIndex)
    }));
  }
  for (let parityIndex = 0; parityIndex < parityFrameCount; parityIndex += 1) {
    frames.push(animatedPairingFrame({
      sessionId: sessionId.slice(0, 24),
      frameIndex: dataFrameCount + parityIndex,
      frameCount,
      payloadSha256,
      frameKind: 'parity',
      dataFrameCount,
      parityFrameCount,
      payloadByteLength: payloadBytes.length,
      fragment: buildPrivacyPreservingAnimatedPairingShard(dataShards, dataFrameCount + parityIndex)
    }));
  }
  return frames;
}

export async function assembleAnimatedPairingFrames(frames: AnimatedPairingFrame[]): Promise<IdentityPairingPackage> {
  if (frames.length === 0) {
    throw new Error('animated pairing frames are required');
  }
  const first = frames[0];
  const byIndex = new Map<number, AnimatedPairingFrame>();
  for (const frame of frames) {
    validateAnimatedPairingFrame(frame, first);
    byIndex.set(frame.frameIndex, frame);
  }
  const recovered = recoverAnimatedPairingDataShards(byIndex, first);
  const payloadBytes = concatBytes(recovered).slice(0, first.payloadByteLength);
  const actualHash = await sha256Hex(payloadBytes);
  if (actualHash !== first.payloadSha256) {
    throw new Error('animated pairing payload checksum did not verify');
  }
  return parseImportedIdentityPackageText(new TextDecoder().decode(payloadBytes));
}

export function createAnimatedPairingScanState(): AnimatedPairingScanState {
  return buildAnimatedPairingScanState([], 0);
}

export function addAnimatedPairingFrameToScan(state: AnimatedPairingScanState, frame: AnimatedPairingFrame): AnimatedPairingScanState {
  // continue scanning subsequent animation loops: duplicates are ignored, missing/new frames keep improving the same scan state.
  // de-duplicate already-seen frames so a first incomplete loop never becomes a hard failure by itself.
  if (state.frames.length > 0) {
    validateAnimatedPairingFrame(frame, state.frames[0]);
  } else {
    validateAnimatedPairingFrame(frame, frame);
  }
  if (state.frames.some((existing) => existing.frameIndex === frame.frameIndex)) {
    return buildAnimatedPairingScanState(state.frames, state.duplicateFrameCount + 1);
  }
  return buildAnimatedPairingScanState([...state.frames, frame], state.duplicateFrameCount);
}

function buildAnimatedPairingScanState(frames: AnimatedPairingFrame[], duplicateFrameCount: number): AnimatedPairingScanState {
  const orderedFrames = [...frames].sort((left, right) => left.frameIndex - right.frameIndex);
  const first = orderedFrames[0];
  const collectedFrameIndexes = orderedFrames.map((frame) => frame.frameIndex);
  const collected = new Set(collectedFrameIndexes);
  const totalFrameCount = first?.frameCount ?? 0;
  const requiredFrameCount = first?.dataFrameCount ?? 1;
  const missingFrameIndexes = Array.from({ length: totalFrameCount }, (_, index) => index).filter((index) => !collected.has(index));
  let status: AnimatedPairingScanProgress['status'] = 'collecting';
  if (first && orderedFrames.length >= first.dataFrameCount) {
    status = missingFrameIndexes.length === 0 ? 'complete' : 'ready';
  }
  const needed = Math.max(0, requiredFrameCount - orderedFrames.length);
  return {
    frames: orderedFrames,
    duplicateFrameCount,
    progress: {
      status,
      collectedFrameCount: orderedFrames.length,
      requiredFrameCount,
      totalFrameCount,
      duplicateFrameCount,
      collectedFrameIndexes,
      missingFrameIndexes,
      message: status === 'collecting'
        ? `Keep phone pointed at screen until pairing is complete. Need ${needed} more unique animated pairing frame${needed === 1 ? '' : 's'}; continue collecting missing/new frames across animation loops.`
        : 'Enough unique frames are available to verify the animated pairing payload; continue collecting if more frames arrive until import completes.'
    }
  };
}

function animatedPairingFrame(input: Omit<AnimatedPairingFrame, 'version' | 'fragmentBase64'> & { fragment: Uint8Array }): AnimatedPairingFrame {
  return {
    version: 'fse-animated-pairing-v1',
    sessionId: input.sessionId,
    frameIndex: input.frameIndex,
    frameCount: input.frameCount,
    payloadSha256: input.payloadSha256,
    frameKind: input.frameKind,
    dataFrameCount: input.dataFrameCount,
    parityFrameCount: input.parityFrameCount,
    payloadByteLength: input.payloadByteLength,
    fragmentBase64: bytesToBase64(input.fragment)
  };
}

function validateAnimatedPairingFrame(frame: AnimatedPairingFrame, first: AnimatedPairingFrame): void {
  if (frame.version !== 'fse-animated-pairing-v1') {
    throw new Error('animated pairing frame version is unsupported');
  }
  if (
    frame.sessionId !== first.sessionId ||
    frame.frameCount !== first.frameCount ||
    frame.payloadSha256 !== first.payloadSha256 ||
    frame.dataFrameCount !== first.dataFrameCount ||
    frame.parityFrameCount !== first.parityFrameCount ||
    frame.payloadByteLength !== first.payloadByteLength
  ) {
    throw new Error('animated pairing frames do not belong to the same session');
  }
  if (frame.frameCount !== frame.dataFrameCount + frame.parityFrameCount) {
    throw new Error('animated pairing frame count does not match data/parity counts');
  }
  if (!Number.isInteger(frame.frameIndex) || frame.frameIndex < 0 || frame.frameIndex >= frame.frameCount) {
    throw new Error('animated pairing frame index is out of range');
  }
  const expectedKind = frame.frameIndex < frame.dataFrameCount ? 'data' : 'parity';
  if (frame.frameKind !== expectedKind) {
    throw new Error('animated pairing frame kind does not match index');
  }
  if (frame.dataFrameCount < 1 || frame.dataFrameCount > 255 || frame.parityFrameCount < 1 || frame.payloadByteLength < 1) {
    throw new Error('animated pairing frame data/parity metadata is invalid');
  }
}

function recoverAnimatedPairingDataShards(byIndex: Map<number, AnimatedPairingFrame>, first: AnimatedPairingFrame): Uint8Array[] {
  const rows: number[][] = [];
  const shards: Uint8Array[] = [];
  for (let frameIndex = 0; frameIndex < first.frameCount && rows.length < first.dataFrameCount; frameIndex += 1) {
    const frame = byIndex.get(frameIndex);
    if (!frame) {
      continue;
    }
    rows.push(coefficientsForPrivacyPreservingFrame(frameIndex, first.dataFrameCount));
    shards.push(base64ToBytes(frame.fragmentBase64));
  }
  if (rows.length < first.dataFrameCount) {
    throw new Error('animated pairing frame set is incomplete; not enough data/parity frames to recover missing animated pairing data frames');
  }
  const shardLength = shards[0]?.length ?? 0;
  if (shardLength === 0 || shards.some((shard) => shard.length !== shardLength)) {
    throw new Error('animated pairing frame shard sizes are inconsistent');
  }
  const inverse = invertMatrix(rows);
  return inverse.map((inverseRow) => {
    const out = new Uint8Array(shardLength);
    for (let sourceIndex = 0; sourceIndex < shards.length; sourceIndex += 1) {
      const factor = inverseRow[sourceIndex];
      if (factor === 0) {
        continue;
      }
      const shard = shards[sourceIndex];
      for (let byteIndex = 0; byteIndex < shardLength; byteIndex += 1) {
        out[byteIndex] ^= gfMul(factor, shard[byteIndex]);
      }
    }
    return out;
  });
}

function buildDataShards(payloadBytes: Uint8Array, dataFrameCount: number, bytesPerFrame: number): Uint8Array[] {
  const shards: Uint8Array[] = [];
  for (let dataIndex = 0; dataIndex < dataFrameCount; dataIndex += 1) {
    const shard = new Uint8Array(bytesPerFrame);
    shard.set(payloadBytes.slice(dataIndex * bytesPerFrame, Math.min((dataIndex + 1) * bytesPerFrame, payloadBytes.length)));
    shards.push(shard);
  }
  return shards;
}

function buildPrivacyPreservingAnimatedPairingShard(dataShards: Uint8Array[], frameIndex: number): Uint8Array {
  // privacy-preserving animated pairing fragments: each frame is a linear combination
  // of every data shard, so a single captured frame cannot reveal a contiguous identity
  // payload chunk. We intentionally avoid identity-basis data shards while keeping the
  // first dataFrameCount rows invertible for recovery once enough frames are collected.
  const shardLength = dataShards[0].length;
  const mixed = new Uint8Array(shardLength);
  const coefficients = coefficientsForPrivacyPreservingFrame(frameIndex, dataShards.length);
  for (let dataIndex = 0; dataIndex < dataShards.length; dataIndex += 1) {
    const factor = coefficients[dataIndex];
    for (let byteIndex = 0; byteIndex < shardLength; byteIndex += 1) {
      mixed[byteIndex] ^= gfMul(factor, dataShards[dataIndex][byteIndex]);
    }
  }
  return mixed;
}

function coefficientsForPrivacyPreservingFrame(frameIndex: number, dataFrameCount: number): number[] {
  const base = frameIndex + 1;
  const coefficients: number[] = [];
  for (let dataIndex = 0; dataIndex < dataFrameCount; dataIndex += 1) {
    coefficients.push(gfPow(base, dataIndex));
  }
  return coefficients;
}

function invertMatrix(matrix: number[][]): number[][] {
  const size = matrix.length;
  const augmented = matrix.map((row, rowIndex) => {
    const identity = new Array<number>(size).fill(0);
    identity[rowIndex] = 1;
    return [...row, ...identity];
  });
  for (let col = 0; col < size; col += 1) {
    let pivot = col;
    while (pivot < size && augmented[pivot][col] === 0) {
      pivot += 1;
    }
    if (pivot === size) {
      throw new Error('animated pairing parity matrix is not recoverable');
    }
    if (pivot !== col) {
      [augmented[col], augmented[pivot]] = [augmented[pivot], augmented[col]];
    }
    const inversePivot = gfInv(augmented[col][col]);
    for (let item = 0; item < size * 2; item += 1) {
      augmented[col][item] = gfMul(augmented[col][item], inversePivot);
    }
    for (let row = 0; row < size; row += 1) {
      if (row === col || augmented[row][col] === 0) {
        continue;
      }
      const factor = augmented[row][col];
      for (let item = 0; item < size * 2; item += 1) {
        augmented[row][item] ^= gfMul(factor, augmented[col][item]);
      }
    }
  }
  return augmented.map((row) => row.slice(size));
}

function gfMul(a: number, b: number): number {
  if (a === 0 || b === 0) {
    return 0;
  }
  const tables = gfTables();
  return tables.exp[tables.log[a] + tables.log[b]];
}

function gfPow(a: number, power: number): number {
  if (power === 0) {
    return 1;
  }
  if (a === 0) {
    return 0;
  }
  const tables = gfTables();
  return tables.exp[(tables.log[a] * power) % 255];
}

function gfInv(a: number): number {
  if (a === 0) {
    throw new Error('cannot invert zero in animated pairing parity matrix');
  }
  const tables = gfTables();
  return tables.exp[255 - tables.log[a]];
}

let cachedGFTables: { exp: number[]; log: number[] } | undefined;

function gfTables(): { exp: number[]; log: number[] } {
  if (cachedGFTables) {
    return cachedGFTables;
  }
  const exp = new Array<number>(512).fill(0);
  const log = new Array<number>(256).fill(0);
  let value = 1;
  for (let index = 0; index < 255; index += 1) {
    exp[index] = value;
    log[value] = index;
    value <<= 1;
    if ((value & 0x100) !== 0) {
      value ^= 0x11d;
    }
  }
  for (let index = 255; index < exp.length; index += 1) {
    exp[index] = exp[index - 255];
  }
  cachedGFTables = { exp, log };
  return cachedGFTables;
}

function sanitizeFilenamePart(value: string): string {
  const sanitized = value.replace(/[^a-zA-Z0-9._-]+/g, '-').replace(/^-+|-+$/g, '');
  return sanitized || 'identity';
}

function buildDeterministicQRPreviewGrid(text: string): boolean[][] {
  // QR fallback image: deterministic preview modules for layout/renderer handoff.
  // A native/browser QR renderer should encode payloadDataURL/text into a standards-compliant QR symbol.
  const size = 21;
  const bytes = new TextEncoder().encode(text);
  return Array.from({ length: size }, (_, y) => Array.from({ length: size }, (_, x) => {
    if (isFinderModule(x, y, 0, 0) || isFinderModule(x, y, size - 7, 0) || isFinderModule(x, y, 0, size - 7)) {
      return true;
    }
    const byte = bytes[(x * 31 + y * 17) % bytes.length] ?? 0;
    return ((byte + x * 13 + y * 7) & 1) === 0;
  }));
}

function isFinderModule(x: number, y: number, originX: number, originY: number): boolean {
  const localX = x - originX;
  const localY = y - originY;
  if (localX < 0 || localY < 0 || localX >= 7 || localY >= 7) {
    return false;
  }
  return localX === 0 || localY === 0 || localX === 6 || localY === 6 || (localX >= 2 && localX <= 4 && localY >= 2 && localY <= 4);
}

async function sha256Hex(bytes: Uint8Array): Promise<string> {
  const digestInput = new Uint8Array(bytes.byteLength);
  digestInput.set(bytes);
  const digest = await crypto.subtle.digest('SHA-256', digestInput.buffer);
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0')).join('');
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = '';
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary);
}

function base64ToBytes(value: string): Uint8Array {
  const binary = atob(value);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes;
}

function concatBytes(chunks: Uint8Array[]): Uint8Array {
  const total = chunks.reduce((sum, chunk) => sum + chunk.length, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    out.set(chunk, offset);
    offset += chunk.length;
  }
  return out;
}
