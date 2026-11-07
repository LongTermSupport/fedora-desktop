package host

import (
	"bytes"
	"errors"
	"io"
	"maps"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
)

// The tests are Plan 00246 SPEC-cross-network X13 and X14 at the host: the team's CA and the
// homeserver's leaf, made at `team apply`, kept in root/<team>/tls/, and the CA carried by the
// team's backup.

func bytesOf(t *testing.T, w *world, p string) []byte {
	t.Helper()
	return readFile(t, w.at(p))
}

// theCA is the CA the host holds, parsed from its files.
func theCA(t *testing.T, w *world) pki.CA {
	t.Helper()
	team := ops(t)
	ca, err := pki.ParseCA(bytesOf(t, w, paths.CACertFile(team)), bytesOf(t, w, paths.CAKeyFile(team)))
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

// theLeaf is the leaf the host holds, parsed against its CA.
func theLeaf(t *testing.T, w *world) pki.Leaf {
	t.Helper()
	team := ops(t)
	leaf, err := pki.ParseLeaf(theCA(t, w).CertPEM(), bytesOf(t, w, paths.LeafCertFile(team)), bytesOf(t, w, paths.LeafKeyFile(team)))
	if err != nil {
		t.Fatal(err)
	}
	return leaf
}

func sameNames(got pki.Names, ips []string, dns string) bool {
	return got.DNS == dns && slices.Equal(got.IPs, addrs(ips...))
}

// certificateFiles are the four files of root/<team>/tls/ as bytes, by path.
func certificateFiles(t *testing.T, w *world) map[string][]byte {
	t.Helper()
	team := ops(t)
	out := map[string][]byte{}
	for _, p := range []string{paths.CACertFile(team), paths.CAKeyFile(team), paths.LeafCertFile(team), paths.LeafKeyFile(team)} {
		out[p] = bytesOf(t, w, p)
	}
	return out
}

func sameFiles(a, b map[string][]byte, only ...string) bool {
	for p, data := range a {
		if (len(only) == 0 || slices.Contains(only, p)) && !bytes.Equal(data, b[p]) {
			return false
		}
	}
	return true
}

// wall0 is the fake clock as pki reads it.
type wall0 struct{ c *fakeClock }

func (w wall0) WallMS() int64 { return w.c.Now().UnixMilli() }

// X13, X14: a first apply makes the CA and the leaf under root/<team>/tls/, with the modes the
// secrets need, the validity from the host's clock and the names from the derived addresses.
func TestFirstApplyMakesTheCAAndTheLeaf(t *testing.T) {
	w := newWorld(t)
	lines, err := w.apply(approved(w, teamDoc()))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(lines, "ca made") || !slices.Contains(lines, "leaf made") {
		t.Errorf("lines %q", lines)
	}
	before(t, w.log, "approvals.load", "add")
	team := ops(t)
	for p, m := range map[string]os.FileMode{
		paths.TLSDir(team): 0o700, paths.CAKeyFile(team): 0o600, paths.CACertFile(team): 0o644,
		paths.LeafKeyFile(team): 0o600, paths.LeafCertFile(team): 0o644,
	} {
		if got := mode(t, w.at(p)); got != m {
			t.Errorf("%s: mode %v, want %v", p, got, m)
		}
	}
	ca, leaf := theCA(t, w), theLeaf(t, w)
	if want := t0.UTC().AddDate(10, 0, 0); !ca.NotAfter().Equal(want) {
		t.Errorf("the CA is valid until %v, want %v: ten years from the host's clock", ca.NotAfter(), want)
	}
	if want := t0.UTC().AddDate(1, 0, 0); !leaf.NotAfter().Equal(want) {
		t.Errorf("the leaf is valid until %v, want %v: a year from the host's clock", leaf.NotAfter(), want)
	}
	if !sameNames(leaf.Names(), []string{"192.0.2.10"}, "") {
		t.Errorf("the leaf's names %+v, want the one listen address", leaf.Names())
	}
	if bytes.Equal(ca.KeyPEM(), bytesOf(t, w, paths.LeafKeyFile(team))) {
		t.Error("the leaf is signed with its own key as a CA")
	}
	lockIsFree(t, w)
}

// X14: the names are every derived listen address, and the url host when it is a name.
func TestTheLeafNamesTheListenAddressesAndTheURLHost(t *testing.T) {
	for name, c := range map[string]struct {
		setup func(w *world)
		doc   func(d map[string]any)
		ips   []string
		dns   string
	}{
		"the one listen address": {func(*world) {}, func(map[string]any) {}, []string{"192.0.2.10"}, ""},
		"a DNS url host as well": {
			func(*world) {}, func(d map[string]any) { docTeam(d)["url"] = "https://chat.example.com:8448" },
			[]string{"192.0.2.10"},
			"chat.example.com",
		},
		"two networks on two interfaces give both": {
			withSecondNIC, func(d map[string]any) { docTeam(d)["networks"] = []any{"192.0.2.0/24", "203.0.113.0/24"} },
			[]string{"192.0.2.10", "203.0.113.9"},
			"",
		},
		"an IPv6 listen address": {
			func(w *world) {
				w.net.ifaces[1].Addrs = addrs("192.0.2.10", "2001:db8::10")
				w.net.routes = append(w.net.routes, fakeRoute{netip.MustParsePrefix("2001:db8::/32"), "eth0"})
			},
			func(d map[string]any) {
				docTeam(d)["networks"] = []any{"192.0.2.0/24", "2001:db8::/32"}
				docTeam(d)["url"] = "http://[2001:db8::10]:8448"
			},
			[]string{"192.0.2.10", "2001:db8::10"},
			"",
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			c.setup(w)
			doc := teamDoc()
			c.doc(doc)
			if _, err := w.apply(approved(w, doc)); err != nil {
				t.Fatal(err)
			}
			if got := theLeaf(t, w).Names(); !sameNames(got, c.ips, c.dns) {
				t.Errorf("the leaf's names %+v, want %v and %q", got, c.ips, c.dns)
			}
		})
	}
}

// X13, X14: a second apply of the same team changes nothing, and says nothing of the files.
func TestSecondApplyKeepsTheCertificates(t *testing.T) {
	w := applied(t)
	was := certificateFiles(t, w)
	lines, err := w.apply(w.cur)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		if strings.Contains(line, "ca ") || strings.Contains(line, "leaf") {
			t.Errorf("a second apply reports %q", line)
		}
	}
	if !sameFiles(certificateFiles(t, w), was) || !sameFiles(was, certificateFiles(t, w)) {
		t.Error("a second apply changed a certificate file")
	}
}

// X14: a changed network is a changed name set, and makes the leaf again and nothing else; the
// CA is never made again, and taking the network away reverses it.
func TestAChangedNetworkMakesOnlyTheLeafAgain(t *testing.T) {
	w := newWorld(t)
	withSecondNIC(w)
	narrow, wide := teamDoc(), teamDoc()
	docTeam(wide)["networks"] = []any{"192.0.2.0/24", "203.0.113.0/24"}
	if _, err := w.apply(approved(w, narrow)); err != nil {
		t.Fatal(err)
	}
	first := certificateFiles(t, w)
	team := ops(t)
	lines, err := w.apply(approved(w, wide))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(lines, "leaf made again: its names changed") || slices.Contains(lines, "ca made") {
		t.Errorf("lines %q", lines)
	}
	second := certificateFiles(t, w)
	if !sameFiles(first, second, paths.CACertFile(team), paths.CAKeyFile(team)) {
		t.Error("a new network made the CA again")
	}
	if bytes.Equal(first[paths.LeafCertFile(team)], second[paths.LeafCertFile(team)]) {
		t.Error("a new network kept the leaf")
	}
	if got := theLeaf(t, w).Names(); !sameNames(got, []string{"192.0.2.10", "203.0.113.9"}, "") {
		t.Errorf("the leaf's names %+v", got)
	}
	lines, err = w.apply(approved(w, narrow))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(lines, "leaf made again: its names changed") {
		t.Errorf("taking the network away: lines %q", lines)
	}
	if got := theLeaf(t, w).Names(); !sameNames(got, []string{"192.0.2.10"}, "") {
		t.Errorf("the leaf's names %+v", got)
	}
	if !sameFiles(certificateFiles(t, w), first, paths.CACertFile(team), paths.CAKeyFile(team)) {
		t.Error("the CA changed")
	}
}

// X14: a leaf with fewer than 30 days left is made again, by the host's clock; with 30 days or
// more it is kept.
func TestALeafNearExpiryIsMadeAgain(t *testing.T) {
	w := applied(t)
	was := certificateFiles(t, w)
	end := theLeaf(t, w).NotAfter()
	day := 24 * time.Hour
	w.clock.now = end.Add(-31 * day)
	lines, err := w.apply(w.cur)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		if strings.Contains(line, "leaf") {
			t.Errorf("31 days before the end: %q", line)
		}
	}
	w.clock.now = end.Add(-29 * day)
	if lines, err = w.apply(w.cur); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(lines, "leaf made again: fewer than 30 days remain") || slices.Contains(lines, "ca made") {
		t.Errorf("29 days before the end: lines %q", lines)
	}
	team := ops(t)
	now := certificateFiles(t, w)
	if bytes.Equal(was[paths.LeafCertFile(team)], now[paths.LeafCertFile(team)]) || bytes.Equal(was[paths.LeafKeyFile(team)], now[paths.LeafKeyFile(team)]) {
		t.Error("the leaf near its end was kept")
	}
	if !sameFiles(was, now, paths.CACertFile(team), paths.CAKeyFile(team)) {
		t.Error("the CA changed")
	}
	if renewed := theLeaf(t, w).NotAfter(); !renewed.After(end) {
		t.Errorf("the new leaf ends %v, the old one %v", renewed, end)
	}
}

// X13, X14: a leaf the host cannot trust (a pair torn by a crash, one signed by another CA, one
// gone) is made again, and the apply says why; the CA is untouched.
func TestALeafThatDoesNotVerifyIsMadeAgain(t *testing.T) {
	team := ops(t)
	for name, damage := range map[string]func(t *testing.T, w *world){
		"a key that is not the certificate's": func(t *testing.T, w *world) {
			t.Helper()
			other, err := pki.Maker{Clock: wall0{w.clock}, Rand: &stream{seed: 9}}.NewCA("another")
			if err != nil {
				t.Fatal(err)
			}
			w.write(paths.LeafKeyFile(team), string(other.KeyPEM()))
		},
		"a certificate of garbage": func(t *testing.T, w *world) { t.Helper(); w.write(paths.LeafCertFile(team), "junk") },
		"a missing key":            func(t *testing.T, w *world) { t.Helper(); deleteAt(t, w, paths.LeafKeyFile(team)) },
		"a missing certificate":    func(t *testing.T, w *world) { t.Helper(); deleteAt(t, w, paths.LeafCertFile(team)) },
	} {
		t.Run(name, func(t *testing.T) {
			w := applied(t)
			was := certificateFiles(t, w)
			damage(t, w)
			lines, err := w.apply(w.cur)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, "leaf made again: ") }) || slices.Contains(lines, "ca made") {
				t.Errorf("lines %q", lines)
			}
			if !sameFiles(certificateFiles(t, w), was, paths.CACertFile(team), paths.CAKeyFile(team)) {
				t.Error("the CA changed")
			}
			if got := theLeaf(t, w).Names(); !sameNames(got, []string{"192.0.2.10"}, "") {
				t.Errorf("the leaf's names %+v", got)
			}
		})
	}
}

func deleteAt(t *testing.T, w *world, p string) {
	t.Helper()
	if err := os.Remove(w.at(p)); err != nil {
		t.Fatal(err)
	}
}

// X13: a CA that cannot be used is refused, naming the file, and never replaced; nothing after the
// step runs and nothing in root/<team>/tls/ changes.
type badCA struct {
	damage func(t *testing.T, w *world)
	file   string
	text   string
}

// badCAs are the ways a CA the host made can be left unusable, and what the refusal says.
func badCAs(team paths.TeamID) map[string]badCA {
	return map[string]badCA{
		"a certificate of garbage": {
			func(t *testing.T, w *world) { t.Helper(); w.write(paths.CACertFile(team), "junk") }, "ca.crt", "does not start with a PEM block",
		},
		"a key of garbage": {
			func(t *testing.T, w *world) { t.Helper(); w.write(paths.CAKeyFile(team), "junk") }, "ca.key", "does not start with a PEM block",
		},
		"a key that is another CA's": {
			func(t *testing.T, w *world) {
				t.Helper()
				other, err := pki.Maker{Clock: wall0{w.clock}, Rand: &stream{seed: 9}}.NewCA("another")
				if err != nil {
					t.Fatal(err)
				}
				w.write(paths.CAKeyFile(team), string(other.KeyPEM()))
			}, "ca.key", "is not the CA certificate's",
		},
		"the leaf's certificate in the CA's place": {
			func(t *testing.T, w *world) {
				t.Helper()
				w.write(paths.CACertFile(team), w.read(paths.LeafCertFile(team)))
			}, "ca.crt", "is not a CA certificate",
		},
		"a certificate with no key": {
			func(t *testing.T, w *world) { t.Helper(); deleteAt(t, w, paths.CAKeyFile(team)) }, "ca.key", "is not there",
		},
		"a key with no certificate": {
			func(t *testing.T, w *world) { t.Helper(); deleteAt(t, w, paths.CACertFile(team)) }, "ca.crt", "is not there",
		},
		"a key that is a link": {
			func(t *testing.T, w *world) {
				t.Helper()
				target := filepath.Join(t.TempDir(), "elsewhere")
				if err := os.WriteFile(target, bytesOf(t, w, paths.CAKeyFile(team)), 0o600); err != nil {
					t.Fatal(err)
				}
				deleteAt(t, w, paths.CAKeyFile(team))
				if err := os.Symlink(target, w.at(paths.CAKeyFile(team))); err != nil {
					t.Fatal(err)
				}
			}, "ca.key", "cannot open",
		},
	}
}

func TestACAThatCannotBeUsedIsRefusedAndNeverReplaced(t *testing.T) {
	for name, c := range badCAs(ops(t)) {
		t.Run(name, func(t *testing.T) {
			w := applied(t)
			c.damage(t, w)
			held := heldFiles(t, w)
			lines, err := w.apply(w.cur)
			e := stepOf(t, err)
			if e.Step != StepTLS || !e.Drift || !strings.Contains(err.Error(), c.file) || !strings.Contains(err.Error(), c.text) {
				t.Errorf("got %v (drift %v), want a refusal at %s naming %s and saying %q", err, e.Drift, StepTLS, c.file, c.text)
			}
			if strings.Contains(err.Error(), "PRIVATE") {
				t.Error("the refusal carries key material")
			}
			if len(lines) != 0 || w.called("add") || w.called("start") || w.called("restart") || w.called("bootstrap") {
				t.Errorf("after the refusal: lines %q, calls %q", lines, w.log)
			}
			if got := heldFiles(t, w); !maps.Equal(got, held) {
				t.Errorf("root/<team>/tls/ changed: now %d files, was %d, or a file's bytes differ", len(got), len(held))
			}
			lockIsFree(t, w)
		})
	}
}

// heldFiles is every file in root/<team>/tls/ by name, with its bytes (a link read through).
func heldFiles(t *testing.T, w *world) map[string]string {
	t.Helper()
	dir := w.at(paths.TLSDir(ops(t)))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	held := make(map[string]string, len(entries))
	for _, e := range entries {
		held[e.Name()] = string(readFile(t, filepath.Join(dir, e.Name())))
	}
	return held
}

// X13: randomness comes from the injected reader, and a reader that fails stops the making before
// any file is written, naming the step.
func TestAFailingReaderMakesNoCA(t *testing.T) {
	w := newWorld(t)
	in := approved(w, teamDoc())
	d := w.deps()
	d.Rand = io.MultiReader(bytes.NewReader(bytes.Repeat([]byte{0xab}, 64)), iotest.ErrReader(errFault))
	w.cur, w.log = in, nil
	lines, err := Apply(t.Context(), d, in)
	if e := stepOf(t, err); e.Step != StepTLS || e.Drift || !errors.Is(err, errFault) {
		t.Fatalf("got %v, want a failure at %s wrapping the reader's error", err, StepTLS)
	}
	team := ops(t)
	for _, p := range []string{paths.CAKeyFile(team), paths.CACertFile(team), paths.LeafKeyFile(team), paths.LeafCertFile(team)} {
		if w.exists(p) {
			t.Errorf("%s was written", p)
		}
	}
	if slices.Contains(lines, "ca made") || w.called("add") || w.called("start") {
		t.Errorf("after the failure: lines %q, calls %q", lines, w.log)
	}
	lockIsFree(t, w)
}

// X13: the keys are the injected reader's and nothing else's: the same randomness makes the same
// CA key and leaf key. (A signature carries randomness the standard library draws for itself, so
// the certificates are not compared whole.)
func TestTheKeysAreTheInjectedReaders(t *testing.T) {
	a, b := newWorld(t), newWorld(t)
	for _, w := range []*world{a, b} {
		if _, err := w.apply(approved(w, teamDoc())); err != nil {
			t.Fatal(err)
		}
	}
	team := ops(t)
	if !sameFiles(certificateFiles(t, a), certificateFiles(t, b), paths.CAKeyFile(team), paths.LeafKeyFile(team)) {
		t.Error("two hosts with one reader made different keys")
	}
}

// X13: the team's backup carries the CA certificate and the CA key, with the modes they have, and
// not the leaf, which the next apply makes again.
func TestTheBackupCarriesTheCAAndNotTheLeaf(t *testing.T) {
	w := applied(t)
	team := ops(t)
	files, err := CABackupFiles(w.root, team)
	if err != nil {
		t.Fatal(err)
	}
	want := []BackupFile{
		{Name: "ca.crt", Mode: 0o644, Data: bytesOf(t, w, paths.CACertFile(team))},
		{Name: "ca.key", Mode: 0o600, Data: bytesOf(t, w, paths.CAKeyFile(team))},
	}
	if len(files) != len(want) {
		t.Fatalf("the backup carries %d files, want %d: %+v", len(files), len(want), files)
	}
	for i, f := range files {
		if f.Name != want[i].Name || f.Mode != want[i].Mode || !bytes.Equal(f.Data, want[i].Data) {
			t.Errorf("member %d is %s, mode %v; want %s, mode %v, the file's bytes", i, f.Name, f.Mode, want[i].Name, want[i].Mode)
		}
	}
	for _, p := range []string{paths.LeafCertFile(team), paths.LeafKeyFile(team)} {
		if !w.exists(p) {
			t.Fatalf("%s is not there to leave out", p)
		}
		for _, f := range files {
			if f.Name == filepath.Base(p) || bytes.Equal(f.Data, bytesOf(t, w, p)) {
				t.Errorf("the backup carries the leaf's %s", p)
			}
		}
	}
}

// X13: a team with no CA yet has nothing of one to carry; a CA the host cannot use is refused,
// as a backup that could not be restored would be worse than none.
func TestTheBackupOfNoCAOrABadCA(t *testing.T) {
	w := newWorld(t)
	team := ops(t)
	if files, err := CABackupFiles(w.root, team); err != nil || len(files) != 0 {
		t.Errorf("a team with no CA: %+v, %v", files, err)
	}
	w = applied(t)
	w.write(paths.CAKeyFile(team), "junk")
	if _, err := CABackupFiles(w.root, team); err == nil || !strings.Contains(err.Error(), "ca.key") {
		t.Errorf("a bad CA key: %v, want a refusal naming ca.key", err)
	}
}

// restored removes root/<team>/tls/, restores the backup's files into it, and answers the files.
func restored(t *testing.T, w *world, files []BackupFile) {
	t.Helper()
	if err := os.RemoveAll(w.at(paths.TLSDir(ops(t)))); err != nil {
		t.Fatal(err)
	}
	if err := CheckRestoreCA(w.root, ops(t), files); err != nil {
		t.Fatalf("the check before the restore: %v", err)
	}
	written, err := RestoreCA(w.root, ops(t), files)
	if err != nil {
		t.Fatal(err)
	}
	if !written {
		t.Error("a restore into a team with no CA says it wrote none")
	}
}

// X13: a restore puts the CA back as it was, with the same modes, and not the leaf; the next
// apply makes the leaf again under the restored CA, so every bundle issued stays valid.
func TestRestoreRoundTrip(t *testing.T) {
	w := applied(t)
	team := ops(t)
	files, err := CABackupFiles(w.root, team)
	if err != nil {
		t.Fatal(err)
	}
	before := certificateFiles(t, w)
	pem, err := CAPEM(w.root, team)
	if err != nil {
		t.Fatal(err)
	}
	restored(t, w, files)
	now := map[string][]byte{}
	for _, p := range []string{paths.CACertFile(team), paths.CAKeyFile(team)} {
		now[p] = bytesOf(t, w, p)
	}
	if !sameFiles(before, now, paths.CACertFile(team), paths.CAKeyFile(team)) {
		t.Error("the restored CA is not the one backed up")
	}
	for p, m := range map[string]os.FileMode{paths.TLSDir(team): 0o700, paths.CAKeyFile(team): 0o600, paths.CACertFile(team): 0o644} {
		if got := mode(t, w.at(p)); got != m {
			t.Errorf("%s: mode %v, want %v", p, got, m)
		}
	}
	for _, p := range []string{paths.LeafCertFile(team), paths.LeafKeyFile(team)} {
		if w.exists(p) {
			t.Errorf("a restore brought back %s", p)
		}
	}
	lines, err := w.apply(w.cur)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(lines, "leaf made") || slices.Contains(lines, "ca made") {
		t.Errorf("the apply after the restore: lines %q", lines)
	}
	if got, err := CAPEM(w.root, team); err != nil || got != pem {
		t.Errorf("the CA a bundle carries changed over the restore: %v", err)
	}
	theLeaf(t, w)
}

// X13: a restore with the CA already in place is a success that changes nothing, and never
// replaces another CA.
func TestRestoreNeverReplacesACA(t *testing.T) {
	w := applied(t)
	team := ops(t)
	files, err := CABackupFiles(w.root, team)
	if err != nil {
		t.Fatal(err)
	}
	was := certificateFiles(t, w)
	if err = CheckRestoreCA(w.root, team, files); err != nil {
		t.Fatalf("the check of the same CA over itself: %v", err)
	}
	written, err := RestoreCA(w.root, team, files)
	if err != nil {
		t.Fatalf("the same CA over itself: %v", err)
	}
	if written {
		t.Error("a restore over the same CA says it wrote the CA")
	}
	if !sameFiles(certificateFiles(t, w), was) {
		t.Error("a restore over the same CA changed a file")
	}
	other, err := pki.Maker{Clock: wall0{w.clock}, Rand: &stream{seed: 9}}.NewCA("another")
	if err != nil {
		t.Fatal(err)
	}
	others := []BackupFile{
		{Name: "ca.crt", Mode: 0o644, Data: other.CertPEM()}, {Name: "ca.key", Mode: 0o600, Data: other.KeyPEM()},
	}
	refusal := func(what string, err error) {
		t.Helper()
		for _, s := range []string{"another CA", "ca.crt", "remove both CA files"} {
			if err == nil || !strings.Contains(err.Error(), s) {
				t.Errorf("%s of another CA over this one: %v, want a refusal saying %q", what, err, s)
			}
		}
	}
	refusal("the check", CheckRestoreCA(w.root, team, others))
	written, err = RestoreCA(w.root, team, others)
	refusal("the restore", err)
	if written {
		t.Error("a refused restore says it wrote the CA")
	}
	if !sameFiles(certificateFiles(t, w), was) {
		t.Error("a refused restore changed a file")
	}
}

// X13: a backup that does not hold exactly the CA it should is refused before any file is made.
func TestRestoreRefusesABadBackup(t *testing.T) {
	good := func(t *testing.T, w *world) []BackupFile {
		t.Helper()
		files, err := CABackupFiles(w.root, ops(t))
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	other, err := pki.Maker{Clock: wall0{newClock()}, Rand: &stream{seed: 9}}.NewCA("another")
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		edit func(files []BackupFile) []BackupFile
		text string
	}{
		"the key without the certificate": {func(f []BackupFile) []BackupFile { return f[1:] }, "ca.crt"},
		"the certificate without the key": {func(f []BackupFile) []BackupFile { return f[:1] }, "ca.key"},
		"a member that is not the CA's": {
			func(f []BackupFile) []BackupFile {
				return append(f, BackupFile{Name: "leaf.key", Mode: 0o600, Data: f[1].Data})
			}, "leaf.key",
		},
		"a member twice": {func(f []BackupFile) []BackupFile { return append(f, f[0]) }, "twice"},
		"a key with a looser mode": {
			func(f []BackupFile) []BackupFile { f[1].Mode = 0o644; return f }, "ca.key",
		},
		"a certificate with a setuid mode": {
			func(f []BackupFile) []BackupFile { f[0].Mode = 0o4644; return f }, "ca.crt",
		},
		"a key that is another CA's": {
			func(f []BackupFile) []BackupFile { f[1].Data = other.KeyPEM(); return f }, "is not the CA certificate's",
		},
		"a certificate of garbage": {func(f []BackupFile) []BackupFile { f[0].Data = []byte("junk"); return f }, "ca.crt"},
		"a certificate over the cap": {
			func(f []BackupFile) []BackupFile { f[0].Data = bytes.Repeat([]byte("x"), fileMax+1); return f }, "ca.crt",
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := applied(t)
			files := c.edit(good(t, w))
			if err := os.RemoveAll(w.at(paths.TLSDir(ops(t)))); err != nil {
				t.Fatal(err)
			}
			if err := CheckRestoreCA(w.root, ops(t), files); err == nil || !strings.Contains(err.Error(), c.text) {
				t.Fatalf("the check: got %v, want a refusal saying %q", err, c.text)
			}
			_, err := RestoreCA(w.root, ops(t), files)
			if err == nil || !strings.Contains(err.Error(), c.text) {
				t.Fatalf("got %v, want a refusal saying %q", err, c.text)
			}
			if w.exists(paths.TLSDir(ops(t))) {
				t.Error("a refused restore made root/<team>/tls/")
			}
		})
	}
}

// X13: no members is a backup taken before the backup carried the CA; the team's CA stays.
func TestRestoreOfABackupWithoutTheCA(t *testing.T) {
	w := applied(t)
	if err := CheckRestoreCA(w.root, ops(t), nil); err != nil {
		t.Fatal(err)
	}
	was := certificateFiles(t, w)
	written, err := RestoreCA(w.root, ops(t), nil)
	if err != nil || written {
		t.Fatalf("RestoreCA of no members = %v, %v; want nothing written and no error", written, err)
	}
	if !sameFiles(certificateFiles(t, w), was) {
		t.Error("a backup without a CA changed the team's CA")
	}
}

// X13: the issuing side reads the team's CA certificate for a bundle; it is the file's, checked.
func TestCAPEM(t *testing.T) {
	team := ops(t)
	w := newWorld(t)
	if _, err := CAPEM(w.root, team); err == nil || !strings.Contains(err.Error(), "not made yet") {
		t.Errorf("before any apply: %v, want a refusal saying the CA is not made yet", err)
	}
	w = applied(t)
	got, err := CAPEM(w.root, team)
	if err != nil {
		t.Fatal(err)
	}
	if got != w.read(paths.CACertFile(team)) {
		t.Error("CAPEM is not the CA certificate file")
	}
	if _, err := pki.Fingerprint([]byte(got)); err != nil {
		t.Error(err)
	}
	if strings.Contains(got, "PRIVATE") {
		t.Error("CAPEM carries a key")
	}
	w.write(paths.CAKeyFile(team), "junk")
	if _, err := CAPEM(w.root, team); err == nil || !strings.Contains(err.Error(), "ca.key") {
		t.Errorf("a CA whose key is bad: %v, want a refusal naming ca.key", err)
	}
}
