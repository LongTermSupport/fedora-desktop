package admin

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/user"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// fakeTick is the health tick as a test decides it.
type fakeTick struct {
	outcomes []health.Outcome
	allErr   error
	one      health.Outcome
	oneErr   error
	asked    []paths.TeamID
	ranAll   bool
}

func (f *fakeTick) RunAll(context.Context) ([]health.Outcome, error) {
	f.ranAll = true
	return f.outcomes, f.allErr
}

func (f *fakeTick) One(_ context.Context, team paths.TeamID) (health.Outcome, error) {
	f.asked = append(f.asked, team)
	return f.one, f.oneErr
}

func teamID(t *testing.T, s string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// runEnv runs teamchat-admin with e's process, wiring and seams.
func runEnv(t *testing.T, e Env, stdin string, args ...string) outcome {
	t.Helper()
	var stdout, stderr bytes.Buffer
	e.Args, e.Stdin, e.Stdout, e.Stderr, e.Digest = args, strings.NewReader(stdin), &stdout, &stderr, digest
	code := Run(t.Context(), e)
	return outcome{code, stdout.String(), stderr.String()}
}

func tickSystem(tick *fakeTick, err error) *System {
	return &System{Health: func() (HealthTick, error) { return tick, err }}
}

// WR1: version and a verb of the wrong class never build the System; a verb that needs one
// builds it once, and a wiring that fails is 78 naming what failed.
func TestWireIsCalledOnlyByAVerbThatNeedsTheSystem(t *testing.T) {
	calls := 0
	wire := func(err error) func() (*System, error) {
		return func() (*System, error) {
			calls++
			if err != nil {
				return nil, err
			}
			return tickSystem(&fakeTick{}, nil), nil
		}
	}
	if got := runEnv(t, Env{Proc: rootProc(), Wire: wire(nil)}, "", "version"); got.code != term.ExitOK || calls != 0 {
		t.Fatalf("version = %+v, wired %d times", got, calls)
	}
	if got := runEnv(t, Env{Proc: ownerProc(), Wire: wire(nil)}, "", "health", "--all"); got.code != ExitClass || calls != 0 {
		t.Fatalf("health by the owner = %+v, wired %d times", got, calls)
	}
	if got := runEnv(t, Env{Proc: rootProc(), Wire: wire(nil)}, "", "health", "--all"); got.code != term.ExitOK || calls != 1 {
		t.Fatalf("health = %+v, wired %d times", got, calls)
	}
	got := runEnv(t, Env{Proc: rootProc(), Wire: wire(errors.New("no pool"))}, "", "health", "--all")
	if want := "teamchat-admin: the wiring: no pool\n"; got.code != ExitConfig || got.stderr != want {
		t.Fatalf("a failed wiring = %+v, want 78 %q", got, want)
	}
	sys := tickSystem(&fakeTick{}, nil)
	if got := runEnv(t, Env{Proc: rootProc(), Sys: sys, Wire: wire(errors.New("unused"))}, "", "health", "--all"); got.code != term.ExitOK {
		t.Fatalf("Sys beside Wire = %+v", got)
	}
}

// WR1: the child serves through the wiring's Opener; a wiring that fails is a failed answer.
func TestCtlChildIsWired(t *testing.T) {
	w := newWorld(t)
	req := Request{Op: apply.OpMemberPark, Team: alpha(t), Args: proto.Obj(goodArgs(t)[apply.OpMemberPark]...)}
	raw, err := EncodeRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	opened := 0
	open := func(ctx context.Context, team paths.TeamID, port int, regKey string) (Opened, error) {
		opened++
		return w.open(ctx, team, port, regKey)
	}
	got := runEnv(t, Env{Proc: childProc(), Wire: func() (*System, error) { return &System{Open: open}, nil }}, string(raw), "ctl-child")
	if opened != 1 || got.stderr != "" {
		t.Fatalf("ctl-child = %+v, opened %d", got, opened)
	}
	got = runEnv(t, Env{Proc: childProc(), Wire: func() (*System, error) { return nil, errors.New("broken") }}, string(raw), "ctl-child")
	a, err := DecodeAnswer([]byte(strings.TrimSuffix(got.stdout, "\n")))
	if err != nil || got.code != term.ExitFailure || a.OK || !strings.Contains(a.Error.Message, "the wiring: broken") {
		t.Fatalf("ctl-child with a failed wiring = %+v (%v)", got, err)
	}
}

// VB1, n2f-D9: one stderr line per team; a failed team is 1, a dropped one (busy) is not.
func TestHealthAllIsOneLinePerTeam(t *testing.T) {
	tick := &fakeTick{outcomes: []health.Outcome{
		{Team: teamID(t, "alpha"), Result: health.ResultWritten},
		{Team: teamID(t, "beta"), Result: health.ResultDropped, Why: "busy: the lock is held"},
	}}
	got := runEnv(t, Env{Proc: rootProc(), Sys: tickSystem(tick, nil)}, "", "health", "--all")
	if want := "alpha written\nbeta dropped: busy: the lock is held\n"; got.code != term.ExitOK || got.stderr != want || got.stdout != "" || !tick.ranAll {
		t.Fatalf("health --all = %+v, want 0 %q", got, want)
	}
	tick.outcomes = append(tick.outcomes, health.Outcome{Team: teamID(t, "gamma"), Result: health.ResultFailed, Err: errors.New("two\nlines")})
	got = runEnv(t, Env{Proc: rootProc(), Sys: tickSystem(tick, nil)}, "", "health", "--all")
	if !strings.HasSuffix(got.stderr, "gamma failed: two lines\n") || got.code != term.ExitFailure {
		t.Fatalf("health --all with a failed team = %+v", got)
	}
	tick.allErr = errors.New("the stamps are unreadable")
	got = runEnv(t, Env{Proc: rootProc(), Sys: tickSystem(tick, nil)}, "", "health", "--all")
	if got.code != term.ExitFailure || !strings.Contains(got.stderr, "the stamps are unreadable") {
		t.Fatalf("health --all with RunAll failing = %+v", got)
	}
}

// VB2: health <team> ticks that one team the same way.
func TestHealthOneTeam(t *testing.T) {
	tick := &fakeTick{one: health.Outcome{Team: teamID(t, "alpha"), Result: health.ResultDropped, Why: "busy"}}
	got := runEnv(t, Env{Proc: rootProc(), Sys: tickSystem(tick, nil)}, "", "health", "alpha")
	if got.code != term.ExitOK || got.stderr != "alpha dropped: busy\n" || len(tick.asked) != 1 || tick.asked[0] != teamID(t, "alpha") {
		t.Fatalf("health alpha = %+v, asked %v", got, tick.asked)
	}
	tick.one = health.Outcome{Team: teamID(t, "alpha"), Result: health.ResultFailed, Err: errors.New("probe")}
	if got = runEnv(t, Env{Proc: rootProc(), Sys: tickSystem(tick, nil)}, "", "health", "alpha"); got.code != term.ExitFailure || got.stderr != "alpha failed: probe\n" {
		t.Fatalf("health alpha failed = %+v", got)
	}
	tick.oneErr = errors.New("health: alpha has no stamp")
	if got = runEnv(t, Env{Proc: rootProc(), Sys: tickSystem(tick, nil)}, "", "health", "alpha"); got.code != term.ExitFailure || !strings.Contains(got.stderr, "alpha has no stamp") {
		t.Fatalf("health of an unstamped team = %+v", got)
	}
}

// VB2: anything but --all or one team is 64, before the tick is made; a tick that cannot be
// made is 78.
func TestHealthUsageAndATickThatCannotBeMade(t *testing.T) {
	for _, args := range [][]string{{}, {"--all", "alpha"}, {"alpha", "beta"}, {"--each"}, {"Not A Team"}} {
		made := false
		sys := &System{Health: func() (HealthTick, error) { made = true; return &fakeTick{}, nil }}
		got := runEnv(t, Env{Proc: rootProc(), Sys: sys}, "", append([]string{"health"}, args...)...)
		if got.code != term.ExitUsage || made {
			t.Errorf("health %q = %+v, tick made %v", args, got, made)
		}
	}
	got := runEnv(t, Env{Proc: rootProc(), Sys: tickSystem(nil, errors.New("no owner group"))}, "", "health", "--all")
	if got.code != ExitConfig || !strings.Contains(got.stderr, "the health tick: no owner group") {
		t.Fatalf("health with no tick = %+v", got)
	}
}

// elementUser is a desktop user's process: the test's own uid when it is not root.
func elementUser() *fakeProc {
	if uid := os.Getuid(); uid != 0 {
		return &fakeProc{uid: uid, euid: uid}
	}
	return ownerProc()
}

func elementSystem(t *testing.T, home string, homeErr error, looked *[]int) *System {
	t.Helper()
	w := newWorld(t)
	sys := w.sys()
	sys.Home = func(uid int) (string, error) {
		*looked = append(*looked, uid)
		return home, homeErr
	}
	return sys
}

// VB3: element check runs for the real uid with its home from the account database; an input
// it refuses is 64, a home that cannot be found 1.
func TestElementCheck(t *testing.T) {
	proc := elementUser()
	var looked []int
	home := t.TempDir()
	got := runEnv(t, Env{Proc: proc, Sys: elementSystem(t, home, nil, &looked)}, "[]", "element", "check")
	if got.code != term.ExitOK || !strings.Contains(got.stderr, "apply would refuse") || len(looked) != 1 || looked[0] != proc.uid {
		t.Fatalf("element check = %+v, looked up %v", got, looked)
	}
	if got = runEnv(t, Env{Proc: proc, Sys: elementSystem(t, home, nil, &looked)}, "{}", "element", "check"); got.code != term.ExitUsage {
		t.Fatalf("element check of a bad input = %+v", got)
	}
	if got = runEnv(t, Env{Proc: proc, Sys: elementSystem(t, home, nil, &looked)}, "[]", "element", "check", "--consent"); got.code != term.ExitUsage {
		t.Fatalf("element check --consent = %+v", got)
	}
	got = runEnv(t, Env{Proc: proc, Sys: elementSystem(t, "", errors.New("no such account"), &looked)}, "[]", "element", "check")
	if got.code != term.ExitFailure || !strings.Contains(got.stderr, "no such account") {
		t.Fatalf("element check with no home = %+v", got)
	}
}

// VB4: element apply --consent records the consent first; without it an unconsented apply is
// refused (64); a setuid process (euid 0) is root to element (EL5).
func TestElementApply(t *testing.T) {
	proc := elementUser()
	var looked []int
	home := t.TempDir()
	if got := runEnv(t, Env{Proc: proc, Sys: elementSystem(t, home, nil, &looked)}, "[]", "element", "apply"); got.code != term.ExitUsage {
		t.Fatalf("element apply without consent = %+v", got)
	}
	got := runEnv(t, Env{Proc: proc, Sys: elementSystem(t, home, nil, &looked)}, "not json", "element", "apply", "--consent")
	consent, err := paths.ElementConsentFile(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(strings.TrimSuffix(consent, "/element-consent")); err != nil || got.code != term.ExitUsage {
		t.Fatalf("element apply --consent of a bad input = %+v; the consent directory: %v", got, err)
	}
	if os.Getuid() != 0 {
		if got = runEnv(t, Env{Proc: proc, Sys: elementSystem(t, home, nil, &looked)}, "[]", "element", "apply", "--consent"); got.code != term.ExitOK {
			t.Fatalf("element apply --consent = %+v", got)
		}
	}
	for _, args := range [][]string{{"--consent", "--consent"}, {"--force"}, {"extra"}} {
		if got = runEnv(t, Env{Proc: proc, Sys: elementSystem(t, home, nil, &looked)}, "[]", append([]string{"element", "apply"}, args...)...); got.code != term.ExitUsage {
			t.Errorf("element apply %q = %+v", args, got)
		}
	}
	setuid := &fakeProc{uid: owner, euid: 0}
	if got = runEnv(t, Env{Proc: setuid, Sys: elementSystem(t, home, nil, &looked)}, "[]", "element", "check"); got.code != term.ExitUsage || !strings.Contains(got.stderr, "not root") {
		t.Fatalf("element check with euid 0 = %+v", got)
	}
}

// VB5 beside WR2: team apply reads the pin and teamchat-ctl's ids only when it runs, and a
// missing one is 78.
func TestTeamApplyReadsThePinAndTheCtlIDs(t *testing.T) {
	for name, broken := range map[string]func(*System){
		"the server pin":           func(s *System) { s.Pin = func() (host.Pin, error) { return host.Pin{}, errors.New("no pin file") } },
		"the teamchat-ctl account": func(s *System) { s.CtlIDs = func() (User, error) { return User{}, errors.New("no account") } },
	} {
		w := newWorld(t)
		sys := w.sys()
		broken(sys)
		var stdout, stderr bytes.Buffer
		code := Run(t.Context(), Env{Args: []string{"team", "apply"}, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Proc: rootProc(), Digest: digest, Sys: sys})
		if code != ExitConfig || !strings.Contains(stderr.String(), name) {
			t.Errorf("%s missing: %d %q", name, code, stderr.String())
		}
	}
}

// LookupHome is the account database's home of a uid, never $HOME.
func TestLookupHome(t *testing.T) {
	t.Setenv("HOME", "/nowhere")
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	home, err := LookupHome(os.Getuid())
	if err != nil || home != u.HomeDir {
		t.Fatalf("LookupHome = %q, %v; want %q", home, err, u.HomeDir)
	}
	if _, err := LookupHome(-7); err == nil {
		t.Fatal("LookupHome of no account answered")
	}
}
