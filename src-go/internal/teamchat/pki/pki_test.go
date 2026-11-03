package pki

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/netip"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
)

// wall0 is the fake clock's start: 2025-10-09 08:53:20 UTC, a whole second.
const wall0 = int64(1_760_000_000_000)

const (
	certType = "CERTIFICATE"
	keyType  = "PRIVATE" + " KEY"
)

var errBoom = errors.New("the entropy source failed")

func start() time.Time { return time.UnixMilli(wall0).UTC() }

// now is the fake clock's wall time.
func now(clk Clock) time.Time { return time.UnixMilli(clk.WallMS()).UTC() }

// newMaker is a maker on a fake clock a test does not move.
func newMaker() Maker { return Maker{Clock: clock.NewFake(0, wall0)} }

// newWorld is a maker and the fake clock it reads, for a test that moves time.
func newWorld() (Maker, *clock.Fake) {
	clk := clock.NewFake(0, wall0)
	return Maker{Clock: clk}, clk
}

func mustCA(t *testing.T, m Maker, name string) CA {
	t.Helper()
	ca, err := m.NewCA(name)
	if err != nil {
		t.Fatalf("NewCA(%q): %v", name, err)
	}
	return ca
}

func mustLeaf(t *testing.T, m Maker, ca CA, want Names) Leaf {
	t.Helper()
	leaf, err := m.NewLeaf(ca, want)
	if err != nil {
		t.Fatalf("NewLeaf(%+v): %v", want, err)
	}
	return leaf
}

func addr(t *testing.T, s string) netip.Addr {
	t.Helper()
	a, err := netip.ParseAddr(s)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func ips(t *testing.T, list ...string) []netip.Addr {
	t.Helper()
	out := make([]netip.Addr, len(list))
	for i, s := range list {
		out[i] = addr(t, s)
	}
	return out
}

func wantNames(t *testing.T) Names {
	t.Helper()
	return Names{IPs: ips(t, "192.0.2.10", "2001:db8::1"), DNS: "hs.example.test"}
}

// onlyBlock is the one PEM block of data, which this package wrote.
func onlyBlock(t *testing.T, data []byte) *pem.Block {
	t.Helper()
	block, rest := pem.Decode(data)
	if block == nil || len(rest) != 0 {
		t.Fatalf("not one PEM block: %.60q", data)
	}
	return block
}

// decodeCert is the certificate in a PEM block this package wrote.
func decodeCert(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	cert, err := x509.ParseCertificate(onlyBlock(t, certPEM).Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func publicKey(t *testing.T, cert *x509.Certificate) *ecdsa.PublicKey {
	t.Helper()
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("public key = %T", cert.PublicKey)
	}
	return pub
}

func keyOf(t *testing.T, keyPEM []byte) *ecdsa.PrivateKey {
	t.Helper()
	parsed, err := x509.ParsePKCS8PrivateKey(onlyBlock(t, keyPEM).Bytes)
	if err != nil {
		t.Fatal(err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("key is %T", parsed)
	}
	return key
}

func pemOf(blockType string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
}

// foreignKeys are the private keys of the wrong type, as PKCS #8 PEM.
func foreignKeys(t *testing.T) map[string][]byte {
	t.Helper()
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edPub, edKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil || len(edPub) == 0 {
		t.Fatalf("Ed25519 key: %v", err)
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for name, key := range map[string]any{"P-384": p384, "Ed25519": edKey, "RSA": rsaKey} {
		der, marshalErr := x509.MarshalPKCS8PrivateKey(key)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		out[name] = pemOf(keyType, der)
	}
	return out
}

// flipEveryByte calls check with the certificate changed in one byte, once for each byte.
func flipEveryByte(t *testing.T, certPEM []byte, check func(tampered []byte, index int)) {
	t.Helper()
	der := onlyBlock(t, certPEM).Bytes
	for i := range der {
		changed := bytes.Clone(der)
		changed[i] ^= 0x01
		check(pemOf(certType, changed), i)
	}
}

type pemPair struct{ cert, key []byte }

// childCA is a CA certificate signed by root, with a key of its own.
func childCA(t *testing.T, root CA) pemPair {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "child"},
		NotBefore: start().Add(-time.Hour), NotAfter: start().AddDate(1, 0, 0),
		KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, decodeCert(t, root.CertPEM()), &key.PublicKey, keyOf(t, root.KeyPEM()))
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pemPair{cert: pemOf(certType, der), key: pemOf(keyType, keyDER)}
}

func TestP1ACAIsAnECDSAP256SelfSignedCAForTenYears(t *testing.T) {
	ca := mustCA(t, newMaker(), "team-a")
	cert := decodeCert(t, ca.CertPEM())
	if pub := publicKey(t, cert); pub.Curve != elliptic.P256() {
		t.Fatalf("curve = %v", pub.Curve.Params().Name)
	}
	if !cert.IsCA || !cert.BasicConstraintsValid || cert.MaxPathLen != 0 || !cert.MaxPathLenZero {
		t.Fatalf("basic constraints: CA %v valid %v path length %d zero %v", cert.IsCA, cert.BasicConstraintsValid, cert.MaxPathLen, cert.MaxPathLenZero)
	}
	if cert.KeyUsage != x509.KeyUsageCertSign || len(cert.ExtKeyUsage) != 0 {
		t.Fatalf("key usage = %v, extended %v", cert.KeyUsage, cert.ExtKeyUsage)
	}
	if err := cert.CheckSignatureFrom(cert); err != nil {
		t.Fatalf("not self-signed: %v", err)
	}
	if want := start().Add(-time.Hour); !cert.NotBefore.Equal(want) {
		t.Fatalf("NotBefore = %v, want %v", cert.NotBefore, want)
	}
	if want := start().AddDate(10, 0, 0); !cert.NotAfter.Equal(want) {
		t.Fatalf("NotAfter = %v, want %v", cert.NotAfter, want)
	}
	if cert.Subject.CommonName != "team-a" || cert.Issuer.CommonName != "team-a" {
		t.Fatalf("subject %q issuer %q", cert.Subject.CommonName, cert.Issuer.CommonName)
	}
	if cert.SerialNumber.Sign() <= 0 {
		t.Fatalf("serial = %v", cert.SerialNumber)
	}
}

func TestP1ACAIsNamedByOnePrintableLine(t *testing.T) {
	m := newMaker()
	for name, bad := range map[string]string{
		"empty": "", "newline": "team\na", "control": "team\x01", "non-ASCII": "téam", "too long": strings.Repeat("a", 65),
	} {
		if _, err := m.NewCA(bad); err == nil {
			t.Errorf("NewCA accepted the %s name %q", name, bad)
		}
	}
	if _, err := m.NewCA(strings.Repeat("a", 64)); err != nil {
		t.Errorf("NewCA refused a name of 64 characters: %v", err)
	}
}

func TestP1AMakerNeedsAClock(t *testing.T) {
	if _, err := (Maker{}).NewCA("team-a"); err == nil {
		t.Fatal("NewCA made a CA with no clock")
	}
	ca := mustCA(t, newMaker(), "team-a")
	if _, err := (Maker{}).NewLeaf(ca, wantNames(t)); err == nil {
		t.Fatal("NewLeaf made a leaf with no clock")
	}
}

func TestP10RandomnessIsTheInjectedReader(t *testing.T) {
	stream := bytes.Repeat([]byte{0x11}, 64)
	m := newMaker()
	m.Rand = bytes.NewReader(stream)
	first := decodeCert(t, mustCA(t, m, "team-a").CertPEM())
	m.Rand = bytes.NewReader(stream)
	second := decodeCert(t, mustCA(t, m, "team-a").CertPEM())
	if !publicKey(t, first).Equal(publicKey(t, second)) {
		t.Fatal("the same randomness made two different keys")
	}
	if first.SerialNumber.Cmp(second.SerialNumber) != 0 {
		t.Fatal("the same randomness made two different serial numbers")
	}
}

func TestP10TheDefaultRandomnessIsTheSystems(t *testing.T) {
	m := newMaker()
	a, b := mustCA(t, m, "team-a"), mustCA(t, m, "team-a")
	if bytes.Equal(a.KeyPEM(), b.KeyPEM()) {
		t.Fatal("two CAs made with the default randomness share a key")
	}
	if decodeCert(t, a.CertPEM()).SerialNumber.Cmp(decodeCert(t, b.CertPEM()).SerialNumber) == 0 {
		t.Fatal("two CAs made with the default randomness share a serial number")
	}
}

func TestP10AFailingReaderFailsTheMaking(t *testing.T) {
	m := newMaker()
	ca := mustCA(t, m, "team-a")
	failing := m
	failing.Rand = iotest.ErrReader(errBoom)
	if _, err := failing.NewCA("team-a"); !errors.Is(err, errBoom) {
		t.Errorf("NewCA error = %v, want the reader's", err)
	}
	if _, err := failing.NewLeaf(ca, wantNames(t)); !errors.Is(err, errBoom) {
		t.Errorf("NewLeaf error = %v, want the reader's", err)
	}
	short := m
	short.Rand = bytes.NewReader(bytes.Repeat([]byte{0x11}, 8))
	if _, err := short.NewCA("team-a"); err == nil {
		t.Error("NewCA made a CA from 8 bytes of randomness")
	}
}

func TestP5ACARoundTripsThroughPEM(t *testing.T) {
	m := newMaker()
	ca := mustCA(t, m, "team-a")
	got, err := ParseCA(ca.CertPEM(), ca.KeyPEM())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.CertPEM(), ca.CertPEM()) || !bytes.Equal(got.KeyPEM(), ca.KeyPEM()) {
		t.Fatal("the parsed CA is not the one that was written")
	}
	if onlyBlock(t, ca.CertPEM()).Type != certType || onlyBlock(t, ca.KeyPEM()).Type != keyType {
		t.Fatalf("block types: %q %q", onlyBlock(t, ca.CertPEM()).Type, onlyBlock(t, ca.KeyPEM()).Type)
	}
	if _, err = m.NewLeaf(got, wantNames(t)); err != nil {
		t.Fatalf("the parsed CA cannot sign: %v", err)
	}
}

func TestP6ParseCAIsStrict(t *testing.T) {
	m := newMaker()
	ca, other := mustCA(t, m, "team-a"), mustCA(t, m, "team-b")
	leaf := mustLeaf(t, m, ca, wantNames(t))
	withHeader := bytes.Replace(ca.CertPEM(), []byte("-----BEGIN CERTIFICATE-----\n"), []byte("-----BEGIN CERTIFICATE-----\nProc-Type: 4,ENCRYPTED\n\n"), 1)
	cases := map[string]pemPair{
		"no input":                            {nil, nil},
		"no certificate":                      {nil, ca.KeyPEM()},
		"no key":                              {ca.CertPEM(), nil},
		"text before the block":               {append([]byte("note\n"), ca.CertPEM()...), ca.KeyPEM()},
		"trailing newline":                    {append(bytes.Clone(ca.CertPEM()), '\n'), ca.KeyPEM()},
		"trailing bytes":                      {append(bytes.Clone(ca.CertPEM()), "extra"...), ca.KeyPEM()},
		"two certificates":                    {append(bytes.Clone(ca.CertPEM()), other.CertPEM()...), ca.KeyPEM()},
		"two keys":                            {ca.CertPEM(), append(bytes.Clone(ca.KeyPEM()), other.KeyPEM()...)},
		"key then trailing":                   {ca.CertPEM(), append(bytes.Clone(ca.KeyPEM()), "x"...)},
		"swapped":                             {ca.KeyPEM(), ca.CertPEM()},
		"another CA's key":                    {ca.CertPEM(), other.KeyPEM()},
		"a leaf as the CA":                    {leaf.CertPEM(), leaf.KeyPEM()},
		"a certificate block of another type": {pemOf("X509 CERTIFICATE", decodeCert(t, ca.CertPEM()).Raw), ca.KeyPEM()},
		"a header":                            {withHeader, ca.KeyPEM()},
		"a SEC1 key block":                    {ca.CertPEM(), pemOf("EC "+keyType, onlyBlock(t, ca.KeyPEM()).Bytes)},
		"a DER that is not a certificate":     {pemOf(certType, []byte("not DER")), ca.KeyPEM()},
	}
	for name, key := range foreignKeys(t) {
		cases["a "+name+" key"] = pemPair{ca.CertPEM(), key}
	}
	for name, c := range cases {
		if _, err := ParseCA(c.cert, c.key); err == nil {
			t.Errorf("ParseCA accepted %s", name)
		}
	}
}

func TestP6ParseCARefusesACASignedByAnother(t *testing.T) {
	child := childCA(t, mustCA(t, newMaker(), "root"))
	if _, err := ParseCA(child.cert, child.key); err == nil {
		t.Fatal("ParseCA accepted an intermediate CA, which is not a team's own self-signed root")
	}
	if _, err := Fingerprint(child.cert); err == nil {
		t.Fatal("Fingerprint accepted an intermediate CA")
	}
	if _, err := Pool(child.cert); err == nil {
		t.Fatal("Pool accepted an intermediate CA")
	}
}

func TestP7ATamperedCACertificateIsRefused(t *testing.T) {
	ca := mustCA(t, newMaker(), "team-a")
	flipEveryByte(t, ca.CertPEM(), func(tampered []byte, i int) {
		if _, err := ParseCA(tampered, ca.KeyPEM()); err == nil {
			t.Errorf("ParseCA accepted a CA certificate with byte %d changed", i)
		}
	})
}

func TestP12AnErrorNeverCarriesTheInput(t *testing.T) {
	m := newMaker()
	ca := mustCA(t, m, "team-a")
	keyBody := strings.Split(string(ca.KeyPEM()), "\n")[1]
	certBody := strings.Split(string(ca.CertPEM()), "\n")[1]
	bad := map[string]pemPair{
		"key with trailing data": {ca.CertPEM(), append(bytes.Clone(ca.KeyPEM()), "x"...)},
		"swapped":                {ca.KeyPEM(), ca.CertPEM()},
		"another key":            {ca.CertPEM(), mustCA(t, m, "team-b").KeyPEM()},
	}
	for name, in := range bad {
		_, err := ParseCA(in.cert, in.key)
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if strings.Contains(err.Error(), keyBody[:24]) || strings.Contains(err.Error(), certBody[:24]) || strings.Contains(err.Error(), "BEGIN") {
			t.Errorf("%s: the error carries the input: %v", name, err)
		}
	}
}
