package lxc

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestroot"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxccmd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/lxcstate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

const firstHandle = "repo.1+local.lxc"

// placed is a one-member world, applied once: c1's agent1 joined to t1, the calls forgotten.
func placed(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	if _, err := w.apply(PhasePresent); err != nil {
		t.Fatal(err)
	}
	w.lxc.calls, w.bus.calls, w.roster.joins, w.log = nil, nil, nil, nil
	return w
}

func TestPresentPlacesAndJoins(t *testing.T) {
	w := newWorld(t)
	b := w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	r, err := w.apply(PhasePresent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "CHANGED", "c1", "joined as ")+firstHandle)
	if !strings.HasPrefix(r.Summary, "2 changed") {
		t.Fatalf("summary %q", r.Summary)
	}
	want := []string{"c1 guest check", "c1 guest place-bundle", "c1 guest membership"}
	if got := w.lxc.verbs(); !slices.Equal(got, want) {
		t.Fatalf("guest ops %v, want %v", got, want)
	}
	if got := w.lxc.calls[1].Stdin; got != `{"bundle":"`+firstHandle+`"}` {
		t.Fatalf("place-bundle stdin %q", got)
	}
	if !strings.Contains(w.lxc.calls[2].Stdin, `"seats":[{"team":"t1","seat":"1"}]`) || !strings.Contains(w.lxc.calls[2].Stdin, `"session":"lxc-`+keyOf(t, "t1").String()+`"`) {
		t.Fatalf("membership stdin %s", w.lxc.calls[2].Stdin)
	}
	if !slices.Equal(w.roster.joins, []string{"member.add " + firstHandle}) {
		t.Fatalf("joins %v", w.roster.joins)
	}
	e := w.entry("c1")
	if e.Status != lxcstate.StatusJoined || e.Handle != firstHandle {
		t.Fatalf("entry %+v", e)
	}
	cp, found, err := lxcstate.ReadCopy(w.host, mustBox(t, "c1"), seatID(t, "t1"))
	if err != nil || !found || cp.Generation != 1 || len(cp.Seats) != 1 {
		t.Fatalf("copy %+v %v %v", cp, found, err)
	}
	placedKit(t, b)
	placedUnit(t, w, b)
	if !slices.Equal(w.refresh, []string{"t1"}) {
		t.Fatalf("refresh %v", w.refresh)
	}
}

// placedKit checks the kit, its current link and the member binary's link in b.
func placedKit(t *testing.T, b *box) {
	t.Helper()
	kit := filepath.Join(b.dir, paths.HostKitRoot, kitDigest)
	info, err := os.Stat(filepath.Join(kit, "bin/teamchat"))
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("kit binary %v %v", info, err)
	}
	if info, err := os.Stat(filepath.Join(kit, "plugin/hooks.json")); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("kit file %v %v", info, err)
	}
	if target, err := os.Readlink(filepath.Join(b.dir, paths.HostKitRoot, "current")); err != nil || target != kitDigest {
		t.Fatalf("current %q %v", target, err)
	}
	if target, err := os.Readlink(filepath.Join(b.dir, paths.MemberBinaryPath)); err != nil || target != paths.HostKitCurrent().Binary() {
		t.Fatalf("bin link %q %v", target, err)
	}
}

// placedUnit checks agent1's unit file and wants link in b, and the systemd calls.
func placedUnit(t *testing.T, w *world, b *box) {
	t.Helper()
	if text, ok := guestFile(t, b, unitFileOf(t, "t1")); !ok || !strings.Contains(text, "Group=agents") {
		t.Fatalf("unit %q", text)
	}
	if target, err := os.Readlink(filepath.Join(b.dir, wantsOf(t, "t1"))); err != nil || target != unitFileOf(t, "t1") {
		t.Fatalf("wants link %q %v", target, err)
	}
	if !slices.Equal(w.bus.calls, []string{"Reload", "Start " + unitOf(t, "t1")}) {
		t.Fatalf("systemd %v", w.bus.calls)
	}
}

func TestPresentConvergedChangesNothing(t *testing.T) {
	w := placed(t)
	before, err := os.ReadFile(filepath.Join(w.host, paths.LXCRecord(mustTeam(t, "t1"))))
	if err != nil {
		t.Fatal(err)
	}
	r, err := w.apply(PhasePresent)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Lines) != 0 || !slices.Equal(w.lxc.verbs(), []string{"c1 guest check"}) || len(w.bus.calls) != 0 || len(w.roster.joins) != 0 {
		t.Fatalf("lines %v ops %v systemd %v joins %v", lines(r), w.lxc.verbs(), w.bus.calls, w.roster.joins)
	}
	after, err := os.ReadFile(filepath.Join(w.host, paths.LXCRecord(mustTeam(t, "t1"))))
	if err != nil || string(after) != string(before) {
		t.Fatalf("record rewritten: %v", err)
	}
}

func TestOfflineContainersNeverFailTheTeam(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.addBox("c2").state = lxccmd.Stopped
	w.addBox("c4").state = lxccmd.Frozen
	w.declare([]string{"t1"}, w.member("t1", "c1"), w.member("t1", "c2"), w.member("t1", "c3"), w.member("t1", "c4"))
	r, err := w.apply(PhasePresent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "CHANGED", "c1", "joined as ")+firstHandle)
	hasLine(t, r, lineOf(t, "DEFERRED", "c2", "offline: container stopped"))
	hasLine(t, r, lineOf(t, "DEFERRED", "c3", "offline: container missing"))
	hasLine(t, r, lineOf(t, "DEFERRED", "c4", "offline: container stopped"))
	for _, c := range []string{"c2", "c3", "c4"} {
		if e := w.entry(c); e.Status != lxcstate.StatusOffline || e.Handle != "" {
			t.Fatalf("%s entry %+v", c, e)
		}
	}
	if w.clock.sleeps != 0 {
		t.Fatalf("slept %d times for an offline member", w.clock.sleeps)
	}
	for _, v := range w.lxc.verbs() {
		if !strings.HasPrefix(v, "c1 ") {
			t.Fatalf("attached to an offline container: %v", v)
		}
	}
}

func TestOfflineWithHandleIsSkipped(t *testing.T) {
	w := placed(t)
	b := w.lxc.boxes["c1"]
	b.state = lxccmd.Stopped
	r, err := w.apply(PhasePresent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "SKIPPED", "c1", "offline: container stopped"))
	if e := w.entry("c1"); e.Status != lxcstate.StatusOffline || e.Handle != firstHandle {
		t.Fatalf("entry %+v", e)
	}
	b.state = lxccmd.Running
	r, err = w.apply(PhasePresent)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.roster.joins) != 0 {
		t.Fatalf("a running, joined member was joined again: %v", w.roster.joins)
	}
	if e := w.entry("c1"); e.Status != lxcstate.StatusJoined {
		t.Fatalf("entry %+v lines %v", e, lines(r))
	}
}

func TestMemberTargetOfflineIs69(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.addBox("c2").state = lxccmd.Stopped
	w.declare([]string{"t1"}, w.member("t1", "c1"), w.member("t1", "c2"))
	w.members = []Target{{Container: mustBox(t, "c2"), Key: keyOf(t, "t1")}}
	_, err := w.apply(PhasePresent)
	off, ok := errors.AsType[*OfflineError](err)
	if !ok || off.Container.String() != "c2" || !strings.Contains(err.Error(), "lxc-start") {
		t.Fatalf("err %v", err)
	}
	if len(w.lxc.calls) != 0 {
		t.Fatalf("--member c2 touched c1: %v", w.lxc.verbs())
	}
}

func TestMemberTargetScopesThePhase(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.addBox("c2")
	w.declare([]string{"t1"}, w.member("t1", "c1"), w.member("t1", "c2"))
	w.members = []Target{{Container: mustBox(t, "c2"), Key: keyOf(t, "t1")}}
	if _, err := w.apply(PhasePresent); err != nil {
		t.Fatal(err)
	}
	for _, v := range w.lxc.verbs() {
		if !strings.HasPrefix(v, "c2 ") {
			t.Fatalf("scoped to c2, ran %v", v)
		}
	}
	w.members = []Target{{Container: mustBox(t, "c9"), Key: keyOf(t, "t1")}}
	if _, err := w.apply(PhasePresent); err == nil {
		t.Fatal("a --member no entry declares was accepted")
	}
}

func TestUnapprovedEntryRefusedBeforeAnyContainer(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	w.approvals[mustTeam(t, "t1")] = nil
	_, err := w.apply(PhasePresent)
	if refused, ok := errors.AsType[*declare.RefusedError](err); !ok || refused == nil {
		t.Fatalf("err %v", err)
	}
	if w.lxc.states != 0 || len(w.lxc.calls) != 0 {
		t.Fatal("a container was touched before validation")
	}
}

func TestBadRequestsRefused(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	if _, err := w.apply("sideways"); err == nil {
		t.Fatal("a bad phase was accepted")
	}
	if _, err := w.apply(PhasePresent, "t9"); err == nil {
		t.Fatal("--only-teams naming an undeclared team was accepted")
	}
	w.declare([]string{"t1"}, w.member("t1", "not a name"))
	if _, err := w.apply(PhasePresent); err == nil {
		t.Fatal("a bad container name was accepted")
	}
	w.declare([]string{"t1"}, w.member("t1", "c1"), w.member("t1", "c1"))
	if _, err := w.apply(PhasePresent); err == nil {
		t.Fatal("a repeated entry was accepted")
	}
}

// refusedFact is one LX4 refusal: the fault planted in c2, beside a correct c1.
type refusedFact struct {
	name  string
	plant func(t *testing.T, b *box)
}

func TestRunningWrongFactsFailThatMemberAlone(t *testing.T) {
	for _, tc := range []refusedFact{
		{"idmap", func(_ *testing.T, b *box) { b.cfg["lxc.idmap"] = "u 0 100000 65536" }},
		{"start.auto", func(_ *testing.T, b *box) { b.cfg["lxc.start.auto"] = "0" }},
		{"init not systemd", func(_ *testing.T, b *box) { b.exe = &guestroot.Info{Dev: 1, Ino: 1} }},
		{"tmux only through a link", func(t *testing.T, b *box) {
			t.Helper()
			if err := os.Remove(filepath.Join(b.dir, "usr/bin/tmux")); err != nil {
				t.Fatal(err)
			}
			writeTree(t, b.dir, map[string]string{"usr/local/bin/tmux": "->/usr/bin/tmux"})
		}},
		{"uid 0", func(t *testing.T, b *box) {
			t.Helper()
			writeTree(t, b.dir, map[string]string{"etc/passwd": "agent1:x:0:1001::/srv/agent1:/bin/bash\n"})
		}},
		{"host uid", func(t *testing.T, b *box) {
			t.Helper()
			writeTree(t, b.dir, map[string]string{"etc/passwd": "agent1:x:1000:1001::/srv/agent1:/bin/bash\n"})
		}},
		{"insteadOf", func(t *testing.T, b *box) {
			t.Helper()
			writeTree(t, b.dir, map[string]string{"srv/agent1/src/repo/.git/config": "[url \"x\"]\n\tinsteadOf = y\n"})
		}},
		{"env file of another uid", func(t *testing.T, b *box) {
			t.Helper()
			writeTree(t, b.dir, map[string]string{"srv/agent1/src/repo/.claude/ccy/ccy.env.local": "HOOKS_DAEMON_HOSTNAME=box\n"})
		}},
		{"source address", func(_ *testing.T, b *box) { b.cfg["lxc.net.0.link"] = "virbr0" }},
		{"no passwd", func(t *testing.T, b *box) {
			t.Helper()
			if err := os.Remove(filepath.Join(b.dir, "etc/passwd")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			w.addBox("c1")
			tc.plant(t, w.addBox("c2"))
			w.declare([]string{"t1"}, w.member("t1", "c1"), w.member("t1", "c2"))
			r, err := w.apply(PhasePresent)
			herr, ok := errors.AsType[*host.Error](err)
			if !ok || !herr.Drift {
				t.Fatalf("err %v", err)
			}
			hasLine(t, r, lineOf(t, "CHANGED", "c1", "joined as ")+firstHandle)
			for _, v := range w.lxc.verbs() {
				if strings.HasPrefix(v, "c2 ") {
					t.Fatalf("a refused member was attached to: %v", v)
				}
			}
			if _, found := w.record().Find(mustBox(t, "c2"), checkout, 1); found {
				t.Fatal("a refused member was recorded")
			}
		})
	}
}

func TestChangedOriginRefused(t *testing.T) {
	w := placed(t)
	writeTree(t, w.lxc.boxes["c1"].dir, map[string]string{"srv/agent1/src/repo/.git/config": "[remote \"origin\"]\n\turl = https://github.com/example/other.git\n"})
	r, err := w.apply(PhasePresent)
	if err == nil || !strings.Contains(err.Error(), "origin changed; give the entry another seat") {
		t.Fatalf("err %v lines %v", err, lines(r))
	}
}

// The holdback failing for one seat (until N6, every migration seat) fails that seat alone: the
// container's other seats still apply.
func TestMigrationHoldbackFailingFailsThatSeatAlone(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1", "t2"}, w.member("t1", "c1"), w.memberAt("t2", "c1", 2))
	w.origin = "migration"
	w.v1.errTeam = "t1"
	r, err := w.apply(PhasePresent)
	if !errors.Is(err, errFault) {
		t.Fatalf("err %v", err)
	}
	hasLine(t, r, lineAt(t, "CHANGED", "t2", "agent1", "c1", 2, "joined as repo.2+local.lxc"))
	if !slices.Equal(w.roster.joins, []string{"member.add repo.2+local.lxc"}) {
		t.Fatalf("joins %v", w.roster.joins)
	}
}

func TestMigrationHoldback(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	w.origin = "migration"
	w.v1.held = true
	r, err := w.apply(PhasePresent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "DEFERRED", "c1", "v1 not yet absent"))
	if len(w.roster.joins) != 0 || w.v1.calls != 1 {
		t.Fatalf("joins %v holdback calls %d", w.roster.joins, w.v1.calls)
	}
	w.origin = "v2"
	if _, err := w.apply(PhasePresent); err != nil || w.v1.calls != 1 {
		t.Fatalf("a v2 team asked the holdback: %v", err)
	}
}

func TestPinMovedIsOffline(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	d := w.deps()
	d.Pin = func(context.Context, string) (Root, error) { return nil, guestroot.ErrMoved }
	r, err := Apply(t.Context(), d, Request{Phase: PhasePresent, Decl: w.decl})
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "DEFERRED", "c1", "offline: container stopped during the apply"))
}

func TestGuestCheckExits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		stopped bool
		line    string
		fails   bool
	}{
		{"gh is offline", &lxccmd.GuestError{Op: "guest check", Exit: 78, Code: "gh"}, false, lineAt(t, "DEFERRED", "t1", "agent1", "c2", 2, "offline: guest check gh"), false},
		{"reach is offline", &lxccmd.GuestError{Op: "guest check", Exit: 78, Code: "reach"}, false, lineAt(t, "DEFERRED", "t1", "agent1", "c2", 2, "offline: guest check reach"), false},
		{"attach into a stopped container", &lxccmd.AttachError{Op: "guest check", Exit: -1}, true, lineAt(t, "DEFERRED", "t1", "agent1", "c2", 2, "offline: container stopped during the apply"), false},
		{"home fails the member", &lxccmd.GuestError{Op: "guest check", Exit: 78, Code: "home"}, false, "", true},
		{"64 fails the member", &lxccmd.GuestError{Op: "guest check", Exit: 64, Code: "usage"}, false, "", true},
		{"undecodable fails the member", &lxccmd.GuestError{Op: "guest check", Exit: 3, Undecodable: true}, false, "", true},
		{"attach failing in a running container", &lxccmd.AttachError{Op: "guest check", Exit: -1}, false, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			w.addBox("c1")
			b := w.addBox("c2")
			w.declare([]string{"t1"}, w.member("t1", "c1"), w.memberAt("t1", "c2", 2))
			w.lxc.answer = func(c guestCall) error {
				if c.Container == "c2" && c.Verb == "guest check" {
					if tc.stopped {
						b.state = lxccmd.Stopped
					}
					return tc.err
				}
				return nil
			}
			r, err := w.apply(PhasePresent)
			guestCheckOutcome(t, r, err, tc.fails, tc.line)
			hasLine(t, r, lineOf(t, "CHANGED", "c1", "joined as ")+firstHandle)
		})
	}
}

// guestCheckOutcome checks that a failing guest check failed its member as a guest check
// error, or else printed line.
func guestCheckOutcome(t *testing.T, r Report, err error, fails bool, line string) {
	t.Helper()
	if (err != nil) != fails {
		t.Fatalf("err %v", err)
	}
	if !fails {
		hasLine(t, r, line)
		return
	}
	if herr, ok := errors.AsType[*host.Error](err); !ok || herr.Step != "guest check" || herr.Drift {
		t.Fatalf("err %#v", err)
	}
}

func TestPlaceBundleHeldSeatIsDeferredThenReissued(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	w.lxc.answer = func(c guestCall) error {
		if c.Verb == "guest place-bundle" {
			return &lxccmd.GuestError{Op: c.Verb, Exit: 75, Code: "busy"}
		}
		return nil
	}
	r, err := w.apply(PhasePresent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "DEFERRED", "c1", "seat held; the new identity is placed at the next apply"))
	if e := w.entry("c1"); e.Status != lxcstate.StatusDeferred || e.Handle != firstHandle {
		t.Fatalf("entry %+v", e)
	}
	cp, _, err := lxcstate.ReadCopy(w.host, mustBox(t, "c1"), seatID(t, "t1"))
	if err != nil || len(cp.Seats) != 0 {
		t.Fatalf("a deferred seat reached the copy: %+v %v", cp, err)
	}
	w.lxc.answer = nil
	w.roster.joins = nil
	if _, err := w.apply(PhasePresent); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.roster.joins, []string{"member.reissue " + firstHandle}) {
		t.Fatalf("joins %v", w.roster.joins)
	}
	if e := w.entry("c1"); e.Status != lxcstate.StatusJoined {
		t.Fatalf("entry %+v", e)
	}
}

func TestStopAfterJoinThenReissueOnce(t *testing.T) {
	w := newWorld(t)
	b := w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	w.lxc.answer = func(c guestCall) error {
		if c.Verb == "guest place-bundle" {
			b.state = lxccmd.Stopped
			return &lxccmd.AttachError{Op: c.Verb, Exit: -1}
		}
		return nil
	}
	r, err := w.apply(PhasePresent)
	if err != nil {
		t.Fatal(err)
	}
	// LX6: a handle this phase's join made is recorded offline, its line still DEFERRED.
	hasLine(t, r, lineOf(t, "DEFERRED", "c1", "offline: bundle not placed"))
	w.lxc.answer = nil
	r, err = w.apply(PhasePresent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "SKIPPED", "c1", "offline: bundle not placed"))
	if e := w.entry("c1"); e.Status != lxcstate.StatusOffline || e.Why != "offline: bundle not placed" {
		t.Fatalf("a still-stopped container lost its pending reissue: %+v", e)
	}
	b.state = lxccmd.Running
	w.roster.joins = nil
	if _, err := w.apply(PhasePresent); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.roster.joins, []string{"member.reissue " + firstHandle}) {
		t.Fatalf("joins %v", w.roster.joins)
	}
}

func TestRosterStatesChooseTheJoin(t *testing.T) {
	for state, want := range map[string]string{
		apply.MemberParked:  "member.return " + firstHandle,
		apply.MemberLost:    "member.return " + firstHandle,
		apply.MemberClaimed: "member.add " + firstHandle,
		"":                  "member.add " + firstHandle,
	} {
		w := placed(t)
		w.roster.roster[mustTeam(t, "t1")][firstHandle] = state
		if state == "" {
			delete(w.roster.roster[mustTeam(t, "t1")], firstHandle)
		}
		if _, err := w.apply(PhasePresent); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(w.roster.joins, []string{want}) {
			t.Fatalf("%s: joins %v", state, w.roster.joins)
		}
	}
	w := placed(t)
	w.roster.roster[mustTeam(t, "t1")][firstHandle] = apply.MemberRetired
	if _, err := w.apply(PhasePresent); err == nil || !strings.Contains(err.Error(), "retire") {
		t.Fatalf("a retired handle was not refused: %v", err)
	}
	w = placed(t)
	w.roster.roster[mustTeam(t, "t1")][firstHandle] = apply.MemberActive
	if _, err := w.apply(PhasePresent); err != nil || len(w.roster.joins) != 0 {
		t.Fatalf("an active joined seat was joined again: %v %v", w.roster.joins, err)
	}
}

func TestMembershipStopIsOfflineAndRerun(t *testing.T) {
	w := newWorld(t)
	b := w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	w.lxc.answer = func(c guestCall) error {
		if c.Verb == "guest membership" {
			b.state = lxccmd.Stopped
			return &lxccmd.AttachError{Op: c.Verb, Exit: -1}
		}
		return nil
	}
	r, err := w.apply(PhasePresent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "SKIPPED", "c1", "offline: membership pending"))
	b.state = lxccmd.Running
	w.lxc.answer, w.lxc.calls = nil, nil
	if _, err := w.apply(PhasePresent); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(w.lxc.verbs(), "c1 guest membership") {
		t.Fatalf("membership not rerun for a converged copy: %v", w.lxc.verbs())
	}
}

func TestOtherTeamsSeatsKeptOutsideOnlyTeams(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1", "t2"}, w.member("t1", "c1"), w.member("t2", "c1"))
	if _, err := w.apply(PhasePresent); err != nil {
		t.Fatal(err)
	}
	w.lxc.calls = nil
	w.roster.roster[mustTeam(t, "t2")][firstHandle] = apply.MemberParked
	if _, err := w.apply(PhasePresent, "t2"); err != nil {
		t.Fatal(err)
	}
	cp, _, err := lxcstate.ReadCopy(w.host, mustBox(t, "c1"), seatID(t, "t1"))
	if err != nil || cp.Generation != 1 || len(cp.Seats) != 1 || cp.Seats[0].Team.String() != "t1" {
		t.Fatalf("t1's copy was touched: %+v %v", cp, err)
	}
	for _, c := range w.lxc.calls {
		if c.Verb == "guest check" && slices.Contains(c.Args, "t1") {
			t.Fatal("a team outside --only-teams was checked")
		}
	}
}

func TestUnitChangedRestartsAndServiceOffRemoves(t *testing.T) {
	w := placed(t)
	b := w.lxc.boxes["c1"]
	writeTree(t, b.dir, map[string]string{unitFileOf(t, "t1")[1:]: "old text"})
	if _, err := w.apply(PhasePresent); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.bus.calls, []string{"Reload", "Restart " + unitOf(t, "t1")}) {
		t.Fatalf("systemd %v", w.bus.calls)
	}
	w.bus.calls = nil
	m := w.member("t1", "c1")
	m.Service = false
	w.declare([]string{"t1"}, m)
	if _, err := w.apply(PhasePresent); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.bus.calls, []string{"Stop " + unitOf(t, "t1"), "Reload"}) {
		t.Fatalf("systemd %v", w.bus.calls)
	}
	if _, ok := guestFile(t, b, unitFileOf(t, "t1")); ok {
		t.Fatal("the unit was not removed")
	}
	if _, err := os.Lstat(filepath.Join(b.dir, wantsOf(t, "t1"))); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the wants link was not removed: %v", err)
	}
	w.bus.calls = nil
	if _, err := w.apply(PhasePresent); err != nil || len(w.bus.calls) != 0 {
		t.Fatalf("a removed unit was touched again: %v %v", w.bus.calls, err)
	}
}

func TestUnitRefusedIsOffline(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	w.bus.err = &guestbus.SystemdError{Name: "org.freedesktop.systemd1.UnitStartLimitHit"}
	r, err := w.apply(PhasePresent)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, r, lineOf(t, "SKIPPED", "c1", "offline: unit down; ccy-teams restart starts it"))
	if e := w.entry("c1"); e.Status != lxcstate.StatusOffline || e.Handle != firstHandle {
		t.Fatalf("entry %+v", e)
	}
	cp, _, err := lxcstate.ReadCopy(w.host, mustBox(t, "c1"), seatID(t, "t1"))
	if err != nil || len(cp.Seats) != 1 {
		t.Fatalf("a unit deferral dropped the seat: %+v %v", cp, err)
	}
	w.bus.err = guestbus.ErrMalformed
	if _, err := w.apply(PhasePresent); err == nil {
		t.Fatal("a malformed systemd answer did not fail the member")
	}
}

// A unit start that failed is started again by the next apply, though its file and link are
// unchanged, and only then is the seat recorded joined; a joined seat's unit is left alone.
func TestUnitStartFailedIsRetried(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	w.bus.err = context.DeadlineExceeded
	if _, err := w.apply(PhasePresent); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err %v", err)
	}
	if e := w.entry("c1"); e.Status == lxcstate.StatusJoined {
		t.Fatalf("a seat whose unit did not start was recorded %+v", e)
	}
	w.bus.err, w.bus.calls = nil, nil
	r, err := w.apply(PhasePresent)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.bus.calls, []string{"Reload", "Start " + unitOf(t, "t1")}) {
		t.Fatalf("systemd %v", w.bus.calls)
	}
	hasLine(t, r, lineOf(t, "CHANGED", "c1", "unit started"))
	if e := w.entry("c1"); e.Status != lxcstate.StatusJoined {
		t.Fatalf("entry %+v", e)
	}
	w.bus.calls = nil
	if _, err := w.apply(PhasePresent); err != nil || len(w.bus.calls) != 0 {
		t.Fatalf("a joined seat's unit was started again: %v %v", w.bus.calls, err)
	}
}

func TestBusyTeamLockIs75(t *testing.T) {
	w := newWorld(t)
	w.addBox("c1")
	w.declare([]string{"t1"}, w.member("t1", "c1"))
	held, err := teamlock.Take(t.Context(), filepath.Join(w.host, "lock"), mustTeam(t, "t1"), "test", 0, w.clock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if releaseErr := held.Release(); releaseErr != nil {
			t.Error(releaseErr)
		}
	}()
	d := w.deps()
	_, err = Apply(t.Context(), d, Request{Phase: PhasePresent, Decl: w.decl})
	if !errors.Is(err, teamlock.ErrBusy) {
		t.Fatalf("err %v", err)
	}
}
