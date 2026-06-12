package cli

import "testing"

func TestParseSupportsMinimalStartStopAndConfigOverride(t *testing.T) {
	start, err := Parse([]string{"start", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse start: %v", err)
	}
	if start.Command != CommandStart || start.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected start options: %+v", start)
	}

	stop, err := Parse([]string{"stop"})
	if err != nil {
		t.Fatalf("Parse stop: %v", err)
	}
	if stop.Command != CommandStop || stop.ConfigPath != "" {
		t.Fatalf("unexpected stop options: %+v", stop)
	}
}

func TestParseRejectsExtraArgumentsAndUnknownCommands(t *testing.T) {
	if _, err := Parse([]string{"restart"}); err == nil {
		t.Fatalf("unknown command should fail")
	}
	if _, err := Parse([]string{"start", "a", "b"}); err == nil {
		t.Fatalf("extra config paths should fail")
	}
}

func TestParseSupportsStreamServeAndPull(t *testing.T) {
	serve, err := Parse([]string{"stream", "serve", "docs", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse stream serve: %v", err)
	}
	if serve.Command != CommandStream || serve.Action != ActionServe || serve.ID != "docs" || serve.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected serve options: %+v", serve)
	}

	pull, err := Parse([]string{"stream", "pull", "docs", "/tmp/target", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse stream pull: %v", err)
	}
	if pull.Command != CommandStream || pull.Action != ActionPull || pull.ID != "docs" || pull.Path != "/tmp/target" || pull.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected pull options: %+v", pull)
	}
}

func TestParseSupportsValidateAndScanCommands(t *testing.T) {
	validate, err := Parse([]string{"validate", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse validate: %v", err)
	}
	if validate.Command != CommandValidate || validate.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected validate options: %+v", validate)
	}

	scanAll, err := Parse([]string{"scan", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse scan all: %v", err)
	}
	if scanAll.Command != CommandScan || scanAll.ConfigPath != "/etc/fse/config.json" || scanAll.ID != "" {
		t.Fatalf("unexpected scan all options: %+v", scanAll)
	}

	scanOne, err := Parse([]string{"scan", "--folder", "docs", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse scan folder: %v", err)
	}
	if scanOne.Command != CommandScan || scanOne.ID != "docs" || scanOne.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected scan folder options: %+v", scanOne)
	}
}

func TestParseSupportsMetadataCompactCommand(t *testing.T) {
	compactAll, err := Parse([]string{"metadata", "compact", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse metadata compact: %v", err)
	}
	if compactAll.Command != CommandMetadata || compactAll.Action != ActionCompact || compactAll.ConfigPath != "/etc/fse/config.json" || compactAll.ID != "" {
		t.Fatalf("unexpected metadata compact options: %+v", compactAll)
	}

	compactOne, err := Parse([]string{"metadata", "compact", "--folder", "docs", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse metadata compact folder: %v", err)
	}
	if compactOne.Command != CommandMetadata || compactOne.Action != ActionCompact || compactOne.ID != "docs" || compactOne.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected metadata compact folder options: %+v", compactOne)
	}
}

func TestParseSupportsMetadataImportJSONCommand(t *testing.T) {
	importAll, err := Parse([]string{"metadata", "import-json", "--source", "/var/lib/fse/state.json", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse metadata import-json: %v", err)
	}
	if importAll.Command != CommandMetadata || importAll.Action != ActionImportJSON || importAll.Path != "/var/lib/fse/state.json" || importAll.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected metadata import-json options: %+v", importAll)
	}

	if _, err := Parse([]string{"metadata", "import-json", "/etc/fse/config.json"}); err == nil {
		t.Fatalf("metadata import-json without --source should fail")
	}
}

func TestParseSupportsMetadataSplitBadgerCommand(t *testing.T) {
	split, err := Parse([]string{"metadata", "split-badger", "--source", "/var/lib/fse/metadata.badger", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse metadata split-badger: %v", err)
	}
	if split.Command != CommandMetadata || split.Action != ActionSplitBadger || split.Path != "/var/lib/fse/metadata.badger" || split.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected metadata split-badger options: %+v", split)
	}

	if _, err := Parse([]string{"metadata", "split-badger", "/etc/fse/config.json"}); err == nil {
		t.Fatalf("metadata split-badger without --source should fail")
	}
}

func TestParseSupportsSnapshotMarkerCommands(t *testing.T) {
	create, err := Parse([]string{"snapshot", "create", "--folder", "docs", "--description", "before cleanup", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse snapshot create: %v", err)
	}
	if create.Command != CommandSnapshot || create.Action != ActionCreate || create.ID != "docs" || create.Mode != "before cleanup" || create.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected snapshot create options: %+v", create)
	}
	show, err := Parse([]string{"snapshot", "show", "snap-001", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse snapshot show: %v", err)
	}
	if show.Command != CommandSnapshot || show.Action != ActionShow || show.ID != "snap-001" || show.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected snapshot show options: %+v", show)
	}
	list, err := Parse([]string{"snapshot", "list", "--folder", "docs", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse snapshot list: %v", err)
	}
	if list.Action != ActionList || list.ID != "docs" || list.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected snapshot list options: %+v", list)
	}
	for _, action := range []string{"pin", "deprecate", "delete"} {
		parsed, err := Parse([]string{"snapshot", action, "snap-001", "/etc/fse/config.json"})
		if err != nil {
			t.Fatalf("Parse snapshot %s: %v", action, err)
		}
		if parsed.Command != CommandSnapshot || parsed.ID != "snap-001" || parsed.ConfigPath != "/etc/fse/config.json" {
			t.Fatalf("unexpected snapshot %s options: %+v", action, parsed)
		}
	}
	if _, err := Parse([]string{"snapshot", "create", "/etc/fse/config.json"}); err == nil {
		t.Fatalf("snapshot create without --folder should fail")
	}
}

func TestParseSnapshotRestorePlanCommand(t *testing.T) {
	parsed, err := Parse([]string{"snapshot", "restore-plan", "--snapshot", "snap-001", "--path", "dir/alpha.txt", "--path", "dir/beta.txt", "--destination", "/tmp/restore", "--alternate", "restored/alpha.txt", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse snapshot restore-plan: %v", err)
	}
	if parsed.Command != CommandSnapshot || parsed.Action != ActionRestorePlan || parsed.ID != "snap-001" || parsed.Destination != "/tmp/restore" || parsed.Path != "restored/alpha.txt" || parsed.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected restore-plan options: %+v", parsed)
	}
	if len(parsed.Paths) != 2 || parsed.Paths[0] != "dir/alpha.txt" || parsed.Paths[1] != "dir/beta.txt" {
		t.Fatalf("restore-plan selected paths not parsed: %+v", parsed.Paths)
	}
	if _, err := Parse([]string{"snapshot", "restore-plan", "--path", "dir/alpha.txt", "/etc/fse/config.json"}); err == nil {
		t.Fatalf("snapshot restore-plan without --snapshot should fail")
	}
}

func TestParseSnapshotRetentionCommand(t *testing.T) {
	parsed, err := Parse([]string{"snapshot", "retention", "--keep-last", "2", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse snapshot retention: %v", err)
	}
	if parsed.Command != CommandSnapshot || parsed.Action != ActionRetention || parsed.KeepLast != 2 || parsed.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected snapshot retention options: %+v", parsed)
	}
	if _, err := Parse([]string{"snapshot", "retention", "/etc/fse/config.json"}); err == nil {
		t.Fatalf("snapshot retention without --keep-last should fail")
	}
	if _, err := Parse([]string{"snapshot", "retention", "--keep-last", "0", "/etc/fse/config.json"}); err == nil {
		t.Fatalf("snapshot retention should reject non-positive keep-last")
	}
}

func TestParseSnapshotRestoreCommand(t *testing.T) {
	parsed, err := Parse([]string{"snapshot", "restore", "--snapshot", "snap-001", "--path", "dir/alpha.txt", "--destination", "/tmp/restore", "--alternate", "restored/alpha.txt", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse snapshot restore: %v", err)
	}
	if parsed.Command != CommandSnapshot || parsed.Action != ActionRestore || parsed.ID != "snap-001" || parsed.Destination != "/tmp/restore" || parsed.Path != "restored/alpha.txt" || parsed.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected restore options: %+v", parsed)
	}
	if len(parsed.Paths) != 1 || parsed.Paths[0] != "dir/alpha.txt" {
		t.Fatalf("restore selected paths not parsed: %+v", parsed.Paths)
	}
	if _, err := Parse([]string{"snapshot", "restore", "--path", "dir/alpha.txt", "/etc/fse/config.json"}); err == nil {
		t.Fatalf("snapshot restore without --snapshot should fail")
	}
	if _, err := Parse([]string{"snapshot", "restore", "--snapshot", "snap-001", "--revert-database", "/etc/fse/config.json"}); err == nil {
		t.Fatalf("ordinary snapshot restore must reject database reversion flags")
	}
}

func TestParseMetadataCompactCommand(t *testing.T) {
	scrubAll, err := Parse([]string{"maintenance", "scrub", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse maintenance scrub: %v", err)
	}
	if scrubAll.Command != CommandMaintenance || scrubAll.Action != ActionScrub || scrubAll.ConfigPath != "/etc/fse/config.json" || scrubAll.ID != "" {
		t.Fatalf("unexpected maintenance scrub options: %+v", scrubAll)
	}

	scrubOne, err := Parse([]string{"maintenance", "scrub", "--folder", "docs", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse maintenance scrub folder: %v", err)
	}
	if scrubOne.Command != CommandMaintenance || scrubOne.Action != ActionScrub || scrubOne.ID != "docs" || scrubOne.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected maintenance scrub folder options: %+v", scrubOne)
	}

	backupScrub, err := Parse([]string{"maintenance", "backup-scrub", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse maintenance backup-scrub: %v", err)
	}
	if backupScrub.Command != CommandMaintenance || backupScrub.Action != ActionBackupScrub || backupScrub.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected maintenance backup-scrub options: %+v", backupScrub)
	}
}

func TestParseSupportsServiceRenderCommand(t *testing.T) {
	render, err := Parse([]string{"service", "render", "--platform", "systemd", "--binary", "/usr/local/bin/fse", "--user", "fse", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse service render: %v", err)
	}
	if render.Command != CommandService || render.Action != ActionRender || render.Platform != "systemd" || render.Path != "/usr/local/bin/fse" || render.User != "fse" || render.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected service render options: %+v", render)
	}
	if _, err := Parse([]string{"service", "render", "--platform", "systemd", "/etc/fse/config.json"}); err == nil {
		t.Fatalf("service render without --binary should fail")
	}
	if _, err := Parse([]string{"service", "render", "--binary", "/usr/local/bin/fse", "/etc/fse/config.json"}); err == nil {
		t.Fatalf("service render without --platform should fail")
	}
}

func TestParseSupportsServiceControlCommands(t *testing.T) {
	status, err := Parse([]string{"service", "status", "--platform", "systemd", "--name", "fse.service", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse service status: %v", err)
	}
	if status.Command != CommandService || status.Action != ActionStatus || status.Platform != "systemd" || status.ID != "fse.service" || status.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected service status options: %+v", status)
	}

	restart, err := Parse([]string{"service", "restart", "--platform", "launchd", "--name", "com.example.fse", "--domain", "system", "/Library/Application Support/FSE/config.json"})
	if err != nil {
		t.Fatalf("Parse service restart: %v", err)
	}
	if restart.Action != ActionRestart || restart.Platform != "launchd" || restart.ID != "com.example.fse" || restart.Domain != "system" {
		t.Fatalf("unexpected service restart options: %+v", restart)
	}

	if _, err := Parse([]string{"service", "status", "--platform", "systemd"}); err == nil {
		t.Fatalf("service control without --name should fail")
	}
	if _, err := Parse([]string{"service", "status", "--name", "fse.service"}); err == nil {
		t.Fatalf("service control without --platform should fail")
	}
}

func TestParseWebGUICommands(t *testing.T) {
	status, err := Parse([]string{"web-gui", "status", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse web-gui status: %v", err)
	}
	if status.Command != CommandWebGUI || status.Action != ActionStatus || status.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected web-gui status options: %+v", status)
	}
	for _, action := range []Action{ActionInstall, ActionUpdate, ActionStart, ActionStop} {
		parsed, err := Parse([]string{"web-gui", string(action), "/etc/fse/config.json"})
		if err != nil {
			t.Fatalf("Parse web-gui %s: %v", action, err)
		}
		if parsed.Command != CommandWebGUI || parsed.Action != action || parsed.ConfigPath != "/etc/fse/config.json" {
			t.Fatalf("unexpected web-gui %s options: %+v", action, parsed)
		}
	}
	if _, err := Parse([]string{"web-gui", "restart"}); err == nil {
		t.Fatalf("unsupported web-gui action should fail")
	}
}

func TestParseIdentityCommands(t *testing.T) {
	export, err := Parse([]string{"identity", "export", "--group", "family-sync", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse identity export: %v", err)
	}
	if export.Command != CommandIdentity || export.Action != ActionExport || export.ID != "family-sync" || export.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected identity export options: %+v", export)
	}

	importOpts, err := Parse([]string{"identity", "import", "--package", "/tmp/identity.json", "/etc/fse/config.json"})
	if err != nil {
		t.Fatalf("Parse identity import: %v", err)
	}
	if importOpts.Command != CommandIdentity || importOpts.Action != ActionImport || importOpts.Path != "/tmp/identity.json" || importOpts.ConfigPath != "/etc/fse/config.json" {
		t.Fatalf("unexpected identity import options: %+v", importOpts)
	}
	if _, err := Parse([]string{"identity", "import", "/etc/fse/config.json"}); err == nil {
		t.Fatalf("identity import without --package should fail")
	}
}
