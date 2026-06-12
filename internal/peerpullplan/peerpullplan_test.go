package peerpullplan

import (
	"testing"

	"filesyncengine/internal/config"
	"filesyncengine/internal/routing"
)

func TestPlanUsesLiveSidecarEndpointCandidateOverConfiguredWAN(t *testing.T) {
	cfg := config.Config{
		Discovery: config.DiscoveryConfig{NetworkHints: config.NetworkHintsConfig{PublishedPortMappings: []config.PublishedPortMappingConfig{{HostIP: "172.18.0.1", HostPort: 32200}}}},
		Folders:   []config.FolderConfig{{ID: "docs", Path: "/srv/docs", Mode: config.ModeReceiveOnly}},
		Peers: []config.PeerConfig{{
			ID:        "container-peer",
			APIKey:    "container-secret",
			Endpoints: []config.EndpointConfig{{Kind: "manual", Address: "https://203.0.113.10:22000"}},
		}},
	}

	pulls := Plan(cfg, "docs", []routing.EndpointObservation{{
		PeerID:    "container-peer",
		Address:   "http://172.18.0.1:32200",
		Reachable: true,
		Path:      routing.DirectPath,
	}})

	if len(pulls) != 1 || pulls[0].BaseURL != "http://172.18.0.1:32200" {
		t.Fatalf("live sidecar candidate should be selected before configured WAN endpoint: %+v", pulls)
	}
	if pulls[0].Network != routing.LocalNetwork || pulls[0].RouteReason != routing.ReasonLocalPreferred {
		t.Fatalf("expected live sidecar candidate to be local preferred, got network=%q reason=%q", pulls[0].Network, pulls[0].RouteReason)
	}
	if len(pulls[0].BlockSources) != 2 {
		t.Fatalf("expected selected pull to retain configured and sidecar block sources, got %+v", pulls[0].BlockSources)
	}
}
