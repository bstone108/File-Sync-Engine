package syncplan

import "testing"

func TestFolderModePolicyDeterminesAdvertiseAndApply(t *testing.T) {
	cases := []struct {
		mode             FolderMode
		advertiseLocal   bool
		applyRemote      bool
		reportLocalDrift bool
	}{
		{ModeSendReceive, true, true, false},
		{ModeSendOnly, true, false, false},
		{ModeReceiveOnly, false, true, true},
	}
	for _, tc := range cases {
		p := PolicyForMode(tc.mode)
		if p.AdvertiseLocalChanges != tc.advertiseLocal || p.ApplyRemoteChanges != tc.applyRemote || p.ReportLocalDrift != tc.reportLocalDrift {
			t.Fatalf("policy for %s = %+v", tc.mode, p)
		}
	}
}

func TestConflictPlannerKeepsBothVersionsForConcurrentBidirectionalEdits(t *testing.T) {
	local := FileVersion{Path: "notes.txt", Version: 4, DeviceID: "local", Hash: "localhash"}
	remote := FileVersion{Path: "notes.txt", Version: 4, DeviceID: "remote", Hash: "remotehash"}

	decision := Decide(local, remote, ModeSendReceive)
	if decision.Action != ActionConflictCopy {
		t.Fatalf("action = %s, want conflict copy", decision.Action)
	}
	if decision.KeepLocalPath == "" || decision.ApplyRemotePath == "" {
		t.Fatalf("conflict decision should preserve both versions: %+v", decision)
	}
	if decision.ApplyRemotePath != "notes.sync-conflict-remote.txt" {
		t.Fatalf("conflict path = %q, want extension-preserving conflict name", decision.ApplyRemotePath)
	}
}

func TestReceiveOnlyReportsLocalDriftInsteadOfAdvertising(t *testing.T) {
	local := FileVersion{Path: "notes.txt", Version: 5, DeviceID: "local", Hash: "newlocal"}
	remote := FileVersion{Path: "notes.txt", Version: 4, DeviceID: "remote", Hash: "oldremote"}

	decision := Decide(local, remote, ModeReceiveOnly)
	if decision.Action != ActionReportDrift {
		t.Fatalf("action = %s, want report drift", decision.Action)
	}
}
