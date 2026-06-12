package config

import "testing"

func TestEffectiveTransferLimitsUseLowestNonZeroCapAcrossPeers(t *testing.T) {
	limits := EffectiveTransferLimits(
		TransferConfig{SendBytesPerSecond: 1000, ReceiveBytesPerSecond: 2000},
		PeerConfig{ID: "remote", SendBytesPerSecond: 700, ReceiveBytesPerSecond: 0},
		TransferConfig{SendBytesPerSecond: 900, ReceiveBytesPerSecond: 500},
		PeerConfig{ID: "local", SendBytesPerSecond: 0, ReceiveBytesPerSecond: 800},
	)

	if limits.SendBytesPerSecond != 700 {
		t.Fatalf("send cap = %d, want lowest non-zero of local send override and remote receive override", limits.SendBytesPerSecond)
	}
	if limits.ReceiveBytesPerSecond != 900 {
		t.Fatalf("receive cap = %d, want lowest non-zero of local receive global and remote send global", limits.ReceiveBytesPerSecond)
	}
}

func TestEffectiveTransferLimitsTreatZeroAsUnlimitedAndUseConfiguredFallbacks(t *testing.T) {
	limits := EffectiveTransferLimits(
		TransferConfig{SendBytesPerSecond: 0, ReceiveBytesPerSecond: 5000},
		PeerConfig{ID: "remote"},
		TransferConfig{SendBytesPerSecond: 0, ReceiveBytesPerSecond: 3000},
		PeerConfig{ID: "local", SendBytesPerSecond: 0, ReceiveBytesPerSecond: 0},
	)

	if limits.SendBytesPerSecond != 3000 {
		t.Fatalf("send cap = %d, want remote receive global when local send is unlimited", limits.SendBytesPerSecond)
	}
	if limits.ReceiveBytesPerSecond != 5000 {
		t.Fatalf("receive cap = %d, want local receive global when remote send is unlimited", limits.ReceiveBytesPerSecond)
	}
}

func TestEffectiveTransferLimitDetailsExplainRemoteThrottlingCauses(t *testing.T) {
	details := EffectiveTransferLimitDetails(
		TransferConfig{SendBytesPerSecond: 1000, ReceiveBytesPerSecond: 0},
		PeerConfig{ID: "remote", SendBytesPerSecond: 0, ReceiveBytesPerSecond: 700},
		TransferConfig{SendBytesPerSecond: 0, ReceiveBytesPerSecond: 600},
		PeerConfig{ID: "local", SendBytesPerSecond: 500, ReceiveBytesPerSecond: 0},
	)

	if details.Effective.SendBytesPerSecond != 600 || details.SendCause != "remote_receive" {
		t.Fatalf("send details = %+v, want remote receive throttling cause", details)
	}
	if details.Effective.ReceiveBytesPerSecond != 500 || details.ReceiveCause != "remote_send" {
		t.Fatalf("receive details = %+v, want remote send throttling cause", details)
	}
}

func TestEffectiveTransferLimitDetailsExplainUnlimitedCaps(t *testing.T) {
	details := EffectiveTransferLimitDetails(TransferConfig{}, PeerConfig{ID: "remote"}, TransferConfig{}, PeerConfig{ID: "local"})

	if details.Effective.SendBytesPerSecond != 0 || details.SendCause != "unlimited" {
		t.Fatalf("send details = %+v, want unlimited", details)
	}
	if details.Effective.ReceiveBytesPerSecond != 0 || details.ReceiveCause != "unlimited" {
		t.Fatalf("receive details = %+v, want unlimited", details)
	}
}
