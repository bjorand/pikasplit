package transport

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base32"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Fingerprint is the SHA-256 of a certificate's DER encoding.
type Fingerprint [32]byte

func (f Fingerprint) String() string { return hex.EncodeToString(f[:]) }

// ParseFingerprint parses the hex form produced by String.
func ParseFingerprint(s string) (Fingerprint, error) {
	var f Fingerprint
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != len(f) {
		return f, fmt.Errorf("transport: invalid fingerprint %q", s)
	}
	copy(f[:], b)
	return f, nil
}

// FingerprintOf hashes a DER certificate.
func FingerprintOf(der []byte) Fingerprint { return sha256.Sum256(der) }

// ShortCode is the human-checkable form of a fingerprint shown by the host
// when pairing: the first 80 bits in base32, e.g. "K7QF-2M4X-PZ3A-9HTD".
// 80 bits makes forging a certificate with the same code infeasible.
func (f Fingerprint) ShortCode() string {
	s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(f[:10])
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16]
}

func normalizeCode(s string) string {
	s = strings.ToUpper(s)
	return strings.NewReplacer("-", "", " ", "").Replace(s)
}

// Identity is a long-lived self-signed certificate identifying a machine.
type Identity struct {
	Cert        tls.Certificate
	Fingerprint Fingerprint
}

// LoadOrCreateIdentity reads dir/identity.pem, generating it on first use.
func LoadOrCreateIdentity(dir, commonName string) (*Identity, error) {
	path := filepath.Join(dir, "identity.pem")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		data, err = newIdentityPEM(commonName)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return parseIdentityPEM(data)
}

// NewIdentity returns an ephemeral identity (used by tests).
func NewIdentity(commonName string) (*Identity, error) {
	data, err := newIdentityPEM(commonName)
	if err != nil {
		return nil, err
	}
	return parseIdentityPEM(data)
}

func newIdentityPEM(commonName string) ([]byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(20, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	out := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	out = append(out, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})...)
	return out, nil
}

func parseIdentityPEM(data []byte) (*Identity, error) {
	cert, err := tls.X509KeyPair(data, data)
	if err != nil {
		return nil, fmt.Errorf("transport: identity: %w", err)
	}
	return &Identity{Cert: cert, Fingerprint: FingerprintOf(cert.Certificate[0])}, nil
}
