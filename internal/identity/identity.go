package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// Identity holds the node's Ed25519 keypair and its fingerprint.
type Identity struct {
	PublicKey   ed25519.PublicKey
	PrivateKey  ed25519.PrivateKey
	Fingerprint string // hex-encoded SHA-256 of the public key bytes
}

// Load loads (or generates on first boot) the node Ed25519 keypair from dataDir.
func Load(dataDir string) (*Identity, error) {
	privPath := filepath.Join(dataDir, "identity.key")
	pubPath := filepath.Join(dataDir, "identity.pub")

	if _, err := os.Stat(privPath); os.IsNotExist(err) {
		return generate(privPath, pubPath)
	}

	return loadFromDisk(privPath, pubPath)
}

func generate(privPath, pubPath string) (*Identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})

	pubBytes, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})

	if err := os.WriteFile(privPath, privPEM, 0600); err != nil {
		return nil, fmt.Errorf("write private key: %w", err)
	}
	if err := os.WriteFile(pubPath, pubPEM, 0644); err != nil {
		return nil, fmt.Errorf("write public key: %w", err)
	}

	id := &Identity{
		PublicKey:   pub,
		PrivateKey:  priv,
		Fingerprint: fingerprint(pub),
	}
	slog.Info("generated new node identity", "fingerprint", id.Fingerprint)
	return id, nil
}

func loadFromDisk(privPath, pubPath string) (*Identity, error) {
	privPEM, err := os.ReadFile(privPath)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	block, _ := pem.Decode(privPEM)
	if block == nil {
		return nil, fmt.Errorf("decode private key PEM")
	}
	privKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	priv, ok := privKey.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not ed25519")
	}

	pubPEM, err := os.ReadFile(pubPath)
	if err != nil {
		return nil, fmt.Errorf("read public key: %w", err)
	}
	block, _ = pem.Decode(pubPEM)
	if block == nil {
		return nil, fmt.Errorf("decode public key PEM")
	}
	pubKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	pub, ok := pubKey.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is not ed25519")
	}

	id := &Identity{
		PublicKey:   pub,
		PrivateKey:  priv,
		Fingerprint: fingerprint(pub),
	}
	slog.Info("loaded node identity", "fingerprint", id.Fingerprint)
	return id, nil
}

func fingerprint(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	return hex.EncodeToString(h[:])
}
