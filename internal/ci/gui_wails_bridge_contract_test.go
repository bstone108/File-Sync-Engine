package ci

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopGUIExposesWarningsAndBackupWorkflowsThroughNativeAPI(t *testing.T) {
	root := filepath.Join("..", "..")
	appSvelte := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	daemonAPI := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "daemonApi.ts"))
	nativeProxy := readRequiredFile(t, filepath.Join(root, "desktop-gui", "app_control.go"))

	for _, want := range []string{
		"fetchDaemonLogs",
		"sendSnapshotCommand",
		"planSnapshotRestore",
		"runSnapshotRestore",
		"runSnapshotRetention",
		"runBackupScrub",
		"fetchBackupJobs",
		`"/v1/snapshots"`,
		`"/v1/restore-plans"`,
		`"/v1/restores"`,
		`"/v1/snapshot-retention"`,
		`"/v1/backup/scrub"`,
		`"/v1/backup/jobs"`,
		"Refresh warnings & logs",
		"Create snapshot",
		"Preview restore",
		"Run backup scrub",
		"reviewedRestoreRequestKey",
	} {
		if !strings.Contains(appSvelte+daemonAPI+nativeProxy, want) {
			t.Fatalf("desktop GUI warnings/backup vertical slice missing %q", want)
		}
	}
	for _, stale := range []string{
		"Structured log/event rendering is not implemented in this desktop slice",
		"Snapshot, restore, retention, and backup management UI are not implemented here",
	} {
		if strings.Contains(appSvelte, stale) {
			t.Fatalf("desktop GUI still presents implemented workflow as unavailable: %q", stale)
		}
	}
}

func TestDesktopGUIFetchesBackupJobsWithDaemonSupportedGETRoute(t *testing.T) {
	root := filepath.Join("..", "..")
	daemonAPI := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "daemonApi.ts"))
	nativeProxy := readRequiredFile(t, filepath.Join(root, "desktop-gui", "app_control.go"))

	start := strings.Index(daemonAPI, "export async function fetchBackupJobs")
	if start < 0 {
		t.Fatal("desktop daemon API helper must expose fetchBackupJobs")
	}
	function := daemonAPI[start:]
	if end := strings.Index(function, "\n}"); end >= 0 {
		function = function[:end+2]
	}
	if !strings.Contains(function, "const query = snapshotId.trim() ? `?snapshotId=${encodeURIComponent(snapshotId.trim())}` : '';") || !strings.Contains(function, "daemonAPIRequest<BackupJobsResponse>(settings, `/v1/backup/jobs${query}`)") || strings.Contains(function, "method: 'POST'") {
		t.Fatalf("backup jobs must use the daemon-supported GET route, got %s", function)
	}
	if !strings.Contains(nativeProxy, `"/v1/backup/jobs": {http.MethodGet: true}`) {
		t.Fatal("native daemon proxy must allow GET /v1/backup/jobs")
	}
}

func TestDesktopGUIExposesDaemonTransferReadModel(t *testing.T) {
	root := filepath.Join("..", "..")
	appSvelte := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	daemonAPI := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "daemonApi.ts"))
	nativeProxy := readRequiredFile(t, filepath.Join(root, "desktop-gui", "app_control.go"))
	sources := appSvelte + daemonAPI + nativeProxy
	for _, want := range []string{"fetchDaemonTransfers", `"/v1/transfers"`, "Active transfer passes", "Recent transfer history", "Live byte progress and rate telemetry are not available"} {
		if !strings.Contains(sources, want) {
			t.Fatalf("desktop GUI transfer read-model vertical slice missing %q", want)
		}
	}
	if strings.Contains(appSvelte, "Transfer queue listing and live rate history are not exposed by the current daemon API") {
		t.Fatal("desktop GUI still presents the transfer read model as wholly unavailable")
	}
}

func TestDesktopGUIInstallsWailsNativeShellBridgeBeforeSvelteStarts(t *testing.T) {
	root := filepath.Join("..", "..")
	mainTS := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "main.ts"))
	bridge := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "wailsNativeShell.ts"))
	appSvelte := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	daemonAPI := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "daemonApi.ts"))
	nativeProxy := readRequiredFile(t, filepath.Join(root, "desktop-gui", "app_control.go"))

	sources := mainTS + bridge + appSvelte + daemonAPI + nativeProxy
	for _, want := range []string{
		"installWailsNativeShellBridge();",
		`from "../../wailsjs/go/main/App"`,
		"window.fseDesktopShell",
		"RequestGUIOwnedNonServiceDaemonLaunch",
		"AdoptGUIOwnedNonServiceDaemon",
		"GetGUIOwnedNonServiceDaemonSession",
		"GetGUIOwnedNonServiceDaemonState",
		"StopGUIOwnedNonServiceDaemonThroughAPI",
		"DiscoverLocalDaemon",
		"ControlLocalDaemon",
		"DaemonAPIRequest",
		"X-FSE-API-Key",
		"onMount(() =>",
		"ensureLocalDaemonConnection",
		"preferExistingReachableDaemon: true",
		"<h2>Local engine</h2>",
		"Start local engine",
		"Restart connection",
	} {
		if !strings.Contains(sources, want) {
			t.Fatalf("desktop GUI Wails native-shell bridge missing %q", want)
		}
	}
	if strings.Index(mainTS, "installWailsNativeShellBridge();") > strings.Index(mainTS, "new App(") {
		t.Fatal("Wails native-shell bridge must be installed before Svelte starts")
	}
	if strings.Contains(appSvelte, "on:click={launchGUIOwnedNonServiceDaemon} disabled={lifecycleLoading || !bundleGate?.bundleVerified}") {
		t.Fatal("native launch must not be disabled by the obsolete all-target frontend bundle gate")
	}
}

func TestDesktopGUIWailsBridgeUsesGeneratedBindingsInsteadOfOptionalWindowGlobals(t *testing.T) {
	root := filepath.Join("..", "..")
	bridge := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "wailsNativeShell.ts"))
	isolatedBuild := readRequiredFile(t, filepath.Join(root, "scripts", "build-desktop-gui-wails.sh"))
	nativeMacBuild := readRequiredFile(t, filepath.Join(root, "scripts", "build-desktop-gui-wails-native-macos.sh"))
	builderDockerfile := readRequiredFile(t, filepath.Join(root, "development", "desktop-wails-builder", "Dockerfile"))

	for _, want := range []string{
		`from "../../wailsjs/go/main/App"`,
		"RequestGUIOwnedNonServiceDaemonLaunch",
		"DiscoverLocalDaemon",
		"DaemonAPIRequest",
	} {
		if !strings.Contains(bridge, want) {
			t.Fatalf("desktop GUI bridge must call generated Wails binding %q", want)
		}
	}
	if strings.Contains(bridge, "window.go?.main?.App") {
		t.Fatal("desktop GUI bridge must not rely on the optional window.go global namespace")
	}
	for name, script := range map[string]string{"isolated Wails build": isolatedBuild, "native macOS Wails build": nativeMacBuild} {
		generate := strings.Index(script, "wails generate module")
		frontendBuild := strings.Index(script, "npm run build")
		if generate < 0 || frontendBuild < 0 || generate > frontendBuild {
			t.Fatalf("%s must generate Wails frontend bindings before the frontend build", name)
		}
	}
	for _, want := range []string{
		"FROM node:22-bookworm AS node-runtime",
		"COPY --from=node-runtime",
		"/usr/local/bin/node",
		"/usr/local/lib/node_modules",
		"ln -sf ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm",
		"ln -sf ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx",
	} {
		if !strings.Contains(builderDockerfile, want) {
			t.Fatalf("desktop Wails builder must provide the supported Node 22 frontend runtime: missing %q", want)
		}
	}
}

func TestDesktopGUIRequiresNativeBridgeForDaemonAPIAuthentication(t *testing.T) {
	root := filepath.Join("..", "..")
	appSvelte := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	daemonAPI := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "daemonApi.ts"))

	for _, want := range []string{
		"Native desktop daemon bridge is unavailable",
		"nativeShell.daemonAPIRequest",
	} {
		if !strings.Contains(daemonAPI, want) {
			t.Fatalf("desktop daemon API client must require the native bridge for local authentication: missing %q", want)
		}
	}
	connectionSettingsEnd := strings.Index(daemonAPI, "export type DaemonStatus")
	requestStart := strings.Index(daemonAPI, "async function daemonAPIRequest")
	requestEnd := strings.Index(daemonAPI[requestStart:], "\n}\n\nfunction jsonBody")
	if connectionSettingsEnd < 0 || requestStart < 0 || requestEnd < 0 {
		t.Fatal("desktop daemon API client contract boundaries are missing")
	}
	connectionSettings := daemonAPI[:connectionSettingsEnd]
	requestImplementation := daemonAPI[requestStart : requestStart+requestEnd]
	for _, forbidden := range []string{
		"apiKey?: string",
		"settings.apiKey",
		"headers.set('X-FSE-API-Key'",
		"fetch(`${baseURL(settings)}${path}`",
		"name=\"apiKey\"",
	} {
		if strings.Contains(connectionSettings+requestImplementation+appSvelte, forbidden) {
			t.Fatalf("desktop daemon API client must not expose local API keys to browser fetch or UI: found %q", forbidden)
		}
	}
}

func TestDesktopGUIWailsBuildRejectsStaleBuilderImagesBeforeWindowsArtifactBuild(t *testing.T) {
	root := filepath.Join("..", "..")
	dockerfile := readRequiredFile(t, filepath.Join(root, "development", "desktop-wails-builder", "Dockerfile"))
	isolatedBuild := readRequiredFile(t, filepath.Join(root, "scripts", "build-desktop-gui-wails.sh"))
	imageBuild := readRequiredFile(t, filepath.Join(root, "scripts", "build-desktop-wails-builder-image.sh"))

	for _, want := range []string{
		`org.filesyncengine.desktop.wails.go-version="1.25.7"`,
		`org.filesyncengine.desktop.wails.node-major="22"`,
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("desktop Wails builder image must declare its runtime compatibility label %q", want)
		}
	}
	for _, want := range []string{
		"REQUIRED_WAILS_BUILDER_GO_VERSION=1.25.7",
		"REQUIRED_WAILS_BUILDER_NODE_MAJOR=22",
		"verify_builder_image_runtime_labels",
		"stale or incompatible Wails builder image",
	} {
		if !strings.Contains(isolatedBuild+imageBuild, want) {
			t.Fatalf("desktop Wails build scripts must reject stale builder images before artifact builds: missing %q", want)
		}
	}
}

func TestLegacyWebKitWailsBuilderUsesSupportedNodeRuntimeAndLabels(t *testing.T) {
	root := filepath.Join("..", "..")
	legacyDockerfile := readRequiredFile(t, filepath.Join(root, "development", "desktop-wails-builder", "Dockerfile.linux-webkit40"))
	for _, want := range []string{
		"FROM node:22-bookworm AS node-runtime",
		"COPY --from=node-runtime /usr/local/bin/node /usr/local/bin/node",
		"COPY --from=node-runtime /usr/local/lib/node_modules /usr/local/lib/node_modules",
		"ln -sf ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm",
		`org.filesyncengine.desktop.wails.go-version="1.25.7"`,
		`org.filesyncengine.desktop.wails.node-major="22"`,
	} {
		if !strings.Contains(legacyDockerfile, want) {
			t.Fatalf("legacy WebKit Wails builder missing supported runtime contract %q", want)
		}
	}
	for _, forbidden := range []string{"        nodejs", "        npm"} {
		if strings.Contains(legacyDockerfile, forbidden) {
			t.Fatalf("legacy WebKit Wails builder must not install Debian's unsupported Node/npm package %q", forbidden)
		}
	}
}

func TestDesktopGUISettingsExposeOnlyImplementedNativeLifecycleControls(t *testing.T) {
	root := filepath.Join("..", "..")
	appSvelte := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	bridge := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "wailsNativeShell.ts"))

	for _, want := range []string{
		"<h2>Local engine</h2>",
		"Start local engine",
		"Restart connection",
		"Launch separate non-service daemon",
		"Adopt recorded non-service daemon",
		"Stop through daemon API",
		"requestGUIOwnedNonServiceDaemonLaunch",
		"ControlLocalDaemon",
	} {
		if !strings.Contains(appSvelte+bridge, want) {
			t.Fatalf("desktop GUI must retain implemented native lifecycle control %q", want)
		}
	}

	for _, stale := range []string{
		"Check first-launch setup",
		"Install/register bundled daemon",
		"Refresh tray/startup status",
		"Simulate tray open/focus GUI",
		"Verify bundled daemon",
		"readBundledEngineResourceManifest: async () => unavailable",
		"getLocalLifecycleSettings: async () => unavailable",
		"getFirstLaunchDaemonRegistrationStatus: async () => unavailable",
		"getDaemonTrayStatus: async () => unavailable",
		"getDaemonStartupIntegrationStatus: async () => unavailable",
		"openGuiFromDaemonTray: async () => unavailable",
		"showMainWindowFromDaemonTray: async () => unavailable",
	} {
		if strings.Contains(appSvelte+bridge, stale) {
			t.Fatalf("desktop GUI exposes placeholder lifecycle/setup behavior %q", stale)
		}
	}
}
