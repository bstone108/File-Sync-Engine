package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicDocumentationFolderIsPresent(t *testing.T) {
	root := filepath.Join("..", "..")
	docsDir := filepath.Join(root, "docs")
	info, err := os.Stat(docsDir)
	if err != nil {
		t.Fatalf("docs directory is required in the source repository: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("docs path is not a directory: %s", docsDir)
	}

	requiredDocs := map[string][]string{
		"API.md":    {"# API Reference", "GET /v1/status"},
		"CLI.md":    {"# CLI Reference", "fse start"},
		"CONFIG.md": {"# Configuration Reference", "nodeName"},
		"DOCKER.md": {"# Docker/container defaults", "FSE_CONFIG_PATH"},
	}
	for name, requiredText := range requiredDocs {
		path := filepath.Join(docsDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("required documentation file missing: %s: %v", path, err)
		}
		content := string(data)
		for _, text := range requiredText {
			if !strings.Contains(content, text) {
				t.Fatalf("%s missing required content %q", path, text)
			}
		}
	}
}
