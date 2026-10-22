package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestroot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/guest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const lxcCheckout = "/srv/checkouts/thing"

// fakeBoxes is lxccmd.LXC as the admin tests need it: each container's state and config. A
// pid, an address or a guest op is never reached by these tests, and fails if it is.
type fakeBoxes struct {
	states  map[string]lxccmd.State
	cfg     map[string]map[string]string
	listErr error
}

func (f fakeBoxes) Containers(context.Context) ([]string, error) {
	return slices.Sorted(maps.Keys(f.states)), f.listErr
}

func (f fakeBoxes) State(_ context.Context, c string) (lxccmd.State, error) { return f.states[c], nil }

func (f fakeBoxes) InitPID(context.Context, string) (int, error) {
	return 0, errors.New("the fake has no pid")
}

func (f fakeBoxes) Addresses(context.Context, string) ([]netip.Addr, error) { return nil, nil }

func (f fakeBoxes) Config(_ context.Context, c string, _ ...string) (map[string]string, error) {
	return f.cfg[c], nil
}

func (f fakeBoxes) Guest(context.Context, lxccmd.Attach, guest.Op, []string, []byte) (lxccmd.GuestResult, error) {
	return lxccmd.GuestResult{}, errors.New("the fake runs no guest op")
}

type gatedApprovals map[paths.TeamID]*declare.Gated

func (a gatedApprovals) Load(team paths.TeamID) (*declare.Gated, error) { return a[team], nil }

// lxcEntry is agent's seat of the checkout in container c, in alpha.
func lxcEntry(c string, seat int) map[string]any {
	return map[string]any{"team": "alpha", "container": c, "user": "agent", "checkout": lxcCheckout, "seat": seat}
}

// lxcDecl is a declaration with alpha and its LXC entries.
func lxcDecl(t *testing.T, entries ...map[string]any) string {
	t.Helper()
	members := make([]any, 0, len(entries))
	for _, e := range entries {
		members = append(members, e)
	}
	return encodeDoc(t, map[string]any{
		"teamchat_address":       "192.0.2.10",
		"teamchat_teams":         []any{docTeam(alphaDoc())},
		"teamchat_lxc_members":   members,
		"teamchat_element_teams": []any{},
	})
}

func seatKey(t *testing.T, seat int) string {
	t.Helper()
	return lxcstate.SeatID{Team: alpha(t), Checkout: lxcCheckout, Seat: seat}.Key().String()
}

// lxcWorld is the world with the LXC seams: alpha approved as decl says, the containers boxes.
func (w *world) lxcSys(t *testing.T, decl string, boxes fakeBoxes) *System {
	t.Helper()
	d, err := declare.Decode([]byte(decl))
	if err != nil {
		t.Fatal(err)
	}
	approvals := gatedApprovals{}
	for _, in := range d.Inputs() {
		g := in.Gated()
		approvals[in.ID()] = &g
	}
	kit, err := paths.ParseDigest(digest)
	if err != nil {
		t.Fatal(err)
	}
	sys := w.sys()
	sys.LXC = func() (*lxc.Deps, error) {
		return &lxc.Deps{
			Root: w.root, LockDir: w.root + paths.AdminRunDir, Clock: w, LXC: boxes,
			Pin: func(context.Context, string) (lxc.Root, error) { return guestroot.OpenDir(t.TempDir()) },
			Bus: func(context.Context, lxc.Root) (guestbus.Systemd, error) {
				return nil, errors.New("the fake has no guest systemd")
			},
			Approvals: approvals,
			Origin:    func(context.Context, paths.TeamID) (string, error) { return "v2", nil },
			Kit:       kit, HostKit: t.TempDir(), HostUIDs: map[int]bool{0: true}, OwnerGID: os.Getgid(), Bridge: "lxcbr0",
		}, nil
	}
	return sys
}

func (w *world) lxcVerb(sys *System, stdin string, args ...string) outcome {
	w.t.Helper()
	var stdout, stderr strings.Builder
	code := Run(w.t.Context(), Env{
		Args: args, Stdin: strings.NewReader(stdin), Stdout: &stdout, Stderr: &stderr,
		Proc: rootProc(), Digest: digest, Sys: sys,
	})
	return outcome{code, stdout.String(), stderr.String()}
}

func stopped(names ...string) fakeBoxes {
	f := fakeBoxes{states: map[string]lxccmd.State{}, cfg: map[string]map[string]string{}}
	for _, n := range names {
		f.states[n] = lxccmd.Stopped
	}
	return f
}

// offlineLine is the line of agent's seat of container c, offline as why says.
func offlineLine(t *testing.T, c string, seat int, why string) string {
	t.Helper()
	return fmt.Sprintf("DEFERRED\talpha\tagent@%s/%s-%d-%s\t%s", c, paths.LXCRepoLabel(lxcCheckout), seat, seatKey(t, seat), why)
}

// n3b-D3, n3b-D1: an offline member that was not targeted fails nothing, and the output
// shows it offline, then the summary.
func TestLXCApplyAnOfflineMemberNotTargetedIsNoFailure(t *testing.T) {
	w := newWorld(t)
	decl := lxcDecl(t, lxcEntry("box-1", 1))
	out := w.lxcVerb(w.lxcSys(t, decl, stopped("box-1")), decl, "lxc", "apply", "--phase", "present")
	want := offlineLine(t, "box-1", 1, "offline: container stopped") + "\nLXC-SUMMARY\t0 changed, 0 skipped, 1 deferred\n"
	if out.code != term.ExitOK || out.stdout != want || out.stderr != "" {
		t.Fatalf("got %d\nstdout %q\nwant   %q\nstderr %s", out.code, out.stdout, want, out.stderr)
	}
}

// n3b-D3: a stopped target alone is 69, its line and the summary printed before the error.
func TestLXCApplyAStoppedTargetIsUnreachable(t *testing.T) {
	w := newWorld(t)
	decl := lxcDecl(t, lxcEntry("box-1", 1), lxcEntry("box-2", 2))
	sys := w.lxcSys(t, decl, stopped("box-1", "box-2"))
	out := w.lxcVerb(sys, decl, "lxc", "apply", "--phase", "present", "--member", "box-1/"+seatKey(t, 1))
	want := offlineLine(t, "box-1", 1, "offline: container stopped") + "\nLXC-SUMMARY\t0 changed, 0 skipped, 1 deferred\n"
	if out.code != ExitUnreachable || out.stdout != want || !strings.Contains(out.stderr, "container box-1 is offline") {
		t.Fatalf("got %d\nstdout %q\nstderr %s", out.code, out.stdout, out.stderr)
	}
}

// n3b-D3: a stopped target joined with a refusal is the refusal's 70.
func TestLXCApplyARefusalBesideAStoppedTargetIsRefused(t *testing.T) {
	w := newWorld(t)
	decl := lxcDecl(t, lxcEntry("box-1", 1), lxcEntry("box-2", 2))
	boxes := stopped("box-1")
	boxes.states["box-2"] = lxccmd.Running
	boxes.cfg["box-2"] = map[string]string{"lxc.start.auto": "0"}
	out := w.lxcVerb(w.lxcSys(t, decl, boxes), decl,
		"lxc", "apply", "--phase", "present", "--member", "box-1/"+seatKey(t, 1), "--member", "box-2/"+seatKey(t, 2))
	if out.code != ExitRefused || !strings.HasSuffix(out.stdout, "LXC-SUMMARY\t0 changed, 0 skipped, 1 deferred\n") {
		t.Fatalf("got %d\nstdout %q\nstderr %s", out.code, out.stdout, out.stderr)
	}
	if !strings.Contains(out.stderr, "container box-1 is offline") || !strings.Contains(out.stderr, "box-2") {
		t.Fatalf("stderr %s", out.stderr)
	}
}

// --only-teams scopes the run, and a team it names that is not present is refused (70) with
// no summary.
func TestLXCApplyOnlyTeams(t *testing.T) {
	w := newWorld(t)
	decl := lxcDecl(t, lxcEntry("box-1", 1))
	sys := w.lxcSys(t, decl, stopped("box-1"))
	if out := w.lxcVerb(sys, decl, "lxc", "apply", "--phase", "absent", "--only-teams", "alpha"); out.code != term.ExitOK || out.stdout != "LXC-SUMMARY\t0 changed, 0 skipped, 0 deferred\n" {
		t.Fatalf("absent: %d %q %s", out.code, out.stdout, out.stderr)
	}
	if out := w.lxcVerb(sys, decl, "lxc", "apply", "--phase", "present", "--only-teams", "alpha,beta"); out.code != ExitRefused || out.stdout != "" {
		t.Fatalf("beta: %d %q %s", out.code, out.stdout, out.stderr)
	}
}

// The flags: each refused with 64 before stdin is read or a seam is built.
func TestLXCApplyRefusesItsFlags(t *testing.T) {
	w := newWorld(t)
	sys := w.sys()
	key := seatKey(t, 1)
	for _, args := range [][]string{
		{},
		{"--phase", "later"},
		{"--phase", "present", "--member"},
		{"--phase", "present", "--member", "box-1"},
		{"--phase", "present", "--member", "Box 1/" + key},
		{"--phase", "present", "--member", "box-1/notakey"},
		{"--phase", "present", "--member", "box-1/" + key, "--member", "box-1/" + key},
		{"--phase", "present", "--only-teams", ""},
		{"--phase", "present", "--only-teams", "alpha,,beta"},
		{"--phase", "present", "--only-teams", "alpha,alpha"},
		{"--phase", "present", "--only-teams", "Alpha"},
		{"--phase", "present", "extra"},
	} {
		out := w.lxcVerb(sys, "", append([]string{"lxc", "apply"}, args...)...)
		if out.code != term.ExitUsage || out.stdout != "" {
			t.Errorf("%v: %d %q %s", args, out.code, out.stdout, out.stderr)
		}
	}
}

// stdin is the declaration, read under 1 MiB and decoded by declare.Decode.
func TestLXCApplyReadsTheDeclaration(t *testing.T) {
	w := newWorld(t)
	decl := lxcDecl(t, lxcEntry("box-1", 1))
	sys := w.lxcSys(t, decl, stopped("box-1"))
	if out := w.lxcVerb(sys, strings.Repeat(" ", maxDeclaration+1), "lxc", "apply", "--phase", "present"); out.code != term.ExitFailure || !strings.Contains(out.stderr, "over") {
		t.Fatalf("oversized: %d %s", out.code, out.stderr)
	}
	if out := w.lxcVerb(sys, `{"teamchat_teams":1}`, "lxc", "apply", "--phase", "present"); out.code == term.ExitOK || out.stdout != "" {
		t.Fatalf("not a declaration: %d %q", out.code, out.stdout)
	}
	if out := w.lxcVerb(sys, "", "lxc", "apply", "--phase", "present"); out.code == term.ExitOK {
		t.Fatalf("empty stdin: %d", out.code)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("stdin broke") }

func TestLXCApplyAStdinThatCannotBeRead(t *testing.T) {
	w := newWorld(t)
	var stdout, stderr strings.Builder
	code := Run(t.Context(), Env{
		Args: []string{"lxc", "apply", "--phase", "present"}, Stdin: failingReader{}, Stdout: &stdout, Stderr: &stderr,
		Proc: rootProc(), Digest: digest, Sys: w.sys(),
	})
	if code != term.ExitFailure || !strings.Contains(stderr.String(), "stdin broke") {
		t.Fatalf("%d %s", code, stderr.String())
	}
}

// The LXC seams: none wired is 1, one that cannot be built 78.
func TestLXCVerbsNeedTheirSeams(t *testing.T) {
	w := newWorld(t)
	decl := lxcDecl(t, lxcEntry("box-1", 1))
	sys := w.sys()
	if out := w.lxcVerb(sys, decl, "lxc", "apply", "--phase", "present"); out.code != term.ExitFailure || !strings.Contains(out.stderr, "not wired") {
		t.Fatalf("unwired apply: %d %s", out.code, out.stderr)
	}
	if out := w.lxcVerb(sys, "", "lxc", "restart", "--member", "box-1/"+seatKey(t, 1)); out.code != term.ExitFailure {
		t.Fatalf("unwired restart: %d %s", out.code, out.stderr)
	}
	sys.LXC = func() (*lxc.Deps, error) { return nil, errors.New("no lxc-ls") }
	if out := w.lxcVerb(sys, decl, "lxc", "apply", "--phase", "present"); out.code != ExitConfig || !strings.Contains(out.stderr, "no lxc-ls") {
		t.Fatalf("apply: %d %s", out.code, out.stderr)
	}
	if out := w.lxcVerb(sys, "", "lxc", "restart", "--member", "box-1/"+seatKey(t, 1)); out.code != ExitConfig {
		t.Fatalf("restart: %d %s", out.code, out.stderr)
	}
}

// A report that cannot be written is a failure.
func TestLXCApplyAnUnwritableStdout(t *testing.T) {
	w := newWorld(t)
	decl := lxcDecl(t, lxcEntry("box-1", 1))
	var stderr strings.Builder
	code := Run(t.Context(), Env{
		Args: []string{"lxc", "apply", "--phase", "present"}, Stdin: strings.NewReader(decl), Stdout: brokenWriter{}, Stderr: &stderr,
		Proc: rootProc(), Digest: digest, Sys: w.lxcSys(t, decl, stopped("box-1")),
	})
	if code != term.ExitFailure || !strings.Contains(stderr.String(), "cannot write the answer") {
		t.Fatalf("%d %s", code, stderr.String())
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("stdout closed") }

// lxc restart: one --member C/K, nothing else; a seat no record holds is refused (70);
// a stopped container holding the seat is 69, naming it.
func TestLXCRestart(t *testing.T) {
	w := newWorld(t)
	decl := lxcDecl(t, lxcEntry("box-1", 1))
	sys := w.lxcSys(t, decl, stopped("box-1"))
	key := seatKey(t, 1)
	target := "box-1/" + key
	for _, args := range [][]string{
		{},
		{"--member"},
		{"--member", "box-1"},
		{"--member", "Box 1/" + key},
		{"--member", "box-1/nokey"},
		{"--member", target, "--seat", "1"},
		{"--member", target, "--member", "box-2/" + key},
		{"--container", "box-1", "--key", key},
		{"--member", target, "extra"},
	} {
		if out := w.lxcVerb(sys, "", append([]string{"lxc", "restart"}, args...)...); out.code != term.ExitUsage {
			t.Errorf("%v: %d %s", args, out.code, out.stderr)
		}
	}
	if out := w.lxcVerb(sys, "", "lxc", "restart", "--member", target); out.code != ExitRefused || !strings.Contains(out.stderr, "names no seat") {
		t.Fatalf("no record: %d %s", out.code, out.stderr)
	}
	box, err := paths.ParseContainerName("box-1")
	if err != nil {
		t.Fatal(err)
	}
	user, err := paths.ParseGuestUser("agent")
	if err != nil {
		t.Fatal(err)
	}
	rec := lxcstate.Record{V: 1, Team: alpha(t), Members: []lxcstate.Entry{{
		Container: box, User: user, Checkout: lxcCheckout, Seat: 1, Status: lxcstate.StatusOffline, SinceMS: t0ms, Why: "offline: container stopped",
	}}}
	if _, err := lxcstate.WriteRecord(w.root, rec, os.Getgid()); err != nil {
		t.Fatal(err)
	}
	out := w.lxcVerb(sys, "", "lxc", "restart", "--member", target)
	if out.code != ExitUnreachable || out.stdout != "" || !strings.Contains(out.stderr, "container box-1 is offline") {
		t.Fatalf("stopped: %d %q %s", out.code, out.stdout, out.stderr)
	}
}

// n3b-D3 as ExitCodeOf: 69 only when every joined error is offline; else the first error that
// is not offline decides, nested joins included.
func TestExitCodeOfAnOfflineMember(t *testing.T) {
	box, err := paths.ParseContainerName("box-1")
	if err != nil {
		t.Fatal(err)
	}
	off := &lxc.OfflineError{Container: box, Why: "offline: container stopped"}
	drift := &host.Error{Verb: "lxc apply", Step: "reads", Drift: true, Err: errors.New("idmap")}
	plain := &host.Error{Verb: "lxc apply", Step: "unit", Err: errors.New("disk full")}
	for name, c := range map[string]struct {
		err  error
		want term.ExitCode
	}{
		"offline alone":            {off, ExitUnreachable},
		"offline twice":            {errors.Join(off, off), ExitUnreachable},
		"offline then a refusal":   {errors.Join(off, drift), ExitRefused},
		"a failure then a refusal": {errors.Join(off, plain, drift), term.ExitFailure},
		"a refusal then a failure": {errors.Join(drift, off, plain), ExitRefused},
		"nested":                   {errors.Join(errors.Join(off, off), errors.Join(off, drift)), ExitRefused},
		"nested offline":           {errors.Join(errors.Join(off), off), ExitUnreachable},
		"wrapped":                  {fmt.Errorf("lxc: %w", off), ExitUnreachable},
		"a wrapped join":           {fmt.Errorf("lxc: %w", errors.Join(off, drift)), ExitRefused},
		"a wrapped offline join":   {fmt.Errorf("lxc: %w", errors.Join(off, fmt.Errorf("again: %w", off))), ExitUnreachable},
	} {
		if got := ExitCodeOf(c.err); got != c.want {
			t.Errorf("%s: %d, want %d", name, got, c.want)
		}
	}
}

// lxcPrefix is the prefix of the fixture's LXC seats.
func lxcPrefix(t *testing.T) proto.Prefix {
	t.Helper()
	p, ok := proto.ParsePrefix("thing+box-1.lxc")
	if !ok {
		t.Fatal("prefix")
	}
	return p
}

// lxcRoster is the child's member work while lxc.Apply holds the lock (gap 20): Join is an add
// with a token credential and kind lxc, whose bundle is the seat side's, and Members is the
// view's states.
func TestLXCRosterAddsAMember(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	r := lxcRoster{sys: w.sys()}
	got, err := r.Join(t.Context(), alpha(t), lxc.Join{Op: apply.OpMemberAdd, Prefix: lxcPrefix(t), Seat: 2, HumanText: true})
	if err != nil || got.Handle != "thing.2+box-1.lxc" {
		t.Fatalf("add = %+v, %v", got, err)
	}
	var bundle struct {
		Handle string `json:"handle"`
		Kind   string `json:"kind"`
	}
	if jerr := json.Unmarshal(got.Bundle, &bundle); jerr != nil || bundle.Handle != got.Handle || bundle.Kind != apply.CredToken {
		t.Fatalf("bundle %s: %v", got.Bundle, jerr)
	}
	states, err := r.Members(t.Context(), alpha(t))
	if err != nil || states[got.Handle] != apply.MemberActive || len(states) != 1 {
		t.Fatalf("members = %v, %v", states, err)
	}
}

// Park parks; return and reissue answer the handle asked for.
func TestLXCRosterParksAndReturns(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	r := lxcRoster{sys: w.sys()}
	ctx := t.Context()
	got, err := r.Join(ctx, alpha(t), lxc.Join{Op: apply.OpMemberAdd, Prefix: lxcPrefix(t), Seat: 2})
	if err != nil {
		t.Fatal(err)
	}
	if perr := r.Park(ctx, alpha(t), got.Handle); perr != nil {
		t.Fatalf("park: %v", perr)
	}
	if states, merr := r.Members(ctx, alpha(t)); merr != nil || states[got.Handle] != apply.MemberParked {
		t.Fatalf("parked = %v, %v", states, merr)
	}
	for _, op := range []string{apply.OpMemberReturn, apply.OpMemberReissue} {
		if again, jerr := r.Join(ctx, alpha(t), lxc.Join{Op: op, Handle: got.Handle}); jerr != nil || again.Handle != got.Handle || len(again.Bundle) == 0 {
			t.Fatalf("%s = %+v, %v", op, again, jerr)
		}
	}
}

// Reach is guest check's stdin: v, team, base_url and plain_http_hosts, nothing else.
func TestLXCRosterReach(t *testing.T) {
	w := newWorld(t)
	w.applied(t)
	reach, err := lxcRoster{sys: w.sys()}.Reach(t.Context(), alpha(t))
	if err != nil {
		t.Fatal(err)
	}
	var facts map[string]any
	if jerr := json.Unmarshal(reach, &facts); jerr != nil || facts["v"] != 1.0 || facts["team"] != "alpha" || facts["base_url"] == "" || facts["plain_http_hosts"] == nil || len(facts) != 4 {
		t.Fatalf("reach %s: %v", reach, jerr)
	}
}

// Join refuses an op it does not make; a retired account is apply.ErrRetired; the child's own
// lie is refused; a team that is not applied fails every call.
func TestLXCRosterFailures(t *testing.T) {
	w := newWorld(t)
	r := lxcRoster{sys: w.sys()}
	ctx := t.Context()
	if _, err := r.Members(ctx, alpha(t)); err == nil {
		t.Fatal("members of a team not applied")
	}
	if _, err := r.Reach(ctx, alpha(t)); err == nil {
		t.Fatal("reach of a team not applied")
	}
	if err := r.Park(ctx, alpha(t), coord); err == nil {
		t.Fatal("park in a team not applied")
	}
	w.applied(t)
	if _, err := r.Join(ctx, alpha(t), lxc.Join{Op: apply.OpMemberPark, Handle: coord}); err == nil || !strings.Contains(err.Error(), "not a join") {
		t.Fatalf("park as a join: %v", err)
	}
	p := lxcPrefix(t)
	got, err := r.Join(ctx, alpha(t), lxc.Join{Op: apply.OpMemberAdd, Prefix: p, Seat: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Park(ctx, alpha(t), got.Handle); err != nil {
		t.Fatal(err)
	}
	if err := w.admin.SetDeactivated(ctx, proto.UserID(got.Handle, server), true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Join(ctx, alpha(t), lxc.Join{Op: apply.OpMemberReturn, Handle: got.Handle}); !errors.Is(err, apply.ErrRetired) {
		t.Fatalf("return of a deactivated account: %v", err)
	}
	w.lie = func(a *Answer) { a.Handle = "thing.9+box-1.lxc" }
	if _, err := r.Join(ctx, alpha(t), lxc.Join{Op: apply.OpMemberAdd, Prefix: p, Seat: 3}); err == nil || !strings.Contains(err.Error(), "not what it was asked") {
		t.Fatalf("a lying child: %v", err)
	}
}
