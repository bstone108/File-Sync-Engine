package link

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"filesyncengine/internal/peeridentity"
)

const (
	CodeVersion = "fse-pair-code-v1"
	// CodeTTL is how long a pairing code can be redeemed.
	CodeTTL = 10 * time.Minute
	// CodeNamespacePrefix is the DHT provider namespace for one pairing code.
	// The rest of the namespace is a hash of the code, so the code itself is
	// not published.
	CodeNamespacePrefix = "filesyncengine/pair/v1/"
)

const codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Record is the DHT payload for a pairing code. The code is not inside it.
// A MAC binds the payload to the code, and a signature binds it to the
// publisher identity key, so a lookup attacker cannot substitute another peer.
type Record struct {
	Version           string    `json:"version"`
	DiscoveryID       string    `json:"discoveryId"`
	NodeName          string    `json:"nodeName"`
	CertificateSHA256 string    `json:"certificateSha256,omitempty"`
	Addresses         []Address `json:"addresses"`
	DHTPeerID         string    `json:"dhtPeerId,omitempty"`
	Relays            []string  `json:"relays,omitempty"`
	ExpiresAt         time.Time `json:"expiresAt"`
	Nonce             string    `json:"nonce"`
	Signature         string    `json:"signature"`
	MAC               string    `json:"mac"`
}

// Profile returns the reconnect profile carried by a verified record.
func (r Record) Profile() Profile {
	return Profile{
		DiscoveryID:       r.DiscoveryID,
		NodeName:          r.NodeName,
		DHTPeerID:         r.DHTPeerID,
		CertificateSHA256: r.CertificateSHA256,
		Addresses:         append([]Address(nil), r.Addresses...),
		Relays:            append([]string(nil), r.Relays...),
	}
}

// Namespace is the DHT lookup key for a code. It is not reversible to the code.
func Namespace(code string) string {
	sum := sha256.Sum256([]byte(CodeVersion + ":" + NormalizeCode(code)))
	return CodeNamespacePrefix + hex.EncodeToString(sum[:])
}

// NewCode returns a short single-use pairing code, displayed as XXXX-XXXX.
func NewCode() (string, error) {
	buf := make([]byte, 5)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	acc := uint64(buf[0])<<32 | uint64(buf[1])<<24 | uint64(buf[2])<<16 | uint64(buf[3])<<8 | uint64(buf[4])
	chars := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		chars[i] = codeAlphabet[acc&31]
		acc >>= 5
	}
	return string(chars[:4]) + "-" + string(chars[4:]), nil
}

// NormalizeCode strips separators and rejects anything that is not 8 alphabet characters.
func NormalizeCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(code)) {
		if r == '-' || r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func validCode(code string) bool {
	if len(code) != 8 {
		return false
	}
	for _, r := range code {
		if !strings.ContainsRune(codeAlphabet, r) {
			return false
		}
	}
	return true
}

// Seal signs the profile with the publisher identity and MACs it under the code.
func Seal(code string, identity peeridentity.Identity, profile Profile, expires time.Time) (Record, []byte, error) {
	normalized := NormalizeCode(code)
	if !validCode(normalized) {
		return Record{}, nil, errors.New("pairing code is invalid")
	}
	if identity.PublicKey == "" || identity.PrivateKey == "" {
		return Record{}, nil, errors.New("pairing identity key is required")
	}
	if profile.DiscoveryID == "" {
		profile.DiscoveryID = identity.PublicKey
	}
	if profile.DiscoveryID != identity.PublicKey {
		return Record{}, nil, errors.New("pairing record identity does not match the signing key")
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return Record{}, nil, err
	}
	record := Record{
		Version:           CodeVersion,
		DiscoveryID:       profile.DiscoveryID,
		NodeName:          profile.NodeName,
		CertificateSHA256: strings.ToLower(strings.TrimSpace(profile.CertificateSHA256)),
		Addresses:         append([]Address(nil), profile.Addresses...),
		DHTPeerID:         profile.DHTPeerID,
		Relays:            append([]string(nil), profile.Relays...),
		ExpiresAt:         expires.UTC().Truncate(time.Second),
		Nonce:             hex.EncodeToString(nonce),
	}
	if err := signRecord(&record, identity); err != nil {
		return Record{}, nil, err
	}
	if err := macRecord(&record, normalized); err != nil {
		return Record{}, nil, err
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return Record{}, nil, err
	}
	return record, payload, nil
}

// Open checks the code MAC, the identity signature, and the expiry.
// A payload sealed under a different key or code is rejected.
func Open(code string, payload []byte, now time.Time) (Record, error) {
	normalized := NormalizeCode(code)
	if !validCode(normalized) {
		return Record{}, errors.New("pairing code is invalid")
	}
	var record Record
	if err := json.Unmarshal(payload, &record); err != nil {
		return Record{}, errors.New("pairing record failed key verification")
	}
	if record.Version != CodeVersion || record.DiscoveryID == "" || record.Signature == "" || record.MAC == "" {
		return Record{}, errors.New("pairing record failed key verification")
	}
	if err := verifyMAC(record, normalized); err != nil {
		return Record{}, err
	}
	body, err := record.signedBody()
	if err != nil {
		return Record{}, err
	}
	if err := peeridentity.Verify(record.DiscoveryID, record.Signature, body); err != nil {
		return Record{}, errors.New("pairing record failed key verification")
	}
	if !record.ExpiresAt.IsZero() && !now.Before(record.ExpiresAt) {
		return Record{}, errors.New("pairing code expired")
	}
	if err := validateCertificatePin(record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func signRecord(record *Record, identity peeridentity.Identity) error {
	body, err := record.signedBody()
	if err != nil {
		return err
	}
	signature, err := peeridentity.Sign(identity, body)
	if err != nil {
		return err
	}
	record.Signature = signature
	return nil
}

func macRecord(record *Record, code string) error {
	body, err := record.macBody()
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, []byte(code))
	_, _ = mac.Write(body)
	record.MAC = hex.EncodeToString(mac.Sum(nil))
	return nil
}

func verifyMAC(record Record, code string) error {
	body, err := record.macBody()
	if err != nil {
		return errors.New("pairing record failed key verification")
	}
	mac := hmac.New(sha256.New, []byte(code))
	_, _ = mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(strings.ToLower(record.MAC))) != 1 {
		return errors.New("pairing record failed key verification")
	}
	return nil
}

func (r Record) signedBody() ([]byte, error) {
	payload := struct {
		Version           string    `json:"version"`
		DiscoveryID       string    `json:"discoveryId"`
		NodeName          string    `json:"nodeName"`
		CertificateSHA256 string    `json:"certificateSha256"`
		Addresses         []Address `json:"addresses"`
		DHTPeerID         string    `json:"dhtPeerId"`
		Relays            []string  `json:"relays"`
		ExpiresAt         string    `json:"expiresAt"`
		Nonce             string    `json:"nonce"`
	}{
		Version:           r.Version,
		DiscoveryID:       r.DiscoveryID,
		NodeName:          r.NodeName,
		CertificateSHA256: r.CertificateSHA256,
		Addresses:         r.Addresses,
		DHTPeerID:         r.DHTPeerID,
		Relays:            r.Relays,
		ExpiresAt:         r.ExpiresAt.UTC().Format(time.RFC3339),
		Nonce:             r.Nonce,
	}
	if payload.Addresses == nil {
		payload.Addresses = []Address{}
	}
	if payload.Relays == nil {
		payload.Relays = []string{}
	}
	return json.Marshal(payload)
}

func (r Record) macBody() ([]byte, error) {
	body, err := r.signedBody()
	if err != nil {
		return nil, err
	}
	return append(body, []byte(r.Signature)...), nil
}

func validateCertificatePin(record Record) error {
	needsPin := false
	for _, address := range record.Addresses {
		if strings.HasPrefix(address.Address, "https://") {
			needsPin = true
			break
		}
	}
	if !needsPin {
		return nil
	}
	if len(record.CertificateSHA256) != 64 {
		return errors.New("pairing record is missing an API certificate fingerprint")
	}
	if _, err := hex.DecodeString(record.CertificateSHA256); err != nil {
		return errors.New("pairing record is missing an API certificate fingerprint")
	}
	return nil
}

// ClaimRequest is the redeemer's authenticated introduction to the publisher.
type ClaimRequest struct {
	Code              string  `json:"code"`
	DiscoveryID       string  `json:"discoveryId"`
	NodeName          string  `json:"nodeName"`
	APIKey            string  `json:"apiKey"`
	CertificateSHA256 string  `json:"certificateSha256,omitempty"`
	Profile           Profile `json:"profile"`
	Signature         string  `json:"signature"`
}

// ClaimResponse returns the publisher credential and reconnect profile.
type ClaimResponse struct {
	DiscoveryID       string  `json:"discoveryId"`
	NodeName          string  `json:"nodeName"`
	APIKey            string  `json:"apiKey"`
	CertificateSHA256 string  `json:"certificateSha256,omitempty"`
	Profile           Profile `json:"profile"`
}

// SignClaim binds the claim to the redeemer's identity key.
func SignClaim(identity peeridentity.Identity, claim ClaimRequest) (ClaimRequest, error) {
	claim.DiscoveryID = identity.PublicKey
	claim.Signature = ""
	body, err := claim.signedBody()
	if err != nil {
		return ClaimRequest{}, err
	}
	signature, err := peeridentity.Sign(identity, body)
	if err != nil {
		return ClaimRequest{}, err
	}
	claim.Signature = signature
	return claim, nil
}

// VerifyClaim checks the redeemer's signature before a code is consumed.
func VerifyClaim(claim ClaimRequest) error {
	if claim.DiscoveryID == "" || claim.APIKey == "" || claim.Signature == "" {
		return errors.New("pairing claim is incomplete")
	}
	signature := claim.Signature
	claim.Signature = ""
	body, err := claim.signedBody()
	if err != nil {
		return err
	}
	if err := peeridentity.Verify(claim.DiscoveryID, signature, body); err != nil {
		return errors.New("pairing claim failed key verification")
	}
	return nil
}

func (c ClaimRequest) signedBody() ([]byte, error) {
	payload := struct {
		Code              string  `json:"code"`
		DiscoveryID       string  `json:"discoveryId"`
		NodeName          string  `json:"nodeName"`
		APIKey            string  `json:"apiKey"`
		CertificateSHA256 string  `json:"certificateSha256"`
		Profile           Profile `json:"profile"`
	}{
		Code:              NormalizeCode(c.Code),
		DiscoveryID:       c.DiscoveryID,
		NodeName:          c.NodeName,
		APIKey:            c.APIKey,
		CertificateSHA256: strings.ToLower(strings.TrimSpace(c.CertificateSHA256)),
		Profile:           c.Profile,
	}
	return json.Marshal(payload)
}

// LinkPush is an already-paired peer announcing replacement addresses.
type LinkPush struct {
	DiscoveryID string  `json:"discoveryId"`
	Profile     Profile `json:"profile"`
	Signature   string  `json:"signature"`
}

// SignPush signs a refresh announcement.
func SignPush(identity peeridentity.Identity, push LinkPush) (LinkPush, error) {
	push.DiscoveryID = identity.PublicKey
	push.Signature = ""
	body, err := json.Marshal(struct {
		DiscoveryID string  `json:"discoveryId"`
		Profile     Profile `json:"profile"`
	}{DiscoveryID: push.DiscoveryID, Profile: push.Profile})
	if err != nil {
		return LinkPush{}, err
	}
	signature, err := peeridentity.Sign(identity, body)
	if err != nil {
		return LinkPush{}, err
	}
	push.Signature = signature
	return push, nil
}

// VerifyPush checks a refresh announcement against the paired identity key.
func VerifyPush(push LinkPush) error {
	if push.DiscoveryID == "" || push.Signature == "" {
		return errors.New("link refresh failed key verification")
	}
	body, err := json.Marshal(struct {
		DiscoveryID string  `json:"discoveryId"`
		Profile     Profile `json:"profile"`
	}{DiscoveryID: push.DiscoveryID, Profile: push.Profile})
	if err != nil {
		return err
	}
	if err := peeridentity.Verify(push.DiscoveryID, push.Signature, body); err != nil {
		return errors.New("link refresh failed key verification")
	}
	return nil
}
