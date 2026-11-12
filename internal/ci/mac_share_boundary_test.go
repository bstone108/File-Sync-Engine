package ci

import (
	"os"
	"strings"
	"testing"
)

// The Mac desktop app is the Wails shell in desktop-gui. It does not decide
// who may manage an engine. It calls the daemon, and the daemon enforces the
// identity versus share-only boundary.
func TestMacDesktopUsesDaemonManagementBoundary(t *testing.T) {
	gui, err := os.ReadFile("../../desktop-gui/src/lib/daemonApi.ts")
	if err != nil {
		t.Fatal(err)
	}
	text := string(gui)
	if !strings.Contains(text, "/v1/config-sync/edit") {
		t.Fatal("Mac GUI does not send management edits to the daemon")
	}
	if strings.Contains(text, "MayManage") || strings.Contains(text, "RoleShare") {
		t.Fatal("Mac GUI implemented a second management policy")
	}
	server, err := os.ReadFile("../api/server.go")
	if err != nil {
		t.Fatal(err)
	}
	daemon := string(server)
	if !strings.Contains(daemon, "requireSync") || !strings.Contains(daemon, "allowSyncFolder") {
		t.Fatal("daemon sync API does not apply the share-only folder boundary")
	}
}
