package peeridentity

import "testing"

func TestDefaultEncryptionLevelIsOrdinaryPeerPairLevelFour(t *testing.T) {
	if DefaultEncryptionLevel != 4 {
		t.Fatalf("DefaultEncryptionLevel = %d, want ordinary peer-pair default level 4", DefaultEncryptionLevel)
	}
}

func TestEncryptionProfileMapsNamedStrengthBands(t *testing.T) {
	cases := []struct {
		level        int
		name         string
		requiresAEAD bool
		kdf          string
	}{
		{level: 0, name: "none-debug", requiresAEAD: false, kdf: "none"},
		{level: 1, name: "minimal-permissive", requiresAEAD: true, kdf: "hkdf-sha256"},
		{level: 5, name: "standard-strong", requiresAEAD: true, kdf: "hkdf-sha384"},
		{level: 10, name: "maximum-high-cpu", requiresAEAD: true, kdf: "argon2id+hkdf-sha512"},
	}

	for _, tc := range cases {
		profile, err := EncryptionProfile(tc.level)
		if err != nil {
			t.Fatalf("EncryptionProfile(%d): %v", tc.level, err)
		}
		if profile.Level != tc.level || profile.Name != tc.name || profile.RequiresAEAD != tc.requiresAEAD || profile.KDF != tc.kdf {
			t.Fatalf("EncryptionProfile(%d) = %+v", tc.level, profile)
		}
	}
}

func TestEncryptionProfileRejectsInvalidLevel(t *testing.T) {
	if _, err := EncryptionProfile(11); err == nil {
		t.Fatal("expected invalid encryption level error")
	}
}

func TestEncryptionProfilesScaleMonotonically(t *testing.T) {
	var previous EncryptionProfileSpec
	for level := 0; level <= 10; level++ {
		profile, err := EncryptionProfile(level)
		if err != nil {
			t.Fatalf("EncryptionProfile(%d): %v", level, err)
		}
		if profile.Level != level || profile.Explanation == "" {
			t.Fatalf("profile %d missing explanation: %+v", level, profile)
		}
		if level == 0 {
			if !profile.Plaintext || profile.RequiresAEAD || profile.AEAD != "none" {
				t.Fatalf("level 0 must be plaintext: %+v", profile)
			}
			previous = profile
			continue
		}
		if profile.RekeyEveryMiB <= 0 || (previous.RekeyEveryMiB > 0 && profile.RekeyEveryMiB > previous.RekeyEveryMiB) {
			t.Fatalf("rekey interval increased at level %d: %d -> %d", level, previous.RekeyEveryMiB, profile.RekeyEveryMiB)
		}
		if profile.MemoryCostMiB < previous.MemoryCostMiB || profile.Iterations < previous.Iterations {
			t.Fatalf("KDF cost dropped at level %d: %+v previous %+v", level, profile, previous)
		}
		if cipherRank(profile.AEAD) < cipherRank(previous.AEAD) || kemRank(profile.KeyAgreement) < kemRank(previous.KeyAgreement) || macRank(profile.MAC) < macRank(previous.MAC) || signatureRank(profile.Signatures) < signatureRank(previous.Signatures) {
			t.Fatalf("suite strength dropped at level %d: %+v previous %+v", level, profile, previous)
		}
		previous = profile
	}
	operating, err := EncryptionProfile(DefaultEncryptionLevel)
	if err != nil {
		t.Fatal(err)
	}
	if operating.KeyAgreement != "x25519" || operating.AEAD != "aes-256-gcm+chacha20-poly1305" {
		t.Fatalf("level 4 = %+v", operating)
	}
	identity, err := EncryptionProfile(IdentityExchangeEncryptionLevel)
	if err != nil {
		t.Fatal(err)
	}
	if identity.KeyAgreement != "x448+ml-kem-1024" || identity.KDF != "argon2id+hkdf-sha512" || identity.MAC != "hmac-sha512+blake2b-512" {
		t.Fatalf("level 10 = %+v", identity)
	}
}

func cipherRank(aead string) int {
	switch aead {
	case "none":
		return 0
	case "aes-128-gcm":
		return 1
	case "chacha20-poly1305":
		return 2
	case "aes-256-gcm":
		return 3
	case "aes-256-gcm+chacha20-poly1305":
		return 4
	case "xchacha20-poly1305+aes-256-gcm":
		return 5
	case "xchacha20-poly1305+aes-256-gcm+twofish-256-gcm":
		return 6
	default:
		return -1
	}
}

func kemRank(agreement string) int {
	switch agreement {
	case "none":
		return 0
	case "x25519":
		return 1
	case "x25519+ml-kem-768":
		return 2
	case "x448+ml-kem-1024":
		return 3
	default:
		return -1
	}
}

func macRank(mac string) int {
	switch mac {
	case "none":
		return 0
	case "aead":
		return 1
	case "hmac-sha512":
		return 2
	case "blake2b-512":
		return 3
	case "hmac-sha512+blake2b-512":
		return 4
	default:
		return -1
	}
}

func signatureRank(signatures string) int {
	switch signatures {
	case "ed25519":
		return 1
	case "ed25519+ml-dsa-65":
		return 2
	case "ed25519+ml-dsa-87+slh-dsa-sha2-256s":
		return 3
	default:
		return -1
	}
}
