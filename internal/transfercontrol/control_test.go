package transfercontrol

import "testing"

func TestControlMatchesExactFolderWideAndPeerWideScopes(t *testing.T) {
	control := New()

	control.Pause("docs", "peer-a")
	if !control.IsPaused("docs", "peer-a") {
		t.Fatal("exact pause scope should match")
	}
	if control.IsPaused("docs", "peer-b") {
		t.Fatal("exact pause scope should not match a different peer")
	}

	control.Resume("docs", "peer-a")
	if control.IsPaused("docs", "peer-a") {
		t.Fatal("resume should clear exact pause scope")
	}

	control.Pause("media", "")
	if !control.IsPaused("media", "peer-b") {
		t.Fatal("folder-wide pause scope should match any peer in that folder")
	}
	if control.IsPaused("docs", "peer-b") {
		t.Fatal("folder-wide pause should not match a different folder")
	}

	control.Pause("", "peer-c")
	if !control.IsPaused("docs", "peer-c") {
		t.Fatal("peer-wide pause scope should match any folder for that peer")
	}
}

func TestClearCancelRemovesMatchingBroadScopesAfterObservation(t *testing.T) {
	control := New()
	control.Cancel("docs", "peer-a")
	control.Cancel("docs", "")
	control.Cancel("", "peer-a")
	control.Cancel("media", "peer-a")
	control.Cancel("docs", "peer-b")

	if !control.IsCancelled("docs", "peer-a") {
		t.Fatal("cancel scope should match before clear")
	}

	control.ClearCancel("docs", "peer-a")

	if control.IsCancelled("docs", "peer-a") {
		t.Fatal("clear should remove exact, folder-wide, and peer-wide scopes observed by the pass")
	}
	if !control.IsCancelled("media", "peer-a") {
		t.Fatal("clear should not remove another exact folder/peer cancel")
	}
	if !control.IsCancelled("docs", "peer-b") {
		t.Fatal("clear should not remove another exact peer cancel in the same folder")
	}
}
