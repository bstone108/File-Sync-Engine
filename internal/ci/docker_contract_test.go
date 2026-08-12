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
		"mkdir -p /config/logs /config/metadata",
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

func TestDockerBuildContextExcludesRepositoryAndNonDaemonPayloads(t *testing.T) {
	root := filepath.Join("..", "..")
	ignore := readRequiredFile(t, filepath.Join(root, ".dockerignore"))

	patterns := make(map[string]bool)
	for _, line := range strings.Split(ignore, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns[line] = true
		}
	}
	for _, want := range []string{
		".git/",
		".github/",
		"desktop-gui/",
		"mobile-gui/",
		"web-gui/",
		"development/",
		"docs/",
		"build/",
		"builds/",
	} {
		if !patterns[want] {
			t.Fatalf("Docker build context must exclude %q:\n%s", want, ignore)
		}
	}
	for _, requiredSource := range []string{"cmd/", "internal/", "scripts/", "go.mod", "go.sum"} {
		if patterns[requiredSource] {
			t.Fatalf("Docker build context must retain daemon build input %q:\n%s", requiredSource, ignore)
		}
	}
}

func TestDockerImageUsesSameRunPrebuiltReleaseDaemon(t *testing.T) {
	root := filepath.Join("..", "..")
	dockerfile := readRequiredFile(t, filepath.Join(root, "Dockerfile"))
	workflow := readRequiredFile(t, filepath.Join(root, ".github", "workflows", "release.yml"))

	for _, want := range []string{
		"ARG TARGETARCH",
		"ARG TARGETVARIANT",
		"COPY --chmod=0755 docker-artifacts/fse-linux-${TARGETARCH}${TARGETVARIANT} /usr/local/bin/fse",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Docker image must select a matching same-run release daemon, missing %q", want)
		}
	}
	if strings.Contains(dockerfile, "go build") || strings.Contains(dockerfile, "FROM golang:") {
		t.Fatal("Docker image must not rebuild the daemon after the release artifact job has already built it")
	}
	for _, want := range []string{
		"Download same-run Linux daemon artifacts for Docker image",
		"daemon-artifacts",
		"sha256sum",
		"fse-linux-armv7",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release container job must download and verify same-run daemon artifacts, missing %q", want)
		}
	}
}

func TestDockerContainerDefaultsHeadlessWithoutBundledWebGUI(t *testing.T) {
	root := filepath.Join("..", "..")
	dockerfile := readRequiredFile(t, filepath.Join(root, "Dockerfile"))
	entrypoint := readRequiredFile(t, filepath.Join(root, "scripts", "container-entrypoint.sh"))
	docs := readRequiredFile(t, filepath.Join(root, "docs", "DOCKER.md"))

	for _, want := range []struct {
		text string
		from string
	}{
		{"FSE_WEB_GUI_ENABLED=false", dockerfile},
		{"EXPOSE 22420 22000", dockerfile},
		{"FSE_CONTAINER_FIRST_RUN=true fse container-bootstrap \"$CONFIG_PATH\"", entrypoint},
		{"Headless by default", docs},
		{"FSE_WEB_GUI_ENABLED=true", docs},
	} {
		if !strings.Contains(want.from, want.text) {
			t.Fatalf("headless Docker contract missing %q", want.text)
		}
	}
	for _, forbidden := range []string{
		"COPY web-gui/",
		"/opt/fse/web",
		"FSE_WEB_GUI_PACKAGE=",
		"FSE_WEB_GUI_LISTEN=",
		"FSE_WEB_GUI_HTTPS_LISTEN=",
		"FSE_WEB_GUI_CHECKSUM=",
		"8385",
		"8943",
	} {
		if strings.Contains(dockerfile, forbidden) {
			t.Fatalf("headless core Dockerfile must not bundle or expose web GUI material %q", forbidden)
		}
	}
}

func TestDockerOptionalGUIOptInRequiresExplicitTrustedDeliveryMetadata(t *testing.T) {
	root := filepath.Join("..", "..")
	entrypoint := readRequiredFile(t, filepath.Join(root, "scripts", "container-entrypoint.sh"))
	bootstrap := readRequiredFile(t, filepath.Join(root, "internal", "containerbootstrap", "bootstrap.go"))
	docs := readRequiredFile(t, filepath.Join(root, "docs", "DOCKER.md"))

	for _, want := range []string{
		"FSE_WEB_GUI_VERSION",
		"FSE_WEB_GUI_PACKAGE",
		"FSE_WEB_GUI_INSTALL_DIR",
		"FSE_WEB_GUI_LISTEN",
		"FSE_WEB_GUI_CHECKSUM",
	} {
		if !strings.Contains(entrypoint, want) || !strings.Contains(bootstrap, want) || !strings.Contains(docs, want) {
			t.Fatalf("optional GUI opt-in must document and carry explicit trusted delivery field %q", want)
		}
	}
	for _, want := range []string{"webGUIOptInComplete", "d.WebGUIVersion != \"\"", "d.WebGUIPackage != \"\"", "d.WebGUIInstallDir != \"\"", "d.WebGUIListen != \"\"", "d.WebGUIChecksum != \"\""} {
		if !strings.Contains(bootstrap, want) {
			t.Fatalf("container bootstrap must fail closed for incomplete GUI opt-in, missing %q", want)
		}
	}
}

func TestDaemonOwnsEnabledWebGUIStartupWithoutBlockingHeadlessServer(t *testing.T) {
	mainSource := readRequiredFile(t, filepath.Join("..", "..", "cmd", "fse", "main.go"))
	for _, want := range []string{
		"filesyncengine/internal/daemonwebgui",
		"daemonwebgui.Start(cfg, webGUIServer, apiServer)",
		"webgui.startup.failed",
		"optional web GUI startup failed",
	} {
		if !strings.Contains(mainSource, want) {
			t.Fatalf("daemon startup must own optional GUI failure isolation and reporting, missing %q", want)
		}
	}
}

func TestRootDockerExamplesUsePublishedImmutableReleaseTags(t *testing.T) {
	readme := readRequiredFile(t, filepath.Join("..", "..", "README.md"))

	if strings.Contains(readme, "ghcr.io/bstone108/file-sync-engine:latest") {
		t.Fatalf("Docker examples must not use the unpublished mutable latest tag")
	}
	for _, want := range []string{
		"ghcr.io/bstone108/file-sync-engine:<version>",
		"Replace `<version>` with a published `YYYY.MM.DD.NN` release tag",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("Docker examples must guide users to a published immutable release tag, missing %q", want)
		}
	}
}

func TestDockerComposeHeadlessDaemonUsesPersistentConfigAndExplicitReleaseTag(t *testing.T) {
	root := filepath.Join("..", "..")
	compose := readRequiredFile(t, filepath.Join(root, "compose.yaml"))

	for _, want := range []string{
		"services:",
		"fse:",
		"ghcr.io/bstone108/file-sync-engine:${FSE_IMAGE_TAG:?Set FSE_IMAGE_TAG to a published YYYY.MM.DD.NN release tag}",
		"FSE_WEB_GUI_ENABLED: \"false\"",
		"fse-config:/config",
		"${FSE_API_HOST_PORT:-22420}:22420/tcp",
		"${FSE_SYNC_HOST_PORT:-22000}:22000/tcp",
		"volumes:",
		"fse-config:",
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("Compose deployment missing %q:\n%s", want, compose)
		}
	}
	for _, forbidden := range []string{
		"8385:8385",
		"8943:8943",
		"FSE_WEB_GUI_ENABLED: \"true\"",
		"web-gui/",
	} {
		if strings.Contains(compose, forbidden) {
			t.Fatalf("headless Compose baseline must not enable or publish optional GUI material %q:\n%s", forbidden, compose)
		}
	}
}

func TestDockerComposeOptionalGUIOverrideMountsTrustedPackageOutsideHeadlessBaseline(t *testing.T) {
	root := filepath.Join("..", "..")
	override := readRequiredFile(t, filepath.Join(root, "compose.web-gui.yaml"))
	docs := readRequiredFile(t, filepath.Join(root, "docs", "DOCKER.md"))

	for _, want := range []string{
		"services:",
		"fse:",
		"FSE_WEB_GUI_ENABLED: \"true\"",
		"FSE_WEB_GUI_VERSION: \"${FSE_WEB_GUI_VERSION:?Set FSE_WEB_GUI_VERSION to the trusted package version}\"",
		"FSE_WEB_GUI_PACKAGE: \"/opt/fse/web-package/fse-web-gui.zip\"",
		"FSE_WEB_GUI_INSTALL_DIR: \"/config/web-gui\"",
		"FSE_WEB_GUI_LISTEN: \"0.0.0.0:8385\"",
		"FSE_WEB_GUI_CHECKSUM: \"${FSE_WEB_GUI_CHECKSUM:?Set FSE_WEB_GUI_CHECKSUM to the trusted package SHA-256}\"",
		"\"${FSE_WEB_GUI_PACKAGE_HOST_PATH:?Set FSE_WEB_GUI_PACKAGE_HOST_PATH to a trusted GUI package}:/opt/fse/web-package/fse-web-gui.zip:ro\"",
		"\"${FSE_WEB_GUI_HOST_PORT:-8385}:8385/tcp\"",
	} {
		if !strings.Contains(override, want) {
			t.Fatalf("optional GUI Compose override missing %q:\n%s", want, override)
		}
	}
	for _, forbidden := range []string{"image:", "build:", "web-gui/dist/"} {
		if strings.Contains(override, forbidden) {
			t.Fatalf("optional GUI override must mount a caller-supplied package instead of bundling %q:\n%s", forbidden, override)
		}
	}
	for _, want := range []string{"compose.web-gui.yaml", "FSE_WEB_GUI_PACKAGE_HOST_PATH", "FSE_WEB_GUI_CHECKSUM"} {
		if !strings.Contains(docs, want) {
			t.Fatalf("Docker docs must explain the explicit optional GUI override and trust inputs, missing %q:\n%s", want, docs)
		}
	}
}

func TestDockerComposeAllowsSeparateHostPortsForDisposableCoexistence(t *testing.T) {
	root := filepath.Join("..", "..")
	compose := readRequiredFile(t, filepath.Join(root, "compose.yaml"))
	docs := readRequiredFile(t, filepath.Join(root, "docs", "DOCKER.md"))

	for _, want := range []string{
		"${FSE_API_HOST_PORT:-22420}:22420/tcp",
		"${FSE_SYNC_HOST_PORT:-22000}:22000/tcp",
	} {
		if !strings.Contains(compose, want) {
			t.Fatalf("Compose deployment must permit a separately mapped host port, missing %q:\n%s", want, compose)
		}
	}
	if !strings.Contains(docs, "FSE_SYNC_HOST_PORT=32000") {
		t.Fatalf("Docker docs must show a separate disposable sync host port for coexistence testing:\n%s", docs)
	}
}

func TestDockerDocsProvideExplicitHostNetworkFallbackForNoNATHosts(t *testing.T) {
	root := filepath.Join("..", "..")
	docs := readRequiredFile(t, filepath.Join(root, "docs", "DOCKER.md"))

	for _, want := range []string{
		"## Host-network fallback for Docker hosts without bridge/NAT publishing",
		"--network host",
		"does not use Docker port publishing",
		"FSE_API_LISTEN=0.0.0.0:22420",
		"FSE_SYNC_LISTEN=tcp://0.0.0.0:22000",
		"FSE_WEB_GUI_LISTEN=0.0.0.0:8385",
		"Do not combine host networking with `-p`",
		"external device",
	} {
		if !strings.Contains(docs, want) {
			t.Fatalf("Docker docs missing host-network fallback safety/deployment contract %q", want)
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

func TestDockerDistributionKeepsWebGUIPackageOutsideCoreImage(t *testing.T) {
	root := filepath.Join("..", "..")
	dockerfile := readRequiredFile(t, filepath.Join(root, "Dockerfile"))
	containerBootstrap := readRequiredFile(t, filepath.Join(root, "internal", "containerbootstrap", "bootstrap.go"))

	for _, text := range []string{dockerfile, containerBootstrap} {
		for _, forbidden := range []string{"fse-web-container-default.zip", "/opt/fse/web", "9f65e8d0ad7bff683a81a9ca081fd8aae53ed43df896b65f1b9c6fd56e0610ab"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("core container distribution must not carry a web GUI package reference %q", forbidden)
			}
		}
	}
}

func TestOptionalWebGUIPackageUsesSameOriginStatusWithoutNativeCredential(t *testing.T) {
	root := filepath.Join("..", "..")
	source := readRequiredFile(t, filepath.Join(root, "web-gui", "src", "index.html"))
	bundle := readRequiredZipFile(t, filepath.Join(root, "web-gui", "dist", "fse-web-container-default.zip"), "index.html")

	for _, page := range []string{source, bundle} {
		for _, want := range []string{"/api/v1/status", "/api/v1/folders", "/api/v1/peers", "/api/v1/transfers", "/api/v1/actionable-errors", "Engine status", "Folder overview", "Peer overview", "Transfer activity", "Actionable errors", "native API credential stays inside the daemon", "does not show raw daemon logs, file paths, or credentials"} {
			if !strings.Contains(page, want) {
				t.Fatalf("functional optional web GUI package missing %q", want)
			}
		}
		for _, forbidden := range []string{"X-FSE-API-Key", "Development in progress", "placeholder page"} {
			if strings.Contains(page, forbidden) {
				t.Fatalf("functional optional web GUI package must not expose or present %q", forbidden)
			}
		}
	}
}

func TestDockerContainerHeadlessDefaultsKeepRuntimePermissionControls(t *testing.T) {
	root := filepath.Join("..", "..")
	dockerfile := readRequiredFile(t, filepath.Join(root, "Dockerfile"))
	entrypoint := readRequiredFile(t, filepath.Join(root, "scripts", "container-entrypoint.sh"))
	docs := readRequiredFile(t, filepath.Join(root, "docs", "DOCKER.md"))

	if !strings.Contains(dockerfile, "FSE_UMASK=002") {
		t.Fatalf("Dockerfile missing runtime permission default")
	}
	for _, want := range []string{
		"umask \"${FSE_UMASK:-002}\"",
		"uid=\"${PUID:-${UID:-99}}\"",
		"gid=\"${PGID:-${GID:-100}}\"",
	} {
		if !strings.Contains(entrypoint, want) {
			t.Fatalf("entrypoint missing runtime permission control %q", want)
		}
	}
	for _, want := range []string{"FSE_UMASK", "Unraid", "nobody", "99:100"} {
		if !strings.Contains(docs, want) {
			t.Fatalf("Docker docs missing runtime permission guidance %q", want)
		}
	}
}

func readRequiredFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && strings.HasSuffix(path, ".md") {
			t.Skipf("local-only Markdown contract file is intentionally outside the GitHub source repository: %s", path)
		}
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
