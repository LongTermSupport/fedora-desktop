// Package pki is a team's certificate authority and the leaf certificate its homeserver is
// reached by (Plan 00246 SPEC-cross-network X13 and X14, and the trust half of X15 and X16). It
// is the standard library only: crypto/ecdsa, crypto/x509 and encoding/pem.
//
// A team has one CA, made once. Its certificate goes to every member in the bundle and is the
// only root a member trusts; its key stays with the host and signs the leaf. The leaf is made
// again when the addresses it is reached by change or its year is nearly out.
//
// Expectations (each has a test whose name starts with its number):
//
//	P1  A CA is ECDSA P-256 and self-signed, a CA of path length 0 whose one use is signing
//	    certificates, valid from one hour before the Maker's clock to ten years after it. Every
//	    validity comes from the Maker's Clock; the package never reads the wall clock.
//	P2  A leaf is ECDSA P-256 signed by the CA, for server authentication only, valid from one
//	    hour before the clock to one year after it, never past its CA. Its names are exactly the
//	    wanted IP addresses (sorted, once each) and the DNS name if there is one, and nothing
//	    else. A CA that is not valid at the clock's time signs nothing.
//	P3  Names are checked before anything is made: at least one; an IP address is a specific,
//	    non-multicast address with no zone, in its plain form (not IPv4-mapped), at most 64 of
//	    them; a DNS name is lower-case ASCII labels of letters, digits and hyphens, not an IP
//	    literal.
//	P4  A leaf must be made again when its names differ from the wanted names, compared as
//	    sets, or when fewer than 30 days of its validity remain at the clock's time (an
//	    expired leaf included). Names are judged first.
//	P5  The CA certificate, the CA key, the leaf certificate and the leaf key are PEM: one
//	    CERTIFICATE or PRIVATE KEY (PKCS #8) block each, and what was written parses back to the
//	    same bytes.
//	P6  Parsing is strict. It refuses no block, text before the block, text or another block
//	    after it, a block of another type, a block with headers, a DER that does not parse, a
//	    key that is not ECDSA P-256, a CA certificate that is not a self-signed CA, a CA key that
//	    is not the certificate's, a leaf that is a CA, has no server-authentication use, has a
//	    name this package does not make, is not signed by the given CA or names another issuer,
//	    and a leaf key that is not the certificate's.
//	P7  A certificate with any one byte changed is refused by every parser.
//	P8  A CA's fingerprint is the SHA-256 of its certificate's DER, in lower-case hex; only a
//	    CA certificate has one.
//	P9  The trust pool built from a CA PEM holds exactly that one certificate: the system's
//	    roots are not in it.
//	P10 Randomness comes from the Maker's Rand, crypto/rand when it is nil: a key is the scalar
//	    read from it, so the same bytes make the same key, and a reader that errors stops the
//	    making. (Go 1.26's ecdsa.GenerateKey ignores the reader it is given, which is why the
//	    scalar is read here.)
//	P11 The package does no file or network I/O: the host stores the bytes it is given.
//	P12 An error is a fixed phrase naming what is wrong; it never carries the PEM or the DER it
//	    was given, so a key or a certificate does not reach a log.
package pki

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"time"
)

const (
	pemCertificate = "CERTIFICATE"
	pemKey         = "PRIVATE KEY"

	// skew is how long before the clock's time a certificate is already valid, so a machine
	// whose clock runs a little slow does not see a fresh certificate as not yet valid.
	skew        = time.Hour
	caYears     = 10
	leafYears   = 1
	renewBefore = 30 * 24 * time.Hour

	scalarBytes  = 32
	keyAttempts  = 8
	serialBits   = 127
	caNameMax    = 64
	printableLow = 0x20
	printableTop = 0x7e
)

// Clock is the one reading the package takes: the wall clock in milliseconds. clock.Clock meets
// it, and clock.Fake is what a test drives.
type Clock interface {
	WallMS() int64
}

// Maker makes a CA and the leaves it signs.
type Maker struct {
	// Clock decides every validity; it is required.
	Clock Clock
	// Rand is the source of keys and serial numbers; nil is crypto/rand.
	Rand io.Reader
}

// CA is a team's certificate authority: its certificate and the key that signs with it.
type CA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
	keyPEM  []byte
}

// CertPEM is the CA certificate, the one block members trust.
func (c CA) CertPEM() []byte { return bytes.Clone(c.certPEM) }

// KeyPEM is the CA key, a PKCS #8 block. It is the team's secret: the host keeps it where only
// root reads it.
func (c CA) KeyPEM() []byte { return bytes.Clone(c.keyPEM) }

// NotAfter is when the CA stops being valid.
func (c CA) NotAfter() time.Time { return c.cert.NotAfter }

// NewCA makes a team's CA, named name (one line of 1 to 64 printable ASCII characters).
func (m Maker) NewCA(name string) (CA, error) {
	now, err := m.now()
	if err != nil {
		return CA{}, err
	}
	if !printable(name) {
		return CA{}, errors.New("pki: a CA name is 1 to 64 printable ASCII characters")
	}
	rnd := m.rand()
	key, err := newKey(rnd)
	if err != nil {
		return CA{}, err
	}
	serial, err := newSerial(rnd)
	if err != nil {
		return CA{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: name, Organization: []string{"teamchat"}},
		NotBefore:             now.Add(-skew),
		NotAfter:              now.AddDate(caYears, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rnd, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return CA{}, fmt.Errorf("pki: signing the CA certificate: %w", err)
	}
	return assembleCA(der, key)
}

func (m Maker) now() (time.Time, error) {
	if m.Clock == nil {
		return time.Time{}, errors.New("pki: a Maker needs a Clock")
	}
	return time.UnixMilli(m.Clock.WallMS()).UTC(), nil
}

func (m Maker) rand() io.Reader {
	if m.Rand == nil {
		return rand.Reader
	}
	return m.Rand
}

func assembleCA(der []byte, key *ecdsa.PrivateKey) (CA, error) {
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return CA{}, fmt.Errorf("pki: reading back the CA certificate: %w", err)
	}
	keyPEM, err := encodeKey(key)
	if err != nil {
		return CA{}, err
	}
	return CA{cert: cert, key: key, certPEM: encode(pemCertificate, der), keyPEM: keyPEM}, nil
}

// ParseCA reads a CA from its two PEM blocks (P6).
func ParseCA(certPEM, keyPEM []byte) (CA, error) {
	cert, err := parseCACertificate(certPEM)
	if err != nil {
		return CA{}, err
	}
	key, err := parseKey("CA key", keyPEM)
	if err != nil {
		return CA{}, err
	}
	if !key.PublicKey.Equal(cert.PublicKey) {
		return CA{}, errors.New("pki: the CA key is not the CA certificate's")
	}
	return CA{cert: cert, key: key, certPEM: encode(pemCertificate, cert.Raw), keyPEM: bytes.Clone(keyPEM)}, nil
}

// parseCACertificate reads one PEM block that is a team's CA certificate: ECDSA P-256, a CA
// that may sign certificates, signed by its own key.
func parseCACertificate(certPEM []byte) (*x509.Certificate, error) {
	const what = "CA certificate"
	cert, err := parseCertificate(what, certPEM)
	if err != nil {
		return nil, err
	}
	if !cert.IsCA || !cert.BasicConstraintsValid || cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		return nil, fmt.Errorf("pki: the %s is not a CA certificate", what)
	}
	if !bytes.Equal(cert.RawIssuer, cert.RawSubject) || cert.CheckSignatureFrom(cert) != nil {
		return nil, fmt.Errorf("pki: the %s is not signed by its own key", what)
	}
	return cert, nil
}

// parseCertificate reads one PEM block that is an ECDSA P-256 certificate.
func parseCertificate(what string, certPEM []byte) (*x509.Certificate, error) {
	der, err := decodeOne(what, certPEM, pemCertificate)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("pki: the %s is not a certificate", what)
	}
	if pub, ok := cert.PublicKey.(*ecdsa.PublicKey); !ok || pub.Curve != elliptic.P256() {
		return nil, fmt.Errorf("pki: the %s does not hold an ECDSA P-256 key", what)
	}
	return cert, nil
}

// parseKey reads one PKCS #8 PEM block that is an ECDSA P-256 private key.
func parseKey(what string, keyPEM []byte) (*ecdsa.PrivateKey, error) {
	der, err := decodeOne(what, keyPEM, pemKey)
	if err != nil {
		return nil, err
	}
	var outer asn1.RawValue
	if rest, asnErr := asn1.Unmarshal(der, &outer); asnErr != nil || len(rest) != 0 {
		return nil, fmt.Errorf("pki: the %s is not one PKCS #8 key", what)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("pki: the %s is not a PKCS #8 key", what)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("pki: the %s is not an ECDSA P-256 key", what)
	}
	return key, nil
}

// decodeOne is the DER in data, which must be exactly one PEM block of blockType with no
// headers: nothing before it, nothing after it.
func decodeOne(what string, data []byte, blockType string) ([]byte, error) {
	if !bytes.HasPrefix(data, []byte("-----BEGIN ")) {
		return nil, fmt.Errorf("pki: the %s does not start with a PEM block", what)
	}
	block, rest := pem.Decode(data)
	switch {
	case block == nil:
		return nil, fmt.Errorf("pki: the %s is not a PEM block", what)
	case len(rest) != 0:
		return nil, fmt.Errorf("pki: the %s has data after its PEM block, or a second block", what)
	case block.Type != blockType:
		return nil, fmt.Errorf("pki: the %s is not a %s block", what, blockType)
	case len(block.Headers) != 0:
		return nil, fmt.Errorf("pki: the %s block has headers", what)
	}
	return block.Bytes, nil
}

func encode(blockType string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
}

func encodeKey(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("pki: encoding a key: %w", err)
	}
	return encode(pemKey, der), nil
}

// newKey reads a P-256 private scalar from rnd, as ecdsa.GenerateKey did before Go 1.26 stopped
// using the reader it is given (P10). A candidate outside [1, n-1] is read again.
func newKey(rnd io.Reader) (*ecdsa.PrivateKey, error) {
	buf := make([]byte, scalarBytes)
	defer clear(buf)
	for range keyAttempts {
		if _, err := io.ReadFull(rnd, buf); err != nil {
			return nil, fmt.Errorf("pki: reading randomness for a key: %w", err)
		}
		if key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), buf); err == nil {
			return key, nil
		}
	}
	return nil, errors.New("pki: the randomness gave no usable key")
}

// newSerial is a positive serial number of up to 127 bits read from rnd.
func newSerial(rnd io.Reader) (*big.Int, error) {
	n, err := rand.Int(rnd, new(big.Int).Lsh(big.NewInt(1), serialBits))
	if err != nil {
		return nil, fmt.Errorf("pki: reading randomness for a serial number: %w", err)
	}
	return n.Add(n, big.NewInt(1)), nil
}

// printable says name is 1 to 64 characters of printable ASCII.
func printable(name string) bool {
	if name == "" || len(name) > caNameMax {
		return false
	}
	for i := range len(name) {
		if name[i] < printableLow || name[i] > printableTop {
			return false
		}
	}
	return true
}
