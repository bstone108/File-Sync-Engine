package peeridentity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

const DefaultEncryptionLevel = 4

type EncryptionProfileSpec struct {
	Level             int
	Name              string
	Description       string
	DebugVisible      bool
	RequiresAEAD      bool
	KeyAgreement      string
	KDF               string
	AEAD              string
	RekeyEveryMiB     int
	MemoryCostMiB     int
	Iterations        int
	CompatibilityNote string
}

var encryptionProfiles = map[int]EncryptionProfileSpec{
	0:  {Level: 0, Name: "none-debug", Description: "No payload encryption for local debugging or protocol inspection.", DebugVisible: true, RequiresAEAD: false, KeyAgreement: "none", KDF: "none", AEAD: "none", CompatibilityNote: "Never use on untrusted links."},
	1:  {Level: 1, Name: "minimal-permissive", Description: "Minimal authenticated encryption for lawful-compatibility and low-end drop-in deployments.", RequiresAEAD: true, KeyAgreement: "x25519", KDF: "hkdf-sha256", AEAD: "aes-128-gcm", RekeyEveryMiB: 1024, CompatibilityNote: "Use only when stronger levels are not permitted or practical."},
	2:  {Level: 2, Name: "basic", Description: "Baseline authenticated encryption with broad hardware/software support.", RequiresAEAD: true, KeyAgreement: "x25519", KDF: "hkdf-sha256", AEAD: "chacha20-poly1305", RekeyEveryMiB: 512},
	3:  {Level: 3, Name: "balanced", Description: "Balanced modern authenticated encryption for ordinary private links.", RequiresAEAD: true, KeyAgreement: "x25519", KDF: "hkdf-sha256", AEAD: "chacha20-poly1305", RekeyEveryMiB: 256},
	4:  {Level: 4, Name: "strong", Description: "Default bank-equivalent strong encryption suitable for ordinary peer-pair sessions.", RequiresAEAD: true, KeyAgreement: "x25519", KDF: "hkdf-sha256", AEAD: "xchacha20-poly1305", RekeyEveryMiB: 256},
	5:  {Level: 5, Name: "standard-strong", Description: "Stronger production session profile with tighter rekeying than the default.", RequiresAEAD: true, KeyAgreement: "x25519", KDF: "hkdf-sha256", AEAD: "xchacha20-poly1305", RekeyEveryMiB: 128},
	6:  {Level: 6, Name: "strong-plus", Description: "Strong encryption with tighter rekeying for higher-risk links.", RequiresAEAD: true, KeyAgreement: "x25519", KDF: "hkdf-sha384", AEAD: "xchacha20-poly1305", RekeyEveryMiB: 64},
	7:  {Level: 7, Name: "hardened", Description: "Hardened session profile with SHA-512 key schedule and frequent rekeying.", RequiresAEAD: true, KeyAgreement: "x25519", KDF: "hkdf-sha512", AEAD: "xchacha20-poly1305", RekeyEveryMiB: 32},
	8:  {Level: 8, Name: "high-security", Description: "High-security profile with hybrid key agreement planning and low rekey interval.", RequiresAEAD: true, KeyAgreement: "x25519+ml-kem-768-planned", KDF: "hkdf-sha512", AEAD: "xchacha20-poly1305", RekeyEveryMiB: 16},
	9:  {Level: 9, Name: "paranoid", Description: "Paranoid profile with hybrid key agreement planning and memory-hard key stretching.", RequiresAEAD: true, KeyAgreement: "x25519+ml-kem-1024-planned", KDF: "argon2id+hkdf-sha512", AEAD: "xchacha20-poly1305", RekeyEveryMiB: 8, MemoryCostMiB: 64, Iterations: 2},
	10: {Level: 10, Name: "maximum-high-cpu", Description: "Maximum protection target with high CPU/memory cost accepted.", RequiresAEAD: true, KeyAgreement: "x25519+ml-kem-1024-planned", KDF: "argon2id+hkdf-sha512", AEAD: "xchacha20-poly1305", RekeyEveryMiB: 4, MemoryCostMiB: 256, Iterations: 4, CompatibilityNote: "Expect higher CPU and memory use."},
}

func EncryptionProfile(level int) (EncryptionProfileSpec, error) {
	profile, ok := encryptionProfiles[level]
	if !ok {
		return EncryptionProfileSpec{}, fmt.Errorf("encryption level must be between 0 and 10")
	}
	return profile, nil
}

type Identity struct {
	PublicKey  string
	PrivateKey string
}

type SignedHello struct {
	NodeID           string
	PublicKey        string
	SessionPublicKey string
	EncryptionLevel  int
	Signature        string
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
	if err := ValidateEncryptionLevel(encryptionLevel); err != nil {
		return SignedHello{}, err
	}
	privateKey, err := decodePrivateKey(identity.PrivateKey)
	if err != nil {
		return SignedHello{}, err
	}
	hello := SignedHello{NodeID: nodeID, PublicKey: identity.PublicKey, SessionPublicKey: sessionPublicKey, EncryptionLevel: encryptionLevel}
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
	return []byte(fmt.Sprintf("fse-peer-hello-v1\n%s\n%s\n%s\n%d\n%s", hello.NodeID, hello.PublicKey, hello.SessionPublicKey, hello.EncryptionLevel, string(nonce)))
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
