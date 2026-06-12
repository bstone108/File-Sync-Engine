package peerevents

import (
	"testing"

	"filesyncengine/internal/peersync"
)

func TestSyncFinishedMessageIncludesCountersAndMissingIgnoreIncludes(t *testing.T) {
	message := SyncFinishedMessage(peersync.Result{
		Writes:                2,
		Deletes:               1,
		FilesMoved:            3,
		BlocksFetched:         4,
		BlocksReused:          5,
		MissingIgnoreIncludes: []string{".sync/rules", "nested/ignore"},
	})

	expected := "writes=2 deletes=1 moves=3 blocksFetched=4 blocksReused=5 missingIgnoreIncludes=2 paths=.sync/rules,nested/ignore"
	if message != expected {
		t.Fatalf("message = %q, want %q", message, expected)
	}
}

func TestSyncFinishedMessageWithRouteAppendsNonEmptyRouteFields(t *testing.T) {
	message := SyncFinishedMessageWithRoute(peersync.Result{Writes: 1}, Route{
		Path:        "direct",
		Network:     "lan",
		RouteReason: "true_local_preferred",
	})

	expected := "writes=1 deletes=0 moves=0 blocksFetched=0 blocksReused=0 routePath=direct routeNetwork=lan routeReason=true_local_preferred"
	if message != expected {
		t.Fatalf("message = %q, want %q", message, expected)
	}
}
