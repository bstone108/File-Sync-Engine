package link

import (
	"testing"
	"time"

	"filesyncengine/internal/discovery"
	"filesyncengine/internal/nat"
)

func TestOfflinePeerDialIsPromptThenSixtySeconds(t *testing.T) {
	forceDialGate = true
	resetPeerDialSchedule()
	t.Cleanup(func() {
		forceDialGate = false
		resetPeerDialSchedule()
	})
	if !allowPeerDial("kiki", nat.DefaultPaths) {
		t.Fatal("the first offline dial was deferred")
	}
	if allowPeerDial("kiki", nat.DefaultPaths) {
		t.Fatal("an offline peer was redialed before 60 seconds")
	}
	peerDialMu.Lock()
	peerDialNext["kiki"] = time.Now().Add(-time.Second)
	peerDialMu.Unlock()
	if !allowPeerDial("kiki", nat.DefaultPaths) {
		t.Fatal("the offline peer was not redialed at the 60 second interval")
	}
	if discovery.OfflineProbeInterval != 60*time.Second {
		t.Fatalf("offline interval = %s", discovery.OfflineProbeInterval)
	}
}
