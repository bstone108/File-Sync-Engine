package cli

import "testing"

func TestParseSupportsStatusAndConfigMutationCommands(t *testing.T) {
	status, err := Parse([]string{"status", "/tmp/fse.jsonc"})
	if err != nil {
		t.Fatalf("status parse: %v", err)
	}
	if status.Command != CommandStatus || status.ConfigPath != "/tmp/fse.jsonc" {
		t.Fatalf("bad status opts: %+v", status)
	}

	peer, err := Parse([]string{"peer", "add", "peer-b", "--endpoint", "pipe:stdio", "/tmp/fse.jsonc"})
	if err != nil {
		t.Fatalf("peer parse: %v", err)
	}
	if peer.Command != CommandPeer || peer.Action != ActionAdd || peer.ID != "peer-b" || peer.Endpoint != "pipe:stdio" {
		t.Fatalf("bad peer opts: %+v", peer)
	}

	folder, err := Parse([]string{"folder", "add", "docs", "./docs", "--mode", "sendonly"})
	if err != nil {
		t.Fatalf("folder parse: %v", err)
	}
	if folder.Command != CommandFolder || folder.Action != ActionAdd || folder.ID != "docs" || folder.Path != "./docs" || folder.Mode != "sendonly" {
		t.Fatalf("bad folder opts: %+v", folder)
	}
}
