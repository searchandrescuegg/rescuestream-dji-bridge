package onboarding

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWT issuer and audience — EMQX should verify both when validating tokens.
const (
	tokenIssuer   = "rescuestream-dji-bridge"
	tokenAudience = "rescuestream-mqtt-broker"
)

// deviceClaims are the JWT claims EMQX validates for a device connection. The
// username claim must match the MQTT username the device connects with.
type deviceClaims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// Minter signs short-lived per-device JWT credentials (RS256); EMQX verifies
// them with the corresponding public key.
type Minter struct {
	key *rsa.PrivateKey
	ttl time.Duration
}

// NewMinter loads an RSA private key from a PEM file and returns a Minter that
// signs tokens valid for ttl.
func NewMinter(privateKeyFile string, ttl time.Duration) (*Minter, error) {
	raw, err := os.ReadFile(privateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("read jwt private key: %w", err)
	}
	key, err := parseRSAPrivateKey(raw)
	if err != nil {
		return nil, err
	}
	return &Minter{key: key, ttl: ttl}, nil
}

// Mint returns a signed JWT for the device with the given serial number. The
// device connects to the broker using deviceSN as its MQTT username and the
// returned token as its password.
func (m *Minter) Mint(deviceSN string) (string, error) {
	now := time.Now()
	claims := deviceClaims{
		Username: deviceSN,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tokenIssuer,
			Subject:   deviceSN,
			Audience:  jwt.ClaimStrings{tokenAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(m.key)
	if err != nil {
		return "", fmt.Errorf("sign jwt for %s: %w", deviceSN, err)
	}
	return signed, nil
}

func parseRSAPrivateKey(raw []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("jwt private key: no PEM block found")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse jwt private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("jwt private key is not an RSA key")
	}
	return key, nil
}
