package peeridentity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

const DefaultEncryptionLevel = 4

// IdentityExchangeEncryptionLevel is the required strength for identity
// introduction, pairing, and session negotiation. Ordinary file payloads use
// DefaultEncryptionLevel unless a peer asks for more.
const IdentityExchangeEncryptionLevel = 10

// EncryptionRetryCode is the stream error code a peer returns when the offered
// handshake is missing material for the negotiated level. The message is
// "retry handshake at encryption level N". Callers retry once at N. They do
// not continue with a weaker cipher.
const EncryptionRetryCode = "encryption_retry"

type EncryptionProfileSpec struct {
	Level             int
	Name              string
	Description       string
	Explanation       string
	Plaintext         bool
	DebugVisible      bool
	RequiresAEAD      bool
	KeyAgreement      string
	KDF               string
	AEAD              string
	MAC               string
	Signatures        string
	RekeyEveryMiB     int
	MemoryCostMiB     int
	Iterations        int
	CompatibilityNote string
}

func EncryptionProfile(level int) (EncryptionProfileSpec, error) {
	if err := ValidateEncryptionLevel(level); err != nil {
		return EncryptionProfileSpec{}, err
	}
	return profileFor(level), nil
}

func EncryptionProfiles() []EncryptionProfileSpec {
	profiles := make([]EncryptionProfileSpec, 0, 11)
	for level := 0; level <= 10; level++ {
		profiles = append(profiles, profileFor(level))
	}
	return profiles
}

type Identity struct {
	PublicKey  string
	PrivateKey string
}

type SignedHello struct {
	NodeID               string
	PublicKey            string
	SessionPublicKey     string
	SessionKEMPublic     string
	SessionKEMCiphertext string
	SessionPQPublic      string
	SessionPQSignature   string
	SessionSLHPublic     string
	SessionSLHSignature  string
	EncryptionLevel      int
	Signature            string
}

// Sign returns a base64 signature over payload using the identity private key.
func Sign(identity Identity, payload []byte) (string, error) {
	privateKey, err := decodePrivateKey(identity.PrivateKey)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload)), nil
}

// Verify checks a base64 signature against the identity public key.
func Verify(publicKey, signature string, payload []byte) error {
	if publicKey == "" {
		return fmt.Errorf("peer public key is required")
	}
	key, err := decodePublicKey(publicKey)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	if !ed25519.Verify(key, payload, sig) {
		return fmt.Errorf("peer identity signature verification failed")
	}
	return nil
}

func GenerateIdentity() (Identity, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Identity{}, err
	}
	return Identity{
		PublicKey:  base64.StdEncoding.EncodeToString(publicKey),
		PrivateKey: base64.StdEncoding.EncodeToString(privateKey),
	}, nil
}

func SignHello(identity Identity, nodeID string, encryptionLevel int, nonce []byte) (SignedHello, error) {
	return SignSessionHello(identity, nodeID, encryptionLevel, "", nonce)
}

func SignSessionHello(identity Identity, nodeID string, encryptionLevel int, sessionPublicKey string, nonce []byte) (SignedHello, error) {
	return SignBoundHello(identity, SignedHello{NodeID: nodeID, SessionPublicKey: sessionPublicKey, EncryptionLevel: encryptionLevel}, nonce)
}

func SignBoundHello(identity Identity, hello SignedHello, nonce []byte) (SignedHello, error) {
	if err := ValidateEncryptionLevel(hello.EncryptionLevel); err != nil {
		return SignedHello{}, err
	}
	privateKey, err := decodePrivateKey(identity.PrivateKey)
	if err != nil {
		return SignedHello{}, err
	}
	hello.PublicKey = identity.PublicKey
	hello.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, helloPayload(hello, nonce)))
	return hello, nil
}

func VerifyHello(hello SignedHello, expectedPublicKey string, nonce []byte) error {
	if err := ValidateEncryptionLevel(hello.EncryptionLevel); err != nil {
		return err
	}
	if hello.PublicKey == "" {
		return fmt.Errorf("peer public key is required")
	}
	if expectedPublicKey != "" && hello.PublicKey != expectedPublicKey {
		return fmt.Errorf("peer identity public key mismatch")
	}
	publicKey, err := decodePublicKey(hello.PublicKey)
	if err != nil {
		return err
	}
	signature, err := base64.StdEncoding.DecodeString(hello.Signature)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	if !ed25519.Verify(publicKey, helloPayload(hello, nonce), signature) {
		return fmt.Errorf("peer identity signature verification failed")
	}
	return nil
}

func Fingerprint(publicKey string) (string, error) {
	key, err := decodePublicKey(publicKey)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(key)
	return base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

func ValidateEncryptionLevel(level int) error {
	if level < 0 || level > 10 {
		return fmt.Errorf("encryption level must be between 0 and 10")
	}
	return nil
}

func helloPayload(hello SignedHello, nonce []byte) []byte {
	return []byte(fmt.Sprintf("fse-peer-hello-v2\n%s\n%s\n%s\n%d\n%s\n%s\n%s\n%s\n%s\n%s\n%s",
		hello.NodeID,
		hello.PublicKey,
		hello.SessionPublicKey,
		hello.EncryptionLevel,
		string(nonce),
		hello.SessionKEMPublic,
		hello.SessionKEMCiphertext,
		hello.SessionPQPublic,
		hello.SessionPQSignature,
		hello.SessionSLHPublic,
		hello.SessionSLHSignature,
	))
}

func decodePublicKey(encoded string) (ed25519.PublicKey, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key must be %d bytes", ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(key), nil
}

func decodePrivateKey(encoded string) (ed25519.PrivateKey, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode private key: %w", err)
	}
	if len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("private key must be %d bytes", ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(key), nil
}
