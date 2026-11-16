package restore

import (
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const (
	proofDigest  = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	otherDigest  = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	proofVersion = "v1.2.3"
	proofSystemd = "256.7-1.fc41"
)

func mustDigest(t *testing.T, s string) paths.Digest {
	t.Helper()
	d, err := paths.ParseDigest(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mustVersion(t *testing.T, s string) paths.ServerVersion {
	t.Helper()
	v, err := paths.ParseServerVersion(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// proofKey is this build, this Tuwunel and this systemd.
func proofKey(t *testing.T) Key {
	t.Helper()
	return Key{Software: mustDigest(t, proofDigest), Tuwunel: mustVersion(t, proofVersion), Systemd: proofSystemd}
}

// passedProof is a record of every clause passed on this build and Tuwunel.
func passedProof(t *testing.T) Proof {
	t.Helper()
	checks := map[Clause]Verdict{}
	for _, c := range Clauses {
		checks[c] = VerdictPass
	}
	return Proof{Key: proofKey(t), Checks: checks, PassedMS: 1700000000000, Where: WhereVMLab}
}

// The clause ids are stable: they are the record's keys, and a refusal names them.
func TestClausesAreHC5AndEachClauseOfHC15(t *testing.T) {
	got := make([]string, 0, len(Clauses))
	for _, c := range Clauses {
		got = append(got, string(c))
	}
	want := "H-C5 H-C15.reach H-C15.revocations H-C15.floors H-C15.epoch H-C15.pool-seat H-C15.held-session"
	if strings.Join(got, " ") != want {
		t.Fatalf("Clauses = %v", got)
	}
}

// The record is strict {"v":1,"software","tuwunel","checks":{clause: pass|skipped},"passed_ms","where"}.
func TestProofRoundTrips(t *testing.T) {
	p := passedProof(t)
	p.Checks[ClauseHeldSession] = VerdictSkipped
	raw, err := EncodeProof(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), `{"v":1,"software":"`+proofDigest+`","tuwunel":"v1.2.3","systemd":"256.7-1.fc41","checks":{"H-C5":"pass",`) ||
		!strings.Contains(string(raw), `"H-C15.held-session":"skipped"},"passed_ms":1700000000000,"where":"vm-lab"}`) {
		t.Fatalf("encoded %s", raw)
	}
	got, err := DecodeProof(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Key != p.Key || got.PassedMS != p.PassedMS || got.Where != p.Where || len(got.Checks) != len(Clauses) ||
		got.Checks[ClauseHeldSession] != VerdictSkipped || got.Checks[ClauseHC5] != VerdictPass {
		t.Fatalf("decoded %+v", got)
	}
}

func TestEncodeProofRefusesAnIncompleteRecord(t *testing.T) {
	p := passedProof(t)
	delete(p.Checks, ClauseFloors)
	if _, err := EncodeProof(p); err == nil {
		t.Error("encoded a record with a clause missing")
	}
	p = passedProof(t)
	p.Checks[ClauseFloors] = "maybe"
	if _, err := EncodeProof(p); err == nil {
		t.Error("encoded a verdict that is not pass or skipped")
	}
	p = passedProof(t)
	p.Systemd = "two hundred"
	if _, err := EncodeProof(p); err == nil {
		t.Error("encoded a systemd version that is not one")
	}
	p = passedProof(t)
	p.Where = "laptop"
	if _, err := EncodeProof(p); err == nil {
		t.Error("encoded an unknown where")
	}
}

func TestDecodeProofRefuses(t *testing.T) {
	raw, err := EncodeProof(passedProof(t))
	if err != nil {
		t.Fatal(err)
	}
	good := string(raw)
	for name, s := range map[string]string{
		"not json":        "{",
		"a version":       strings.Replace(good, `"v":1`, `"v":2`, 1),
		"a bad digest":    strings.Replace(good, proofDigest, "xyz", 1),
		"a bad verdict":   strings.Replace(good, `"H-C5":"pass"`, `"H-C5":"fail"`, 1),
		"an unknown key":  strings.Replace(good, `"H-C5":"pass"`, `"H-C99":"pass"`, 1),
		"a clause absent": strings.Replace(good, `"H-C5":"pass",`, ``, 1),
		"a bad where":     strings.Replace(good, `"vm-lab"`, `"elsewhere"`, 1),
		"a bad systemd":   strings.Replace(good, proofSystemd, "v256", 1),
		"a negative time": strings.Replace(good, `1700000000000`, `-1`, 1),
		"an extra":        strings.Replace(good, `"v":1,`, `"v":1,"x":0,`, 1),
		"too large":       good + strings.Repeat(" ", maxProof),
	} {
		if _, err := DecodeProof([]byte(s)); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
}

// selfUID is the test's own uid, which owns the files it writes, as root owns them on a host.
func selfUID(t *testing.T, dir string) uint32 {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("no owner in a stat")
	}
	return st.Uid
}

// proofRoot is a scratch root with the admin state directory made.
func proofRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(root+paths.AdminStateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

// WriteProof writes the record 0600 where ReadProof finds it.
func TestWriteProofThenReadProof(t *testing.T) {
	root := proofRoot(t)
	if err := WriteProof(root, passedProof(t)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root + paths.RestoreProofFile)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("stat %v, %v", info, err)
	}
	p, found, err := ReadProof(root, selfUID(t, root))
	if err != nil || !found || p.Software.String() != proofDigest {
		t.Fatalf("ReadProof = %+v, %v, %v", p, found, err)
	}
	if _, found, err := ReadProof(t.TempDir(), selfUID(t, root)); err != nil || found {
		t.Fatalf("an absent record: %v, %v", found, err)
	}
}

// Proven allows only a record of this build and this Tuwunel with every clause passed; each
// refusal is ErrUnproven and a RefusedError (exit 70) and names what is missing.
func TestProven(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(*Proof)
		want string
	}{
		"another build":    {func(p *Proof) { p.Software = mustDigest(t, otherDigest) }, "for another build (" + otherDigest + ")"},
		"another tuwunel":  {func(p *Proof) { p.Tuwunel = mustVersion(t, "v9.9.9") }, "for Tuwunel v9.9.9, not v1.2.3"},
		"another systemd":  {func(p *Proof) { p.Systemd = "255.4-1.fc40" }, "for systemd 255.4-1.fc40, not 256.7-1.fc41"},
		"a skipped clause": {func(p *Proof) { p.Checks[ClausePoolSeat] = VerdictSkipped }, "not passed: H-C15.pool-seat"},
		"two skipped": {func(p *Proof) {
			p.Checks[ClausePoolSeat], p.Checks[ClauseHeldSession] = VerdictSkipped, VerdictSkipped
		}, "not passed: H-C15.pool-seat, H-C15.held-session"},
	} {
		root := proofRoot(t)
		p := passedProof(t)
		tc.edit(&p)
		if err := WriteProof(root, p); err != nil {
			t.Fatal(err)
		}
		err := Proven(root, selfUID(t, root), proofKey(t))
		if !errors.Is(err, ErrUnproven) || !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Proven = %v, want ErrUnproven naming %q", name, err, tc.want)
		}
	}
	root := proofRoot(t)
	if err := WriteProof(root, passedProof(t)); err != nil {
		t.Fatal(err)
	}
	if err := Proven(root, selfUID(t, root), proofKey(t)); err != nil {
		t.Fatalf("a matching record: %v", err)
	}
	// Only the systemd major is matched: a package update keeps the proof.
	now := proofKey(t)
	now.Systemd = "256.9-2.fc41"
	if err := Proven(root, selfUID(t, root), now); err != nil {
		t.Fatalf("another 256 package: %v", err)
	}
}

func TestProvenRefusesNoRecordACorruptOneAndAnotherOwners(t *testing.T) {
	k := proofKey(t)
	root := proofRoot(t)
	err := Proven(root, selfUID(t, root), k)
	if !errors.Is(err, ErrUnproven) || !strings.Contains(err.Error(), "no restore drill has passed on this host") {
		t.Errorf("no record: %v", err)
	}
	if err := os.WriteFile(root+paths.RestoreProofFile, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Proven(root, selfUID(t, root), k); !errors.Is(err, ErrUnproven) || !strings.Contains(err.Error(), "cannot be read") {
		t.Errorf("a corrupt record: %v", err)
	}
	if err := WriteProof(root, passedProof(t)); err != nil {
		t.Fatal(err)
	}
	if err := Proven(root, selfUID(t, root)+1, k); !errors.Is(err, ErrUnproven) || !strings.Contains(err.Error(), "cannot be read") {
		t.Errorf("a record of another owner: %v", err)
	}
}

// With today's gates the pool-seat and held-session clauses cannot pass (Gate B, Task 4.4): a
// drill's record leaves restore refused, and the refusal says so.
func TestADrillRecordOfTodayLeavesRestoreRefused(t *testing.T) {
	root := proofRoot(t)
	p := passedProof(t)
	p.Checks[ClausePoolSeat], p.Checks[ClauseHeldSession] = VerdictSkipped, VerdictSkipped
	if err := WriteProof(root, p); err != nil {
		t.Fatal(err)
	}
	err := Proven(root, selfUID(t, root), proofKey(t))
	want := "restore is not enabled: the restore proof has not passed: H-C15.pool-seat, H-C15.held-session (DC8)"
	if err == nil || err.Error() != want {
		t.Fatalf("Proven = %v, want %q", err, want)
	}
}

func TestSystemdMajor(t *testing.T) {
	for v, want := range map[string]string{"256.7-1.fc41": "256", "257": "257", "258~rc1": "258", "": "", "v256": "", "256 (x)": ""} {
		if got := SystemdMajor(v); got != want {
			t.Errorf("SystemdMajor(%q) = %q, want %q", v, got, want)
		}
	}
}
