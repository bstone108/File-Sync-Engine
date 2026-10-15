package api

import (
	"context"
	"encoding/json"
	"net/http"
)

// ConfigSyncGetHandler returns the local replica summary for the GUI.
type ConfigSyncGetHandler func(context.Context) (any, error)

// ConfigSyncEditHandler queues one non-secret field edit.
type ConfigSyncEditHandler func(context.Context, json.RawMessage) (any, error)

// ConfigSyncMergeHandler folds a peer replica into the local database and
// returns the merged replica so the peer can carry edits for offline engines.
type ConfigSyncMergeHandler func(context.Context, json.RawMessage) (any, error)

func (s *Server) handleConfigSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	handler := s.configSyncGetHandler
	s.mu.RUnlock()
	if handler == nil {
		http.Error(w, "config sync handler is not configured", http.StatusServiceUnavailable)
		return
	}
	response, err := handler(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, response)
}

func (s *Server) handleConfigSyncEdit(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	handler := s.configSyncEditHandler
	s.mu.RUnlock()
	s.handleConfigSyncPost(w, r, handler)
}

func (s *Server) handleConfigSyncMerge(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	handler := s.configSyncMergeHandler
	s.mu.RUnlock()
	s.handleConfigSyncPost(w, r, handler)
}

func (s *Server) handleConfigSyncPost(w http.ResponseWriter, r *http.Request, handler func(rctx context.Context, raw json.RawMessage) (any, error)) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if handler == nil {
		http.Error(w, "config sync handler is not configured", http.StatusServiceUnavailable)
		return
	}
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, "invalid config sync body", http.StatusBadRequest)
		return
	}
	response, err := handler(r.Context(), raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, response)
}
