package ci

import (
	"bytes"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const sparklePublicEDKey = "dV+k5IynR3jrGAA7dbDmr66A2rrOH3vPbc45CVcuGUE="

const sparkleEdDSATestKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

// Dummy 88-char base64 (64-byte expanded Ed25519 private key). Not a real secret.
const sparkleEdDSATestKey88 = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="

const stubSignUpdate = `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  printf 'usage: sign_update [--ed-key-file FILE] ARCHIVE\n'
  exit 0
fi
method="unknown"
key_len=0
if [[ "${1:-}" == "--ed-key-file" && "${2:-}" == "-" ]]; then
  method="stdin"
  IFS= read -r key || true
  key_len=${#key}
elif [[ "${1:-}" == "--ed-key-file" ]]; then
  method="file"
  if [[ -f "${2:-}" ]]; then
    key_len="$(wc -c < "$2" | tr -d ' ')"
  fi
fi
printf 'stub method=%s key_len=%s\n' "$method" "$key_len" >&2
if [[ "${STUB_SIGN_UPDATE_REJECT_STDIN:-}" == "1" && "$method" == "stdin" ]]; then
  printf 'ERROR! Unable to read EdDSA private key from standard input\n'
  exit 1
fi
if [[ "${STUB_SIGN_UPDATE_FAIL:-}" == "1" ]]; then
  printf 'ERROR! simulated sign_update failure on stdout\n'
  exit 1
fi
printf 'sparkle:edSignature="dGVzdHNpZ25hdHVyZVZhbHVlQmFzZTY0UGFkZGluZw==" length="12"\n'
`

func TestSignSparkleAppcastUsesStdinAndSurfacesSignUpdateOutput(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	sparkleDir := filepath.Join(tmp, "sparkle")
	zipPath := filepath.Join(tmp, "payload.zip")
	outXML := filepath.Join(tmp, "appcast-darwin-arm64.xml")
	mustWriteFile(t, zipPath, "hello sparkle")
	mustMkdir(t, filepath.Join(sparkleDir, "Sparkle.framework"))
	mustWriteExecutable(t, filepath.Join(sparkleDir, "bin", "sign_update"), stubSignUpdate)

	cmd := exec.Command("bash", "scripts/sign-sparkle-appcast.sh", zipPath, "2026.8.28.1", "arm64", outXML)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"FSE_SPARKLE_DIR="+sparkleDir,
		"SPARKLE_EDDSA_PRIVATE_KEY="+sparkleEdDSATestKey,
	)
	output, err := cmd.CombinedOutput()
	got := string(output)
	if err != nil {
		t.Fatalf("appcast signer failed: %v\n%s", err, got)
	}
	if strings.Contains(got, sparkleEdDSATestKey) {
		t.Fatal("appcast signer leaked the EdDSA private key into logs")
	}
	if !strings.Contains(got, "stub method=stdin") {
		t.Fatalf("expected sign_update to be invoked via stdin; output:\n%s", got)
	}
	body, err := os.ReadFile(outXML)
	if err != nil {
		t.Fatalf("read appcast: %v", err)
	}
	xml := string(body)
	for _, want := range []string{
		`sparkle:edSignature="dGVzdHNpZ25hdHVyZVZhbHVlQmFzZTY0UGFkZGluZw=="`,
		`length="12"`,
		"fse-desktop-darwin-arm64-installer-2026.8.28.1.zip",
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("appcast XML missing %q:\n%s", want, xml)
		}
	}
}

func TestSignSparkleAppcastSurfacesSignUpdateStdoutOnFailure(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	sparkleDir := filepath.Join(tmp, "sparkle")
	zipPath := filepath.Join(tmp, "payload.zip")
	outXML := filepath.Join(tmp, "appcast.xml")
	mustWriteFile(t, zipPath, "hello sparkle")
	mustMkdir(t, filepath.Join(sparkleDir, "Sparkle.framework"))
	mustWriteExecutable(t, filepath.Join(sparkleDir, "bin", "sign_update"), stubSignUpdate)

	cmd := exec.Command("bash", "scripts/sign-sparkle-appcast.sh", zipPath, "2026.8.28.1", "amd64", outXML)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"FSE_SPARKLE_DIR="+sparkleDir,
		"SPARKLE_EDDSA_PRIVATE_KEY="+sparkleEdDSATestKey,
		"STUB_SIGN_UPDATE_FAIL=1",
	)
	output, err := cmd.CombinedOutput()
	got := string(output)
	if err == nil {
		t.Fatalf("appcast signer unexpectedly succeeded:\n%s", got)
	}
	if !strings.Contains(got, "ERROR! simulated sign_update failure on stdout") {
		t.Fatalf("sign_update stdout was swallowed; output:\n%s", got)
	}
	if !strings.Contains(got, "sign_update --help") {
		t.Fatalf("failure diagnostics missing sign_update --help; output:\n%s", got)
	}
	if strings.Contains(got, sparkleEdDSATestKey) {
		t.Fatal("appcast signer leaked the EdDSA private key into logs")
	}
	if _, statErr := os.Stat(outXML); !os.IsNotExist(statErr) {
		t.Fatal("failed signing must not write an appcast")
	}
}

func TestSignSparkleAppcastFallsBackToKeyFileWhenStdinRejected(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	sparkleDir := filepath.Join(tmp, "sparkle")
	zipPath := filepath.Join(tmp, "payload.zip")
	outXML := filepath.Join(tmp, "appcast.xml")
	mustWriteFile(t, zipPath, "hello sparkle")
	mustMkdir(t, filepath.Join(sparkleDir, "Sparkle.framework"))
	mustWriteExecutable(t, filepath.Join(sparkleDir, "bin", "sign_update"), stubSignUpdate)

	cmd := exec.Command("bash", "scripts/sign-sparkle-appcast.sh", zipPath, "2026.8.28.1", "arm64", outXML)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"FSE_SPARKLE_DIR="+sparkleDir,
		"SPARKLE_EDDSA_PRIVATE_KEY="+sparkleEdDSATestKey,
		"STUB_SIGN_UPDATE_REJECT_STDIN=1",
	)
	output, err := cmd.CombinedOutput()
	got := string(output)
	if err != nil {
		t.Fatalf("file fallback should succeed after stdin rejection: %v\n%s", err, got)
	}
	if !strings.Contains(got, "ERROR! Unable to read EdDSA private key from standard input") {
		t.Fatalf("stdin rejection must stay visible in the log; output:\n%s", got)
	}
	if !strings.Contains(got, "stub method=file") {
		t.Fatalf("expected temp-file fallback; output:\n%s", got)
	}
	if strings.Contains(got, sparkleEdDSATestKey) {
		t.Fatal("appcast signer leaked the EdDSA private key into logs")
	}
}

func TestSignSparkleAppcastAcceptsExpandedEd25519PrivateKeyLength88(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	sparkleDir := filepath.Join(tmp, "sparkle")
	zipPath := filepath.Join(tmp, "payload.zip")
	outXML := filepath.Join(tmp, "appcast.xml")
	mustWriteFile(t, zipPath, "hello sparkle")
	mustMkdir(t, filepath.Join(sparkleDir, "Sparkle.framework"))
	mustWriteExecutable(t, filepath.Join(sparkleDir, "bin", "sign_update"), stubSignUpdate)

	if len(sparkleEdDSATestKey88) != 88 {
		t.Fatalf("test fixture must be 88 chars, got %d", len(sparkleEdDSATestKey88))
	}
	cmd := exec.Command("bash", "scripts/sign-sparkle-appcast.sh", zipPath, "2026.8.28.4", "arm64", outXML)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"FSE_SPARKLE_DIR="+sparkleDir,
		"SPARKLE_EDDSA_PRIVATE_KEY="+sparkleEdDSATestKey88,
	)
	output, err := cmd.CombinedOutput()
	got := string(output)
	if err != nil {
		t.Fatalf("88-char Sparkle key must reach sign_update: %v\n%s", err, got)
	}
	if strings.Contains(got, "unexpected length") {
		t.Fatalf("length 88 must not be rejected: output:\n%s", got)
	}
	if !strings.Contains(got, "normalized Sparkle EdDSA secret: decoded 64 bytes -> old-96 (96 bytes)") {
		t.Fatalf("64-byte expanded key must be rewritten to Sparkle old-96; output:\n%s", got)
	}
	if !strings.Contains(got, "stub method=stdin key_len=128") {
		t.Fatalf("expected stdin sign_update with 96-byte old-format key; output:\n%s", got)
	}
	if strings.Contains(got, sparkleEdDSATestKey88) {
		t.Fatal("appcast signer leaked the EdDSA private key into logs")
	}
	if _, err := os.Stat(outXML); err != nil {
		t.Fatalf("expected appcast XML: %v", err)
	}
}

func TestSignSparkleAppcastNormalizesLibsodiumSecretToSeed(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	sparkleDir := filepath.Join(tmp, "sparkle")
	zipPath := filepath.Join(tmp, "payload.zip")
	outXML := filepath.Join(tmp, "appcast.xml")
	mustWriteFile(t, zipPath, "hello sparkle")
	mustMkdir(t, filepath.Join(sparkleDir, "Sparkle.framework"))
	mustWriteExecutable(t, filepath.Join(sparkleDir, "bin", "sign_update"), stubSignUpdate)

	pub, err := base64.StdEncoding.DecodeString(sparklePublicEDKey)
	if err != nil || len(pub) != 32 {
		t.Fatalf("SUPublicEDKey fixture: %v len=%d", err, len(pub))
	}
	secret := append(bytes.Repeat([]byte{0x11}, 32), pub...)
	key := base64.StdEncoding.EncodeToString(secret)
	if len(key) != 88 {
		t.Fatalf("libsodium-style fixture must be 88 chars, got %d", len(key))
	}

	cmd := exec.Command("bash", "scripts/sign-sparkle-appcast.sh", zipPath, "2026.8.28.5", "amd64", outXML)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"FSE_SPARKLE_DIR="+sparkleDir,
		"SPARKLE_EDDSA_PRIVATE_KEY="+key,
	)
	output, err := cmd.CombinedOutput()
	got := string(output)
	if err != nil {
		t.Fatalf("libsodium-style Sparkle key must reach sign_update: %v\n%s", err, got)
	}
	if !strings.Contains(got, "normalized Sparkle EdDSA secret: decoded 64 bytes -> seed (32 bytes)") {
		t.Fatalf("seed||public must become 32-byte Sparkle seed; output:\n%s", got)
	}
	if !strings.Contains(got, "stub method=stdin key_len=44") {
		t.Fatalf("expected stdin sign_update with 32-byte seed; output:\n%s", got)
	}
	if strings.Contains(got, key) {
		t.Fatal("appcast signer leaked the EdDSA private key into logs")
	}
}

func TestSignSparkleAppcastNormalizeRewritesExpandedAndSeedSecrets(t *testing.T) {
	root := filepath.Join("..", "..")
	pub, err := base64.StdEncoding.DecodeString(sparklePublicEDKey)
	if err != nil {
		t.Fatalf("decode public: %v", err)
	}

	run := func(input string) (stdout, stderr string, rc int) {
		cmd := exec.Command("python3", "scripts/normalize-sparkle-ed-key.py", sparklePublicEDKey)
		cmd.Dir = root
		cmd.Stdin = strings.NewReader(input)
		var outBuf, errBuf bytes.Buffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		runErr := cmd.Run()
		rc = 0
		if runErr != nil {
			if ee, ok := runErr.(*exec.ExitError); ok {
				rc = ee.ExitCode()
			} else {
				t.Fatalf("normalize: %v", runErr)
			}
		}
		return outBuf.String(), errBuf.String(), rc
	}

	expanded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 64))
	out, errLog, rc := run(expanded)
	if rc != 0 {
		t.Fatalf("expanded 64-byte key failed: rc=%d stderr=%s", rc, errLog)
	}
	got, err := base64.StdEncoding.DecodeString(out)
	if err != nil {
		t.Fatalf("expanded output not base64: %v", err)
	}
	if len(got) != 96 || !bytes.Equal(got[:64], bytes.Repeat([]byte{0x42}, 64)) || !bytes.Equal(got[64:], pub) {
		t.Fatalf("expanded key must become orlp-private||SUPublicEDKey, len=%d", len(got))
	}
	if !strings.Contains(errLog, "decoded 64 bytes -> old-96 (96 bytes)") {
		t.Fatalf("missing old-96 log: %s", errLog)
	}
	if strings.Contains(errLog, expanded) || strings.Contains(errLog, out) {
		t.Fatal("normalizer leaked the EdDSA secret on stderr")
	}

	libsodium := base64.StdEncoding.EncodeToString(append(bytes.Repeat([]byte{0x11}, 32), pub...))
	out, errLog, rc = run(libsodium)
	if rc != 0 {
		t.Fatalf("libsodium-style key failed: rc=%d stderr=%s", rc, errLog)
	}
	got, err = base64.StdEncoding.DecodeString(out)
	if err != nil {
		t.Fatalf("seed output not base64: %v", err)
	}
	if !bytes.Equal(got, bytes.Repeat([]byte{0x11}, 32)) {
		t.Fatalf("seed||public must collapse to 32-byte seed, len=%d", len(got))
	}
	if !strings.Contains(errLog, "decoded 64 bytes -> seed (32 bytes)") {
		t.Fatalf("missing seed log: %s", errLog)
	}

	seed := sparkleEdDSATestKey
	out, errLog, rc = run(seed)
	if rc != 0 {
		t.Fatalf("32-byte seed failed: rc=%d stderr=%s", rc, errLog)
	}
	got, err = base64.StdEncoding.DecodeString(out)
	if err != nil || len(got) != 32 {
		t.Fatalf("seed passthrough: %v len=%d", err, len(got))
	}
	if !strings.Contains(errLog, "decoded 32 bytes -> seed (32 bytes)") {
		t.Fatalf("missing 32-byte passthrough log: %s", errLog)
	}

	old := base64.StdEncoding.EncodeToString(append(bytes.Repeat([]byte{0x42}, 64), pub...))
	out, errLog, rc = run(old)
	if rc != 0 {
		t.Fatalf("old-96 failed: rc=%d stderr=%s", rc, errLog)
	}
	got, err = base64.StdEncoding.DecodeString(out)
	if err != nil || len(got) != 96 {
		t.Fatalf("old-96 passthrough: %v len=%d", err, len(got))
	}
	if !strings.Contains(errLog, "decoded 96 bytes -> old-96 (96 bytes)") {
		t.Fatalf("missing 96-byte passthrough log: %s", errLog)
	}
}

func TestSignSparkleAppcastRejectsUnexpectedPrivateKeyLength(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "payload.zip")
	outXML := filepath.Join(tmp, "appcast.xml")
	mustWriteFile(t, zipPath, "hello sparkle")

	cmd := exec.Command("bash", "scripts/sign-sparkle-appcast.sh", zipPath, "2026.8.28.1", "arm64", outXML)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"FSE_SPARKLE_DIR="+tmp,
		"SPARKLE_EDDSA_PRIVATE_KEY=too-short",
	)
	output, err := cmd.CombinedOutput()
	got := string(output)
	if err == nil {
		t.Fatalf("short Sparkle key unexpectedly accepted:\n%s", got)
	}
	if !strings.Contains(got, "SPARKLE_EDDSA_PRIVATE_KEY has unexpected length 9") {
		t.Fatalf("missing unexpected-length error; output:\n%s", got)
	}
	if strings.Contains(got, "too-short") {
		t.Fatal("unexpected-length error must not print the secret")
	}
}

func TestFetchSparkleFrameworkFailsWithoutSignUpdate(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dest := filepath.Join(tmp, "dest")
	archive := filepath.Join(tmp, "Sparkle-2.7.1.tar.xz")
	mustMkdir(t, filepath.Join(src, "Sparkle.framework"))
	mustWriteFile(t, filepath.Join(src, "Sparkle.framework", "Sparkle"), "framework")
	pack := exec.Command("tar", "-C", src, "-cJf", archive, "Sparkle.framework")
	if out, err := pack.CombinedOutput(); err != nil {
		t.Fatalf("create fixture archive: %v\n%s", err, out)
	}

	cmd := exec.Command("bash", "scripts/fetch-sparkle-framework.sh", dest)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "FSE_SPARKLE_ARCHIVE="+archive)
	output, err := cmd.CombinedOutput()
	got := string(output)
	if err == nil {
		t.Fatalf("fetch unexpectedly succeeded without sign_update:\n%s", got)
	}
	if !strings.Contains(got, "Sparkle sign_update is missing or not executable") {
		t.Fatalf("fetch must fail loudly without sign_update; output:\n%s", got)
	}
}

func TestFetchSparkleFrameworkIgnoresOldDSASignUpdate(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dest := filepath.Join(tmp, "dest")
	archive := filepath.Join(tmp, "Sparkle-2.7.1.tar.xz")
	mustMkdir(t, filepath.Join(src, "Sparkle.framework"))
	mustWriteExecutable(t, filepath.Join(src, "bin", "old_dsa_scripts", "sign_update"), "#!/bin/bash\nexit 1\n")
	pack := exec.Command("tar", "-C", src, "-cJf", archive, "Sparkle.framework", "bin")
	if out, err := pack.CombinedOutput(); err != nil {
		t.Fatalf("create fixture archive: %v\n%s", err, out)
	}

	cmd := exec.Command("bash", "scripts/fetch-sparkle-framework.sh", dest)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "FSE_SPARKLE_ARCHIVE="+archive)
	output, err := cmd.CombinedOutput()
	got := string(output)
	if err == nil {
		t.Fatalf("fetch must not treat old_dsa_scripts/sign_update as Sparkle sign_update:\n%s", got)
	}
	if !strings.Contains(got, "Sparkle sign_update is missing or not executable") {
		t.Fatalf("fetch must refuse old DSA sign_update; output:\n%s", got)
	}
}

func TestFetchSparkleFrameworkRequiresExecutableSignUpdateAfterExtract(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	dest := filepath.Join(tmp, "dest")
	archive := filepath.Join(tmp, "Sparkle-2.7.1.tar.xz")
	mustMkdir(t, filepath.Join(src, "Sparkle.framework"))
	mustWriteExecutable(t, filepath.Join(src, "bin", "sign_update"), "#!/bin/bash\nexit 0\n")
	pack := exec.Command("tar", "-C", src, "-cJf", archive, "Sparkle.framework", "bin")
	if out, err := pack.CombinedOutput(); err != nil {
		t.Fatalf("create fixture archive: %v\n%s", err, out)
	}

	cmd := exec.Command("bash", "scripts/fetch-sparkle-framework.sh", dest)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "FSE_SPARKLE_ARCHIVE="+archive)
	output, err := cmd.CombinedOutput()
	got := string(output)
	if err != nil {
		t.Fatalf("fetch with sign_update failed: %v\n%s", err, got)
	}
	if _, err := os.Stat(filepath.Join(dest, "bin", "sign_update")); err != nil {
		t.Fatalf("expected extracted sign_update: %v", err)
	}
}
