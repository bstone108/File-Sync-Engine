package engine

const (
	IndexModeVerified      = "verified"
	IndexModeLazyHashing   = "lazy-hashing"
	IndexModeQuickMetadata = "quick-metadata"
	IndexModeRepairing     = "repairing"
)

type FolderIndexState struct {
	Mode                   string `json:"mode"`
	TotalFiles             int    `json:"totalFiles"`
	VerifiedFiles          int    `json:"verifiedFiles"`
	UnknownFiles           int    `json:"unknownFiles"`
	UnverifiedSeedFiles    int    `json:"unverifiedSeedFiles"`
	KnownBlocks            int    `json:"knownBlocks"`
	BadBlocks              int    `json:"badBlocks"`
	QueuedHashJobs         int    `json:"queuedHashJobs"`
	ActiveHashJobs         int    `json:"activeHashJobs"`
	DateCorrectionsPending int    `json:"dateCorrectionsPending"`
	ProvisionalReadOnly    bool   `json:"provisionalReadOnly"`
}

func (e Engine) FolderIndexState(folderID string) (FolderIndexState, error) {
	manifests, err := e.store.ListManifests(folderID)
	if err != nil {
		return FolderIndexState{}, err
	}
	state := FolderIndexState{Mode: IndexModeVerified}
	for _, manifest := range manifests {
		state.TotalFiles++
		state.KnownBlocks += len(manifest.Blocks)
		switch manifest.HashState {
		case "complete":
			state.VerifiedFiles++
		case HashStateAssumedValidUnverified:
			state.UnverifiedSeedFiles++
			state.QueuedHashJobs++
			state.ProvisionalReadOnly = true
			if manifest.ModTimeUnixNano != 0 && manifest.SeedBaselineModTimeUnixNano != manifest.ModTimeUnixNano {
				state.DateCorrectionsPending++
			}
		default:
			state.UnknownFiles++
			state.QueuedHashJobs++
		}
	}
	if state.UnverifiedSeedFiles > 0 {
		state.Mode = IndexModeLazyHashing
	} else if state.UnknownFiles > 0 {
		state.Mode = IndexModeQuickMetadata
	}
	return state, nil
}
