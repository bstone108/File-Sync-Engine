package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"filesyncengine/internal/api"
	"filesyncengine/internal/apicontrol"
	"filesyncengine/internal/apistate"
	"filesyncengine/internal/backup"
	"filesyncengine/internal/backupcontrol"
	"filesyncengine/internal/cli"
	"filesyncengine/internal/clicontrol"
	"filesyncengine/internal/clioutput"
	"filesyncengine/internal/cliruntime"
	"filesyncengine/internal/commanddispatch"
	"filesyncengine/internal/config"
	"filesyncengine/internal/containerbootstrap"
	"filesyncengine/internal/daemonapi"
	"filesyncengine/internal/daemonapihandlers"
	"filesyncengine/internal/daemonclient"
	"filesyncengine/internal/daemoncontrol"
	"filesyncengine/internal/daemonfolders"
	"filesyncengine/internal/daemonlifecycle"
	"filesyncengine/internal/daemonlogging"
	"filesyncengine/internal/daemonloop"
	"filesyncengine/internal/daemonmonitor"
	"filesyncengine/internal/daemonreload"
	"filesyncengine/internal/daemonrun"
	"filesyncengine/internal/daemonruntime"
	"filesyncengine/internal/daemonstop"
	"filesyncengine/internal/daemonwebgui"
	"filesyncengine/internal/discovery"
	"filesyncengine/internal/discoverycontrol"
	"filesyncengine/internal/foldersync"
	"filesyncengine/internal/maintenance"
	"filesyncengine/internal/maintenancecli"
	"filesyncengine/internal/maintenancecontrol"
	"filesyncengine/internal/metadatacli"
	"filesyncengine/internal/metadatacontrol"
	"filesyncengine/internal/metadataops"
	"filesyncengine/internal/metadatareconcile"
	"filesyncengine/internal/metadatastore"
	"filesyncengine/internal/monitor"
	"filesyncengine/internal/pairing"
	"filesyncengine/internal/peerevents"
	"filesyncengine/internal/peerpullplan"
	"filesyncengine/internal/peersync"
	"filesyncengine/internal/routing"
	"filesyncengine/internal/runtimesync"
	"filesyncengine/internal/scancli"
	"filesyncengine/internal/servicecontrol"
	"filesyncengine/internal/snapshotapi"
	"filesyncengine/internal/snapshotcli"
	"filesyncengine/internal/snapshotcontrol"
	"filesyncengine/internal/state"
	"filesyncengine/internal/streamcontrol"
	"filesyncengine/internal/structuredlog"
	"filesyncengine/internal/transfercontrol"
	"filesyncengine/internal/validatecontrol"
	"filesyncengine/internal/webgui"
)

var newRuntimeDHTRouter = func(cfg config.Config) discovery.DHTRouter {
	router, err := discovery.NewLibp2pDHTRouter(context.Background(), discovery.Libp2pDHTRouterOptions{NodeID: cfg.NodeName})
	if err != nil {
		daemonlogging.DiscoveryRouterUnavailable(err)
		return nil
	}
	return router
}

const configQuietPeriod = 15 * time.Second
const pollInterval = time.Second
const discoveryPollInterval = 30 * time.Second
const metadataReconciliationInterval = 45 * time.Second

// CLI command failures intentionally use concise human-readable stderr lines.
// Long-running daemon operational logs use structured JSON records instead.
func cliErrorLine(context string, err error) string {
	return clioutput.ErrorLine(context, err)
}

func exitCLI(context string, err error) {
	fmt.Fprintln(os.Stderr, cliErrorLine(context, err))
	os.Exit(1)
}

func exitCLIF(format string, args ...any) {
	fmt.Fprint(os.Stderr, clioutput.FormattedLine(format, args...))
	os.Exit(1)
}

func main() {
	exe, err := os.Executable()
	if err != nil {
		exitCLIF("resolve executable: %v", err)
	}
	err = cliruntime.Run(os.Args[1:], exe, cliruntime.Runners{
		Start:              run,
		Stop:               stop,
		Status:             status,
		Validate:           validateConfig,
		Scan:               scanConfigured,
		Config:             handleConfig,
		Peer:               handlePeer,
		Folder:             handleFolder,
		Stream:             handleStream,
		Metadata:           handleMetadata,
		Maintenance:        handleMaintenance,
		Snapshot:           handleSnapshot,
		Service:            handleService,
		WebGUI:             handleWebGUI,
		Identity:           handleIdentity,
		ContainerBootstrap: handleContainerBootstrap,
	})
	if err != nil {
		exitCLIF("%s: %v", commanddispatch.Usage, err)
	}
}

func run(configPath string) {
	cfg, _, err := config.EnsureAPIKey(configPath)
	if err != nil {
		exitCLIF("ensure api key: %v", err)
	}
	if err := daemonlogging.Configure(cfg, configPath); err != nil {
		exitCLIF("configure logging: %v", err)
	}
	mgr, err := config.NewDebouncedManager(configPath, configQuietPeriod)
	if err != nil {
		exitCLIF("load config: %v", err)
	}
	configVersion := uint64(1)
	metadataStore, metadataStorePath, err := openConfiguredMetadataStore(cfg, configPath)
	if err != nil {
		exitCLIF("open metadata store: %v", err)
	}
	defer metadataStore.Close()
	metadataIdentity := metadatastore.CurrentIdentity(cfg, configPath)
	apiServer := api.NewServer(apiStateFromConfigWithStore(cfg, configPath, configVersion, "running", metadataStore), cfg.API.Key)
	stopSignal := daemonstop.NewSignal()
	apiServer.SetStopHandler(stopSignal.Handler)
	registerStoreBackedAPIHandlers(apiServer, cfg, configPath, metadataStore, metadataStorePath)
	transferControl := transfercontrol.New()
	webGUIServer := webgui.NewServer()
	webGUIServer.SetNativeAPI(apiServer.Router(), cfg.API.Key)
	daemonapihandlers.RegisterStatic(apiServer, daemonapihandlers.BuildStaticHandlers(daemonapihandlers.StaticRuntimeDeps{
		ConfigPath:      configPath,
		CurrentStore:    func() state.JSONStore { return metadataStore },
		TransferControl: transferControl,
		WebGUIServer:    webGUIServer,
		CurrentConfig:   func() config.Config { return cfg },
	}))
	webGUIStartup := daemonwebgui.Start(cfg, webGUIServer, apiServer)
	if webGUIStartup.Err != nil {
		structuredlog.Event("warn", "webgui.startup.failed", "optional web GUI startup failed", map[string]any{"error": webGUIStartup.Err.Error()})
	}
	syncRunner := foldersync.New(syncFolders(cfg))
	liveEndpointObservations := []routing.EndpointObservation{}
	httpServer, httpServerPlan, err := daemonapi.PrepareHTTPServer(daemonapi.PrepareHTTPServerOptions{
		Config:          &cfg,
		ConfigPath:      configPath,
		Handler:         apiServer.Router(),
		EnsureTLSAssets: config.EnsureAPITLSAssets,
	})
	if err != nil {
		daemonlogging.APIServerStopped(err)
		return
	}
	if httpServerPlan.Enabled {
		go daemonapi.ServePreparedHTTPServer(httpServer, httpServerPlan, daemonlogging.APIListening, daemonlogging.APIServerStopped)
	}
	emitMonitorEvent := func(event monitor.Event) {
		peerPulls := peerPullsWithEndpointObservations(cfg, event.FolderID, liveEndpointObservations)
		daemonruntime.HandleMonitorEvent(daemonruntime.MonitorEventOptions{
			Event:        event,
			Publisher:    apiServer,
			TransferGate: transferControl,
			SyncRunner:   syncRunner,
			PeerPulls:    peerPulls,
			PeerPuller: daemonruntime.PeerPullerFunc(func(pull peerPull) (peersync.Result, error) {
				return peersync.PullFolderWithOptions(pull.BaseURL, pull.APIKey, pull.FolderID, pull.LocalPath, peersync.PullOptions{ReceiveBytesPerSecond: pull.ReceiveBytesPerSecond, BlockSources: pull.BlockSources})
			}),
			WarningHandler: func(warnings []foldersync.InaccessibleWarning) {
				publishFolderWarnings(apiServer, warnings)
			},
			Cooperative: func(folderID string, pulls []peerPull) {
				runtimesync.PublishCooperativeBlockFetchPlans(apiServer, folderID, runtimesync.PeerPullsFromPlans(pulls))
			},
		})
	}
	monitorOpts := monitor.Options{EventDebounce: 500 * time.Millisecond, FallbackInterval: time.Minute}
	activeMonitorFolders := daemonmonitor.FoldersFromConfig(cfg)
	mon, err := monitor.New(activeMonitorFolders, monitorOpts, emitMonitorEvent)
	var activeMonitor daemonmonitor.Closable
	defer func() {
		_ = daemonrun.Cleanup(daemonrun.CleanupOptions{Monitor: activeMonitor, HTTPServer: httpServer, HTTPShutdownTimeout: 5 * time.Second})
	}()
	if err != nil {
		daemonlogging.MonitorUnavailable(err)
	} else {
		activeMonitor = mon
	}
	daemonRuntime := daemonreload.RuntimeState{
		Config:            cfg,
		Version:           configVersion,
		MetadataStore:     metadataStore,
		MetadataStorePath: metadataStorePath,
		MetadataIdentity:  metadataIdentity,
		Monitor:           activeMonitor,
		MonitorFolders:    activeMonitorFolders,
	}
	daemonlogging.DaemonStarted(mgr.Current().NodeName, len(mgr.Current().Folders), configPath)
	schedule := daemonloop.NewPollSchedule(time.Now(), time.Now())
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stopSignal.Done():
			stopProjection := daemonlifecycle.ApplyStopRequest(apiServer.CurrentState())
			apiServer.UpdateState(stopProjection.State)
			apiServer.Publish(stopProjection.Event)
			structuredlog.Event(stopProjection.LogLevel, stopProjection.LogEvent, stopProjection.LogMessage, stopProjection.LogFields)
			return
		case <-ticker.C:
		}
		now := time.Now()
		daemonloop.RunDueWork(&schedule, now, daemonloop.DueWorkOptions{
			DiscoveryInterval: discoveryPollInterval,
			MetadataInterval:  metadataReconciliationInterval,
			Discovery: func() {
				liveEndpointObservations = processDiscoveryPollWithEndpointObservations(apiServer, runtimeDiscoverySources(cfg), cfg)
			},
			Metadata: func() {
				processMetadataReconciliation(context.Background(), apiServer, cfg, metadataStore, runtimeMetadataDialer(liveEndpointObservations, apistate.RoutingNetworkHints(cfg)))
			},
		})
		changed, err := mgr.ReloadIfQuiet(time.Now())
		if err != nil {
			daemonlogging.ConfigReloadRejected(err)
			continue
		}
		if changed {
			reloadResult := daemonreload.Apply(mgr.Current(), &daemonRuntime, daemonreload.Options{
				ConfigPath:       configPath,
				ConfigureLogging: daemonlogging.Configure,
				ReloadStore: func(current metadatastore.Identity, currentStore state.JSONStore, next config.Config, configPath string) (state.JSONStore, string, metadatastore.Identity, bool, error) {
					return metadatastore.ReloadStore(current, currentStore, next, configPath, openConfiguredMetadataStore)
				},
				RebuildMonitor: func(current daemonmonitor.Closable, active []monitor.Folder, next config.Config) (daemonmonitor.Closable, bool, error) {
					return daemonmonitor.Rebuild(current, active, next, func(folders []monitor.Folder) (daemonmonitor.Closable, error) {
						return monitor.New(folders, monitorOpts, emitMonitorEvent)
					})
				},
				StateForConfig: apiStateFromConfigWithStore,
				UpdateState:    apiServer.UpdateState,
				RegisterStoreHandlers: func(next config.Config, configPath string, store state.JSONStore, storePath string) {
					registerStoreBackedAPIHandlers(apiServer, next, configPath, store, storePath)
				},
				Publish: apiServer.Publish,
			})
			if reloadResult.LoggingError != nil {
				daemonlogging.ConfigReloadRejected(reloadResult.LoggingError)
				continue
			}
			if reloadResult.MetadataReloadError != nil {
				daemonlogging.ReloadedMetadataStoreOpenFailed(reloadResult.MetadataReloadError)
			}
			if reloadResult.MonitorRebuildError != nil {
				daemonlogging.MonitorRebuildFailed(reloadResult.MonitorRebuildError)
			}
			cfg = daemonRuntime.Config
			configVersion = daemonRuntime.Version
			metadataStore = daemonRuntime.MetadataStore
			metadataStorePath = daemonRuntime.MetadataStorePath
			metadataIdentity = daemonRuntime.MetadataIdentity
			activeMonitor = daemonRuntime.Monitor
			activeMonitorFolders = daemonRuntime.MonitorFolders
			syncRunner = foldersync.New(syncFolders(cfg))
			daemonlogging.ConfigReloaded(len(cfg.Folders))
		}
	}
}

func syncFolders(cfg config.Config) []foldersync.Folder {
	return daemonfolders.SyncFolders(cfg)
}

func registerStoreBackedAPIHandlers(server *api.Server, cfg config.Config, configPath string, store state.JSONStore, storePath string) {
	daemonapihandlers.RegisterStoreBacked(server, daemonapihandlers.StoreBackedHandlers{
		MaintenanceScrub: func(ctx context.Context, req api.MaintenanceScrubRequest) (api.MaintenanceScrubResponse, error) {
			started := time.Now().UTC()
			results, err := maintenanceScrubConfiguredWithStore(cli.Options{Command: cli.CommandMaintenance, Action: cli.ActionScrub, ID: req.FolderID}, cfg, configPath, store, storePath)
			if err != nil {
				return api.MaintenanceScrubResponse{}, err
			}
			return maintenanceScrubAPIResponse(started, time.Now().UTC(), results), nil
		},
		BackupScrub: func(ctx context.Context, req api.BackupScrubRequest) (api.BackupScrubResponse, error) {
			return backupScrubConfiguredWithStore(cli.Options{Command: cli.CommandMaintenance, Action: cli.ActionBackupScrub}, cfg, configPath, store)
		},
		BackupJobs: func(ctx context.Context, req api.BackupJobsRequest) (api.BackupJobsResponse, error) {
			return backupJobsAPIResponse(store, req)
		},
		Snapshot: func(ctx context.Context, req api.SnapshotRequest) (api.SnapshotResponse, error) {
			return snapshotAPIResponse(req, cfg, store, configPath)
		},
		RestorePlan: func(ctx context.Context, req api.RestorePlanRequest) (api.RestorePlanResponse, error) {
			return snapshotRestorePlanAPIResponse(req, cfg, store, configPath)
		},
		Restore: func(ctx context.Context, req api.RestoreRequest) (api.RestoreResponse, error) {
			return snapshotRestoreAPIResponse(req, cfg, store, configPath)
		},
		SnapshotRetention: func(ctx context.Context, req api.SnapshotRetentionRequest) (api.SnapshotRetentionResponse, error) {
			return snapshotRetentionAPIResponse(req, cfg, store, configPath)
		},
		MeshSettings: func(ctx context.Context, req api.MeshSettingsRequest) (api.MeshSettingsResponse, error) {
			return meshSettingsAPIResponse(store, req)
		},
		MeshSettingsCommand: func(ctx context.Context, req api.MeshSettingsCommandRequest) (api.MeshSettingsCommandResponse, error) {
			return meshSettingsCommandAPIResponse(store, cfg.NodeName, req)
		},
	})
}

type peerPull = peerpullplan.Pull

func publishFolderWarnings(server *api.Server, warnings []foldersync.InaccessibleWarning) {
	state, events := apicontrol.ApplyFolderWarningProjections(server.CurrentState(), warnings)
	for _, event := range events {
		server.Publish(event)
	}
	server.UpdateState(state)
}

func publishMaintenanceScrubIssue(server *api.Server, issue maintenance.FileScrubIssue, quarantinePath string) string {
	state, projection := apicontrol.ApplyMaintenanceWarningProjection(server.CurrentState(), issue, quarantinePath)
	server.UpdateState(state)
	server.Publish(projection.Event)
	structuredlog.Event("warn", projection.Event.Type, projection.Message, projection.LogFields)
	return projection.Message
}

func peerPulls(cfg config.Config, folderID string) []peerPull {
	return peerPullsWithEndpointObservations(cfg, folderID, nil)
}

func planRuntimeCooperativeBlockFetches(folderID string, pulls []peerPull) []routing.CooperativeBlockFetchPlan {
	return runtimesync.PlanCooperativeBlockFetches(folderID, runtimesync.PeerPullsFromPlans(pulls))
}

func peerPullsWithEndpointObservations(cfg config.Config, folderID string, sidecarObservations []routing.EndpointObservation) []peerPull {
	return peerpullplan.Plan(cfg, folderID, sidecarObservations)
}

func runtimeDiscoverySources(cfg config.Config) []discovery.Source {
	return discoverycontrol.BuildRuntimeSources(cfg, newRuntimeDHTRouter)
}

func peerSyncFinishedMessage(result peersync.Result) string {
	return peerevents.SyncFinishedMessage(result)
}

func peerSyncFinishedMessageWithRoute(result peersync.Result, pull peerPull) string {
	return peerevents.SyncFinishedMessageWithRoute(result, peerevents.Route{
		Path:        string(pull.Path),
		Network:     string(pull.Network),
		RouteReason: string(pull.RouteReason),
	})
}

type metadataReconciliationResult struct {
	Started   int
	Completed int
	Failed    int
}

type metadataReconciliationDialer = metadatareconcile.CatchupDialer

func processMetadataReconciliation(ctx context.Context, server *api.Server, cfg config.Config, store state.JSONStore, dial metadataReconciliationDialer) metadataReconciliationResult {
	result := metadatareconcile.ProcessCatchup(ctx, metadatareconcile.CatchupOptions{
		Publisher: server,
		Config:    cfg,
		Store:     store,
		Dial:      metadatareconcile.CatchupDialer(dial),
	})
	return metadataReconciliationResult{Started: result.Started, Completed: result.Completed, Failed: result.Failed}
}

func runtimeMetadataDialer(sidecarObservations []routing.EndpointObservation, networkHints routing.NetworkHints) metadataReconciliationDialer {
	return metadatareconcile.RuntimeDialer(metadatareconcile.RuntimeDialerOptions{EndpointObservations: sidecarObservations, NetworkHints: networkHints})
}

func processDiscoveryPoll(server *api.Server, sources []discovery.Source) {
	_ = processDiscoveryPollWithEndpointObservations(server, sources, config.Config{})
}

func processDiscoveryPollWithEndpointObservations(server *api.Server, sources []discovery.Source, cfg config.Config) []routing.EndpointObservation {
	return discoverycontrol.ProcessPoll(server, sources, apistate.RoutingNetworkHints(cfg))
}

func apiStateFromConfig(cfg config.Config, configPath string, version uint64, status string) api.State {
	return apistate.BuildConfiguredState(apistate.ConfiguredStateBuildOptions{Config: cfg, ConfigPath: configPath, Version: version, Status: status})
}

func apiStateFromConfigWithStore(cfg config.Config, configPath string, version uint64, status string, store state.JSONStore) api.State {
	return apistate.BuildState(apistate.StateBuildOptions{
		Config:         cfg,
		ConfigPath:     configPath,
		Version:        version,
		Status:         status,
		Store:          store,
		ArchiveRoot:    snapshotArchivePath(cfg, configPath),
		CheckpointRoot: snapshotcontrol.CheckpointRootPath(cfg, configPath),
	})
}

func status(configPath string) {
	body, err := daemoncontrol.RequestStatus(configPath, daemonclient.Request)
	if err != nil {
		exitCLI("", err)
	}
	fmt.Print(string(body))
}

func stop(configPath string) {
	body, err := daemoncontrol.RequestStop(configPath, daemonclient.Request)
	if err != nil {
		exitCLI("", err)
	}
	fmt.Print(string(body))
}

func handleWebGUI(opts cli.Options, configPath string) {
	body, err := daemoncontrol.RequestWebGUI(configPath, string(opts.Action), daemonclient.Request)
	if err != nil {
		exitCLI("", err)
	}
	fmt.Print(string(body))
}

func handleIdentity(opts cli.Options, configPath string) {
	var (
		body []byte
		err  error
	)
	switch opts.Action {
	case cli.ActionExport:
		body, err = daemoncontrol.RequestIdentityExport(configPath, opts.ID, daemonclient.Request)
	case cli.ActionImport:
		body, err = daemoncontrol.RequestIdentityImport(configPath, opts.Path, daemonclient.Request)
	default:
		exitCLIF("unsupported identity action: %s", opts.Action)
	}
	if err != nil {
		exitCLI("", err)
	}
	fmt.Print(string(body))
}

func handleContainerBootstrap(configPath string) {
	if err := containerbootstrap.RunFromEnvironment(configPath); err != nil {
		exitCLI("container bootstrap", err)
	}
}

func daemonAPIRequest(cfg config.Config, method string, path string, body io.Reader) []byte {
	responseBody, err := daemonclient.Request(cfg, method, path, body)
	if err != nil {
		exitCLI("", err)
	}
	return responseBody
}

func daemonAPIRequestOptions(cfg config.Config, method string, path string, body io.Reader) (string, *http.Client, error) {
	return daemonclient.RequestOptions(cfg, method, path, body)
}

func daemonAPITLSClientConfig(cfg config.Config) (*tls.Config, error) {
	return daemonclient.TLSClientConfig(cfg)
}

func apiTrustAPIResponse(configPath string) (api.APITrustResponse, error) {
	return apicontrol.HandleAPITrustStatus(configPath)
}

func apiTrustCommandAPIResponse(configPath string, req api.APITrustCommandRequest) (api.APITrustCommandResponse, error) {
	return apicontrol.HandleAPITrustCommand(configPath, req)
}

func apiCertificateFingerprintSHA256(certPEM []byte) (string, error) {
	return apicontrol.CertificateFingerprintSHA256(certPEM)
}

func validateConfig(configPath string) {
	result, err := validatecontrol.ValidateConfig(configPath)
	if err != nil {
		exitCLI("", err)
	}
	fmt.Printf("config valid: %s\n", result.ConfigPath)
}

func scanConfigured(opts cli.Options, configPath string) {
	output, err := scancli.RunConfigured(configPath, opts.ID)
	if err != nil {
		exitCLI("", err)
	}
	fmt.Print(output)
}

func handleMaintenance(opts cli.Options, configPath string) {
	output, err := maintenancecli.Run(opts, maintenancecli.Runners{
		Scrub: func(opts cli.Options) ([]maintenancecontrol.ScrubResult, error) {
			return maintenanceScrubConfigured(opts, configPath)
		},
		BackupScrub: func(opts cli.Options) (api.BackupScrubResponse, error) {
			return backupScrubConfigured(opts, configPath)
		},
	})
	if err != nil {
		exitCLI("", err)
	}
	fmt.Print(output)
}

func backupScrubConfigured(opts cli.Options, configPath string) (api.BackupScrubResponse, error) {
	_ = opts
	return backupcontrol.RunConfigured(configPath)
}

func backupScrubConfiguredWithStore(opts cli.Options, cfg config.Config, configPath string, store state.JSONStore) (api.BackupScrubResponse, error) {
	_ = opts
	return backupcontrol.RunScrub(cfg, configPath, store)
}

type maintenanceScrubResult = maintenancecontrol.ScrubResult

func maintenanceScrubAPIResponse(started, finished time.Time, results []maintenanceScrubResult) api.MaintenanceScrubResponse {
	return apicontrol.MaintenanceScrubResponse(started, finished, apicontrol.MaintenanceScrubResultsFromControl(results))
}

func maintenanceScrubConfigured(opts cli.Options, configPath string) ([]maintenanceScrubResult, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return nil, err
	}
	store, storePath, err := openConfiguredMetadataStore(cfg, configPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return maintenanceScrubConfiguredWithStore(opts, cfg, configPath, store, storePath)
}

func maintenanceScrubConfiguredWithStore(opts cli.Options, cfg config.Config, configPath string, store state.JSONStore, storePath string) ([]maintenanceScrubResult, error) {
	_ = configPath
	return maintenancecontrol.RunScrub(cfg, store, storePath, opts.ID)
}

func effectiveFolderMaintenance(global config.MaintenanceConfig, folder config.MaintenanceConfig) config.MaintenanceConfig {
	return maintenancecontrol.EffectiveFolderMaintenance(global, folder)
}

func maintenanceScrubMode(mode config.MaintenanceScrubMode) maintenance.FileScrubVerifyMode {
	return maintenancecontrol.ScrubMode(mode)
}

func maintenanceScrubCheckpointPath(storePath string, folderID string) string {
	return maintenancecontrol.ScrubCheckpointPath(storePath, folderID)
}

func metadataCompactionStatePath(configPath string) string {
	return metadatastore.CompactionStatePath(configPath, config.LoadFile)
}

func handleMetadata(opts cli.Options, configPath string) {
	output, err := metadatacli.Run(opts, metadatacli.Runners{
		Compact: func(opts cli.Options) ([]state.MetadataCompactionResult, error) {
			return compactMetadataConfigured(opts, configPath)
		},
		CompactionStatePath: func() string {
			return metadataCompactionStatePath(configPath)
		},
		ImportJSON: func(opts cli.Options) (metadataops.Result, error) {
			return importJSONMetadataConfigured(opts, configPath)
		},
		SplitBadger: func(opts cli.Options) (metadataops.Result, error) {
			return splitBadgerMetadataConfigured(opts, configPath)
		},
	})
	if err != nil {
		exitCLI("", err)
	}
	fmt.Print(output)
}

func handleSnapshot(opts cli.Options, configPath string) {
	output, err := snapshotcli.Run(opts, snapshotcli.Runners{
		List: func(opts cli.Options) ([]state.SnapshotMarker, error) {
			return snapshotListConfigured(opts, configPath)
		},
		RestorePlan: func(opts cli.Options) (backup.RestorePlan, error) {
			return snapshotRestorePlanConfigured(opts, configPath)
		},
		Restore: func(opts cli.Options) (backup.RestoreResult, error) {
			return snapshotRestoreConfigured(opts, configPath)
		},
		Retention: func(opts cli.Options) (backup.SnapshotRetentionPlan, error) {
			return snapshotRetentionConfigured(opts, configPath)
		},
		Marker: func(opts cli.Options) (state.SnapshotMarker, error) {
			return snapshotConfigured(opts, configPath)
		},
	})
	if err != nil {
		exitCLI("", err)
	}
	fmt.Print(output)
}

func snapshotListConfigured(opts cli.Options, configPath string) ([]state.SnapshotMarker, error) {
	return snapshotcontrol.ListConfigured(snapshotcontrol.ConfiguredOptions{
		ConfigPath: configPath,
		FolderID:   opts.ID,
		LoadConfig: config.LoadFile,
		OpenStore:  openConfiguredMetadataStore,
	})
}

func snapshotAPIResponse(req api.SnapshotRequest, cfg config.Config, store state.JSONStore, configPath string) (api.SnapshotResponse, error) {
	return snapshotapi.MarkerResponse(req, cfg, store, configPath, time.Now)
}

func snapshotRestorePlanAPIResponse(req api.RestorePlanRequest, cfg config.Config, store state.JSONStore, configPath string) (api.RestorePlanResponse, error) {
	return snapshotapi.RestorePlanResponse(req, cfg, store, configPath)
}

func snapshotRestoreAPIResponse(req api.RestoreRequest, cfg config.Config, store state.JSONStore, configPath string) (api.RestoreResponse, error) {
	return snapshotapi.RestoreResponse(req, cfg, store, configPath, time.Now)
}

func snapshotRetentionAPIResponse(req api.SnapshotRetentionRequest, cfg config.Config, store state.JSONStore, configPath string) (api.SnapshotRetentionResponse, error) {
	return snapshotapi.RetentionResponse(req, cfg, store, configPath, time.Now)
}

func backupJobsAPIResponse(store state.JSONStore, req api.BackupJobsRequest) (api.BackupJobsResponse, error) {
	return apicontrol.HandleBackupJobs(store, req)
}

func snapshotRestorePlanConfigured(opts cli.Options, configPath string) (backup.RestorePlan, error) {
	return snapshotcontrol.PlanRestoreConfigured(snapshotcontrol.RestoreConfiguredOptions{
		ConfigPath:      configPath,
		SnapshotID:      opts.ID,
		Paths:           opts.Paths,
		DestinationRoot: opts.Destination,
		AlternatePath:   opts.Path,
		LoadConfig:      config.LoadFile,
		OpenStore:       openConfiguredMetadataStore,
	})
}

func snapshotRestoreConfigured(opts cli.Options, configPath string) (backup.RestoreResult, error) {
	return snapshotcontrol.ExecuteRestoreConfigured(snapshotcontrol.RestoreConfiguredOptions{
		ConfigPath:      configPath,
		SnapshotID:      opts.ID,
		Paths:           opts.Paths,
		DestinationRoot: opts.Destination,
		AlternatePath:   opts.Path,
		LoadConfig:      config.LoadFile,
		OpenStore:       openConfiguredMetadataStore,
	})
}

func snapshotRetentionConfigured(opts cli.Options, configPath string) (backup.SnapshotRetentionPlan, error) {
	return snapshotcontrol.RetentionConfigured(snapshotcontrol.RetentionConfiguredOptions{
		ConfigPath: configPath,
		KeepLast:   opts.KeepLast,
		LoadConfig: config.LoadFile,
		OpenStore:  openConfiguredMetadataStore,
	})
}

func snapshotConfigured(opts cli.Options, configPath string) (state.SnapshotMarker, error) {
	return snapshotcontrol.MarkerConfigured(snapshotcontrol.MarkerConfiguredOptions{
		ConfigPath: configPath,
		Action:     opts.Action,
		ID:         opts.ID,
		Mode:       opts.Mode,
		LoadConfig: config.LoadFile,
		OpenStore:  openConfiguredMetadataStore,
		Now:        func() time.Time { return time.Now().UTC() },
	})
}

func snapshotCheckpointPath(cfg config.Config, marker state.SnapshotMarker, configPath string) string {
	return snapshotcontrol.CheckpointPath(cfg, marker, configPath)
}

func snapshotArchivePath(cfg config.Config, configPath string) string {
	return snapshotcontrol.ArchivePath(cfg, configPath)
}

func persistSnapshotArchiveIntakeJobs(cfg config.Config, store state.JSONStore, marker state.SnapshotMarker, createdAt time.Time, configPath string) error {
	return snapshotcontrol.PersistArchiveIntakeJobs(cfg, store, marker, createdAt, configPath)
}

func configuredFolderPath(cfg config.Config, folderID string) (string, bool) {
	return snapshotcontrol.FolderPath(cfg, folderID)
}

func configuredFolderExists(cfg config.Config, folderID string) bool {
	return snapshotcontrol.FolderExists(cfg, folderID)
}

type metadataImportResult = metadataops.Result

func importJSONMetadataConfigured(opts cli.Options, configPath string) (metadataImportResult, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return metadataImportResult{}, err
	}
	return metadataops.ImportJSON(metadataops.ImportJSONOptions{SourcePath: opts.Path, Config: cfg, ConfigPath: configPath})
}

func splitBadgerMetadataConfigured(opts cli.Options, configPath string) (metadataImportResult, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return metadataImportResult{}, err
	}
	return metadataops.SplitBadger(metadataops.SplitBadgerOptions{SourcePath: opts.Path, Config: cfg, ConfigPath: configPath})
}

func compactMetadataConfigured(opts cli.Options, configPath string) ([]state.MetadataCompactionResult, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return nil, err
	}
	store, _, err := openConfiguredMetadataStore(cfg, configPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return metadatacontrol.Compact(cfg, store, opts.ID)
}

func openConfiguredMetadataStore(cfg config.Config, configPath string) (state.JSONStore, string, error) {
	return metadatastore.Open(cfg, configPath)
}

func openConfiguredFolderMetadataStore(cfg config.Config, configPath string, folderID string) (state.JSONStore, string, error) {
	return metadatastore.OpenFolder(cfg, configPath, folderID)
}

func effectiveMetadataBackend(cfg config.Config) config.MetadataBackend {
	return metadatastore.EffectiveBackend(cfg)
}

func configuredMetadataStorePath(cfg config.Config, configPath string) string {
	return metadatastore.ConfiguredStorePath(cfg, configPath)
}

func configuredFolderMetadataStorePath(cfg config.Config, configPath string, folderID string) string {
	return metadatastore.ConfiguredFolderStorePath(cfg, configPath, folderID)
}

func sanitizeMetadataPathSegment(value string) string {
	return metadatastore.SanitizePathSegment(value)
}

func defaultStatePath(configPath string) string {
	return metadatastore.DefaultStatePath(configPath)
}

func handleConfig(opts cli.Options, configPath string) {
	out, err := clicontrol.HandleConfig(opts, configPath)
	if err != nil {
		exitCLI("", err)
	}
	fmt.Print(out)
}

func handleService(opts cli.Options, configPath string) {
	definition, err := servicecontrol.Handle(opts, configPath)
	if err != nil {
		exitCLI("", err)
	}
	fmt.Print(definition)
}

func peerCommandAPIResponse(configPath string, req api.PeerCommandRequest) (api.PeerCommandResponse, error) {
	return apicontrol.HandlePeerCommand(configPath, req)
}

func folderCommandAPIResponse(configPath string, req api.FolderCommandRequest) (api.FolderCommandResponse, error) {
	return apicontrol.HandleFolderCommand(configPath, req)
}

func discoveryCommandAPIResponse(configPath string, req api.DiscoveryCommandRequest) (api.DiscoveryCommandResponse, error) {
	return apicontrol.HandleDiscoveryCommand(configPath, req)
}

func configReadAPIResponse(configPath string) (config.Config, error) {
	return config.LoadFile(configPath)
}

func identityPackageAPIResponse(configPath string, req api.IdentityPackageRequest) (pairing.IdentityPackage, error) {
	return apicontrol.HandleIdentityPackage(configPath, req)
}

func identityImportAPIResponse(configPath string, req api.IdentityImportRequest) (api.IdentityImportResponse, error) {
	return apicontrol.HandleIdentityImport(configPath, req)
}

func meshSettingsAPIResponse(store state.JSONStore, req api.MeshSettingsRequest) (api.MeshSettingsResponse, error) {
	return apicontrol.HandleMeshSettings(store, req)
}

func meshSettingsCommandAPIResponse(store state.JSONStore, localNodeID string, req api.MeshSettingsCommandRequest) (api.MeshSettingsCommandResponse, error) {
	return apicontrol.HandleMeshSettingsCommand(store, localNodeID, req)
}

func configUpdateAPIResponse(configPath string, req api.ConfigUpdateRequest) (api.ConfigUpdateResponse, error) {
	return apicontrol.HandleConfigUpdate(configPath, req)
}

func configUpdateAPIResponseWithStore(configPath string, store state.JSONStore, req api.ConfigUpdateRequest) (api.ConfigUpdateResponse, error) {
	return apicontrol.HandleConfigUpdateWithStore(configPath, store, req)
}

func serviceCommandAPIResponse(req api.ServiceCommandRequest) (api.ServiceCommandResponse, error) {
	return apicontrol.HandleServiceCommand(req)
}

func webGUICommandAPIResponse(cfg config.Config, req api.WebGUICommandRequest) (api.WebGUICommandResponse, error) {
	return apicontrol.HandleWebGUICommand(cfg, req)
}

func webGUICommandAPIResponseWithHTTPClient(cfg config.Config, req api.WebGUICommandRequest, client *http.Client) (api.WebGUICommandResponse, error) {
	return apicontrol.HandleWebGUICommandWithHTTPClient(cfg, req, client)
}

func webGUICommandAPIResponseWithManager(cfg config.Config, req api.WebGUICommandRequest, manager *webgui.Server, client *http.Client) (api.WebGUICommandResponse, error) {
	return apicontrol.HandleWebGUICommandWithManager(cfg, req, manager, client)
}

func handlePeer(opts cli.Options, configPath string) {
	out, err := clicontrol.HandlePeer(opts, configPath)
	if err != nil {
		exitCLI("", err)
	}
	fmt.Print(out)
}

func handleFolder(opts cli.Options, configPath string) {
	out, err := clicontrol.HandleFolder(opts, configPath)
	if err != nil {
		exitCLI("", err)
	}
	fmt.Print(out)
}

func handleStream(opts cli.Options, configPath string) {
	result, err := streamcontrol.RunConfigured(streamcontrol.ConfiguredOptions{ConfigPath: configPath, CLI: opts, In: os.Stdin, Out: os.Stdout})
	if err != nil {
		exitCLI("", err)
	}
	if result.Pull != nil {
		fmt.Fprint(os.Stderr, clioutput.StreamPullSummary(*result.Pull))
	}
}
