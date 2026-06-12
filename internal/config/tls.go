package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultAutoAPICertFile = "fse-api-auto.crt"
	defaultAutoAPIKeyFile  = "fse-api-auto.key"
)

// EnsureAPITLSAssets resolves configured API TLS paths and, for auto mode on
// non-loopback listeners, creates a local self-signed certificate/key pair when
// none is configured. It mutates cfg with absolute runtime paths but does not
// rewrite the config file or expose generated key material through config APIs.
func EnsureAPITLSAssets(cfg *Config, configPath string) error {
	if cfg == nil || !cfg.API.RequiresTLS() {
		return nil
	}
	baseDir := filepath.Dir(configPath)
	if baseDir == "." || baseDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		baseDir = cwd
	}
	certFile := cfg.API.Encryption.CertFile
	keyFile := cfg.API.Encryption.KeyFile
	if cfg.API.Encryption.Mode == APIEncryptionAuto && certFile == "" && keyFile == "" {
		certFile = filepath.Join(baseDir, defaultAutoAPICertFile)
		keyFile = filepath.Join(baseDir, defaultAutoAPIKeyFile)
	}
	if certFile == "" || keyFile == "" {
		return fmt.Errorf("api TLS requires both certFile and keyFile")
	}
	certFile = resolveConfigRelativePath(baseDir, certFile)
	keyFile = resolveConfigRelativePath(baseDir, keyFile)
	if _, err := os.Stat(certFile); err == nil {
		if _, err := os.Stat(keyFile); err != nil {
			return fmt.Errorf("api TLS key file %q is not usable: %w", keyFile, err)
		}
		cfg.API.Encryption.CertFile = certFile
		cfg.API.Encryption.KeyFile = keyFile
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("api TLS cert file %q is not usable: %w", certFile, err)
	}
	if err := generateSelfSignedAPICertificate(certFile, keyFile, cfg.API.Listen); err != nil {
		return err
	}
	cfg.API.Encryption.CertFile = certFile
	cfg.API.Encryption.KeyFile = keyFile
	return nil
}

func resolveConfigRelativePath(baseDir string, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(baseDir, path)
}

func generateSelfSignedAPICertificate(certFile, keyFile, listen string) error {
	if err := os.MkdirAll(filepath.Dir(certFile), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(keyFile), 0o700); err != nil {
		return err
	}
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return err
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "filesyncengine API auto-generated certificate",
		},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if host := apiListenHost(listen); host != "" {
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = []net.IP{ip}
		} else {
			template.DNSNames = []string{host}
		}
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := WriteFileAtomic(certFile, certPEM, 0o600); err != nil {
		return err
	}
	return WriteFileAtomic(keyFile, keyPEM, 0o600)
}

func apiListenHost(listen string) string {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		host = listen
	}
	return strings.Trim(host, "[]")
}
