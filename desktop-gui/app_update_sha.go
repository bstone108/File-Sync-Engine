package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func verifySHA256(path, expected string) error {
	want := strings.ToLower(strings.TrimSpace(expected))
	if want == "" {
		return fmt.Errorf("update asset is missing a SHA-256 digest")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hash staged update: %w", err)
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if got != want {
		return fmt.Errorf("staged update SHA-256 mismatch: got %s want %s", got, want)
	}
	return nil
}

func hashReaderToFile(r io.Reader, dest string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return "", err
	}
	tmp := dest + ".part"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, err = io.Copy(io.MultiWriter(file, hash), r)
	closeErr := file.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return "", closeErr
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
