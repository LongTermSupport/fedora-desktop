package ops

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/playlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/config"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

const (
	opsPlay        = "/root/play-agent-bus.yml"
	opsElementPlay = "/root/play-agent-bus-element.yml"
	opsHostVars    = "/home/u/hv/localhost.yml"
	opsDeclare     = "env PYTHONPATH=/root /usr/bin/python3 -P -m helpers.agent_bus.declare "
)

// opsRig is a Ops wired to a scripted runner and buffers for everything it writes.
type opsRig struct {
	ops       *Ops
	fake      *execx.Fake
	err       *bytes.Buffer
	out       *bytes.Buffer
	tty       bool
	statuses  []map[string]declare.Status // the answer of the n-th refresh; the last one repeats
	refreshes int
	refreshEr error
	checkout  error
	files     map[string]bool
	tools     map[string]bool
	// The play lock: its runtime directory, the clock only the fake sleep moves, the sleeps
	// taken, and a hook run at each (where a test lets another holder go).
	runDir string
	now    time.Time
	sleeps int
	onNap  func(n int)
}

func newOpsRig(t *testing.T) *opsRig {
	t.Helper()
	r := &opsRig{
		fake: &execx.Fake{}, err: &bytes.Buffer{}, out: &bytes.Buffer{}, tty: true,
		files: map[string]bool{}, tools: map[string]bool{"gum": true, "fzf": true},
	}
	r.statuses = []map[string]declare.Status{{}}
	r.runDir = filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(r.runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	r.now = lockStart
	tm := &term.Term{
		Err:        r.err,
		IsTerminal: func(any) bool { return r.tty },
		Getenv:     func(string) string { return "" },
	}
	su := &sudo.Sudo{
		Runner: r.fake, Term: tm, Stdin: strings.NewReader(""), Stdout: r.out,
		OpenTTY: func() (io.ReadCloser, error) {
			if !r.tty {
				return nil, errors.New("no tty")
			}
			return io.NopCloser(strings.NewReader("")), nil
		},
	}
	r.ops = &Ops{
		Cfg:        config.Config{RootDir: "/root", HostVars: opsHostVars, Play: opsPlay, ElementPlay: opsElementPlay, Home: "/home/u"},
		Runner:     r.fake,
		Declare:    &declare.Client{Runner: r.fake, Root: "/root", File: opsHostVars, Err: r.err},
		Sudo:       su,
		Bus:        &agentbus.Client{Runner: r.fake, Root: su, Err: r.err},
		Gum:        &ui.Gum{Runner: r.fake, Stdin: strings.NewReader(""), Stderr: r.err, Plain: true},
		Picker:     &ui.Picker{Runner: r.fake, Stderr: r.err, Plain: true},
		Term:       tm,
		Stdin:      strings.NewReader(""),
		Stdout:     r.out,
		IsTerminal: func(any) bool { return r.tty },
		Refresh: func(context.Context) (map[string]declare.Status, error) {
			n := min(r.refreshes, len(r.statuses)-1)
			r.refreshes++
			if r.refreshEr != nil {
				return nil, r.refreshEr
			}
			return r.statuses[n], nil
		},
		NeedCheckout: func() error { return r.checkout },
		LookPath: func(name string) (string, error) {
			if r.tools[name] {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		FileExists: func(p string) bool { return r.files[p] },
		Width:      func(context.Context) int { return 80 },
		PlayLock: &playlock.Waiter{
			RuntimeDir: r.runDir, UID: os.Getuid(), GID: os.Getgid(), PID: 4321,
			Now: func() time.Time { return r.now },
			Sleep: func(ctx context.Context, d time.Duration) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				r.sleeps++
				r.now = r.now.Add(d)
				if r.onNap != nil {
					r.onNap(r.sleeps)
				}
				return nil
			},
		},
		PlayLockWait: DefaultPlayLockWait,
	}
	return r
}

// on scripts every command that starts with the space-separated prefix.
func (r *opsRig) on(prefix string, res execx.Result) {
	r.fake.On(strings.Fields(prefix), res)
}

// onExit scripts a command that exits with code.
func (r *opsRig) onExit(prefix string, code int, stderr string) {
	r.on(prefix, execx.Result{ExitCode: code, Stderr: []byte(stderr)})
}

// sudoOK makes sudo ready without asking.
func (r *opsRig) sudoOK() { r.on("sudo -n -v", execx.Result{}) }

func (r *opsRig) called(prefix string) bool {
	return slices.ContainsFunc(r.lines(), func(l string) bool { return strings.HasPrefix(l, prefix) })
}

// lines is the command lines run, in order, with the screens' headings cut out (headings has
// them).
func (r *opsRig) lines() []string {
	calls := r.fake.Calls()
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		if c.Name == "tput" { // ui reading the terminal's width for a heading
			continue
		}
		out = append(out, teamtest.Unheaded(c).Line())
	}
	return out
}

// headings is the heading of every question asked (gum and fzf), in order.
func (r *opsRig) headings() []string {
	var out []string
	for _, c := range r.fake.Calls() {
		if c.Name != "fzf" && (c.Name != "gum" || len(c.Args) == 0 || c.Args[0] == "style") {
			continue
		}
		for _, a := range c.Args {
			if heading := teamtest.HeadingOf(strings.TrimPrefix(a, "--header=")); heading != "" {
				out = append(out, heading)
				break
			}
		}
	}
	return out
}

// said is what went to the terminal, without colour codes.
func (r *opsRig) said() string { return opsANSI.ReplaceAllString(r.err.String(), "") }

var opsANSI = regexp.MustCompile("\x1b\\[[0-9;]*m")

func st(state declare.ApplyState, detail string) declare.Status {
	return declare.Status{State: state, Detail: detail}
}

func TestEveryFlowThatMayChangeATeamSaysSo(t *testing.T) {
	r := newOpsRig(t)
	changed := 0
	r.ops.Changed = func() { changed++ }
	ctx := t.Context()
	r.on(opsPlay, execx.Result{})
	r.ops.Apply(ctx, nil)
	r.ops.Remove(ctx, nil)
	r.ops.Restore(ctx, nil)
	if changed != 3 {
		t.Fatalf("apply, remove and restore said so %d times, want 3", changed)
	}
	if err := r.ops.ApplyTeams(ctx, []string{"alpha"}); err == nil || changed != 4 {
		t.Fatalf("applyTeams: err %v, said so %d times in all", err, changed)
	}
}

func TestPlayJSONIsExactlyPythonsDumps(t *testing.T) {
	if got := playJSON([]string{"a", "b"}); got != `{"agent_bus_only_teams": ["a", "b"]}` {
		t.Errorf("got %s", got)
	}
	if got := playJSON([]string{"beta"}); got != `{"agent_bus_only_teams": ["beta"]}` {
		t.Errorf("got %s", got)
	}
}

func TestApplyTeamsRunsPlayThenElementThenSays(t *testing.T) {
	r := newOpsRig(t)
	r.on(opsPlay, execx.Result{})
	r.on(opsDeclare+"element-teams", execx.Result{Stdout: []byte("ELEMENT\tbeta\thttp://192.0.2.10:6167\nELEMENT\tother\thttp://192.0.2.10:6168\n")})
	r.on(opsElementPlay, execx.Result{})
	r.files["/home/u/.local/share/applications/agent-bus-beta-element.desktop"] = true
	r.statuses = []map[string]declare.Status{{"beta": st("current", ""), "delta": st("current", "")}}
	if err := r.ops.ApplyTeams(context.Background(), []string{"beta", "delta"}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		opsPlay + ` -e {"agent_bus_only_teams": ["beta", "delta"]}`,
		opsDeclare + "element-teams --file " + opsHostVars,
		opsElementPlay,
	}
	if got := r.lines(); !slices.Equal(got, want) {
		t.Errorf("calls:\n%q\nwant\n%q", got, want)
	}
	for _, s := range []string{
		"Running play-agent-bus.yml for beta delta: it starts each team's homeserver, and creates its room and its people's accounts.",
		"Running play-agent-bus-element.yml: it installs Element Desktop (sudo) and writes the Element profile for beta.",
		"Applied: beta delta.",
	} {
		if !strings.Contains(r.said(), s) {
			t.Errorf("missing %q in %q", s, r.said())
		}
	}
}

// SPEC-lxc-stopped-members.md L9: a member the play held back because its container was not
// running is said after the apply, which still succeeds.
func TestApplyTeamsSaysTheLxcMembersTheRunHeldBack(t *testing.T) {
	r := newOpsRig(t)
	r.ops.Cfg.AppliedDir = t.TempDir()
	teamtest.MustWrite(t, filepath.Join(r.ops.Cfg.AppliedDir, "lxc", "beta.json"), `{"version": 1, "team": "beta", "members": [
		{"container": "container-a", "user": "agent", "state": "present", "status": "joined", "handle": "p.1+box.lxc", "why": ""},
		{"container": "container-b", "user": "agent", "state": "present", "status": "deferred", "handle": null,
		 "why": "LXC container container-b is STOPPED: start it"}]}`)
	r.on(opsPlay, execx.Result{})
	r.on(opsDeclare+"element-teams", execx.Result{})
	r.statuses = []map[string]declare.Status{{"beta": st("current", "")}}
	if err := r.ops.ApplyTeams(t.Context(), []string{"beta"}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"beta: agent@container-b deferred: LXC container container-b is STOPPED: start it",
		"Applied: beta, with 1 LXC member held back (above).",
	} {
		if !strings.Contains(r.said(), s) {
			t.Errorf("missing %q in %q", s, r.said())
		}
	}
	if strings.Contains(r.said(), "container-a") {
		t.Errorf("a joined member was said: %q", r.said())
	}
}

func TestApplyTeamsWithAnUnreadableLxcRecordFails(t *testing.T) {
	r := newOpsRig(t)
	r.ops.Cfg.AppliedDir = t.TempDir()
	teamtest.MustWrite(t, filepath.Join(r.ops.Cfg.AppliedDir, "lxc", "beta.json"), "{")
	r.on(opsPlay, execx.Result{})
	r.statuses = []map[string]declare.Status{{"beta": st("current", "")}}
	if err := r.ops.ApplyTeams(t.Context(), []string{"beta"}); err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(r.said(), "the LXC member record") {
		t.Errorf("said %q", r.said())
	}
}

func TestApplyTeamsPlayStreamsToStdoutAndStderr(t *testing.T) {
	r := newOpsRig(t)
	r.on(opsPlay, execx.Result{Stdout: []byte("PLAY RECAP\n")})
	r.on(opsDeclare+"element-teams", execx.Result{})
	r.statuses = []map[string]declare.Status{{"beta": st("current", "")}}
	if err := r.ops.ApplyTeams(context.Background(), []string{"beta"}); err != nil {
		t.Fatal(err)
	}
	if r.out.String() != "PLAY RECAP\n" {
		t.Errorf("stdout %q", r.out.String())
	}
	if c := r.fake.Calls()[0]; c.Stderr != r.err {
		t.Errorf("play stderr is not the terminal: %v", c.Stderr)
	}
}

func TestApplyTeamsPlayFailureNamesTheExit(t *testing.T) {
	r := newOpsRig(t)
	r.onExit(opsPlay, 2, "")
	err := r.ops.ApplyTeams(context.Background(), []string{"beta"})
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(r.said(), "ERROR: play-agent-bus.yml failed (exit 2, above).") {
		t.Errorf("said %q", r.said())
	}
	if r.refreshes != 0 {
		t.Error("refreshed after a failed play")
	}
}

func TestApplyTeamsRefreshFailureIsAFailure(t *testing.T) {
	r := newOpsRig(t)
	r.on(opsPlay, execx.Result{})
	r.refreshEr = errors.New("unreadable")
	if err := r.ops.ApplyTeams(context.Background(), []string{"beta"}); err == nil {
		t.Fatal("want an error")
	}
}

func TestApplyTeamsStillBehind(t *testing.T) {
	r := newOpsRig(t)
	r.on(opsPlay, execx.Result{})
	r.statuses = []map[string]declare.Status{{"a": st("current", ""), "b": st("outdated", "config"), "c": st("", "")}}
	if err := r.ops.ApplyTeams(context.Background(), []string{"a", "b", "c"}); err == nil {
		t.Fatal("want an error")
	}
	want := "ERROR: the play finished, but these teams are still not as declared: b (out of date: config) c (unknown)."
	if !strings.Contains(r.said(), want) {
		t.Errorf("said %q", r.said())
	}
	if r.called(opsDeclare + "element-teams") {
		t.Error("element profile attempted though the homeserver side is not applied")
	}
}

func TestApplyTeamsElementTeamsDeclareFailure(t *testing.T) {
	r := newOpsRig(t)
	r.on(opsPlay, execx.Result{})
	r.onExit(opsDeclare+"element-teams", 78, "declare: refused")
	r.statuses = []map[string]declare.Status{{"beta": st("current", "")}}
	if err := r.ops.ApplyTeams(context.Background(), []string{"beta"}); err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(r.said(), "declare: refused") {
		t.Errorf("declare's own words not shown: %q", r.said())
	}
}

func TestApplyTeamsElementPlayFailure(t *testing.T) {
	r := newOpsRig(t)
	r.on(opsPlay, execx.Result{})
	r.on(opsDeclare+"element-teams", execx.Result{Stdout: []byte("ELEMENT\tbeta\thttp://192.0.2.10:6167\n")})
	r.onExit(opsElementPlay, 2, "")
	r.statuses = []map[string]declare.Status{{"beta": st("current", "")}}
	if err := r.ops.ApplyTeams(context.Background(), []string{"beta"}); err == nil {
		t.Fatal("want an error")
	}
	for _, s := range []string{
		"ERROR: play-agent-bus-element.yml failed (above), so beta has no Element profile yet.",
		"ERROR: the homeserver side is applied, but the Element profile is not; once the cause above is fixed, ccy-teams apply beta runs both again.",
	} {
		if !strings.Contains(r.said(), s) {
			t.Errorf("missing %q in %q", s, r.said())
		}
	}
	if strings.Contains(r.said(), "Applied:") {
		t.Error("said Applied after a failure")
	}
}

func TestApplyTeamsElementLauncherMissing(t *testing.T) {
	r := newOpsRig(t)
	r.on(opsPlay, execx.Result{})
	r.on(opsDeclare+"element-teams", execx.Result{Stdout: []byte("ELEMENT\tbeta\thttp://192.0.2.10:6167\n")})
	r.on(opsElementPlay, execx.Result{})
	r.statuses = []map[string]declare.Status{{"beta": st("current", "")}}
	if err := r.ops.ApplyTeams(context.Background(), []string{"beta"}); err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(r.said(), "ERROR: play-agent-bus-element.yml finished, but beta's Element launcher is not there.") {
		t.Errorf("said %q", r.said())
	}
}

func TestElementTeamsKeepsDeclaredOrderAndOnlyNamed(t *testing.T) {
	r := newOpsRig(t)
	r.on(opsDeclare+"element-teams", execx.Result{Stdout: []byte("ELEMENT\tz\tu\nELEMENT\ta\tu\nELEMENT\tq\tu\n")})
	got, err := r.ops.elementTeams(context.Background(), []string{"a", "z"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"z", "a"}) {
		t.Errorf("got %v", got)
	}
}

func TestRunElementPlayDirectly(t *testing.T) {
	r := newOpsRig(t)
	r.on(opsElementPlay, execx.Result{})
	r.files["/home/u/.local/share/applications/agent-bus-a-element.desktop"] = true
	r.files["/home/u/.local/share/applications/agent-bus-b-element.desktop"] = true
	if err := r.ops.RunElementPlay(context.Background(), []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.said(), "for a b.") {
		t.Errorf("said %q", r.said())
	}
}

// ---- cmd_apply -------------------------------------------------------------------------

func TestApplyUsageNeedsOneArgument(t *testing.T) {
	for _, args := range [][]string{nil, {"a", "b"}} {
		r := newOpsRig(t)
		if code := r.ops.Apply(context.Background(), args); code != 64 {
			t.Errorf("%v: code %d", args, code)
		}
		if !strings.Contains(r.said(), "ccy-teams: 'apply' takes one team, or --all. See ccy-teams --help.") {
			t.Errorf("said %q", r.said())
		}
	}
}

func TestApplyCheckoutGone(t *testing.T) {
	r := newOpsRig(t)
	r.checkout = errors.New("gone")
	if code := r.ops.Apply(context.Background(), []string{"--all"}); code != 1 {
		t.Errorf("code %d", code)
	}
	if r.refreshes != 0 {
		t.Error("refreshed with no checkout")
	}
}

func TestApplyRefreshFailureExitsOne(t *testing.T) {
	r := newOpsRig(t)
	r.refreshEr = errors.New("x")
	if code := r.ops.Apply(context.Background(), []string{"--all"}); code != 1 {
		t.Errorf("code %d", code)
	}
}

func TestApplyAllSortedTargetsAndWaiting(t *testing.T) {
	r := newOpsRig(t)
	r.on(opsPlay, execx.Result{})
	r.on(opsDeclare+"element-teams", execx.Result{})
	r.statuses = []map[string]declare.Status{
		{
			"delta": st("unbuilt", ""), "beta": st("outdated", "config"), "ok": st("current", ""),
			"zeta": st("decide", "undeclared"), "gone": st("decide", "absent"), "old": st("removed", ""),
		},
		{"beta": st("current", ""), "delta": st("current", "")},
	}
	if code := r.ops.Apply(context.Background(), []string{"--all"}); code != 0 {
		t.Fatalf("code %d: %s", code, r.said())
	}
	if !r.called(opsPlay + ` -e {"agent_bus_only_teams": ["beta", "delta"]}`) {
		t.Errorf("calls %q", r.lines())
	}
	for _, s := range []string{
		"Left alone, as they need your decision (ccy-teams remove <team>, or declare them again): gone (decide: absent) zeta (decide: undeclared).",
		"Applied: beta delta.",
	} {
		if !strings.Contains(r.said(), s) {
			t.Errorf("missing %q in %q", s, r.said())
		}
	}
	if r.called("sudo") {
		t.Error("apply took a team down")
	}
}

func TestApplyAllBrokenRefuses(t *testing.T) {
	r := newOpsRig(t)
	r.statuses = []map[string]declare.Status{{"z": st("unreadable", "bad port"), "a": st("unreadable", "no name"), "b": st("outdated", "config")}}
	if code := r.ops.Apply(context.Background(), []string{"--all"}); code != 1 {
		t.Errorf("code %d", code)
	}
	want := "ERROR: these teams' entries cannot be read, so nothing was applied: a: no name z: bad port."
	if !strings.Contains(r.said(), want) {
		t.Errorf("said %q", r.said())
	}
	if len(r.fake.Calls()) != 0 {
		t.Errorf("ran %q", r.lines())
	}
}

func TestApplyAllNothingToDo(t *testing.T) {
	r := newOpsRig(t)
	r.statuses = []map[string]declare.Status{{"a": st("current", ""), "b": st("removed", "")}}
	if code := r.ops.Apply(context.Background(), []string{"--all"}); code != 0 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "No team is out of date or waiting to be built.") {
		t.Errorf("said %q", r.said())
	}
	if len(r.fake.Calls()) != 0 {
		t.Errorf("ran %q", r.lines())
	}
}

func TestApplyAllWaitingOnlyStillSaysNothingToDo(t *testing.T) {
	r := newOpsRig(t)
	r.statuses = []map[string]declare.Status{{"gone": st("decide", "absent")}}
	if code := r.ops.Apply(context.Background(), []string{"--all"}); code != 0 {
		t.Errorf("code %d", code)
	}
	for _, s := range []string{"Left alone", "No team is out of date or waiting to be built."} {
		if !strings.Contains(r.said(), s) {
			t.Errorf("missing %q", s)
		}
	}
}

func TestApplyOneTeamStates(t *testing.T) {
	cases := []struct {
		state    declare.Status
		wantCode term.ExitCode
		wantMsg  string
	}{
		{st("outdated", "config"), 0, ""},
		{st("unbuilt", ""), 0, ""},
		{st("current", ""), 0, ""},
		{st("decide", "absent"), 64, "ccy-teams: team t is not declared present; ccy-teams remove t takes a team down. See ccy-teams --help."},
		{st("removed", ""), 64, "team t is not declared present; ccy-teams remove t takes a team down."},
		{st("unreadable", "bad port"), 64, "ccy-teams: team t's entry cannot be read: bad port. See ccy-teams --help."},
		{st("", ""), 64, "ccy-teams: team t is not declared in ~/hv/localhost.yml. See ccy-teams --help."},
	}
	for _, c := range cases {
		r := newOpsRig(t)
		r.on(opsPlay, execx.Result{})
		r.on(opsDeclare+"element-teams", execx.Result{})
		r.statuses = []map[string]declare.Status{{"t": c.state}, {"t": st("current", "")}}
		if c.state.State == "" {
			r.statuses = []map[string]declare.Status{{}}
		}
		if code := r.ops.Apply(context.Background(), []string{"t"}); code != c.wantCode {
			t.Errorf("%+v: code %d, said %q", c.state, code, r.said())
		}
		if c.wantMsg != "" && !strings.Contains(r.said(), c.wantMsg) {
			t.Errorf("%+v: said %q, want %q", c.state, r.said(), c.wantMsg)
		}
		if c.wantCode == 64 && r.called(opsPlay) {
			t.Errorf("%+v: ran the play", c.state)
		}
	}
}

func TestApplyFailureAddsNotApplied(t *testing.T) {
	r := newOpsRig(t)
	r.onExit(opsPlay, 2, "")
	r.statuses = []map[string]declare.Status{{"b": st("unbuilt", ""), "a": st("unbuilt", "")}}
	if code := r.ops.Apply(context.Background(), []string{"--all"}); code != 1 {
		t.Errorf("code %d", code)
	}
	want := "ERROR: not applied: a b. Their entries stay as declared; once the cause above is fixed, ccy-teams apply runs the play again."
	if !strings.Contains(r.said(), want) {
		t.Errorf("said %q", r.said())
	}
}

// The play creates each person's account whether or not the apply then passes, so the
// sign-in is looked at after every apply of a team, and a failed apply still fails.
func TestApplyLooksAtSignInAfterAPassedAndAFailedApply(t *testing.T) {
	for _, exit := range []int{0, 2} {
		r := newOpsRig(t)
		r.onExit(opsPlay, exit, "")
		r.on(opsDeclare+"element-teams", execx.Result{})
		r.statuses = []map[string]declare.Status{{"t": st("unbuilt", "")}, {"t": st("current", "")}}
		var teams []string
		var asked []bool
		r.ops.SignInGap = func(_ context.Context, ts []string, ask bool) error {
			teams, asked = ts, append(asked, ask)
			return nil
		}
		code := r.ops.Apply(context.Background(), []string{"t"})
		if (exit == 0) != (code == 0) {
			t.Errorf("play exit %d: code %d", exit, code)
		}
		if !slices.Equal(teams, []string{"t"}) || !slices.Equal(asked, []bool{true}) {
			t.Errorf("play exit %d: looked at %v asking %v", exit, teams, asked)
		}
		r.tty = false
		asked = nil
		r.refreshes = 0
		r.ops.Apply(context.Background(), []string{"t"})
		if !slices.Equal(asked, []bool{false}) {
			t.Errorf("play exit %d: without a terminal asking %v", exit, asked)
		}
	}
}

func TestApplyThatCannotFinishTheSignInOfferFailsEvenWhenTheApplyPassed(t *testing.T) {
	r := newOpsRig(t)
	r.on(opsPlay, execx.Result{})
	r.on(opsDeclare+"element-teams", execx.Result{})
	r.statuses = []map[string]declare.Status{{"t": st("unbuilt", "")}, {"t": st("current", "")}}
	r.ops.SignInGap = func(context.Context, []string, bool) error { return errors.New("gum broke") }
	if code := r.ops.Apply(context.Background(), []string{"t"}); code != term.ExitFailure {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(r.said(), "gum broke") || strings.Contains(r.said(), "nothing was changed") {
		t.Errorf("said %q", r.said())
	}
}

func TestNewIsOverTheHostsAdapters(t *testing.T) {
	w := teamtest.New(t)
	changed := 0
	o := New(w.Host, func(context.Context) (map[string]declare.Status, error) { return map[string]declare.Status{}, nil }, func() { changed++ })
	if o.Declare != w.Host.Declare || o.Sudo != w.Host.Sudo || o.Bus != w.Host.Bus || o.Gum != w.Host.Gum ||
		o.Picker != w.Host.Picker || o.Refresh == nil || o.NeedCheckout == nil || o.IsTerminal == nil || o.Term == w.Host.Term {
		t.Fatalf("Ops %+v", o)
	}
	if !o.FileExists(w.Root) || o.FileExists(w.Root+"/nowhere") {
		t.Fatal("FileExists does not follow the file system")
	}
	o.Changed()
	if changed != 1 {
		t.Fatal("Changed is the caller's")
	}
}
