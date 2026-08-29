package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const sparkleEdDSATestKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

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

// 82-character base64 line with no sparkle:edSignature= wrapper, so the grep -E
// fallback (not awk) has to extract it. "/" is included so a slash-in-class
// regex would be required if someone brought awk back.
const stubSignUpdateBareSignature = `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  printf 'usage: sign_update [--ed-key-file FILE] ARCHIVE\n'
  exit 0
fi
printf 'stub method=stdin key_len=44\n' >&2
printf 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA/AAAAAAAAAAAAAA==\n'
`

// Emulate macOS /usr/bin/awk: "/" inside a /regex/ character class ends the
// literal ("nonterminated character class") and exits 1. Existing stub tests
// must keep passing when this awk is first on PATH (Linux CI uses GNU/mawk).
const bsdLikeAwkStub = `#!/usr/bin/env bash
set -euo pipefail
# "/" then later "[" then later "/" — the BSD /regex/ class trap.
pat='/.*\[.*/'
for arg in "$@"; do
  if printf '%s' "$arg" | grep -Eq "$pat"; then
    printf 'awk: nonterminated character class ^[A-Za-z0-9+\n' >&2
    printf ' source line number 1\n' >&2
    printf ' context is\n' >&2
    printf '\t >>> /^[A-Za-z0-9+/ <<<\n' >&2
    exit 1
  fi
done
exec /usr/bin/awk "$@"
`

func withBSDLikeAwkPath(t *testing.T, extraEnv ...string) []string {
	t.Helper()
	dir := t.TempDir()
	mustWriteExecutable(t, filepath.Join(dir, "awk"), bsdLikeAwkStub)
	path := dir + string(os.PathListSeparator) + os.Getenv("PATH")
	env := append(os.Environ(), "PATH="+path)
	return append(env, extraEnv...)
}

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
	cmd.Env = withBSDLikeAwkPath(t,
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
	cmd.Env = withBSDLikeAwkPath(t,
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
	cmd.Env = withBSDLikeAwkPath(t,
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

func TestSignSparkleAppcastParseSignatureAvoidsBSDAwkSlashClass(t *testing.T) {
	root := filepath.Join("..", "..")
	body := readRequiredFile(t, filepath.Join(root, "scripts", "sign-sparkle-appcast.sh"))
	start := strings.Index(body, "parse_signature_line()")
	if start < 0 {
		t.Fatal("sign-sparkle-appcast.sh must define parse_signature_line")
	}
	end := strings.Index(body[start:], "\ndump_sign_update_diagnostics")
	if end < 0 {
		t.Fatal("could not isolate parse_signature_line")
	}
	fn := body[start : start+end]
	// BSD awk treats "/" inside [] of a /regex/ as the terminator. Do not
	// allow that construct anywhere in the appcast signer, and keep parse
	// helpers from aborting the script under set -euo pipefail.
	awkSlashClass := regexp.MustCompile(`awk[^\n]*'/[^']*\[[^'\]]*\/`)
	if awkSlashClass.MatchString(body) {
		t.Fatalf("appcast signer must not use an awk /regex/ with / inside []: %s", awkSlashClass.FindString(body))
	}
	if strings.Contains(fn, "awk '/") || strings.Contains(fn, `awk "/`) {
		t.Fatal("parse_signature_line must not use awk /regex/ delimiters")
	}
	if !strings.Contains(fn, "set +e") {
		t.Fatal("parse_signature_line must disable set -e around helpers so diagnostics still run")
	}
}

func TestSignSparkleAppcastParsesBareBase64SignatureLine(t *testing.T) {
	root := filepath.Join("..", "..")
	tmp := t.TempDir()
	sparkleDir := filepath.Join(tmp, "sparkle")
	zipPath := filepath.Join(tmp, "payload.zip")
	outXML := filepath.Join(tmp, "appcast.xml")
	mustWriteFile(t, zipPath, "hello sparkle")
	mustMkdir(t, filepath.Join(sparkleDir, "Sparkle.framework"))
	mustWriteExecutable(t, filepath.Join(sparkleDir, "bin", "sign_update"), stubSignUpdateBareSignature)

	cmd := exec.Command("bash", "scripts/sign-sparkle-appcast.sh", zipPath, "2026.8.28.1", "arm64", outXML)
	cmd.Dir = root
	cmd.Env = withBSDLikeAwkPath(t,
		"FSE_SPARKLE_DIR="+sparkleDir,
		"SPARKLE_EDDSA_PRIVATE_KEY="+sparkleEdDSATestKey,
	)
	output, err := cmd.CombinedOutput()
	got := string(output)
	if err != nil {
		t.Fatalf("bare signature line should parse without awk: %v\n%s", err, got)
	}
	if strings.Contains(got, "nonterminated character class") {
		t.Fatalf("BSD-like awk must not run on signature parse; output:\n%s", got)
	}
	body, err := os.ReadFile(outXML)
	if err != nil {
		t.Fatalf("read appcast: %v", err)
	}
	xml := string(body)
	wantSig := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA/AAAAAAAAAAAAAA=="
	if !strings.Contains(xml, `sparkle:edSignature="`+wantSig+`"`) {
		t.Fatalf("appcast missing bare-line edSignature:\n%s", xml)
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
