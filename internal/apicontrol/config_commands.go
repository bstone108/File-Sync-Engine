package apicontrol

import (
	"fmt"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/configops"
)

// HandlePeerCommand applies an authenticated peer config command through the
// normal config mutation layer so daemon hot reload remains the adoption path.
func HandlePeerCommand(configPath string, req api.PeerCommandRequest) (api.PeerCommandResponse, error) {
	switch req.Action {
	case "add":
		if err := configops.AddPeer(configPath, req.ID, req.Endpoint); err != nil {
			return api.PeerCommandResponse{}, err
		}
	case "remove":
		if err := configops.RemovePeer(configPath, req.ID); err != nil {
			return api.PeerCommandResponse{}, err
		}
	case "update":
		if err := configops.UpdatePeer(configPath, req.ID, req.Endpoint); err != nil {
			return api.PeerCommandResponse{}, err
		}
	default:
		return api.PeerCommandResponse{}, fmt.Errorf("unsupported peer command action %q", req.Action)
	}
	return api.PeerCommandResponse{Action: req.Action, ID: req.ID, Status: "accepted", Message: fmt.Sprintf("peer %s accepted; daemon hot reload will adopt the config change", req.Action)}, nil
}

// HandleFolderCommand applies an authenticated folder config command through
// configops while keeping the response payload compact and non-secret.
func HandleFolderCommand(configPath string, req api.FolderCommandRequest) (api.FolderCommandResponse, error) {
	switch req.Action {
	case "add":
		if err := configops.AddFolder(configPath, req.ID, req.Path, req.Mode); err != nil {
			return api.FolderCommandResponse{}, err
		}
	case "remove":
		if err := configops.RemoveFolder(configPath, req.ID); err != nil {
			return api.FolderCommandResponse{}, err
		}
	case "update":
		if err := configops.UpdateFolder(configPath, req.ID, req.Path, req.Mode); err != nil {
			return api.FolderCommandResponse{}, err
		}
	default:
		return api.FolderCommandResponse{}, fmt.Errorf("unsupported folder command action %q", req.Action)
	}
	return api.FolderCommandResponse{Action: req.Action, ID: req.ID, Status: "accepted", Message: fmt.Sprintf("folder %s accepted; daemon hot reload will adopt the config change", req.Action)}, nil
}

// HandleDiscoveryCommand applies authenticated discovery config updates while
// copying slice fields from the API request before mutation.
func HandleDiscoveryCommand(configPath string, req api.DiscoveryCommandRequest) (api.DiscoveryCommandResponse, error) {
	if req.Action != "update" {
		return api.DiscoveryCommandResponse{}, fmt.Errorf("unsupported discovery command action %q", req.Action)
	}
	discovery := config.DiscoveryConfig{
		Disabled:          req.Disabled,
		DHT:               req.DHT,
		Local:             req.Local,
		DHTNamespace:      req.DHTNamespace,
		DHTBootstrapPeers: append([]string(nil), req.DHTBootstrapPeers...),
		NetworkHints: config.NetworkHintsConfig{
			LocalContainerGatewayIPs: append([]string(nil), req.NetworkHints.LocalContainerGatewayIPs...),
			LocalCIDRs:               append([]string(nil), req.NetworkHints.LocalCIDRs...),
			PublishedPortMappings:    append([]config.PublishedPortMappingConfig(nil), req.NetworkHints.PublishedPortMappings...),
		},
	}
	if err := configops.UpdateDiscovery(configPath, discovery); err != nil {
		return api.DiscoveryCommandResponse{}, err
	}
	return api.DiscoveryCommandResponse{Action: req.Action, Status: "accepted", Message: "discovery update accepted; daemon hot reload will adopt the config change"}, nil
}
