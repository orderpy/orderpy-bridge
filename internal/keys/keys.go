package keys

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
)

// LoadOrGeneratePublicKeyPEM returns PEM public key; creates RSA-2048 keypair on disk like Python bridge.
func LoadOrGeneratePublicKeyPEM(keyPath string) (string, error) {
	if keyPath == "" {
		return "", errors.New("empty key path")
	}
	dir := filepath.Dir(keyPath)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o700)
	}
	privPath := keyPath
	if !hasSuffixFold(keyPath, ".pem") {
		privPath = keyPath + ".priv"
	}
	pubPath := keyPath
	if hasSuffixFold(keyPath, ".pem") {
		pubPath = keyPath[:len(keyPath)-4] + ".pub"
	} else {
		pubPath = keyPath + ".pub"
	}
	if b, err := os.ReadFile(pubPath); err == nil && len(b) > 0 {
		return string(b), nil
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", err
	}
	privDER := x509.MarshalPKCS1PrivateKey(key)
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privDER})
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", err
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(pubPath, pubPEM, 0o644); err != nil {
		return "", err
	}
	return string(pubPEM), nil
}

func hasSuffixFold(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}
