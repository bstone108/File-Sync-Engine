package peeridentity

import "testing"

func TestSignedHelloAuthenticatesPeerIdentityAndEncryptionLevel(t *testing.T) {
	local, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	remote, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity remote: %v", err)
	}

	hello, err := SignHello(local, "node-a", 5, []byte("session-nonce"))
	if err != nil {
		t.Fatalf("SignHello: %v", err)
	}
	if hello.NodeID != "node-a" {
		t.Fatalf("node id = %q", hello.NodeID)
	}
	if hello.EncryptionLevel != 5 {
		t.Fatalf("encryption level = %d", hello.EncryptionLevel)
	}
	if hello.PublicKey == "" || hello.Signature == "" {
		t.Fatalf("signed hello did not include public key and signature: %+v", hello)
	}
	if err := VerifyHello(hello, local.PublicKey, []byte("session-nonce")); err != nil {
		t.Fatalf("VerifyHello with expected identity: %v", err)
	}
	if err := VerifyHello(hello, remote.PublicKey, []byte("session-nonce")); err == nil {
		t.Fatalf("VerifyHello accepted a different peer identity")
	}
}

func TestSignedHelloRejectsTamperedEncryptionLevel(t *testing.T) {
	identity, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	hello, err := SignHello(identity, "node-a", 4, []byte("nonce"))
	if err != nil {
		t.Fatalf("SignHello: %v", err)
	}
	hello.EncryptionLevel = 10
	if err := VerifyHello(hello, identity.PublicKey, []byte("nonce")); err == nil {
		t.Fatalf("VerifyHello accepted tampered encryption level")
	}
}

func TestValidateEncryptionLevelBounds(t *testing.T) {
	for _, level := range []int{0, 1, 5, 10} {
		if err := ValidateEncryptionLevel(level); err != nil {
			t.Fatalf("level %d should be valid: %v", level, err)
		}
	}
	for _, level := range []int{-1, 11} {
		if err := ValidateEncryptionLevel(level); err == nil {
			t.Fatalf("level %d should be rejected", level)
		}
	}
}
