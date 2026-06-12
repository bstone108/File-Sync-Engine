package apicontrol

import (
	"fmt"

	"filesyncengine/internal/api"
	"filesyncengine/internal/foldersync"
)

// ApplyFolderWarningProjections projects scanner inaccessible-file warnings into
// API folder warning state and realtime events. Unknown folders still produce an
// event so observers can see the warning without mutating unrelated folder state.
func ApplyFolderWarningProjections(state api.State, warnings []foldersync.InaccessibleWarning) (api.State, []api.Event) {
	if len(warnings) == 0 {
		return state, nil
	}
	folderIndexes := map[string]int{}
	for i, folder := range state.FoldersState {
		folderIndexes[folder.ID] = i
	}
	events := make([]api.Event, 0, len(warnings))
	for _, warning := range warnings {
		message := fmt.Sprintf("%s scan could not read %s: %s", warning.Role, warning.Path, warning.Error)
		if index, ok := folderIndexes[warning.FolderID]; ok {
			folderWarning := api.FolderWarning{Kind: "inaccessible", Path: warning.Path, Message: message}
			state.FoldersState[index].Warnings.InaccessibleFiles++
			state.FoldersState[index].Warnings.Recent = append(state.FoldersState[index].Warnings.Recent, folderWarning)
		}
		events = append(events, api.Event{Type: "folder.warning", FolderID: warning.FolderID, Path: warning.Path, Message: message})
	}
	return state, events
}
