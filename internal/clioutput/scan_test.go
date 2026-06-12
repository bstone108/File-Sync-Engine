package clioutput

import (
	"testing"

	"filesyncengine/internal/scancontrol"
)

func TestScanOutputRendersFolderRowsAndSummary(t *testing.T) {
	result := scancontrol.Result{
		StatePath: "/tmp/state/root",
		Folders: []scancontrol.FolderResult{
			{FolderID: "docs", Changed: 3, Deleted: 1, StatePath: "/tmp/state/root/docs.badger"},
			{FolderID: "media", Changed: 0, Deleted: 2, StatePath: "/tmp/state/root/media.badger"},
		},
	}

	got := ScanOutput(result, true)
	want := "scan finished: folder=docs changed=3 deleted=1 state=/tmp/state/root/docs.badger\n" +
		"scan finished: folder=media changed=0 deleted=2 state=/tmp/state/root/media.badger\n" +
		"scan summary: folders=2 state=/tmp/state/root\n"
	if got != want {
		t.Fatalf("unexpected scan output:\n got: %q\nwant: %q", got, want)
	}
}

func TestScanOutputOmitsFolderStateForSingleStore(t *testing.T) {
	result := scancontrol.Result{
		StatePath: "/tmp/state.json",
		Folders: []scancontrol.FolderResult{
			{FolderID: "docs", Changed: 3, Deleted: 1, StatePath: "/tmp/state.json"},
		},
	}

	got := ScanOutput(result, false)
	want := "scan finished: folder=docs changed=3 deleted=1\n" +
		"scan summary: folders=1 state=/tmp/state.json\n"
	if got != want {
		t.Fatalf("unexpected scan output:\n got: %q\nwant: %q", got, want)
	}
}
