package daemonwebgui

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/webgui"
)

type recordingPublisher struct {
	events []api.Event
}

func (p *recordingPublisher) Publish(event api.Event) {
	p.events = append(p.events, event)
}

func TestStartPublishesOptionalGUIFailureAndLeavesDaemonAvailable(t *testing.T) {
	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "fse-web.zip")
	writePackage(t, pkgPath)
	publisher := &recordingPublisher{}
	cfg := config.Config{WebGUI: config.WebGUIConfig{
		Enabled:        true,
		Version:        "1.2.3",
		PackagePath:    pkgPath,
		InstallDir:     filepath.Join(dir, "web", "current"),
		Listen:         "127.0.0.1:0",
		ChecksumSHA256: strings.Repeat("0", 64),
	}}

	result := Start(cfg, webgui.NewServer(), publisher)

	if result.Response.Status != "failed" || result.Response.Running || result.Err == nil {
		t.Fatalf("optional GUI failure should be retained without blocking daemon: %+v", result)
	}
	if len(publisher.events) != 1 || publisher.events[0].Type != "webgui.startup.failed" || publisher.events[0].Message != "optional web GUI unavailable; daemon continues headless" {
		t.Fatalf("expected actionable optional-GUI failure event, got %+v", publisher.events)
	}
}

func writePackage(t *testing.T, path string) {
	t.Helper()
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(out)
	entry, err := writer.Create("index.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("web")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
