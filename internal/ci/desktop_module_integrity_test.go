package ci

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopModuleRecordsContentHashesForRequiredTextDependency(t *testing.T) {
	goMod := readRequiredFile(t, filepath.Join("..", "..", "desktop-gui", "go.mod"))
	goSum := readRequiredFile(t, filepath.Join("..", "..", "desktop-gui", "go.sum"))

	if !strings.Contains(goMod, "golang.org/x/text v0.38.0") {
		t.Fatal("desktop module must pin golang.org/x/text v0.38.0")
	}
	if !strings.Contains(goSum, "golang.org/x/text v0.38.0 h1:") {
		t.Fatal("desktop module must record the golang.org/x/text v0.38.0 content hash required for read-only builds")
	}
}
