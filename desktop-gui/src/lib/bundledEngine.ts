export type DesktopTarget =
  | 'windows-amd64'
  | 'windows-arm64'
  | 'darwin-amd64'
  | 'darwin-arm64'
  | 'linux-amd64'
  | 'linux-arm64';

export type BundledEngineManifestEntry = {
  target: DesktopTarget;
  relativePath: string;
  expectedExecutable: string;
  expectedVersion: string;
  expectedSHA256: string;
};

export type BundledEngineManifest = Record<DesktopTarget, BundledEngineManifestEntry>;

export type BundledEngineResourceManifest = {
  version: string;
  entries: BundledEngineManifestEntry[];
};

export type BundledEngineCandidate = {
  target: DesktopTarget;
  executableName: string;
  version: string;
  sha256: string;
};

export type BundledEngineResourceObservation = {
  target: DesktopTarget;
  relativePath: string;
  executableName: string;
  sha256: string;
  exists: boolean;
};

export type BundledEngineVerification = {
  ok: boolean;
  reason?: string;
  entry: BundledEngineManifestEntry;
};

export type BundledEngineRuntimeGate =
  | { bundleVerified: true; manifest: BundledEngineResourceManifest; verified: BundledEngineVerification[] }
  | { bundleVerified: false; manifest?: BundledEngineResourceManifest; reason: string; failures: BundledEngineVerification[] };

export type BundledDaemonLifecycleAction = 'status' | 'start' | 'stop' | 'restart';

export type BundledDaemonLifecycleSettings = {
  encryptedApiBaseURL: `https://${string}`;
  apiKey: string;
  platform: 'systemd' | 'launchd' | 'windows';
  serviceName: string;
};

export const bundledDesktopTargets: DesktopTarget[] = [
  'windows-amd64',
  'windows-arm64',
  'darwin-amd64',
  'darwin-arm64',
  'linux-amd64',
  'linux-arm64'
];

export const bundledEngineManifest: BundledEngineManifest = {
  'windows-amd64': {
    target: 'windows-amd64',
    relativePath: 'engine/windows/amd64/fse.exe',
    expectedExecutable: 'fse.exe',
    expectedVersion: '0.0.0-prototype',
    expectedSHA256: 'provided-by-release-manifest'
  },
  'windows-arm64': {
    target: 'windows-arm64',
    relativePath: 'engine/windows/arm64/fse.exe',
    expectedExecutable: 'fse.exe',
    expectedVersion: '0.0.0-prototype',
    expectedSHA256: 'provided-by-release-manifest'
  },
  'darwin-amd64': {
    target: 'darwin-amd64',
    relativePath: 'engine/darwin/amd64/fse',
    expectedExecutable: 'fse',
    expectedVersion: '0.0.0-prototype',
    expectedSHA256: 'provided-by-release-manifest'
  },
  'darwin-arm64': {
    target: 'darwin-arm64',
    relativePath: 'engine/darwin/arm64/fse',
    expectedExecutable: 'fse',
    expectedVersion: '0.0.0-prototype',
    expectedSHA256: 'provided-by-release-manifest'
  },
  'linux-amd64': {
    target: 'linux-amd64',
    relativePath: 'engine/linux/amd64/fse',
    expectedExecutable: 'fse',
    expectedVersion: '0.0.0-prototype',
    expectedSHA256: 'provided-by-release-manifest'
  },
  'linux-arm64': {
    target: 'linux-arm64',
    relativePath: 'engine/linux/arm64/fse',
    expectedExecutable: 'fse',
    expectedVersion: '0.0.0-prototype',
    expectedSHA256: 'provided-by-release-manifest'
  }
};

export function verifyBundledEngine(candidate: BundledEngineCandidate): BundledEngineVerification {
  const entry = bundledEngineManifest[candidate.target];
  if (candidate.executableName !== entry.expectedExecutable) {
    return { ok: false, reason: `unexpected executable ${candidate.executableName}`, entry };
  }
  if (candidate.version !== entry.expectedVersion) {
    return { ok: false, reason: `unexpected engine version ${candidate.version}`, entry };
  }
  if (candidate.sha256 !== entry.expectedSHA256) {
    return { ok: false, reason: 'unexpected engine SHA-256', entry };
  }
  return { ok: true, entry };
}

export function verifyBundledEngineResourceEntry(
  entry: BundledEngineManifestEntry,
  observation: BundledEngineResourceObservation | undefined,
  manifestVersion: string
): BundledEngineVerification {
  if (!observation || !observation.exists) {
    return { ok: false, reason: `missing packaged daemon for ${entry.target}`, entry };
  }
  if (observation.relativePath !== entry.relativePath) {
    return { ok: false, reason: `unexpected packaged daemon path ${observation.relativePath}`, entry };
  }
  if (observation.executableName !== entry.expectedExecutable) {
    return { ok: false, reason: `unexpected executable ${observation.executableName}`, entry };
  }
  if (entry.expectedVersion !== manifestVersion) {
    return { ok: false, reason: `entry version ${entry.expectedVersion} does not match manifest version ${manifestVersion}`, entry };
  }
  if (observation.sha256 !== entry.expectedSHA256) {
    return { ok: false, reason: 'unexpected engine SHA-256', entry };
  }
  return { ok: true, entry };
}

export function verifyBundledEngineResourceManifest(
  manifest: BundledEngineResourceManifest,
  observations: BundledEngineResourceObservation[]
): BundledEngineRuntimeGate {
  const entriesByTarget = new Map<DesktopTarget, BundledEngineManifestEntry>();
  const observationsByTarget = new Map<DesktopTarget, BundledEngineResourceObservation>();
  const failures: BundledEngineVerification[] = [];
  const verified: BundledEngineVerification[] = [];

  for (const entry of manifest.entries) {
    if (entriesByTarget.has(entry.target)) {
      failures.push({ ok: false, reason: `duplicate manifest target ${entry.target}`, entry });
      continue;
    }
    entriesByTarget.set(entry.target, entry);
  }
  for (const observation of observations) {
    observationsByTarget.set(observation.target, observation);
  }

  for (const target of bundledDesktopTargets) {
    const entry = entriesByTarget.get(target);
    if (!entry) {
      const placeholder = bundledEngineManifest[target];
      failures.push({ ok: false, reason: `missing manifest target ${target}`, entry: placeholder });
      continue;
    }
    const result = verifyBundledEngineResourceEntry(entry, observationsByTarget.get(target), manifest.version);
    if (result.ok) {
      verified.push(result);
    } else {
      failures.push(result);
    }
  }

  if (failures.length > 0) {
    return { bundleVerified: false, manifest, reason: 'bundled engine verification failed', failures };
  }
  return { bundleVerified: true, manifest, verified };
}

export async function controlBundledDaemonLifecycle(
  settings: BundledDaemonLifecycleSettings,
  action: BundledDaemonLifecycleAction
): Promise<Response> {
  const base = settings.encryptedApiBaseURL.replace(/\/+$/, '');
  return await fetch(`${base}/v1/service-command`, {
    method: 'POST',
    headers: {
      'X-API-Key': settings.apiKey,
      'Content-Type': 'application/json',
      'Accept': 'application/json'
    },
    body: JSON.stringify({
      action,
      platform: settings.platform,
      name: settings.serviceName
    })
  });
}

export async function controlVerifiedBundledDaemonLifecycle(
  gate: BundledEngineRuntimeGate,
  settings: BundledDaemonLifecycleSettings,
  action: BundledDaemonLifecycleAction
): Promise<Response> {
  if (!gate.bundleVerified) {
    throw new Error(`bundled engine verification failed: ${gate.reason}`);
  }
  return await controlBundledDaemonLifecycle(settings, action);
}
