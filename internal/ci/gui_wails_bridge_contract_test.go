package ci

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopGUIInstallsWailsNativeShellBridgeBeforeSvelteStarts(t *testing.T) {
	root := filepath.Join("..", "..")
	mainTS := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "main.ts"))
	bridge := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "wailsNativeShell.ts"))
	appSvelte := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "App.svelte"))
	daemonAPI := readRequiredFile(t, filepath.Join(root, "desktop-gui", "src", "lib", "daemonApi.ts"))

	sources := mainTS + bridge + appSvelte + daemonAPI
	for _, want := range []string{
		"installWailsNativeShellBridge();",
		"window.go?.main?.App",
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
