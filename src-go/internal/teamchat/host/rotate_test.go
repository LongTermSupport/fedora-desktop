package host

import (
	"bytes"
	"errors"
	"io/fs"
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/tlsterm"
)

// The tests are Plan 00246 SPEC-cross-network X33 at the host: a new team CA and a new leaf,
// made and checked before any file is replaced, written where apply writes them, and the
// terminator restarted so that it serves the new leaf.

// faulty is a FileWriter that writes through state.WriteFile and fails its n-th call (0: none),
// the bytes written first when landed, so a write that landed but reported failure is a case.
type faulty struct {
	n, calls int
	landed   bool
	paths    []string
}

func (f *faulty) write(path string, data []byte, mode fs.FileMode, gid int) error {
	f.calls++
	f.paths = append(f.paths, path)
	if f.calls != f.n {
		return state.WriteFile(path, data, mode, gid)
	}
	if f.landed {
		if err := state.WriteFile(path, data, mode, gid); err != nil {
			return err
		}
	}
	return errFault
}

// rotator is the world's CARotation over the writer w gives.
func rotator(w *world, write FileWriter) CARotation {
	return CARotation{Root: w.root, Clock: w.clock, Rand: w.rand, Units: w.sd, Write: write}
}

// appliedNames are the names the world's apply made its leaf for.
func appliedNames(t *testing.T, w *world) pki.Names {
	t.Helper()
	names, err := AppliedLeafNames(w.root)(ops(t))
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// extraFile is a file beside the team's state that a rotation also replaces (the LXC record, in
// the verb), with its old and new bytes.
func extraFile(t *testing.T, w *world) Replacement {
	t.Helper()
	path := w.root + paths.TLSDir(ops(t)) + "/../extra.json"
	if err := os.WriteFile(path, []byte(`{"old":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return Replacement{Path: path, Old: []byte(`{"old":true}`), New: []byte(`{"new":true}`), Mode: 0o600, GID: -1}
}

func fingerprintOf(t *testing.T, caPEM []byte) string {
	t.Helper()
	fp, err := pki.Fingerprint(caPEM)
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

// X33: the new CA's fingerprint differs from the old; the new leaf verifies under the new CA and
// not under the old; the files are where apply writes them with apply's modes; the CA a bundle
// is now issued with is the new one; the active terminator is restarted once.
func TestX33ARotationMakesANewCAAndALeafOnlyItSigns(t *testing.T) {
	w := applied(t)
	team := ops(t)
	old := theCA(t, w)
	w.sd.unit(paths.TLSUnit(team)).Active = "active"
	w.log, w.sd.started = nil, nil
	got, err := rotator(w, state.WriteFile).Rotate(t.Context(), team, appliedNames(t, w), nil)
	if err != nil {
		t.Fatal(err)
	}
	now := theCA(t, w)
	if got.Old != fingerprintOf(t, old.CertPEM()) || got.New != fingerprintOf(t, now.CertPEM()) || got.Old == got.New {
		t.Fatalf("fingerprints %+v; old %s, new %s", got, fingerprintOf(t, old.CertPEM()), fingerprintOf(t, now.CertPEM()))
	}
	cert, key := bytesOf(t, w, paths.LeafCertFile(team)), bytesOf(t, w, paths.LeafKeyFile(team))
	leaf, err := pki.ParseLeaf(now.CertPEM(), cert, key)
	if err != nil {
		t.Fatalf("the new leaf under the new CA: %v", err)
	}
	if _, err = pki.ParseLeaf(old.CertPEM(), cert, key); err == nil {
		t.Fatal("the new leaf verifies under the old CA")
	}
	if !samePKINames(leaf.Names(), appliedNames(t, w)) {
		t.Fatalf("the leaf's names %+v", leaf.Names())
	}
	tlsModes(t, w)
	issued, err := CAPEM(w.root, team)
	if err != nil || issued != string(now.CertPEM()) {
		t.Fatalf("the CA a bundle is issued with: %v", err)
	}
	if !slices.Equal(w.sd.started, []string{paths.TLSUnit(team)}) || !got.Restarted {
		t.Fatalf("restarted %v (%+v)", w.sd.started, got)
	}
	if _, err = rotator(w, state.WriteFile).Rotate(t.Context(), team, appliedNames(t, w), nil); err != nil {
		t.Fatalf("a second rotation: %v", err)
	}
	if bytes.Equal(theCA(t, w).CertPEM(), now.CertPEM()) {
		t.Fatal("a second rotation kept the CA")
	}
}

// tlsModes holds the team's four TLS files to the modes apply gives them.
func tlsModes(t *testing.T, w *world) {
	t.Helper()
	team := ops(t)
	for p, m := range map[string]os.FileMode{
		paths.CAKeyFile(team): 0o600, paths.CACertFile(team): 0o644, paths.LeafKeyFile(team): 0o600, paths.LeafCertFile(team): 0o644,
	} {
		if got := mode(t, w.at(p)); got != m {
			t.Errorf("%s: mode %v, want %v", p, got, m)
		}
	}
}

func samePKINames(a, b pki.Names) bool { return a.DNS == b.DNS && slices.Equal(a.IPs, b.IPs) }

// X33: the CA key is written before the CA certificate and the leaf key before the leaf
// certificate, as apply writes them, then what the caller also replaces.
func TestX33TheFilesAreWrittenKeyFirstThenTheCallersOwn(t *testing.T) {
	w := applied(t)
	team := ops(t)
	f := &faulty{}
	extra := extraFile(t, w)
	if _, err := rotator(w, f.write).Rotate(t.Context(), team, appliedNames(t, w), []Replacement{extra}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		w.at(paths.CAKeyFile(team)), w.at(paths.CACertFile(team)), w.at(paths.LeafKeyFile(team)), w.at(paths.LeafCertFile(team)), extra.Path,
	}
	if !slices.Equal(f.paths, want) {
		t.Fatalf("written %v\nwant %v", f.paths, want)
	}
	if got, err := os.ReadFile(extra.Path); err != nil || string(got) != `{"new":true}` {
		t.Fatalf("the extra file: %q, %v", got, err)
	}
}

// X33: a failure at each write, whether or not its bytes landed, leaves the old CA, the old
// leaf and the caller's file whole, restarts nothing and says nothing was replaced.
func TestX33AFailureAtEveryWriteLeavesEverythingAsItWas(t *testing.T) {
	const writes = 5
	for _, landed := range []bool{false, true} {
		for n := 1; n <= writes; n++ {
			w := applied(t)
			team := ops(t)
			w.sd.unit(paths.TLSUnit(team)).Active = "active"
			extra := extraFile(t, w)
			was := certificateFiles(t, w)
			w.log, w.sd.started = nil, nil
			f := &faulty{n: n, landed: landed}
			_, err := rotator(w, f.write).Rotate(t.Context(), team, appliedNames(t, w), []Replacement{extra})
			if !errors.Is(err, errFault) || !strings.Contains(err.Error(), "nothing is replaced") {
				t.Fatalf("write %d (landed %v): %v", n, landed, err)
			}
			if !sameFiles(was, certificateFiles(t, w)) {
				t.Fatalf("write %d (landed %v): a CA or leaf file changed", n, landed)
			}
			if got, rerr := os.ReadFile(extra.Path); rerr != nil || string(got) != `{"old":true}` {
				t.Fatalf("write %d (landed %v): the extra file %q, %v", n, landed, got, rerr)
			}
			if len(w.sd.started) != 0 {
				t.Fatalf("write %d: restarted %v", n, w.sd.started)
			}
			if f.calls != 2*n {
				t.Fatalf("write %d: %d writes, want the failed one and one back for it and each before it", n, f.calls)
			}
		}
	}
}

// X33: a failure while the new material is made (the randomness) or checked (names a leaf
// cannot hold) writes nothing.
func TestX33AFailureWhileMakingWritesNothing(t *testing.T) {
	for name, c := range map[string]struct {
		rand  bool
		names pki.Names
	}{
		"randomness": {rand: true},
		"names":      {names: pki.Names{}},
	} {
		w := applied(t)
		team := ops(t)
		was := certificateFiles(t, w)
		names := appliedNames(t, w)
		r := rotator(w, (&faulty{}).write)
		if c.rand {
			r.Rand = bytes.NewReader(nil)
		} else {
			names = c.names
		}
		w.sd.started = nil
		if _, err := r.Rotate(t.Context(), team, names, nil); err == nil || !strings.Contains(err.Error(), "nothing is replaced") {
			t.Fatalf("%s: %v", name, err)
		}
		if !sameFiles(was, certificateFiles(t, w)) || len(w.sd.started) != 0 {
			t.Fatalf("%s: a file changed or a unit restarted", name)
		}
	}
}

// X33: a team with no CA, or a CA apply would refuse, is not rotated: the rotation is of a CA
// that is there, and a team never applied has none.
func TestX33ATeamWithNoUsableCAIsRefused(t *testing.T) {
	w := applied(t)
	team := ops(t)
	if err := os.Remove(w.at(paths.CAKeyFile(team))); err != nil {
		t.Fatal(err)
	}
	was := certificateFiles2(t, w)
	if _, err := rotator(w, state.WriteFile).Rotate(t.Context(), team, appliedNames(t, w), nil); err == nil || !strings.Contains(err.Error(), "nothing is replaced") {
		t.Fatalf("a lone CA certificate: %v", err)
	}
	if !sameFiles(was, certificateFiles2(t, w)) {
		t.Fatal("a file changed")
	}
	if _, err := rotator(w, state.WriteFile).Rotate(t.Context(), mustTeam(t, "other"), appliedNames(t, w), nil); err == nil {
		t.Fatal("a team with no CA was rotated")
	}
}

// certificateFiles2 is certificateFiles without the CA key.
func certificateFiles2(t *testing.T, w *world) map[string][]byte {
	t.Helper()
	team := ops(t)
	out := map[string][]byte{}
	for _, p := range []string{paths.CACertFile(team), paths.LeafCertFile(team), paths.LeafKeyFile(team)} {
		out[p] = bytesOf(t, w, p)
	}
	return out
}

// X33: an inactive terminator is not started (it reads the new leaf at its start); a restart
// that fails comes after every file is replaced, so the rotation stands and says so.
func TestX33TheTerminatorIsRestartedOnlyWhenActiveAndAFailedRestartLeavesTheRotation(t *testing.T) {
	w := applied(t)
	team := ops(t)
	w.sd.unit(paths.TLSUnit(team)).Active = "inactive"
	w.sd.started = nil
	got, err := rotator(w, state.WriteFile).Rotate(t.Context(), team, appliedNames(t, w), nil)
	if err != nil || got.Restarted || len(w.sd.started) != 0 {
		t.Fatalf("inactive: %+v, %v, restarted %v", got, err, w.sd.started)
	}
	w.sd.unit(paths.TLSUnit(team)).Active = "active"
	w.faults = map[string]error{"restart": errFault}
	old := theCA(t, w)
	got, err = rotator(w, state.WriteFile).Rotate(t.Context(), team, appliedNames(t, w), nil)
	if !errors.Is(err, errFault) || !strings.Contains(err.Error(), "the CA is rotated") || got.New == "" {
		t.Fatalf("a failed restart: %+v, %v", got, err)
	}
	if bytes.Equal(theCA(t, w).CertPEM(), old.CertPEM()) || got.New != fingerprintOf(t, theCA(t, w).CertPEM()) {
		t.Fatal("a failed restart undid the rotation")
	}
}

// X33, X16: a member holding a bundle with the old CA refuses the terminator serving the new
// leaf, with X16's message naming the pinned (old) CA's fingerprint and the host; one holding
// the new CA gets past TLS.
func TestX33AClientHoldingTheOldCARefusesTheNewLeaf(t *testing.T) {
	w := applied(t)
	team := ops(t)
	old := theCA(t, w)
	loopback := pki.Names{IPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}
	if _, err := rotator(w, state.WriteFile).Rotate(t.Context(), team, loopback, nil); err != nil {
		t.Fatal(err)
	}
	now := theCA(t, w)
	leaf, err := pki.ParseLeaf(now.CertPEM(), bytesOf(t, w, paths.LeafCertFile(team)), bytesOf(t, w, paths.LeafKeyFile(team)))
	if err != nil {
		t.Fatal(err)
	}
	srv, err := tlsterm.Start(t.Context(), tlsterm.Config{
		Listen: []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:0")}, Upstream: echoServer(t),
		CertPEM: leaf.CertPEM(), KeyPEM: leaf.KeyPEM(), Clock: clock.NewFake(0, t0.UnixMilli()),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cerr := srv.Close(); cerr != nil {
			t.Error(cerr)
		}
	})
	base := "https://" + srv.Addrs()[0].String()
	ask := func(caPEM []byte) error {
		c, cerr := matrix.NewMember(matrix.Config{BaseURL: base, CAPEM: string(caPEM), Token: "unused", Wall: wall0{w.clock}})
		if cerr != nil {
			t.Fatal(cerr)
		}
		defer c.Close()
		_, cerr = c.Versions(t.Context())
		return cerr
	}
	err = ask(old.CertPEM())
	want := "TLS verification of " + srv.Addrs()[0].String() + " failed against the pinned CA sha256:" + fingerprintOf(t, old.CertPEM()) + ": unknown authority"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("the old bundle: %v\nwant %q", err, want)
	}
	if err = ask(now.CertPEM()); err != nil && strings.Contains(err.Error(), "TLS verification") {
		t.Fatalf("the new bundle: %v", err)
	}
}
