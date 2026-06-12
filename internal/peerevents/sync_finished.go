package peerevents

import (
	"fmt"
	"strings"

	"filesyncengine/internal/peersync"
)

type Route struct {
	Path        string
	Network     string
	RouteReason string
}

func SyncFinishedMessage(result peersync.Result) string {
	message := fmt.Sprintf("writes=%d deletes=%d moves=%d blocksFetched=%d blocksReused=%d", result.Writes, result.Deletes, result.FilesMoved, result.BlocksFetched, result.BlocksReused)
	if len(result.MissingIgnoreIncludes) > 0 {
		message += fmt.Sprintf(" missingIgnoreIncludes=%d paths=%s", len(result.MissingIgnoreIncludes), strings.Join(result.MissingIgnoreIncludes, ","))
	}
	return message
}

func SyncFinishedMessageWithRoute(result peersync.Result, route Route) string {
	message := SyncFinishedMessage(result)
	if route.Path != "" {
		message += fmt.Sprintf(" routePath=%s", route.Path)
	}
	if route.Network != "" {
		message += fmt.Sprintf(" routeNetwork=%s", route.Network)
	}
	if route.RouteReason != "" {
		message += fmt.Sprintf(" routeReason=%s", route.RouteReason)
	}
	return message
}
