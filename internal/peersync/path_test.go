package peersync

import "testing"

func TestSafeLocalPathRejectsTraversal(t *testing.T) {
	if _, ok := safeLocalPath(t.TempDir(), "../escape.txt"); ok {
		t.Fatal("accepted traversal path")
	}
	if _, ok := safeLocalPath(t.TempDir(), "/absolute.txt"); ok {
		t.Fatal("accepted absolute path")
	}
}
