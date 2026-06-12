package recovery

import "testing"

func TestIsInterruptedTempNameMatchesOnlyOwnedTempMarkers(t *testing.T) {
	owned := []string{
		".doc.txt.12345.staging",
		"doc.txt.fse-peer-tmp",
		"doc.txt.fse-stream-tmp",
	}
	for _, name := range owned {
		if !IsInterruptedTempName(name) {
			t.Fatalf("expected %q to be treated as owned interrupted temp", name)
		}
	}

	userFiles := []string{
		".project.notes.staging",
		"draft.staging",
		"notes.fse-peer-tmp.backup",
	}
	for _, name := range userFiles {
		if IsInterruptedTempName(name) {
			t.Fatalf("ordinary user file %q should not be removed as interrupted temp", name)
		}
	}
}
