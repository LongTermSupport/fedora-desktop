package pki

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"
)

func TestP2ALeafIsSignedByTheCAForItsNamesForOneYear(t *testing.T) {
	m := newMaker()
	ca := mustCA(t, m, "team-a")
	leaf := mustLeaf(t, m, ca, Names{IPs: ips(t, "2001:db8::1", "192.0.2.10", "192.0.2.10"), DNS: "hs.example.test"})
	cert := decodeCert(t, leaf.CertPEM())
	if pub := publicKey(t, cert); pub.Curve != elliptic.P256() {
		t.Fatalf("curve = %v", pub.Curve.Params().Name)
	}
	if cert.IsCA || cert.KeyUsage != x509.KeyUsageDigitalSignature || !slices.Equal(cert.ExtKeyUsage, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}) {
		t.Fatalf("CA %v key usage %v extended %v", cert.IsCA, cert.KeyUsage, cert.ExtKeyUsage)
	}
	if err := cert.CheckSignatureFrom(decodeCert(t, ca.CertPEM())); err != nil {
		t.Fatalf("not signed by the CA: %v", err)
	}
	got := make([]string, 0, len(cert.IPAddresses))
	for _, ip := range cert.IPAddresses {
		got = append(got, ip.String())
	}
	if !slices.Equal(got, []string{"192.0.2.10", "2001:db8::1"}) {
		t.Fatalf("IP names = %v, want the two, sorted and once each", got)
	}
	if !slices.Equal(cert.DNSNames, []string{"hs.example.test"}) || len(cert.URIs) != 0 || len(cert.EmailAddresses) != 0 {
		t.Fatalf("names: DNS %v URIs %v emails %v", cert.DNSNames, cert.URIs, cert.EmailAddresses)
	}
	if want := start().Add(-time.Hour); !cert.NotBefore.Equal(want) {
		t.Fatalf("NotBefore = %v, want %v", cert.NotBefore, want)
	}
	if want := start().AddDate(1, 0, 0); !cert.NotAfter.Equal(want) || !leaf.NotAfter().Equal(want) {
		t.Fatalf("NotAfter = %v (leaf says %v), want %v", cert.NotAfter, leaf.NotAfter(), want)
	}
}

func TestP2ALeafIsValidForItsNamesAndNoOthers(t *testing.T) {
	m := newMaker()
	cert := decodeCert(t, mustLeaf(t, m, mustCA(t, m, "team-a"), wantNames(t)).CertPEM())
	for _, name := range []string{"192.0.2.10", "2001:db8::1", "hs.example.test"} {
		if err := cert.VerifyHostname(name); err != nil {
			t.Errorf("the leaf is not valid for %s: %v", name, err)
		}
	}
	for _, name := range []string{"192.0.2.11", "2001:db8::2", "other.example.test"} {
		if err := cert.VerifyHostname(name); err == nil {
			t.Errorf("the leaf is valid for %s", name)
		}
	}
}

func TestP2ALeafMayHaveOnlyAnAddressOrOnlyAName(t *testing.T) {
	m := newMaker()
	ca := mustCA(t, m, "team-a")
	only := mustLeaf(t, m, ca, Names{IPs: ips(t, "198.51.100.7")})
	if cert := decodeCert(t, only.CertPEM()); len(cert.DNSNames) != 0 || len(cert.IPAddresses) != 1 {
		t.Fatalf("names: %v %v", cert.DNSNames, cert.IPAddresses)
	}
	named := mustLeaf(t, m, ca, Names{DNS: "hs.example.test"})
	if cert := decodeCert(t, named.CertPEM()); len(cert.IPAddresses) != 0 || len(cert.DNSNames) != 1 {
		t.Fatalf("names: %v %v", cert.DNSNames, cert.IPAddresses)
	}
}

func TestP2ALeafNeverOutlivesItsCAAndNeedsAValidOne(t *testing.T) {
	m, clk := newWorld()
	ca := mustCA(t, m, "team-a")
	clk.Advance(ca.NotAfter().Sub(now(clk)) - 100*24*time.Hour)
	leaf := mustLeaf(t, m, ca, wantNames(t))
	if !leaf.NotAfter().Equal(ca.NotAfter()) {
		t.Fatalf("leaf ends %v, CA ends %v: the leaf outlives its CA", leaf.NotAfter(), ca.NotAfter())
	}
	clk.Advance(101 * 24 * time.Hour)
	if _, err := m.NewLeaf(ca, wantNames(t)); err == nil {
		t.Fatal("NewLeaf signed with an expired CA")
	}
	early, earlyClk := newWorld()
	earlyCA := mustCA(t, early, "team-b")
	earlyClk.Advance(-2 * time.Hour)
	if _, err := early.NewLeaf(earlyCA, wantNames(t)); err == nil {
		t.Fatal("NewLeaf signed with a CA that is not yet valid")
	}
}

func TestP2ANewLeafRefusesAnEmptyCA(t *testing.T) {
	if _, err := newMaker().NewLeaf(CA{}, wantNames(t)); err == nil {
		t.Fatal("NewLeaf signed with a zero CA")
	}
}

// X31: a leaf says when it was made, to the second, from its own certificate, so that the
// health tick can tell a terminator started before it (which serves the leaf it read at its start)
// from one started after; the parsed leaf says the same as the one made.
func TestALeafSaysWhenItWasMade(t *testing.T) {
	m, clk := newWorld()
	ca := mustCA(t, m, "team-a")
	clk.Advance(90*24*time.Hour + 1500*time.Millisecond)
	leaf := mustLeaf(t, m, ca, wantNames(t))
	want := now(m.Clock).Truncate(time.Second)
	if !leaf.MadeAt().Equal(want) {
		t.Fatalf("MadeAt = %v, want %v", leaf.MadeAt(), want)
	}
	parsed, err := ParseLeaf(ca.CertPEM(), leaf.CertPEM(), leaf.KeyPEM())
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.MadeAt().Equal(want) {
		t.Fatalf("the parsed leaf's MadeAt = %v, want %v", parsed.MadeAt(), want)
	}
}

func TestP4ALeafIsKeptWhileItsNamesAndItsTimeHold(t *testing.T) {
	m := newMaker()
	leaf := mustLeaf(t, m, mustCA(t, m, "team-a"), wantNames(t))
	reordered := Names{IPs: ips(t, "2001:db8::1", "192.0.2.10", "2001:db8::1"), DNS: "hs.example.test"}
	for name, want := range map[string]Names{"as made": wantNames(t), "reordered and doubled": reordered} {
		got, err := leaf.NeedsRenewal(m.Clock, want)
		if err != nil || got != NoRenewal {
			t.Errorf("%s: NeedsRenewal = %v, %v", name, got, err)
		}
	}
}

func TestP4ALeafIsMadeAgainWhenItsNamesDiffer(t *testing.T) {
	m := newMaker()
	leaf := mustLeaf(t, m, mustCA(t, m, "team-a"), wantNames(t))
	for name, want := range map[string]Names{
		"an address added":   {IPs: ips(t, "192.0.2.10", "2001:db8::1", "198.51.100.1"), DNS: "hs.example.test"},
		"an address missing": {IPs: ips(t, "192.0.2.10"), DNS: "hs.example.test"},
		"an address changed": {IPs: ips(t, "192.0.2.11", "2001:db8::1"), DNS: "hs.example.test"},
		"the name changed":   {IPs: ips(t, "192.0.2.10", "2001:db8::1"), DNS: "other.example.test"},
		"the name dropped":   {IPs: ips(t, "192.0.2.10", "2001:db8::1")},
	} {
		got, err := leaf.NeedsRenewal(m.Clock, want)
		if err != nil || got != NamesChanged {
			t.Errorf("%s: NeedsRenewal = %v, %v", name, got, err)
		}
	}
	noName := mustLeaf(t, m, mustCA(t, m, "team-a"), Names{IPs: ips(t, "192.0.2.10")})
	if got, err := noName.NeedsRenewal(m.Clock, wantNames(t)); err != nil || got != NamesChanged {
		t.Errorf("a name added: NeedsRenewal = %v, %v", got, err)
	}
}

func TestP4ALeafIsMadeAgainWhenFewerThanThirtyDaysRemain(t *testing.T) {
	m, clk := newWorld()
	leaf := mustLeaf(t, m, mustCA(t, m, "team-a"), wantNames(t))
	clk.Advance(leaf.NotAfter().Sub(now(clk)) - 30*24*time.Hour)
	if got, err := leaf.NeedsRenewal(clk, wantNames(t)); err != nil || got != NoRenewal {
		t.Fatalf("with exactly 30 days left: NeedsRenewal = %v, %v", got, err)
	}
	clk.Advance(time.Second)
	if got, err := leaf.NeedsRenewal(clk, wantNames(t)); err != nil || got != Expiring {
		t.Fatalf("with 30 days less a second left: NeedsRenewal = %v, %v", got, err)
	}
	clk.Advance(400 * 24 * time.Hour)
	if got, err := leaf.NeedsRenewal(clk, wantNames(t)); err != nil || got != Expiring {
		t.Fatalf("an expired leaf: NeedsRenewal = %v, %v", got, err)
	}
}

func TestP4ANameChangeIsReportedBeforeExpiry(t *testing.T) {
	m, clk := newWorld()
	leaf := mustLeaf(t, m, mustCA(t, m, "team-a"), wantNames(t))
	clk.Advance(360 * 24 * time.Hour)
	got, err := leaf.NeedsRenewal(clk, Names{IPs: ips(t, "192.0.2.99")})
	if err != nil || got != NamesChanged {
		t.Fatalf("NeedsRenewal = %v, %v", got, err)
	}
}

func TestP4RenewalRefusesNamesThatAreNotValid(t *testing.T) {
	m := newMaker()
	leaf := mustLeaf(t, m, mustCA(t, m, "team-a"), wantNames(t))
	if _, err := leaf.NeedsRenewal(m.Clock, Names{}); err == nil {
		t.Fatal("NeedsRenewal judged an empty name set")
	}
	if _, err := leaf.NeedsRenewal(nil, wantNames(t)); err == nil {
		t.Fatal("NeedsRenewal judged with no clock")
	}
}

func TestP4RenewalHasWordsForTheLog(t *testing.T) {
	for r, want := range map[Renewal]string{NoRenewal: "kept", NamesChanged: "its names changed", Expiring: "fewer than 30 days remain"} {
		if r.String() != want || r.Needed() != (r != NoRenewal) {
			t.Errorf("%d: %q needed %v", r, r.String(), r.Needed())
		}
	}
}

func TestP5ALeafRoundTripsThroughPEM(t *testing.T) {
	m := newMaker()
	ca := mustCA(t, m, "team-a")
	leaf := mustLeaf(t, m, ca, wantNames(t))
	got, err := ParseLeaf(ca.CertPEM(), leaf.CertPEM(), leaf.KeyPEM())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.CertPEM(), leaf.CertPEM()) || !bytes.Equal(got.KeyPEM(), leaf.KeyPEM()) || !got.NotAfter().Equal(leaf.NotAfter()) {
		t.Fatal("the parsed leaf is not the one that was written")
	}
	if onlyBlock(t, leaf.CertPEM()).Type != certType || onlyBlock(t, leaf.KeyPEM()).Type != keyType {
		t.Fatalf("block types: %q %q", onlyBlock(t, leaf.CertPEM()).Type, onlyBlock(t, leaf.KeyPEM()).Type)
	}
	if need, err := got.NeedsRenewal(m.Clock, wantNames(t)); err != nil || need != NoRenewal {
		t.Fatalf("the parsed leaf: NeedsRenewal = %v, %v", need, err)
	}
	wantSet := wantNames(t)
	gotNames := got.Names()
	if gotNames.DNS != wantSet.DNS || !slices.Equal(gotNames.IPs, wantSet.IPs) {
		t.Fatalf("Names = %+v, want %+v", gotNames, wantSet)
	}
}

// issuedBy signs template as a leaf of ca with key as its key, so a test can make the
// certificates this package would never make.
func issuedBy(t *testing.T, ca CA, tmpl *x509.Certificate, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	tmpl.SerialNumber = big.NewInt(99)
	tmpl.NotBefore, tmpl.NotAfter = start().Add(-time.Hour), start().AddDate(1, 0, 0)
	der, err := x509.CreateCertificate(rand.Reader, tmpl, decodeCert(t, ca.CertPEM()), &key.PublicKey, keyOf(t, ca.KeyPEM()))
	if err != nil {
		t.Fatal(err)
	}
	return pemOf(certType, der)
}

func newKeyPEM(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return key, pemOf(keyType, der)
}

func TestP6ParseLeafIsStrict(t *testing.T) {
	m := newMaker()
	ca, other := mustCA(t, m, "team-a"), mustCA(t, m, "team-b")
	leaf := mustLeaf(t, m, ca, wantNames(t))
	elsewhere := mustLeaf(t, m, other, wantNames(t))
	twin := mustLeaf(t, m, ca, wantNames(t))
	base := func() *x509.Certificate {
		return &x509.Certificate{
			Subject: pkix.Name{CommonName: "x"}, KeyUsage: x509.KeyUsageDigitalSignature,
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true,
			IPAddresses: []net.IP{net.ParseIP("192.0.2.10")},
		}
	}
	type input struct{ ca, cert, key []byte }
	cases := map[string]input{
		"no input":                  {ca.CertPEM(), nil, nil},
		"no key":                    {ca.CertPEM(), leaf.CertPEM(), nil},
		"no CA":                     {nil, leaf.CertPEM(), leaf.KeyPEM()},
		"trailing bytes":            {ca.CertPEM(), append(bytes.Clone(leaf.CertPEM()), "x"...), leaf.KeyPEM()},
		"a chain of two blocks":     {ca.CertPEM(), append(bytes.Clone(leaf.CertPEM()), ca.CertPEM()...), leaf.KeyPEM()},
		"two leaves":                {ca.CertPEM(), append(bytes.Clone(leaf.CertPEM()), twin.CertPEM()...), leaf.KeyPEM()},
		"two keys":                  {ca.CertPEM(), leaf.CertPEM(), append(bytes.Clone(leaf.KeyPEM()), twin.KeyPEM()...)},
		"the CA's own certificate":  {ca.CertPEM(), ca.CertPEM(), leaf.KeyPEM()},
		"a leaf of another CA":      {ca.CertPEM(), elsewhere.CertPEM(), elsewhere.KeyPEM()},
		"the other CA as the CA":    {other.CertPEM(), leaf.CertPEM(), leaf.KeyPEM()},
		"another leaf's key":        {ca.CertPEM(), leaf.CertPEM(), twin.KeyPEM()},
		"the key as the leaf":       {ca.CertPEM(), leaf.KeyPEM(), leaf.CertPEM()},
		"a certificate of the type": {ca.CertPEM(), pemOf("X509 CERTIFICATE", decodeCert(t, leaf.CertPEM()).Raw), leaf.KeyPEM()},
	}
	for name, key := range foreignKeys(t) {
		cases["a "+name+" key"] = input{ca.CertPEM(), leaf.CertPEM(), key}
	}
	odd := map[string]func(*x509.Certificate){
		"a CA certificate":           func(c *x509.Certificate) { c.IsCA = true; c.KeyUsage |= x509.KeyUsageCertSign },
		"a client certificate":       func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth} },
		"one with no usage":          func(c *x509.Certificate) { c.ExtKeyUsage = nil },
		"one with no names":          func(c *x509.Certificate) { c.IPAddresses = nil },
		"one with an e-mail name":    func(c *x509.Certificate) { c.EmailAddresses = []string{"a@example.test"} },
		"one with two DNS names":     func(c *x509.Certificate) { c.DNSNames = []string{"a.example.test", "b.example.test"} },
		"one with an upper-case DNS": func(c *x509.Certificate) { c.DNSNames = []string{"HS.example.test"} },
	}
	for name, edit := range odd {
		tmpl := base()
		edit(tmpl)
		key, keyPEM := newKeyPEM(t)
		cases[name] = input{ca.CertPEM(), issuedBy(t, ca, tmpl, key), keyPEM}
	}
	for name, c := range cases {
		if _, err := ParseLeaf(c.ca, c.cert, c.key); err == nil {
			t.Errorf("ParseLeaf accepted %s", name)
		}
	}
}

func TestP6ParseLeafRefusesACertificateNamingAnotherIssuer(t *testing.T) {
	m := newMaker()
	ca := mustCA(t, m, "team-a")
	// Signed with the CA's key, but a certificate that says another subject issued it.
	key, keyPEM := newKeyPEM(t)
	imposter := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "someone else"},
		NotBefore: start().Add(-time.Hour), NotAfter: start().AddDate(1, 0, 0),
		KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true, IsCA: true,
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "x"},
		NotBefore: start().Add(-time.Hour), NotAfter: start().AddDate(1, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IPAddresses: []net.IP{net.ParseIP("192.0.2.10")},
	}
	der, err := x509.CreateCertificate(rand.Reader, leafTmpl, imposter, &key.PublicKey, keyOf(t, ca.KeyPEM()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ParseLeaf(ca.CertPEM(), pemOf(certType, der), keyPEM); err == nil {
		t.Fatal("ParseLeaf accepted a leaf whose issuer is not the CA's subject")
	}
}

func TestP7ATamperedLeafCertificateIsRefused(t *testing.T) {
	m := newMaker()
	ca := mustCA(t, m, "team-a")
	leaf := mustLeaf(t, m, ca, wantNames(t))
	flipEveryByte(t, leaf.CertPEM(), func(tampered []byte, i int) {
		if _, err := ParseLeaf(ca.CertPEM(), tampered, leaf.KeyPEM()); err == nil {
			t.Errorf("ParseLeaf accepted a leaf with byte %d changed", i)
		}
	})
}

func TestP7ATamperedCAMakesTheLeafUnsigned(t *testing.T) {
	m := newMaker()
	ca := mustCA(t, m, "team-a")
	leaf := mustLeaf(t, m, ca, wantNames(t))
	flipEveryByte(t, ca.CertPEM(), func(tampered []byte, i int) {
		if _, err := ParseLeaf(tampered, leaf.CertPEM(), leaf.KeyPEM()); err == nil {
			t.Errorf("ParseLeaf accepted a leaf under a CA certificate with byte %d changed", i)
		}
	})
}

func TestP3TheLeafKeepsItsNamesAsNetipAddresses(t *testing.T) {
	m := newMaker()
	leaf := mustLeaf(t, m, mustCA(t, m, "team-a"), Names{IPs: ips(t, "192.0.2.10")})
	got := leaf.Names()
	if got.DNS != "" || !slices.Equal(got.IPs, []netip.Addr{addr(t, "192.0.2.10")}) {
		t.Fatalf("Names = %+v", got)
	}
}
