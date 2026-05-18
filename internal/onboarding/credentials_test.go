package onboarding

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// writeTestKey generates an RSA key, writes it as a PKCS8 PEM file, and returns
// the path plus the key for verification.
func writeTestKey(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	path := filepath.Join(t.TempDir(), "jwt-private.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return path, key
}

func TestMintAndVerify(t *testing.T) {
	keyFile, key := writeTestKey(t)

	m, err := NewMinter(keyFile, time.Hour)
	if err != nil {
		t.Fatalf("NewMinter: %v", err)
	}
	tokenStr, err := m.Mint("RC-123")
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	// Verify with the public key — and reject any non-RSA algorithm, the
	// classic JWT algorithm-confusion guard.
	parsed, err := jwt.Parse(tokenStr, func(tok *jwt.Token) (any, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", tok.Header["alg"])
		}
		return &key.PublicKey, nil
	})
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if !parsed.Valid {
		t.Fatal("token is not valid")
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatalf("unexpected claims type %T", parsed.Claims)
	}
	if claims["sub"] != "RC-123" || claims["username"] != "RC-123" {
		t.Errorf("subject/username wrong: %v / %v", claims["sub"], claims["username"])
	}
	if claims["iss"] != tokenIssuer {
		t.Errorf("iss = %v, want %v", claims["iss"], tokenIssuer)
	}
	if exp, err := parsed.Claims.GetExpirationTime(); err != nil || exp == nil {
		t.Errorf("token has no expiry: %v", err)
	}
}

func TestNewMinterMissingFile(t *testing.T) {
	if _, err := NewMinter(filepath.Join(t.TempDir(), "absent.pem"), time.Hour); err == nil {
		t.Error("NewMinter should fail when the key file is missing")
	}
}
