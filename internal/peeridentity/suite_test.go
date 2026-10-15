package peeridentity

import (
	"bytes"
	"crypto/cipher"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestEveryEncryptionLevelRoundTrips(t *testing.T) {
	payload := []byte("folder bytes stay confidential")
	for level := 0; level <= 10; level++ {
		level := level
		t.Run(fmt.Sprintf("level-%d", level), func(t *testing.T) {
			client, server, clientKeys, serverKeys := handshakeAt(t, level, level)
			if level == 0 {
				if clientKeys.Send != nil || serverKeys.Send != nil {
					t.Fatal("level 0 produced a payload cipher")
				}
				opened := append([]byte(nil), payload...)
				if !bytes.Equal(opened, payload) {
					t.Fatal("plaintext round trip changed the payload")
				}
				return
			}
			if client == nil || server == nil {
				t.Fatal("missing handshake")
			}
			nonce := make([]byte, clientKeys.Send.NonceSize())
			nonce[len(nonce)-1] = 1
			sealed := clientKeys.Send.Seal(nil, nonce, payload, nil)
			if bytes.Contains(sealed, payload) {
				t.Fatalf("level %d leaked plaintext", level)
			}
			opened, err := serverKeys.Recv.Open(nil, nonce, sealed, nil)
			if err != nil || !bytes.Equal(opened, payload) {
				t.Fatalf("level %d open: %v %q", level, err, opened)
			}
			nonce[len(nonce)-1] = 2
			reply := serverKeys.Send.Seal(nil, nonce, payload, nil)
			opened, err = clientKeys.Recv.Open(nil, nonce, reply, nil)
			if err != nil || !bytes.Equal(opened, payload) {
				t.Fatalf("level %d reply open: %v", level, err)
			}
		})
	}
}

func TestPeersAtDifferentLevelsUseTheHigherSuite(t *testing.T) {
	_, _, clientKeys, serverKeys := handshakeAt(t, 3, 6)
	payload := []byte("raised to the higher peer")
	nonce := make([]byte, clientKeys.Send.NonceSize())
	nonce[len(nonce)-1] = 3
	sealed := clientKeys.Send.Seal(nil, nonce, payload, nil)
	opened, err := serverKeys.Recv.Open(nil, nonce, sealed, nil)
	if err != nil || !bytes.Equal(opened, payload) {
		t.Fatalf("same-curve interop open: %v", err)
	}

	low, err := BeginInitiator(4, "client-nonce", "client")
	if err != nil {
		t.Fatal(err)
	}
	err = AssessMaterial(8, low.Material())
	var retry *HandshakeRetryError
	if !errors.As(err, &retry) || retry.Level != 8 || retry.Error() != "retry handshake at encryption level 8" {
		t.Fatalf("retry = %v", err)
	}
	if got, ok := ParseRetryLevel(retry.Error()); !ok || got != 8 {
		t.Fatalf("parsed retry = %d %v", got, ok)
	}
	_, _, clientKeys, serverKeys = handshakeAt(t, 8, 8)
	nonce = make([]byte, clientKeys.Send.NonceSize())
	nonce[len(nonce)-1] = 4
	sealed = clientKeys.Send.Seal(nil, nonce, payload, nil)
	opened, err = serverKeys.Recv.Open(nil, nonce, sealed, nil)
	if err != nil || !bytes.Equal(opened, payload) {
		t.Fatalf("hybrid interop open: %v", err)
	}
}

func TestMissingStrongerMaterialDoesNotSilentlyDowngrade(t *testing.T) {
	offered, err := BeginInitiator(4, "client-nonce", "client")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BeginResponder(8, "server-nonce", "server", offered.Material()); err == nil {
		t.Fatal("level 4 material opened a level 8 session")
	} else if !errors.As(err, new(*HandshakeRetryError)) {
		t.Fatalf("expected retry, got %v", err)
	}
	client, err := BeginInitiator(8, "client-nonce", "client")
	if err != nil {
		t.Fatal(err)
	}
	server, err := BeginResponder(8, "server-nonce", "server", client.Material())
	if err != nil {
		t.Fatal(err)
	}
	broken := server.Material()
	broken.KEMCiphertext = nil
	if _, err := client.Finish(broken); err == nil {
		t.Fatal("missing KEM ciphertext still derived traffic keys")
	}
	stripped := client.Material()
	stripped.KEMPublic = nil
	if _, err := BeginResponder(8, "server-nonce", "server", stripped); err == nil {
		t.Fatal("stripped KEM public key was accepted")
	}
}

func TestNegotiateSessionLevelUsesTheHigherConfiguredLevel(t *testing.T) {
	got, err := NegotiateSessionLevel(4, 6)
	if err != nil || got != 6 {
		t.Fatalf("max(4, 6) = %d %v", got, err)
	}
	got, err = NegotiateSessionLevel(6, 4)
	if err != nil || got != 6 {
		t.Fatalf("max(6, 4) = %d %v", got, err)
	}
	got, err = NegotiateSessionLevel(6, 6)
	if err != nil || got != 6 {
		t.Fatalf("max(6, 6) = %d %v", got, err)
	}
}

func TestFinishRejectsPeerOfferBelowLocalLevel(t *testing.T) {
	client, err := BeginInitiator(6, "client-nonce", "client")
	if err != nil {
		t.Fatal(err)
	}
	lower, err := BeginInitiator(4, "forged-nonce", "server")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Finish(lower.Material()); err == nil || !strings.Contains(err.Error(), "weaker encryption level") {
		t.Fatalf("expected lower offer rejection, got %v", err)
	}
}

func TestInnerNonceIncludesTheFullCounter(t *testing.T) {
	outer := bytes.Repeat([]byte{0x11}, 24)
	changed := append([]byte(nil), outer...)
	changed[23] = 0x12
	first, err := deriveInnerNonce(outer, "aes-256-gcm", 12)
	if err != nil {
		t.Fatal(err)
	}
	second, err := deriveInnerNonce(changed, "aes-256-gcm", 12)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) || bytes.Equal(first, outer[:12]) {
		t.Fatalf("inner nonce dropped the counter: %x vs %x", first, second)
	}
}

func TestRekeyChangesKeyBeforeTheNextMessageCrosses(t *testing.T) {
	params := suiteFor(1)
	master := bytes.Repeat([]byte{7}, 64)
	salt := bytes.Repeat([]byte{9}, 32)
	build := func(gen uint32) (cipher.AEAD, error) {
		return newCascade(params, master, salt, "c2s", gen)
	}
	inner, err := build(0)
	if err != nil {
		t.Fatal(err)
	}
	send := &rekeyAEAD{build: build, every: 16, inner: inner, nonceSize: inner.NonceSize(), overhead: inner.Overhead()}
	recvInner, err := build(0)
	if err != nil {
		t.Fatal(err)
	}
	recv := &rekeyAEAD{build: build, every: 16, inner: recvInner, nonceSize: recvInner.NonceSize(), overhead: recvInner.Overhead()}
	nonce := make([]byte, send.NonceSize())
	nonce[len(nonce)-1] = 1
	first := send.Seal(nil, nonce, []byte("0123456789"), nil)
	nonce[len(nonce)-1] = 2
	second := send.Seal(nil, nonce, []byte("0123456789"), nil)
	nonce[len(nonce)-1] = 1
	if opened, err := recv.Open(nil, nonce, first, nil); err != nil || string(opened) != "0123456789" {
		t.Fatalf("first open: %v %q", err, opened)
	}
	nonce[len(nonce)-1] = 2
	if opened, err := recv.Open(nil, nonce, second, nil); err != nil || string(opened) != "0123456789" {
		t.Fatalf("second open: %v %q", err, opened)
	}
	stale, err := build(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stale.Open(nil, nonce, second, nil); err == nil {
		t.Fatal("message after the rekey threshold still used the previous key")
	}
}

func handshakeAt(t *testing.T, clientLevel, serverLevel int) (*PendingHandshake, *PendingHandshake, SessionCipher, SessionCipher) {
	t.Helper()
	client, err := BeginInitiator(clientLevel, "client-nonce", "client")
	if err != nil {
		t.Fatalf("initiator %d: %v", clientLevel, err)
	}
	server, err := BeginResponder(serverLevel, "server-nonce", "server", client.Material())
	if err != nil {
		t.Fatalf("responder %d: %v", serverLevel, err)
	}
	serverKeys, err := server.Finish(client.Material())
	if err != nil {
		t.Fatal(err)
	}
	clientKeys, err := client.Finish(server.Material())
	if err != nil {
		t.Fatal(err)
	}
	return client, server, clientKeys, serverKeys
}
