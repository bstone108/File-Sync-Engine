export type CredentialVaultPlatform = 'windows' | 'macos' | 'linux';

export type RemoteInstanceCredentialRecord = {
  credentialRef: string;
  platform: CredentialVaultPlatform;
  instanceID: string;
  label: string;
  createdAt: string;
  updatedAt: string;
};

export type RemoteInstanceCredentialSecret = {
  credentialRef: string;
  secretValue: string;
};

export type CredentialVaultBridge = {
  storeRemoteInstanceCredential(record: RemoteInstanceCredentialRecord, secret: RemoteInstanceCredentialSecret): Promise<RemoteInstanceCredentialRecord>;
  resolveRemoteInstanceCredential(credentialRef: string): Promise<RemoteInstanceCredentialSecret>;
  deleteRemoteInstanceCredential(credentialRef: string): Promise<void>;
};

export const credentialVaultPlatformNotes: Record<CredentialVaultPlatform, string> = {
  windows: 'Windows Credential Manager stores remote daemon API credentials outside the instance registry.',
  macos: 'macOS Keychain stores remote daemon API credentials outside the instance registry.',
  linux: 'Freedesktop Secret Service stores remote daemon API credentials outside the instance registry.'
};

export function buildRemoteInstanceCredentialRef(instanceID: string): string {
  const safeID = instanceID.trim().replace(/[^a-zA-Z0-9_.:-]/g, '-');
  if (!safeID) {
    throw new Error('remote instance ID is required before creating a credentialRef');
  }
  return `desktop-vault:remote:${safeID}`;
}

export function remoteCredentialStorageBoundary(): string {
  return 'api key material is never persisted in the instance registry; only credentialRef metadata is stored there while the native shell writes secrets to Windows Credential Manager, macOS Keychain, or Freedesktop Secret Service.';
}

export async function storeRemoteInstanceCredential(
  bridge: CredentialVaultBridge,
  record: RemoteInstanceCredentialRecord,
  secret: RemoteInstanceCredentialSecret
): Promise<RemoteInstanceCredentialRecord> {
  if (!record.credentialRef || record.credentialRef !== secret.credentialRef) {
    throw new Error('matching credentialRef is required before storing a remote instance credential');
  }
  if (!secret.secretValue) {
    throw new Error('remote instance credential secret is required');
  }
  return await bridge.storeRemoteInstanceCredential(record, secret);
}

export async function resolveRemoteInstanceCredential(
  bridge: CredentialVaultBridge,
  credentialRef: string
): Promise<RemoteInstanceCredentialSecret> {
  if (!credentialRef) {
    throw new Error('credentialRef is required before resolving a remote instance credential');
  }
  return await bridge.resolveRemoteInstanceCredential(credentialRef);
}

export async function deleteRemoteInstanceCredential(bridge: CredentialVaultBridge, credentialRef: string): Promise<void> {
  if (!credentialRef) {
    throw new Error('credentialRef is required before deleting a remote instance credential');
  }
  await bridge.deleteRemoteInstanceCredential(credentialRef);
}
