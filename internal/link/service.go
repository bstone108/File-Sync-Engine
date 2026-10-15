package link

import (
	"context"
	"crypto/subtle"
	"errors"
	"sync"
	"time"

	"filesyncengine/internal/peeridentity"
)

type issuedCode struct {
	code    string
	record  Record
	payload []byte
	expires time.Time
	used    bool
}

// Service issues and redeems short-lived single-use pairing codes.
type Service struct {
	dir    Directory
	newDir func() (Directory, string, error)
	mu     sync.Mutex
	codes  map[string]*issuedCode
	ttl    time.Duration
	now    func() time.Time
	peerID string
	relays []string
}

func NewService(dir Directory) *Service {
	return &Service{
		dir:   dir,
		codes: map[string]*issuedCode{},
		ttl:   CodeTTL,
		now:   time.Now,
	}
}

// SetDirectoryFactory lazily builds a DHT directory on the first code issue
// or lookup. Tests inject a directory directly and do not use the factory.
func (s *Service) SetDirectoryFactory(factory func() (Directory, string, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.newDir = factory
}

func (s *Service) SetPeerID(peerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.peerID = peerID
}

func (s *Service) SetRelays(relays []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.relays = append([]string(nil), relays...)
}

func (s *Service) PeerID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peerID
}

func (s *Service) Relays() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.relays...)
}

// Issue publishes a code-bound record and remembers it until it is redeemed or expires.
func (s *Service) Issue(ctx context.Context, identity peeridentity.Identity, profile Profile) (string, time.Time, error) {
	dir, err := s.directory(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	code, err := NewCode()
	if err != nil {
		return "", time.Time{}, err
	}
	expires := s.now().UTC().Add(s.ttl).Truncate(time.Second)
	if profile.DHTPeerID == "" {
		profile.DHTPeerID = s.PeerID()
	}
	if len(profile.Relays) == 0 {
		profile.Relays = s.Relays()
	}
	record, payload, err := Seal(code, identity, profile, expires)
	if err != nil {
		return "", time.Time{}, err
	}
	normalized := NormalizeCode(code)
	s.mu.Lock()
	s.codes[normalized] = &issuedCode{code: normalized, record: record, payload: payload, expires: expires}
	s.mu.Unlock()
	if err := dir.Publish(ctx, Namespace(normalized), payload); err != nil {
		s.mu.Lock()
		delete(s.codes, normalized)
		s.mu.Unlock()
		return "", time.Time{}, err
	}
	return code, expires, nil
}

// Lookup reads DHT payloads for the code and returns the one the code authenticates.
// Records published by a different key under the same namespace are ignored.
func (s *Service) Lookup(ctx context.Context, code string) (Record, error) {
	dir, err := s.directory(ctx)
	if err != nil {
		return Record{}, err
	}
	normalized := NormalizeCode(code)
	if !validCode(normalized) {
		return Record{}, errors.New("pairing code is invalid")
	}
	payloads, err := dir.Lookup(ctx, Namespace(normalized))
	if err != nil {
		return Record{}, err
	}
	var last error
	for _, payload := range payloads {
		record, openErr := Open(normalized, payload, s.now())
		if openErr == nil {
			return record, nil
		}
		last = openErr
	}
	if last != nil && len(payloads) > 0 {
		return Record{}, last
	}
	return Record{}, errors.New("pairing code was not found")
}

// Claim burns a code and returns the published record. A second claim fails.
func (s *Service) Claim(ctx context.Context, claim ClaimRequest) (Record, error) {
	if err := VerifyClaim(claim); err != nil {
		return Record{}, err
	}
	normalized := NormalizeCode(claim.Code)
	if !validCode(normalized) {
		return Record{}, errors.New("pairing code is invalid")
	}
	s.mu.Lock()
	issued := s.codes[normalized]
	if issued == nil {
		s.mu.Unlock()
		return Record{}, errors.New("pairing code was not found")
	}
	if subtle.ConstantTimeCompare([]byte(issued.code), []byte(normalized)) != 1 {
		s.mu.Unlock()
		return Record{}, errors.New("pairing code was not found")
	}
	if issued.used {
		s.mu.Unlock()
		return Record{}, errors.New("pairing code already used")
	}
	if !s.now().Before(issued.expires) {
		s.mu.Unlock()
		return Record{}, errors.New("pairing code expired")
	}
	issued.used = true
	record := issued.record
	s.mu.Unlock()
	if dir, err := s.directory(ctx); err == nil {
		_ = dir.Unpublish(ctx, Namespace(normalized))
	}
	return record, nil
}

func (s *Service) directory(ctx context.Context) (Directory, error) {
	s.mu.Lock()
	dir := s.dir
	factory := s.newDir
	s.mu.Unlock()
	if dir != nil {
		return dir, nil
	}
	if factory == nil {
		return nil, errors.New("pairing codes require DHT discovery; paste a pairing package instead")
	}
	created, peerID, err := factory()
	if err != nil {
		return nil, err
	}
	if created == nil {
		return nil, errors.New("pairing codes require DHT discovery; paste a pairing package instead")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dir == nil {
		s.dir = created
		if s.peerID == "" {
			s.peerID = peerID
		}
	}
	_ = ctx
	return s.dir, nil
}
