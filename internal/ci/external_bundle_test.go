package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExternalSmokeBundleScriptsCoverTargetsAndResults(t *testing.T) {
	root := filepath.Join("..", "..")
	generatorPath := filepath.Join(root, "scripts", "make-external-smoke-bundle.sh")
	generatorBytes, err := os.ReadFile(generatorPath)
	if err != nil {
		t.Fatalf("read external smoke bundle generator: %v", err)
	}
	generator := string(generatorBytes)

	requiredGeneratorText := []string{
		"fse-external-smoke",
		"external-smoke-unix.sh",
		"external-smoke-windows.ps1",
		"linux-amd64",
		"linux-arm64",
		"darwin-arm64",
		"windows-amd64",
		"windows-arm64",
		"SHA256SUMS",
	}
	for _, text := range requiredGeneratorText {
		if !strings.Contains(generator, text) {
			t.Fatalf("generator missing %q", text)
		}
	}

	unixPath := filepath.Join(root, "scripts", "external-smoke-unix.sh")
	unixBytes, err := os.ReadFile(unixPath)
	if err != nil {
		t.Fatalf("read unix smoke runner: %v", err)
	}
	unixScript := string(unixBytes)
	for _, text := range []string{
		"host.json",
		"summary.md",
		"results.json",
		"uname -m",
		"sysctl -n machdep.cpu.brand_string",
		"lscpu",
		"fse validate",
		"fse config init",
		"fse scan",
		"fse status",
		"zip",
		"tar",
	} {
		if !strings.Contains(unixScript, text) {
			t.Fatalf("unix smoke runner missing %q", text)
		}
	}

	windowsPath := filepath.Join(root, "scripts", "external-smoke-windows.ps1")
	windowsBytes, err := os.ReadFile(windowsPath)
	if err != nil {
		t.Fatalf("read windows smoke runner: %v", err)
	}
	windowsScript := string(windowsBytes)
	for _, text := range []string{
		"host.json",
		"summary.md",
		"results.json",
		"Get-CimInstance Win32_OperatingSystem",
		"Get-CimInstance Win32_Processor",
		"fse.exe validate",
		"fse.exe config init",
		"fse.exe scan",
		"fse.exe status",
		"Compress-Archive",
	} {
		if !strings.Contains(windowsScript, text) {
			t.Fatalf("windows smoke runner missing %q", text)
		}
	}
}

func TestExternalMetadataBenchmarkBundleScriptsCoverBetterHardwareRuns(t *testing.T) {
	root := filepath.Join("..", "..")
	generatorPath := filepath.Join(root, "scripts", "make-external-metabench-bundle.sh")
	generatorBytes, err := os.ReadFile(generatorPath)
	if err != nil {
		t.Fatalf("read external metadata benchmark bundle generator: %v", err)
	}
	generator := string(generatorBytes)

	for _, text := range []string{
		"fse-external-metabench",
		"fse-metabench",
		"cmd/fse-metabench",
		"linux-amd64",
		"linux-arm64",
		"darwin-amd64",
		"darwin-arm64",
		"windows-amd64",
		"windows-arm64",
		"external-metabench-unix.sh",
		"external-metabench-windows.ps1",
		"SHA256SUMS",
	} {
		if !strings.Contains(generator, text) {
			t.Fatalf("metadata benchmark bundle generator missing %q", text)
		}
	}

	unixPath := filepath.Join(root, "scripts", "external-metabench-unix.sh")
	unixBytes, err := os.ReadFile(unixPath)
	if err != nil {
		t.Fatalf("read unix metadata benchmark runner: %v", err)
	}
	unixScript := string(unixBytes)
	for _, text := range []string{
		"host.json",
		"metadata-benchmark.md",
		"fse-metabench",
		"-output",
		"uname -a",
		"lscpu",
		"sysctl -n machdep.cpu.brand_string",
		"tar",
	} {
		if !strings.Contains(unixScript, text) {
			t.Fatalf("unix metadata benchmark runner missing %q", text)
		}
	}

	windowsPath := filepath.Join(root, "scripts", "external-metabench-windows.ps1")
	windowsBytes, err := os.ReadFile(windowsPath)
	if err != nil {
		t.Fatalf("read windows metadata benchmark runner: %v", err)
	}
	windowsScript := string(windowsBytes)
	for _, text := range []string{
		"host.json",
		"metadata-benchmark.md",
		"fse-metabench.exe",
		"-output",
		"Get-CimInstance Win32_OperatingSystem",
		"Get-CimInstance Win32_Processor",
		"Compress-Archive",
	} {
		if !strings.Contains(windowsScript, text) {
			t.Fatalf("windows metadata benchmark runner missing %q", text)
		}
	}
}
