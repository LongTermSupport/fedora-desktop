package pki

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"time"
)

const leafName = "teamchat homeserver"

// Leaf is the certificate and key a team's homeserver is reached by, signed by the team's CA.
type Leaf struct {
	cert    *x509.Certificate
	certPEM []byte
	keyPEM  []byte
	names   Names
}

// CertPEM is the leaf certificate.
func (l Leaf) CertPEM() []byte { return bytes.Clone(l.certPEM) }

// KeyPEM is the leaf key, a PKCS #8 block; the terminator takes it as a credential.
func (l Leaf) KeyPEM() []byte { return bytes.Clone(l.keyPEM) }

// NotAfter is when the leaf stops being valid.
func (l Leaf) NotAfter() time.Time { return l.cert.NotAfter }

// Names are the names the leaf holds, sorted and once each.
func (l Leaf) Names() Names {
	return Names{IPs: slices.Clone(l.names.IPs), DNS: l.names.DNS}
}

// NewLeaf makes a leaf for want, signed by ca (P2).
func (m Maker) NewLeaf(ca CA, want Names) (Leaf, error) {
	now, err := m.now()
	if err != nil {
		return Leaf{}, err
	}
	names, err := want.canonical()
	if err != nil {
		return Leaf{}, err
	}
	if ca.cert == nil || ca.key == nil {
		return Leaf{}, errors.New("pki: the CA is empty")
	}
	if now.Before(ca.cert.NotBefore) || now.After(ca.cert.NotAfter) {
		return Leaf{}, errors.New("pki: the CA is not valid at this time")
	}
	rnd := m.rand()
	key, err := newKey(rnd)
	if err != nil {
		return Leaf{}, err
	}
	serial, err := newSerial(rnd)
	if err != nil {
		return Leaf{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: leafName, Organization: []string{"teamchat"}},
		NotBefore:             laterOf(now.Add(-skew), ca.cert.NotBefore),
		NotAfter:              earlierOf(now.AddDate(leafYears, 0, 0), ca.cert.NotAfter),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           ipList(names.IPs),
	}
	if names.DNS != "" {
		tmpl.DNSNames = []string{names.DNS}
	}
	der, err := x509.CreateCertificate(rnd, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		return Leaf{}, fmt.Errorf("pki: signing the leaf certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return Leaf{}, fmt.Errorf("pki: reading back the leaf certificate: %w", err)
	}
	keyPEM, err := encodeKey(key)
	if err != nil {
		return Leaf{}, err
	}
	return Leaf{cert: cert, certPEM: encode(pemCertificate, der), keyPEM: keyPEM, names: names}, nil
}

// ParseLeaf reads a leaf from its two PEM blocks and checks it against the CA certificate in
// caPEM (P6): signed by that CA and naming it as its issuer, for server authentication, with
// names this package makes, and a key that is the certificate's.
func ParseLeaf(caPEM, certPEM, keyPEM []byte) (Leaf, error) {
	ca, err := parseCACertificate(caPEM)
	if err != nil {
		return Leaf{}, err
	}
	cert, err := parseCertificate("leaf certificate", certPEM)
	if err != nil {
		return Leaf{}, err
	}
	key, err := parseKey("leaf key", keyPEM)
	if err != nil {
		return Leaf{}, err
	}
	if err = checkLeaf(cert, ca, key); err != nil {
		return Leaf{}, err
	}
	names, err := namesOf(cert)
	if err != nil {
		return Leaf{}, err
	}
	return Leaf{cert: cert, certPEM: encode(pemCertificate, cert.Raw), keyPEM: bytes.Clone(keyPEM), names: names}, nil
}

func checkLeaf(cert, ca *x509.Certificate, key *ecdsa.PrivateKey) error {
	switch {
	case cert.IsCA:
		return errors.New("pki: the leaf certificate is a CA certificate")
	case cert.KeyUsage&x509.KeyUsageDigitalSignature == 0 || cert.KeyUsage&x509.KeyUsageCertSign != 0:
		return errors.New("pki: the leaf certificate's key usage is not a server's")
	case !slices.Equal(cert.ExtKeyUsage, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}):
		return errors.New("pki: the leaf certificate is not for server authentication alone")
	case !bytes.Equal(cert.RawIssuer, ca.RawSubject):
		return errors.New("pki: the leaf certificate names another issuer than the CA")
	case cert.CheckSignatureFrom(ca) != nil:
		return errors.New("pki: the leaf certificate is not signed by the CA")
	case !key.PublicKey.Equal(cert.PublicKey):
		return errors.New("pki: the leaf key is not the leaf certificate's")
	}
	return nil
}

// namesOf is the names a certificate holds, when they are only names this package makes: IP
// addresses and at most one DNS name.
func namesOf(cert *x509.Certificate) (Names, error) {
	if len(cert.URIs) != 0 || len(cert.EmailAddresses) != 0 || len(cert.DNSNames) > 1 {
		return Names{}, errors.New("pki: the leaf certificate holds a name this package does not make")
	}
	var names Names
	for _, ip := range cert.IPAddresses {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			return Names{}, errors.New("pki: the leaf certificate holds an IP name that is not an address")
		}
		names.IPs = append(names.IPs, addr.Unmap())
	}
	if len(cert.DNSNames) == 1 {
		names.DNS = cert.DNSNames[0]
	}
	return names.canonical()
}

// Renewal is whether a leaf must be made again, and why (P4).
type Renewal int

const (
	// NoRenewal means the leaf holds the wanted names and has time left.
	NoRenewal Renewal = iota
	// NamesChanged means the leaf's names are not the wanted names.
	NamesChanged
	// Expiring means fewer than 30 days of the leaf's validity remain.
	Expiring
)

// String is the reason in words, for the log.
func (r Renewal) String() string {
	switch r {
	case NoRenewal:
		return "kept"
	case NamesChanged:
		return "its names changed"
	case Expiring:
		return "fewer than 30 days remain"
	}
	return "unknown"
}

// Needed says the leaf must be made again.
func (r Renewal) Needed() bool { return r != NoRenewal }

// NeedsRenewal says whether the leaf must be made again for want at the clock's time (P4).
func (l Leaf) NeedsRenewal(clk Clock, want Names) (Renewal, error) {
	if clk == nil {
		return NoRenewal, errors.New("pki: a renewal check needs a Clock")
	}
	names, err := want.canonical()
	if err != nil {
		return NoRenewal, err
	}
	if !l.names.same(names) {
		return NamesChanged, nil
	}
	if l.cert.NotAfter.Sub(time.UnixMilli(clk.WallMS()).UTC()) < renewBefore {
		return Expiring, nil
	}
	return NoRenewal, nil
}

func ipList(addrs []netip.Addr) []net.IP {
	out := make([]net.IP, len(addrs))
	for i, a := range addrs {
		out[i] = net.IP(a.AsSlice())
	}
	return out
}

func laterOf(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earlierOf(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
