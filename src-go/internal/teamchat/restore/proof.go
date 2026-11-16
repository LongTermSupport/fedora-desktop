package restore

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// Clause is one clause of host checks H-C5 and H-C15 (DESIGN-v2-C-host.md, the H-C table), a
// stable id: the proof record's key, and what a refusal names.
type Clause string

// The clauses. H-C5 is one; H-C15 is the reach of the joined unit, the revocations of a member
// reissued and a human password reset after the backup, the floors, the restore epoch, the pool
// seat's token from the backup, and a held ccy session working again after hot reissue.
const (
	ClauseHC5         Clause = "H-C5"
	ClauseReach       Clause = "H-C15.reach"
	ClauseRevocations Clause = "H-C15.revocations"
	ClauseFloors      Clause = "H-C15.floors"
	ClauseEpoch       Clause = "H-C15.epoch"
	ClausePoolSeat    Clause = "H-C15.pool-seat"
	ClauseHeldSession Clause = "H-C15.held-session"
)

// Clauses is every clause, in the record's order. Restore needs each one passed.
var Clauses = []Clause{ClauseHC5, ClauseReach, ClauseRevocations, ClauseFloors, ClauseEpoch, ClausePoolSeat, ClauseHeldSession}

// Verdict is a clause's result in the record. A drill that fails a clause writes no record.
type Verdict string

// The verdicts: a clause the drill proved, or one it could not attempt (the pool seat while Gate
// B holds, the held session until Task 4.4).
const (
	VerdictPass    Verdict = "pass"
	VerdictSkipped Verdict = "skipped"
)

// Where is where the drill ran.
type Where string

// The places a drill runs.
const (
	WhereVMLab Where = "vm-lab"
	WhereHost  Where = "host"
)

// proofRecordVersion is the record's `v`.
const proofRecordVersion = 1

// maxProof caps restore-proof.json.
const maxProof = 4 << 10

// Key is what a proof is evidence about: the build (source digest), the Tuwunel version and the
// systemd version the drill ran on. The systemd version is recorded whole and matched on its
// major only, so a package update keeps the proof and a systemd upgrade lapses it.
type Key struct {
	Software paths.Digest
	Tuwunel  paths.ServerVersion
	Systemd  string
}

// Proof is the restore drill's record, root's restore-proof.json: its key, each clause's verdict
// and when it passed. It has no expiry, and it is never copied between machines: it is evidence
// about the machine that ran the drill.
type Proof struct {
	Key

	Checks   map[Clause]Verdict
	PassedMS int64
	Where    Where
}

// systemdVersion is a systemd version as `systemctl --version` names it: the major, then the
// package's own suffix.
var systemdVersion = regexp.MustCompile(`^[0-9]{1,6}(?:[.~+_-][0-9A-Za-z.~+_-]{0,57})?$`)

// SystemdMajor is v's major version, its leading digits; "" for no valid version.
func SystemdMajor(v string) string {
	if !systemdVersion.MatchString(v) {
		return ""
	}
	return v[:strings.IndexFunc(v+".", func(r rune) bool { return r < '0' || r > '9' })]
}

// ErrUnproven is a restore refused because no proof record enables it (DC8, n2g-D8, RW2). An
// *UnprovenError is it, and is ErrRefused, so the verb exits 70 before it takes the lock.
var ErrUnproven = errors.New("restore: unproven")

// UnprovenError is ErrUnproven with what is missing.
type UnprovenError struct{ Missing string }

func (e *UnprovenError) Error() string {
	return "restore is not enabled: " + e.Missing + " (DC8)"
}

// Is makes an UnprovenError both ErrUnproven and ErrRefused.
func (e *UnprovenError) Is(target error) bool { return target == ErrUnproven || target == ErrRefused }

func unproven(format string, args ...any) error {
	return &UnprovenError{Missing: fmt.Sprintf(format, args...)}
}

// EncodeProof is the record's canonical JSON; it refuses a record that lacks a clause or holds
// a verdict or a place that is not known.
func EncodeProof(p Proof) ([]byte, error) {
	if SystemdMajor(p.Systemd) == "" {
		return nil, fmt.Errorf("restore-proof.json: systemd version %q is not valid", p.Systemd)
	}
	if !slices.Contains([]Where{WhereVMLab, WhereHost}, p.Where) {
		return nil, fmt.Errorf("restore-proof.json: where %q is not known", p.Where)
	}
	if len(p.Checks) != len(Clauses) {
		return nil, errors.New("restore-proof.json: the checks are not exactly the clauses")
	}
	checks := make([]proto.Member, 0, len(Clauses))
	for _, c := range Clauses {
		v, ok := p.Checks[c]
		if !ok || (v != VerdictPass && v != VerdictSkipped) {
			return nil, fmt.Errorf("restore-proof.json: clause %s has no verdict", c)
		}
		checks = append(checks, proto.M(string(c), proto.Str(string(v))))
	}
	return proto.Obj(
		proto.M("v", proto.Int(proofRecordVersion)), proto.M("software", proto.Str(p.Software.String())),
		proto.M("tuwunel", proto.Str(p.Tuwunel.String())), proto.M("systemd", proto.Str(p.Systemd)), proto.M("checks", proto.Obj(checks...)),
		proto.M("passed_ms", proto.Int(p.PassedMS)), proto.M("where", proto.Str(string(p.Where))),
	).Compact(), nil
}

// DecodeProof reads the record strictly: every clause once, each pass or skipped.
func DecodeProof(raw []byte) (Proof, error) {
	v, err := parse(raw, "restore-proof.json", maxProof)
	if err != nil {
		return Proof{}, err
	}
	rd := &reader{file: "restore-proof.json"}
	m := rd.object(v, "file", []string{"v", "software", "tuwunel", "systemd", "checks", "passed_ms", "where"})
	rd.integer(m["v"], "v", proofRecordVersion, proofRecordVersion)
	var p Proof
	p.Software, err = paths.ParseDigest(rd.str(m["software"], "software", false, func(string) bool { return true }))
	if err != nil {
		rd.fail("software", "is not a source digest")
	}
	p.Tuwunel, err = paths.ParseServerVersion(rd.str(m["tuwunel"], "tuwunel", false, func(string) bool { return true }))
	if err != nil {
		rd.fail("tuwunel", "is not a server version")
	}
	p.Systemd = rd.str(m["systemd"], "systemd", false, func(s string) bool { return SystemdMajor(s) != "" })
	keys := make([]string, 0, len(Clauses))
	for _, c := range Clauses {
		keys = append(keys, string(c))
	}
	checks := rd.object(m["checks"], "checks", keys)
	p.Checks = map[Clause]Verdict{}
	for _, c := range Clauses {
		p.Checks[c] = Verdict(rd.str(checks[string(c)], "checks."+string(c), false, func(s string) bool {
			return s == string(VerdictPass) || s == string(VerdictSkipped)
		}))
	}
	p.PassedMS = rd.integer(m["passed_ms"], "passed_ms", 0, maxSafeMS)
	p.Where = Where(rd.str(m["where"], "where", false, func(s string) bool {
		return s == string(WhereVMLab) || s == string(WhereHost)
	}))
	return p, rd.err
}

// maxSafeMS is the largest time the record holds, JSON's safe integer.
const maxSafeMS = 1<<53 - 1

// WriteProof writes the record under root, owner only (state.WriteFile, the writer's own group).
func WriteProof(root string, p Proof) error {
	raw, err := EncodeProof(p)
	if err != nil {
		return err
	}
	return state.WriteFile(root+paths.RestoreProofFile, raw, 0o600, -1)
}

// ReadProof is the record under root, which uid must own (the trust rule); found is false when
// there is none.
func ReadProof(root string, uid uint32) (Proof, bool, error) {
	raw, found, err := state.ReadOwnedFile(root+paths.RestoreProofFile, maxProof, uid)
	if err != nil || !found {
		return Proof{}, found, err
	}
	p, err := DecodeProof(raw)
	return p, err == nil, err
}

// Proven is nil only when root holds a record, owned by uid, of this key (the build, the
// Tuwunel version, the systemd major) with every clause passed; otherwise an *UnprovenError
// naming what is missing.
func Proven(root string, uid uint32, now Key) error {
	p, found, err := ReadProof(root, uid)
	switch {
	case err != nil:
		return unproven("the restore proof record cannot be read: %v", err)
	case !found:
		return unproven("no restore drill has passed on this host (teamchat-admin restore-drill)")
	case p.Software != now.Software:
		return unproven("the restore proof is for another build (%s)", p.Software)
	case p.Tuwunel != now.Tuwunel:
		return unproven("the restore proof is for Tuwunel %s, not %s", p.Tuwunel, now.Tuwunel)
	case SystemdMajor(p.Systemd) != SystemdMajor(now.Systemd):
		return unproven("the restore proof is for systemd %s, not %s", p.Systemd, now.Systemd)
	}
	var open []string
	for _, c := range Clauses {
		if p.Checks[c] != VerdictPass {
			open = append(open, string(c))
		}
	}
	if len(open) > 0 {
		return unproven("the restore proof has not passed: %s", strings.Join(open, ", "))
	}
	return nil
}
