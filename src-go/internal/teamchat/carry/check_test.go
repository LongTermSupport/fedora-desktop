package carry

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// checkWorld is a guest an import of kind landed on, with the loopback start and carry.check
// faked: the start makes the homeserver active, and carry.check finds every tail event but the
// rooms in missing, and answers epoch as the roster event's.
type checkWorld struct {
	*runWorld

	missing []string
	epoch   restore.Epoch
	// loopbackErr fails the loopback start after it made the unit active; checkErr fails
	// carry.check.
	loopbackErr, checkErr error
	asked                 []CheckArgs
}

func newCheckWorld(t *testing.T, kind Kind) *checkWorld {
	t.Helper()
	w := &checkWorld{runWorld: newRunWorld(t, kind), epoch: restore.Epoch{RestoreEpoch: 2, Rev: 5}}
	if _, err := w.fresh(); err != nil {
		t.Fatal(err)
	}
	w.reloads = true
	return w
}

func (w *checkWorld) checkDeps() Deps {
	d := w.runDeps()
	d.Child = checkChild{w}
	d.Loopback = func(_ context.Context, team paths.TeamID) error {
		w.effect("loopback")
		if team != w.team {
			w.t.Errorf("the loopback start of team %s", team)
		}
		w.put(paths.ServerDropInFile(team), "[Service]\n")
		w.unit(paths.ServerUnit(team)).active = "active"
		return w.loopbackErr
	}
	return d
}

func (w *checkWorld) check() ([]byte, error) {
	return Check(context.Background(), w.checkDeps(), w.team)
}

type checkChild struct{ w *checkWorld }

func (c checkChild) Tail(context.Context, paths.TeamID, []byte) (Tail, error) {
	c.w.t.Error("the check read a tail")
	return Tail{}, errors.New("no tail")
}

func (c checkChild) Place(context.Context, paths.TeamID, PlaceArgs) ([]string, error) {
	c.w.t.Error("the check placed files")
	return nil, errors.New("no place")
}

func (c checkChild) Check(_ context.Context, team paths.TeamID, a CheckArgs) (Presence, error) {
	c.w.effect("child check")
	if team != c.w.team {
		c.w.t.Errorf("carry.check of team %s", team)
	}
	c.w.asked = append(c.w.asked, a)
	if c.w.unit(paths.ServerUnit(team)).active != "active" {
		c.w.t.Error("carry.check asked of a homeserver not started")
	}
	p := Presence{Epoch: c.w.epoch}
	for _, e := range a.Tail {
		p.Found = append(p.Found, !slices.Contains(c.w.missing, e.Room))
	}
	return p, c.w.checkErr
}

// checkCalls are a whole check's calls: the start, carry.check, then the stop of the
// homeserver (the terminator is already stopped), RS18's reads of both units, and the reload
// after the recorded drop-in is removed (sc-D14).
func checkCalls(team paths.TeamID) []string {
	tls, server := paths.TLSUnit(team), paths.ServerUnit(team)
	return []string{
		"loopback", "child check", "systemd state " + tls, "systemd state " + server, "stop " + server,
		"systemd state " + tls, "systemd state " + server, "daemon-reload",
	}
}

// checkAnswer is section 6's answer.
type checkAnswer struct {
	team, kind string
	rooms      []string
	found      []bool
	rev, epoch int64
	ok         bool
}

// fields is v's members by key, which must be exactly keys, in that order.
func fields(t *testing.T, v proto.Value, keys ...string) map[string]proto.Value {
	t.Helper()
	got := map[string]proto.Value{}
	order := make([]string, 0, len(v.Members()))
	for _, m := range v.Members() {
		got[m.Key] = m.Val
		order = append(order, m.Key)
	}
	if !slices.Equal(order, keys) {
		t.Fatalf("the keys are %v, not %v", order, keys)
	}
	return got
}

func checkAnswerOf(t *testing.T, raw []byte) checkAnswer {
	t.Helper()
	v, err := proto.Parse(raw)
	if err != nil {
		t.Fatalf("the answer %s: %v", raw, err)
	}
	f := fields(t, v, "v", keyTeam, keyKind, "rooms", "roster_event", "ok")
	ev := fields(t, f["roster_event"], "rev", "restore_epoch")
	a := checkAnswer{team: f[keyTeam].Str(), kind: f[keyKind].Str(), ok: f["ok"].Bool(), rev: ev["rev"].Int(), epoch: ev["restore_epoch"].Int()}
	for _, r := range f["rooms"].Items() {
		room := fields(t, r, "room", "event_id", "found")
		a.rooms = append(a.rooms, room["room"].Str())
		a.found = append(a.found, room["found"].Bool())
	}
	return a
}

// stoppedAfter holds the homeserver stopped after the check, and the tree as it was before it:
// the recorded drop-in the start wrote is gone, with its directory (sc-D14).
func (w *checkWorld) stoppedAfter(at string, before map[string]string) {
	w.t.Helper()
	if a := w.unit(paths.ServerUnit(w.team)).active; a != "inactive" {
		w.t.Errorf("%s: the homeserver is %s after the check", at, a)
	}
	sameContents(w.t, at, w.contents(), before)
}

// Section 6, O5: on a team either kind of import placed, the check starts the homeserver on
// loopback, asks carry.check for the seal's tail on the recorded port with team.json, stops the
// unit again and answers each room found and ok. It writes nothing of its own.
func TestCarryCheckProvesTheCopy(t *testing.T) {
	for _, kind := range []Kind{KindSealed, KindDrill} {
		w := newCheckWorld(t, kind)
		before, mark := w.contents(), len(w.calls)
		out, err := w.check()
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if got := w.calls[mark:]; !slices.Equal(got, checkCalls(w.team)) {
			t.Errorf("%s: calls %q\nwant %q", kind, got, checkCalls(w.team))
		}
		if len(w.asked) != 1 {
			t.Fatalf("%s: carry.check asked %d times", kind, len(w.asked))
		}
		a := w.asked[0]
		if string(a.TeamFile) != string(w.read(paths.TeamDeclaration(w.team))) || a.Port.Int() != 8448 || !slices.Equal(a.Tail, w.src.tail.Rooms) {
			t.Errorf("%s: carry.check asked %+v", kind, a)
		}
		got := checkAnswerOf(t, out)
		if got.team != "ops" || got.kind != string(kind) || !got.ok || got.rev != 5 || got.epoch != 2 ||
			!slices.Equal(got.rooms, []string{"main", "reports"}) || !slices.Equal(got.found, []bool{true, true}) {
			t.Errorf("%s: the answer %s", kind, out)
		}
		w.stoppedAfter(string(kind), before)
	}
}

// Section 6: a tail event missing, or a roster event that is not the seal's, fails the check (1,
// not a refusal) naming the room or the event; the answer is still given, not ok, and the unit
// is stopped.
func TestCarryCheckFindsACopyNotWhole(t *testing.T) {
	for name, c := range map[string]struct {
		change func(*checkWorld)
		names  string
		found  []bool
	}{
		"a missing event":    {func(w *checkWorld) { w.missing = []string{"reports"} }, "reports", []bool{true, false}},
		"another rev":        {func(w *checkWorld) { w.epoch.Rev = 4 }, "rev 4", []bool{true, true}},
		"another epoch":      {func(w *checkWorld) { w.epoch.RestoreEpoch = 3 }, "restore epoch 3", []bool{true, true}},
		"every event absent": {func(w *checkWorld) { w.missing = []string{"main", "reports"} }, "main", []bool{false, false}},
	} {
		w := newCheckWorld(t, KindSealed)
		c.change(w)
		before := w.contents()
		out, err := w.check()
		if err == nil || errors.Is(err, ErrRefused) || errors.Is(err, ErrSealed) || !strings.Contains(err.Error(), c.names) {
			t.Errorf("%s: err = %v", name, err)
		}
		got := checkAnswerOf(t, out)
		if got.ok || !slices.Equal(got.found, c.found) {
			t.Errorf("%s: the answer %s", name, out)
		}
		w.stoppedAfter(name, before)
	}
}

// Section 6, RS15: a start or a carry.check that fails is the check's failure, with no answer,
// and the unit is stopped all the same.
func TestCarryCheckStopsAfterAFailure(t *testing.T) {
	failed := errors.New("the server was lost")
	for name, change := range map[string]func(*checkWorld){
		"start": func(w *checkWorld) { w.loopbackErr = failed },
		"check": func(w *checkWorld) { w.checkErr = failed },
	} {
		w := newCheckWorld(t, KindDrill)
		change(w)
		before := w.contents()
		out, err := w.check()
		if !errors.Is(err, failed) || out != nil {
			t.Errorf("%s: out %s, err %v", name, out, err)
		}
		if name == "start" && slices.Contains(w.calls, "child check") {
			t.Error("carry.check was asked after the start failed")
		}
		w.stoppedAfter(name, before)
	}
}

// RS18: a homeserver still activating after its stop fails the check, and the drop-in it runs
// on is left in place with no reload (sc-D14): a later check writes and removes it again.
func TestCarryCheckNeedsTheUnitStopped(t *testing.T) {
	w := newCheckWorld(t, KindSealed)
	w.stuck = true
	if _, err := w.check(); err == nil || !strings.Contains(err.Error(), "activating") {
		t.Fatalf("err = %v", err)
	}
	if !w.has(paths.ServerDropInFile(w.team)) || slices.Contains(w.calls, "daemon-reload") {
		t.Errorf("a unit not stopped lost its drop-in: calls %q", w.calls)
	}
	w.stuck = false
	before := w.contents()
	delete(before, paths.ServerDropInFile(w.team))
	delete(before, filepath.Dir(paths.ServerDropInFile(w.team)))
	if _, err := w.check(); err != nil {
		t.Fatal(err)
	}
	w.stoppedAfter("the check again", before)
}

// sc-D14: the recorded drop-in is removed after the stop; a reload that then fails is the
// check's failure, the drop-in already gone.
func TestCarryCheckRemovesTheDropIn(t *testing.T) {
	w := newCheckWorld(t, KindDrill)
	failed := errors.New("reload failed")
	w.reloadErr = failed
	before := w.contents()
	if _, err := w.check(); !errors.Is(err, failed) {
		t.Fatalf("err = %v", err)
	}
	w.stoppedAfter("a failed reload", before)
}

// sc-D14: a team already applied (its stamp present) refuses the check (70) before anything is
// started, written or stopped: the check would stop a serving team and remove its drop-in.
func TestCarryCheckRefusesAnAppliedTeam(t *testing.T) {
	w := newCheckWorld(t, KindSealed)
	w.put(paths.StampFile(w.team), "{}")
	before, mark := w.contents(), len(w.calls)
	if _, err := w.check(); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "team apply") {
		t.Fatalf("err = %v", err)
	}
	if len(w.calls) != mark {
		t.Errorf("calls %q", w.calls[mark:])
	}
	sameContents(t, "an applied team", w.contents(), before)
}

// Section 6, SG1, SG3: the check runs only on a team whose carried.json exists and decodes, and
// refuses while any carry record is present; nothing is started.
func TestCarryCheckRefusals(t *testing.T) {
	bare := &checkWorld{runWorld: newRunWorld(t, KindSealed)}
	if _, err := bare.check(); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "team import") {
		t.Errorf("no carried.json: %v", err)
	}
	if slices.Contains(bare.calls, "loopback") {
		t.Error("a team never imported was started")
	}
	for _, record := range []func(paths.TeamID) string{paths.CarryImportRecord, paths.SealRecord, paths.DrillRecord} {
		w := newCheckWorld(t, KindSealed)
		w.put(record(w.team), "{}")
		mark := len(w.calls)
		if _, err := w.check(); !errors.Is(err, ErrSealed) {
			t.Errorf("%s: %v", record(w.team), err)
		}
		if len(w.calls) != mark {
			t.Errorf("%s: calls %q", record(w.team), w.calls[mark:])
		}
	}
	w := newCheckWorld(t, KindSealed)
	w.put(paths.CarriedRecord(w.team), "{}")
	if _, err := w.check(); err == nil || slices.Contains(w.calls, "loopback") {
		t.Errorf("a carried.json that does not decode: %v", err)
	}
}

// SL1: the check waits host.PlayWait for the lock, and needs a loopback start in its wiring.
func TestCarryCheckNeedsTheLockAndTheStart(t *testing.T) {
	w := newCheckWorld(t, KindSealed)
	held, err := teamlock.Take(context.Background(), w.root+paths.AdminRunDir, w.team, "apply", 0, w.clock, teamlock.Unguarded)
	if err != nil {
		t.Fatal(err)
	}
	mark, slept := len(w.calls), w.clock.slept
	_, err = w.check()
	if rerr := held.Release(); rerr != nil {
		t.Fatal(rerr)
	}
	if !errors.Is(err, teamlock.ErrBusy) || w.clock.slept-slept < host.PlayWait || len(w.calls) != mark {
		t.Fatalf("err = %v after waiting %v", err, w.clock.slept-slept)
	}
	d := w.checkDeps()
	d.Loopback = nil
	if _, err := Check(context.Background(), d, w.team); err == nil || len(w.calls) != mark {
		t.Fatalf("no loopback start: %v", err)
	}
}
