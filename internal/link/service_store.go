package link

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type storedCode struct {
	Code        string    `json:"code"`
	Expires     time.Time `json:"expires"`
	Used        bool      `json:"used"`
	Unpublished bool      `json:"unpublished"`
	Payload     []byte    `json:"payload"`
	Record      Record    `json:"record"`
}

// SetStorePath loads pairing codes from appdata and saves them until they are
// used or expired. The daemon passes the file next to the config, which in
// Docker is /config/pairing-codes.json on the appdata volume, not /tmp.
// Loading the file is not enough for another engine to redeem the code: the
// local listener and the DHT provider record are memory-only and are
// republished by Warmup.
func (s *Service) SetStorePath(path string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.storePath = path
	s.mu.Unlock()
	s.loadCodes()
}

func (s *Service) loadCodes() {
	if s == nil {
		return
	}
	s.mu.Lock()
	path := s.storePath
	s.mu.Unlock()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var stored []storedCode
	if err := json.Unmarshal(data, &stored); err != nil {
		return
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.codes == nil {
		s.codes = map[string]*issuedCode{}
	}
	for _, item := range stored {
		if item.Code == "" || item.Used || !now.Before(item.Expires) {
			continue
		}
		s.codes[item.Code] = &issuedCode{
			code:        item.Code,
			record:      item.Record,
			payload:     item.Payload,
			expires:     item.Expires,
			used:        item.Used,
			unpublished: item.Unpublished,
		}
	}
}

func (s *Service) saveCodes() {
	if s == nil {
		return
	}
	s.mu.Lock()
	path := s.storePath
	items := make([]storedCode, 0, len(s.codes))
	for _, issued := range s.codes {
		if issued == nil {
			continue
		}
		items = append(items, storedCode{
			Code:        issued.code,
			Expires:     issued.expires,
			Used:        issued.used,
			Unpublished: issued.unpublished,
			Payload:     append([]byte(nil), issued.payload...),
			Record:      issued.record,
		})
	}
	s.mu.Unlock()
	if path == "" {
		return
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}
