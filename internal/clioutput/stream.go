package clioutput

import (
	"fmt"

	"filesyncengine/internal/streamsync"
)

func StreamPullSummary(result streamsync.PullResult) string {
	return fmt.Sprintf("stream pull finished: writes=%d deletes=%d moves=%d blocksFetched=%d blocksReused=%d\n", result.FilesWritten, result.FilesDeleted, result.FilesMoved, result.BlocksFetched, result.BlocksReused)
}
