package ci

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerDistributionDefaultsRootPersistentStateAtConfig(t *testing.T) {
	root := filepath.Join("..", "..")
	dockerfile := readRequiredFile(t, filepath.Join(root, "Dockerfile"))
	entrypoint := readRequiredFile(t, filepath.Join(root, "scripts", "container-entrypoint.sh"))

	for _, want := range []string{
		"VOLUME [\"/config\"]",
		"EXPOSE 22420 22000",
		"/config/config.jsonc",
		"FSE_CONFIG_PATH=/config/config.jsonc",
		"FSE_API_LISTEN=0.0.0.0:22420",
		"FSE_SYNC_LISTEN=tcp://0.0.0.0:22000",
		"ENTRYPOINT [\"/usr/local/bin/fse-container-entrypoint\"]",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Dockerfile missing %q:\n%s", want, dockerfile)
		}
	}
	for _, forbidden := range []string{"FSE_API_KEY", "identity.privateKey", "--secret"} {
		if strings.Contains(dockerfile, forbidden) {
			t.Fatalf("Dockerfile should not bake or request secret material %q:\n%s", forbidden, dockerfile)
		}
	}

	for _, want := range []string{
		"CONFIG_PATH=\"${FSE_CONFIG_PATH:-/config/config.jsonc}\"",
		"mkdir -p /config/logs /config/metadata /config/web",
		"PUID",
		"PGID",
		"FSE_API_LISTEN",
		"FSE_SYNC_LISTEN",
		"FSE_LOG_LEVEL",
		"fse config init \"$CONFIG_PATH\"",
		"FSE_CONTAINER_FIRST_RUN=true fse container-bootstrap \"$CONFIG_PATH\"",
		"fse start \"$CONFIG_PATH\"",
	} {
		if !strings.Contains(entrypoint, want) {
			t.Fatalf("container entrypoint missing %q:\n%s", want, entrypoint)
		}
	}
	for _, forbidden := range []string{"set -x", "FSE_API_KEY", "FSE_IDENTITY_PRIVATE_KEY"} {
		if strings.Contains(entrypoint, forbidden) {
			t.Fatalf("container entrypoint should not log or accept raw secret env %q:\n%s", forbidden, entrypoint)
		}
	}
	if strings.Contains(dockerfile, "python3") || strings.Contains(entrypoint, "python3") {
		t.Fatalf("container runtime bootstrap should not require Python after Go bootstrap helper:\nDockerfile:\n%s\nentrypoint:\n%s", dockerfile, entrypoint)
	}
}

func TestDockerContainerBundlesWebGUIEnabledByDefault(t *testing.T) {
	root := filepath.Join("..", "..")
	dockerfile := readRequiredFile(t, filepath.Join(root, "Dockerfile"))
	entrypoint := readRequiredFile(t, filepath.Join(root, "scripts", "container-entrypoint.sh"))
	docs := readRequiredFile(t, filepath.Join(root, "docs", "DOCKER.md"))
	bundlePath := filepath.Join(root, "web-gui", "dist", "fse-web-container-default.zip")
	if _, err := os.Stat(bundlePath); err != nil {
		t.Fatalf("container web GUI bundle missing at %s: %v", bundlePath, err)
	}
	for _, want := range []string{
		"COPY web-gui/dist/fse-web-container-default.zip /opt/fse/web/fse-web-container-default.zip",
		"FSE_WEB_GUI_ENABLED=true",
		"FSE_WEB_GUI_PACKAGE=/opt/fse/web/fse-web-container-default.zip",
		"FSE_WEB_GUI_INSTALL_DIR=/config/web/current",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Dockerfile missing bundled web GUI default %q:\n%s", want, dockerfile)
		}
	}
	for _, want := range []string{
		"FSE_CONTAINER_FIRST_RUN=true fse container-bootstrap \"$CONFIG_PATH\"",
		"fse container-bootstrap \"$CONFIG_PATH\"",
	} {
		if !strings.Contains(entrypoint, want) {
			t.Fatalf("container entrypoint missing bundled web GUI default %q:\n%s", want, entrypoint)
		}
	}
	for _, want := range []string{
		"The container image bundles the default optional web GUI package and enables it on first-run config creation.",
		"FSE_WEB_GUI_ENABLED",
		"FSE_WEB_GUI_PACKAGE",
		"FSE_WEB_GUI_CHECKSUM",
	} {
		if !strings.Contains(docs, want) {
			t.Fatalf("Docker docs missing bundled web GUI default %q:\n%s", want, docs)
		}
	}
}

func TestContainerEntrypointExportsIdentityPackageWithoutRegenerating(t *testing.T) {
	root := filepath.Join("..", "..")
	entrypoint := readRequiredFile(t, filepath.Join(root, "scripts", "container-entrypoint.sh"))
	docs := readRequiredFile(t, filepath.Join(root, "docs", "DOCKER.md"))

	for _, want := range []string{
		"FSE_IDENTITY_EXPORT_PATH",
		"FSE_IDENTITY_EXPORT_FORCE",
		"fse container-bootstrap \"$CONFIG_PATH\"",
	} {
		if !strings.Contains(entrypoint, want) {
			t.Fatalf("container entrypoint missing identity-export contract %q:\n%s", want, entrypoint)
		}
	}
	for _, forbidden := range []string{"print(package", "print(cfg", "echo $FSE_IDENTITY_EXPORT_PATH"} {
		if strings.Contains(entrypoint, forbidden) {
			t.Fatalf("container entrypoint must not log identity package material through %q:\n%s", forbidden, entrypoint)
		}
	}
	for _, want := range []string{
		"FSE_IDENTITY_EXPORT_PATH",
		"FSE_IDENTITY_EXPORT_FORCE",
		"The entrypoint never prints the exported package",
		"will not overwrite an existing export unless",
	} {
		if !strings.Contains(docs, want) {
			t.Fatalf("Docker docs missing identity-export contract %q:\n%s", want, docs)
		}
	}
}

func TestContainerDefaultWebGUIIsDevelopmentStatusPage(t *testing.T) {
	root := filepath.Join("..", "..")
	dockerfile := readRequiredFile(t, filepath.Join(root, "Dockerfile"))
	containerBootstrap := readRequiredFile(t, filepath.Join(root, "internal", "containerbootstrap", "bootstrap.go"))
	bundlePath := filepath.Join(root, "web-gui", "dist", "fse-web-container-default.zip")
	index := readRequiredZipFile(t, bundlePath, "index.html")

	for _, want := range []string{
		"Development in progress",
		"Engine status",
		"/health",
		"container-default-dev-status",
	} {
		if !strings.Contains(index, want) {
			t.Fatalf("container default web GUI missing development status marker %q:\n%s", want, index)
		}
	}
	checksum := sha256HexFile(t, bundlePath)
	for _, haystack := range []struct {
		name string
		text string
	}{
		{"Dockerfile", dockerfile},
		{"containerbootstrap", containerBootstrap},
	} {
		if !strings.Contains(haystack.text, checksum) {
			t.Fatalf("%s missing current web GUI bundle checksum %s", haystack.name, checksum)
		}
	}
}

func TestDockerContainerExposesWebGUIAndRuntimePermissionControls(t *testing.T) {
	root := filepath.Join("..", "..")
	dockerfile := readRequiredFile(t, filepath.Join(root, "Dockerfile"))
	entrypoint := readRequiredFile(t, filepath.Join(root, "scripts", "container-entrypoint.sh"))
	docs := readRequiredFile(t, filepath.Join(root, "docs", "DOCKER.md"))

	for _, want := range []string{
		"EXPOSE 22420 22000 8385",
		"8943",
		"FSE_WEB_GUI_LISTEN=0.0.0.0:8385",
		"FSE_WEB_GUI_HTTPS_LISTEN=0.0.0.0:8943",
		"FSE_WEB_GUI_TLS_ENABLED=true",
		"FSE_UMASK=002",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Dockerfile missing Docker web GUI/permission default %q:\n%s", want, dockerfile)
		}
	}
	for _, want := range []string{
		"FSE_CONTAINER_FIRST_RUN=true fse container-bootstrap \"$CONFIG_PATH\"",
		"umask \"${FSE_UMASK:-002}\"",
		"uid=\"${PUID:-${UID:-99}}\"",
		"gid=\"${PGID:-${GID:-100}}\"",
	} {
		if !strings.Contains(entrypoint, want) {
			t.Fatalf("entrypoint missing runtime permission control %q:\n%s", want, entrypoint)
		}
	}
	for _, want := range []string{
		"FSE_WEB_GUI_TLS_ENABLED",
		"FSE_WEB_GUI_HTTPS_LISTEN",
		"8943:8943",
		"FSE_UMASK",
		"Unraid",
		"nobody",
		"99:100",
		"8385:8385",
	} {
		if !strings.Contains(docs, want) {
			t.Fatalf("Docker docs missing web GUI/runtime permission guidance %q:\n%s", want, docs)
		}
	}
}

func readRequiredFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func readRequiredZipFile(t *testing.T, zipPath, memberName string) string {
	t.Helper()
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("open zip %s: %v", zipPath, err)
	}
	defer archive.Close()
	for _, file := range archive.File {
		if file.Name != memberName {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatalf("open %s in %s: %v", memberName, zipPath, err)
		}
		defer reader.Close()
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read %s in %s: %v", memberName, zipPath, err)
		}
		return string(data)
	}
	t.Fatalf("zip %s missing required member %s", zipPath, memberName)
	return ""
}

func sha256HexFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
