package discovery

import (
	"testing"
	"time"
)

func TestProbeIntervalsAreFiveAndSixtySeconds(t *testing.T) {
	if LiveProbeInterval != 5*time.Second {
		t.Fatalf("live probe = %s", LiveProbeInterval)
	}
	if OfflineProbeInterval != 60*time.Second {
		t.Fatalf("offline probe = %s", OfflineProbeInterval)
	}
	if probeInterval(true) != LiveProbeInterval || probeInterval(false) != OfflineProbeInterval {
		t.Fatal("probeInterval did not follow liveness")
	}
	if LiveProbeInterval <= 2*time.Second || OfflineProbeInterval <= 2*time.Second {
		t.Fatal("a probe still runs every 2 seconds or faster")
	}
}

func TestDialbackIntervalFollowsCandidateLiveness(t *testing.T) {
	router := &Libp2pDHTRouter{}
	if router.dialbackInterval() != OfflineProbeInterval {
		t.Fatalf("no candidate = %s", router.dialbackInterval())
	}
}
