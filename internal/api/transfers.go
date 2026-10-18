package api

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"filesyncengine/internal/transferprogress"
)

const defaultTransferHistoryLimit = 50

// TransferReadModel is a bounded, in-memory view of transfer-pass lifecycle
// events plus live byte progress. Rates stay unavailable until a transfer is
// actually moving; an empty pass does not invent them.
type TransferReadModel struct {
	Active                []TransferReadModelItem `json:"active"`
	History               []TransferReadModelItem `json:"history"`
	LiveRatesAvailable    bool                    `json:"liveRatesAvailable"`
	ByteProgressAvailable bool                    `json:"byteProgressAvailable"`
}

type TransferReadModelItem struct {
	FolderID       string    `json:"folderId"`
	PeerID         string    `json:"peerId,omitempty"`
	Status         string    `json:"status"`
	StartedAt      time.Time `json:"startedAt,omitempty"`
	FinishedAt     time.Time `json:"finishedAt,omitempty"`
	EventType      string    `json:"eventType"`
	Message        string    `json:"message,omitempty"`
	Writes         int       `json:"writes,omitempty"`
	BlocksFetched  int       `json:"blocksFetched,omitempty"`
	BlocksReused   int       `json:"blocksReused,omitempty"`
	RoutePath      string    `json:"routePath,omitempty"`
	ReceivedBytes  int64     `json:"receivedBytes,omitempty"`
	RemainingBytes int64     `json:"remainingBytes,omitempty"`
	BytesPerSecond int64     `json:"bytesPerSecond,omitempty"`
}

func (s *Server) handleTransfers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := defaultTransferHistoryLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			http.Error(w, "invalid transfer history limit", http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	s.mu.RLock()
	events := append([]Event(nil), s.events...)
	s.mu.RUnlock()
	model := buildTransferReadModel(events, limit)
	applyLiveTransferProgress(&model)
	writeJSON(w, model)
}

func applyLiveTransferProgress(model *TransferReadModel) {
	snaps := transferprogress.ActiveSnapshots()
	if len(snaps) == 0 || model == nil {
		return
	}
	model.ByteProgressAvailable = true
	seen := map[string]struct{}{}
	for i := range model.Active {
		snap, ok := snaps[model.Active[i].PeerID]
		if !ok {
			continue
		}
		fillTransferProgress(&model.Active[i], snap)
		seen[model.Active[i].PeerID] = struct{}{}
		if snap.BytesPerSecond > 0 {
			model.LiveRatesAvailable = true
		}
	}
	for id, snap := range snaps {
		if _, ok := seen[id]; ok {
			continue
		}
		item := TransferReadModelItem{
			PeerID: id, Status: "active", EventType: "transfer.progress", StartedAt: snap.Updated,
		}
		fillTransferProgress(&item, snap)
		model.Active = append(model.Active, item)
		if snap.BytesPerSecond > 0 {
			model.LiveRatesAvailable = true
		}
	}
	sort.Slice(model.Active, func(i, j int) bool {
		if model.Active[i].StartedAt.Equal(model.Active[j].StartedAt) {
			return model.Active[i].PeerID < model.Active[j].PeerID
		}
		return model.Active[i].StartedAt.Before(model.Active[j].StartedAt)
	})
}

func fillTransferProgress(item *TransferReadModelItem, snap transferprogress.Snapshot) {
	item.ReceivedBytes = snap.ReceivedBytes
	item.RemainingBytes = snap.RemainingBytes
	item.BytesPerSecond = snap.BytesPerSecond
}

func buildTransferReadModel(events []Event, limit int) TransferReadModel {
	active := map[string]TransferReadModelItem{}
	history := make([]TransferReadModelItem, 0, limit)
	for _, event := range events {
		status, transferEvent := transferEventStatus(event.Type)
		if !transferEvent {
			continue
		}
		key := event.FolderID + "\x00" + event.PeerID
		writes, fetched, reused, route := transferCounts(event.Message)
		item := TransferReadModelItem{
			FolderID: event.FolderID, PeerID: event.PeerID, Status: status, EventType: event.Type, Message: event.Message,
			Writes: writes, BlocksFetched: fetched, BlocksReused: reused, RoutePath: route,
		}
		if status == "active" {
			item.StartedAt = event.Time
			active[key] = item
			continue
		}
		if current, ok := active[key]; ok {
			item.StartedAt = current.StartedAt
			delete(active, key)
		}
		item.FinishedAt = event.Time
		history = append(history, item)
	}
	activeItems := make([]TransferReadModelItem, 0, len(active))
	for _, item := range active {
		activeItems = append(activeItems, item)
	}
	sort.Slice(activeItems, func(i, j int) bool { return activeItems[i].StartedAt.Before(activeItems[j].StartedAt) })
	for left, right := 0, len(history)-1; left < right; left, right = left+1, right-1 {
		history[left], history[right] = history[right], history[left]
	}
	if len(history) > limit {
		history = history[:limit]
	}
	return TransferReadModel{Active: activeItems, History: history}
}

func transferCounts(message string) (writes, fetched, reused int, route string) {
	for _, field := range strings.Fields(message) {
		key, val, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch key {
		case "writes":
			writes, _ = strconv.Atoi(val)
		case "blocksFetched":
			fetched, _ = strconv.Atoi(val)
		case "blocksReused":
			reused, _ = strconv.Atoi(val)
		case "routePath":
			route = val
		}
	}
	return writes, fetched, reused, route
}

func transferEventStatus(eventType string) (string, bool) {
	switch eventType {
	case "sync.started", "peer.sync.started":
		return "active", true
	case "sync.finished", "peer.sync.finished":
		return "completed", true
	case "sync.error", "peer.sync.error":
		return "failed", true
	case "transfer.paused":
		return "paused", true
	case "transfer.cancelled":
		return "cancelled", true
	default:
		return "", false
	}
}
