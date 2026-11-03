package pki

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"regexp"
	"testing"
	"time"
)

var fingerprintRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestP8TheFingerprintIsTheSHA256OfTheCertificateInLowerCaseHex(t *testing.T) {
	m := newMaker()
	ca := mustCA(t, m, "team-a")
	got, err := Fingerprint(ca.CertPEM())
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(onlyBlock(t, ca.CertPEM()).Bytes)
	if want := hex.EncodeToString(sum[:]); got != want || !fingerprintRE.MatchString(got) {
		t.Fatalf("Fingerprint = %q, want %q", got, want)
	}
	other, err := Fingerprint(mustCA(t, m, "team-a").CertPEM())
	if err != nil {
		t.Fatal(err)
	}
	if other == got {
		t.Fatal("two CAs share a fingerprint")
	}
	again, err := Fingerprint(ca.CertPEM())
	if err != nil || again != got {
		t.Fatalf("Fingerprint is not stable: %q %q %v", got, again, err)
	}
}

func TestP8TheFingerprintIsStrict(t *testing.T) {
	m := newMaker()
	ca, other := mustCA(t, m, "team-a"), mustCA(t, m, "team-b")
	leaf := mustLeaf(t, m, ca, wantNames(t))
	for name, in := range map[string][]byte{
		"nothing":         nil,
		"a leaf":          leaf.CertPEM(),
		"the key":         ca.KeyPEM(),
		"two":             append(bytes.Clone(ca.CertPEM()), other.CertPEM()...),
		"trailing":        append(bytes.Clone(ca.CertPEM()), '\n'),
		"text before":     append([]byte("x\n"), ca.CertPEM()...),
		"not a PEM block": []byte("hello"),
	} {
		if got, err := Fingerprint(in); err == nil {
			t.Errorf("Fingerprint of %s = %q, want a refusal", name, got)
		}
	}
	flipEveryByte(t, ca.CertPEM(), func(tampered []byte, i int) {
		if _, err := Fingerprint(tampered); err == nil {
			t.Errorf("Fingerprint accepted a CA certificate with byte %d changed", i)
		}
	})
}

func TestP9ThePoolHoldsExactlyTheOneCertificate(t *testing.T) {
	m := newMaker()
	ca := mustCA(t, m, "team-a")
	pool, err := Pool(ca.CertPEM())
	if err != nil {
		t.Fatal(err)
	}
	want := x509.NewCertPool()
	want.AddCert(decodeCert(t, ca.CertPEM()))
	if !pool.Equal(want) {
		t.Fatal("the pool is not exactly the one certificate: the system's roots, or more, are in it")
	}
	system, err := x509.SystemCertPool()
	if err == nil && pool.Equal(system) {
		t.Fatal("the pool is the system's")
	}
}

func TestP9ThePoolRefusesWhatIsNotOneCACertificate(t *testing.T) {
	m := newMaker()
	ca, other := mustCA(t, m, "team-a"), mustCA(t, m, "team-b")
	leaf := mustLeaf(t, m, ca, wantNames(t))
	for name, in := range map[string][]byte{
		"nothing":          nil,
		"a leaf":           leaf.CertPEM(),
		"a bundle of CAs":  append(bytes.Clone(ca.CertPEM()), other.CertPEM()...),
		"the CA's key":     ca.KeyPEM(),
		"trailing bytes":   append(bytes.Clone(ca.CertPEM()), "x"...),
		"a different type": pemOf("X509 CERTIFICATE", decodeCert(t, ca.CertPEM()).Raw),
	} {
		if _, err := Pool(in); err == nil {
			t.Errorf("Pool accepted %s", name)
		}
	}
}

// verifyAt checks the leaf against the pool for the name at the time.
func verifyAt(t *testing.T, pool *x509.CertPool, leaf Leaf, name string, at time.Time) error {
	t.Helper()
	_, err := decodeCert(t, leaf.CertPEM()).Verify(x509.VerifyOptions{
		Roots: pool, DNSName: name, CurrentTime: at, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	return err
}

func TestP9ALeafVerifiesAgainstItsCAsPoolForItsNamesAndNoOther(t *testing.T) {
	m := newMaker()
	ca := mustCA(t, m, "team-a")
	leaf := mustLeaf(t, m, ca, wantNames(t))
	pool, err := Pool(ca.CertPEM())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"192.0.2.10", "2001:db8::1", "hs.example.test"} {
		if err = verifyAt(t, pool, leaf, name, start()); err != nil {
			t.Errorf("verify for %s: %v", name, err)
		}
	}
	var hostErr x509.HostnameError
	if err = verifyAt(t, pool, leaf, "192.0.2.11", start()); !errors.As(err, &hostErr) {
		t.Errorf("verify for another address: %v", err)
	}
}

func TestP9ALeafOfAnotherCAIsUnknownAuthority(t *testing.T) {
	m := newMaker()
	ca, other := mustCA(t, m, "team-a"), mustCA(t, m, "team-b")
	elsewhere := mustLeaf(t, m, other, wantNames(t))
	pool, err := Pool(ca.CertPEM())
	if err != nil {
		t.Fatal(err)
	}
	var unknown x509.UnknownAuthorityError
	if err = verifyAt(t, pool, elsewhere, "192.0.2.10", start()); !errors.As(err, &unknown) {
		t.Fatalf("verify: %v", err)
	}
}

func TestP9TheValidityFollowsTheClockForTheLeaf(t *testing.T) {
	m := newMaker()
	ca := mustCA(t, m, "team-a")
	leaf := mustLeaf(t, m, ca, wantNames(t))
	pool, err := Pool(ca.CertPEM())
	if err != nil {
		t.Fatal(err)
	}
	var invalid x509.CertificateInvalidError
	cases := []struct {
		name string
		at   time.Time
		ok   bool
	}{
		{"a day before it was made", start().Add(-24 * time.Hour), false},
		{"the skew allowance before", start().Add(-time.Hour + time.Second), true},
		{"when it was made", start(), true},
		{"a year less a second on", start().AddDate(1, 0, 0).Add(-time.Second), true},
		{"a year on", start().AddDate(1, 0, 0), true},
		{"a year and a second on", start().AddDate(1, 0, 0).Add(time.Second), false},
	}
	for _, c := range cases {
		err = verifyAt(t, pool, leaf, "192.0.2.10", c.at)
		switch {
		case c.ok && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case !c.ok && !errors.As(err, &invalid):
			t.Errorf("%s: want an expiry or not-yet-valid refusal, got %v", c.name, err)
		}
	}
}
