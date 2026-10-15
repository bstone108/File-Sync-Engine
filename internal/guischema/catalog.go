// Package guischema resolves the newest web GUI package whose declared schema
// range includes the running engine. Incompatible packages are never selected.
package guischema

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"filesyncengine/internal/schemaversion"
)

// ErrNoneCompatible means the catalog has no package for this engine schema.
var ErrNoneCompatible = errors.New("no GUI package is compatible with this engine schema")

// Package is one published GUI build in the schema catalog.
type Package struct {
	GUIVersion      string `json:"guiVersion"`
	MinEngineSchema int    `json:"minEngineSchema"`
	MaxEngineSchema int    `json:"maxEngineSchema"`
	DownloadURL     string `json:"downloadURL"`
	ChecksumSHA256  string `json:"checksumSHA256"`
}

// Catalog maps GUI package versions to the engine schemas they support.
type Catalog struct {
	Schema   string    `json:"schema"`
	Packages []Package `json:"packages"`
}

// Declaration is the schema.json shipped inside a GUI package.
type Declaration struct {
	GUIVersion      string `json:"guiVersion"`
	MinEngineSchema int    `json:"minEngineSchema"`
	MaxEngineSchema int    `json:"maxEngineSchema"`
}

// Resolved is a checksum-verified package saved on local disk.
type Resolved struct {
	Package
	LocalPath string
}

// EngineSchemaVersion is the schema implemented by the running engine.
func EngineSchemaVersion() int { return schemaversion.Engine }

// Compatible reports whether pkg supports engineSchema.
func Compatible(pkg Package, engineSchema int) bool {
	if pkg.GUIVersion == "" || pkg.MinEngineSchema < 1 || pkg.MaxEngineSchema < pkg.MinEngineSchema {
		return false
	}
	return engineSchema >= pkg.MinEngineSchema && engineSchema <= pkg.MaxEngineSchema
}

// SelectNewestCompatible returns the highest GUI version that supports engineSchema.
// Newer packages outside the range are rejected. ErrNoneCompatible is returned
// when every package is incompatible or the catalog is empty.
func SelectNewestCompatible(packages []Package, engineSchema int) (Package, error) {
	var (
		found bool
		best  Package
	)
	for _, pkg := range packages {
		if !Compatible(pkg, engineSchema) {
			continue
		}
		if !found || CompareGUIVersion(pkg.GUIVersion, best.GUIVersion) > 0 {
			best = pkg
			found = true
		}
	}
	if !found {
		return Package{}, fmt.Errorf("%w %d", ErrNoneCompatible, engineSchema)
	}
	return best, nil
}

// CompareGUIVersion returns -1, 0, or 1. Numeric dotted parts compare as
// integers; a trailing suffix such as "-dev" sorts before a bare number of
// the same value so a dated release is newer than a dev build.
func CompareGUIVersion(left, right string) int {
	lp := splitVersion(left)
	rp := splitVersion(right)
	n := len(lp)
	if len(rp) > n {
		n = len(rp)
	}
	for i := 0; i < n; i++ {
		var l, r versionPart
		if i < len(lp) {
			l = lp[i]
		}
		if i < len(rp) {
			r = rp[i]
		}
		if l.n != r.n {
			if l.n < r.n {
				return -1
			}
			return 1
		}
		if l.rest != r.rest {
			if l.rest < r.rest {
				return -1
			}
			return 1
		}
	}
	return 0
}

type versionPart struct {
	n    int
	rest string
}

func splitVersion(raw string) []versionPart {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "v")
	if raw == "" {
		return []versionPart{{rest: ""}}
	}
	pieces := strings.Split(raw, ".")
	out := make([]versionPart, 0, len(pieces))
	for _, piece := range pieces {
		i := 0
		for i < len(piece) && piece[i] >= '0' && piece[i] <= '9' {
			i++
		}
		n := 0
		if i > 0 {
			parsed, err := strconv.Atoi(piece[:i])
			if err == nil {
				n = parsed
			}
		}
		out = append(out, versionPart{n: n, rest: piece[i:]})
	}
	return out
}

// LoadCatalog reads a local schema-versions file or an HTTPS URL.
func LoadCatalog(ctx context.Context, source string, client *http.Client) (Catalog, error) {
	var data []byte
	var err error
	if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://") {
		if client == nil {
			client = http.DefaultClient
		}
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if reqErr != nil {
			return Catalog{}, reqErr
		}
		resp, doErr := client.Do(req)
		if doErr != nil {
			return Catalog{}, doErr
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return Catalog{}, fmt.Errorf("schema catalog %s returned %s", source, resp.Status)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	} else {
		data, err = os.ReadFile(source)
	}
	if err != nil {
		return Catalog{}, err
	}
	var catalog Catalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return Catalog{}, fmt.Errorf("parse schema catalog: %w", err)
	}
	return catalog, nil
}

// ResolveNewestCompatible selects, downloads, and checksum-verifies the newest
// GUI package compatible with engineSchema. The package's own schema.json must
// agree. Incompatible catalog entries are ignored.
func ResolveNewestCompatible(ctx context.Context, indexSource, destDir string, engineSchema int, client *http.Client) (Resolved, error) {
	catalog, err := LoadCatalog(ctx, indexSource, client)
	if err != nil {
		return Resolved{}, err
	}
	selected, err := SelectNewestCompatible(catalog.Packages, engineSchema)
	if err != nil {
		return Resolved{}, err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return Resolved{}, err
	}
	dest := filepath.Join(destDir, sanitizeName(selected.GUIVersion)+".zip")
	if err := downloadVerified(ctx, selected, dest, client); err != nil {
		return Resolved{}, err
	}
	declared, err := ReadPackageDeclaration(dest)
	if err != nil {
		_ = os.Remove(dest)
		return Resolved{}, err
	}
	if !Compatible(Package{GUIVersion: declared.GUIVersion, MinEngineSchema: declared.MinEngineSchema, MaxEngineSchema: declared.MaxEngineSchema}, engineSchema) {
		_ = os.Remove(dest)
		return Resolved{}, fmt.Errorf("downloaded GUI package %s schema range %d-%d is incompatible with engine schema %d", declared.GUIVersion, declared.MinEngineSchema, declared.MaxEngineSchema, engineSchema)
	}
	return Resolved{Package: selected, LocalPath: dest}, nil
}

func downloadVerified(ctx context.Context, pkg Package, dest string, client *http.Client) error {
	if !checksumOK(pkg.ChecksumSHA256) {
		return fmt.Errorf("GUI package %s is missing a SHA-256 checksum", pkg.GUIVersion)
	}
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pkg.DownloadURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download GUI package %s: %w", pkg.GUIVersion, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download GUI package %s: %s", pkg.GUIVersion, resp.Status)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".fse-gui-download-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hash), resp.Body); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(sum, pkg.ChecksumSHA256) {
		return fmt.Errorf("GUI package %s checksum mismatch", pkg.GUIVersion)
	}
	fmt.Fprintln(os.Stderr, "web GUI package checksum verified")
	if err := os.Rename(tmpName, dest); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// ReadPackageDeclaration reads schema.json from a verified GUI zip.
func ReadPackageDeclaration(zipPath string) (Declaration, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return Declaration{}, err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if file.Name != "schema.json" {
			continue
		}
		body, err := file.Open()
		if err != nil {
			return Declaration{}, err
		}
		defer body.Close()
		var declared Declaration
		if err := json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&declared); err != nil {
			return Declaration{}, fmt.Errorf("parse GUI schema.json: %w", err)
		}
		return declared, nil
	}
	return Declaration{}, errors.New("GUI package is missing schema.json")
}

func checksumOK(sum string) bool {
	if len(sum) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(sum)
	return err == nil
}

func sanitizeName(version string) string {
	var b strings.Builder
	for _, r := range version {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "gui"
	}
	return b.String()
}
