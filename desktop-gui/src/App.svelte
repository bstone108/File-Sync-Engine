<script lang="ts">
  import { onMount } from 'svelte';
  import {
    fetchDaemonStatus,
    fetchAPITrustStatus,
    browseFilesystemDirectories,
    fetchRemoteMeshSettings,
    generateIdentityPairingPackage,
    importIdentityPairingPackage,
    patchDaemonConfig,
    pinActiveAPICertificate,
    queueRemoteMeshSettingsCommand,
    readDaemonConfig,
    runMaintenanceScrub,
    sendDiscoveryCommand,
    sendFolderCommand,
    sendPeerCommand,
    sendTransferCommand,
    sendWebGUICommand,
    type CommandResponse,
    type APITrustStatus,
    type DaemonConfig,
    type DaemonStatus,
    type IdentityPairingPackage,
    type FilesystemBrowseEntry,
    type MeshSettingsCommandResponse,
    type MeshSettingsDocument
  } from './lib/daemonApi';
  import {
    adoptGUIOwnedNonServiceDaemon,
    getDaemonStartupIntegrationStatus,
    getDaemonTrayStatus,
    getFirstLaunchDaemonRegistrationStatus,
    getGUIOwnedNonServiceDaemonSession,
    getGUIOwnedNonServiceDaemonState,
    installBundledDaemonForCurrentOS,
    loadBundledDaemonGate,
    openGuiFromDaemonTray,
    promptForStartupAtLogin,
    requestGUIOwnedNonServiceDaemonLaunch,
    runBundledDaemonLifecycle,
    showMainWindowFromDaemonTray,
    stopGUIOwnedNonServiceDaemonThroughAPI,
    storeRemoteInstanceCredential,
    type DaemonStartupIntegrationStatus,
    type DaemonTrayStatus
  } from './lib/nativeShell';
  import { type BundledDaemonLifecycleAction, type BundledEngineRuntimeGate } from './lib/bundledEngine';
  import { type FirstLaunchDaemonRegistrationStatus, type GUIManagedNonServiceDaemonSession } from './lib/firstLaunch';
  import { handleDaemonTrayOpenRequest, isDaemonTrayOpenRequest } from './lib/trayOpen';
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
    buildDiscoveredIdentityPeersFromLoadedSharedIdentity,
    buildOfflineRemoteSettingsEdit,
    buildRemoteInstanceOnboardingCandidate,
    buildRemoteMeshSettingsDocument,
    queueRemoteMeshSettingsChange,
    defaultExpandedInstanceGroups,
    defaultLocalDaemonInstance,
    ensureLocalInstanceFirst,
    formatConnectionStateLabel,
    groupDaemonInstances,
    addRemoteDaemonInstance,
    upsertDiscoveredIdentityPeerInstances,
    type DiscoveredIdentityPeer,
    type LoadedSharedIdentityDiscoverySnapshot,
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
  let apiKey = '';
  let status: DaemonStatus | null = null;
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
  let discoveryFormDisabled = false;
  let discoveryFormDHT = true;
  let discoveryFormLocal = true;
  let transferFormAction: 'pause' | 'resume' | 'cancel' = 'pause';
  let transferFormFolderID = '';
  let transferFormPeerID = '';
  let maintenanceFormFolderID = '';
  let webGUIFormAction: 'status' | 'install' | 'update' | 'start' | 'stop' = 'status';
  let configPatchLoggingLevel = 'info';
  let pairingGroupID = '';
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
  let animatedPairingScanState = createAnimatedPairingScanState();
  let animatedScanProgress: AnimatedPairingScanProgress = animatedPairingScanState.progress;
  let desktopAnimatedPairingScannerScreen: DesktopAnimatedPairingScannerScreenModel = buildDesktopAnimatedPairingScannerScreen(
    buildDesktopAnimatedPairingCameraCaptureState({ cameraPermission: 'unknown', scannerState: animatedPairingScanState })
  );
  let pairingLoading = false;
  let errorMessage = '';
  let loading = false;
  let bundleGate: BundledEngineRuntimeGate | null = null;
  let bundleMessage = '';
  let lifecycleLoading = false;
  let firstLaunchStatus: FirstLaunchDaemonRegistrationStatus | null = null;
  let firstLaunchMessage = '';
  let configureStartup = true;
  let guiOwnedNonServiceDaemonSession: GUIManagedNonServiceDaemonSession | null = null;
  let guiOwnedNonServiceDaemonMessage = 'No GUI-owned non-service daemon session has been launched or adopted yet.';
  let guiOwnedNonServiceDaemonMode: GUIManagedNonServiceDaemonSession['sessionMode'] = 'persistent-user-daemon';
  let trayStatus: DaemonTrayStatus | null = null;
  let startupIntegrationStatus: DaemonStartupIntegrationStatus | null = null;
  let trayMessage = '';
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
  type DesktopUIView =
    | 'overview'
    | 'folders'
    | 'peers'
    | 'transfers'
    | 'warnings'
    | 'maintenance'
    | 'daemon-settings'
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

  type DesktopSettingsSection = {
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

  const lifecycleActions: BundledDaemonLifecycleAction[] = ['status', 'start', 'stop', 'restart'];
  let activeView: DesktopUIView = 'overview';
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
  let remoteMeshDocuments: MeshSettingsDocument[] = [];
  let remoteMeshPendingChanges: MeshSettingsCommandResponse[] = [];
  let remoteMeshStatusMessage = 'Remote mesh status has not been refreshed yet; online, relay-reachable, and offline instances will show cached document and pending-change state here.';
  const loadedSharedIdentityDiscoverySnapshot: LoadedSharedIdentityDiscoverySnapshot = {
    sharedIdentityID: 'family-sync-loaded-identity',
    discoveredPeers: [
      {
        peerIDCode: 'peer-code-known-before-endpoint',
        label: 'Discovered peer pending name',
        connectionState: 'connecting',
        pairingState: 'negotiating-identity',
        pairingDetail: 'Negotiating identity before exchanging keys; status can also report Waiting on relay/mesh hop, Direct connection established, Paired, Failed, Revoked identity, or Offline as discovery continues.',
        capabilities: ['capabilities pending'],
        folders: ['folders pending'],
        lastSeenAt: 'trustworthy identifier known'
      }
    ]
  };
  $: discoveredIdentityPeers = buildDiscoveredIdentityPeersFromLoadedSharedIdentity(loadedSharedIdentityDiscoverySnapshot);
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
    { id: 'peers', label: 'Peers & identity', description: 'Peers, discovery, identity pairing, and future remote instances.' },
    { id: 'transfers', label: 'Transfers', description: 'Transfer queue controls, pause/resume/cancel state, and rates.' },
    { id: 'warnings', label: 'Warnings & logs', description: 'Warnings, structured logs, and recent daemon events.' },
    { id: 'maintenance', label: 'Maintenance & backups', description: 'Scrub, repair, backup, snapshot, restore, retention, and web GUI package operations.' },
    { id: 'daemon-settings', label: 'Daemon settings', description: 'Selected-host daemon options and non-secret config patches.' },
    { id: 'desktop-settings', label: 'Desktop app settings', description: 'GUI-only desktop preferences kept separate from daemon host settings.' },
    { id: 'help', label: 'Help & details', description: 'Advanced option details and contextual help without cluttering operation views.' }
  ];
  const daemonSettingsSections: DaemonSettingsSection[] = [
    { title: 'Daemon identity & API', summary: 'Selected-host API URL, redacted API key usage, TLS/encryption policy, node identity, and pairing-sensitive controls belong to the daemon host.' },
    { title: 'Folders, peers, discovery, and transfers', summary: 'Share roots, peer endpoints, discovery mode, and transfer caps are selected-host daemon settings, not desktop app preferences.' },
    { title: 'Metadata, logging, and backup policy', summary: 'Metadata backend, maintenance budgets, structured logging, web GUI package state, backup destinations, snapshots, restore, and repair policy stay scoped to the selected engine.' }
  ];
  const desktopSettingsSections: DesktopSettingsSection[] = [
    { title: 'Theme and window behavior', summary: 'Desktop GUI settings menu options such as theme, density, remembered window placement, and tray-open behavior affect only this GUI app.' },
    { title: 'Credential storage and notifications', summary: 'Credential vault preferences, notification style, update prompts, and local UI privacy controls stay separate from selected-host daemon settings.' }
  ];
  const helpDetailsSections: HelpDetailsSection[] = [
    { title: 'Encryption, pairing, and identity details', summary: 'Dedicated help/details pages explain API encryption, identity pairing, key rotation, and revocation without crowding daily peer controls.' },
    { title: 'Maintenance, repair, and backup details', summary: 'Dedicated help/details pages explain scrub classifications, quarantine, restore, retention, and backup protection states before users run high-impact actions.' }
  ];

  async function handleTrayLaunchIfRequested() {
    const trayOpenLaunchArgument = '--open-from-tray';
    const argv = typeof window === 'undefined'
      ? []
      : [window.location.href, ...(window.location.search.includes(trayOpenLaunchArgument) ? [trayOpenLaunchArgument] : [])];
    const request = {
      source: 'launch-argument' as const,
      uri: typeof window !== 'undefined' && window.location.href.startsWith('fse-desktop://open')
        ? 'fse-desktop://open' as const
        : undefined,
      argv,
      boundary: 'separate GUI process' as const,
      daemonBoundary: 'no direct daemon process launch' as const
    };
    if (!isDaemonTrayOpenRequest(request)) {
      return;
    }
    await handleDaemonTrayOpenRequest(request, { showMainWindowFromDaemonTray });
  }

  void handleTrayLaunchIfRequested();

  function toggleInstanceGroup(groupName: string) {
    expandedInstanceGroups = {
      ...expandedInstanceGroups,
      [groupName]: !(expandedInstanceGroups[groupName] ?? true)
    };
  }

  function selectManagedInstance(instanceID: string) {
    selectedInstanceID = instanceID;
    const selected = managedDaemonInstances.find((instance) => instance.id === instanceID);
    if (selected) {
      apiBaseURL = selected.apiBaseURL;
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
      const response = await browseFilesystemDirectories({ apiBaseURL, apiKey }, path);
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

  function hydrateDiscoveredIdentityPeers(peers: DiscoveredIdentityPeer[] = discoveredIdentityPeers) {
    managedDaemonInstances = upsertDiscoveredIdentityPeerInstances(managedDaemonInstances, peers);
    expandedInstanceGroups = defaultExpandedInstanceGroups(managedDaemonInstances);
    remoteOnboardingMessage = 'Added newly discovered same-identity peers from each trustworthy unique peer ID/code; the host list will progressively fill in name, status, capabilities, folders, rates, and other metadata as negotiation/discovery completes.';
  }

  function autoPopulateLoadedSharedIdentityInstances() {
    const peers = buildDiscoveredIdentityPeersFromLoadedSharedIdentity(loadedSharedIdentityDiscoverySnapshot);
    hydrateDiscoveredIdentityPeers(peers);
    remoteOnboardingMessage = 'Automatically populate reachable same-identity instances from the loaded shared identity; each host starts from a trustworthy Peer ID/code and progressively fills in name, status, capabilities, folders, rates, and other metadata.';
  }

  async function refreshRemoteMeshStatus() {
    remoteMeshStatusMessage = 'Refreshing remote mesh document and pending-change status for the selected host.';
    try {
      const response = await fetchRemoteMeshSettings({ apiBaseURL, apiKey }, selectedManagedInstance.id);
      remoteMeshDocuments = response.documents ?? [];
      remoteMeshStatusMessage = `Remote mesh document status refreshed for ${selectedManagedInstance.label}; ${remoteMeshDocuments.length} cached per-node settings document(s) available for online, relay-reachable, and offline instances.`;
    } catch (error) {
      remoteMeshStatusMessage = error instanceof Error ? error.message : String(error);
    }
  }

  async function queueSelectedRemoteMeshSettingsChange() {
    remoteMeshStatusMessage = 'Queueing durable pending settings change for mesh replication.';
    try {
      const queued = await queueRemoteMeshSettingsCommand({ apiBaseURL, apiKey }, {
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
    remoteOnboardingMessage = '';
    try {
      if (remoteOnboardingSource === 'loaded-shared-identity') {
        remoteOnboardingSharedIdentityID = requireNonEmpty(remoteOnboardingSharedIdentityID || loadedSharedIdentityDiscoverySnapshot.sharedIdentityID, 'Loaded shared identity');
        autoPopulateLoadedSharedIdentityInstances();
        return;
      }
      const credentialRefSeed = `${remoteOnboardingSource}:${remoteOnboardingEndpoint || remoteOnboardingSharedIdentityID || remoteOnboardingLabel}`;
      const credentialRef = `desktop-vault:remote:${credentialRefSeed.toLowerCase().replace(/[^a-z0-9_.:-]/g, '-').replace(/-+/g, '-').slice(0, 96)}`;
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
      if (remoteOnboardingSource === 'api-endpoint-key') {
        const secret = requireNonEmpty(remoteOnboardingAPIKey, 'Remote API key');
        await storeRemoteInstanceCredential({
          credentialRef,
          platform: 'linux',
          instanceID: candidate.id,
          label: candidate.label,
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString()
        }, { credentialRef, secretValue: secret });
      }
      managedDaemonInstances = addRemoteDaemonInstance(managedDaemonInstances, candidate);
      expandedInstanceGroups = defaultExpandedInstanceGroups(managedDaemonInstances);
      selectedInstanceID = candidate.id;
      apiBaseURL = candidate.apiBaseURL;
      remoteOnboardingAPIKey = '';
      remoteOnboardingMessage = 'Remote instance onboarding saved endpoint/source metadata; raw API keys stay in native credential storage and pairing/identity imports remain daemon-owned.';
    } catch (error) {
      remoteOnboardingMessage = error instanceof Error ? error.message : String(error);
    }
  }

  async function connectToDaemon() {
    loading = true;
    errorMessage = '';
    status = null;
    try {
      status = await fetchDaemonStatus({ apiBaseURL, apiKey });
    } catch (error) {
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
      daemonConfig = await readDaemonConfig({ apiBaseURL, apiKey });
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
      apiTrustStatus = await fetchAPITrustStatus({ apiBaseURL, apiKey });
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
      const response = await pinActiveAPICertificate({ apiBaseURL, apiKey });
      apiTrustStatus = response;
      return response;
    });
  }

  function apiTrustStatusSummary(): string {
    if (!apiTrustStatus) {
      return 'API certificate trust status has not been fetched for the selected host.';
    }
    const fingerprint = apiTrustStatus.certificateSha256 ?? 'none';
    const trusted = apiTrustStatus.trustedCertificateConfigured ? 'trusted fingerprint configured' : 'no trusted fingerprint configured';
    const match = apiTrustStatus.trustedCertificateMatches ? 'trustedCertificateMatches=true' : 'trustedCertificateMatches=false';
    return `mode=${apiTrustStatus.mode}; tlsRequired=${apiTrustStatus.tlsRequired}; certificateSha256=${fingerprint}; ${trusted}; ${match}`;
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
    return { action: folderFormAction, id, path, mode: folderFormMode };
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

  function validateConfigPatchForm() {
    const level = requireNonEmpty(configPatchLoggingLevel, 'Logging level');
    return { logging: { level } };
  }

  async function generatePairingPackage() {
    pairingLoading = true;
    pairingMessage = '';
    try {
      const groupID = requireNonEmpty(pairingGroupID, 'Identity group ID');
      pairingPackage = await generateIdentityPairingPackage({ apiBaseURL, apiKey }, groupID);
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

  async function importParsedPairingPackage() {
    pairingLoading = true;
    try {
      const imported = parsedPairingImport ?? parseImportedIdentityPackageText(pairingImportText);
      parsedPairingImport = imported;
      const response = await importIdentityPairingPackage({ apiBaseURL, apiKey }, imported);
      pairingImportSummary = `Imported identity package for group ${response.groupId} from ${response.remoteDiscoveryId}; peer-pair key ${response.keyId ?? 'pending'} prepared at level ${response.peerPairEncryptionLevel}.`;
      pairingMessage = response.message ?? 'Identity package import accepted by daemon-owned import execution.';
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

  function setLocalControlOperationState(area: LocalControlArea, state: LocalControlOperationState) {
    localControlOperationStates = { ...localControlOperationStates, [area]: state };
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
    setLocalControlOperationState(area, {
      phase: 'completed',
      command,
      summary: `${command} request completed and status was refreshed.`,
      detail: commandResponseDetail(response)
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
      status = await fetchDaemonStatus({ apiBaseURL, apiKey });
      markLocalControlOperationCompleted(area, command, response);
    } catch (error) {
      markLocalControlOperationFailed(area, command, error);
      controlMessage = error instanceof Error ? error.message : String(error);
    } finally {
      loading = false;
    }
  }

  async function submitPeerForm() {
    const settings = { apiBaseURL, apiKey };
    await runControlCommand('peer', `peer ${peerFormAction}`, async () => sendPeerCommand(settings, validatePeerCommandForm()));
  }

  async function submitFolderForm() {
    const settings = { apiBaseURL, apiKey };
    await runControlCommand('folder', `folder ${folderFormAction}`, async () => sendFolderCommand(settings, validateFolderCommandForm()));
  }

  async function submitDiscoveryForm() {
    const settings = { apiBaseURL, apiKey };
    await runControlCommand('discovery', 'discovery update', async () => sendDiscoveryCommand(settings, validateDiscoveryCommandForm()));
  }

  async function submitTransferForm() {
    const settings = { apiBaseURL, apiKey };
    await runControlCommand('transfer', `transfer ${transferFormAction}`, async () => sendTransferCommand(settings, validateTransferCommandForm()));
  }

  async function submitMaintenanceForm() {
    const settings = { apiBaseURL, apiKey };
    await runControlCommand('maintenance', 'maintenance scrub', async () => runMaintenanceScrub(settings, validateMaintenanceCommandForm()));
  }

  async function submitWebGUIForm() {
    const settings = { apiBaseURL, apiKey };
    await runControlCommand('webGUI', `web GUI ${webGUIFormAction}`, async () => sendWebGUICommand(settings, validateWebGUICommandForm()));
  }

  async function submitConfigPatchForm() {
    const settings = { apiBaseURL, apiKey };
    await runControlCommand('config', 'config patch', async () => patchDaemonConfig(settings, validateConfigPatchForm()));
  }

  async function refreshBundledDaemonGate() {
    // loadBundledDaemonGate calls verifyBundledEngineResourceManifest with native observations.
    lifecycleLoading = true;
    bundleMessage = '';
    try {
      bundleGate = await loadBundledDaemonGate();
      bundleMessage = bundleGate.bundleVerified
        ? `Bundled daemon verified for ${bundleGate.verified.length} targets.`
        : bundleGate.reason;
    } catch (error) {
      bundleGate = null;
      bundleMessage = error instanceof Error ? error.message : String(error);
    } finally {
      lifecycleLoading = false;
    }
  }

  async function runLifecycleAction(action: BundledDaemonLifecycleAction) {
    // runBundledDaemonLifecycle calls controlVerifiedBundledDaemonLifecycle after verification.
    lifecycleLoading = true;
    bundleMessage = '';
    try {
      const response = await runBundledDaemonLifecycle(action);
      if (!response.ok) {
        throw new Error(`daemon lifecycle ${action} request failed: ${response.status}`);
      }
      bundleMessage = `Daemon lifecycle ${action} request accepted.`;
      bundleGate = await loadBundledDaemonGate();
    } catch (error) {
      bundleMessage = error instanceof Error ? error.message : String(error);
    } finally {
      lifecycleLoading = false;
    }
  }

  async function checkFirstLaunchRegistration() {
    lifecycleLoading = true;
    firstLaunchMessage = '';
    try {
      firstLaunchStatus = await getFirstLaunchDaemonRegistrationStatus();
      configureStartup = promptForStartupAtLogin(firstLaunchStatus);
      firstLaunchMessage = firstLaunchStatus.message;
    } catch (error) {
      firstLaunchMessage = error instanceof Error ? error.message : String(error);
    } finally {
      lifecycleLoading = false;
    }
  }

  async function registerBundledDaemon() {
    lifecycleLoading = true;
    firstLaunchMessage = '';
    try {
      const result = await installBundledDaemonForCurrentOS({ configureStartup });
      firstLaunchMessage = result.message;
      firstLaunchStatus = await getFirstLaunchDaemonRegistrationStatus();
    } catch (error) {
      firstLaunchMessage = error instanceof Error ? error.message : String(error);
    } finally {
      lifecycleLoading = false;
    }
  }

  async function refreshTrayAndStartupStatus() {
    lifecycleLoading = true;
    trayMessage = '';
    try {
      [trayStatus, startupIntegrationStatus] = await Promise.all([
        getDaemonTrayStatus(),
        getDaemonStartupIntegrationStatus()
      ]);
      trayMessage = `${trayStatus.message} ${startupIntegrationStatus.message}`.trim();
    } catch (error) {
      trayStatus = null;
      startupIntegrationStatus = null;
      trayMessage = error instanceof Error ? error.message : String(error);
    } finally {
      lifecycleLoading = false;
    }
  }

  async function openGuiFromTrayBridge() {
    lifecycleLoading = true;
    trayMessage = '';
    try {
      await openGuiFromDaemonTray();
      trayMessage = 'Requested the daemon-owned tray integration to open or focus the separate GUI app.';
    } catch (error) {
      trayMessage = error instanceof Error ? error.message : String(error);
    } finally {
      lifecycleLoading = false;
    }
  }

  function reflectLocalDaemonSession(session: GUIManagedNonServiceDaemonSession) {
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
      apiBaseURL = guiOwnedNonServiceDaemonSession.encryptedApiBaseURL;
      reflectLocalDaemonSession(guiOwnedNonServiceDaemonSession);
      guiOwnedNonServiceDaemonMessage = guiOwnedNonServiceDaemonSession.message;
    } catch (error) {
      try {
        guiOwnedNonServiceDaemonSession = await requestGUIOwnedNonServiceDaemonLaunch({
          sessionMode: 'persistent-user-daemon',
          preferExistingReachableDaemon: true
        });
        apiBaseURL = guiOwnedNonServiceDaemonSession.encryptedApiBaseURL;
        reflectLocalDaemonSession(guiOwnedNonServiceDaemonSession);
        guiOwnedNonServiceDaemonMessage = guiOwnedNonServiceDaemonSession.message;
      } catch (launchError) {
        guiOwnedNonServiceDaemonMessage = launchError instanceof Error ? launchError.message : String(launchError);
      }
    } finally {
      lifecycleLoading = false;
    }
  }

  onMount(() => {
    void ensureLocalDaemonConnection();
  });

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
      guiOwnedNonServiceDaemonMessage = guiOwnedNonServiceDaemonSession.message;
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
      if (guiOwnedNonServiceDaemonSession?.pid) {
        await stopGUIOwnedNonServiceDaemonThroughAPI(guiOwnedNonServiceDaemonSession.sessionID);
      }
      guiOwnedNonServiceDaemonSession = await requestGUIOwnedNonServiceDaemonLaunch({
        sessionMode: 'persistent-user-daemon',
        preferExistingReachableDaemon: false
      });
      apiBaseURL = guiOwnedNonServiceDaemonSession.encryptedApiBaseURL;
      reflectLocalDaemonSession(guiOwnedNonServiceDaemonSession);
      guiOwnedNonServiceDaemonMessage = guiOwnedNonServiceDaemonSession.message;
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
      guiOwnedNonServiceDaemonSession = await stopGUIOwnedNonServiceDaemonThroughAPI(guiOwnedNonServiceDaemonSession.sessionID);
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
</script>

<main class="host-scoped-shell">
  <aside class="host-sidebar" aria-label="Host and view navigation">
    <section class="host-card">
      <span class="eyebrow">Selected host</span>
      <h1>{selectedManagedInstance.label}</h1>
      <p>{selectedManagedInstance.statusSummary}</p>
      <small>Local bundled engine; local instance pinned first; selected host scope applies to every view except the top GUI menu.</small>
    </section>
    <section class="host-card" aria-label="Managed daemon instances">
      <span class="eyebrow">Instance registry</span>
      <p class="instance-note">Expandable left navigation panel for local and remote engines. The local instance remains pinned first and quick switching updates the selected host scope.</p>
      {#each Object.entries(groupedManagedDaemonInstances) as [groupName, instances]}
        <div class="instance-group">
          <button
            type="button"
            class="instance-group-toggle"
            aria-expanded={expandedInstanceGroups[groupName] ?? true}
            aria-controls={`instance-group-${groupName.replace(/\s+/g, '-').toLowerCase()}`}
            on:click={() => toggleInstanceGroup(groupName)}
          >
            <span>{groupName}</span>
            <small>{(expandedInstanceGroups[groupName] ?? true) ? 'Collapse' : 'Expand'} · {instances.length} host{instances.length === 1 ? '' : 's'}</small>
          </button>
          {#if expandedInstanceGroups[groupName] ?? true}
            <div class="instance-list" id={`instance-group-${groupName.replace(/\s+/g, '-').toLowerCase()}`}>
              {#each instances as instance}
                <button
                  type="button"
                  class="quick-switch"
                  class:active={selectedInstanceID === instance.id}
                  aria-current={selectedInstanceID === instance.id ? 'true' : undefined}
                  on:click={() => selectManagedInstance(instance.id)}
                >
                  <span>{instance.label}</span>
                  <strong class="connection-state" data-state={instance.connectionState}>{formatConnectionStateLabel(instance.connectionState)}</strong>
                  <small>{instance.apiBaseURL}</small>
                  <p class="peer-pairing-status" aria-label="Peer pairing/negotiation status">
                    <strong>Peer pairing/negotiation status</strong>: {buildPeerPairingStatusLine(instance)}
                  </p>
                  <span class="sr-only">Negotiating identity · Exchanging keys · Waiting on relay/mesh hop · Direct connection established · Revoked identity</span>
                  <dl class="host-status-metrics" aria-label="Online/offline state, remaining transfer data, and average transfer rates">
                    {#each buildReadableHostStatusMetrics(instance) as metric}
                      <div>
                        <dt>{metric.label}</dt>
                        <dd>{metric.value}</dd>
                      </div>
                    {/each}
                  </dl>
                </button>
              {/each}
            </div>
          {/if}
        </div>
      {/each}
      <p class="instance-note">Remote daemon instances can be added by API endpoint, pairing code, identity file, or future animated code without storing raw API keys in this registry. The host list keeps a readable status layout for Online/offline state, Remaining to receive, Remaining to send, Average receive rate, and Average send rate.</p>
    </section>
    <nav aria-label="Selected host sections">
      {#each viewCatalog as view}
        <button
          type="button"
          class:active={activeView === view.id}
          aria-current={activeView === view.id ? 'page' : undefined}
          on:click={() => activeView = view.id}
        >
          <span>{view.label}</span>
          <small>{view.description}</small>
        </button>
      {/each}
    </nav>
  </aside>

  <section class="host-content" aria-live="polite">
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
          <dd>{formatConnectionStateLabel(selectedHostContext.connectionState)}</dd>
        </div>
      </dl>
    </section>

    {#if activeView === 'overview'}
  <section class="connection-card">
    <span class="eyebrow">Engine status</span>
    <h2>Local engine</h2>
    <p class:success={guiOwnedNonServiceDaemonSession?.connectionState === 'running'}>
      {guiOwnedNonServiceDaemonSession?.connectionState === 'running' ? 'Running and reachable' : lifecycleLoading ? 'Connecting…' : 'Stopped or unreachable'}
    </p>
    <p>{guiOwnedNonServiceDaemonMessage}</p>
    {#if guiOwnedNonServiceDaemonSession}
      <dl class="host-status-grid" aria-label="Local engine state">
        <dt>Node</dt><dd>{guiOwnedNonServiceDaemonSession.nodeName ?? 'Starting'}</dd>
        <dt>Process</dt><dd>{guiOwnedNonServiceDaemonSession.pid > 0 ? `PID ${guiOwnedNonServiceDaemonSession.pid}` : 'Stopped'}</dd>
        <dt>API</dt><dd>{guiOwnedNonServiceDaemonSession.encryptedApiBaseURL}</dd>
      </dl>
    {/if}
    <div class="lifecycle-actions">
      <button type="button" on:click={ensureLocalDaemonConnection} disabled={lifecycleLoading || guiOwnedNonServiceDaemonSession?.connectionState === 'running'}>Start local engine</button>
      <button type="button" on:click={stopGUIOwnedNonServiceDaemon} disabled={lifecycleLoading || !guiOwnedNonServiceDaemonSession?.pid}>Stop</button>
      <button type="button" on:click={restartLocalDaemon} disabled={lifecycleLoading}>Restart connection</button>
    </div>
    <small>The engine runs separately and continues syncing when this window closes.</small>
  </section>

  {#if selectedManagedInstance.kind === 'remote'}
  <section class="connection-card">
    <h2>Remote engine connection</h2>
    <p>Connect to {selectedManagedInstance.label} through its authenticated API.</p>
    <label>API URL<input bind:value={apiBaseURL} name="apiBaseURL" autocomplete="off" /></label>
    <label>API key<input bind:value={apiKey} name="apiKey" type="password" autocomplete="off" /></label>
    <button type="button" on:click={connectToDaemon} disabled={loading || apiKey.length === 0}>{loading ? 'Connecting…' : 'Connect'}</button>
    {#if errorMessage}<p class="error">{errorMessage}</p>{/if}
    {#if status}<pre>{JSON.stringify(status, null, 2)}</pre>{/if}
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
    <button type="button" on:click={refreshDaemonConfig} disabled={loading || apiKey.length === 0}>Read redacted config</button>

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
      <button type="submit" disabled={loading || apiKey.length === 0}>Submit peer command</button>
      <p class="operation-status" data-phase={getLocalControlOperationState('peer').phase} aria-label="Peer operation status">
        {renderLocalControlOperationSummary(getLocalControlOperationState('peer'))}
        {#if getLocalControlOperationState('peer').detail}
          <span>{getLocalControlOperationState('peer').detail}</span>
        {/if}
      </p>
    </form>

    <form class="control-form" on:submit|preventDefault={submitFolderForm} aria-label="Folder form">
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
        <input bind:value={folderFormID} name="folderFormID" autocomplete="off" />
      </label>
      <label>
        Folder path
        <input bind:value={folderFormPath} name="folderFormPath" autocomplete="off" />
      </label>
      <label>
        Folder mode
        <select bind:value={folderFormMode} name="folderFormMode">
          <option value="sendrecv">sendrecv</option>
          <option value="sendonly">sendonly</option>
          <option value="recvonly">recvonly</option>
        </select>
      </label>
      <button type="submit" disabled={loading || apiKey.length === 0}>Submit folder command</button>
      <p class="operation-status" data-phase={getLocalControlOperationState('folder').phase} aria-label="Folder operation status">
        {renderLocalControlOperationSummary(getLocalControlOperationState('folder'))}
        {#if getLocalControlOperationState('folder').detail}
          <span>{getLocalControlOperationState('folder').detail}</span>
        {/if}
      </p>
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
      <button type="submit" disabled={loading || apiKey.length === 0}>Submit discovery update</button>
      <p class="operation-status" data-phase={getLocalControlOperationState('discovery').phase} aria-label="Discovery operation status">
        {renderLocalControlOperationSummary(getLocalControlOperationState('discovery'))}
        {#if getLocalControlOperationState('discovery').detail}
          <span>{getLocalControlOperationState('discovery').detail}</span>
        {/if}
      </p>
    </form>

    <form class="control-form" on:submit|preventDefault={submitTransferForm} aria-label="Transfer form">
      <h3>Transfer form</h3>
      <label>
        Action
        <select bind:value={transferFormAction} name="transferFormAction">
          <option value="pause">pause</option>
          <option value="resume">resume</option>
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
      <button type="submit" disabled={loading || apiKey.length === 0}>Submit transfer command</button>
      <p class="operation-status" data-phase={getLocalControlOperationState('transfer').phase} aria-label="Transfer operation status">
        {renderLocalControlOperationSummary(getLocalControlOperationState('transfer'))}
        {#if getLocalControlOperationState('transfer').detail}
          <span>{getLocalControlOperationState('transfer').detail}</span>
        {/if}
      </p>
    </form>

    <form class="control-form" on:submit|preventDefault={submitMaintenanceForm} aria-label="Maintenance form">
      <h3>Maintenance form</h3>
      <label>
        Optional folder ID
        <input bind:value={maintenanceFormFolderID} name="maintenanceFormFolderID" autocomplete="off" />
      </label>
      <button type="submit" disabled={loading || apiKey.length === 0}>Run maintenance scrub</button>
      <p class="operation-status" data-phase={getLocalControlOperationState('maintenance').phase} aria-label="Maintenance operation status">
        {renderLocalControlOperationSummary(getLocalControlOperationState('maintenance'))}
        {#if getLocalControlOperationState('maintenance').detail}
          <span>{getLocalControlOperationState('maintenance').detail}</span>
        {/if}
      </p>
    </form>

    <form class="control-form" on:submit|preventDefault={submitWebGUIForm} aria-label="Web GUI form">
      <h3>Web GUI form</h3>
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
      <button type="submit" disabled={loading || apiKey.length === 0}>Submit web GUI command</button>
      <p class="operation-status" data-phase={getLocalControlOperationState('webGUI').phase} aria-label="Web GUI operation status">
        {renderLocalControlOperationSummary(getLocalControlOperationState('webGUI'))}
        {#if getLocalControlOperationState('webGUI').detail}
          <span>{getLocalControlOperationState('webGUI').detail}</span>
        {/if}
      </p>
    </form>

    <form class="control-form" on:submit|preventDefault={submitConfigPatchForm} aria-label="Config patch form">
      <h3>Config patch form</h3>
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
      <button type="submit" disabled={loading || apiKey.length === 0}>Submit non-secret config patch</button>
      <p class="operation-status" data-phase={getLocalControlOperationState('config').phase} aria-label="Config operation status">
        {renderLocalControlOperationSummary(getLocalControlOperationState('config'))}
        {#if getLocalControlOperationState('config').detail}
          <span>{getLocalControlOperationState('config').detail}</span>
        {/if}
      </p>
    </form>

    <section class="control-form" aria-label="API certificate trust">
      <h3>API certificate trust</h3>
      <p>
        Fetch the active certificate fingerprint for the selected host, then pin it only after the user verifies the TOFU pairing context. API keys and private key material are never displayed here.
      </p>
      <div class="lifecycle-actions">
        <button type="button" on:click={refreshAPITrustStatus} disabled={loading || apiKey.length === 0}>Fetch API trust status</button>
        <button type="button" on:click={pinActiveAPICertificateForSelectedHost} disabled={loading || apiKey.length === 0 || !apiTrustStatus?.certificateSha256}>Pin active certificate</button>
      </div>
      <p class="operation-status" data-phase={getLocalControlOperationState('apiTrust').phase} aria-label="API trust operation status">
        {renderLocalControlOperationSummary(getLocalControlOperationState('apiTrust'))}
        {#if getLocalControlOperationState('apiTrust').detail}
          <span>{getLocalControlOperationState('apiTrust').detail}</span>
        {/if}
      </p>
      <p>{apiTrustStatusSummary()}</p>
      {#if apiTrustStatus}
        <dl class="host-status-grid" aria-label="API certificate trust details">
          <dt>certificateSha256</dt>
          <dd>{apiTrustStatus.certificateSha256 ?? 'not available'}</dd>
          <dt>trustedCertificateMatches</dt>
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

    {#if activeView === 'desktop-settings'}

  <section class="connection-card">
    <h2>First launch daemon setup</h2>
    <p>
      First launch checks whether the bundled daemon is registered for this OS, verifies the bundle, and asks whether startup/login/start-at-boot should be configured.
    </p>
    <button type="button" on:click={checkFirstLaunchRegistration} disabled={lifecycleLoading}>
      Check first-launch setup
    </button>
    {#if firstLaunchStatus?.registrationRequired}
      <label class="inline-choice">
        <input type="checkbox" bind:checked={configureStartup} name="configureStartup" />
        Configure automatic startup/login/start-at-boot
      </label>
      <button type="button" on:click={registerBundledDaemon} disabled={lifecycleLoading || !bundleGate?.bundleVerified}>
        Install/register bundled daemon
      </button>
    {/if}
    {#if firstLaunchMessage}
      <p>{firstLaunchMessage}</p>
    {/if}
  </section>

  <section class="connection-card">
    <h2>GUI-owned non-service daemon</h2>
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

  <section class="connection-card">
    <h2>Daemon tray and startup</h2>
    <p>
      The daemon/service owns the tray icon/status. This GUI only asks the native shell for daemonOwnedTray and startupEnabled status, and the daemon tray can open/focus the separate GUI without keeping the GUI resident.
    </p>
    <button type="button" on:click={refreshTrayAndStartupStatus} disabled={lifecycleLoading}>
      Refresh tray/startup status
    </button>
    <button type="button" on:click={openGuiFromTrayBridge} disabled={lifecycleLoading || !trayStatus?.daemonOwnedTray}>
      Simulate tray open/focus GUI
    </button>
    {#if trayStatus || startupIntegrationStatus}
      <pre>{JSON.stringify({ trayStatus, startupIntegrationStatus }, null, 2)}</pre>
    {/if}
    {#if trayMessage}
      <p>{trayMessage}</p>
    {/if}
  </section>

  <section class="connection-card">
    <h2>Bundled daemon lifecycle</h2>
    <p>
      Native desktop builds must verify packaged daemon resources before service lifecycle controls are enabled.
      Commands still go through the encrypted daemon API rather than direct process control.
    </p>
    <button type="button" on:click={refreshBundledDaemonGate} disabled={lifecycleLoading}>
      {lifecycleLoading ? 'Checking…' : 'Verify bundled daemon'}
    </button>
    <div class="lifecycle-actions">
      {#each lifecycleActions as action}
        <button
          type="button"
          on:click={() => runLifecycleAction(action)}
          disabled={lifecycleLoading || !bundleGate?.bundleVerified}
        >
          {action}
        </button>
      {/each}
    </div>
    {#if bundleMessage}
      <p class:success={bundleGate?.bundleVerified} class:error={!bundleGate?.bundleVerified}>{bundleMessage}</p>
    {/if}
  </section>
    {/if}

    {#if activeView === 'folders'}
      <section class="connection-card">
        <h2>Selected-host folders</h2>
        <p>Folder roots, sync modes, and sync groups for {selectedManagedInstance.label} belong here. Folder commands remain available from daemon settings until the richer folder table lands.</p>
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
          <button type="button" on:click={() => browseRemoteFolderTree()} disabled={remoteBrowseLoading || !canBrowseSelectedHostFilesystem || apiKey.length === 0}>
            {remoteBrowseLoading ? 'Browsing…' : 'Browse selected host'}
          </button>
          <button type="button" on:click={() => browseRemoteFolderTree(folderFormPath)} disabled={remoteBrowseLoading || !canBrowseSelectedHostFilesystem || apiKey.length === 0 || folderFormPath.length === 0}>
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
        <p>Peer lists, discovery, identity pairing, and remote-instance onboarding for {selectedManagedInstance.label} belong here instead of being buried in one long settings page.</p>
        <p>Newly discovered same-identity peers appear in the host list as soon as any trustworthy identifier is known, even when the only initial data is a unique Peer ID/code.</p>
        <button type="button" on:click={autoPopulateLoadedSharedIdentityInstances}>Automatically populate reachable same-identity instances</button>
        <button type="button" on:click={() => hydrateDiscoveredIdentityPeers()}>Show newly discovered same-identity peers</button>
        <ul>
          {#each discoveredIdentityPeers as peer}
            <li>Peer ID/code {peer.peerIDCode}: progressively fill in name, status, capabilities, folders, rates, and other metadata while negotiation/discovery completes.</li>
          {/each}
        </ul>
      </section>
      <section class="connection-card">
        <h2>Remote instance onboarding form</h2>
        <p>Add remote instances directly with an API endpoint/key, pasted pairing code, imported identity file, scanned animated code, or loaded shared identity. Raw API keys stay in native credential storage; the registry stores only endpoint/source metadata and credentialRef.</p>
        <form class="control-form" on:submit|preventDefault={submitRemoteInstanceOnboarding} aria-label="Remote instance onboarding form">
          <label>
            Onboarding source
            <select bind:value={remoteOnboardingSource} name="remoteOnboardingSource">
              <option value="api-endpoint-key">Direct API endpoint/key</option>
              <option value="pasted-pairing-code">Pasted pairing code</option>
              <option value="imported-identity-file">Imported identity file</option>
              <option value="scanned-animated-code">Scanned animated code</option>
              <option value="loaded-shared-identity">Loaded shared identity</option>
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
          <label>
            Pasted pairing code
            <textarea bind:value={remoteOnboardingPairingCode} name="remoteOnboardingPairingCode" rows="3"></textarea>
          </label>
          <label>
            Imported identity file
            <textarea bind:value={remoteOnboardingIdentityFileText} name="remoteOnboardingIdentityFileText" rows="3"></textarea>
          </label>
          <label>
            Scanned animated code summary
            <input bind:value={remoteOnboardingAnimatedCodeSummary} name="remoteOnboardingAnimatedCodeSummary" autocomplete="off" />
          </label>
          <label>
            Loaded shared identity
            <input bind:value={remoteOnboardingSharedIdentityID} name="remoteOnboardingSharedIdentityID" autocomplete="off" />
          </label>
          <button type="submit">Add remote instance</button>
        </form>
        {#if remoteOnboardingMessage}
          <p>{remoteOnboardingMessage}</p>
        {/if}
      </section>
      <section class="connection-card remote-mesh-settings-plan">
        <h2>Trusted eventually-consistent mesh management</h2>
        <p>Each identity-linked daemon will publish a per-node settings document so offline instances can be inspected or queued for later adoption through trusted relays instead of requiring direct reachability.</p>
        <p>Design boundary: the config file remains local import/export; remote edits become durable pending changes against the selected node's canonical document and must be authenticated, validated, applied, and acknowledged by that owner node.</p>
        <div class="lifecycle-actions">
          <button type="button" on:click={refreshRemoteMeshStatus} disabled={loading || apiKey.length === 0}>Refresh remote mesh status</button>
          <button type="button" on:click={queueSelectedRemoteMeshSettingsChange} disabled={loading || apiKey.length === 0}>Queue selected-host settings change</button>
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
          Generate a same-identity pairing package as copyable pairing text, a downloadable identity file, a QR fallback payload, and an animated visual code frame list.
          The package still contains identity secret material, so it is never written to logs or generic status events.
        </p>
        <label>
          Identity group ID
          <input bind:value={pairingGroupID} placeholder="family-sync" />
        </label>
        <button type="button" on:click={generatePairingPackage} disabled={pairingLoading}>
          Generate pairing package
        </button>
        {#if pairingExportText}
          <label>
            copyable pairing text
            <textarea readonly rows="8" value={pairingExportText}></textarea>
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
          <textarea bind:value={pairingImportText} rows="8" placeholder="Paste identity package JSON here"></textarea>
        </label>
        <input type="file" accept="application/json,.json" on:change={handlePairingFileUpload} aria-label="Upload identity file" />
        <button type="button" on:click={parsePairingImportText}>Parse pasted identity file</button>
        <button type="button" on:click={importParsedPairingPackage} disabled={pairingLoading}>Import identity package</button>
        <p>Import uses daemon-owned import execution over the authenticated encrypted API and returns only redacted peer-pair identifiers.</p>
        {#if pairingImportSummary}
          <p class="success">{pairingImportSummary}</p>
        {/if}
        {#if pairingMessage}
          <p>{pairingMessage}</p>
        {/if}
      </section>
    {/if}

    {#if activeView === 'transfers'}
      <section class="connection-card">
        <h2>Selected-host transfers</h2>
        <p>Transfer queues, active rates, pauses, cancellations, and source/path explanations for {selectedManagedInstance.label} will be scoped to the selected host here.</p>
      </section>
    {/if}

    {#if activeView === 'warnings'}
      <section class="connection-card">
        <h2>Selected-host warnings & logs</h2>
        <p>Maintenance warnings, inaccessible files, structured logs, and recent daemon events for {selectedManagedInstance.label} get a dedicated readable home.</p>
      </section>
    {/if}

    {#if activeView === 'maintenance'}
      <section class="connection-card">
        <h2>Selected-host maintenance & backups</h2>
        <p>Scrub, repair, snapshot, restore, retention, backup availability, and optional web GUI package operations for {selectedManagedInstance.label} are grouped here.</p>
      </section>
    {/if}

    {#if activeView === 'desktop-settings'}
      <section class="connection-card">
        <h2>Desktop GUI settings menu</h2>
        <p>These preferences belong to the desktop application itself and must not be written into the selected host's daemon config.</p>
        <div class="settings-grid" aria-label="Desktop GUI settings sections">
          {#each desktopSettingsSections as section}
            <article class="settings-card">
              <h3>{section.title}</h3>
              <p>{section.summary}</p>
            </article>
          {/each}
        </div>
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
  </section>
</main>

<style>
  main {
    min-height: 100vh;
    font-family: system-ui, sans-serif;
    background: #10131a;
    color: #f4f7fb;
  }
  .host-scoped-shell {
    display: grid;
    grid-template-columns: minmax(17rem, 22rem) minmax(0, 1fr);
    align-items: start;
    gap: 1rem;
    padding: 1rem;
  }
  .host-sidebar {
    position: sticky;
    top: 1rem;
    display: grid;
    gap: 1rem;
  }
  .host-card {
    display: grid;
    gap: 0.5rem;
    padding: 1.25rem;
    border: 1px solid #2b3344;
    border-radius: 1rem;
    background: #171c26;
  }
  .host-card h1,
  .host-card p {
    margin: 0;
  }
  .eyebrow {
    color: #8fa3c6;
    font-size: 0.8rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }
  .host-sidebar nav {
    display: grid;
    gap: 0.5rem;
  }
  .host-sidebar nav button {
    display: grid;
    gap: 0.25rem;
    text-align: left;
    background: #171c26;
    color: inherit;
  }
  .host-sidebar nav button.active,
  .host-sidebar nav button[aria-current='page'] {
    border-color: #80a7ff;
    background: #202a3d;
  }
  .host-sidebar nav small {
    color: #bac7df;
    line-height: 1.35;
  }
  .instance-group,
  .instance-list {
    display: grid;
    gap: 0.5rem;
  }
  .instance-group-toggle,
  .quick-switch {
    display: grid;
    gap: 0.25rem;
    width: 100%;
    text-align: left;
    background: #111722;
    color: inherit;
  }
  .instance-group-toggle {
    border-color: #44506a;
  }
  .quick-switch.active,
  .quick-switch[aria-current='true'] {
    border-color: #80a7ff;
    background: #202a3d;
  }
  .connection-state {
    justify-self: start;
    padding: 0.15rem 0.45rem;
    border-radius: 999px;
    background: #263148;
    color: #dbe7ff;
    font-size: 0.8rem;
  }
  .connection-state[data-state='online'],
  .connection-state[data-state='paired'] {
    background: #1d5033;
    color: #c9ffd8;
  }
  .connection-state[data-state='failed'],
  .connection-state[data-state='revoked'] {
    background: #5a2525;
    color: #ffd1d1;
  }
  .peer-pairing-status {
    margin: 0;
    padding: 0.4rem;
    border-radius: 0.35rem;
    background: #121b2a;
    color: #d8e2f8;
    line-height: 1.35;
  }
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }
  .host-status-metrics {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 0.35rem;
    margin: 0;
  }
  .host-status-metrics div {
    display: grid;
    gap: 0.1rem;
    padding: 0.35rem;
    border-radius: 0.35rem;
    background: #0e1420;
  }
  .host-status-metrics dt {
    color: #8fa3c6;
    font-size: 0.72rem;
    text-transform: uppercase;
  }
  .host-status-metrics dd {
    margin: 0;
    color: #f4f7fb;
    font-weight: 700;
  }
  .instance-note {
    color: #bac7df;
    line-height: 1.4;
  }
  .host-content {
    display: grid;
    justify-items: center;
    gap: 1rem;
  }
  .host-scope-banner {
    width: min(44rem, calc(100vw - 2rem));
    display: grid;
    gap: 0.75rem;
    padding: 1.25rem;
    border: 1px solid #405177;
    border-radius: 1rem;
    background: #172033;
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
    background: #0e1420;
  }
  .host-scope-details dt {
    color: #8fa3c6;
    font-size: 0.72rem;
    text-transform: uppercase;
  }
  .host-scope-details dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
  .connection-card {
    width: min(44rem, calc(100vw - 2rem));
    display: grid;
    gap: 1rem;
    padding: 2rem;
    border: 1px solid #2b3344;
    border-radius: 1rem;
    background: #171c26;
  }
  label {
    display: grid;
    gap: 0.35rem;
  }
  input, button, select, textarea {
    font: inherit;
    padding: 0.65rem 0.75rem;
    border-radius: 0.5rem;
    border: 1px solid #3c465b;
  }
  textarea {
    color: #f4f7fb;
    background: #0e1420;
  }
  button {
    cursor: pointer;
  }
  .error {
    color: #ffb4b4;
  }
  .success {
    color: #b8f7c0;
  }
  .control-form {
    display: grid;
    gap: 0.75rem;
    padding: 1rem;
    border: 1px solid #2b3344;
    border-radius: 0.75rem;
    background: #111722;
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
    background: #0e1420;
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
  .operation-status span {
    color: #bac7df;
  }
  .settings-grid {
    display: grid;
    gap: 0.75rem;
  }
  .settings-card {
    padding: 1rem;
    border: 1px solid #2b3344;
    border-radius: 0.75rem;
    background: #111722;
  }
  .settings-card h3,
  .settings-card p {
    margin: 0;
  }
  .settings-card p {
    margin-top: 0.35rem;
    color: #bac7df;
  }
  .download-link {
    color: #b8d2ff;
  }
</style>
