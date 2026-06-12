package clioutput

import (
	"fmt"
	"strings"

	"filesyncengine/internal/scancontrol"
)

func ScanOutput(result scancontrol.Result, perFolderMetadata bool) string {
	var b strings.Builder
	for _, folder := range result.Folders {
		if perFolderMetadata {
			fmt.Fprintf(&b, "scan finished: folder=%s changed=%d deleted=%d state=%s\n", folder.FolderID, folder.Changed, folder.Deleted, folder.StatePath)
			continue
		}
		fmt.Fprintf(&b, "scan finished: folder=%s changed=%d deleted=%d\n", folder.FolderID, folder.Changed, folder.Deleted)
	}
	fmt.Fprintf(&b, "scan summary: folders=%d state=%s\n", len(result.Folders), result.StatePath)
	return b.String()
}
