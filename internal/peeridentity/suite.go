package peeridentity

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/mlkem"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"fmt"
	"hash"
	"io"

	"github.com/cloudflare/circl/dh/x448"
	"github.com/cloudflare/circl/sign"
	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"github.com/cloudflare/circl/sign/slhdsa"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/twofish"
)

// HandshakeRetryError tells the initiator to send one new hello at Level.
// It is the only response to missing stronger-suite material. The session
// must not continue with the weaker suite that was already offered.
type HandshakeRetryError struct {
	Level int
}

func (e *HandshakeRetryError) Error() string {
	return fmt.Sprintf("retry handshake at encryption level %d", e.Level)
}

func ParseRetryLevel(message string) (int, bool) {
	var level int
	n, err := fmt.Sscanf(message, "retry handshake at encryption level %d", &level)
	if err != nil || n != 1 || ValidateEncryptionLevel(level) != nil {
		return 0, false
	}
	return level, true
}

// NegotiateSessionLevel is the stream policy. The session level is the higher
// of the two configured levels. A peer never accepts a session below its own
// level, and there is no weaker-policy override.
func NegotiateSessionLevel(localLevel, peerLevel int) (int, error) {
	if err := ValidateEncryptionLevel(localLevel); err != nil {
		return 0, err
	}
	if err := ValidateEncryptionLevel(peerLevel); err != nil {
		return 0, err
	}
	if peerLevel > localLevel {
		return peerLevel, nil
	}
	return localLevel, nil
}

type cipherID string

const (
	cipherAES128  cipherID = "aes-128-gcm"
	cipherAES256  cipherID = "aes-256-gcm"
	cipherChaCha  cipherID = "chacha20-poly1305"
	cipherXChaCha cipherID = "xchacha20-poly1305"
	cipherTwofish cipherID = "twofish-256-gcm"
)

type suiteParams struct {
	name        string
	description string
	explanation string
	note        string
	classical   string
	kem         string
	ciphers     []cipherID
	kdf         string
	hash        func() hash.Hash
	argonMem    int
	argonTime   uint32
	macHMAC     bool
	macBlake    bool
	mac         string
	signatures  string
	pq          string
	slh         bool
	rekeyMiB    int
	plaintext   bool
}

func profileFor(level int) EncryptionProfileSpec {
	params := suiteFor(level)
	aead := "none"
	if len(params.ciphers) > 0 {
		aead = string(params.ciphers[0])
		for _, id := range params.ciphers[1:] {
			aead += "+" + string(id)
		}
	}
	agreement := "none"
	switch {
	case params.classical != "" && params.kem != "":
		agreement = params.classical + "+" + params.kem
	case params.classical != "":
		agreement = params.classical
	}
	return EncryptionProfileSpec{
		Level:             level,
		Name:              params.name,
		Description:       params.description,
		Explanation:       params.explanation,
		Plaintext:         params.plaintext,
		DebugVisible:      params.plaintext,
		RequiresAEAD:      !params.plaintext,
		KeyAgreement:      agreement,
		KDF:               params.kdf,
		AEAD:              aead,
		MAC:               params.mac,
		Signatures:        params.signatures,
		RekeyEveryMiB:     params.rekeyMiB,
		MemoryCostMiB:     params.argonMem,
		Iterations:        int(params.argonTime),
		CompatibilityNote: params.note,
	}
}

func suiteFor(level int) suiteParams {
	switch level {
	case 0:
		return suiteParams{
			name: "none-debug", description: "No payload encryption for local debugging or protocol inspection.",
			explanation: "Level 0 sends file contents and control messages as plaintext. Anyone on the path can read them. Use it only for local debugging.",
			note:        "Never use on untrusted links.",
			kdf:         "none", mac: "none", signatures: "ed25519", plaintext: true,
		}
	case 1:
		return suiteParams{
			name: "minimal-permissive", description: "Minimal authenticated encryption for lawful-compatibility and low-end drop-in deployments.",
			explanation: "Level 1 is the weakest encrypted session: X25519, HKDF-SHA256, and AES-128-GCM, rekeyed every 1024 MiB. Use it only where stronger levels are not permitted.",
			note:        "Use only when stronger levels are not permitted or practical.",
			classical:   "x25519", ciphers: []cipherID{cipherAES128}, kdf: "hkdf-sha256", hash: sha256.New,
			mac: "aead", signatures: "ed25519", rekeyMiB: 1024,
		}
	case 2:
		return suiteParams{
			name: "basic", description: "Baseline authenticated encryption with a 256-bit stream cipher.",
			explanation: "Level 2 upgrades the cipher to ChaCha20-Poly1305 and rekeys twice as often as level 1. Key agreement stays X25519 with HKDF-SHA256.",
			classical:   "x25519", ciphers: []cipherID{cipherChaCha}, kdf: "hkdf-sha256", hash: sha256.New,
			mac: "aead", signatures: "ed25519", rekeyMiB: 512,
		}
	case 3:
		return suiteParams{
			name: "balanced", description: "Single-cipher AES-256-GCM for ordinary private links.",
			explanation: "Level 3 uses AES-256-GCM, still with X25519 and HKDF-SHA256, and rekeys every 256 MiB.",
			classical:   "x25519", ciphers: []cipherID{cipherAES256}, kdf: "hkdf-sha256", hash: sha256.New,
			mac: "aead", signatures: "ed25519", rekeyMiB: 256,
		}
	case 4:
		return suiteParams{
			name: "strong", description: "Default bank-equivalent cascade for ordinary peer-pair sessions.",
			explanation: "Level 4 is the default data level. It is TLS 1.3-grade: ephemeral X25519, independent AES-256-GCM and ChaCha20-Poly1305 layers, and HKDF-SHA256, rekeyed every 128 MiB.",
			classical:   "x25519", ciphers: []cipherID{cipherAES256, cipherChaCha}, kdf: "hkdf-sha256", hash: sha256.New,
			mac: "aead", signatures: "ed25519", rekeyMiB: 128,
		}
	case 5:
		return suiteParams{
			name: "standard-strong", description: "Long-nonce cascade with a wider HKDF hash.",
			explanation: "Level 5 stacks XChaCha20-Poly1305 and AES-256-GCM under HKDF-SHA384 and rekeys every 64 MiB.",
			classical:   "x25519", ciphers: []cipherID{cipherXChaCha, cipherAES256}, kdf: "hkdf-sha384", hash: sha512.New384,
			mac: "aead", signatures: "ed25519", rekeyMiB: 64,
		}
	case 6:
		return suiteParams{
			name: "strong-plus", description: "Level 5 cascade plus an independent HMAC-SHA512.",
			explanation: "Level 6 keeps the level 5 cipher stack, switches the KDF to HKDF-SHA512, adds HMAC-SHA512, and rekeys every 32 MiB.",
			classical:   "x25519", ciphers: []cipherID{cipherXChaCha, cipherAES256}, kdf: "hkdf-sha512", hash: sha512.New,
			macHMAC: true, mac: "hmac-sha512", signatures: "ed25519", rekeyMiB: 32,
		}
	case 7:
		return suiteParams{
			name: "hardened", description: "Hybrid post-quantum key agreement with a memory-hard KDF.",
			explanation: "Level 7 adds ML-KEM-768 beside X25519, stretches the secret with Argon2id (16 MiB, 1 pass) then HKDF-SHA512, keeps HMAC-SHA512, and rekeys every 16 MiB.",
			classical:   "x25519", kem: "ml-kem-768", ciphers: []cipherID{cipherXChaCha, cipherAES256},
			kdf: "argon2id+hkdf-sha512", hash: sha512.New, argonMem: 16, argonTime: 1,
			macHMAC: true, mac: "hmac-sha512", signatures: "ed25519", rekeyMiB: 16,
		}
	case 8:
		return suiteParams{
			name: "high-security", description: "Three independent ciphers and a larger Argon2id stretch.",
			explanation: "Level 8 adds Twofish-256-GCM to the XChaCha20 and AES-256 cascade, still with X25519 plus ML-KEM-768, Argon2id at 32 MiB, HMAC-SHA512, and an 8 MiB rekey.",
			classical:   "x25519", kem: "ml-kem-768", ciphers: []cipherID{cipherXChaCha, cipherAES256, cipherTwofish},
			kdf: "argon2id+hkdf-sha512", hash: sha512.New, argonMem: 32, argonTime: 1,
			macHMAC: true, mac: "hmac-sha512", signatures: "ed25519", rekeyMiB: 8,
		}
	case 9:
		return suiteParams{
			name: "paranoid", description: "X448, ML-KEM-1024, and an ML-DSA-65 session signature.",
			explanation: "Level 9 moves key agreement to X448 plus ML-KEM-1024, uses the three-cipher cascade, Argon2id at 64 MiB for 2 passes, a keyed BLAKE2b-512 MAC, ML-DSA-65 beside Ed25519, and a 4 MiB rekey.",
			classical:   "x448", kem: "ml-kem-1024", ciphers: []cipherID{cipherXChaCha, cipherAES256, cipherTwofish},
			kdf: "argon2id+hkdf-sha512", hash: sha512.New, argonMem: 64, argonTime: 2,
			macBlake: true, mac: "blake2b-512", signatures: "ed25519+ml-dsa-65", pq: "ml-dsa-65", rekeyMiB: 4,
		}
	default:
		return suiteParams{
			name: "maximum-high-cpu", description: "Maximum protection target. Handshake cost is accepted.",
			explanation: "Level 10 is the identity-exchange and negotiation level. Speed is not a goal: X448 plus ML-KEM-1024, AES-256-GCM, XChaCha20-Poly1305, and Twofish-256-GCM with independent keys, Argon2id at 128 MiB for 3 passes, HMAC-SHA512 and BLAKE2b-512, ML-DSA-87 and SLH-DSA-SHA2-256s beside Ed25519, and a 1 MiB rekey.",
			note:        "Expect higher CPU and memory use. This is the identity exchange and negotiation level.",
			classical:   "x448", kem: "ml-kem-1024", ciphers: []cipherID{cipherXChaCha, cipherAES256, cipherTwofish},
			kdf: "argon2id+hkdf-sha512", hash: sha512.New, argonMem: 128, argonTime: 3,
			macHMAC: true, macBlake: true, mac: "hmac-sha512+blake2b-512",
			signatures: "ed25519+ml-dsa-87+slh-dsa-sha2-256s", pq: "ml-dsa-87", slh: true, rekeyMiB: 1,
		}
	}
}

// PeerMaterial is the public handshake offer carried in a hello.
type PeerMaterial struct {
	Level           int
	Nonce           string
	NodeID          string
	ClassicalPublic []byte
	KEMPublic       []byte
	KEMCiphertext   []byte
	PQPublic        []byte
	PQSignature     []byte
	SLHPublic       []byte
	SLHSignature    []byte
}

// PendingHandshake holds ephemeral secrets until Finish derives traffic keys.
type PendingHandshake struct {
	initiator        bool
	level            int
	nonce            string
	nodeID           string
	classicalPrivate []byte
	classicalPublic  []byte
	kemSeed          []byte
	kemPublic        []byte
	kemShared        []byte
	kemCiphertext    []byte
	pqScheme         sign.Scheme
	pqPublic         sign.PublicKey
	pqPrivate        sign.PrivateKey
	pqSig            []byte
	slhPrivate       *slhdsa.PrivateKey
	slhPublic        []byte
	slhSig           []byte
}

// SessionCipher is the directional traffic protection for one handshake.
// Nil Send means the negotiated level is plaintext.
type SessionCipher struct {
	Send            cipher.AEAD
	Recv            cipher.AEAD
	SendNoncePrefix []byte
	RecvNoncePrefix []byte
}

func BeginInitiator(level int, nonce, nodeID string) (*PendingHandshake, error) {
	pending, err := begin(level, nonce, nodeID, true)
	if err != nil {
		return nil, err
	}
	if err := pending.signOwnOffer(); err != nil {
		return nil, err
	}
	return pending, nil
}

func BeginResponder(level int, nonce, nodeID string, client PeerMaterial) (*PendingHandshake, error) {
	if err := AssessMaterial(level, client); err != nil {
		return nil, err
	}
	pending, err := begin(level, nonce, nodeID, false)
	if err != nil {
		return nil, err
	}
	if err := pending.encapsulate(client); err != nil {
		return nil, err
	}
	return pending, nil
}

func begin(level int, nonce, nodeID string, initiator bool) (*PendingHandshake, error) {
	if err := ValidateEncryptionLevel(level); err != nil {
		return nil, err
	}
	params := suiteFor(level)
	pending := &PendingHandshake{initiator: initiator, level: level, nonce: nonce, nodeID: nodeID}
	if params.plaintext {
		return pending, nil
	}
	switch params.classical {
	case "x25519":
		private, public, err := newX25519()
		if err != nil {
			return nil, err
		}
		pending.classicalPrivate, pending.classicalPublic = private, public
	case "x448":
		private, public, err := newX448()
		if err != nil {
			return nil, err
		}
		pending.classicalPrivate, pending.classicalPublic = private, public
	default:
		return nil, fmt.Errorf("encryption level %d has no classical key agreement", level)
	}
	if initiator && params.kem != "" {
		seed, public, err := newKEM(params.kem)
		if err != nil {
			return nil, err
		}
		pending.kemSeed, pending.kemPublic = seed, public
	}
	if params.pq != "" {
		scheme := pqScheme(params.pq)
		public, private, err := scheme.GenerateKey()
		if err != nil {
			return nil, err
		}
		pending.pqScheme, pending.pqPublic, pending.pqPrivate = scheme, public, private
	}
	if params.slh {
		public, private, err := slhdsa.GenerateKey(rand.Reader, slhdsa.SHA2_256s)
		if err != nil {
			return nil, err
		}
		encoded, err := public.MarshalBinary()
		if err != nil {
			return nil, err
		}
		pending.slhPrivate = &private
		pending.slhPublic = encoded
	}
	return pending, nil
}

func (p *PendingHandshake) signOwnOffer() error {
	params := suiteFor(p.level)
	if params.pq == "" && !params.slh {
		return nil
	}
	var transcript []byte
	if p.initiator {
		transcript = clientOfferTranscript(p.Material())
	} else {
		return nil
	}
	return p.signTranscript(transcript)
}

func (p *PendingHandshake) signResponderOffer(client PeerMaterial) error {
	params := suiteFor(p.level)
	if params.pq == "" && !params.slh {
		return nil
	}
	return p.signTranscript(serverOfferTranscript(client, p.Material(), p.level))
}

func (p *PendingHandshake) signTranscript(transcript []byte) error {
	params := suiteFor(p.level)
	if params.pq != "" {
		p.pqSig = p.pqScheme.Sign(p.pqPrivate, transcript, nil)
	}
	if params.slh {
		signature, err := slhdsa.SignDeterministic(p.slhPrivate, slhdsa.NewMessage(transcript), nil)
		if err != nil {
			return err
		}
		p.slhSig = signature
	}
	return nil
}

func (p *PendingHandshake) encapsulate(client PeerMaterial) error {
	params := suiteFor(p.level)
	if params.kem != "" {
		shared, ciphertext, err := kemEncapsulate(params.kem, client.KEMPublic)
		if err != nil {
			return err
		}
		p.kemShared = shared
		p.kemCiphertext = ciphertext
	}
	return p.signResponderOffer(client)
}

func (p *PendingHandshake) Material() PeerMaterial {
	material := PeerMaterial{
		Level:           p.level,
		Nonce:           p.nonce,
		NodeID:          p.nodeID,
		ClassicalPublic: append([]byte(nil), p.classicalPublic...),
		KEMPublic:       append([]byte(nil), p.kemPublic...),
		KEMCiphertext:   append([]byte(nil), p.kemCiphertext...),
		SLHPublic:       append([]byte(nil), p.slhPublic...),
		SLHSignature:    append([]byte(nil), p.slhSig...),
		PQSignature:     append([]byte(nil), p.pqSig...),
	}
	if p.pqPublic != nil {
		encoded, err := p.pqPublic.MarshalBinary()
		if err == nil {
			material.PQPublic = encoded
		}
	}
	return material
}

// AssessMaterial reports whether peer can open a session at level. Missing
// fields produce HandshakeRetryError. Present but unusable fields do not.
func AssessMaterial(level int, peer PeerMaterial) error {
	if err := ValidateEncryptionLevel(level); err != nil {
		return err
	}
	params := suiteFor(level)
	if params.plaintext {
		return nil
	}
	if !classicalSizeOK(params.classical, peer.ClassicalPublic) {
		return &HandshakeRetryError{Level: level}
	}
	if params.kem != "" && !kemPublicSizeOK(params.kem, peer.KEMPublic) {
		return &HandshakeRetryError{Level: level}
	}
	if params.pq != "" && (len(peer.PQPublic) != pqScheme(params.pq).PublicKeySize() || len(peer.PQSignature) != pqScheme(params.pq).SignatureSize()) {
		return &HandshakeRetryError{Level: level}
	}
	if params.slh && (len(peer.SLHPublic) == 0 || len(peer.SLHSignature) == 0) {
		return &HandshakeRetryError{Level: level}
	}
	return nil
}

func (p *PendingHandshake) Finish(peer PeerMaterial) (SessionCipher, error) {
	sessionLevel := p.level
	if p.initiator {
		if peer.Level < p.level {
			return SessionCipher{}, fmt.Errorf("peer negotiated weaker encryption level %d below local level %d", peer.Level, p.level)
		}
		sessionLevel = peer.Level
	}
	if err := ValidateEncryptionLevel(sessionLevel); err != nil {
		return SessionCipher{}, err
	}
	params := suiteFor(sessionLevel)
	if params.plaintext {
		return SessionCipher{}, nil
	}
	if !classicalSizeOK(params.classical, p.classicalPublic) || !classicalSizeOK(params.classical, peer.ClassicalPublic) {
		return SessionCipher{}, fmt.Errorf("classical key does not match encryption level %d", sessionLevel)
	}
	classical, err := classicalShared(params.classical, p.classicalPrivate, peer.ClassicalPublic)
	if err != nil {
		return SessionCipher{}, err
	}
	var kemShared []byte
	if params.kem != "" {
		if p.initiator {
			if len(peer.KEMCiphertext) == 0 {
				return SessionCipher{}, fmt.Errorf("peer handshake is missing key material for encryption level %d", sessionLevel)
			}
			kemShared, err = kemDecapsulate(params.kem, p.kemSeed, peer.KEMCiphertext)
			if err != nil {
				return SessionCipher{}, err
			}
		} else {
			if len(p.kemShared) == 0 {
				return SessionCipher{}, fmt.Errorf("peer handshake is missing key material for encryption level %d", sessionLevel)
			}
			kemShared = p.kemShared
		}
	}
	if err := p.acceptPeerProof(peer, sessionLevel); err != nil {
		return SessionCipher{}, err
	}
	client, server := p.Material(), peer
	if !p.initiator {
		client, server = peer, p.Material()
	}
	return newSessionCipher(sessionLevel, classical, kemShared, client, server, p.initiator)
}

func (p *PendingHandshake) acceptPeerProof(peer PeerMaterial, sessionLevel int) error {
	params := suiteFor(sessionLevel)
	if params.pq == "" && !params.slh {
		return nil
	}
	var transcript []byte
	if p.initiator {
		transcript = serverOfferTranscript(p.Material(), peer, sessionLevel)
	} else {
		transcript = clientOfferTranscript(peer)
	}
	if params.pq != "" {
		scheme := pqScheme(params.pq)
		public, err := scheme.UnmarshalBinaryPublicKey(peer.PQPublic)
		if err != nil {
			return fmt.Errorf("peer PQ key is unusable at encryption level %d", sessionLevel)
		}
		if !scheme.Verify(public, transcript, peer.PQSignature, nil) {
			return fmt.Errorf("peer session proof rejected at encryption level %d", sessionLevel)
		}
	}
	if params.slh {
		public := slhdsa.PublicKey{ID: slhdsa.SHA2_256s}
		if err := public.UnmarshalBinary(peer.SLHPublic); err != nil {
			return fmt.Errorf("peer SLH key is unusable at encryption level %d", sessionLevel)
		}
		if !slhdsa.Verify(&public, slhdsa.NewMessage(transcript), peer.SLHSignature, nil) {
			return fmt.Errorf("peer session proof rejected at encryption level %d", sessionLevel)
		}
	}
	return nil
}

func newSessionCipher(level int, classical, kemShared []byte, client, server PeerMaterial, initiator bool) (SessionCipher, error) {
	params := suiteFor(level)
	if params.hash == nil {
		return SessionCipher{}, fmt.Errorf("encryption level %d has no key schedule", level)
	}
	ikm := append([]byte(nil), classical...)
	if params.kem != "" {
		if len(kemShared) == 0 {
			return SessionCipher{}, fmt.Errorf("peer handshake is missing key material for encryption level %d", level)
		}
		ikm = append(ikm, kemShared...)
	}
	transcript := kdfTranscript(level, client, server)
	salt := sha256.Sum256(transcript)
	master, err := stretchMaster(params, ikm, salt[:], transcript)
	if err != nil {
		return SessionCipher{}, err
	}
	sendDirection, recvDirection := "c2s", "s2c"
	if !initiator {
		sendDirection, recvDirection = "s2c", "c2s"
	}
	send, err := newTrafficAEAD(params, master, salt[:], sendDirection)
	if err != nil {
		return SessionCipher{}, err
	}
	recv, err := newTrafficAEAD(params, master, salt[:], recvDirection)
	if err != nil {
		return SessionCipher{}, err
	}
	prefixLen := send.NonceSize() - 8
	sendPrefix, err := hkdfBytes(params.hash, master, salt[:], []byte("fse-nonce-prefix-v1|"+sendDirection), prefixLen)
	if err != nil {
		return SessionCipher{}, err
	}
	recvPrefix, err := hkdfBytes(params.hash, master, salt[:], []byte("fse-nonce-prefix-v1|"+recvDirection), prefixLen)
	if err != nil {
		return SessionCipher{}, err
	}
	return SessionCipher{Send: send, Recv: recv, SendNoncePrefix: sendPrefix, RecvNoncePrefix: recvPrefix}, nil
}

func stretchMaster(params suiteParams, ikm, salt, transcript []byte) ([]byte, error) {
	secret := ikm
	if params.argonMem > 0 {
		secret = argon2.IDKey(ikm, salt, params.argonTime, uint32(params.argonMem)*1024, 1, 64)
	}
	return hkdfBytes(params.hash, secret, salt, append([]byte("fse-suite-v1|"), transcript...), 64)
}

func newTrafficAEAD(params suiteParams, master, salt []byte, direction string) (cipher.AEAD, error) {
	build := func(gen uint32) (cipher.AEAD, error) {
		return newCascade(params, master, salt, direction, gen)
	}
	inner, err := build(0)
	if err != nil {
		return nil, err
	}
	every := int64(params.rekeyMiB) << 20
	return &rekeyAEAD{build: build, every: every, inner: inner, nonceSize: inner.NonceSize(), overhead: inner.Overhead()}, nil
}

func newCascade(params suiteParams, master, salt []byte, direction string, gen uint32) (cipher.AEAD, error) {
	layers := make([]layerAEAD, 0, len(params.ciphers))
	for _, id := range params.ciphers {
		keyLen := 32
		if id == cipherAES128 {
			keyLen = 16
		}
		key, err := hkdfBytes(params.hash, master, salt, []byte(fmt.Sprintf("fse-traffic-v1|%s|%s|gen-%d", direction, id, gen)), keyLen)
		if err != nil {
			return nil, err
		}
		aead, err := newPrimitive(id, key)
		if err != nil {
			return nil, err
		}
		layers = append(layers, layerAEAD{name: string(id), aead: aead})
	}
	macs := make([]macFunc, 0, 2)
	if params.macHMAC {
		key, err := hkdfBytes(params.hash, master, salt, []byte(fmt.Sprintf("fse-mac-v1|%s|hmac-sha512|gen-%d", direction, gen)), 64)
		if err != nil {
			return nil, err
		}
		macs = append(macs, hmacSHA512(key))
	}
	if params.macBlake {
		key, err := hkdfBytes(params.hash, master, salt, []byte(fmt.Sprintf("fse-mac-v1|%s|blake2b-512|gen-%d", direction, gen)), 64)
		if err != nil {
			return nil, err
		}
		mac, err := blakeMAC(key)
		if err != nil {
			return nil, err
		}
		macs = append(macs, mac)
	}
	nonceSize := 12
	for _, id := range params.ciphers {
		if id == cipherXChaCha {
			nonceSize = 24
		}
	}
	overhead := 0
	for _, layer := range layers {
		overhead += layer.aead.Overhead()
	}
	for _, mac := range macs {
		overhead += mac.size
	}
	return &cascadeAEAD{layers: layers, macs: macs, nonceSize: nonceSize, overhead: overhead}, nil
}

func newPrimitive(id cipherID, key []byte) (cipher.AEAD, error) {
	switch id {
	case cipherAES128, cipherAES256:
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(block)
	case cipherChaCha:
		return chacha20poly1305.New(key)
	case cipherXChaCha:
		return chacha20poly1305.NewX(key)
	case cipherTwofish:
		block, err := twofish.NewCipher(key)
		if err != nil {
			return nil, err
		}
		return cipher.NewGCM(block)
	default:
		return nil, fmt.Errorf("unknown cipher %s", id)
	}
}

type layerAEAD struct {
	name string
	aead cipher.AEAD
}

type macFunc struct {
	size int
	sum  func(data []byte) []byte
}

type cascadeAEAD struct {
	layers    []layerAEAD
	macs      []macFunc
	nonceSize int
	overhead  int
}

func (c *cascadeAEAD) NonceSize() int { return c.nonceSize }
func (c *cascadeAEAD) Overhead() int  { return c.overhead }

func (c *cascadeAEAD) Seal(dst, nonce, plaintext, additionalData []byte) []byte {
	if len(nonce) != c.nonceSize {
		panic("crypto/cipher: incorrect nonce length given to Seal")
	}
	if len(additionalData) != 0 {
		panic("crypto/cipher: cascade AEAD does not take additional data")
	}
	buf := append([]byte(nil), plaintext...)
	for _, layer := range c.layers {
		inner, err := deriveInnerNonce(nonce, layer.name, layer.aead.NonceSize())
		if err != nil {
			panic("crypto/cipher: " + err.Error())
		}
		buf = layer.aead.Seal(nil, inner, buf, nil)
	}
	macInput := append(append([]byte{}, nonce...), buf...)
	for _, mac := range c.macs {
		buf = append(buf, mac.sum(macInput)...)
	}
	return append(dst, buf...)
}

func (c *cascadeAEAD) Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error) {
	if len(nonce) != c.nonceSize {
		return nil, errors.New("incorrect nonce length")
	}
	if len(additionalData) != 0 {
		return nil, errors.New("cascade AEAD does not take additional data")
	}
	if len(ciphertext) < c.overhead {
		return nil, errors.New("ciphertext too short")
	}
	buf := append([]byte(nil), ciphertext...)
	macStart := len(buf)
	for i := len(c.macs) - 1; i >= 0; i-- {
		macStart -= c.macs[i].size
	}
	body := buf[:macStart]
	macInput := append(append([]byte{}, nonce...), body...)
	offset := macStart
	for _, mac := range c.macs {
		got := buf[offset : offset+mac.size]
		offset += mac.size
		if !hmac.Equal(got, mac.sum(macInput)) {
			return nil, errors.New("message authentication failed")
		}
	}
	for i := len(c.layers) - 1; i >= 0; i-- {
		layer := c.layers[i]
		inner, err := deriveInnerNonce(nonce, layer.name, layer.aead.NonceSize())
		if err != nil {
			return nil, err
		}
		opened, err := layer.aead.Open(nil, inner, body, nil)
		if err != nil {
			return nil, err
		}
		body = opened
	}
	return append(dst, body...), nil
}

func deriveInnerNonce(outer []byte, name string, n int) ([]byte, error) {
	return hkdfBytes(sha256.New, outer, []byte("fse-inner-nonce-v1"), []byte(name), n)
}

type rekeyAEAD struct {
	build     func(gen uint32) (cipher.AEAD, error)
	every     int64
	used      int64
	gen       uint32
	inner     cipher.AEAD
	nonceSize int
	overhead  int
}

func (r *rekeyAEAD) NonceSize() int { return r.nonceSize }
func (r *rekeyAEAD) Overhead() int  { return r.overhead }

func (r *rekeyAEAD) Seal(dst, nonce, plaintext, additionalData []byte) []byte {
	if r.needsRatchet(len(plaintext)) {
		r.ratchet()
	}
	out := r.inner.Seal(dst, nonce, plaintext, additionalData)
	r.used += int64(len(plaintext))
	return out
}

func (r *rekeyAEAD) Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error) {
	plainLen := len(ciphertext) - r.overhead
	if plainLen < 0 {
		return nil, errors.New("ciphertext too short")
	}
	if r.needsRatchet(plainLen) {
		r.ratchet()
	}
	plain, err := r.inner.Open(dst, nonce, ciphertext, additionalData)
	if err != nil {
		return nil, err
	}
	r.used += int64(len(plain))
	return plain, nil
}

func (r *rekeyAEAD) needsRatchet(plainLen int) bool {
	return r.every > 0 && r.used > 0 && r.used+int64(plainLen) > r.every
}

func (r *rekeyAEAD) ratchet() {
	r.gen++
	r.used = 0
	next, err := r.build(r.gen)
	if err != nil {
		panic("crypto/cipher: " + err.Error())
	}
	r.inner = next
}

func hmacSHA512(key []byte) macFunc {
	return macFunc{size: sha512.Size, sum: func(data []byte) []byte {
		mac := hmac.New(sha512.New, key)
		_, _ = mac.Write(data)
		return mac.Sum(nil)
	}}
}

func blakeMAC(key []byte) (macFunc, error) {
	return macFunc{size: 64, sum: func(data []byte) []byte {
		mac, err := blake2b.New512(key)
		if err != nil {
			return make([]byte, 64)
		}
		_, _ = mac.Write(data)
		return mac.Sum(nil)
	}}, nil
}

func hkdfBytes(h func() hash.Hash, secret, salt, info []byte, n int) ([]byte, error) {
	reader := hkdf.New(h, secret, salt, info)
	out := make([]byte, n)
	if _, err := io.ReadFull(reader, out); err != nil {
		return nil, err
	}
	return out, nil
}

func newX25519() ([]byte, []byte, error) {
	curve := ecdh.X25519()
	private, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	return append([]byte(nil), private.Bytes()...), append([]byte(nil), private.PublicKey().Bytes()...), nil
}

func newX448() ([]byte, []byte, error) {
	var secret, public x448.Key
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, nil, err
	}
	x448.KeyGen(&public, &secret)
	return append([]byte(nil), secret[:]...), append([]byte(nil), public[:]...), nil
}

func classicalShared(kind string, private, peerPublic []byte) ([]byte, error) {
	switch kind {
	case "x25519":
		curve := ecdh.X25519()
		secret, err := curve.NewPrivateKey(private)
		if err != nil {
			return nil, err
		}
		public, err := curve.NewPublicKey(peerPublic)
		if err != nil {
			return nil, err
		}
		return secret.ECDH(public)
	case "x448":
		if len(private) != x448.Size || len(peerPublic) != x448.Size {
			return nil, fmt.Errorf("x448 key must be %d bytes", x448.Size)
		}
		var secret, public, shared x448.Key
		copy(secret[:], private)
		copy(public[:], peerPublic)
		if !x448.Shared(&shared, &secret, &public) {
			return nil, errors.New("x448 shared secret rejected")
		}
		return append([]byte(nil), shared[:]...), nil
	default:
		return nil, fmt.Errorf("unknown classical agreement %s", kind)
	}
}

func classicalSizeOK(kind string, public []byte) bool {
	switch kind {
	case "x25519":
		return len(public) == 32
	case "x448":
		return len(public) == x448.Size
	default:
		return false
	}
}

func newKEM(kind string) ([]byte, []byte, error) {
	seed := make([]byte, mlkem.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, nil, err
	}
	switch kind {
	case "ml-kem-768":
		key, err := mlkem.NewDecapsulationKey768(seed)
		if err != nil {
			return nil, nil, err
		}
		return seed, key.EncapsulationKey().Bytes(), nil
	case "ml-kem-1024":
		key, err := mlkem.NewDecapsulationKey1024(seed)
		if err != nil {
			return nil, nil, err
		}
		return seed, key.EncapsulationKey().Bytes(), nil
	default:
		return nil, nil, fmt.Errorf("unknown KEM %s", kind)
	}
}

func kemEncapsulate(kind string, public []byte) ([]byte, []byte, error) {
	switch kind {
	case "ml-kem-768":
		key, err := mlkem.NewEncapsulationKey768(public)
		if err != nil {
			return nil, nil, err
		}
		shared, ciphertext := key.Encapsulate()
		return shared, ciphertext, nil
	case "ml-kem-1024":
		key, err := mlkem.NewEncapsulationKey1024(public)
		if err != nil {
			return nil, nil, err
		}
		shared, ciphertext := key.Encapsulate()
		return shared, ciphertext, nil
	default:
		return nil, nil, fmt.Errorf("unknown KEM %s", kind)
	}
}

func kemDecapsulate(kind string, seed, ciphertext []byte) ([]byte, error) {
	switch kind {
	case "ml-kem-768":
		key, err := mlkem.NewDecapsulationKey768(seed)
		if err != nil {
			return nil, err
		}
		return key.Decapsulate(ciphertext)
	case "ml-kem-1024":
		key, err := mlkem.NewDecapsulationKey1024(seed)
		if err != nil {
			return nil, err
		}
		return key.Decapsulate(ciphertext)
	default:
		return nil, fmt.Errorf("unknown KEM %s", kind)
	}
}

func kemPublicSizeOK(kind string, public []byte) bool {
	switch kind {
	case "ml-kem-768":
		return len(public) == mlkem.EncapsulationKeySize768
	case "ml-kem-1024":
		return len(public) == mlkem.EncapsulationKeySize1024
	default:
		return false
	}
}

func pqScheme(name string) sign.Scheme {
	switch name {
	case "ml-dsa-65":
		return mldsa65.Scheme()
	case "ml-dsa-87":
		return mldsa87.Scheme()
	default:
		return nil
	}
}

func b64(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func clientOfferTranscript(m PeerMaterial) []byte {
	return []byte(fmt.Sprintf("fse-client-offer-v1\n%d\n%s\n%s\n%s\n%s\n%s\n%s",
		m.Level, m.Nonce, m.NodeID, b64(m.ClassicalPublic), b64(m.KEMPublic), b64(m.PQPublic), b64(m.SLHPublic)))
}

func serverOfferTranscript(client, server PeerMaterial, sessionLevel int) []byte {
	return []byte(fmt.Sprintf("fse-server-offer-v1\n%d\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s",
		sessionLevel, client.Nonce, server.Nonce, client.NodeID, server.NodeID,
		b64(client.ClassicalPublic), b64(server.ClassicalPublic), b64(client.KEMPublic), b64(server.KEMCiphertext),
		b64(client.PQPublic), b64(server.PQPublic), b64(client.SLHPublic), b64(server.SLHPublic)))
}

func kdfTranscript(level int, client, server PeerMaterial) []byte {
	return []byte(fmt.Sprintf("fse-kdf-v1\n%d\n%s\n%s\n%s\n%s\n%s\n%s\n%s\n%s",
		level, client.Nonce, server.Nonce, client.NodeID, server.NodeID,
		b64(client.ClassicalPublic), b64(server.ClassicalPublic), b64(client.KEMPublic), b64(server.KEMCiphertext)))
}
