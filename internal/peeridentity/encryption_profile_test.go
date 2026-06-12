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
		{level: 5, name: "standard-strong", requiresAEAD: true, kdf: "hkdf-sha256"},
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
