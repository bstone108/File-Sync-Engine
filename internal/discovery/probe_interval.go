package discovery

import "time"

// LiveProbeInterval is how often a connected peer, or a candidate that is
// being verified, is probed. Nothing in this process probes faster than this.
const LiveProbeInterval = 5 * time.Second

// OfflineProbeInterval is how often an unreachable peer or path is probed.
const OfflineProbeInterval = 60 * time.Second

// probeInterval selects the cadence for one liveness check.
func probeInterval(live bool) time.Duration {
	if live {
		return LiveProbeInterval
	}
	return OfflineProbeInterval
}
