package streamsync

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"filesyncengine/internal/peeridentity"
	"filesyncengine/internal/protocol"
)

func negotiateEncryptionLevel(localLevel, peerLevel int, allowWeaker bool) (int, error) {
	return peeridentity.NegotiateSessionLevel(localLevel, peerLevel, allowWeaker)
}

func signedProtocolHello(nodeID string, identity peeridentity.Identity, encryptionLevel int, nonce string, transfer *protocol.TransferLimits) (*protocol.Hello, error) {
	hello, _, err := signedInitiatorHello(nodeID, identity, encryptionLevel, nonce, transfer, nil)
	return hello, err
}

func signedInitiatorHello(nodeID string, identity peeridentity.Identity, encryptionLevel int, nonce string, transfer *protocol.TransferLimits, capabilities []string) (*protocol.Hello, *peeridentity.PendingHandshake, error) {
	pending, err := peeridentity.BeginInitiator(encryptionLevel, nonce, nodeID)
	if err != nil {
		return nil, nil, err
	}
	hello, err := helloFromPending(nodeID, identity, pending, transfer, capabilities)
	if err != nil {
		return nil, nil, err
	}
	return hello, pending, nil
}

func signedResponderHello(nodeID string, identity peeridentity.Identity, level int, client *protocol.Hello, transfer *protocol.TransferLimits, capabilities []string) (*protocol.Hello, *peeridentity.PendingHandshake, peeridentity.SessionCipher, error) {
	clientMaterial, err := materialFromHello(client)
	if err != nil {
		return nil, nil, peeridentity.SessionCipher{}, err
	}
	nonce, err := newNonce()
	if err != nil {
		return nil, nil, peeridentity.SessionCipher{}, err
	}
	pending, err := peeridentity.BeginResponder(level, nonce, nodeID, clientMaterial)
	if err != nil {
		return nil, nil, peeridentity.SessionCipher{}, err
	}
	session, err := pending.Finish(clientMaterial)
	if err != nil {
		return nil, nil, peeridentity.SessionCipher{}, err
	}
	hello, err := helloFromPending(nodeID, identity, pending, transfer, capabilities)
	if err != nil {
		return nil, nil, peeridentity.SessionCipher{}, err
	}
	return hello, pending, session, nil
}

func helloFromPending(nodeID string, identity peeridentity.Identity, pending *peeridentity.PendingHandshake, transfer *protocol.TransferLimits, capabilities []string) (*protocol.Hello, error) {
	material := pending.Material()
	hello := &protocol.Hello{
		ProtocolVersion: 1,
		NodeID:          nodeID,
		Capabilities:    capabilities,
		EncryptionLevel: material.Level,
		Transfer:        transfer,
		Nonce:           material.Nonce,
	}
	applyMaterial(hello, material)
	if material.Level == 0 && identity.PrivateKey == "" {
		return hello, nil
	}
	if identity.PrivateKey == "" {
		hello.PublicKey = identity.PublicKey
		return hello, nil
	}
	signed, err := peeridentity.SignBoundHello(identity, signedHelloFromProtocol(hello), []byte(material.Nonce))
	if err != nil {
		return nil, err
	}
	hello.PublicKey = signed.PublicKey
	hello.Signature = signed.Signature
	return hello, nil
}

func applyMaterial(hello *protocol.Hello, material peeridentity.PeerMaterial) {
	hello.SessionPublicKey = encodeKey(material.ClassicalPublic)
	hello.SessionKEMPublic = encodeKey(material.KEMPublic)
	hello.SessionKEMCiphertext = encodeKey(material.KEMCiphertext)
	hello.SessionPQPublic = encodeKey(material.PQPublic)
	hello.SessionPQSignature = encodeKey(material.PQSignature)
	hello.SessionSLHPublic = encodeKey(material.SLHPublic)
	hello.SessionSLHSignature = encodeKey(material.SLHSignature)
}

func encodeKey(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func decodeKey(encoded string) ([]byte, error) {
	if encoded == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode handshake field: %w", err)
	}
	return decoded, nil
}

func materialFromHello(hello *protocol.Hello) (peeridentity.PeerMaterial, error) {
	if hello == nil {
		return peeridentity.PeerMaterial{}, fmt.Errorf("hello payload required")
	}
	classical, err := decodeKey(hello.SessionPublicKey)
	if err != nil {
		return peeridentity.PeerMaterial{}, err
	}
	kemPublic, err := decodeKey(hello.SessionKEMPublic)
	if err != nil {
		return peeridentity.PeerMaterial{}, err
	}
	kemCiphertext, err := decodeKey(hello.SessionKEMCiphertext)
	if err != nil {
		return peeridentity.PeerMaterial{}, err
	}
	pqPublic, err := decodeKey(hello.SessionPQPublic)
	if err != nil {
		return peeridentity.PeerMaterial{}, err
	}
	pqSignature, err := decodeKey(hello.SessionPQSignature)
	if err != nil {
		return peeridentity.PeerMaterial{}, err
	}
	slhPublic, err := decodeKey(hello.SessionSLHPublic)
	if err != nil {
		return peeridentity.PeerMaterial{}, err
	}
	slhSignature, err := decodeKey(hello.SessionSLHSignature)
	if err != nil {
		return peeridentity.PeerMaterial{}, err
	}
	return peeridentity.PeerMaterial{
		Level:           hello.EncryptionLevel,
		Nonce:           hello.Nonce,
		NodeID:          hello.NodeID,
		ClassicalPublic: classical,
		KEMPublic:       kemPublic,
		KEMCiphertext:   kemCiphertext,
		PQPublic:        pqPublic,
		PQSignature:     pqSignature,
		SLHPublic:       slhPublic,
		SLHSignature:    slhSignature,
	}, nil
}

func signedHelloFromProtocol(hello *protocol.Hello) peeridentity.SignedHello {
	if hello == nil {
		return peeridentity.SignedHello{}
	}
	return peeridentity.SignedHello{
		NodeID:               hello.NodeID,
		PublicKey:            hello.PublicKey,
		SessionPublicKey:     hello.SessionPublicKey,
		SessionKEMPublic:     hello.SessionKEMPublic,
		SessionKEMCiphertext: hello.SessionKEMCiphertext,
		SessionPQPublic:      hello.SessionPQPublic,
		SessionPQSignature:   hello.SessionPQSignature,
		SessionSLHPublic:     hello.SessionSLHPublic,
		SessionSLHSignature:  hello.SessionSLHSignature,
		EncryptionLevel:      hello.EncryptionLevel,
		Signature:            hello.Signature,
	}
}

func codecForSession(stream io.ReadWriter, session peeridentity.SessionCipher) *protocol.Codec {
	if session.Send == nil || session.Recv == nil {
		return protocol.NewCodec(stream, stream)
	}
	return protocol.NewEncryptedCodec(stream, stream, session.Send, session.Recv, session.SendNoncePrefix, session.RecvNoncePrefix)
}

func clientHandshake(stream io.ReadWriter, opts PullOptions, peerPublicKey string, capabilities []string) (*protocol.Codec, *protocol.Hello, error) {
	if peerPublicKey == "" {
		peerPublicKey = opts.PeerPublicKey
	}
	level := opts.EncryptionLevel
	codec := protocol.NewCodec(stream, stream)
	for attempt := 0; attempt < 2; attempt++ {
		nonce, err := newNonce()
		if err != nil {
			return nil, nil, err
		}
		hello, pending, err := signedInitiatorHello(opts.NodeID, opts.Identity, level, nonce, advertisedTransferLimits(opts.Transfer, opts.Peer), capabilities)
		if err != nil {
			return nil, nil, err
		}
		if err := codec.Write(protocol.Message{Type: protocol.MessageHello, Hello: hello}); err != nil {
			return nil, nil, err
		}
		msg, err := codec.Read()
		if err != nil {
			return nil, nil, err
		}
		if msg.Type == protocol.MessageError {
			if msg.Error != nil && msg.Error.Code == peeridentity.EncryptionRetryCode && attempt == 0 {
				next, ok := peeridentity.ParseRetryLevel(msg.Error.Message)
				if !ok || next <= level {
					return nil, nil, fmt.Errorf("peer error %s: %s", msg.Error.Code, msg.Error.Message)
				}
				level = next
				continue
			}
			if msg.Error == nil {
				return nil, nil, fmt.Errorf("peer error")
			}
			return nil, nil, fmt.Errorf("peer error %s: %s", msg.Error.Code, msg.Error.Message)
		}
		if msg.Type != protocol.MessageHello {
			return nil, nil, fmt.Errorf("expected hello, got %s", msg.Type)
		}
		if err := verifyServerHello(msg.Hello, peerPublicKey, msg.Hello.Nonce); err != nil {
			return nil, nil, err
		}
		if msg.Hello.EncryptionLevel < opts.EncryptionLevel && !opts.AllowWeakerEncryptionLevel {
			return nil, nil, fmt.Errorf("peer negotiated weaker encryption level %d below local level %d", msg.Hello.EncryptionLevel, opts.EncryptionLevel)
		}
		if err := peeridentity.ValidateEncryptionLevel(msg.Hello.EncryptionLevel); err != nil {
			return nil, nil, err
		}
		if msg.Hello.EncryptionLevel == 0 {
			return codec, msg.Hello, nil
		}
		serverMaterial, err := materialFromHello(msg.Hello)
		if err != nil {
			return nil, nil, err
		}
		session, err := pending.Finish(serverMaterial)
		if err != nil {
			return nil, nil, err
		}
		return codecForSession(stream, session), msg.Hello, nil
	}
	return nil, nil, errors.New("encryption handshake retry failed")
}

func encryptionRetry(err error) *protocol.Error {
	var retry *peeridentity.HandshakeRetryError
	if errors.As(err, &retry) {
		return &protocol.Error{Code: peeridentity.EncryptionRetryCode, Message: retry.Error()}
	}
	return &protocol.Error{Code: "encryption_rejected", Message: err.Error()}
}
