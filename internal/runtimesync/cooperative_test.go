package runtimesync

import (
	"strings"
	"testing"

	"filesyncengine/internal/api"
	"filesyncengine/internal/peerpullplan"
	"filesyncengine/internal/peersync"
	"filesyncengine/internal/routing"
)

func TestPeerPullsFromPlansKeepsOnlyRuntimeRoutingFields(t *testing.T) {
	pulls := PeerPullsFromPlans([]peerpullplan.Pull{
		{PeerID: "peer-a", BaseURL: "https://peer-a.example", APIKey: "secret", FolderID: "docs", LocalPath: "/sync/docs", ReceiveBytesPerSecond: 4096, Path: routing.DirectPath, Network: routing.LocalNetwork, RouteReason: routing.ReasonDirectPreferred, BlockSources: []peersync.BlockSource{{PeerID: "peer-a", BaseURL: "https://peer-a.example", APIKey: "secret"}}},
		{PeerID: "peer-b", BaseURL: "https://peer-b.example", APIKey: "other-secret", FolderID: "docs", LocalPath: "/sync/docs", Path: routing.RelayPath, Network: routing.WANNetwork},
	})

	if len(pulls) != 2 {
		t.Fatalf("expected two runtime peer pulls, got %#v", pulls)
	}
	if pulls[0] != (PeerPull{PeerID: "peer-a", Path: routing.DirectPath, Network: routing.LocalNetwork}) {
		t.Fatalf("unexpected first runtime pull projection: %#v", pulls[0])
	}
	if pulls[1] != (PeerPull{PeerID: "peer-b", Path: routing.RelayPath, Network: routing.WANNetwork}) {
		t.Fatalf("unexpected second runtime pull projection: %#v", pulls[1])
	}
}

func TestPlanCooperativeBlockFetchesAssignsOneWANFetcherForSameLocalPeers(t *testing.T) {
	plans := PlanCooperativeBlockFetches("docs", []PeerPull{
		{PeerID: "lan-b", Network: routing.LocalNetwork, Path: routing.DirectPath},
		{PeerID: "lan-c", Network: routing.LocalNetwork, Path: routing.DirectPath},
		{PeerID: "wan-d", Network: routing.WANNetwork, Path: routing.DirectPath},
	})

	if len(plans) != 1 {
		t.Fatalf("expected one cooperative plan, got %d", len(plans))
	}
	plan := plans[0]
	if plan.BlockID != "docs:live-transfer-pass" || plan.Reason != routing.ReasonCooperativeLocalRedistribution {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if len(plan.Assignments) != 2 {
		t.Fatalf("expected assignments for two LAN peers, got %#v", plan.Assignments)
	}
	if plan.Assignments[0].Action != routing.CooperativeFetchWAN || plan.Assignments[0].PeerID != "lan-b" {
		t.Fatalf("expected deterministic first LAN peer to fetch from WAN, got %#v", plan.Assignments[0])
	}
	if plan.Assignments[1].Action != routing.CooperativeFetchLocal || plan.Assignments[1].PeerID != "lan-c" || plan.Assignments[1].SourcePeerID != "lan-b" {
		t.Fatalf("expected second LAN peer to reuse from first peer, got %#v", plan.Assignments[1])
	}
}

func TestPlanCooperativeBlockFetchesIgnoresDuplicateAndNonDirectPeers(t *testing.T) {
	plans := PlanCooperativeBlockFetches("docs", []PeerPull{
		{PeerID: "lan-b", Network: routing.LocalNetwork, Path: routing.DirectPath},
		{PeerID: "lan-b", Network: routing.LocalNetwork, Path: routing.DirectPath},
		{PeerID: "relay-c", Network: routing.LocalNetwork, Path: routing.RelayPath},
	})

	if len(plans) != 0 {
		t.Fatalf("expected no cooperative plan without two unique direct local peers, got %#v", plans)
	}
}

func TestCooperativeBlockFetchPlanMessageIsStableForStatusEvents(t *testing.T) {
	plan := PlanCooperativeBlockFetches("docs", []PeerPull{
		{PeerID: "lan-b", Network: routing.LocalNetwork, Path: routing.DirectPath},
		{PeerID: "lan-c", Network: routing.LocalNetwork, Path: routing.DirectPath},
	})[0]

	message := CooperativeBlockFetchPlanMessage(plan)
	for _, want := range []string{"cooperativeBlockFetch", "block=docs:live-transfer-pass", "reason=cooperative_local_redistribution", "wanFetchers=lan-b", "localReuse=lan-c<- lan-b"} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected message %q to contain %q", message, want)
		}
	}
}

func TestPublishCooperativeBlockFetchPlansPublishesRuntimeEvents(t *testing.T) {
	publisher := &recordingPublisher{}

	PublishCooperativeBlockFetchPlans(publisher, "docs", []PeerPull{
		{PeerID: "lan-b", Network: routing.LocalNetwork, Path: routing.DirectPath},
		{PeerID: "lan-c", Network: routing.LocalNetwork, Path: routing.DirectPath},
	})

	if len(publisher.events) != 1 {
		t.Fatalf("expected one published event, got %#v", publisher.events)
	}
	event := publisher.events[0]
	if event.Type != "transfer.cooperative_block_fetch.planned" || event.FolderID != "docs" {
		t.Fatalf("unexpected event envelope: %#v", event)
	}
	for _, want := range []string{"cooperativeBlockFetch", "block=docs:live-transfer-pass", "wanFetchers=lan-b", "localReuse=lan-c<- lan-b"} {
		if !strings.Contains(event.Message, want) {
			t.Fatalf("expected event message %q to contain %q", event.Message, want)
		}
	}
}

type recordingPublisher struct {
	events []api.Event
}

func (p *recordingPublisher) Publish(event api.Event) {
	p.events = append(p.events, event)
}
