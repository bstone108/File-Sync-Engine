export type FSEPlatform = 'macos' | 'windows' | 'linux' | 'docker';

export function fsePlatform(): FSEPlatform {
  const configured = import.meta.env.VITE_FSE_PLATFORM;
  if (configured === 'macos' || configured === 'windows' || configured === 'linux' || configured === 'docker') {
    return configured;
  }
  if (typeof navigator === 'undefined') {
    return 'linux';
  }
  const ua = navigator.userAgent.toLowerCase();
  if (ua.includes('mac')) {
    return 'macos';
  }
  if (ua.includes('windows')) {
    return 'windows';
  }
  return 'linux';
}

export function isDockerGUI(): boolean {
  return fsePlatform() === 'docker';
}
