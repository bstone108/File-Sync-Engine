<script lang="ts">
  import { onMount } from 'svelte';
  import { fsePlatform, isDockerGUI } from './lib/platform';
  import {
    fetchDaemonStatus,
    fetchDaemonFolders,
    fetchDaemonPeers,
    fetchConfigSync,
    fetchDaemonLogs,
    fetchDaemonTransfers,
    fetchBackupJobs,
    fetchAPITrustStatus,
    fetchEncryptionProfiles,
    browseFilesystemDirectories,
    fetchRemoteMeshSettings,
    generateIdentityPairingPackage,
    importIdentityPairingPackage,
    queueOnboarding,
    patchDaemonConfig,
    pinActiveAPICertificate,
    queueConfigSyncEdit,
    queueRemoteMeshSettingsCommand,
    readDaemonConfig,
    runBackupScrub,
    runMaintenanceScrub,
    runSnapshotRestore,
    runSnapshotRetention,
    planSnapshotRestore,
    sendSnapshotCommand,
    sendDiscoveryCommand,
    sendFolderCommand,
    sendPeerCommand,
    sendTransferCommand,
    sendWebGUICommand,
    type CommandResponse,
    type APITrustStatus,
    type EncryptionProfile,
    type DaemonConfig,
    type DaemonStatus,
    type DaemonFolder,
    type DaemonPeer,
    type DaemonEvent,
    type DaemonTransferReadModel,
    type SnapshotMarker,
    type RestorePlanResponse,
    type IdentityPairingPackage,
    type FilesystemBrowseEntry,
    type MeshSettingsCommandResponse,
    type MeshSettingsDocument,
    type ConfigSyncInstance
  } from './lib/daemonApi';
  import {
    adoptGUIOwnedNonServiceDaemon,
    controlLocalDaemon,
    getNativeDesktopShell,
    getGUIOwnedNonServiceDaemonSession,
    getGUIOwnedNonServiceDaemonState,
    requestGUIOwnedNonServiceDaemonLaunch,
    stopGUIOwnedNonServiceDaemonThroughAPI,
    type BundledEngineInspection,
    type DesktopPreferences,
    type DesktopAppUpdateStatus
  } from './lib/nativeShell';
  import { type GUIManagedNonServiceDaemonSession } from './lib/firstLaunch';
  import {
    animatedPairingCodeDescriptor,
    animatedPairingConservativeDensityProfile,
    buildAnimatedPairingFrames,
    buildIdentityPackageDownload,
    buildIdentityPairingQRFallbackPayload,
    buildIdentityPairingQRImageModel,
    createAnimatedPairingScanState,
    exportIdentityPackageAsCopyableText,
    parseImportedIdentityPackageText,
    type AnimatedPairingScanProgress
  } from './lib/identityPairing';
  import {
    buildDesktopAnimatedPairingCameraCaptureState,
    buildDesktopAnimatedPairingScannerScreen,
    type DesktopAnimatedPairingScannerScreenModel
  } from './lib/desktopAnimatedPairingScanner';
  import {
    buildReadableHostStatusMetrics,
    buildPeerPairingStatusLine,
    summarizeLocalPeerLink,
    buildOfflineRemoteSettingsEdit,
    buildRemoteInstanceOnboardingCandidate,
    buildRemoteMeshSettingsDocument,
    queueRemoteMeshSettingsChange,
    defaultExpandedInstanceGroups,
    defaultLocalDaemonInstance,
    ensureLocalInstanceFirst,
    formatConnectionStateLabel,
    formatTransferByteSummary,
    groupDaemonInstances,
    addRemoteDaemonInstance,
    type ManagedDaemonInstance,
    type RemoteInstanceOnboardingSource
  } from './lib/instanceRegistry';

  type LocalControlArea = 'peer' | 'folder' | 'discovery' | 'transfer' | 'maintenance' | 'webGUI' | 'config' | 'apiTrust';
  type LocalControlOperationPhase = 'idle' | 'accepted' | 'pending' | 'failed' | 'completed';
  type LocalControlOperationState = {
    phase: LocalControlOperationPhase;
    command: string;
    summary: string;
    detail?: string;
  };

  let apiBaseURL = 'https://127.0.0.1:22420';
  let localCredentialRef = '';
  $: daemonConnectionSettings = { apiBaseURL, credentialRef: localCredentialRef || undefined };
  $: hasDaemonCredential = Boolean(localCredentialRef);
  let status: DaemonStatus | null = null;
  let configInstances: ConfigSyncInstance[] = [];
  let selectedConfigInstanceId = '';
  let configInstanceTouched = false;
  let theme: 'dark' | 'light' = 'dark';
  let folderRemoveConfirm = false;
  let folderRemoveArmed = false;
  let bundledEngineInspection: BundledEngineInspection | null = null;
  let bundledEngineInspectionMessage = 'Bundled engine manifest has not been inspected.';
  let desktopPreferences: DesktopPreferences = { theme: 'system', density: 'comfortable', minimizeToTray: false, notificationsEnabled: true };
  let desktopPreferencesMessage = 'Loading persisted desktop preferences…';
  let desktopAppUpdate: DesktopAppUpdateStatus | null = null;
  let desktopAppUpdateBusy = false;
  let daemonFolders: DaemonFolder[] = [];
  let daemonPeers: DaemonPeer[] = [];
  let daemonEvents: DaemonEvent[] = [];
  let daemonTransfers: DaemonTransferReadModel = { active: [], history: [], liveRatesAvailable: false, byteProgressAvailable: false };
  let transfersMessage = 'Transfer activity has not been loaded.';
  let warningsMessage = 'Recent daemon events have not been loaded.';
  let snapshotMarkers: SnapshotMarker[] = [];
  let snapshotFolderID = '';
  let snapshotDescription = '';
  let selectedSnapshotID = '';
  let restorePaths = '';
  let restoreDestination = '';
  let restorePlan: RestorePlanResponse | null = null;
  let reviewedRestoreRequestKey = '';
  $: currentRestoreRequestKey = JSON.stringify({ snapshotId: selectedSnapshotID.trim(), paths: selectedRestorePaths(), alternatePath: restoreDestination.trim() || undefined });
  let retentionKeepLast = 10;
  let maintenanceBackupMessage = 'Snapshot and backup state has not been loaded.';
  let backupJobsSummary = '';
  let apiTrustStatus: APITrustStatus | null = null;
  let daemonConfig: DaemonConfig | null = null;
  let controlMessage = '';
  let peerFormAction: 'add' | 'update' | 'remove' = 'add';
  let peerFormID = '';
  let peerFormEndpoint = '';
  let folderFormAction: 'add' | 'update' | 'remove' = 'add';
  let folderFormID = '';
  let folderFormPath = '';
  let remoteBrowsePath = '';
  let remoteBrowseEntries: FilesystemBrowseEntry[] = [];
  let remoteBrowseMessage = 'Manual path entry remains available when the selected host is offline.';
  let remoteBrowseLoading = false;
  let folderFormMode = 'sendrecv';
  let folderFormSharedWith = '';
  let folderEditor: DaemonFolder | null = null;
  let folderEditPath = '';
  let folderEditMode = 'sendrecv';
  let folderEditIgnore = '';
  let folderEditSharedPeers: string[] = [];
  let folderEditPeerDraft = '';
  let folderEditorMessage = '';
  let folderEditorRemoveConfirm = false;
  let folderRowMessage = '';
  let expandedFolderIds: Record<string, boolean> = {};
  let addPeerMenuOpen = false;
  let linkPeerFormOpen = false;
  const dockerGUI = isDockerGUI();
  const guiPlatform = fsePlatform();
  let discoveryFormDisabled = false;
  let discoveryFormDHT = true;
  let discoveryFormLocal = true;
  let transferFormAction: 'pause' | 'resume' | 'cancel' = 'pause';
  let transferFormFolderID = '';
  let transferFormPeerID = '';
  let maintenanceFormFolderID = '';
  let webGUIFormAction: 'status' | 'install' | 'update' | 'start' | 'stop' = 'status';
  let configPatchLoggingLevel = 'info';
  let encryptionProfiles: EncryptionProfile[] = [];
  let selectedEncryptionLevel = '4';
  let encryptionLevelDirty = false;
  let encryptionLevelLoaded = false;
  let identityExchangeEncryptionLevel = 10;
  const primaryIdentityPackageID = 'primary';
  const pairingGroupID = primaryIdentityPackageID;
  let advertiseOverride = '';
  let advertiseFieldDirty = false;
  let localEngineConnectionState: ManagedDaemonInstance['connectionState'] = 'offline';
  let localEngineConnectionLabel = 'Offline';
  let pairingPackage: IdentityPairingPackage | null = null;
  let pairingExportText = '';
  let pairingDownloadFilename = '';
  let pairingQRFallbackPayload = '';
  let qrFallbackImageModel: ReturnType<typeof buildIdentityPairingQRImageModel> | null = null;
  let pairingAnimatedFrameList = '';
  let pairingImportText = '';
  let parsedPairingImport: IdentityPairingPackage | null = null;
  let pairingImportSummary = '';
  let pairingMessage = '';
  let pairingCode = '';
  let onboardingCommands: Array<{ id: string; targetInstanceId: string; action: string; code?: string; expiresAt?: string; status: string; message?: string }> = [];
  let remotePairingCode = '';
  let animatedPairingScanState = createAnimatedPairingScanState();
  let animatedScanProgress: AnimatedPairingScanProgress = animatedPairingScanState.progress;
  let desktopAnimatedPairingScannerScreen: DesktopAnimatedPairingScannerScreenModel = buildDesktopAnimatedPairingScannerScreen(
    buildDesktopAnimatedPairingCameraCaptureState({ cameraPermission: 'unknown', scannerState: animatedPairingScanState })
  );
  let pairingLoading = false;
  let errorMessage = '';
  let loading = false;
  let lifecycleLoading = false;
  let guiOwnedNonServiceDaemonSession: GUIManagedNonServiceDaemonSession | null = null;
  let guiOwnedNonServiceDaemonMessage = 'No GUI-owned non-service daemon session has been launched or adopted yet.';
  let guiOwnedNonServiceDaemonMode: GUIManagedNonServiceDaemonSession['sessionMode'] = 'persistent-user-daemon';
  let localControlOperationStates: Record<LocalControlArea, LocalControlOperationState> = {
    peer: { phase: 'idle', command: 'peer', summary: 'No peer command has run yet.' },
    folder: { phase: 'idle', command: 'folder', summary: 'No folder command has run yet.' },
    discovery: { phase: 'idle', command: 'discovery', summary: 'No discovery command has run yet.' },
    transfer: { phase: 'idle', command: 'transfer', summary: 'No transfer command has run yet.' },
    maintenance: { phase: 'idle', command: 'maintenance', summary: 'No maintenance command has run yet.' },
    webGUI: { phase: 'idle', command: 'web GUI', summary: 'No web GUI command has run yet.' },
    config: { phase: 'idle', command: 'config', summary: 'No config command has run yet.' },
    apiTrust: { phase: 'idle', command: 'API certificate trust', summary: 'API certificate trust status has not been checked yet.' }
  };
  let peerOperationStatusText = 'idle: No peer command has run yet.';
  let folderOperationStatusText = 'idle: No folder command has run yet.';
  let discoveryOperationStatusText = 'idle: No discovery command has run yet.';
  let transferOperationStatusText = 'idle: No transfer command has run yet.';
  let maintenanceOperationStatusText = 'idle: No maintenance command has run yet.';
  let webGUIOperationStatusText = 'idle: No web GUI command has run yet.';
  let configOperationStatusText = 'idle: No config command has run yet.';
  let apiTrustOperationStatusText = 'idle: API certificate trust status has not been checked yet.';
  type DesktopUIView =
    | 'overview'
    | 'folders'
    | 'peers'
    | 'transfers'
    | 'warnings'
    | 'maintenance'
    | 'daemon-settings'
    | 'security'
    | 'desktop-settings'
    | 'help';

  type DesktopViewCatalogEntry = {
    id: DesktopUIView;
    label: string;
    description: string;
  };

  type DaemonSettingsSection = {
    title: string;
    summary: string;
  };

  type HelpDetailsSection = {
    title: string;
    summary: string;
  };

  type SelectedHostContext = {
    label: string;
    apiBaseURL: string;
    kind: ManagedDaemonInstance['kind'];
    connectionState: ManagedDaemonInstance['connectionState'];
    scopeNote: string;
  };

  let activeView: DesktopUIView = 'overview';
  let nodeOptionsOpen = false;

  function pairingLooksFailed(message: string): boolean {
    return /not found|already used|expired|failed|unavailable|invalid|deadline|502|error/i.test(message)
      && !/is ready|paired by code/i.test(message);
  }

  function onPairingButtonClick(event: MouseEvent) {
    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    const button = target.closest('[data-testid="show-pairing-code"], [data-testid="redeem-pairing-code"]');
    if (!(button instanceof HTMLButtonElement) || button.disabled) {
      return;
    }
    event.preventDefault();
    if (button.getAttribute('data-testid') === 'show-pairing-code') {
      void showPairingCode();
      return;
    }
    void redeemRemotePairingCode();
  }
  let managedDaemonInstances: ManagedDaemonInstance[] = ensureLocalInstanceFirst([defaultLocalDaemonInstance()]);
  let selectedInstanceID = managedDaemonInstances[0].id;
  let expandedInstanceGroups: Record<string, boolean> = defaultExpandedInstanceGroups(managedDaemonInstances);
  let remoteOnboardingSource: RemoteInstanceOnboardingSource = 'api-endpoint-key';
  let remoteOnboardingLabel = '';
  let remoteOnboardingEndpoint = '';
  let remoteOnboardingAPIKey = '';
  let remoteOnboardingPairingCode = '';
  let remoteOnboardingIdentityFileText = '';
  let remoteOnboardingAnimatedCodeSummary = '';
  let remoteOnboardingSharedIdentityID = '';
  let remoteOnboardingMessage = '';
  let remoteRegistryMessage = 'Loading saved remote hosts…';
  let remoteRegistrySaveChain: Promise<void> = Promise.resolve();
  let remoteEditOpen = false;
  let remoteEditLabel = '';
  let remoteEditEndpoint = '';
  let remoteEditTargetID = '';
  let remoteEditTargetCredentialRef = '';
  let remoteEditTargetRevision = 0;
  let remoteCredentialCleanupPending: string[] = [];
  let persistedRemoteSelectionID = '';
  let remoteRemovalConfirmation = '';
  let remoteLifecycleLoading = false;
  let remoteMeshDocuments: MeshSettingsDocument[] = [];
  let remoteMeshPendingChanges: MeshSettingsCommandResponse[] = [];
  let remoteMeshStatusMessage = 'Remote mesh status has not been refreshed yet; online, relay-reachable, and offline instances will show cached document and pending-change state here.';
  $: selectedManagedInstance = managedDaemonInstances.find((instance) => instance.id === selectedInstanceID) ?? managedDaemonInstances[0];
  $: selectedHostContext = {
    label: selectedManagedInstance.label,
    apiBaseURL: selectedManagedInstance.apiBaseURL,
    kind: selectedManagedInstance.kind,
    connectionState: selectedManagedInstance.connectionState,
    scopeNote: 'Every operational view below uses the selected managed daemon instance; only the top GUI menu remains desktop-app scoped.'
  } satisfies SelectedHostContext;
  $: canBrowseSelectedHostFilesystem = selectedManagedInstance.connectionState === 'online' || selectedManagedInstance.connectionState === 'paired';
  $: selectedHostBrowseDisabledReason = canBrowseSelectedHostFilesystem
    ? ''
    : 'Browse disabled: selected host is offline or unreachable for live folder-tree queries; use manual path entry instead.';
  $: remoteMeshSettingsDocument = buildRemoteMeshSettingsDocument({
    nodeID: selectedManagedInstance.id,
    redactedSettingsSummary: `${selectedManagedInstance.label} per-node settings document; offline instances can be inspected or queued for later adoption.`
  });
  $: remoteMeshPendingSettingsChange = queueRemoteMeshSettingsChange({
    targetNodeID: selectedManagedInstance.id,
    nonSecretPatchSummary: 'Example non-secret daemon settings patch queued as durable pending changes through the trusted mesh.'
  });
  $: offlineRemoteSettingsEdit = buildOfflineRemoteSettingsEdit({
    targetNodeID: selectedManagedInstance.id,
    selectedHostLabel: selectedManagedInstance.label,
    nonSecretPatchSummary: 'Selected-host non-secret logging settings edit queued while offline or relay-delayed.'
  });
  $: groupedManagedDaemonInstances = groupDaemonInstances(managedDaemonInstances);
  const viewCatalog: DesktopViewCatalogEntry[] = [
    { id: 'overview', label: 'Overview', description: 'Connection, daemon health, startup, tray, and bundled lifecycle summary.' },
    { id: 'folders', label: 'Folders', description: 'Folder roots, modes, sync groups, and local folder commands.' },
    { id: 'peers', label: 'Peers & Linking', description: 'Peers, discovery, pairing codes, and identity packages.' },
    { id: 'transfers', label: 'Transfers', description: 'Activity, queue, pause, and rates.' },
    { id: 'warnings', label: 'Warnings & logs', description: 'Warnings, structured logs, and recent daemon events.' },
    { id: 'maintenance', label: 'Maintenance & backups', description: 'Scrub, repair, backup, snapshot, restore, retention, and web GUI package operations.' },
    { id: 'daemon-settings', label: 'Settings', description: 'General, network, logging, and folders for the selected engine.' },
    { id: 'security', label: 'Security', description: 'Certificate trust and how session encryption is chosen.' },
    { id: 'desktop-settings', label: 'Desktop app settings', description: 'GUI-only desktop preferences kept separate from daemon host settings.' },
    { id: 'help', label: 'Help & details', description: 'Advanced option details and contextual help without cluttering operation views.' }
  ];
  const daemonSettingsSections: DaemonSettingsSection[] = [
    { title: 'Daemon identity & API', summary: 'Selected-host API URL, redacted API key usage, TLS/encryption policy, node identity, and pairing-sensitive controls belong to the daemon host.' },
    { title: 'Folders, peers, discovery, and transfers', summary: 'Share roots, peer endpoints, discovery mode, and transfer caps are selected-host daemon settings, not desktop app preferences.' },
    { title: 'Metadata, logging, and backup policy', summary: 'Metadata backend, maintenance budgets, structured logging, web GUI package state, backup destinations, snapshots, restore, and repair policy stay scoped to the selected engine.' }
  ];
  const helpDetailsSections: HelpDetailsSection[] = [
    { title: 'Encryption, pairing, and identity details', summary: 'Dedicated help/details pages explain API encryption, identity pairing, key rotation, and revocation without crowding daily peer controls.' },
    { title: 'Maintenance, repair, and backup details', summary: 'Dedicated help/details pages explain scrub classifications, quarantine, restore, retention, and backup protection states before users run high-impact actions.' }
  ];


  function toggleInstanceGroup(groupName: string) {
    expandedInstanceGroups = {
      ...expandedInstanceGroups,
      [groupName]: !(expandedInstanceGroups[groupName] ?? true)
    };
  }

  async function persistRemoteSelection(selected = selectedInstanceID) {
    const target = managedDaemonInstances.some((instance) => instance.kind === 'remote' && instance.id === selected) ? selected : '';
    const saved = await queueRemoteRegistryOperation(() => getNativeDesktopShell().selectRemoteInstance({ instanceID: target, expectedSelectedInstanceID: persistedRemoteSelectionID }));
    persistedRemoteSelectionID = saved.selectedInstanceID ?? '';
  }

  function queueRemoteRegistryOperation<T>(operation: () => Promise<T>): Promise<T> {
    const result = remoteRegistrySaveChain.catch(() => undefined).then(operation);
    remoteRegistrySaveChain = result.then(() => undefined, () => undefined);
    return result;
  }

  async function loadRemoteRegistry() {
    try {
      const saved = await getNativeDesktopShell().getRemoteInstanceRegistry();
      const remotes: ManagedDaemonInstance[] = saved.instances.map((entry) => ({
        ...entry,
        kind: 'remote',
        onboardingSource: entry.source,
        group: 'Remote daemon instances',
        connectionState: 'offline',
        statusSummary: `Saved remote host; last connection state was ${entry.connectionState}. Connect to refresh live daemon state.`
      }));
      managedDaemonInstances = ensureLocalInstanceFirst([defaultLocalDaemonInstance(), ...remotes]);
      remoteCredentialCleanupPending = saved.credentialCleanupPending ?? [];
      persistedRemoteSelectionID = saved.selectedInstanceID ?? '';
      expandedInstanceGroups = defaultExpandedInstanceGroups(managedDaemonInstances);
      const selected = saved.selectedInstanceID && remotes.some((entry) => entry.id === saved.selectedInstanceID) ? saved.selectedInstanceID : managedDaemonInstances[0].id;
      selectManagedInstance(selected, false);
      remoteRegistryMessage = `Loaded ${remotes.length} saved remote host${remotes.length === 1 ? '' : 's'}; live status is refreshed only when connected.`;
    } catch (error) {
      remoteRegistryMessage = error instanceof Error ? error.message : String(error);
    }
  }

  function selectManagedInstance(instanceID: string, persist = true) {
    if (remoteEditOpen && instanceID !== remoteEditTargetID) {
      remoteEditOpen = false;
      remoteRemovalConfirmation = '';
    }
    selectedInstanceID = instanceID;
    const selected = managedDaemonInstances.find((instance) => instance.id === instanceID);
    if (selected) {
      apiBaseURL = selected.apiBaseURL;
      localCredentialRef = selected.credentialRef ?? '';
      if (persist && !remoteLifecycleLoading) void persistRemoteSelection().catch(async (error) => {
        remoteRegistryMessage = error instanceof Error ? error.message : String(error);
        await loadRemoteRegistry();
      });
    }
  }

  async function browseRemoteFolderTree(path = remoteBrowsePath || folderFormPath) {
    if (!canBrowseSelectedHostFilesystem) {
      remoteBrowseEntries = [];
      remoteBrowseMessage = `${selectedHostBrowseDisabledReason} Manual path entry remains available when the selected host is offline.`;
      return;
    }
    remoteBrowseLoading = true;
    remoteBrowseMessage = `Browsing directories on ${selectedManagedInstance.label} through the selected remote host's authenticated API.`;
    try {
      const response = await browseFilesystemDirectories(daemonConnectionSettings, path);
      remoteBrowsePath = response.path;
      remoteBrowseEntries = response.entries ?? [];
      remoteBrowseMessage = `Remote folder browse loaded ${remoteBrowseEntries.length} director${remoteBrowseEntries.length === 1 ? 'y' : 'ies'} from ${response.path}; manual path entry remains available when the selected host is offline.`;
    } catch (error) {
      remoteBrowseEntries = [];
      remoteBrowseMessage = `${error instanceof Error ? error.message : String(error)}; manual path entry remains available when the selected host is offline.`;
    } finally {
      remoteBrowseLoading = false;
    }
  }

  function chooseRemoteBrowseEntry(entry: FilesystemBrowseEntry) {
    if (!entry.readable) {
      remoteBrowseMessage = `${entry.path} is not readable from the selected host API; manual path entry remains available when the selected host is offline.`;
      return;
    }
    folderFormPath = entry.path;
    remoteBrowsePath = entry.path;
    remoteBrowseMessage = `Selected ${entry.path} for the folder form. Manual path entry remains available when the selected host is offline.`;
  }

  async function refreshRemoteMeshStatus() {
    remoteMeshStatusMessage = 'Refreshing remote mesh document and pending-change status for the selected host.';
    try {
      const response = await fetchRemoteMeshSettings(daemonConnectionSettings, selectedManagedInstance.id);
      remoteMeshDocuments = response.documents ?? [];
      remoteMeshStatusMessage = `Remote mesh document status refreshed for ${selectedManagedInstance.label}; ${remoteMeshDocuments.length} cached per-node settings document(s) available for online, relay-reachable, and offline instances.`;
    } catch (error) {
      remoteMeshStatusMessage = error instanceof Error ? error.message : String(error);
    }
  }

  async function queueSelectedRemoteMeshSettingsChange() {
    remoteMeshStatusMessage = 'Queueing durable pending settings change for mesh replication.';
    try {
      const queued = await queueRemoteMeshSettingsCommand(daemonConnectionSettings, {
        action: 'queue',
        targetNodeId: selectedManagedInstance.id,
        originNodeId: 'desktop-gui-selected-host',
        idempotencyKey: remoteMeshPendingSettingsChange.idempotencyKey,
        settingsPatch: { logging: { level: configPatchLoggingLevel } }
      });
      remoteMeshPendingChanges = [queued, ...remoteMeshPendingChanges.filter((change) => change.idempotencyKey !== queued.idempotencyKey)];
      remoteMeshStatusMessage = `Pending remote settings changes updated for ${selectedManagedInstance.label}; status ${queued.status}.`;
    } catch (error) {
      remoteMeshStatusMessage = error instanceof Error ? error.message : String(error);
    }
  }

  async function submitRemoteInstanceOnboarding() {
    if (remoteLifecycleLoading) return;
    remoteLifecycleLoading = true;
    remoteOnboardingMessage = '';
    try {
      if (remoteOnboardingSource !== 'api-endpoint-key') {
        throw new Error('Remote host creation from pairing or shared-identity discovery is unavailable until the daemon publishes a live remote-instance read model. Use the daemon-owned pairing import controls without creating a pretend host entry.');
      }
      const credentialRef = `desktop-vault:remote:${crypto.randomUUID()}`;
      const candidate = buildRemoteInstanceOnboardingCandidate({
        source: remoteOnboardingSource,
        label: remoteOnboardingLabel,
        endpoint: remoteOnboardingEndpoint,
        credentialRef,
        pairingCode: remoteOnboardingPairingCode,
        identityFileText: remoteOnboardingIdentityFileText,
        animatedCodeSummary: remoteOnboardingAnimatedCodeSummary,
        sharedIdentityID: remoteOnboardingSharedIdentityID
      });
      if (managedDaemonInstances.some((instance) => instance.id === candidate.id)) {
        throw new Error('That remote host is already registered. Duplicate onboarding is blocked so a failed metadata update cannot delete its existing native credential.');
      }
      const secret = requireNonEmpty(remoteOnboardingAPIKey, 'Remote API key');
      const saved = await queueRemoteRegistryOperation(() => getNativeDesktopShell().onboardRemoteInstance({
        entry: {
          id: candidate.id,
          label: candidate.label,
          apiBaseURL: candidate.apiBaseURL,
          credentialRef,
          source: 'api-endpoint-key',
          connectionState: 'offline',
          revision: 1
        },
        secretValue: secret
      }));
      const nextInstances = addRemoteDaemonInstance(managedDaemonInstances, { ...candidate, revision: saved.instances.find((entry) => entry.id === candidate.id)?.revision ?? 1 });
      remoteCredentialCleanupPending = saved.credentialCleanupPending ?? [];
      persistedRemoteSelectionID = saved.selectedInstanceID ?? '';
      managedDaemonInstances = nextInstances;
      expandedInstanceGroups = defaultExpandedInstanceGroups(managedDaemonInstances);
      selectManagedInstance(candidate.id, false);
      remoteOnboardingMessage = 'Remote host metadata and selection were persisted; raw API keys stay in native credential storage. Connect to load live daemon state.';
    } catch (error) {
      remoteOnboardingMessage = error instanceof Error ? error.message : String(error);
      await loadRemoteRegistry();
    } finally {
      remoteOnboardingAPIKey = '';
      remoteLifecycleLoading = false;
    }
  }

  function beginRemoteHostEdit() {
    if (selectedManagedInstance.kind !== 'remote' || !selectedManagedInstance.credentialRef || !selectedManagedInstance.revision) return;
    remoteEditTargetID = selectedManagedInstance.id;
    remoteEditTargetCredentialRef = selectedManagedInstance.credentialRef;
    remoteEditTargetRevision = selectedManagedInstance.revision;
    remoteEditLabel = selectedManagedInstance.label;
    remoteEditEndpoint = selectedManagedInstance.apiBaseURL;
    remoteRemovalConfirmation = '';
    remoteEditOpen = true;
    remoteOnboardingMessage = 'Credential reference is preserved during metadata edits; changing an API key requires a separate credential-rotation flow.';
  }

  async function saveRemoteHostEdit() {
    if (!remoteEditOpen || !remoteEditTargetID || !remoteEditTargetCredentialRef || !remoteEditTargetRevision) return;
    const targetID = remoteEditTargetID;
    const targetCredentialRef = remoteEditTargetCredentialRef;
    const targetRevision = remoteEditTargetRevision;
    remoteLifecycleLoading = true;
    try {
      const saved = await queueRemoteRegistryOperation(() => getNativeDesktopShell().updateRemoteInstance({
        id: targetID,
        expectedCredentialRef: targetCredentialRef,
        expectedRevision: targetRevision,
        label: requireNonEmpty(remoteEditLabel, 'Remote label'),
        apiBaseURL: requireNonEmpty(remoteEditEndpoint, 'Remote API endpoint')
      }));
      const edited = saved.instances.find((entry) => entry.id === targetID);
      if (!edited) throw new Error('Native edit completed without the edited remote host.');
      remoteCredentialCleanupPending = saved.credentialCleanupPending ?? [];
      persistedRemoteSelectionID = saved.selectedInstanceID ?? '';
      managedDaemonInstances = ensureLocalInstanceFirst(managedDaemonInstances.map((instance) => instance.id === edited.id ? {
        ...instance,
        label: edited.label,
        apiBaseURL: edited.apiBaseURL,
        credentialRef: edited.credentialRef,
        revision: edited.revision,
        connectionState: 'offline',
        statusSummary: 'Remote host metadata updated. Credential reference is preserved; reconnect to refresh live state.'
      } : instance));
      selectManagedInstance(edited.id, false);
      remoteEditOpen = false;
      remoteOnboardingMessage = 'Remote host metadata updated safely. Credential reference is preserved and the host was set offline until reconnect.';
    } catch (error) {
      remoteOnboardingMessage = error instanceof Error ? error.message : String(error);
      await loadRemoteRegistry();
    } finally {
      remoteLifecycleLoading = false;
    }
  }

  async function removeSelectedRemoteHost() {
    if (!remoteEditOpen || !remoteEditTargetID || !remoteEditTargetCredentialRef || !remoteEditTargetRevision) return;
    const removedID = remoteEditTargetID;
    const credentialRef = remoteEditTargetCredentialRef;
    const targetRevision = remoteEditTargetRevision;
    remoteLifecycleLoading = true;
    try {
      const saved = await queueRemoteRegistryOperation(() => getNativeDesktopShell().removeRemoteInstance({ id: removedID, expectedCredentialRef: credentialRef, expectedRevision: targetRevision, confirmLabel: remoteRemovalConfirmation }));
      remoteCredentialCleanupPending = saved.credentialCleanupPending ?? [];
      persistedRemoteSelectionID = saved.selectedInstanceID ?? '';
      managedDaemonInstances = ensureLocalInstanceFirst(managedDaemonInstances.filter((instance) => instance.kind === 'local' || instance.id !== removedID));
      expandedInstanceGroups = defaultExpandedInstanceGroups(managedDaemonInstances);
      selectManagedInstance(managedDaemonInstances[0].id, false);
      remoteEditOpen = false;
      remoteRemovalConfirmation = '';
      remoteOnboardingMessage = remoteCredentialCleanupPending.includes(credentialRef)
        ? 'Remote host removed. Native credential cleanup could not be verified, so an uncertain-cleanup tombstone was saved and the host was not restored.'
        : `Remote host removed and its native credential deleted. ${saved.instances.length} remote host${saved.instances.length === 1 ? '' : 's'} remain.`;
    } catch (error) {
      remoteOnboardingMessage = error instanceof Error ? error.message : String(error);
      await loadRemoteRegistry();
    } finally {
      remoteLifecycleLoading = false;
    }
  }

  async function refreshOperationalState() {
    try {
      [status, daemonFolders, daemonPeers] = await Promise.all([
        fetchDaemonStatus(daemonConnectionSettings),
        fetchDaemonFolders(daemonConnectionSettings),
        fetchDaemonPeers(daemonConnectionSettings)
      ]);
      const liveName = status?.nodeName || 'Local engine';
      const link = summarizeLocalPeerLink(daemonPeers);
      localEngineConnectionState = link.connectionState;
      localEngineConnectionLabel = link.label;
      if (!advertiseFieldDirty) {
        advertiseOverride = status?.advertiseURL ?? '';
      }
      void refreshConfigInstances();
      await ensureEncryptionCatalog().catch(() => undefined);
      managedDaemonInstances = managedDaemonInstances.map((instance) => instance.id === selectedInstanceID ? {
        ...instance,
        connectionState: link.connectionState,
        pairingState: link.pairingState,
        pairingDetail: link.pairingDetail,
        receiveRemainingBytes: link.receiveRemainingBytes,
        sendRemainingBytes: link.sendRemainingBytes,
        averageReceiveBytesPerSecond: link.averageReceiveBytesPerSecond,
        averageSendBytesPerSecond: link.averageSendBytesPerSecond,
        statusSummary: `${liveName}: ${link.statusSummary}`
      } : instance);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      localEngineConnectionState = 'failed';
      localEngineConnectionLabel = 'Failed';
      managedDaemonInstances = managedDaemonInstances.map((instance) => instance.id === selectedInstanceID ? {
        ...instance,
        connectionState: 'failed',
        statusSummary: message
      } : instance);
      throw error;
    }
  }

  async function connectToDaemon() {
    loading = true;
    errorMessage = '';
    status = null;
    try {
      await refreshOperationalState();
    } catch (error) {
      managedDaemonInstances = managedDaemonInstances.map((instance) => instance.id === selectedInstanceID ? {
        ...instance,
        connectionState: 'failed',
        statusSummary: error instanceof Error ? error.message : String(error)
      } : instance);
      errorMessage = error instanceof Error ? error.message : String(error);
    } finally {
      loading = false;
    }
  }

  async function refreshDaemonConfig() {
    loading = true;
    controlMessage = '';
    markLocalControlOperationPending('config', 'config read');
    try {
      daemonConfig = await readDaemonConfig(daemonConnectionSettings);
      markLocalControlOperationCompleted('config', 'config read', { status: 'completed', message: 'Loaded redacted daemon config through the local encrypted API.' });
      controlMessage = 'Loaded redacted daemon config through the local encrypted API.';
    } catch (error) {
      markLocalControlOperationFailed('config', 'config read', error);
      controlMessage = error instanceof Error ? error.message : String(error);
    } finally {
      loading = false;
    }
  }

  async function refreshAPITrustStatus() {
    loading = true;
    controlMessage = '';
    markLocalControlOperationPending('apiTrust', 'API certificate trust status');
    try {
      apiTrustStatus = await fetchAPITrustStatus(daemonConnectionSettings);
      markLocalControlOperationCompleted('apiTrust', 'API certificate trust status', apiTrustStatus);
      controlMessage = apiTrustStatus.message ?? 'Loaded API certificate trust status without exposing API keys or private key material.';
    } catch (error) {
      markLocalControlOperationFailed('apiTrust', 'API certificate trust status', error);
      controlMessage = error instanceof Error ? error.message : String(error);
    } finally {
      loading = false;
    }
  }

  async function pinActiveAPICertificateForSelectedHost() {
    await runControlCommand('apiTrust', 'pin active API certificate', async () => {
      const response = await pinActiveAPICertificate(daemonConnectionSettings);
      apiTrustStatus = response;
      return response;
    });
  }

  function requireNonEmpty(value: string, label: string): string {
    const trimmed = value.trim();
    if (trimmed.length === 0) {
      throw new Error(`${label} is required.`);
    }
    return trimmed;
  }

  function validatePeerCommandForm() {
    const id = requireNonEmpty(peerFormID, 'Peer ID');
    const endpoint = peerFormAction === 'remove' ? undefined : requireNonEmpty(peerFormEndpoint, 'Peer endpoint');
    return { action: peerFormAction, id, endpoint };
  }

  function validateFolderCommandForm() {
    const id = requireNonEmpty(folderFormID, 'Folder ID');
    const path = folderFormAction === 'remove' ? undefined : requireNonEmpty(folderFormPath, 'Folder path');
    const sharedWith = folderFormAction === 'remove' ? undefined : folderFormSharedWith.split(',').map((part) => part.trim()).filter((part) => part.length > 0);
    return { action: folderFormAction, id, path, mode: folderFormMode, sharedWith };
  }

  function validateDiscoveryCommandForm() {
    if (discoveryFormDisabled && (discoveryFormDHT || discoveryFormLocal)) {
      throw new Error('Discovery disabled cannot be combined with active DHT or local discovery.');
    }
    return { action: 'update' as const, disabled: discoveryFormDisabled, dht: discoveryFormDHT, local: discoveryFormLocal };
  }

  function validateTransferCommandForm() {
    const folderID = transferFormFolderID.trim() || undefined;
    const peerID = transferFormPeerID.trim() || undefined;
    if (!folderID && !peerID) {
      throw new Error('Transfer commands require a folder ID, peer ID, or both.');
    }
    return { action: transferFormAction, folderID, peerID };
  }

  function validateMaintenanceCommandForm() {
    return { folderId: maintenanceFormFolderID.trim() || undefined };
  }

  function validateWebGUICommandForm() {
    return { action: webGUIFormAction };
  }

  async function ensureEncryptionCatalog() {
    if (!hasDaemonCredential) return;
    if (encryptionProfiles.length === 0) {
      const catalog = await fetchEncryptionProfiles(daemonConnectionSettings);
      encryptionProfiles = catalog.profiles ?? [];
      identityExchangeEncryptionLevel = catalog.identityExchangeLevel ?? 10;
      if (!encryptionLevelDirty && !encryptionLevelLoaded && typeof catalog.dataLevel === 'number') {
        selectedEncryptionLevel = String(catalog.dataLevel);
      }
    }
    if (!encryptionLevelDirty && !encryptionLevelLoaded) {
      const cfg = await readDaemonConfig(daemonConnectionSettings);
      const identity = cfg.identity;
      if (identity && typeof identity === 'object' && 'encryptionLevel' in identity) {
        const level = (identity as { encryptionLevel?: unknown }).encryptionLevel;
        if (typeof level === 'number') selectedEncryptionLevel = String(level);
      }
      encryptionLevelLoaded = true;
    }
  }

  function selectedEncryptionProfile(): EncryptionProfile | undefined {
    return encryptionProfiles.find((profile) => String(profile.level) === selectedEncryptionLevel);
  }

  function validateConfigPatchForm() {
    const level = requireNonEmpty(configPatchLoggingLevel, 'Logging level');
    const patch: { logging: { level: string }; encryptionLevel?: number } = { logging: { level } };
    if (encryptionLevelLoaded || encryptionLevelDirty) {
      const encryptionLevel = Number(selectedEncryptionLevel);
      if (!Number.isInteger(encryptionLevel) || encryptionLevel < 0 || encryptionLevel > 10) {
        throw new Error('Encryption level must be an integer from 0 through 10.');
      }
      patch.encryptionLevel = encryptionLevel;
    }
    return patch;
  }

  async function generatePairingPackage() {
    pairingLoading = true;
    pairingMessage = '';
    try {
      await adoptBrowserReachableURL();
      pairingPackage = await generateIdentityPairingPackage(daemonConnectionSettings, pairingGroupID);
      pairingExportText = exportIdentityPackageAsCopyableText(pairingPackage);
      const download = buildIdentityPackageDownload(pairingPackage);
      pairingDownloadFilename = download.filename;
      const qrFallback = buildIdentityPairingQRFallbackPayload(pairingPackage);
      pairingQRFallbackPayload = qrFallback.dataURL;
      qrFallbackImageModel = buildIdentityPairingQRImageModel(pairingPackage);
      const animatedFrames = await buildAnimatedPairingFrames(pairingPackage);
      pairingAnimatedFrameList = animatedFrames.map((frame) => JSON.stringify(frame)).join('\n');
      const visual = animatedPairingCodeDescriptor(pairingPackage);
      pairingMessage = `Generated copyable pairing text, downloadable identity file ${download.filename}, QR fallback payload, and ${animatedFrames.length} animated visual code frame list entries for ${visual.mode}.`;
      animatedPairingScanState = createAnimatedPairingScanState();
      animatedScanProgress = animatedPairingScanState.progress;
      desktopAnimatedPairingScannerScreen = buildDesktopAnimatedPairingScannerScreen(
        buildDesktopAnimatedPairingCameraCaptureState({ cameraPermission: 'unknown', scannerState: animatedPairingScanState })
      );
    } catch (error) {
      pairingMessage = error instanceof Error ? error.message : String(error);
    } finally {
      pairingLoading = false;
    }
  }

  function parsePairingImportText() {
    try {
      const imported = parseImportedIdentityPackageText(pairingImportText);
      parsedPairingImport = imported;
      pairingImportSummary = `Ready to import identity package for group ${imported.groupId} from ${imported.discoveryId}.`;
      pairingMessage = 'Parsed pasted or uploaded identity file. Use Import identity package to run daemon-owned import execution through the encrypted API.';
    } catch (error) {
      parsedPairingImport = null;
      pairingImportSummary = '';
      pairingMessage = error instanceof Error ? error.message : String(error);
    }
  }

  function applyOnboardingView(view: { instances?: typeof configInstances; onboarding?: typeof onboardingCommands }) {
    if (view.instances) {
      configInstances = view.instances;
    }
    if (view.onboarding) {
      onboardingCommands = view.onboarding;
    }
  }

  function onboardingStateText(instance: { label: string; local: boolean; online: boolean } | undefined): string {
    if (!instance) {
      return 'Select an instance. That peer issues and accepts the one-time pairing code.';
    }
    if (instance.local) {
      return `${instance.label} performs onboarding on this engine. It generates the one-time pairing code and accepts a new device now.`;
    }
    if (instance.online) {
      return `${instance.label} performs onboarding. This engine relays the pairing code and the accept over the mesh, and ${instance.label} runs them while it is online.`;
    }
    return `${instance.label} is offline. This engine relays onboarding over the mesh. ${instance.label} generates the pairing code and accepts the new device when it is online.`;
  }

  async function showPairingCode() {
    const target = selectedSidebarInstance;
    if (pairingLoading) {
      return;
    }
    if (!target || target.instanceId === 'local') {
      pairingMessage = 'Wait for the linked instances to load, then choose the peer that should issue the code.';
      return;
    }
    pairingLoading = true;
    pairingMessage = 'Requesting a pairing code from this engine…';
    try {
      const response = await queueOnboarding(daemonConnectionSettings, { targetInstanceId: target.instanceId, action: 'issue' });
      applyOnboardingView(response);
      const command = response.command;
      if (command?.status === 'done' && command.code) {
        pairingCode = command.code;
        pairingMessage = `Pairing code ${command.code} is ready. It expires at ${command.expiresAt ?? 'the configured time'} and can be used once.`;
      } else if (command?.status === 'pending') {
        pairingCode = '';
        pairingMessage = `Queued for ${target.label}. This engine relays it over the mesh. ${target.label} generates the pairing code when it is online.`;
      } else {
        pairingCode = '';
        pairingMessage = command?.message || `Onboarding did not finish on ${target.label}.`;
      }
    } catch (error) {
      pairingMessage = error instanceof Error ? error.message : String(error);
    } finally {
      pairingLoading = false;
    }
  }

  async function redeemRemotePairingCode() {
    const target = selectedSidebarInstance;
    if (pairingLoading) {
      return;
    }
    if (!target || target.instanceId === 'local') {
      pairingMessage = 'Wait for the linked instances to load, then choose the peer that should accept the new device.';
      return;
    }
    pairingLoading = true;
    pairingMessage = 'Linking with that pairing code…';
    try {
      const response = await queueOnboarding(daemonConnectionSettings, {
        targetInstanceId: target.instanceId,
        action: 'accept',
        code: remotePairingCode
      });
      applyOnboardingView(response);
      const command = response.command;
      if (command?.status === 'done') {
        pairingMessage = command.message || 'paired by code';
      } else if (command?.status === 'pending') {
        pairingMessage = `Queued for ${target.label}. This engine relays the accept over the mesh. ${target.label} links the new device when it is online.`;
      } else {
        pairingMessage = command?.message || `Onboarding did not finish on ${target.label}.`;
      }
    } catch (error) {
      pairingMessage = error instanceof Error ? error.message : String(error);
    } finally {
      pairingLoading = false;
    }
  }

  async function importParsedPairingPackage() {
    pairingLoading = true;
    try {
      const imported = parsedPairingImport ?? parseImportedIdentityPackageText(pairingImportText);
      parsedPairingImport = imported;
      const response = await importIdentityPairingPackage(daemonConnectionSettings, imported);
      if ((response.message ?? '').includes('already paired with this engine')) {
        pairingImportSummary = '';
        pairingMessage = response.message ?? 'already paired with this engine';
      } else {
        pairingImportSummary = `Imported identity package for group ${response.groupId} from ${response.remoteDiscoveryId}; peer-pair key ${response.keyId ?? 'pending'} prepared at level ${response.peerPairEncryptionLevel}.`;
        pairingMessage = response.message ?? 'Identity package import accepted by daemon-owned import execution.';
      }
    } catch (error) {
      pairingMessage = error instanceof Error ? error.message : String(error);
    } finally {
      pairingLoading = false;
    }
  }

  async function handlePairingFileUpload(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) {
      return;
    }
    pairingImportText = await file.text();
    parsePairingImportText();
  }

  function publishOperationStatusText(area: LocalControlArea, state: LocalControlOperationState) {
    const extra = state.detail && !state.summary.includes(state.detail) ? ` ${state.detail}` : '';
    const text = `${state.phase}: ${state.summary}${extra}`;
    if (area === 'peer') peerOperationStatusText = text;
    else if (area === 'folder') folderOperationStatusText = text;
    else if (area === 'discovery') discoveryOperationStatusText = text;
    else if (area === 'transfer') transferOperationStatusText = text;
    else if (area === 'maintenance') maintenanceOperationStatusText = text;
    else if (area === 'webGUI') webGUIOperationStatusText = text;
    else if (area === 'config') configOperationStatusText = text;
    else apiTrustOperationStatusText = text;
  }

  function setLocalControlOperationState(area: LocalControlArea, state: LocalControlOperationState) {
    localControlOperationStates = { ...localControlOperationStates, [area]: state };
    publishOperationStatusText(area, state);
  }

  function containerInternalHost(host: string): boolean {
    const match = host.match(/^172\.(\d+)\./);
    if (!match) {
      return false;
    }
    const second = Number(match[1]);
    return second >= 17 && second <= 31;
  }

  function browserHostAdvertiseURL(apiPort: string): string {
    if (typeof location === 'undefined') {
      return '';
    }
    const host = location.hostname;
    if (!host || host === 'localhost' || host === '127.0.0.1' || host === '::1' || containerInternalHost(host)) {
      return '';
    }
    const bracketed = host.includes(':') ? `[${host}]` : host;
    return `https://${bracketed}:${apiPort}`;
  }

  function publishedAPIPort(): string {
    if (status?.publishedAPIPort && status.publishedAPIPort > 0) {
      return String(status.publishedAPIPort);
    }
    const listen = status?.apiListen ?? '';
    const match = listen.match(/:(\d+)$/);
    return match?.[1] || '22420';
  }

  async function adoptBrowserReachableURL() {
    if (!dockerGUI) {
      return;
    }
    const current = status?.advertiseURL ?? '';
    if (current && !containerInternalHost(current.replace(/^https?:\/\//, '').replace(/:\d+$/, '').replace(/^\[|\]$/g, ''))) {
      return;
    }
    const candidate = browserHostAdvertiseURL(publishedAPIPort());
    if (!candidate || candidate === current) {
      return;
    }
    await patchDaemonConfig(daemonConnectionSettings, { api: { advertiseURL: candidate } });
    advertiseFieldDirty = false;
    await refreshOperationalState();
  }

  async function saveAdvertiseOverride() {
    const value = advertiseOverride.trim();
    await runControlCommand('config', 'advertise URL', async () => patchDaemonConfig(daemonConnectionSettings, { api: { advertiseURL: value } }));
    advertiseFieldDirty = false;
    await refreshOperationalState();
  }

  function getLocalControlOperationState(area: LocalControlArea): LocalControlOperationState {
    return localControlOperationStates[area];
  }

  function renderLocalControlOperationSummary(state: LocalControlOperationState): string {
    return `${state.phase}: ${state.summary}`;
  }

  function commandResponseDetail(response: unknown): string | undefined {
    const commandResponse = response as CommandResponse | undefined;
    if (!commandResponse || typeof commandResponse !== 'object') {
      return undefined;
    }
    return commandResponse.message ?? commandResponse.status;
  }

  function markLocalControlOperationPending(area: LocalControlArea, command: string) {
    setLocalControlOperationState(area, {
      phase: 'pending',
      command,
      summary: `${command} request is pending.`,
    });
  }

  function markLocalControlOperationAccepted(area: LocalControlArea, command: string, response: unknown) {
    setLocalControlOperationState(area, {
      phase: 'accepted',
      command,
      summary: `${command} request was accepted by the daemon API.`,
      detail: commandResponseDetail(response)
    });
  }

  function markLocalControlOperationFailed(area: LocalControlArea, command: string, error: unknown) {
    setLocalControlOperationState(area, {
      phase: 'failed',
      command,
      summary: `${command} request failed.`,
      detail: error instanceof Error ? error.message : String(error)
    });
  }

  function markLocalControlOperationCompleted(area: LocalControlArea, command: string, response: unknown) {
    const detail = commandResponseDetail(response);
    setLocalControlOperationState(area, {
      phase: 'completed',
      command,
      summary: detail ? `${command} request completed. ${detail}` : `${command} request completed and status was refreshed.`,
      detail
    });
  }

  async function runControlCommand(area: LocalControlArea, command: string, action: () => Promise<unknown>) {
    loading = true;
    controlMessage = '';
    markLocalControlOperationPending(area, command);
    try {
      const response = await action();
      markLocalControlOperationAccepted(area, command, response);
      controlMessage = `Local engine command accepted: ${command}`;
      await refreshOperationalState();
      markLocalControlOperationCompleted(area, command, response);
    } catch (error) {
      markLocalControlOperationFailed(area, command, error);
      controlMessage = error instanceof Error ? error.message : String(error);
    } finally {
      loading = false;
    }
  }

  async function submitPeerForm() {
    const settings = daemonConnectionSettings;
    await runControlCommand('peer', `peer ${peerFormAction}`, async () => sendPeerCommand(settings, validatePeerCommandForm()));
  }

  async function refreshConfigInstances() {
    try {
      const view = await fetchConfigSync(daemonConnectionSettings);
      configInstances = view.instances ?? [];
      onboardingCommands = view.onboarding ?? [];
      if (!configInstanceTouched) {
        selectedConfigInstanceId = view.localInstanceId;
      }
    } catch {
      configInstances = configInstances;
    }
  }

  function selectConfigInstance(instanceId: string) {
    if (!configInstances.some((instance) => instance.instanceId === instanceId)) {
      return;
    }
    configInstanceTouched = true;
    selectedConfigInstanceId = instanceId;
    pairingCode = '';
    pairingMessage = '';
    addPeerMenuOpen = false;
    linkPeerFormOpen = false;
    expandedFolderIds = {};
    folderRowMessage = '';
    if (folderEditor) {
      folderEditor = null;
    }
  }

  function closeAddPeerMenu() {
    addPeerMenuOpen = false;
    linkPeerFormOpen = false;
  }

  function toggleAddPeerMenu() {
    addPeerMenuOpen = !addPeerMenuOpen;
    if (!addPeerMenuOpen) {
      linkPeerFormOpen = false;
    }
  }

  async function showPairingCodeFromMenu() {
    closeAddPeerMenu();
    await showPairingCode();
  }

  async function redeemPairingCodeFromMenu() {
    closeAddPeerMenu();
    await redeemRemotePairingCode();
  }

  function folderStatusText(folder: DaemonFolder): string {
    if (folder.enabled === false) {
      return 'paused';
    }
    return folder.status || 'configured';
  }

  function folderProgressText(folder: DaemonFolder): string {
    if (typeof folder.progressLabel === 'string' && folder.progressLabel.length > 0) {
      return folder.progressLabel;
    }
    if (folder.enabled === false) {
      return 'Paused';
    }
    if (folder.status === 'syncing') {
      return 'Syncing';
    }
    return folder.status || 'Unknown';
  }

  function folderSizeText(folder: DaemonFolder): string {
    if (typeof folder.localBytes === 'number') {
      return formatTransferByteSummary(folder.localBytes);
    }
    return '—';
  }

  function folderModeLabel(mode: string | undefined): string {
    if (mode === 'sendonly') {
      return 'send';
    }
    if (mode === 'recvonly') {
      return 'recv';
    }
    return mode || 'sendrecv';
  }

  function folderShareText(folder: DaemonFolder): string {
    const peers = folder.sharedWith ?? [];
    if (peers.length === 0) {
      return 'Every paired peer';
    }
    return peers.join(', ');
  }

  function toggleFolder(id: string) {
    expandedFolderIds = { ...expandedFolderIds, [id]: !expandedFolderIds[id] };
  }

  function formatPeerRate(bytes: number | undefined): string {
    return `${formatTransferByteSummary(bytes ?? 0)}/s`;
  }

  function formatPeerPending(bytes: number | undefined): string {
    return `${formatTransferByteSummary(bytes ?? 0)} pending`;
  }

  $: sidebarInstances = (configInstances.length > 0 ? configInstances : [{
    instanceId: 'local',
    label: status?.nodeName ?? 'Loading',
    online: localEngineConnectionState === 'online' || localEngineConnectionState === 'paired',
    pendingChanges: 0,
    local: true,
    averageBytesPerSecond: 0,
    pendingBytes: 0
  } satisfies ConfigSyncInstance]).map((instance) => ({
    ...instance,
    selected: selectedConfigInstanceId ? selectedConfigInstanceId === instance.instanceId : instance.local
  }));
  $: selectedSidebarInstance = sidebarInstances.find((instance) => instance.selected) ?? sidebarInstances[0];
  $: visiblePairingCode = pairingCode || onboardingCommands.reduce((found, command) => (
    command.targetInstanceId === selectedSidebarInstance?.instanceId && command.action === 'issue' && command.status === 'done' && command.code
      ? command.code
      : found
  ), '');

  function toggleTheme() {
    theme = theme === 'dark' ? 'light' : 'dark';
  }

  function openNodeOptions() {
    nodeOptionsOpen = true;
  }

  function closeNodeOptions() {
    nodeOptionsOpen = false;
  }

  function openFolderEditor(folder: DaemonFolder) {
    nodeOptionsOpen = false;
    folderEditor = folder;
    folderEditPath = folder.path ?? '';
    folderEditMode = folder.mode || 'sendrecv';
    folderEditIgnore = (folder.ignore ?? []).join('\n');
    folderEditSharedPeers = [...(folder.sharedWith ?? [])];
    folderEditPeerDraft = '';
    folderEditorMessage = '';
    folderEditorRemoveConfirm = false;
  }

  function addFolderSharedPeer() {
    const peer = folderEditPeerDraft.trim();
    if (peer.length === 0 || folderEditSharedPeers.includes(peer)) {
      folderEditPeerDraft = '';
      return;
    }
    folderEditSharedPeers = [...folderEditSharedPeers, peer];
    folderEditPeerDraft = '';
  }

  function removeFolderSharedPeer(peer: string) {
    folderEditSharedPeers = folderEditSharedPeers.filter((id) => id !== peer);
  }

  async function runFolderEditor(command: string, action: () => Promise<CommandResponse>) {
    loading = true;
    folderEditorMessage = `${command} is pending.`;
    try {
      const response = await action();
      folderEditorMessage = response.message ?? `${command} completed.`;
      await refreshOperationalState();
      const current = daemonFolders.find((folder) => folder.id === folderEditor?.id);
      if (current) {
        folderEditor = current;
      }
    } catch (error) {
      folderEditorMessage = error instanceof Error ? error.message : String(error);
    } finally {
      loading = false;
    }
  }

  async function saveFolderEditor() {
    if (!folderEditor) {
      return;
    }
    const id = folderEditor.id;
    const ignore = folderEditIgnore.split(/\r?\n/).map((line) => line.trim()).filter((line) => line.length > 0);
    await runFolderEditor('folder update', async () => sendFolderCommand(daemonConnectionSettings, {
      action: 'update',
      id,
      path: folderEditPath.trim(),
      mode: folderEditMode,
      sharedWith: folderEditSharedPeers,
      ignore
    }));
  }

  async function runFolderRow(folder: DaemonFolder, command: string, action: () => Promise<CommandResponse>) {
    loading = true;
    folderRowMessage = `${command} is pending.`;
    try {
      const response = await action();
      folderRowMessage = response.message ?? `${command} completed.`;
      await refreshOperationalState();
      const current = daemonFolders.find((item) => item.id === folder.id);
      if (current && folderEditor?.id === current.id) {
        folderEditor = current;
      }
    } catch (error) {
      folderRowMessage = error instanceof Error ? error.message : String(error);
    } finally {
      loading = false;
    }
  }

  async function pauseOrResumeFolderRow(folder: DaemonFolder) {
    const action: 'pause' | 'resume' = folder.enabled === false ? 'resume' : 'pause';
    await runFolderRow(folder, `folder ${action}`, async () => {
      const updated = await sendFolderCommand(daemonConnectionSettings, { action, id: folder.id });
      try {
        await sendTransferCommand(daemonConnectionSettings, { action, folderID: folder.id });
      } catch {
        return { ...updated, message: `${updated.message ?? ''} In-progress transfers were not updated.`.trim() };
      }
      return updated;
    });
  }

  async function rescanFolderRow(folder: DaemonFolder) {
    await runFolderRow(folder, 'folder rescan', () => sendFolderCommand(daemonConnectionSettings, { action: 'rescan', id: folder.id }));
  }

  async function pauseOrResumeFolder(action: 'pause' | 'resume') {
    if (!folderEditor) {
      return;
    }
    const id = folderEditor.id;
    await runFolderEditor(`folder ${action}`, async () => {
      const updated = await sendFolderCommand(daemonConnectionSettings, { action, id });
      try {
        await sendTransferCommand(daemonConnectionSettings, { action, folderID: id });
      } catch {
        return { ...updated, message: `${updated.message ?? ''} In-progress transfers were not updated.`.trim() };
      }
      return updated;
    });
  }

  async function removeFolderFromEditor() {
    if (!folderEditor) {
      return;
    }
    if (!folderEditorRemoveConfirm) {
      folderEditorRemoveConfirm = true;
      return;
    }
    const id = folderEditor.id;
    folderEditorRemoveConfirm = false;
    await runFolderEditor('folder remove', async () => sendFolderCommand(daemonConnectionSettings, { action: 'remove', id }));
    if (!folderEditorMessage.toLowerCase().includes('fail') && !folderEditorMessage.toLowerCase().includes('not found')) {
      folderEditor = null;
    }
  }

  async function submitFolderForm() {
    if (folderFormAction === 'remove' && !folderRemoveArmed) {
      folderRemoveConfirm = true;
      return;
    }
    folderRemoveConfirm = false;
    folderRemoveArmed = false;
    const settings = daemonConnectionSettings;
    await runControlCommand('folder', `folder ${folderFormAction}`, async () => sendFolderCommand(settings, validateFolderCommandForm()));
  }

  async function submitDiscoveryForm() {
    const settings = daemonConnectionSettings;
    await runControlCommand('discovery', 'discovery update', async () => sendDiscoveryCommand(settings, validateDiscoveryCommandForm()));
  }

  async function submitTransferForm() {
    const settings = daemonConnectionSettings;
    await runControlCommand('transfer', `transfer ${transferFormAction}`, async () => sendTransferCommand(settings, validateTransferCommandForm()));
  }

  async function submitMaintenanceForm() {
    const settings = daemonConnectionSettings;
    await runControlCommand('maintenance', 'maintenance scrub', async () => runMaintenanceScrub(settings, validateMaintenanceCommandForm()));
  }

  async function submitWebGUIForm() {
    const settings = daemonConnectionSettings;
    await runControlCommand('webGUI', `web GUI ${webGUIFormAction}`, async () => sendWebGUICommand(settings, validateWebGUICommandForm()));
  }

  async function submitConfigPatchForm() {
    const settings = daemonConnectionSettings;
    await ensureEncryptionCatalog().catch(() => undefined);
    const target = configInstances.find((instance) => instance.instanceId === selectedConfigInstanceId);
    if (target && !target.local) {
      await runControlCommand('config', 'config patch', async () => {
        const view = await queueConfigSyncEdit(settings, {
          targetInstanceId: target.instanceId,
          path: 'logging.level',
          value: configPatchLoggingLevel
        });
        configInstances = view.instances ?? configInstances;
        return { status: 'queued', message: target.online ? `Queued for ${target.label}. It applies on that engine.` : `Queued for ${target.label}. It stays pending until that engine is online, and any online linked engine can relay it.` };
      });
      return;
    }
    await runControlCommand('config', 'config patch', async () => patchDaemonConfig(settings, validateConfigPatchForm()));
  }

  async function refreshWarningsAndLogs() {
    loading = true;
    try {
      const response = await fetchDaemonLogs(daemonConnectionSettings);
      daemonEvents = [...(response.entries ?? [])].reverse();
      warningsMessage = `Loaded ${daemonEvents.length} recent daemon event${daemonEvents.length === 1 ? '' : 's'}.`;
    } catch (error) {
      warningsMessage = error instanceof Error ? error.message : String(error);
    } finally {
      loading = false;
    }
  }

  async function refreshTransfers() {
    loading = true;
    try {
      daemonTransfers = await fetchDaemonTransfers(daemonConnectionSettings);
      transfersMessage = `Loaded ${daemonTransfers.active.length} active transfer pass${daemonTransfers.active.length === 1 ? '' : 'es'} and ${daemonTransfers.history.length} recent outcome${daemonTransfers.history.length === 1 ? '' : 's'}.`;
    } catch (error) {
      transfersMessage = error instanceof Error ? error.message : String(error);
    } finally {
      loading = false;
    }
  }

  function selectedRestorePaths(): string[] | undefined {
    const paths = restorePaths.split(/\r?\n|,/).map((path) => path.trim()).filter(Boolean);
    return paths.length ? paths : undefined;
  }

  async function refreshSnapshotsAndJobs() {
    loading = true;
    try {
      const [snapshots, jobs] = await Promise.all([
        sendSnapshotCommand(daemonConnectionSettings, { action: 'list' }),
        fetchBackupJobs(daemonConnectionSettings, selectedSnapshotID)
      ]);
      snapshotMarkers = snapshots.markers ?? [];
      backupJobsSummary = `${jobs.restoreJobs?.length ?? 0} restore, ${jobs.retentionJobs?.length ?? 0} retention, ${jobs.repairJobs?.length ?? 0} repair job(s)`;
      maintenanceBackupMessage = `Loaded ${snapshotMarkers.length} snapshot marker(s) and ${backupJobsSummary}.`;
    } catch (error) {
      maintenanceBackupMessage = error instanceof Error ? error.message : String(error);
    } finally {
      loading = false;
    }
  }

  async function createSnapshot() {
    loading = true;
    try {
      const response = await sendSnapshotCommand(daemonConnectionSettings, { action: 'create', folderId: requireNonEmpty(snapshotFolderID, 'Folder ID'), description: snapshotDescription.trim() || undefined });
      snapshotMarkers = response.markers ?? [];
      maintenanceBackupMessage = 'Snapshot marker created; backup protection may continue asynchronously and is shown in daemon status/jobs.';
    } catch (error) {
      maintenanceBackupMessage = error instanceof Error ? error.message : String(error);
    } finally { loading = false; }
  }

  async function previewRestore() {
    loading = true;
    try {
      restorePlan = await planSnapshotRestore(daemonConnectionSettings, { snapshotId: requireNonEmpty(selectedSnapshotID, 'Snapshot ID'), paths: selectedRestorePaths(), alternatePath: restoreDestination.trim() || undefined });
      reviewedRestoreRequestKey = currentRestoreRequestKey;
      maintenanceBackupMessage = `Restore preview contains ${restorePlan.files?.length ?? 0} file(s); no files were changed.`;
    } catch (error) {
      restorePlan = null;
      reviewedRestoreRequestKey = '';
      maintenanceBackupMessage = error instanceof Error ? error.message : String(error);
    } finally { loading = false; }
  }

  async function executeRestore() {
    loading = true;
    try {
      if (!restorePlan || reviewedRestoreRequestKey !== currentRestoreRequestKey) throw new Error('Restore inputs changed after preview; preview the restore again before running it.');
      const response = await runSnapshotRestore(daemonConnectionSettings, { snapshotId: requireNonEmpty(selectedSnapshotID, 'Snapshot ID'), paths: selectedRestorePaths(), alternatePath: restoreDestination.trim() || undefined });
      maintenanceBackupMessage = `Restore accepted: ${response.restoredFiles ?? 0}/${response.totalFiles ?? 0} files restored; ${response.remainingFiles ?? 0} remaining.`;
      await refreshSnapshotsAndJobs();
    } catch (error) {
      maintenanceBackupMessage = error instanceof Error ? error.message : String(error);
    } finally { loading = false; }
  }

  async function executeRetention() {
    loading = true;
    try {
      if (!Number.isInteger(retentionKeepLast) || retentionKeepLast < 1) throw new Error('Keep-last count must be at least 1.');
      const response = await runSnapshotRetention(daemonConnectionSettings, retentionKeepLast);
      maintenanceBackupMessage = `Retention accepted; ${response.deprecatedSnapshots ?? 0} snapshot(s) deprecated and ${response.deletedSnapshots ?? 0} deleted.`;
      await refreshSnapshotsAndJobs();
    } catch (error) {
      maintenanceBackupMessage = error instanceof Error ? error.message : String(error);
    } finally { loading = false; }
  }

  async function executeBackupScrub() {
    loading = true;
    try {
      const response = await runBackupScrub(daemonConnectionSettings);
      maintenanceBackupMessage = `Backup scrub completed: ${JSON.stringify(response)}`;
      await refreshSnapshotsAndJobs();
    } catch (error) {
      maintenanceBackupMessage = error instanceof Error ? error.message : String(error);
    } finally { loading = false; }
  }

  function reflectLocalDaemonSession(session: GUIManagedNonServiceDaemonSession) {
    localCredentialRef = session.credentialRef;
    managedDaemonInstances = managedDaemonInstances.map((instance) => instance.kind === 'local' ? {
      ...instance,
      apiBaseURL: session.encryptedApiBaseURL,
      connectionState: session.connectionState === 'running' ? 'online' : 'connecting',
      statusSummary: session.connectionState === 'running'
        ? `${session.nodeName ?? 'Local engine'} is reachable through the native authenticated API bridge.`
        : session.message
    } : instance);
  }

  async function ensureLocalDaemonConnection() {
    lifecycleLoading = true;
    guiOwnedNonServiceDaemonMessage = 'Checking for a reachable local engine…';
    try {
      const runtimeState = await getGUIOwnedNonServiceDaemonState();
      if (runtimeState.connectionState === 'running' && runtimeState.sessionID) {
        const existing = await getGUIOwnedNonServiceDaemonSession();
        if (!existing) throw new Error('The native shell reported a running engine without a reconnectable session.');
        guiOwnedNonServiceDaemonSession = await adoptGUIOwnedNonServiceDaemon(existing.sessionID);
        guiOwnedNonServiceDaemonSession = {
          ...guiOwnedNonServiceDaemonSession,
          connectionState: runtimeState.connectionState,
          nodeName: runtimeState.nodeName
        };
      } else {
        guiOwnedNonServiceDaemonSession = await requestGUIOwnedNonServiceDaemonLaunch({
          sessionMode: 'persistent-user-daemon',
          preferExistingReachableDaemon: true
        });
      }
      reflectLocalDaemonSession(guiOwnedNonServiceDaemonSession);
      guiOwnedNonServiceDaemonMessage = guiOwnedNonServiceDaemonSession.message;
      if (selectedManagedInstance.kind === 'local') {
        apiBaseURL = guiOwnedNonServiceDaemonSession.encryptedApiBaseURL;
        await refreshOperationalState();
      }
    } catch (error) {
      try {
        guiOwnedNonServiceDaemonSession = await requestGUIOwnedNonServiceDaemonLaunch({
          sessionMode: 'persistent-user-daemon',
          preferExistingReachableDaemon: true
        });
        reflectLocalDaemonSession(guiOwnedNonServiceDaemonSession);
        guiOwnedNonServiceDaemonMessage = guiOwnedNonServiceDaemonSession.message;
        if (selectedManagedInstance.kind === 'local') {
          apiBaseURL = guiOwnedNonServiceDaemonSession.encryptedApiBaseURL;
          await refreshOperationalState();
        }
      } catch (launchError) {
        guiOwnedNonServiceDaemonMessage = launchError instanceof Error ? launchError.message : String(launchError);
      }
    } finally {
      lifecycleLoading = false;
    }
  }

  async function inspectBundledEngineResources() {
    bundledEngineInspectionMessage = 'Inspecting packaged engine manifest and hashing present resources…';
    try {
      bundledEngineInspection = await getNativeDesktopShell().inspectBundledEngineResources();
      bundledEngineInspectionMessage = bundledEngineInspection.message;
    } catch (error) {
      bundledEngineInspection = null;
      bundledEngineInspectionMessage = error instanceof Error ? error.message : String(error);
    }
  }

  async function loadDesktopPreferences() {
    try {
      desktopPreferences = await getNativeDesktopShell().getDesktopPreferences();
      desktopPreferencesMessage = 'Desktop preferences loaded from native per-user storage.';
    } catch (error) {
      desktopPreferencesMessage = error instanceof Error ? error.message : String(error);
    }
  }

  async function saveDesktopPreferences() {
    try {
      desktopPreferences = await getNativeDesktopShell().saveDesktopPreferences(desktopPreferences);
      desktopPreferencesMessage = 'Desktop preferences saved to native per-user storage.';
    } catch (error) {
      desktopPreferencesMessage = error instanceof Error ? error.message : String(error);
    }
  }

  async function refreshDesktopAppUpdateStatus() {
    try {
      desktopAppUpdate = await getNativeDesktopShell().getDesktopAppUpdateStatus();
    } catch (error) {
      desktopAppUpdate = {
        platform: 'unsupported',
        phase: 'error',
        currentVersion: '',
        message: error instanceof Error ? error.message : String(error),
        allowDownloadLink: false,
        canRestartNow: false,
        canRetry: true
      };
    }
  }

  async function checkDesktopAppUpdate() {
    desktopAppUpdateBusy = true;
    try {
      desktopAppUpdate = await getNativeDesktopShell().checkDesktopAppUpdate();
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      desktopAppUpdate = {
        platform: desktopAppUpdate?.platform ?? 'unsupported',
        phase: 'error',
        currentVersion: desktopAppUpdate?.currentVersion ?? '',
        availableVersion: desktopAppUpdate?.availableVersion,
        message,
        allowDownloadLink: false,
        canRestartNow: false,
        canRetry: true
      };
    } finally {
      desktopAppUpdateBusy = false;
    }
  }

  async function restartDesktopAppUpdate() {
    desktopAppUpdateBusy = true;
    try {
      desktopAppUpdate = await getNativeDesktopShell().restartDesktopAppUpdate();
    } catch (error) {
      desktopAppUpdate = {
        platform: desktopAppUpdate?.platform ?? 'unsupported',
        phase: 'error',
        currentVersion: desktopAppUpdate?.currentVersion ?? '',
        message: error instanceof Error ? error.message : String(error),
        allowDownloadLink: false,
        canRestartNow: false,
        canRetry: true
      };
    } finally {
      desktopAppUpdateBusy = false;
    }
  }

  async function postponeDesktopAppUpdate() {
    desktopAppUpdateBusy = true;
    try {
      desktopAppUpdate = await getNativeDesktopShell().postponeDesktopAppUpdate();
    } catch (error) {
      desktopAppUpdate = {
        platform: desktopAppUpdate?.platform ?? 'unsupported',
        phase: 'error',
        currentVersion: desktopAppUpdate?.currentVersion ?? '',
        message: error instanceof Error ? error.message : String(error),
        allowDownloadLink: false,
        canRestartNow: false,
        canRetry: true
      };
    } finally {
      desktopAppUpdateBusy = false;
    }
  }

  $: showDesktopUpdateBanner =
    desktopAppUpdate != null &&
    desktopAppUpdate.phase !== 'idle' &&
    desktopAppUpdate.phase !== '';

  async function adoptGUIOwnedNonServiceDaemonSession() {
    lifecycleLoading = true;
    guiOwnedNonServiceDaemonMessage = '';
    try {
      const existing = await getGUIOwnedNonServiceDaemonSession();
      if (!existing) {
        guiOwnedNonServiceDaemonMessage = 'No previous GUI-owned non-service daemon session was recorded for adoption.';
        return;
      }
      guiOwnedNonServiceDaemonSession = await adoptGUIOwnedNonServiceDaemon(existing.sessionID);
      apiBaseURL = guiOwnedNonServiceDaemonSession.encryptedApiBaseURL;
      reflectLocalDaemonSession(guiOwnedNonServiceDaemonSession);
      guiOwnedNonServiceDaemonMessage = guiOwnedNonServiceDaemonSession.message;
      await refreshOperationalState();
    } catch (error) {
      guiOwnedNonServiceDaemonMessage = error instanceof Error ? error.message : String(error);
    } finally {
      lifecycleLoading = false;
    }
  }

  async function launchGUIOwnedNonServiceDaemon() {
    lifecycleLoading = true;
    guiOwnedNonServiceDaemonMessage = '';
    try {
      guiOwnedNonServiceDaemonSession = await requestGUIOwnedNonServiceDaemonLaunch({
        sessionMode: guiOwnedNonServiceDaemonMode,
        preferExistingReachableDaemon: true
      });
      guiOwnedNonServiceDaemonMessage = guiOwnedNonServiceDaemonSession.message;
      apiBaseURL = guiOwnedNonServiceDaemonSession.encryptedApiBaseURL;
      reflectLocalDaemonSession(guiOwnedNonServiceDaemonSession);
      await refreshOperationalState();
    } catch (error) {
      guiOwnedNonServiceDaemonMessage = error instanceof Error ? error.message : String(error);
    } finally {
      lifecycleLoading = false;
    }
  }

  async function restartLocalDaemon() {
    lifecycleLoading = true;
    guiOwnedNonServiceDaemonMessage = 'Restarting local engine through its API…';
    try {
      if (guiOwnedNonServiceDaemonSession?.kind === 'service') {
        const runtimeState = await controlLocalDaemon('restart', guiOwnedNonServiceDaemonSession.sessionID);
        guiOwnedNonServiceDaemonSession = {
          ...guiOwnedNonServiceDaemonSession,
          connectionState: runtimeState.connectionState,
          nodeName: runtimeState.nodeName,
          message: runtimeState.message
        };
      } else {
        if (guiOwnedNonServiceDaemonSession?.pid) {
          await stopGUIOwnedNonServiceDaemonThroughAPI(guiOwnedNonServiceDaemonSession.sessionID);
        }
        guiOwnedNonServiceDaemonSession = await requestGUIOwnedNonServiceDaemonLaunch({
          sessionMode: 'persistent-user-daemon',
          preferExistingReachableDaemon: false
        });
      }
      apiBaseURL = guiOwnedNonServiceDaemonSession.encryptedApiBaseURL;
      reflectLocalDaemonSession(guiOwnedNonServiceDaemonSession);
      guiOwnedNonServiceDaemonMessage = guiOwnedNonServiceDaemonSession.message;
      await refreshOperationalState();
    } catch (error) {
      guiOwnedNonServiceDaemonMessage = error instanceof Error ? error.message : String(error);
    } finally {
      lifecycleLoading = false;
    }
  }

  async function stopGUIOwnedNonServiceDaemon() {
    if (!guiOwnedNonServiceDaemonSession) {
      guiOwnedNonServiceDaemonMessage = 'No GUI-owned non-service daemon session is selected to stop through daemon API.';
      return;
    }
    lifecycleLoading = true;
    guiOwnedNonServiceDaemonMessage = '';
    try {
      if (guiOwnedNonServiceDaemonSession.kind === 'service') {
        const runtimeState = await controlLocalDaemon('stop', guiOwnedNonServiceDaemonSession.sessionID);
        guiOwnedNonServiceDaemonSession = { ...guiOwnedNonServiceDaemonSession, connectionState: 'stopped', message: runtimeState.message };
      } else {
        guiOwnedNonServiceDaemonSession = await stopGUIOwnedNonServiceDaemonThroughAPI(guiOwnedNonServiceDaemonSession.sessionID);
      }
      guiOwnedNonServiceDaemonMessage = guiOwnedNonServiceDaemonSession.message;
      managedDaemonInstances = managedDaemonInstances.map((instance) => instance.kind === 'local'
        ? { ...instance, connectionState: 'offline', statusSummary: guiOwnedNonServiceDaemonMessage }
        : instance);
    } catch (error) {
      guiOwnedNonServiceDaemonMessage = error instanceof Error ? error.message : String(error);
    } finally {
      lifecycleLoading = false;
    }
  }

  onMount(() => {
    document.addEventListener('click', onPairingButtonClick);
    if (window.matchMedia('(prefers-color-scheme: light)').matches) {
      theme = 'light';
    }
    if (dockerGUI) {
      localCredentialRef = 'same-origin';
      const tick = () => refreshOperationalState().catch(() => undefined);
      void tick().then(() => adoptBrowserReachableURL().catch(() => undefined));
      const timer = setInterval(() => {
        void tick();
      }, 3000);
      return () => {
        document.removeEventListener('click', onPairingButtonClick);
        clearInterval(timer);
      };
    }
    void (async () => {
      await loadRemoteRegistry();
      await ensureLocalDaemonConnection();
      await loadDesktopPreferences();
      await checkDesktopAppUpdate();
    })();
    const statusTimer = setInterval(() => {
      void refreshDesktopAppUpdateStatus();
    }, 15000);
    return () => {
      document.removeEventListener('click', onPairingButtonClick);
      clearInterval(statusTimer);
    };
  });
</script>

<main class="host-scoped-shell" data-theme={theme}>
  <aside class="host-sidebar" aria-label="Linked instances" data-testid="instance-switcher">
    <div class="peer-list" role="listbox" aria-label="Linked engines">
      {#each sidebarInstances as instance (instance.instanceId)}
        <button
          type="button"
          class="peer-row"
          class:active={instance.selected}
          aria-current={instance.selected ? 'page' : undefined}
          data-testid="instance-option"
          on:click={() => selectConfigInstance(instance.instanceId)}
        >
          <span class="peer-line">
            <span class="peer-name">
              {#if instance.local}
                <span data-testid="device-node-name">{instance.label}</span><span> · local</span>
              {:else}
                {instance.label}
              {/if}
            </span>
            <strong class="connection-state" data-state={instance.online ? 'online' : 'offline'} data-testid={instance.local ? 'local-engine-connection' : undefined}>{instance.online ? 'Online' : 'Offline'}</strong>
          </span>
          <small class="peer-stats"><span data-testid="instance-average-rate">{formatPeerRate(instance.averageBytesPerSecond)}</span> · <span data-testid="instance-pending-bytes">{formatPeerPending(instance.pendingBytes)}</span></small>
        </button>
      {/each}
    </div>
  </aside>

  <section class="host-content" aria-live="polite">
    {#if showDesktopUpdateBanner && desktopAppUpdate}
      <section class="update-banner" data-phase={desktopAppUpdate.phase} data-platform={desktopAppUpdate.platform} aria-label="Desktop application update">
        <span class="eyebrow">Desktop app update</span>
        <h2>Version {desktopAppUpdate.availableVersion || desktopAppUpdate.currentVersion}</h2>
        <p>{desktopAppUpdate.message}</p>
        <div class="lifecycle-actions">
          {#if desktopAppUpdate.canRestartNow}
            <button type="button" on:click={restartDesktopAppUpdate} disabled={desktopAppUpdateBusy}>Restart now</button>
            <button type="button" on:click={postponeDesktopAppUpdate} disabled={desktopAppUpdateBusy}>Later</button>
          {/if}
          {#if desktopAppUpdate.canRetry}
            <button type="button" on:click={checkDesktopAppUpdate} disabled={desktopAppUpdateBusy}>Retry</button>
          {/if}
        </div>
        <!-- Windows auto-update never uses a download-link CTA; allowDownloadLink is AppImage-only. -->
        {#if desktopAppUpdate.allowDownloadLink && desktopAppUpdate.downloadURL && desktopAppUpdate.platform !== 'windows' && desktopAppUpdate.platform !== 'sparkle'}
          <p><a class="download-link" href={desktopAppUpdate.downloadURL}>Download {desktopAppUpdate.availableVersion}</a></p>
        {/if}
      </section>
    {/if}
    <div class="main-toolbar">
      <button type="button" data-testid="node-options" on:click={openNodeOptions}>Options</button>
      <div class="actions-menu">
        <button type="button" data-testid="add-peer-menu" aria-haspopup="menu" aria-expanded={addPeerMenuOpen ? 'true' : 'false'} on:click={toggleAddPeerMenu}>Add peer</button>
        {#if addPeerMenuOpen}
          <div class="actions-menu-panel" role="menu" aria-label="Add peer">
            <button type="button" role="menuitem" data-testid="show-pairing-code" on:click={showPairingCodeFromMenu} disabled={pairingLoading}>Show pairing code</button>
            <button type="button" role="menuitem" data-testid="link-peer-form" on:click={() => { linkPeerFormOpen = true; }}>Link with pairing code</button>
            {#if linkPeerFormOpen}
              <label>
                Pairing code
                <input data-testid="pairing-code-input" bind:value={remotePairingCode} autocomplete="off" placeholder="ABCD-EFGH-JKMN-PQRS" />
              </label>
              <button type="button" data-testid="redeem-pairing-code" on:click={redeemPairingCodeFromMenu} disabled={pairingLoading}>Link</button>
            {/if}
          </div>
        {/if}
      </div>
      <button type="button" class="theme-toggle" data-testid="theme-toggle" on:click={toggleTheme}>{theme === 'dark' ? 'Light theme' : 'Dark theme'}</button>
    </div>
    {#if addPeerMenuOpen}
      <button type="button" class="menu-backdrop" aria-label="Close add peer menu" on:click={closeAddPeerMenu}></button>
    {/if}
    <section class="onboarding-compact" aria-label="Onboarding" data-testid="onboarding-panel">
      <p data-testid="onboarding-state">{onboardingStateText(selectedSidebarInstance)}</p>
      {#if visiblePairingCode}
        <p data-testid="pairing-code">Pairing code {visiblePairingCode}. Enter this one code on the other engine. It works once and expires if unused. The other engine looks it up on the DHT.</p>
      {/if}
      {#each onboardingCommands.filter((command) => command.targetInstanceId === selectedSidebarInstance?.instanceId && command.status === 'pending') as command (command.id)}
        <p data-testid="onboarding-pending">{command.action === 'accept' ? 'Accept' : 'Pairing code'} is relayed and waiting for {selectedSidebarInstance?.label} to run it online.</p>
      {/each}
      {#if pairingMessage && !nodeOptionsOpen}
        <p data-testid="pairing-message">{pairingMessage}</p>
      {/if}
    </section>
    {#if folderEditor}
      <form class="control-form folder-edit-view" data-testid="folder-editor" aria-label="Folder editor" on:submit|preventDefault={saveFolderEditor}>
        <span class="eyebrow">Folder</span>
        <h2>Edit {folderEditor.id}</h2>
        <p>sendrecv exchanges files both ways. send only uploads. recv only downloads. An empty peer list shares with every paired peer. Ignore patterns use one pattern per line.</p>
        <label>
          Path
          <input bind:value={folderEditPath} name="folderEditPath" data-testid="folder-edit-path" autocomplete="off" />
        </label>
        <label>
          Mode
          <select bind:value={folderEditMode} name="folderEditMode" data-testid="folder-edit-mode">
            <option value="sendrecv">sendrecv</option>
            <option value="sendonly">send</option>
            <option value="recvonly">recv</option>
          </select>
        </label>
        <label>
          Ignore patterns
          <textarea bind:value={folderEditIgnore} name="folderEditIgnore" data-testid="folder-edit-ignore" rows="4"></textarea>
        </label>
        <div>
          <p>Shared peers</p>
          {#if folderEditSharedPeers.length === 0}
            <p>Every paired peer.</p>
          {:else}
            <ul aria-label="Shared peers">
              {#each folderEditSharedPeers as peer}
                <li>
                  <span data-testid="folder-shared-peer">{peer}</span>
                  <button type="button" data-testid="folder-shared-remove" on:click={() => removeFolderSharedPeer(peer)}>Remove {peer}</button>
                </li>
              {/each}
            </ul>
          {/if}
          <label>
            Add peer ID
            <input bind:value={folderEditPeerDraft} name="folderEditPeerDraft" data-testid="folder-shared-add-input" autocomplete="off" />
          </label>
          <button type="button" data-testid="folder-shared-add" on:click={addFolderSharedPeer}>Add shared peer</button>
        </div>
        <div class="lifecycle-actions">
          <button type="submit" data-testid="folder-edit-save" disabled={loading || !hasDaemonCredential}>Save folder</button>
          <button type="button" data-testid="folder-pause" on:click={() => pauseOrResumeFolder('pause')} disabled={loading || !hasDaemonCredential}>Pause folder</button>
          <button type="button" data-testid="folder-resume" on:click={() => pauseOrResumeFolder('resume')} disabled={loading || !hasDaemonCredential}>Resume folder</button>
          <button type="button" data-testid="folder-editor-remove" on:click={removeFolderFromEditor} disabled={loading || !hasDaemonCredential}>Remove folder</button>
          <button type="button" on:click={() => { folderEditor = null; }}>Close</button>
        </div>
        {#if folderEditorRemoveConfirm}
          <div class="confirm-dialog" role="dialog" data-testid="folder-editor-confirm">
            <p>Stop sharing {folderEditor.id}? Files already on disk stay where they are.</p>
            <button type="button" data-testid="folder-editor-confirm-remove" on:click={removeFolderFromEditor}>Remove folder</button>
          </div>
        {/if}
        <p class="operation-status" data-testid="folder-edit-status">{folderEditorMessage}</p>
      </form>
    {:else}
      <section class="connection-card" aria-label="Folders">
        <h2 data-testid="selected-instance-title">{selectedSidebarInstance?.label ?? 'Folders'}</h2>
        {#if selectedSidebarInstance && selectedSidebarInstance.local === false}
          <p class="empty-state">This engine's folders stay on that instance. Options holds its settings.</p>
        {:else if daemonFolders.length === 0}
          <p class="empty-state">No folders are configured.</p>
        {:else}
          <ul class="folder-list" aria-label="Configured folders">
            {#each daemonFolders as folder (folder.id)}
              <li class="folder-row">
                <button type="button" class="folder-summary" data-testid="folder-expand" aria-expanded={expandedFolderIds[folder.id] ? 'true' : 'false'} on:click={() => toggleFolder(folder.id)}>
                  <span class="folder-chevron" aria-hidden="true">{expandedFolderIds[folder.id] ? '▾' : '▸'}</span>
                  <span class="folder-name">{folder.id}</span>
                  <span class="folder-meta">{folderStatusText(folder)}</span>
                  <span class="folder-meta" data-testid="folder-progress">{folderProgressText(folder)}</span>
                  <span class="folder-meta" data-testid="folder-size">{folderSizeText(folder)}</span>
                </button>
                {#if expandedFolderIds[folder.id]}
                  <div class="folder-details">
                    <p>{folder.path || 'No path'}</p>
                    <p>{folderModeLabel(folder.mode)} · {folderShareText(folder)}</p>
                    <div class="lifecycle-actions">
                      <button type="button" data-testid="folder-open" on:click={() => openFolderEditor(folder)}>Edit</button>
                      <button type="button" data-testid="folder-row-pause" on:click={() => pauseOrResumeFolderRow(folder)} disabled={loading || !hasDaemonCredential}>{folder.enabled === false ? 'Resume' : 'Pause'}</button>
                      <button type="button" data-testid="folder-rescan" on:click={() => rescanFolderRow(folder)} disabled={loading || !hasDaemonCredential}>Rescan</button>
                    </div>
                  </div>
                {/if}
              </li>
            {/each}
          </ul>
          {#if folderRowMessage}
            <p class="operation-status" data-testid="folder-row-status">{folderRowMessage}</p>
          {/if}
        {/if}
      </section>
    {/if}
  </section>
  {#if nodeOptionsOpen}
    <div class="node-options-dialog" role="dialog" aria-modal="false" aria-label="Node options" data-testid="node-options-dialog">
      <div class="node-options-bar">
        <div>
          <span class="eyebrow">Node options</span>
          <h2>Options</h2>
        </div>
        <button type="button" data-testid="node-options-close" on:click={closeNodeOptions}>Close</button>
      </div>
      <nav class="node-options-nav" aria-label="Selected host sections">
        {#each viewCatalog.filter((view) => !(dockerGUI && view.id === 'desktop-settings')) as view}
          <button
            type="button"
            class:active={activeView === view.id}
            aria-current={activeView === view.id ? 'page' : undefined}
            data-testid={`nav-${view.id}`}
            on:click={() => activeView = view.id}
          >
            <span>{view.label}</span>
            <small>{view.description}</small>
          </button>
        {/each}
      </nav>
    <section class="host-scope-banner" aria-label="Selected host scope">
      <span class="eyebrow">Selected host scope</span>
      <h2>{selectedHostContext.label}</h2>
      <p>{selectedHostContext.scopeNote}</p>
      <dl class="host-scope-details">
        <div>
          <dt>Host type</dt>
          <dd>{selectedHostContext.kind}</dd>
        </div>
        <div>
          <dt>API endpoint</dt>
          <dd>{selectedHostContext.apiBaseURL}</dd>
        </div>
        <div>
          <dt>Connection</dt>
          <dd>{selectedManagedInstance.kind === 'local' ? localEngineConnectionLabel : formatConnectionStateLabel(selectedHostContext.connectionState)}</dd>
        </div>
      </dl>
    </section>

    {#if activeView === 'overview'}
  <section class="connection-card" aria-label="Engine status">
    <span class="eyebrow">Engine status</span>
    {#if dockerGUI}
      <h2>Device identity</h2>
      <dl class="host-status-grid" aria-label="Device identity">
        <dt>Node</dt><dd>{status?.nodeName ?? 'Loading'}</dd>
        <dt>Identity</dt><dd>{status?.identityPublicKey ?? 'Loading'}</dd>
        <dt>Engine schema</dt><dd>{status?.engineSchemaVersion ?? '—'}</dd>
        <dt>Daemon</dt><dd>{status?.status ?? 'starting'}</dd>
        <dt>Folders</dt><dd>{daemonFolders.length}</dd>
        <dt>Peers</dt><dd>{daemonPeers.length}</dd>
      </dl>
      <p>The native API credential stays inside the daemon.</p>
      <button type="button" on:click={refreshOperationalState}>Refresh folders, peers, and status</button>
    {:else}
    <h2>Local engine</h2>
    <p class:success={guiOwnedNonServiceDaemonSession?.connectionState === 'running'}>
      {guiOwnedNonServiceDaemonSession?.connectionState === 'running' ? 'Running and reachable' : lifecycleLoading ? 'Connecting…' : 'Stopped or unreachable'}
    </p>
    <p>{guiOwnedNonServiceDaemonMessage}</p>
    {#if guiOwnedNonServiceDaemonSession}
      <dl class="host-status-grid" aria-label="Local engine state">
        <dt>Node</dt><dd>{guiOwnedNonServiceDaemonSession.nodeName ?? 'Starting'}</dd>
        <dt>Process</dt><dd>{guiOwnedNonServiceDaemonSession.kind === 'service' ? `${guiOwnedNonServiceDaemonSession.manager ?? 'service manager'} · ${guiOwnedNonServiceDaemonSession.serviceName ?? guiOwnedNonServiceDaemonSession.sessionID}` : guiOwnedNonServiceDaemonSession.pid > 0 ? `PID ${guiOwnedNonServiceDaemonSession.pid}` : 'Stopped'}</dd>
        <dt>API</dt><dd>{guiOwnedNonServiceDaemonSession.encryptedApiBaseURL}</dd>
      </dl>
    {/if}
    <div class="lifecycle-actions">
      <button type="button" on:click={ensureLocalDaemonConnection} disabled={lifecycleLoading || guiOwnedNonServiceDaemonSession?.connectionState === 'running'}>Start local engine</button>
      <button type="button" on:click={stopGUIOwnedNonServiceDaemon} disabled={lifecycleLoading || !guiOwnedNonServiceDaemonSession || guiOwnedNonServiceDaemonSession.connectionState !== 'running'}>Stop</button>
      <button type="button" on:click={restartLocalDaemon} disabled={lifecycleLoading}>Restart connection</button>
    </div>
    <small>The engine runs separately and continues syncing when this window closes.</small>
    {#if status}
      <dl class="host-status-grid" aria-label="Daemon operational summary">
        <dt>Daemon</dt><dd>{status.status ?? 'running'}</dd>
        <dt>Folders</dt><dd>{daemonFolders.length}</dd>
        <dt>Peers</dt><dd>{daemonPeers.length}</dd>
        <dt>Started</dt><dd>{status.startedAt ?? 'unknown'}</dd>
      </dl>
      <button type="button" on:click={refreshOperationalState} disabled={loading || !hasDaemonCredential}>Refresh folders, peers, and status</button>
    {/if}
    {/if}
  </section>

  {#if selectedManagedInstance.kind === 'remote'}
  <section class="connection-card">
    <h2>Remote engine connection</h2>
    <p>Connect to {selectedManagedInstance.label} through its authenticated native credential. API keys are stored only in the OS credential vault and are never entered into the running browser surface.</p>
    <label>API URL<input bind:value={apiBaseURL} name="apiBaseURL" autocomplete="off" /></label>
    <button type="button" on:click={connectToDaemon} disabled={loading || !hasDaemonCredential}>{loading ? 'Connecting…' : 'Connect'}</button>
    {#if errorMessage}<p class="error">{errorMessage}</p>{/if}
    {#if status}<pre>{JSON.stringify(status, null, 2)}</pre>{/if}
    <div class="lifecycle-actions">
      <button type="button" on:click={beginRemoteHostEdit} disabled={remoteLifecycleLoading}>Edit saved host</button>
    </div>
    {#if remoteEditOpen}
      <form class="control-form" on:submit|preventDefault={saveRemoteHostEdit} aria-label="Edit saved remote host">
        <h3>Edit saved remote host</h3>
        <p>Credential reference is preserved during metadata edits. API-key rotation is intentionally separate so a failed metadata save cannot destroy the working credential.</p>
        <label>Remote label<input bind:value={remoteEditLabel} autocomplete="off" /></label>
        <label>Remote HTTPS API endpoint<input bind:value={remoteEditEndpoint} autocomplete="off" /></label>
        <button type="submit" disabled={remoteLifecycleLoading}>Save host metadata</button>
        <button type="button" on:click={() => remoteEditOpen = false} disabled={remoteLifecycleLoading}>Cancel edit</button>
        <fieldset>
          <legend>Remove saved remote host and native credential</legend>
          <p>This removes the registry entry and its OS-native credential. Type the host label exactly to confirm: <strong>{selectedManagedInstance.label}</strong></p>
          <label>Type the host label exactly<input bind:value={remoteRemovalConfirmation} autocomplete="off" /></label>
          <button type="button" class="danger" on:click={removeSelectedRemoteHost} disabled={remoteLifecycleLoading || remoteRemovalConfirmation !== selectedManagedInstance.label}>Remove host and credential</button>
        </fieldset>
      </form>
    {/if}
  </section>
  {/if}
    {/if}

    {#if activeView === 'daemon-settings'}

  <section class="connection-card">
    <h2>Selected-host daemon settings</h2>
    <p>
      Daemon options are scoped to {selectedManagedInstance.label}. Selected-host connection credentials, folder/peer/discovery policy,
      metadata, logging, maintenance, backup, and web GUI package settings stay separate from GUI-only desktop preferences.
    </p>
    <div class="settings-grid" aria-label="Selected-host daemon settings sections">
      {#each daemonSettingsSections as section}
        <article class="settings-card">
          <h3>{section.title}</h3>
          <p>{section.summary}</p>
        </article>
      {/each}
    </div>
  </section>

  <section class="connection-card">
    <h2>Selected-host engine controls</h2>
    <p>
      Normal selected-host engine control stays inside the authenticated daemon API: redacted config, peers, folders, discovery,
      transfers, maintenance, optional web GUI lifecycle, and non-secret config patches do not require manual config-file edits or command-line switches.
    </p>
    <button type="button" on:click={refreshDaemonConfig} disabled={loading || !hasDaemonCredential}>Read redacted config</button>

    <form class="control-form" on:submit|preventDefault={submitPeerForm} aria-label="Peer form">
      <h3>Peer form</h3>
      <label>
        Action
        <select bind:value={peerFormAction} name="peerFormAction">
          <option value="add">add</option>
          <option value="update">update</option>
          <option value="remove">remove</option>
        </select>
      </label>
      <label>
        Peer ID
        <input bind:value={peerFormID} name="peerFormID" autocomplete="off" />
      </label>
      <label>
        Peer endpoint
        <input bind:value={peerFormEndpoint} name="peerFormEndpoint" autocomplete="off" placeholder="tcp://host:port or https://host" />
      </label>
      <button type="submit" disabled={loading || !hasDaemonCredential}>Submit peer command</button>
      <p class="operation-status" data-phase={localControlOperationStates.peer.phase} aria-label="Peer operation status">{peerOperationStatusText}</p>
    </form>

    <form class="control-form" on:submit|preventDefault={submitFolderForm} aria-label="Daemon settings folder form">
      <h3>Folder form</h3>
      <label>
        Action
        <select bind:value={folderFormAction} name="folderFormAction">
          <option value="add">add</option>
          <option value="update">update</option>
          <option value="remove">remove</option>
        </select>
      </label>
      <label>
        Folder ID
        <input bind:value={folderFormID} name="folderFormID" data-testid="folder-id" autocomplete="off" />
      </label>
      <label>
        Folder path
        <input bind:value={folderFormPath} name="folderFormPath" data-testid="folder-path" autocomplete="off" />
      </label>
      <label>
        Folder mode
        <select bind:value={folderFormMode} name="folderFormMode">
          <option value="sendrecv">sendrecv</option>
          <option value="sendonly">sendonly</option>
          <option value="recvonly">recvonly</option>
        </select>
      </label>
      <label>
        Share with peer IDs
        <input bind:value={folderFormSharedWith} name="folderFormSharedWith" data-testid="folder-shared-with" autocomplete="off" placeholder="empty shares with every paired peer" />
      </label>
      <button type="button" data-testid="folder-submit" on:click={submitFolderForm} disabled={loading || !hasDaemonCredential}>Submit folder command</button>
      {#if folderRemoveConfirm}
        <div class="confirm-dialog" role="dialog" aria-labelledby="folder-remove-title" data-testid="destructive-confirm">
          <h3 id="folder-remove-title">Remove folder {folderFormID || 'without an id'}?</h3>
          <p>This asks the selected engine to stop sharing that folder. Files already on disk are left in place.</p>
          <div class="lifecycle-actions">
            <button type="button" data-testid="confirm-destructive" on:click={() => { folderRemoveArmed = true; submitFolderForm(); }}>Remove folder</button>
            <button type="button" data-testid="cancel-destructive" on:click={() => { folderRemoveConfirm = false; }}>Cancel</button>
          </div>
        </div>
      {/if}
      <p class="operation-status" data-phase={localControlOperationStates.folder.phase} aria-label="Folder operation status">{folderOperationStatusText}</p>
    </form>

    <form class="control-form" on:submit|preventDefault={submitDiscoveryForm} aria-label="Discovery form">
      <h3>Discovery form</h3>
      <label class="inline-choice">
        <input type="checkbox" bind:checked={discoveryFormDisabled} name="discoveryFormDisabled" />
        Disable all automatic discovery
      </label>
      <label class="inline-choice">
        <input type="checkbox" bind:checked={discoveryFormDHT} name="discoveryFormDHT" />
        Public DHT discovery enabled
      </label>
      <label class="inline-choice">
        <input type="checkbox" bind:checked={discoveryFormLocal} name="discoveryFormLocal" />
        Local/LAN discovery enabled
      </label>
      <button type="submit" disabled={loading || !hasDaemonCredential}>Submit discovery update</button>
      <p class="operation-status" data-phase={localControlOperationStates.discovery.phase} aria-label="Discovery operation status">{discoveryOperationStatusText}</p>
    </form>

    <form class="control-form" on:submit|preventDefault={submitTransferForm} aria-label="Transfer form">
      <h3>Transfer form</h3>
      <label>
        Action
        <select bind:value={transferFormAction} name="transferFormAction">
          <option value="pause">Pause sync</option>
          <option value="resume">Resume sync</option>
          <option value="cancel">cancel next matching transfer</option>
        </select>
      </label>
      <label>
        Folder ID scope
        <input bind:value={transferFormFolderID} name="transferFormFolderID" autocomplete="off" />
      </label>
      <label>
        Peer ID scope
        <input bind:value={transferFormPeerID} name="transferFormPeerID" autocomplete="off" />
      </label>
      <button type="submit" disabled={loading || !hasDaemonCredential}>Submit transfer command</button>
      <p class="operation-status" data-phase={localControlOperationStates.transfer.phase} aria-label="Transfer operation status">{transferOperationStatusText}</p>
    </form>

    <form class="control-form" on:submit|preventDefault={submitMaintenanceForm} aria-label="Maintenance form">
      <h3>Maintenance form</h3>
      <label>
        Optional folder ID
        <input bind:value={maintenanceFormFolderID} name="maintenanceFormFolderID" autocomplete="off" />
      </label>
      <button type="submit" disabled={loading || !hasDaemonCredential}>Run maintenance scrub</button>
      <p class="operation-status" data-phase={localControlOperationStates.maintenance.phase} aria-label="Maintenance operation status">{maintenanceOperationStatusText}</p>
    </form>

    <form class="control-form" on:submit|preventDefault={submitWebGUIForm} aria-label="Web GUI form">
      <h3>Web GUI form</h3>
      <p data-testid="web-gui-version">Web GUI version {status?.webGUI?.version || 'not installed'}</p>
      <p data-testid="web-gui-update-check">
        {#if status?.webGUI?.lastUpdateCheck}
          Last update check {status.webGUI.lastUpdateCheck}. {status.webGUI.lastUpdateMessage || status.webGUI.lastUpdateResult}
        {:else}
          No web GUI update check yet.
        {/if}
      </p>
      <label>
        Action
        <select bind:value={webGUIFormAction} name="webGUIFormAction">
          <option value="status">status</option>
          <option value="install">install</option>
          <option value="update">update</option>
          <option value="start">start</option>
          <option value="stop">stop</option>
        </select>
      </label>
      <button type="submit" disabled={loading || !hasDaemonCredential}>Submit web GUI command</button>
      <p class="operation-status" data-phase={localControlOperationStates.webGUI.phase} aria-label="Web GUI operation status">{webGUIOperationStatusText}</p>
    </form>

    <h2 class="section-label">Logging</h2>
    <form class="control-form" on:submit|preventDefault={submitConfigPatchForm} aria-label="Config patch form">
      <h3>Logging level</h3>
      <p data-testid="instance-owned-setting">
        {#if configInstances.find((instance) => instance.instanceId === selectedConfigInstanceId)?.local === false}
          Stored logging level for {configInstances.find((instance) => instance.instanceId === selectedConfigInstanceId)?.label}: {configInstances.find((instance) => instance.instanceId === selectedConfigInstanceId)?.settings?.['logging.level'] ?? 'not in the replica yet'}. Submitting queues the change for that engine only and does not change this engine's logging level.
        {:else}
          This form changes this engine's own logging level. Linked engines can view that record, and it does not change their logging level.
        {/if}
      </p>
      <label>
        Logging level
        <select bind:value={configPatchLoggingLevel} name="configPatchLoggingLevel">
          <option value="debug">debug</option>
          <option value="info">info</option>
          <option value="warn">warn</option>
          <option value="error">error</option>
          <option value="off">off</option>
        </select>
      </label>
      <label>
        Data encryption level
        <select bind:value={selectedEncryptionLevel} name="encryptionLevel" data-testid="encryption-level" on:change={() => { encryptionLevelDirty = true; }}>
          {#if encryptionProfiles.length === 0}
            {#each Array.from({ length: 11 }, (_, level) => level) as level}
              <option value={String(level)}>{level}</option>
            {/each}
          {:else}
            {#each encryptionProfiles as profile}
              <option value={String(profile.level)}>{profile.level} — {profile.name}</option>
            {/each}
          {/if}
        </select>
      </label>
      <p data-testid="encryption-level-explanation">
        {selectedEncryptionProfile()?.explanation ?? 'Encryption levels load from the local engine. Level 4 is the default for file data. Identity exchange, negotiation, and pairing stay at level ' + identityExchangeEncryptionLevel + '.'}
      </p>
      {#if selectedEncryptionProfile()?.keyAgreement}
        <p>
          {selectedEncryptionProfile()?.keyAgreement}; {selectedEncryptionProfile()?.aead}; {selectedEncryptionProfile()?.kdf}; MAC {selectedEncryptionProfile()?.mac}; signatures {selectedEncryptionProfile()?.signatures}; rekey every {selectedEncryptionProfile()?.rekeyEveryMiB} MiB.
        </p>
      {/if}
      {#if selectedEncryptionLevel === '0' || selectedEncryptionProfile()?.plaintext}
        <p class="plaintext-warning" role="alert" data-testid="encryption-plaintext-warning">
          Level 0 is plaintext. File contents and control messages are not encrypted. Anyone who can see the connection can read them. Do not use this level except for local debugging.
        </p>
      {/if}
      <button type="submit" disabled={loading || !hasDaemonCredential}>Submit non-secret config patch</button>
      <p class="operation-status" data-phase={localControlOperationStates.config.phase} aria-label="Config operation status">{configOperationStatusText}</p>
    </form>

    <section class="control-form" aria-label="API certificate trust">
      <h3>API certificate trust</h3>
      <p>
        Fetch the active certificate fingerprint for the selected host, then pin it only after the user verifies the TOFU pairing context. API keys and private key material are never displayed here.
      </p>
      <div class="lifecycle-actions">
        <button type="button" on:click={refreshAPITrustStatus} disabled={loading || !hasDaemonCredential}>Fetch API trust status</button>
        <button type="button" on:click={pinActiveAPICertificateForSelectedHost} disabled={loading || !hasDaemonCredential || !apiTrustStatus?.certificateSha256}>Pin active certificate</button>
      </div>
      <p class="operation-status" data-phase={localControlOperationStates.apiTrust.phase} aria-label="API trust operation status">{apiTrustOperationStatusText}</p>
      <p data-testid="api-trust-summary">
        {#if apiTrustStatus}
          Mode {apiTrustStatus.mode}. TLS required: {apiTrustStatus.tlsRequired ? 'yes' : 'no'}. Certificate fingerprint: {apiTrustStatus.certificateSha256 ?? 'not available'}. {apiTrustStatus.trustedCertificateConfigured ? 'A trusted fingerprint is configured.' : 'No trusted fingerprint is configured.'} {apiTrustStatus.trustedCertificateMatches ? 'The trusted certificate matches.' : 'The trusted certificate does not match.'}
        {:else}
          API certificate trust status has not been fetched for the selected host.
        {/if}
      </p>
      {#if apiTrustStatus}
        <dl class="host-status-grid" aria-label="API certificate trust details">
          <dt>Certificate fingerprint</dt>
          <dd>{apiTrustStatus.certificateSha256 ?? 'not available'}</dd>
          <dt>Trusted certificate matches</dt>
          <dd>{apiTrustStatus.trustedCertificateMatches ? 'yes' : 'no'}</dd>
          <dt>TLS required</dt>
          <dd>{apiTrustStatus.tlsRequired ? 'yes' : 'no'}</dd>
        </dl>
      {/if}
    </section>

    {#if controlMessage}
      <p>{controlMessage}</p>
    {/if}
    {#if daemonConfig}
      <pre>{JSON.stringify(daemonConfig, null, 2)}</pre>
    {/if}
  </section>
    {/if}

    {#if activeView === 'desktop-settings' && !dockerGUI}

  <section class="connection-card">
    <h2>Local engine lifecycle</h2>
    <p>
      If no system/user service is installed or reachable, the GUI can ask the native shell to launch the bundled daemon as a separate non-service process, then adopt it through the same authenticated encrypted API. persistent-user-daemon mode keeps sync independent after the GUI closes; temporary-session-only mode is explicit and user-selected.
    </p>
    <label class="inline-choice">
      <span>Non-service session mode</span>
      <select bind:value={guiOwnedNonServiceDaemonMode} name="guiOwnedNonServiceDaemonMode">
        <option value="persistent-user-daemon">persistent-user-daemon</option>
        <option value="temporary-session-only">temporary-session-only</option>
      </select>
    </label>
    <div class="lifecycle-actions">
      <button type="button" on:click={launchGUIOwnedNonServiceDaemon} disabled={lifecycleLoading}>
        Launch separate non-service daemon
      </button>
      <button type="button" on:click={adoptGUIOwnedNonServiceDaemonSession} disabled={lifecycleLoading}>
        Adopt recorded non-service daemon
      </button>
      <button type="button" on:click={stopGUIOwnedNonServiceDaemon} disabled={lifecycleLoading || !guiOwnedNonServiceDaemonSession}>
        Stop through daemon API
      </button>
    </div>
    <p>{guiOwnedNonServiceDaemonMessage}</p>
    {#if guiOwnedNonServiceDaemonSession}
      <dl class="host-status-grid" aria-label="GUI-owned non-service daemon session">
        <dt>PID</dt>
        <dd>{guiOwnedNonServiceDaemonSession.pid}</dd>
        <dt>API</dt>
        <dd>{guiOwnedNonServiceDaemonSession.encryptedApiBaseURL}</dd>
        <dt>credentialRef</dt>
        <dd>{guiOwnedNonServiceDaemonSession.credentialRef}</dd>
        <dt>mode</dt>
        <dd>{guiOwnedNonServiceDaemonSession.sessionMode}</dd>
      </dl>
    {/if}
  </section>
    {/if}

    {#if activeView === 'folders'}
      <section class="connection-card">
        <h2>Selected-host folders</h2>
        <p>Folder editing opens in the main view. This list is the same set shown beside Options.</p>
        <button type="button" on:click={refreshOperationalState} disabled={loading || !hasDaemonCredential}>Refresh folders</button>
        {#if daemonFolders.length === 0}
          <p>No folders are configured.</p>
        {:else}
          <ul aria-label="Configured folders">
            {#each daemonFolders as folder}
              <li data-testid="folder-row">
                <strong>{folder.id}</strong> · {folder.path ?? 'path unavailable'} · {folder.mode ?? 'mode unavailable'} · {folder.enabled === false ? 'paused' : (folder.status ?? 'configured')}
              </li>
            {/each}
          </ul>
        {/if}
      </section>
      <section class="connection-card remote-folder-browse">
        <h2>Remote folder browse</h2>
        <p>Browse directory roots through the selected remote host's authenticated API; manual path entry remains available when the selected host is offline.</p>
        <label>
          Manual folder path
          <input bind:value={folderFormPath} placeholder="/srv/share" autocomplete="off" />
        </label>
        <label>
          Remote browse path
          <input bind:value={remoteBrowsePath} placeholder={folderFormPath || '/'} autocomplete="off" />
        </label>
        <div class="lifecycle-actions">
          <button type="button" on:click={() => browseRemoteFolderTree()} disabled={remoteBrowseLoading || !canBrowseSelectedHostFilesystem || !hasDaemonCredential}>
            {remoteBrowseLoading ? 'Browsing…' : 'Browse selected host'}
          </button>
          <button type="button" on:click={() => browseRemoteFolderTree(folderFormPath)} disabled={remoteBrowseLoading || !canBrowseSelectedHostFilesystem || !hasDaemonCredential || folderFormPath.length === 0}>
            Browse manual path
          </button>
        </div>
        {#if selectedHostBrowseDisabledReason}
          <p class="operation-status" data-phase="failed">{selectedHostBrowseDisabledReason}</p>
        {/if}
        <p>{remoteBrowseMessage}</p>
        {#if remoteBrowseEntries.length > 0}
          <ul aria-label="Remote folder browse directory entries">
            {#each remoteBrowseEntries as entry}
              <li>
                <button type="button" on:click={() => chooseRemoteBrowseEntry(entry)} disabled={!entry.readable}>
                  {entry.name} · {entry.path}{entry.readable ? '' : ' (not readable)'}
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    {/if}

    {#if activeView === 'peers'}
      <section class="connection-card">
        <h2>Selected-host peers & identity</h2>
        <p>Peer lists, discovery, identity pairing, and remote-instance onboarding for {selectedManagedInstance.label}. Add, update, and remove operations are available under Daemon settings.</p>
        <button type="button" on:click={refreshOperationalState} disabled={loading || !hasDaemonCredential}>Refresh peers</button>
        {#if daemonPeers.length === 0}
          <p class="empty-state">No peers are linked yet. Pair from this page with a code or an identity package.</p>
        {:else}
          <ul aria-label="Configured peers">
            {#each daemonPeers as peer}
              <li><strong>{peer.id}</strong> · {(peer.endpoints && peer.endpoints.length > 0) ? peer.endpoints.join(' · ') : (peer.endpoint ?? 'endpoint unavailable')} · {peer.status ?? 'configured'} · <span data-testid="peer-connection-path">{peer.connectionPath || (peer.status === 'discovered' || (peer.endpoint ?? '').startsWith('discovered') ? 'discovered' : 'unreachable')}</span> · receive {peer.receiveRemainingBytes ?? 0} B left at {peer.averageReceiveBytesPerSecond ?? 0} B/s</li>
            {/each}
          </ul>
        {/if}
        <p>Configured and discovered peers above are live daemon state. They are not promoted into remote API hosts because the daemon does not yet publish authenticated remote API endpoints or a progressive remote-instance pairing read model.</p>
      </section>
      {#if !dockerGUI}
      <section class="connection-card">
        <h2>Remote instance onboarding form</h2>
        <p>Add a remote instance only when you have its HTTPS API endpoint and key. On Linux, raw API keys are stored through Freedesktop Secret Service and resolved only inside the native API proxy. Pairing import remains daemon-owned and does not fabricate a remote host entry.</p>
        <p>{remoteRegistryMessage}</p>
        <form class="control-form" on:submit|preventDefault={submitRemoteInstanceOnboarding} aria-label="Remote instance onboarding form">
          <label>
            Onboarding source
            <select bind:value={remoteOnboardingSource} name="remoteOnboardingSource">
              <option value="api-endpoint-key">Direct API endpoint/key</option>
            </select>
          </label>
          <label>
            Remote label
            <input bind:value={remoteOnboardingLabel} name="remoteOnboardingLabel" autocomplete="off" />
          </label>
          <label>
            Remote API endpoint
            <input bind:value={remoteOnboardingEndpoint} name="remoteOnboardingEndpoint" autocomplete="off" placeholder="https://host:22420" />
          </label>
          <label>
            Remote API key
            <input bind:value={remoteOnboardingAPIKey} name="remoteOnboardingAPIKey" type="password" autocomplete="off" />
          </label>
          <p>Pairing-code, identity-file, animated-code, and shared-identity inputs are not accepted here because the daemon does not yet publish the authenticated endpoint needed to create a manageable remote host.</p>
          <button type="submit" disabled={remoteLifecycleLoading}>{remoteLifecycleLoading ? 'Adding remote instance…' : 'Add remote instance'}</button>
        </form>
        {#if remoteOnboardingMessage}
          <p>{remoteOnboardingMessage}</p>
        {/if}
      </section>
      {/if}
      <section class="connection-card remote-mesh-settings-plan">
        <h2>Trusted eventually-consistent mesh management</h2>
        <p>Each identity-linked daemon will publish a per-node settings document so offline instances can be inspected or queued for later adoption through trusted relays instead of requiring direct reachability.</p>
        <p>Design boundary: the config file remains local import/export; remote edits become durable pending changes against the selected node's canonical document and must be authenticated, validated, applied, and acknowledged by that owner node.</p>
        <div class="lifecycle-actions">
          <button type="button" on:click={refreshRemoteMeshStatus} disabled={loading || !hasDaemonCredential}>Refresh remote mesh status</button>
          <button type="button" on:click={queueSelectedRemoteMeshSettingsChange} disabled={loading || !hasDaemonCredential}>Queue selected-host settings change</button>
        </div>
        <dl>
          <dt>per-node settings document</dt>
          <dd>{remoteMeshSettingsDocument.nodeID} · revision {remoteMeshSettingsDocument.revision} · {remoteMeshSettingsDocument.redactedSettingsSummary}</dd>
          <dt>durable pending changes</dt>
          <dd>{remoteMeshPendingSettingsChange.status} · {remoteMeshPendingSettingsChange.idempotencyKey}</dd>
          <dt>offline edit queue</dt>
          <dd>{offlineRemoteSettingsEdit.selectedHostLabel} · {offlineRemoteSettingsEdit.pendingChange.status} · {offlineRemoteSettingsEdit.offlineDeliveryNote}</dd>
          <dt>Remote mesh document status</dt>
          <dd>{remoteMeshStatusMessage}</dd>
          <dt>Pending remote settings changes</dt>
          <dd>{remoteMeshPendingChanges.length} queued/applied/failed/acknowledged change record(s) visible for the selected host.</dd>
        </dl>
        {#if remoteMeshDocuments.length > 0}
          <ul aria-label="Remote mesh document status">
            {#each remoteMeshDocuments as document}
              <li>{document.nodeId ?? document.nodeID ?? 'unknown-node'} · revision {document.revision ?? 0} · updated {document.updatedAt ?? 'unknown'}</li>
            {/each}
          </ul>
        {/if}
        {#if remoteMeshPendingChanges.length > 0}
          <ul aria-label="Pending remote settings changes">
            {#each remoteMeshPendingChanges as change}
              <li>{change.targetNodeId ?? selectedManagedInstance.id} · {change.status} · {change.idempotencyKey ?? 'missing idempotency key'}</li>
            {/each}
          </ul>
        {/if}
      </section>
      <section class="connection-card">
        <h2>Identity pairing export/import</h2>
        <p>
          This GUI is connected to the local engine automatically. The instance selected in the list issues and accepts the one-time pairing code from the main panel. A remote or offline selection is relayed over the mesh and runs when that instance is online. Identity packages below are imported and exported by this engine. After either method, the engines store every reachable address, DHT identity, relay, and certificate pin, and fail over if the first address dies.
        </p>
        <p data-testid="pairing-advertise-url">
          This local engine advertises {status?.advertiseURL || 'its reachable API URL'}. Other engines use that address from the pairing package. You do not enter this engine's URL.
        </p>
        <details data-testid="pairing-advertise-advanced">
          <summary>Advanced: override advertised API URL</summary>
          <p>The field starts with the address this engine will advertise. Docker bridge addresses are never used. FSE_ADVERTISE_URL or FSE_ADVERTISE_URLS at install does the same thing. Pairing does not ask for the local URL.</p>
          <label>
            Advertised API URL
            <input data-testid="pairing-advertise-override" bind:value={advertiseOverride} on:input={() => { advertiseFieldDirty = true; }} autocomplete="off" placeholder="https://lan-host:22420" />
          </label>
          <button type="button" on:click={saveAdvertiseOverride} disabled={loading || !hasDaemonCredential}>Save advertised URL</button>
        </details>
        <button type="button" data-testid="generate-pairing" on:click={generatePairingPackage} disabled={pairingLoading}>
          Generate pairing package
        </button>
        {#if pairingExportText}
          <label>
            copyable pairing text
            <textarea data-testid="pairing-export-text" readonly rows="8" value={pairingExportText}></textarea>
          </label>
          <a
            class="download-link"
            download={pairingDownloadFilename}
            href={`data:application/json;charset=utf-8,${encodeURIComponent(pairingExportText)}`}
          >
            Download identity file
          </a>
          <p>QR fallback image is ready for a native/browser QR renderer and animated visual code frame list is generated below; animated scanner state will continue collecting missing/new frames across animation loops instead of failing after the first incomplete pass.</p>
          <label>
            QR fallback payload
            <textarea readonly rows="3" value={pairingQRFallbackPayload}></textarea>
          </label>
          {#if qrFallbackImageModel}
            <div class="qr-fallback-image" aria-label={qrFallbackImageModel.altText}>
              <strong>QR fallback image</strong>
              <p>{qrFallbackImageModel.guidance}</p>
              <p>Show only in the pairing view; renderer source {qrFallbackImageModel.sourceURI}, module size {qrFallbackImageModel.moduleSizePixels}px, quiet zone {qrFallbackImageModel.quietZoneModules} modules.</p>
            </div>
          {/if}
          <label>
            animated visual code frame list
            <textarea readonly rows="6" value={pairingAnimatedFrameList}></textarea>
          </label>
          <p>Using conservative animated pairing density for weak cameras and shaky hands: 64 bytes per frame ({animatedPairingConservativeDensityProfile.maxPayloadBytesPerFrame} configured maximum), large visual modules, and slow frame timing before denser camera-tested profiles exist.</p>
          <p>Using privacy-preserving animated pairing fragments: a single captured frame cannot reveal much of the identity code or a contiguous payload chunk; enough frames must be collected and checksum-verified before daemon-owned import.</p>
          <p class="scan-progress">
            {animatedScanProgress.message || 'Keep phone pointed at screen until pairing is complete.'}
            Collected {animatedScanProgress.collectedFrameCount}/{animatedScanProgress.requiredFrameCount} required unique frames; duplicates ignored: {animatedScanProgress.duplicateFrameCount}.
          </p>
          <div class="scan-progress" aria-label="Desktop animated pairing camera scanner">
            <strong>animated camera scanner</strong>
            <p>{desktopAnimatedPairingScannerScreen.cameraPermissionMessage}</p>
            <p>{desktopAnimatedPairingScannerScreen.guidance || 'Keep camera pointed at the animated code until pairing is complete.'}</p>
            <p>Camera scan progress: {desktopAnimatedPairingScannerScreen.progressPercent}% ({desktopAnimatedPairingScannerScreen.collectedFrameCount}/{desktopAnimatedPairingScannerScreen.requiredFrameCount} required frames).</p>
          </div>
        {/if}
        <label>
          Paste or upload identity file
          <textarea data-testid="pairing-import-text" bind:value={pairingImportText} rows="8" placeholder="Paste identity package JSON here"></textarea>
        </label>
        <input type="file" accept="application/json,.json" on:change={handlePairingFileUpload} aria-label="Upload identity file" />
        <button type="button" data-testid="parse-pairing" on:click={parsePairingImportText}>Parse pasted identity file</button>
        <button type="button" data-testid="import-pairing" on:click={importParsedPairingPackage} disabled={pairingLoading}>Import identity package</button>
        <p>Import uses daemon-owned import execution over the authenticated encrypted API and returns only redacted peer-pair identifiers.</p>
        {#if pairingImportSummary}
          <p class="success">{pairingImportSummary}</p>
        {/if}
        {#if pairingMessage && nodeOptionsOpen}
          <p data-testid="pairing-message" class:pairing-error={pairingLooksFailed(pairingMessage)} role={pairingLooksFailed(pairingMessage) ? 'alert' : 'status'}>{pairingMessage}</p>
        {/if}
      </section>
    {/if}

    {#if activeView === 'transfers'}
      <section class="connection-card">
        <h2>Selected-host transfers</h2>
        <p>The daemon exposes active transfer-pass lifecycle state and bounded recent outcomes. Pause, resume, and cancel controls remain available under Daemon settings.</p>
        <button type="button" on:click={refreshTransfers} disabled={loading || !hasDaemonCredential}>Refresh transfer activity</button>
        <p>{transfersMessage}</p>
        <h3>Active transfer passes</h3>
        {#if daemonTransfers.active.length === 0}
          <p>No transfer pass is currently active.</p>
        {:else}
          <ul class="event-list" aria-label="Active transfer passes">
            {#each daemonTransfers.active as transfer}
              <li><strong>{transfer.folderId}</strong>{transfer.peerId ? ` · peer ${transfer.peerId}` : ' · local folder pass'} · {transfer.status}<br />{transfer.message ?? transfer.eventType}</li>
            {/each}
          </ul>
        {/if}
        <h3>Recent transfer history</h3>
        {#if daemonTransfers.history.length === 0}
          <p>No transfer outcomes are available since this daemon started.</p>
        {:else}
          <ul class="event-list" aria-label="Recent transfer history">
            {#each daemonTransfers.history as transfer}
              <li><strong>{transfer.folderId}</strong>{transfer.peerId ? ` · peer ${transfer.peerId}` : ' · local folder pass'} · {transfer.status} · {transfer.finishedAt ?? 'time unavailable'}<br />{transfer.message ?? transfer.eventType}</li>
            {/each}
          </ul>
        {/if}
        <p>Live byte progress and rate telemetry are not available from the daemon runtime, so this view does not invent percentages, bytes remaining, or throughput charts.</p>
      </section>
    {/if}

    {#if activeView === 'warnings'}
      <section class="connection-card">
        <h2>Selected-host warnings & logs</h2>
        <p>Recent structured daemon events are loaded through the authenticated native API bridge. Secrets and raw credential material are not shown.</p>
        <button type="button" on:click={refreshWarningsAndLogs} disabled={loading || !hasDaemonCredential}>Refresh warnings & logs</button>
        <p>{warningsMessage}</p>
        {#if daemonEvents.length === 0}
          <p>No recent events are available.</p>
        {:else}
          <ul class="event-list" aria-label="Recent daemon events">
            {#each daemonEvents as event}
              <li><strong>{event.type}</strong> · {event.time}{event.folderID ? ` · folder ${event.folderID}` : ''}{event.peerID ? ` · peer ${event.peerID}` : ''}<br />{event.message ?? event.path ?? 'No additional detail'}</li>
            {/each}
          </ul>
        {/if}
      </section>
    {/if}

    {#if activeView === 'maintenance'}
      <section class="connection-card">
        <h2>Selected-host maintenance & backups</h2>
        <p>Create and inspect snapshots, preview restores before writing, run an explicit restore, apply deprecate-first retention, and scrub backup protection through the authenticated daemon API.</p>
        <div class="lifecycle-actions">
          <button type="button" on:click={refreshSnapshotsAndJobs} disabled={loading || !hasDaemonCredential}>Refresh snapshots & jobs</button>
          <button type="button" on:click={executeBackupScrub} disabled={loading || !hasDaemonCredential}>Run backup scrub</button>
        </div>
        <p>{maintenanceBackupMessage}</p>
        {#if backupJobsSummary}<p>Jobs: {backupJobsSummary}</p>{/if}
        <form class="control-form" on:submit|preventDefault={createSnapshot}>
          <h3>Create snapshot</h3>
          <label>Folder ID<input bind:value={snapshotFolderID} autocomplete="off" /></label>
          <label>Description<input bind:value={snapshotDescription} autocomplete="off" /></label>
          <button type="submit" disabled={loading || !hasDaemonCredential}>Create snapshot</button>
        </form>
        {#if snapshotMarkers.length > 0}
          <label>Snapshot
            <select bind:value={selectedSnapshotID}>
              <option value="">Select a snapshot</option>
              {#each snapshotMarkers as marker}<option value={marker.id}>{marker.id} · {marker.folderId}{marker.deprecated ? ' · deprecated' : ''}</option>{/each}
            </select>
          </label>
        {:else}
          <label>Snapshot ID<input bind:value={selectedSnapshotID} autocomplete="off" /></label>
        {/if}
        <label>Restore paths (optional, comma or newline separated)<textarea bind:value={restorePaths} rows="3"></textarea></label>
        <label>Alternate destination (optional)<input bind:value={restoreDestination} autocomplete="off" /></label>
        <div class="lifecycle-actions">
          <button type="button" on:click={previewRestore} disabled={loading || !hasDaemonCredential || !selectedSnapshotID}>Preview restore</button>
          <button type="button" class="danger" on:click={executeRestore} disabled={loading || !hasDaemonCredential || !restorePlan || reviewedRestoreRequestKey !== currentRestoreRequestKey}>Run reviewed restore</button>
        </div>
        {#if restorePlan}<p>Reviewed plan: {restorePlan.files?.length ?? 0} file(s) to {restorePlan.destination}. The restore button applies this selected snapshot/path request without database reversion.</p>{/if}
        <form class="control-form" on:submit|preventDefault={executeRetention}>
          <h3>Snapshot retention</h3>
          <label>Keep newest snapshots<input type="number" min="1" step="1" bind:value={retentionKeepLast} /></label>
          <button type="submit" disabled={loading || !hasDaemonCredential}>Run deprecate-first retention</button>
        </form>
      </section>
    {/if}

    {#if activeView === 'desktop-settings' && !dockerGUI}
      <section class="connection-card">
        <h2>Desktop GUI settings menu</h2>
        <p>These preferences are persisted in native per-user storage and are never written into the selected host's daemon config.</p>
        {#if guiPlatform === 'macos'}<p data-platform="macos">macOS settings stay on this platform.</p>{/if}
        {#if guiPlatform === 'windows'}<p data-platform="windows">Windows settings stay on this platform.</p>{/if}
        {#if guiPlatform === 'linux'}<p data-platform="linux">Linux settings stay on this platform.</p>{/if}
        <form class="control-form" on:submit|preventDefault={saveDesktopPreferences} aria-label="Desktop preferences">
          <label>Theme<select bind:value={desktopPreferences.theme}><option value="system">Follow system</option><option value="light">Light</option><option value="dark">Dark</option></select></label>
          <label>Density<select bind:value={desktopPreferences.density}><option value="comfortable">Comfortable</option><option value="compact">Compact</option></select></label>
          <label class="inline-choice"><input type="checkbox" bind:checked={desktopPreferences.minimizeToTray} /> Minimize to tray when native tray support is available</label>
          <label class="inline-choice"><input type="checkbox" bind:checked={desktopPreferences.notificationsEnabled} /> Enable desktop notifications when native notification support is available</label>
          <button type="submit">Save desktop preferences</button>
          <p>{desktopPreferencesMessage}</p>
        </form>
        <h3>Bundled engine integrity</h3>
        <button type="button" on:click={inspectBundledEngineResources}>Inspect and verify packaged engine files</button>
        <p>{bundledEngineInspectionMessage}</p>
        {#if bundledEngineInspection}
          <p>Manifest version: {bundledEngineInspection.version} · overall {bundledEngineInspection.verified ? 'verified' : 'not verified'}</p>
          <ul class="event-list" aria-label="Bundled engine manifest inspection">
            {#each bundledEngineInspection.entries as entry}
              <li><strong>{entry.target}</strong> · {entry.exists ? entry.verified ? 'verified' : 'mismatch' : 'not included in this target-specific bundle'}<br />{entry.relativePath} · {entry.message}</li>
            {/each}
          </ul>
        {/if}
        <p>Tray minimization and desktop notifications remain explicitly conditional: saving these preferences does not claim that a platform tray or notification backend exists.</p>
      </section>
    {/if}

    {#if activeView === 'security'}
      <section class="connection-card">
        <h2>Security</h2>
        <p>Session encryption follows the paired identity. Certificate trust for this engine is checked in Settings, and the labels there say what they mean instead of showing raw field names.</p>
        <p>API keys are not shown in this browser. The Docker GUI uses the daemon's own credential, and desktop remote engines keep keys in the operating system vault. Startup, tray, and installer choices are operating-system specific and appear only in Desktop app settings.</p>
        <dl class="host-status-grid" aria-label="Security summary">
          <dt>Selected engine</dt><dd>{selectedConfigInstanceId || 'local'}</dd>
          <dt>Pending changes</dt><dd>{configInstances.find((instance) => instance.instanceId === selectedConfigInstanceId)?.pendingChanges ?? 0}</dd>
        </dl>
      </section>
    {/if}

    {#if activeView === 'help'}
      <section class="connection-card">
        <h2>Dedicated help/details pages</h2>
        <p>Advanced explanations live here so daily status, folders, transfers, and settings pages stay readable.</p>
        <div class="settings-grid" aria-label="Help and details sections">
          {#each helpDetailsSections as section}
            <article class="settings-card">
              <h3>{section.title}</h3>
              <p>{section.summary}</p>
            </article>
          {/each}
        </div>
      </section>
    {/if}
    </div>
  {/if}
</main>

<style>
  main {
    --bg: #101510;
    --surface: #1b2420;
    --surface-2: #121815;
    --line: #314038;
    --text: #f4f7f2;
    --muted: #b7c4b8;
    --accent: #8fd0a8;
    --danger: #ffb4b4;
    --ok: #b8f7c0;
    --focus: #e7ffe8;
    min-height: 100vh;
    font-family: "Avenir Next", "Segoe UI", sans-serif;
    background: var(--bg);
    color: var(--text);
    line-height: 1.45;
  }
  main[data-theme='light'] {
    --bg: #f6f8f4;
    --surface: #ffffff;
    --surface-2: #eef3ec;
    --line: #c9d4c8;
    --text: #172117;
    --muted: #3f5346;
    --accent: #1d6b42;
    --danger: #8d2b2b;
    --ok: #1d6b42;
    --focus: #1d6b42;
  }
  .host-scoped-shell {
    display: grid;
    grid-template-columns: minmax(11rem, 14rem) minmax(0, 1fr);
    align-items: start;
    gap: 1rem;
    padding: 1rem;
  }
  @media (max-width: 800px) {
    .host-scoped-shell {
      grid-template-columns: 1fr;
    }
    .host-sidebar {
      position: static;
    }
    .connection-card {
      width: auto;
      padding: 1rem;
    }
    .node-options-dialog {
      left: 0;
    }
  }
  .host-sidebar {
    position: sticky;
    top: 1rem;
    display: grid;
    gap: 1rem;
    min-width: 0;
    max-height: calc(100vh - 2rem);
    overflow: auto;
    overflow-wrap: anywhere;
  }
  .eyebrow {
    color: var(--muted);
    font-size: 0.8rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }
  .connection-state {
    justify-self: start;
    padding: 0.15rem 0.45rem;
    border-radius: 999px;
    background: #263148;
    color: #dbe7ff;
    font-size: 0.8rem;
  }
  .connection-state[data-state='online'] {
    background: #1d5033;
    color: #c9ffd8;
  }
  .host-content {
    display: grid;
    justify-items: center;
    gap: 1rem;
    min-width: 0;
  }
  .host-scope-banner {
    width: min(44rem, 100%);
    display: grid;
    gap: 0.75rem;
    padding: 1.25rem;
    border: 1px solid #405177;
    border-radius: 1rem;
    background: #172033;
  }
  .update-banner {
    width: min(44rem, 100%);
    display: grid;
    gap: 0.75rem;
    padding: 1.25rem;
    border: 1px solid #6de08c;
    border-radius: 1rem;
    background: #13251c;
  }
  .update-banner[data-phase='error'] {
    border-color: #ff8a8a;
    background: #2a1518;
  }
  .update-banner h2,
  .update-banner p {
    margin: 0;
  }
  .host-scope-banner h2,
  .host-scope-banner p,
  .host-scope-details {
    margin: 0;
  }
  .host-scope-details {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 0.5rem;
  }
  .host-scope-details div {
    display: grid;
    gap: 0.15rem;
    padding: 0.5rem;
    border-radius: 0.5rem;
    background: var(--surface-2);
  }
  .host-scope-details dt {
    color: var(--muted);
    font-size: 0.72rem;
    text-transform: uppercase;
  }
  .host-scope-details dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
  .connection-card {
    width: min(44rem, 100%);
    display: grid;
    gap: 1rem;
    padding: 2rem;
    border: 1px solid var(--line);
    border-radius: 1rem;
    background: var(--surface);
  }
  label {
    display: grid;
    gap: 0.35rem;
  }
  input, button, select, textarea {
    font: inherit;
    padding: 0.65rem 0.75rem;
    border-radius: 0.5rem;
    border: 1px solid var(--line);
  }
  textarea {
    color: var(--text);
    background: var(--surface-2);
  }
  button {
    cursor: pointer;
  }
  .error,
  .pairing-error {
    color: var(--danger);
    font-weight: 600;
  }
  .success {
    color: var(--ok);
  }
  .control-form {
    display: grid;
    gap: 0.75rem;
    padding: 1rem;
    border: 1px solid var(--line);
    border-radius: 0.75rem;
    background: var(--surface-2);
  }
  .control-form h3 {
    margin: 0;
  }
  .lifecycle-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
  }
  .inline-choice {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .operation-status {
    display: grid;
    gap: 0.25rem;
    margin: 0;
    padding: 0.75rem;
    border: 1px solid #33405a;
    border-radius: 0.5rem;
    background: var(--surface-2);
  }
  .operation-status[data-phase='pending'], .operation-status[data-phase='accepted'] {
    border-color: #e8c86a;
  }
  .operation-status[data-phase='completed'] {
    border-color: #6de08c;
  }
  .operation-status[data-phase='failed'] {
    border-color: #ff8a8a;
  }
  .plaintext-warning {
    margin: 0;
    padding: 0.75rem;
    border: 2px solid #ff8a8a;
    border-radius: 0.5rem;
    background: #3a1212;
    color: #ffd0d0;
    font-weight: 600;
  }
  .settings-grid {
    display: grid;
    gap: 0.75rem;
  }
  .settings-card {
    padding: 1rem;
    border: 1px solid var(--line);
    border-radius: 0.75rem;
    background: var(--surface-2);
  }
  .settings-card h3,
  .settings-card p {
    margin: 0;
  }
  .settings-card p {
    margin-top: 0.35rem;
    color: var(--muted);
  }
  .download-link {
    color: var(--accent);
  }
  .section-label {
    width: min(52rem, 100%);
    margin: 0.5rem 0 0;
    font-size: 0.8rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--muted);
  }
  .empty-state {
    margin: 0;
    padding: 0.85rem 1rem;
    border: 1px dashed var(--line);
    border-radius: 0.75rem;
    color: var(--muted);
  }
  .confirm-dialog {
    display: grid;
    gap: 0.75rem;
    padding: 1rem;
    border: 1px solid var(--danger);
    border-radius: 0.75rem;
    background: var(--surface-2);
  }
  .confirm-dialog h3,
  .confirm-dialog p {
    margin: 0;
  }
  .theme-toggle {
    background: transparent;
    color: var(--text);
  }
  button, input, select, textarea {
    background: var(--surface-2);
    color: var(--text);
  }
  button:focus-visible, input:focus-visible, select:focus-visible, textarea:focus-visible, a:focus-visible {
    outline: 2px solid var(--focus);
    outline-offset: 2px;
  }
  .host-content {
    justify-items: stretch;
  }
  .connection-card, .host-scope-banner, .update-banner {
    width: min(52rem, 100%);
  }
  .onboarding-compact {
    display: grid;
    gap: 0.35rem;
    width: min(52rem, 100%);
  }
  .onboarding-compact p {
    margin: 0;
    color: var(--muted);
    font-size: 0.85rem;
  }
  .host-status-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(9rem, 1fr));
    gap: 0.5rem;
    margin: 0;
  }
  .host-status-grid dt {
    color: var(--muted);
    font-size: 0.72rem;
    text-transform: uppercase;
  }
  .host-status-grid dd {
    margin: 0 0 0.5rem;
    overflow-wrap: anywhere;
  }
  .host-sidebar {
    gap: 0.35rem;
  }
  .peer-list {
    display: grid;
    gap: 0.25rem;
  }
  .peer-row {
    display: grid;
    gap: 0.1rem;
    width: 100%;
    padding: 0.35rem 0.45rem;
    text-align: left;
    background: transparent;
    color: inherit;
    border-radius: 0.4rem;
  }
  .peer-row.active,
  .peer-row[aria-current='true'] {
    border-color: var(--accent);
    background: #243028;
  }
  main[data-theme='light'] .peer-row.active,
  main[data-theme='light'] .peer-row[aria-current='true'] {
    background: #e5f3ea;
  }
  .peer-line {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: 0.35rem;
    min-width: 0;
  }
  .peer-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .peer-stats {
    color: var(--muted);
    font-size: 0.75rem;
    line-height: 1.2;
  }
  .main-toolbar {
    position: relative;
    z-index: 22;
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
    width: min(52rem, 100%);
  }
  .actions-menu {
    position: relative;
  }
  .menu-backdrop {
    position: fixed;
    inset: 0;
    z-index: 20;
    background: transparent;
    border: 0;
    padding: 0;
  }
  .actions-menu-panel {
    position: absolute;
    z-index: 21;
    top: calc(100% + 0.25rem);
    right: 0;
    display: grid;
    gap: 0.4rem;
    min-width: 16rem;
    padding: 0.55rem;
    background: var(--surface);
    color: var(--text);
    border: 1px solid var(--line);
    border-radius: 0.45rem;
  }
  .actions-menu-panel label {
    display: grid;
    gap: 0.25rem;
    color: var(--muted);
    font-size: 0.8rem;
  }
  .folder-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 0.25rem;
  }
  .folder-row {
    border: 1px solid var(--line);
    border-radius: 0.4rem;
    background: var(--surface);
  }
  .folder-summary {
    display: grid;
    grid-template-columns: 1rem minmax(0, 1.4fr) minmax(4.5rem, 0.7fr) minmax(5.5rem, 0.7fr) minmax(4rem, 0.6fr);
    gap: 0.5rem;
    align-items: baseline;
    width: 100%;
    padding: 0.35rem 0.55rem;
    text-align: left;
    background: transparent;
    color: var(--text);
    border: 0;
  }
  .folder-chevron,
  .folder-meta {
    color: var(--muted);
    font-size: 0.8rem;
  }
  .folder-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 600;
  }
  .folder-meta {
    white-space: nowrap;
  }
  .folder-details {
    display: grid;
    gap: 0.4rem;
    padding: 0.35rem 0.55rem 0.55rem;
    border-top: 1px solid var(--line);
  }
  .folder-details p {
    margin: 0;
    color: var(--muted);
    font-size: 0.85rem;
  }
  .folder-edit-view {
    width: min(52rem, 100%);
  }
  .node-options-dialog {
    position: fixed;
    z-index: 40;
    top: 0;
    right: 0;
    bottom: 0;
    left: 16rem;
    overflow: auto;
    padding: 1rem 1.25rem 2rem;
    background: var(--bg);
    border-left: 1px solid var(--line);
    display: grid;
    align-content: start;
    gap: 1rem;
  }
  .node-options-bar {
    position: sticky;
    top: 0;
    z-index: 1;
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 1rem;
    padding: 0.25rem 0 0.75rem;
    background: var(--bg);
  }
  .node-options-bar h2 {
    margin: 0;
  }
  .node-options-nav {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(11rem, 1fr));
    gap: 0.5rem;
  }
  .node-options-nav button {
    display: grid;
    gap: 0.25rem;
    text-align: left;
    background: var(--surface);
    color: inherit;
  }
  .node-options-nav button.active,
  .node-options-nav button[aria-current='page'] {
    border-color: var(--accent);
  }
  .node-options-nav small {
    color: var(--muted);
    line-height: 1.35;
  }
</style>
