package ci

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestResolveReleaseVersionAutoStampsUnpaddedChicagoDateBuild(t *testing.T) {
	script := resolverScript(t)
	repo := initVersionRepo(t)

	got := runResolver(t, script, repo, pinnedChicagoEnv(), nil)
	if got != "2026.8.24.1" {
		t.Fatalf("first auto-stamp on 2026-08-24 Chicago with no tags: got %q, want 2026.8.24.1", got)
	}

	tagVersionRepo(t, repo, "v2026.8.24.1")
	got = runResolver(t, script, repo, pinnedChicagoEnv(), nil)
	if got != "2026.8.24.2" {
		t.Fatalf("second auto-stamp the same Chicago day: got %q, want 2026.8.24.2", got)
	}
}

func TestResolveReleaseVersionCountsPaddedHistoricalTags(t *testing.T) {
	script := resolverScript(t)
	repo := initVersionRepo(t)
	tagVersionRepo(t, repo, "v2026.08.24.01")

	got := runResolver(t, script, repo, pinnedChicagoEnv(), nil)
	if got != "2026.8.24.2" {
		t.Fatalf("padded v2026.08.24.01 must count as N=1: got %q, want 2026.8.24.2", got)
	}

	tagVersionRepo(t, repo, "v2026.8.24.2")
	got = runResolver(t, script, repo, pinnedChicagoEnv(), nil)
	if got != "2026.8.24.3" {
		t.Fatalf("mixed padded and unpadded same-day tags: got %q, want 2026.8.24.3", got)
	}
}

func TestResolveReleaseVersionIgnoresOtherDaysAndDevStamps(t *testing.T) {
	script := resolverScript(t)
	repo := initVersionRepo(t)
	tagVersionRepo(t, repo, "v2026.08.11.01", "v2026.8.23.9", "v0.1.99-test")

	got := runResolver(t, script, repo, pinnedChicagoEnv(), nil)
	if got != "2026.8.24.1" {
		t.Fatalf("other days and dummy tags must not consume 2026.8.24 N: got %q", got)
	}
}

func TestResolveReleaseVersionCountsRemoteTagsAndGitHubReleases(t *testing.T) {
	script := resolverScript(t)
	origin := initVersionRepo(t)
	tagVersionRepo(t, origin, "v2026.08.24.01")
	local := initVersionRepo(t)
	runGit(t, local, "remote", "add", "origin", origin)

	got := runResolver(t, script, local, pinnedChicagoEnv(), nil)
	if got != "2026.8.24.2" {
		t.Fatalf("remote padded tag must count: got %q, want 2026.8.24.2", got)
	}

	ghDir := t.TempDir()
	ghPath := filepath.Join(ghDir, "gh")
	if err := os.WriteFile(ghPath, []byte("#!/usr/bin/env bash\nset -euo pipefail\nif [[ \"${1:-}\" == \"release\" && \"${2:-}\" == \"list\" ]]; then\n  printf '%s\\n' 'v2026.8.24.2' 'v2026.08.11.01'\n  exit 0\nfi\nprintf 'unexpected gh invocation: %s\\n' \"$*\" >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write mock gh: %v", err)
	}
	empty := initVersionRepo(t)
	got = runResolver(t, script, empty, pinnedChicagoEnv(
		"GH_TOKEN=test-token",
		"GITHUB_REPOSITORY=bstone108/File-Sync-Engine",
		"PATH="+ghDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	), nil)
	if got != "2026.8.24.3" {
		t.Fatalf("GitHub release v2026.8.24.2 must count: got %q, want 2026.8.24.3", got)
	}
}

func TestResolveReleaseVersionExplicitAndTagPushStillWork(t *testing.T) {
	script := resolverScript(t)
	repo := initVersionRepo(t)
	tagVersionRepo(t, repo, "v2026.8.24.1")

	got := runResolver(t, script, repo, nil, []string{"2026.8.24.9"})
	if got != "2026.8.24.9" {
		t.Fatalf("explicit unpadded override: got %q, want 2026.8.24.9", got)
	}

	got = runResolver(t, script, repo, nil, []string{"2026.08.24.03"})
	if got != "2026.8.24.3" {
		t.Fatalf("explicit leftover padded version must canonicalize: got %q, want 2026.8.24.3", got)
	}

	got = runResolver(t, script, repo, []string{"GITHUB_REF_TYPE=tag", "GITHUB_REF_NAME=v2026.8.24.4"}, nil)
	if got != "2026.8.24.4" {
		t.Fatalf("tag push: got %q, want 2026.8.24.4", got)
	}

	got = runResolver(t, script, repo, []string{"GITHUB_REF_TYPE=tag", "GITHUB_REF_NAME=v2026.08.11.01"}, nil)
	if got != "2026.8.11.1" {
		t.Fatalf("padded tag push must canonicalize: got %q, want 2026.8.11.1", got)
	}

	got = runResolver(t, script, repo, []string{"GITHUB_REF_TYPE=tag", "GITHUB_REF_NAME=v2026.8.24.4"}, []string{"2026.8.25.1"})
	if got != "2026.8.25.1" {
		t.Fatalf("explicit argument must win over tag push: got %q, want 2026.8.25.1", got)
	}
}

func TestResolveReleaseVersionLiveChicagoCalendarDay(t *testing.T) {
	script := resolverScript(t)
	repo := initVersionRepo(t)

	ymd, err := exec.Command("bash", "-c", "TZ=America/Chicago date +%Y-%m-%d").Output()
	if err != nil {
		t.Fatalf("compute Chicago calendar day: %v", err)
	}
	parts := strings.Split(strings.TrimSpace(string(ymd)), "-")
	if len(parts) != 3 {
		t.Fatalf("unexpected Chicago date %q", ymd)
	}
	year, err := strconv.Atoi(parts[0])
	if err != nil {
		t.Fatalf("year: %v", err)
	}
	month, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Fatalf("month: %v", err)
	}
	day, err := strconv.Atoi(parts[2])
	if err != nil {
		t.Fatalf("day: %v", err)
	}
	want := fmt.Sprintf("%d.%d.%d.1", year, month, day)

	got := runResolver(t, script, repo, nil, nil)
	if got != want {
		t.Fatalf("live America/Chicago auto-stamp: got %q, want %q", got, want)
	}
}

func TestResolveReleaseVersionRejectsInvalidAndActionsDateOverride(t *testing.T) {
	script := resolverScript(t)
	repo := initVersionRepo(t)

	if _, err := runResolverErr(t, script, repo, nil, []string{"not-a-version"}); err == nil {
		t.Fatal("invalid explicit version should fail")
	}
	output, err := runResolverErr(t, script, repo, []string{"GITHUB_ACTIONS=true", "FSE_RELEASE_DATE=2026-08-24"}, nil)
	if err == nil || !strings.Contains(output, "FSE_RELEASE_DATE is test-only") {
		t.Fatalf("FSE_RELEASE_DATE must be rejected in GitHub Actions; output:\n%s", output)
	}
	// Explicit success path should still work without the test-only date override.
	got := runResolver(t, script, repo, []string{"GITHUB_ACTIONS=true", "GH_TOKEN=unused"}, []string{"2026.8.24.1"})
	if got != "2026.8.24.1" {
		t.Fatalf("explicit version in GITHUB_ACTIONS: got %q, want 2026.8.24.1", got)
	}
}

func TestResolveReleaseVersionIsReleasePathOnly(t *testing.T) {
	script := readRequiredFile(t, filepath.Join("..", "..", "scripts", "resolve-release-version.sh"))
	ci := readWorkflow(t, "ci.yml")
	buildAll := readRequiredFile(t, filepath.Join("..", "..", "scripts", "build-all.sh"))
	packagerTest := readRequiredFile(t, filepath.Join("desktop_gui_release_packager_behavior_test.go"))

	for _, want := range []string{
		"never used for go test",
		"serious harness",
		"PR CI",
		"0.1.0-dev",
		"0.1.99-test",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("resolver must document that date.build is release-only, missing %q", want)
		}
	}
	if strings.Contains(ci, "resolve-release-version.sh") || strings.Contains(ci, "YYYY.M.D.N") {
		t.Fatal("PR CI must not stamp published date.build versions")
	}
	if !strings.Contains(buildAll, `VERSION="${1:-0.1.0-dev}"`) {
		t.Fatal("local build-all must keep its dummy 0.1.0-dev default")
	}
	if !strings.Contains(packagerTest, "0.1.99-test") {
		t.Fatal("packager contract tests must keep dummy 0.1.99-test versions")
	}
}

func resolverScript(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "scripts", "resolve-release-version.sh"))
	if err != nil {
		t.Fatalf("resolver path: %v", err)
	}
	return path
}

func initVersionRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "version-test@example.com")
	runGit(t, dir, "config", "user.name", "Version Test")
	runGit(t, dir, "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "init")
	return dir
}

func tagVersionRepo(t *testing.T, dir string, tags ...string) {
	t.Helper()
	for _, tag := range tags {
		runGit(t, dir, "tag", tag)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, output)
	}
}

func runResolver(t *testing.T, script, dir string, extraEnv, args []string) string {
	t.Helper()
	got, err := runResolverErr(t, script, dir, extraEnv, args)
	if err != nil {
		t.Fatalf("resolve-release-version.sh failed: %v\n%s", err, got)
	}
	return got
}

func runResolverErr(t *testing.T, script, dir string, extraEnv, args []string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = dir
	cmd.Env = resolverTestEnv(extraEnv)
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func pinnedChicagoEnv(extra ...string) []string {
	return append([]string{"FSE_RELEASE_DATE=2026-08-24"}, extra...)
}

func resolverTestEnv(extra []string) []string {
	drop := map[string]struct{}{
		"GH_TOKEN":          {},
		"GITHUB_TOKEN":      {},
		"GITHUB_ACTIONS":    {},
		"GITHUB_REF_TYPE":   {},
		"GITHUB_REF_NAME":   {},
		"GITHUB_REPOSITORY": {},
		"FSE_RELEASE_DATE":  {},
	}
	replaced := map[string]string{}
	order := make([]string, 0, len(extra))
	for _, item := range extra {
		key, _, _ := strings.Cut(item, "=")
		if _, seen := replaced[key]; !seen {
			order = append(order, key)
		}
		replaced[key] = item
		drop[key] = struct{}{}
	}
	env := make([]string, 0, 16)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if _, skip := drop[key]; skip {
			continue
		}
		env = append(env, item)
	}
	for _, key := range order {
		env = append(env, replaced[key])
	}
	return env
}
