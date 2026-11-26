package drill

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
)

// The scratch team the fake random source gives.
const rigTeam = "rd-0123456789ab"

// rig is a fake machine the drill runs on: every operation recorded in calls, the team's live
// epoch and floors, and the faults that make one check fail.
type rig struct {
	t      *testing.T
	root   string
	uid    uint32
	calls  []string
	faults map[string]bool
	fail   string // an operation that answers an error

	epoch    string
	floors   map[string]string
	staged   bool
	restored bool
}

func newRig(t *testing.T, faults ...string) *rig {
	t.Helper()
	root, uid := host(t)
	r := &rig{
		t: t, root: root, uid: uid, faults: map[string]bool{}, epoch: "epoch-live",
		floors: map[string]string{"@a1:x": "$e1", "@a2:x": "$e2"},
	}
	for _, f := range faults {
		r.faults[f] = true
	}
	return r
}

var (
	kept     = Credential{Handle: "@a1:x", Secret: "kept-token"}
	reissued = Credential{Handle: "@a2:x", Secret: "old-token"}
	human    = Credential{Handle: "@h:x", Secret: "old-password"}
)

func (r *rig) Lock(context.Context) (func() error, error) {
	return func() error { return r.op("unlock") }, r.op("lock")
}

func (r *rig) Apply(_ context.Context, team paths.TeamID, port int) error {
	if team.String() != rigTeam || port != 8470 {
		r.t.Errorf("Apply(%s, %d)", team, port)
	}
	if !markedBefore(r) {
		r.t.Error("the team was applied before its marker was written")
	}
	return r.op("apply")
}

func markedBefore(r *rig) bool {
	_, found, err := ReadMarker(r.root, r.uid, team(r.t, rigTeam))
	return err == nil && found
}

func (r *rig) Seed(context.Context, paths.TeamID) (Seed, error) {
	return Seed{Events: []string{"$m1", "$m2"}, Kept: kept, Reissued: reissued, Human: human}, r.op("seed")
}

func (r *rig) Backup(context.Context, paths.TeamID) (int64, error) { return 7, r.op("backup") }

func (r *rig) Floors(context.Context, paths.TeamID) (map[string]string, error) {
	out := maps.Clone(r.floors)
	if r.restored && r.faults["floors"] {
		out["@a2:x"] = "$moved"
	}
	return out, r.op("floors")
}

func (r *rig) Change(_ context.Context, _ paths.TeamID, s Seed) error {
	if s.Reissued != reissued || s.Human != human {
		r.t.Errorf("Change(%+v)", s)
	}
	return r.op("change")
}

func (r *rig) Owners(context.Context, paths.TeamID) (map[string]uint32, error) {
	out := map[string]uint32{"db": 900, "config.toml": 0}
	if r.staged && r.faults["owners"] {
		out["db"] = 901
	}
	return out, r.op("owners")
}

func (r *rig) LiveEpoch(context.Context, paths.TeamID) (string, error) {
	e := r.epoch
	if r.staged && !r.restored && r.faults["live-epoch"] {
		e = "epoch-merged"
	}
	return e, r.op("live-epoch")
}

func (r *rig) MemberReach(context.Context, paths.TeamID) (bool, error) {
	return r.faults["member-reach"], r.op("member-reach")
}

func (r *rig) JoinedEpoch(context.Context, paths.TeamID) (string, error) {
	if r.faults["joined"] {
		return r.epoch, r.op("joined-epoch")
	}
	return "epoch-merged", r.op("joined-epoch")
}

// Restore runs the hooks where the engine does: OnStaged with the copy staged, AfterSwap after
// the swap; AfterSwap's error is reverted, the copy kept staged.
func (r *rig) Restore(ctx context.Context, _ paths.TeamID, n int64, h Hooks) error {
	if n != 7 {
		r.t.Errorf("Restore of backup %d", n)
	}
	if err := r.op("restore"); err != nil {
		return err
	}
	r.staged = true
	if h.OnStaged != nil {
		if err := h.OnStaged(ctx); err != nil {
			return fmt.Errorf("restore not done, staged: %w", err)
		}
	}
	if h.AfterSwap != nil && !r.faults["no-force"] {
		if err := h.AfterSwap(ctx); err != nil {
			r.staged = !r.faults["no-revert"]
			return fmt.Errorf("restore not done, reverted: %w", err)
		}
	}
	r.staged, r.restored = false, true
	if !r.faults["epoch"] {
		r.epoch = "epoch-restored"
	}
	return nil
}

func (r *rig) Staged(context.Context, paths.TeamID) (bool, error) { return r.staged, r.op("staged") }

func (r *rig) Discard(context.Context, paths.TeamID) error {
	r.staged = false
	return r.op("discard")
}

func (r *rig) MissingEvents(_ context.Context, _ paths.TeamID, events []string) ([]string, error) {
	if !slices.Equal(events, []string{"$m1", "$m2"}) {
		r.t.Errorf("MissingEvents(%q)", events)
	}
	if r.faults["history"] {
		return []string{"$m2"}, r.op("events")
	}
	return nil, r.op("events")
}

func (r *rig) Accepts(_ context.Context, _ paths.TeamID, c Credential) (bool, error) {
	switch c {
	case kept:
		return !r.faults["kept"], r.op("accepts kept")
	case reissued:
		return r.faults["revoked-member"], r.op("accepts reissued")
	case human:
		return r.faults["revoked-human"], r.op("accepts human")
	}
	r.t.Errorf("Accepts(%+v)", c)
	return false, nil
}

func (r *rig) Cleaner() Cleaner {
	return Cleaner{
		Recorded: func(paths.TeamID) (bool, error) { return r.staged, nil },
		Discard:  func(ctx context.Context, id paths.TeamID) error { return r.Discard(ctx, id) },
		Remove: func(_ context.Context, id paths.TeamID) error {
			if err := r.op("remove"); err != nil {
				return err
			}
			return os.RemoveAll(r.root + paths.AdminState(id))
		},
	}
}

func (r *rig) op(name string) error {
	r.calls = append(r.calls, name)
	if r.fail == name {
		return fmt.Errorf("injected %s", name)
	}
	return nil
}

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

var drillTime = time.UnixMilli(1700000123456)

func (r *rig) config() Config {
	return Config{
		Root: r.root, UID: r.uid, Port: 8470, Where: restore.WhereVMLab,
		Rand:  bytes.NewReader([]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab}),
		Clock: fixedClock{drillTime},
		Key:   restore.Key{Software: marker(r.t).Digest, Tuwunel: version(r.t), Systemd: "258.2-1.fc44"},
	}
}

func version(t *testing.T) paths.ServerVersion {
	t.Helper()
	v, err := paths.ParseServerVersion("v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func (r *rig) run() (Outcome, error) { return Run(context.Background(), r.config(), r) }

// happyCalls is the drill's order: the lock, the team applied and seeded, backup N with its
// floors, epoch and owners, the changes after N, the restore forced to fail with its probes at
// staged, the revert judged and discarded, the restore that finishes, its checks, the clean-up.
var happyCalls = []string{
	"lock", "apply", "seed", "backup", "floors", "live-epoch", "owners", "change",
	"restore", "owners", "live-epoch", "member-reach", "joined-epoch",
	"staged", "live-epoch", "discard",
	"restore", "events", "accepts kept", "accepts reissued", "accepts human", "floors", "live-epoch",
	"remove", "unlock",
}

func noProof(t *testing.T, r *rig) {
	t.Helper()
	if _, found, err := restore.ReadProof(r.root, r.uid); err != nil || found {
		t.Errorf("a proof record is on disk (found %v, %v)", found, err)
	}
}

func cleaned(t *testing.T, r *rig) {
	t.Helper()
	if !slices.Contains(r.calls, "remove") {
		t.Errorf("the scratch team was not removed: %q", r.calls)
	}
	if err := CheckStart(r.root, r.uid); err != nil {
		t.Errorf("the drill left something behind: %v", err)
	}
}

// A drill with every check passing writes the proof: every clause it attempted passed, the pool
// seat and the held session skipped, keyed to this build, Tuwunel and systemd; restore stays
// refused, naming the two skipped clauses.
func TestRunWritesTheProofWhenEveryCheckPasses(t *testing.T) {
	r := newRig(t)
	out, err := r.run()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.calls, happyCalls) {
		t.Errorf("calls =\n%q\nwant\n%q", r.calls, happyCalls)
	}
	if out.Team.String() != rigTeam || !out.Written || len(out.Failed) != 0 {
		t.Errorf("outcome = %+v", out)
	}
	p, found, err := restore.ReadProof(r.root, r.uid)
	if err != nil || !found {
		t.Fatalf("ReadProof = %v, %v", found, err)
	}
	if p.Key != r.config().Key || p.PassedMS != drillTime.UnixMilli() || p.Where != restore.WhereVMLab {
		t.Errorf("proof = %+v", p)
	}
	verdicts(t, p.Checks, out.Checks)
	err = restore.Proven(r.root, r.uid, r.config().Key)
	if err == nil || !strings.Contains(err.Error(), "H-C15.pool-seat, H-C15.held-session") {
		t.Errorf("Proven = %v", err)
	}
	cleaned(t, r)
}

// verdicts holds that the record and the outcome pass every clause but the pool seat and the
// held session, which are skipped.
func verdicts(t *testing.T, record, outcome map[restore.Clause]restore.Verdict) {
	t.Helper()
	for _, c := range restore.Clauses {
		want := restore.VerdictPass
		if c == restore.ClausePoolSeat || c == restore.ClauseHeldSession {
			want = restore.VerdictSkipped
		}
		if record[c] != want || outcome[c] != want {
			t.Errorf("clause %s: record %q, outcome %q, want %q", c, record[c], outcome[c], want)
		}
	}
}

// Each check, faked to fail, fails its clause: the drill still cleans up, answers ErrFailed
// naming the clause, and writes no proof record.
func TestEachFailedCheckWritesNoProof(t *testing.T) {
	cases := map[string]struct {
		clause restore.Clause
		says   string
	}{
		"owners":         {restore.ClauseHC5, "changed owner"},
		"live-epoch":     {restore.ClauseHC5, "the live server answered epoch-merged"},
		"member-reach":   {restore.ClauseHC5, "a member address reached"},
		"no-force":       {restore.ClauseHC5, "the forced failure"},
		"no-revert":      {restore.ClauseHC5, "not kept staged"},
		"history":        {restore.ClauseHC5, "$m2"},
		"kept":           {restore.ClauseHC5, "@a1:x"},
		"joined":         {restore.ClauseReach, "the joined unit"},
		"revoked-member": {restore.ClauseRevocations, "@a2:x"},
		"revoked-human":  {restore.ClauseRevocations, "@h:x"},
		"floors":         {restore.ClauseFloors, "@a2:x"},
		"epoch":          {restore.ClauseEpoch, "restore_epoch"},
	}
	for fault, c := range cases {
		t.Run(fault, func(t *testing.T) {
			r := newRig(t, fault)
			out, err := r.run()
			if !errors.Is(err, ErrFailed) || !strings.Contains(err.Error(), string(c.clause)) {
				t.Fatalf("err = %v", err)
			}
			if !strings.Contains(strings.Join(out.Failed[c.clause], "; "), c.says) {
				t.Errorf("failed[%s] = %q, want %q", c.clause, out.Failed[c.clause], c.says)
			}
			if len(out.Failed) != 1 || out.Written {
				t.Errorf("outcome = %+v", out)
			}
			if _, passed := out.Checks[c.clause]; passed {
				t.Errorf("clause %s is both failed and %s", c.clause, out.Checks[c.clause])
			}
			noProof(t, r)
			cleaned(t, r)
		})
	}
}

// An operation that fails ends the drill there: no further step, the team cleaned up, the
// error the operation's, and no proof.
func TestAFailedOperationStopsTheDrill(t *testing.T) {
	for _, op := range []string{"apply", "seed", "backup", "change", "restore", "discard", "events", "owners"} {
		t.Run(op, func(t *testing.T) {
			r := newRig(t)
			r.fail = op
			_, err := r.run()
			if err == nil || !strings.Contains(err.Error(), "injected "+op) || errors.Is(err, ErrFailed) {
				t.Fatalf("err = %v", err)
			}
			at := slices.Index(r.calls, op)
			for _, c := range r.calls[at+1:] {
				if c != "remove" && c != "unlock" {
					t.Errorf("the drill went on after %s: %q", op, r.calls)
					break
				}
			}
			noProof(t, r)
			cleaned(t, r)
		})
	}
}

// The drill refuses to start beside another team (Q1) or a leftover, before it applies anything.
func TestRunRefusesBesideAnotherTeam(t *testing.T) {
	r := newRig(t)
	applied(t, r.root, team(t, "ops"))
	_, err := r.run()
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v", err)
	}
	if !slices.Equal(r.calls, []string{"lock", "unlock"}) {
		t.Errorf("calls = %q", r.calls)
	}
	noProof(t, r)
}

// A lock that cannot be taken is the drill's error; nothing else runs.
func TestRunNeedsTheLock(t *testing.T) {
	r := newRig(t)
	r.fail = "lock"
	if _, err := r.run(); err == nil || !strings.Contains(err.Error(), "injected lock") {
		t.Fatalf("err = %v", err)
	}
	if !slices.Equal(r.calls, []string{"lock"}) {
		t.Errorf("calls = %q", r.calls)
	}
}

// A clean-up that fails is the drill's error, and no proof is written: the leftover stays
// marked for --clean.
func TestAFailedCleanUpWritesNoProof(t *testing.T) {
	r := newRig(t)
	r.fail = "remove"
	_, err := r.run()
	if err == nil || !strings.Contains(err.Error(), "injected remove") || !strings.Contains(err.Error(), "--clean") {
		t.Fatalf("err = %v", err)
	}
	noProof(t, r)
	if !errors.Is(CheckStart(r.root, r.uid), ErrRefused) {
		t.Error("the leftover is not marked for --clean")
	}
}
