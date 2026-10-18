package link

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"
	"sync"
	"time"

	"filesyncengine/internal/peeridentity"
	"filesyncengine/internal/structuredlog"
)

type issuedCode struct {
	code        string
	record      Record
	payload     []byte
	expires     time.Time
	used        bool
	unpublished bool
}

// Service issues and redeems short-lived single-use pairing codes.
type Service struct {
	dir         Directory
	newDir      func() (Directory, string, error)
	mu          sync.Mutex
	createMu    sync.Mutex
	codes       map[string]*issuedCode
	ttl         time.Duration
	now         func() time.Time
	peerID      string
	relays      []string
	storePath   string
	unpublishWG sync.WaitGroup
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

// SetTTL overrides how long an unused code remains redeemable.
// A non-positive duration keeps the one-hour default.
func (s *Service) SetTTL(ttl time.Duration) {
	if s == nil || ttl <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ttl = ttl
}

// Issue publishes a code-bound record and remembers it until it is redeemed or expires.
func (s *Service) Issue(ctx context.Context, identity peeridentity.Identity, profile Profile) (string, time.Time, error) {
	s.Sweep(ctx)
	dir, err := s.directory(ctx)
	if err != nil {
		return "", time.Time{}, err
	}
	code, err := NewCode()
	if err != nil {
		return "", time.Time{}, err
	}
	s.mu.Lock()
	ttl := s.ttl
	s.mu.Unlock()
	expires := s.now().UTC().Add(ttl).Truncate(time.Second)
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
		s.saveCodes()
		return "", time.Time{}, err
	}
	s.saveCodes()
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

// Claim burns a code and returns the published record. The burn happens
// before the record is returned, so a second claim is rejected even while
// the code is still inside its expiry. An expired code is unpublished too.
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
	if !s.now().Before(issued.expires) {
		issued.used = true
		s.mu.Unlock()
		s.unpublishCode(ctx, normalized)
		return Record{}, errors.New("pairing code expired")
	}
	if issued.used {
		s.mu.Unlock()
		return Record{}, errors.New("pairing code already used")
	}
	issued.used = true
	record := issued.record
	s.mu.Unlock()
	s.saveCodes()
	// The code is already burned, so a second claim is rejected. Unpublish
	// only drops the directory copy. Doing that on the HTTP response lets a
	// slow directory cancel the redeemer, which then hears "already used"
	// from the next path.
	s.unpublishWG.Add(1)
	go func(code string) {
		defer s.unpublishWG.Done()
		s.unpublishCode(context.Background(), code)
	}(normalized)
	return record, nil
}

// waitUnpublish blocks until background unpublishes started by Claim finish.
func (s *Service) waitUnpublish() {
	if s == nil {
		return
	}
	s.unpublishWG.Wait()
}

// Sweep unpublishes codes that were redeemed or reached their expiry.
// A failed unpublish is retried on the next sweep.
func (s *Service) Sweep(ctx context.Context) {
	if s == nil {
		return
	}
	s.mu.Lock()
	now := s.now()
	pending := make([]string, 0)
	for _, issued := range s.codes {
		if issued == nil || issued.unpublished {
			continue
		}
		if issued.used || !now.Before(issued.expires) {
			if !now.Before(issued.expires) {
				issued.used = true
			}
			pending = append(pending, issued.code)
		}
	}
	s.mu.Unlock()
	for _, code := range pending {
		s.unpublishCode(ctx, code)
	}
}

func (s *Service) unpublishCode(ctx context.Context, code string) {
	dir, err := s.directory(ctx)
	if err != nil {
		return
	}
	if err := dir.Unpublish(ctx, Namespace(code)); err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if issued := s.codes[NormalizeCode(code)]; issued != nil {
		issued.unpublished = true
	}
}

// ServedPayload returns the sealed record for a DHT namespace while the code
// is still redeemable. Used and unpublished codes are omitted.
func (s *Service) ServedPayload(namespace string) ([]byte, bool) {
	if s == nil || !strings.HasPrefix(namespace, CodeNamespacePrefix) || strings.Contains(namespace, "..") {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for _, issued := range s.codes {
		if issued == nil || issued.used || issued.unpublished || !now.Before(issued.expires) {
			continue
		}
		if Namespace(issued.code) != namespace {
			continue
		}
		return append([]byte(nil), issued.payload...), true
	}
	return nil, false
}

// Warmup builds the pairing directory and republishes unused codes loaded
// from appdata. A container restart otherwise keeps the file and still
// answers redeem with "not found", because the local listener and the DHT
// provider record only live in memory.
func (s *Service) Warmup(ctx context.Context) error {
	if _, err := s.directory(ctx); err != nil {
		return err
	}
	return s.Republish(ctx)
}

// Republish stores every unused, unexpired code again. A directory that is
// not ready yet does not delete the on-disk code; the next attempt retries.
func (s *Service) Republish(ctx context.Context) error {
	if s == nil {
		return nil
	}
	dir, err := s.directory(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	now := s.now()
	type pendingCode struct {
		code    string
		payload []byte
	}
	pending := make([]pendingCode, 0, len(s.codes))
	for _, issued := range s.codes {
		if issued == nil || issued.used || issued.unpublished || len(issued.payload) == 0 || !now.Before(issued.expires) {
			continue
		}
		pending = append(pending, pendingCode{code: issued.code, payload: append([]byte(nil), issued.payload...)})
	}
	s.mu.Unlock()
	var errs []error
	for _, item := range pending {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		publishErr := publishRestored(ctx, dir, Namespace(item.code), item.payload)
		var partial partialPublishError
		if publishErr == nil || errors.As(publishErr, &partial) {
			structuredlog.Event("info", "pairing.republish", "pairing code republished after restart", nil)
		}
		if publishErr != nil {
			errs = append(errs, publishErr)
			structuredlog.Event("warn", "pairing.republish.failed", "stored pairing code was not republished", map[string]any{"error": publishErr.Error()})
			continue
		}
	}
	return errors.Join(errs...)
}

func publishRestored(ctx context.Context, dir Directory, namespace string, payload []byte) error {
	if all, ok := dir.(*MultiDirectory); ok {
		return all.PublishAll(ctx, namespace, payload)
	}
	return dir.Publish(ctx, namespace, payload)
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
	s.createMu.Lock()
	defer s.createMu.Unlock()
	s.mu.Lock()
	dir = s.dir
	s.mu.Unlock()
	if dir != nil {
		return dir, nil
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
