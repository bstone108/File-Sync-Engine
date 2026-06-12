package daemonapihandlers

import (
	"context"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/transfercontrol"
	"filesyncengine/internal/webgui"
)

func TestBuildStaticHandlersUsesLiveRuntimeDependencies(t *testing.T) {
	transferGate := transfercontrol.New()
	currentConfig := config.Config{WebGUI: config.WebGUIConfig{Enabled: false}}

	handlers := BuildStaticHandlers(StaticRuntimeDeps{
		ConfigPath:      t.TempDir() + "/config.jsonc",
		CurrentConfig:   func() config.Config { return currentConfig },
		TransferControl: transferGate,
		WebGUIServer:    webgui.NewServer(),
	})

	transferResp, err := handlers.TransferCommand(context.Background(), api.TransferCommandRequest{Action: "pause", FolderID: "docs"})
	if err != nil {
		t.Fatalf("transfer command: %v", err)
	}
	if transferResp.Status != "accepted" || !transferGate.IsPaused("docs", "peer-a") {
		t.Fatalf("transfer handler did not use live transfer control: response=%+v paused=%v", transferResp, transferGate.IsPaused("docs", "peer-a"))
	}

	webResp, err := handlers.WebGUICommand(context.Background(), api.WebGUICommandRequest{Action: "status"})
	if err != nil {
		t.Fatalf("web gui command: %v", err)
	}
	if webResp.Status != "disabled" || webResp.Running {
		t.Fatalf("web GUI handler did not use current config/manager dependency: %+v", webResp)
	}
}
