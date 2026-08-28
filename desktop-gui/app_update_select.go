package main

import (
	"fmt"
	"runtime"
	"strings"
)

const (
	defaultLinuxWebKitLane = "webkit41"
	windowsInstallerKind   = "windows-installer"
	linuxAppImageKind      = "linux-appimage"
	macSparkleZipKind      = "macos-sparkle-zip"
	macSparkleDMGKind      = "macos-sparkle-dmg"
)

type selectedUpdateAsset struct {
	Kind     string
	Name     string
	URL      string
	SHA256   string
	Size     int64
	Platform string
	Arch     string
}

func selectWindowsInstallerAsset(assets []githubAsset, arch, version string) (selectedUpdateAsset, error) {
	arch = normalizeDesktopArch(arch)
	names := windowsInstallerNames(arch, version)
	asset, err := firstNamedAsset(assets, names)
	if err != nil {
		return selectedUpdateAsset{}, fmt.Errorf("no Windows %s installer in the latest GitHub Release: %w", arch, err)
	}
	return selectedUpdateAsset{
		Kind:     windowsInstallerKind,
		Name:     asset.Name,
		URL:      asset.BrowserDownloadURL,
		SHA256:   asset.SHA256(),
		Size:     asset.Size,
		Platform: "windows",
		Arch:     arch,
	}, nil
}

func selectLinuxAppImageAsset(assets []githubAsset, arch, webkitLane, version string) (selectedUpdateAsset, error) {
	arch = normalizeDesktopArch(arch)
	lane := strings.TrimSpace(webkitLane)
	if lane != "webkit40" && lane != "webkit41" {
		lane = defaultLinuxWebKitLane
	}
	names := linuxAppImageNames(arch, lane, version)
	asset, err := firstNamedAsset(assets, names)
	if err != nil && lane != defaultLinuxWebKitLane {
		asset, err = firstNamedAsset(assets, linuxAppImageNames(arch, defaultLinuxWebKitLane, version))
		lane = defaultLinuxWebKitLane
	}
	if err != nil {
		return selectedUpdateAsset{}, fmt.Errorf("no Linux %s AppImage in the latest GitHub Release: %w", arch, err)
	}
	return selectedUpdateAsset{
		Kind:     linuxAppImageKind,
		Name:     asset.Name,
		URL:      asset.BrowserDownloadURL,
		SHA256:   asset.SHA256(),
		Size:     asset.Size,
		Platform: "linux",
		Arch:     arch,
	}, nil
}

func selectMacSparklePayload(assets []githubAsset, arch, version string) (selectedUpdateAsset, error) {
	arch = normalizeDesktopArch(arch)
	if zip, err := firstNamedAsset(assets, macInstallerNames(arch, version, ".zip")); err == nil {
		return selectedUpdateAsset{
			Kind:     macSparkleZipKind,
			Name:     zip.Name,
			URL:      zip.BrowserDownloadURL,
			SHA256:   zip.SHA256(),
			Size:     zip.Size,
			Platform: "darwin",
			Arch:     arch,
		}, nil
	}
	dmg, err := firstNamedAsset(assets, macInstallerNames(arch, version, ".dmg"))
	if err != nil {
		return selectedUpdateAsset{}, fmt.Errorf("no macOS %s Sparkle payload (.zip of the notarized .app, else .dmg) in the latest GitHub Release: %w", arch, err)
	}
	return selectedUpdateAsset{
		Kind:     macSparkleDMGKind,
		Name:     dmg.Name,
		URL:      dmg.BrowserDownloadURL,
		SHA256:   dmg.SHA256(),
		Size:     dmg.Size,
		Platform: "darwin",
		Arch:     arch,
	}, nil
}

func windowsInstallerNames(arch, version string) []string {
	var names []string
	parsed, ok := parseDesktopVersion(version)
	candidates := []string{strings.TrimPrefix(strings.TrimSpace(version), "v")}
	if ok {
		candidates = parsed.FilenameCandidates()
	}
	for _, ver := range candidates {
		names = append(names,
			"fse-desktop-windows-"+arch+"-installer-"+ver+".exe",
			"fse-desktop-"+ver+"-windows-"+arch+"-installer.exe",
		)
	}
	return names
}

func linuxAppImageNames(arch, webkitLane, version string) []string {
	var names []string
	parsed, ok := parseDesktopVersion(version)
	candidates := []string{strings.TrimPrefix(strings.TrimSpace(version), "v")}
	if ok {
		candidates = parsed.FilenameCandidates()
	}
	for _, ver := range candidates {
		names = append(names,
			"fse-desktop-linux-"+arch+"-"+webkitLane+"-installers-"+ver+".AppImage",
			"fse-desktop-"+ver+"-linux-"+arch+".AppImage",
		)
	}
	return names
}

func macInstallerNames(arch, version, ext string) []string {
	var names []string
	parsed, ok := parseDesktopVersion(version)
	candidates := []string{strings.TrimPrefix(strings.TrimSpace(version), "v")}
	if ok {
		candidates = parsed.FilenameCandidates()
	}
	for _, ver := range candidates {
		names = append(names,
			"fse-desktop-darwin-"+arch+"-installer-"+ver+ext,
			"fse-desktop-"+ver+"-darwin-"+arch+ext,
		)
	}
	return names
}

func firstNamedAsset(assets []githubAsset, names []string) (githubAsset, error) {
	index := map[string]githubAsset{}
	for _, asset := range assets {
		index[strings.ToLower(asset.Name)] = asset
	}
	for _, name := range names {
		if asset, ok := index[strings.ToLower(name)]; ok {
			return asset, nil
		}
	}
	return githubAsset{}, fmt.Errorf("none of %s", strings.Join(names, ", "))
}

func checksumMapFromSHA256SUMS(contents string) map[string]string {
	sums := map[string]string{}
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hash := strings.ToLower(fields[0])
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if len(hash) == 64 {
			sums[name] = hash
			sums[strings.ToLower(name)] = hash
		}
	}
	return sums
}

func applyChecksumsToAsset(asset *selectedUpdateAsset, sums map[string]string) {
	if asset == nil || asset.SHA256 != "" || len(sums) == 0 {
		return
	}
	if hash, ok := sums[asset.Name]; ok {
		asset.SHA256 = hash
		return
	}
	if hash, ok := sums[strings.ToLower(asset.Name)]; ok {
		asset.SHA256 = hash
	}
}

func normalizeDesktopArch(arch string) string {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "arm64", "aarch64":
		return "arm64"
	case "amd64", "x86_64", "x64":
		return "amd64"
	default:
		if runtime.GOARCH == "arm64" {
			return "arm64"
		}
		return "amd64"
	}
}

func runtimeDesktopArch() string {
	return normalizeDesktopArch(runtime.GOARCH)
}
