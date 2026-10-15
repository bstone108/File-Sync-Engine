package apicontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filesyncengine/internal/api"
	"filesyncengine/internal/config"
	"filesyncengine/internal/configsync"
	"filesyncengine/internal/discovery"
	"filesyncengine/internal/link"
	"filesyncengine/internal/linkstats"
)

// ConfigSyncResponse is the redacted view returned to a GUI.
type ConfigSyncResponse struct {
	GroupID         string                         `json:"groupId"`
	LocalInstanceID string                         `json:"localInstanceId"`
	Instances       []configsync.InstanceView      `json:"instances"`
	Onboarding      []configsync.OnboardingCommand `json:"onboarding,omitempty"`
}

// ConfigSyncMergeResponse is exchanged between engines. It carries the replica
// and the same instance list the GUI renders.
type ConfigSyncMergeResponse struct {
	ConfigSyncResponse
	Replica configsync.Replica `json:"replica"`
}

type configSyncEditRequest struct {
	TargetInstanceID string `json:"targetInstanceId"`
	Path             string `json:"path"`
	Value            any    `json:"value"`
	Deleted          bool   `json:"deleted"`
}

type configSyncMergeRequest struct {
	Replica configsync.Replica `json:"replica"`
}

type onboardingRequest struct {
	TargetInstanceID string `json:"targetInstanceId"`
	Action           string `json:"action"`
	Code             string `json:"code"`
}

// OnboardingResponse is the GUI view after queueing or finishing one
// onboarding action for the selected instance.
type OnboardingResponse struct {
	ConfigSyncResponse
	Command configsync.OnboardingCommand `json:"command"`
}

// HandleConfigSync returns linked instances for the GUI. The local engine is first.
func HandleConfigSync(configPath string) (ConfigSyncResponse, error) {
	store, replica, err := loadPublishedReplica(configPath)
	if err != nil {
		return ConfigSyncResponse{}, err
	}
	if err := executePendingOnboarding(context.Background(), configPath, &replica); err != nil {
		return ConfigSyncResponse{}, err
	}
	if err := store.Save(replica); err != nil {
		return ConfigSyncResponse{}, err
	}
	return responseFrom(replica), nil
}

// HandleConfigSyncEdit queues a field change for one linked engine.
func HandleConfigSyncEdit(configPath string, raw json.RawMessage) (ConfigSyncResponse, error) {
	var req configSyncEditRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return ConfigSyncResponse{}, fmt.Errorf("invalid config sync edit")
	}
	if req.TargetInstanceID == "" || req.Path == "" {
		return ConfigSyncResponse{}, fmt.Errorf("targetInstanceId and path are required")
	}
	store, err := openStore(configPath)
	if err != nil {
		return ConfigSyncResponse{}, err
	}
	replica, err := store.Update(func(replica *configsync.Replica) error {
		edit, err := replica.QueueEdit(req.TargetInstanceID, req.Path, req.Value, req.Deleted, time.Now().UnixMilli())
		if err != nil {
			return err
		}
		if edit.Status == configsync.StatusApplied {
			return applyOwnRecord(configPath, *replica)
		}
		return nil
	})
	if err != nil {
		return ConfigSyncResponse{}, err
	}
	return responseFrom(replica), nil
}

// HandleConfigSyncMerge folds a peer replica into the local database.
func HandleConfigSyncMerge(configPath string, raw json.RawMessage) (ConfigSyncMergeResponse, error) {
	var req configSyncMergeRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return ConfigSyncMergeResponse{}, fmt.Errorf("invalid config sync replica")
	}
	store, local, err := loadPublishedReplica(configPath)
	if err != nil {
		return ConfigSyncMergeResponse{}, err
	}
	if req.Replica.GroupID != "" && local.GroupID != "" && req.Replica.GroupID != local.GroupID {
		return ConfigSyncMergeResponse{}, fmt.Errorf("config sync group mismatch")
	}
	merged, stats := configsync.Merge(local, req.Replica)
	if stats.EditsApplied > 0 {
		if err := applyOwnRecord(configPath, merged); err != nil {
			return ConfigSyncMergeResponse{}, err
		}
	}
	if err := executePendingOnboarding(context.Background(), configPath, &merged); err != nil {
		return ConfigSyncMergeResponse{}, err
	}
	if err := store.Save(merged); err != nil {
		return ConfigSyncMergeResponse{}, err
	}
	view := responseFrom(merged)
	return ConfigSyncMergeResponse{ConfigSyncResponse: view, Replica: merged}, nil
}

// ExchangeConfigSync pushes the local replica to a paired engine and saves the
// merged result. Pending edits for engines this peer cannot dial directly still
// travel inside whichever replica is online.
func ExchangeConfigSync(ctx context.Context, configPath string, peer config.PeerConfig) error {
	store, local, err := loadPublishedReplica(configPath)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(configSyncMergeRequest{Replica: local})
	if err != nil {
		return err
	}
	body, _, err := link.PostJSON(ctx, peer, "/v1/config-sync/merge", payload)
	if err != nil {
		return err
	}
	var remote ConfigSyncMergeResponse
	if err := json.Unmarshal(body, &remote); err != nil {
		return err
	}
	merged, _ := configsync.Merge(local, remote.Replica)
	if err := executePendingOnboarding(ctx, configPath, &merged); err != nil {
		return err
	}
	return store.Save(merged)
}

func executePendingOnboarding(ctx context.Context, configPath string, replica *configsync.Replica) error {
	pending := make([]string, 0)
	for _, command := range replica.Onboarding {
		if command.TargetInstanceID == replica.InstanceID && command.Status == configsync.OnboardingPending {
			pending = append(pending, command.ID)
		}
	}
	now := time.Now().UnixMilli()
	for _, id := range pending {
		command, ok := onboardingByID(*replica, id)
		if !ok {
			continue
		}
		var err error
		switch command.Action {
		case configsync.OnboardingIssue:
			issued, issueErr := HandleIssuePairCode(configPath)
			if issueErr != nil {
				_, err = replica.CompleteOnboarding(id, configsync.OnboardingFailed, "", "", onboardingFailure(issueErr, command.Code), now)
			} else {
				_, err = replica.CompleteOnboarding(id, configsync.OnboardingDone, issued.Code, issued.ExpiresAt.UTC().Format(time.RFC3339), "Pairing code is ready. It can be used once.", now)
			}
		case configsync.OnboardingAccept:
			response, redeemErr := HandleRedeemPairCode(ctx, configPath, api.PairCodeRedeemRequest{Code: command.Code})
			if redeemErr != nil {
				_, err = replica.CompleteOnboarding(id, configsync.OnboardingFailed, "", "", onboardingFailure(redeemErr, command.Code), now)
			} else {
				message := response.Message
				if message == "" {
					message = "paired by code"
				}
				_, err = replica.CompleteOnboarding(id, configsync.OnboardingDone, "", "", message, now)
			}
		default:
			_, err = replica.CompleteOnboarding(id, configsync.OnboardingFailed, "", "", "unsupported onboarding action", now)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func onboardingByID(replica configsync.Replica, id string) (configsync.OnboardingCommand, bool) {
	for _, command := range replica.Onboarding {
		if command.ID == id {
			return command, true
		}
	}
	return configsync.OnboardingCommand{}, false
}

func onboardingFailure(err error, code string) string {
	message := err.Error()
	if code != "" {
		message = strings.ReplaceAll(message, code, "the pairing code")
	}
	return message
}

func responseFrom(replica configsync.Replica) ConfigSyncResponse {
	views := replica.Instances()
	total := linkstats.Default.Total()
	for i := range views {
		if views[i].Local {
			views[i].AverageBytesPerSecond = total.AverageBytesPerSecond
			views[i].PendingBytes = total.PendingBytes
			continue
		}
		sample := linkstats.Default.Get(views[i].InstanceID)
		if sample == (linkstats.Sample{}) {
			sample = linkstats.Default.Get(views[i].Label)
		}
		views[i].AverageBytesPerSecond = sample.AverageBytesPerSecond
		views[i].PendingBytes = sample.PendingBytes
	}
	return ConfigSyncResponse{
		GroupID:         replica.GroupID,
		LocalInstanceID: replica.InstanceID,
		Instances:       views,
		Onboarding:      append([]configsync.OnboardingCommand(nil), replica.Onboarding...),
	}
}

// HandleOnboarding queues pairing-code issue or accept for one linked instance.
// The local instance runs it immediately. Any other instance keeps the command
// pending so the mesh can deliver it, and that instance runs it when it is online.
func HandleOnboarding(ctx context.Context, configPath string, raw json.RawMessage) (OnboardingResponse, error) {
	var req onboardingRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return OnboardingResponse{}, fmt.Errorf("invalid onboarding request")
	}
	store, err := openStore(configPath)
	if err != nil {
		return OnboardingResponse{}, err
	}
	replica, err := store.Update(func(replica *configsync.Replica) error {
		_, err := replica.QueueOnboarding(req.TargetInstanceID, req.Action, req.Code, time.Now().UnixMilli())
		return err
	})
	if err != nil {
		return OnboardingResponse{}, err
	}
	commandID := replica.Onboarding[len(replica.Onboarding)-1].ID
	if req.TargetInstanceID == replica.InstanceID {
		replica, err = store.Update(func(replica *configsync.Replica) error {
			return executePendingOnboarding(ctx, configPath, replica)
		})
		if err != nil {
			return OnboardingResponse{}, err
		}
	}
	command, ok := onboardingByID(replica, commandID)
	if !ok {
		return OnboardingResponse{}, fmt.Errorf("onboarding command was not found")
	}
	return OnboardingResponse{ConfigSyncResponse: responseFrom(replica), Command: command}, nil
}

func openStore(configPath string) (*configsync.Store, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return nil, err
	}
	if cfg.NodeName == "" {
		return nil, fmt.Errorf("nodeName is required for config sync")
	}
	path := filepath.Join(filepath.Dir(configPath), "config-sync.json")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		id, err := configsync.NewInstanceID()
		if err != nil {
			return nil, err
		}
		return configsync.Open(path, identityGroupID(cfg), id)
	} else if err != nil {
		return nil, err
	}
	return configsync.Open(path, identityGroupID(cfg), "")
}

func loadPublishedReplica(configPath string) (*configsync.Store, configsync.Replica, error) {
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return nil, configsync.Replica{}, err
	}
	store, err := openStore(configPath)
	if err != nil {
		return nil, configsync.Replica{}, err
	}
	replica, err := store.Load()
	if err != nil {
		return nil, configsync.Replica{}, err
	}
	announcement := discovery.CurrentHelperAnnouncement(discovery.SharedPeerID(), discovery.SharedListenAddrs())
	if _, err := replica.PublishConnection(configsync.Connection{
		InstanceID:  replica.InstanceID,
		Label:       cfg.NodeName,
		APIURL:      cfg.API.AdvertiseURL,
		DiscoveryID: cfg.Identity.PublicKey,
		DHTPeerID:   announcement.DHTPeerID,
		RelayAddrs:  announcement.RelayAddrs,
		MeshHelper:  announcement.Enabled,
		Reachable:   announcement.Reachable,
		Online:      true,
		LastSeen:    time.Now().UTC().Format(time.RFC3339),
	}, time.Now().UnixMilli()); err != nil {
		return nil, configsync.Replica{}, err
	}
	noteReplicaMembers(cfg, replica)
	return store, replica, nil
}

func noteReplicaMembers(cfg config.Config, replica configsync.Replica) {
	discovery.RefreshIdentityMembers(cfg)
	var ids []string
	var relays []string
	var exchanged []discovery.Peer
	for id, conn := range replica.Connections {
		if conn.DHTPeerID != "" && id != replica.InstanceID {
			ids = append(ids, conn.DHTPeerID)
		}
		if conn.MeshHelper {
			relays = append(relays, conn.RelayAddrs...)
		}
		addresses := append([]string(nil), conn.Addresses...)
		if conn.APIURL != "" {
			addresses = append(addresses, conn.APIURL)
		}
		if len(addresses) == 0 {
			continue
		}
		if conn.DHTPeerID != "" {
			exchanged = append(exchanged, discovery.Peer{ID: conn.DHTPeerID, Addresses: addresses})
		}
		for _, peer := range cfg.Peers {
			if peer.ID == "" {
				continue
			}
			if conn.DHTPeerID != "" && peer.DHTPeerID == conn.DHTPeerID {
				exchanged = append(exchanged, discovery.Peer{ID: peer.ID, Addresses: addresses})
			} else if conn.DiscoveryID != "" && peer.IdentityPublicKey == conn.DiscoveryID {
				exchanged = append(exchanged, discovery.Peer{ID: peer.ID, Addresses: addresses})
			}
		}
	}
	discovery.SetConnectionMembers(ids)
	discovery.SetIdentityRelays(relays)
	if len(exchanged) > 0 {
		discovery.NoteExchangedPeers(exchanged)
	}
}

// recordOwnSettings copies this engine's non-secret settings into its own
// replica record so linked GUIs can view them. It does not write any other
// instance's record.
func recordOwnSettings(configPath string, cfg config.Config) error {
	store, replica, err := loadPublishedReplica(configPath)
	if err != nil {
		return err
	}
	wall := time.Now().UnixMilli()
	for key, value := range ownSettings(cfg) {
		if _, err := replica.SetOwn(key, value, false, wall); err != nil {
			return err
		}
	}
	return store.Save(replica)
}

func ownSettings(cfg config.Config) map[string]any {
	settings := map[string]any{}
	if cfg.Logging.Level != "" {
		settings["logging.level"] = string(cfg.Logging.Level)
	}
	if cfg.Logging.Output != "" {
		settings["logging.output"] = cfg.Logging.Output
	}
	if cfg.Transfer.SendBytesPerSecond != 0 {
		settings["transfer.sendBytesPerSecond"] = cfg.Transfer.SendBytesPerSecond
	}
	if cfg.Transfer.ReceiveBytesPerSecond != 0 {
		settings["transfer.receiveBytesPerSecond"] = cfg.Transfer.ReceiveBytesPerSecond
	}
	return settings
}

// applyOwnRecord writes this engine's own record into the local config file.
// Records stored for other instances are ignored.
func applyOwnRecord(configPath string, replica configsync.Replica) error {
	field, ok := replica.OwnRecord().Fields["logging.level"]
	if !ok || field.Deleted {
		return nil
	}
	level, ok := field.Value.(string)
	if !ok || level == "" {
		return nil
	}
	switch config.LogLevel(level) {
	case config.LogLevelDebug, config.LogLevelInfo, config.LogLevelWarn, config.LogLevelError, config.LogLevelOff:
	default:
		return fmt.Errorf("logging.level %q is not a setting this engine applies", level)
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return err
	}
	if string(cfg.Logging.Level) == level {
		return nil
	}
	cfg.Logging.Level = config.LogLevel(level)
	if err := cfg.Validate(); err != nil {
		return err
	}
	return writePatchedConfig(configPath, cfg)
}

func identityGroupID(cfg config.Config) string {
	for _, group := range cfg.Identity.Groups {
		if group.Enabled && group.ID != "" {
			return group.ID
		}
	}
	return "primary"
}
