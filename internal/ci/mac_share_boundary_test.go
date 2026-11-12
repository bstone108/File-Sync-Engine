package ci

import (
	"os"
	"strings"
	"testing"
)

// TestDesktopClientsUseDaemonManagementBoundary checks that the Mac and
// Windows desktop builds call one daemon. Management, authorization, and
// folder sync live in that daemon. The desktop shell only proxies the same
// routes on both platforms and launches the platform engine binary.
func TestDesktopClientsUseDaemonManagementBoundary(t *testing.T) {
	checkDesktopClientsUseDaemonManagementBoundary(t)
}

func TestMacDesktopUsesDaemonManagementBoundary(t *testing.T) {
	checkDesktopClientsUseDaemonManagementBoundary(t)
}

func checkDesktopClientsUseDaemonManagementBoundary(t *testing.T) {
	gui := readRepoFile(t, "../../desktop-gui/src/lib/daemonApi.ts")
	if !strings.Contains(gui, "/v1/config-sync/edit") || !strings.Contains(gui, "/v1/folder-command") {
		t.Fatal("desktop GUI does not send management and folder commands to the daemon")
	}
	if strings.Contains(gui, "MayManage") || strings.Contains(gui, "RoleShare") {
		t.Fatal("desktop GUI implemented a second management policy")
	}
	appView := readRepoFile(t, "../../desktop-gui/src/App.svelte")
	if !strings.Contains(appView, "from './lib/daemonApi'") {
		t.Fatal("desktop GUI is not using the shared daemon client")
	}
	if _, err := os.Stat("../../desktop-gui/src/lib/windowsDaemonApi.ts"); !os.IsNotExist(err) {
		t.Fatal("Windows desktop has a separate daemon client")
	}

	bridge := readRepoFile(t, "../../desktop-gui/app_control.go")
	if strings.Count(bridge, "var nativeProxyPaths") != 1 {
		t.Fatal("desktop bridge does not have one shared proxy map")
	}
	for _, path := range []string{
		`"/v1/config-sync"`,
		`"/v1/config-sync/edit"`,
		`"/v1/config-sync/merge"`,
		`"/v1/folders"`,
		`"/v1/folder-command"`,
		`"/v1/pair-code"`,
		`"/v1/identity-import"`,
	} {
		if !strings.Contains(bridge, path) {
			t.Fatalf("shared desktop bridge is missing %s", path)
		}
	}
	proxy := bridge[strings.Index(bridge, "var nativeProxyPaths"):]
	if end := strings.Index(proxy, "\nfunc "); end > 0 {
		proxy = proxy[:end]
	}
	if strings.Contains(proxy, "runtime.GOOS") {
		t.Fatal("management proxy paths differ by desktop platform")
	}

	launcher := readRepoFile(t, "../../desktop-gui/app.go")
	if !strings.Contains(launcher, `return "fse.exe"`) || !strings.Contains(launcher, `return "fse"`) {
		t.Fatal("desktop launcher does not select the bundled engine for both platforms")
	}

	daemon := readRepoFile(t, "../api/server.go")
	if !strings.Contains(daemon, "requireSync") || !strings.Contains(daemon, "allowSyncFolder") {
		t.Fatal("daemon sync API does not apply the share-only folder boundary")
	}
	sync := readRepoFile(t, "../foldershare/sync.go")
	if !strings.Contains(sync, "MaySyncFolder") {
		t.Fatal("folder sync does not use the share-only authorization check")
	}
}

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
