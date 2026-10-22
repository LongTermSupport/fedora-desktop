package admin

import (
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

var _ restore.Child = RestoreChild{}

var testUnit = []string{"/usr/bin/systemd-run", "--pipe", "--wait", "--", "/usr/bin/env", "-i", "PATH=/usr/bin:/bin", testSelf, "ctl-child"}

// teamFile is alpha's applied team.json as restore's ops are given it.
func (w *world) teamFile(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(w.root + paths.TeamDeclaration(alpha(t)))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// restoreChild is a RestoreChild whose live and unit children are the in-process child; the
// argvs Unit was given are kept.
func (w *world) restoreChild(argvs *[][]string) RestoreChild {
	return RestoreChild{Live: inProcessChild{w: w}, Unit: func(argv []string) Child {
		*argvs = append(*argvs, argv)
		return inProcessChild{w: w}
	}}
}

// TestRestoreChildSecuresAndChecksThroughTheUnit is RC1 and RC8 end to end: the args encoded
// as the child reads them, secure and the staged check through the unit's child, the live
// check through Live.
func TestRestoreChildSecuresAndChecksThroughTheUnit(t *testing.T) {
	w := withMember(t)
	var argvs [][]string
	c := w.restoreChild(&argvs)
	tf := w.teamFile(t)
	live := w.liveRoster(t)
	e, err := c.Check(t.Context(), alpha(t), nil, restore.CheckArgs{TeamFile: tf})
	if err != nil || e != (restore.Epoch{RestoreEpoch: live.RestoreEpoch, Rev: live.Rev}) || len(argvs) != 0 || len(w.stagedPorts) != 0 {
		t.Fatalf("live check %+v %v, argvs %q", e, err, argvs)
	}
	f := w.merged(t)
	token := w.adminToken(t)
	changed, err := c.Secure(t.Context(), alpha(t), testUnit, restore.SecureArgs{
		TeamFile: tf, Port: stagedPort, Token: token, Rooms: w.rooms(t), Roster: f,
		Retire: []string{sampleHandle}, Revoke: []string{coord}, RevokeHumans: []string{"alice"}, Lock: []string{"bob"},
	})
	if err != nil || len(changed) == 0 || len(argvs) != 1 || !slices.Equal(argvs[0], testUnit) {
		t.Fatalf("secure %q %v, argvs %q", changed, err, argvs)
	}
	w.wantSecuredRoster(t, f)
	e, err = c.Check(t.Context(), alpha(t), testUnit, restore.CheckArgs{TeamFile: tf, Port: stagedPort, Token: token})
	if err != nil || e != (restore.Epoch{RestoreEpoch: 7, Rev: live.Rev + 1}) || len(argvs) != 2 || !slices.Equal(w.stagedPorts, []int{stagedPort, stagedPort}) {
		t.Fatalf("staged check %+v %v, ports %v", e, err, w.stagedPorts)
	}
}

// TestRestoreChildSwapsAndExports is RC2 and RC3 through Live.
func TestRestoreChildSwapsAndExports(t *testing.T) {
	w := withMember(t)
	var argvs [][]string
	c := w.restoreChild(&argvs)
	tf := w.teamFile(t)
	ids := w.rooms(t)
	f := w.merged(t)
	changed, err := c.Swap(t.Context(), alpha(t), restore.SwapArgs{TeamFile: tf, Token: "tar-token", Rooms: ids, Roster: f})
	if err != nil || len(changed) == 0 {
		t.Fatalf("swap %q %v", changed, err)
	}
	if string(w.files.token) != "tar-token" || string(w.files.rooms) != string(apply.EncodeRooms(alpha(t), ids)) || string(w.files.roster) != string(f.Encode()) {
		t.Fatalf("files %q %q %q", w.files.token, w.files.rooms, w.files.roster)
	}
	since := w.send(t, ids.Main, "at-since")
	first := w.send(t, ids.Main, "one")
	x, err := c.Export(t.Context(), alpha(t), restore.ExportArgs{TeamFile: tf, Backup: 3, SinceMS: since})
	if err != nil || x.Count != 1 || x.FirstMS != first || x.Truncated || !slices.Equal(bodiesOf(t, w.files.lost[3]), []string{"one"}) {
		t.Fatalf("export %+v %v", x, err)
	}
	if len(argvs) != 0 {
		t.Fatalf("swap or export went through a unit: %q", argvs)
	}
}

// TestRestoreChildFinishesAndReverts is RC4 and RC5 through Live.
func TestRestoreChildFinishesAndReverts(t *testing.T) {
	w := withMember(t)
	var argvs [][]string
	c := w.restoreChild(&argvs)
	tf := w.teamFile(t)
	w.files.previous = []string{"admin.token"}
	if changed, err := c.Finish(t.Context(), alpha(t), tf); err != nil || !slices.Equal(changed, []string{"admin.token.previous removed"}) {
		t.Fatalf("finish %q %v", changed, err)
	}
	if changed, err := c.Revert(t.Context(), alpha(t), tf); err != nil || !slices.Equal(changed, []string{"admin.token put back"}) {
		t.Fatalf("revert %q %v", changed, err)
	}
	if len(argvs) != 0 {
		t.Fatalf("finish or revert went through a unit: %q", argvs)
	}
	w.files.err = errors.New("eio")
	changed, err := c.Revert(t.Context(), alpha(t), tf)
	if ce, ok := errors.AsType[*ChildError](err); !ok || ce.Class != ClassFailed || len(changed) != 0 {
		t.Fatalf("a failed revert %q %v", changed, err)
	}
}

// stubRestore is a RestoreChild whose children are one stub.
func stubRestore(s *stubChild) RestoreChild {
	return RestoreChild{Live: s, Unit: func([]string) Child { return s }}
}

// TestRestoreChildEncodesTheLiveCheckWithNulls is RC8's live form: port and token null.
func TestRestoreChildEncodesTheLiveCheckWithNulls(t *testing.T) {
	s := &stubChild{a: Answer{OK: true, Changed: []string{}, Epoch: &restore.Epoch{RestoreEpoch: 2, Rev: 9}}}
	e, err := stubRestore(s).Check(t.Context(), alpha(t), nil, restore.CheckArgs{TeamFile: []byte(`{"v":1}`)})
	if err != nil || e.Rev != 9 || len(s.reqs) != 1 || s.reqs[0].Op != OpRestoreCheck {
		t.Fatalf("check %+v %v %+v", e, err, s.reqs)
	}
	want := proto.Obj(proto.M(keyTeamFile, proto.Obj(proto.M("v", proto.Int(1)))), proto.M(keyPort, proto.Null()), proto.M(keyToken, proto.Null()))
	if got := s.reqs[0].Args.Compact(); string(got) != string(want.Compact()) {
		t.Fatalf("args %s", got)
	}
}

// TestRestoreChildAFailedAnswerIsAChildError: its class decides the exit code, and the lines
// changed before it come back beside it.
func TestRestoreChildAFailedAnswerIsAChildError(t *testing.T) {
	failed := Answer{Changed: []string{"member x retired"}, Error: &AnswerError{Class: ClassRefused, Message: "no"}}
	s := &stubChild{a: failed}
	changed, err := stubRestore(s).Secure(t.Context(), alpha(t), testUnit, restore.SecureArgs{TeamFile: []byte(`{"v":1}`), Port: 1, Token: "t"})
	if ce, ok := errors.AsType[*ChildError](err); !ok || ce.Class != ClassRefused || ExitCodeOf(err) != ExitRefused || !slices.Equal(changed, failed.Changed) {
		t.Fatalf("a refused secure %q %v", changed, err)
	}
}

// TestRestoreChildRefusesWhatDoesNotFit: a child that could not answer, or answered what does
// not fit the op, is an error that is not a ChildError.
func TestRestoreChildRefusesWhatDoesNotFit(t *testing.T) {
	tf := []byte(`{"v":1}`)
	broken := errors.New("the pipe broke")
	if changed, err := stubRestore(&stubChild{err: broken}).Finish(t.Context(), alpha(t), tf); !errors.Is(err, broken) || changed != nil {
		t.Fatalf("a child that could not answer %q %v", changed, err)
	}
	notChild := func(name string, err error) {
		t.Helper()
		if ce, isChild := errors.AsType[*ChildError](err); err == nil || isChild {
			t.Errorf("%s: %v %v", name, err, ce)
		}
	}
	lying := &stubChild{a: Answer{OK: true, Changed: []string{}, Epoch: &restore.Epoch{Rev: 1}}}
	changed, err := stubRestore(lying).Finish(t.Context(), alpha(t), tf)
	notChild("an epoch on finish "+strings.Join(changed, ","), err)
	bare := &stubChild{a: Answer{OK: true, Changed: []string{}}}
	x, err := stubRestore(bare).Export(t.Context(), alpha(t), restore.ExportArgs{TeamFile: tf, Backup: 1})
	notChild("no export", err)
	e, err := stubRestore(bare).Check(t.Context(), alpha(t), nil, restore.CheckArgs{TeamFile: tf})
	notChild("no epoch", err)
	if x != (restore.Export{}) || e != (restore.Epoch{}) {
		t.Fatalf("a refused answer's values %+v %+v", x, e)
	}
}

// TestRestoreChildAsksNothingOfWhatIsWrong: a team file that is not an object, a check whose
// unit and endpoint disagree, a secure with no unit or no Unit start.
func TestRestoreChildAsksNothingOfWhatIsWrong(t *testing.T) {
	tf := []byte(`{"v":1}`)
	s := &stubChild{}
	c := stubRestore(s)
	secure := restore.SecureArgs{TeamFile: tf, Port: 1, Token: "t"}
	refused := func(name string, n int, err error) {
		t.Helper()
		if err == nil || n != 0 || len(s.reqs) != 0 {
			t.Errorf("%s: %v, %d values, %d requests", name, err, n, len(s.reqs))
		}
	}
	changed, err := c.Revert(t.Context(), alpha(t), []byte("{"))
	refused("team file not json", len(changed), err)
	changed, err = c.Revert(t.Context(), alpha(t), []byte("[]"))
	refused("team file not an object", len(changed), err)
	changed, err = c.Secure(t.Context(), alpha(t), nil, secure)
	refused("secure with no unit", len(changed), err)
	changed, err = RestoreChild{Live: s}.Secure(t.Context(), alpha(t), testUnit, secure)
	refused("no Unit", len(changed), err)
	e, err := c.Check(t.Context(), alpha(t), nil, restore.CheckArgs{TeamFile: tf, Port: 1, Token: "t"})
	refused("staged check with no unit", int(e.Rev), err)
	e, err = c.Check(t.Context(), alpha(t), testUnit, restore.CheckArgs{TeamFile: tf})
	refused("live check in a unit", int(e.Rev), err)
}

// unitAnswering is a UnitChild over a fake whose child prints stdout and the unit's words and
// exits code; the request it read is put in *stdin.
func unitAnswering(stdout string, code int, stdin *[]byte) (UnitChild, *execx.Fake) {
	f := &execx.Fake{}
	f.OnFunc(testUnit[:1], func(c execx.Cmd) (execx.Result, error) {
		raw, err := io.ReadAll(c.Stdin)
		if err != nil {
			return execx.Result{}, err
		}
		if stdin != nil {
			*stdin = raw
		}
		return execx.Result{Stdout: []byte(stdout), Stderr: []byte("unit words\n"), ExitCode: code}, nil
	})
	return UnitChild{Runner: f, Argv: testUnit}, f
}

// TestUnitChildRunsTheUnitsArgvAsRoot is RU2 and RU3: the argv as given, as root (no
// credential), in /, with PATH and LANG=C alone, the request on stdin.
func TestUnitChildRunsTheUnitsArgvAsRoot(t *testing.T) {
	var stdin []byte
	c, f := unitAnswering(`{"v":1,"ok":true,"changed":["member x parked"]}`+"\n", 0, &stdin)
	a, err := c.Ask(t.Context(), parkRequest(t))
	if err != nil || !a.OK || !slices.Equal(a.Changed, []string{"member x parked"}) {
		t.Fatalf("answer %+v %v", a, err)
	}
	want, err := EncodeRequest(parkRequest(t))
	if err != nil || string(stdin) != string(want) {
		t.Fatalf("stdin %s %v", stdin, err)
	}
	got := f.Calls()[0]
	if got.Name != testUnit[0] || !slices.Equal(got.Args, testUnit[1:]) || got.Dir != "/" || got.Credential != nil {
		t.Fatalf("command %q in %q as %+v", got.Line(), got.Dir, got.Credential)
	}
	if !got.CleanEnv || !slices.Equal(got.Env, []string{"PATH=/usr/bin:/bin", "LANG=C"}) || len(got.ExtraFiles) != 0 {
		t.Fatalf("environment %v %q", got.CleanEnv, got.Env)
	}
}

// TestUnitChildIsJudgedAsExecChild is RU2's judgement (CS4): a failed answer with exit 1 is the
// answer, another exit or a broken answer ErrChild with the unit's stderr; an argv that does not
// start with /usr/bin/systemd-run runs nothing.
func TestUnitChildIsJudgedAsExecChild(t *testing.T) {
	c, f := unitAnswering(`{"v":1,"ok":false,"error":{"class":"refused","message":"no"}}`+"\n", 1, nil)
	if a, err := c.Ask(t.Context(), parkRequest(t)); err != nil || a.OK || a.Error.Class != ClassRefused || len(f.Calls()) != 1 {
		t.Fatalf("a refused answer %+v %v", a, err)
	}
	for name, run := range map[string]struct {
		stdout string
		code   int
	}{"exit 2": {`{"v":1,"ok":true,"changed":[]}` + "\n", 2}, "not json": {"x\n", 0}} {
		c, f := unitAnswering(run.stdout, run.code, nil)
		a, err := c.Ask(t.Context(), parkRequest(t))
		if !errors.Is(err, ErrChild) || ExitCodeOf(err) != term.ExitFailure || !strings.Contains(err.Error(), "unit words") || len(f.Calls()) != 1 {
			t.Errorf("%s: %+v %v", name, a, err)
		}
	}
	for _, argv := range [][]string{nil, {"systemd-run"}, {"/usr/bin/env", testSelf, "ctl-child"}} {
		c, f := unitAnswering("", 0, nil)
		c.Argv = argv
		if a, err := c.Ask(t.Context(), parkRequest(t)); err == nil || ExitCodeOf(err) != ExitConfig || len(f.Calls()) != 0 {
			t.Errorf("argv %q: %+v %v", argv, a, err)
		}
	}
}

// TestUnitChildIsBoundedByChildWait is CS5's bound on the unit's child too.
func TestUnitChildIsBoundedByChildWait(t *testing.T) {
	c, f := unitAnswering(`{"v":1,"ok":true,"changed":[]}`+"\n", 0, nil)
	r := &deadlineRunner{fake: f}
	c.Runner = r
	if a, err := c.Ask(t.Context(), parkRequest(t)); err != nil || !r.bounded {
		t.Fatalf("answer %+v, err %v, bounded %v", a, err, r.bounded)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if a, err := c.Ask(ctx, parkRequest(t)); !errors.Is(err, context.Canceled) || errors.Is(err, ErrChild) {
		t.Fatalf("a cancelled run %+v %v", a, err)
	}
}
