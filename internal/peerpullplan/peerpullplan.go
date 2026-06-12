package peerpullplan

import (
	"strings"

	"filesyncengine/internal/apistate"
	"filesyncengine/internal/config"
	"filesyncengine/internal/peersync"
	"filesyncengine/internal/routing"
)

// Pull is the daemon's planned manual HTTP peer pull for one folder.
type Pull struct {
	PeerID                string
	BaseURL               string
	APIKey                string
	FolderID              string
	LocalPath             string
	ReceiveBytesPerSecond int64
	Path                  routing.PathKind
	Network               routing.NetworkKind
	RouteReason           routing.SelectionReason
	BlockSources          []peersync.BlockSource
}

// Plan builds manual HTTP peer pulls for a receive-capable folder, selecting the
// best reachable endpoint class while preserving all reachable block sources so
// block-level fetches can still prefer direct/local sources per block.
func Plan(cfg config.Config, folderID string, sidecarObservations []routing.EndpointObservation) []Pull {
	folder, ok := findFolder(cfg, folderID)
	if !ok || (folder.Mode != config.ModeReceiveOnly && folder.Mode != config.ModeSendReceive) {
		return nil
	}
	candidates := []Pull{}
	blockSources := []peersync.BlockSource{}
	routingCandidates := []routing.CandidateSource{}
	networkHints := apistate.RoutingNetworkHints(cfg)
	for _, peer := range cfg.Peers {
		if peer.APIKey == "" {
			continue
		}
		for _, candidate := range apistate.PeerEndpointCandidates(peer, sidecarObservations, networkHints) {
			if candidate.ControlPlaneOnly || !candidate.Reachable || !strings.HasPrefix(candidate.Address, "http") {
				continue
			}
			limits := config.EffectiveTransferLimits(cfg.Transfer, peer, config.TransferConfig{}, config.PeerConfig{})
			candidates = append(candidates, Pull{PeerID: peer.ID, BaseURL: candidate.Address, APIKey: peer.APIKey, FolderID: folder.ID, LocalPath: folder.Path, ReceiveBytesPerSecond: limits.ReceiveBytesPerSecond, Path: candidate.Path, Network: candidate.Network})
			blockSources = append(blockSources, peersync.BlockSource{PeerID: peer.ID, BaseURL: candidate.Address, APIKey: peer.APIKey, Path: candidate.Path, Network: candidate.Network, Reachable: true})
			routingCandidates = append(routingCandidates, routing.CandidateSource{PeerID: peer.ID, ContentID: folder.ID, Path: candidate.Path, Network: candidate.Network, Reachable: true})
		}
	}
	choice, ok := routing.ChooseTransferSource(routing.SourceSelectionRequest{ContentID: folder.ID, Candidates: routingCandidates})
	if !ok {
		return nil
	}
	pulls := []Pull{}
	for _, candidate := range candidates {
		if choice.Path == routing.DirectPath && candidate.Path != routing.DirectPath {
			continue
		}
		if choice.Network == routing.LocalNetwork && candidate.Network != routing.LocalNetwork {
			continue
		}
		candidate.RouteReason = choice.Reason
		candidate.BlockSources = blockSources
		pulls = append(pulls, candidate)
	}
	return pulls
}

func findFolder(cfg config.Config, folderID string) (config.FolderConfig, bool) {
	for _, folder := range cfg.Folders {
		if folder.ID == folderID {
			return folder, true
		}
	}
	return config.FolderConfig{}, false
}
