package link

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/fakeinbox"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// inbox is the session's inbox socket, served by fakeinbox with the hooks' token.
func (w *linkWorld) inbox() *fakeinbox.Inbox {
	w.t.Helper()
	sock := w.inboxSocket()
	check(w.t, os.MkdirAll(filepath.Dir(sock), 0o700))
	in, err := fakeinbox.Start(sock, fakeinbox.Options{Token: heldToken, Clock: w.clk})
	check(w.t, err)
	w.t.Cleanup(func() { check(w.t, in.Close()) })
	return in
}

// notice is the content of the inbox's first user frame. It is the session's first socket
// write, so the delivery loop's pace never holds it, and the fake clock is left alone: a
// stepped clock would end the write's phases under it.
func (w *linkWorld) notice(in *fakeinbox.Inbox) string {
	w.t.Helper()
	check(w.t, in.Await(w.t.Context(), 2))
	f := in.Frames()
	if f[1].Auth {
		w.t.Fatalf("frames %+v", f)
	}
	return f[1].Content
}

// worker and coordinator are the healths that give a team its role.
func worker() team.Health {
	coord := "repo.2+host.podman"
	return team.Health{Role: proto.RoleWorker, Coordinator: &coord}
}

func coordinator() team.Health { return team.Health{Role: proto.RoleCoordinator} }

// toldWorld is a hook world whose session was framed with ids, each a worker, and is now live
// on the inbox the test serves.
func toldWorld(t *testing.T, ids ...paths.TeamID) (*linkWorld, *fakeinbox.Inbox) {
	t.Helper()
	w := newHookWorld(t, ids...)
	for _, id := range ids {
		w.link().eng.SetHealth(id, worker())
	}
	in := w.inbox()
	w.hook(wire.OpHookSessionStart)
	until(t, w.link().eng.Live)
	return w, in
}

func toldRoles(w *linkWorld) map[paths.TeamID]string { return w.link().session.File().Told.Roles }

// hotAdd adds id after ready and gives it h.
func (w *linkWorld) hotAdd(id paths.TeamID, h team.Health) {
	w.t.Helper()
	w.add(id)
	until(w.t, func() bool {
		v, err := w.link().eng.HookView(w.t.Context())
		check(w.t, err)
		return slices.ContainsFunc(v.Teams, func(ht engine.HookTeam) bool { return ht.Team == id })
	})
	w.link().eng.SetHealth(id, h)
}

// DV6: a team joined is announced on the socket, and told once it is written.
func TestAJoinIsAnnounced(t *testing.T) {
	a, b := tid(t, "alpha"), tid(t, "bravo")
	w, in := toldWorld(t, a)
	w.hotAdd(b, coordinator())
	text := w.notice(in)
	for _, want := range []string{"you joined team bravo as repo.1+site.podman.", "in team bravo you are the coordinator.", "teams are now: alpha, bravo."} {
		if !strings.Contains(text, want) {
			t.Fatalf("notice %q lacks %q", text, want)
		}
	}
	until(t, func() bool { return toldRoles(w)[b] == proto.RoleCoordinator })
	if s := w.sessionFile(); s.Told.Roles[b] != proto.RoleCoordinator || s.Told.Roles[a] != proto.RoleWorker {
		t.Fatalf("told %+v", s.Told)
	}
}

// DV6: a team left is announced with the remove's farewell reason.
func TestLeaves(t *testing.T) {
	released, removed, parked := state.FarewellReleased, state.FarewellRemoved, state.FarewellParked
	for name, c := range map[string]struct {
		farewell *string
		words    string
	}{
		"none":     {nil, "taken out of this session"},
		"released": {&released, "released from this session by the owner; you are still on the team"},
		"removed":  {&removed, "removed by the owner"},
		"parked":   {&parked, "parked by the owner"},
	} {
		t.Run(name, func(t *testing.T) {
			a, b := tid(t, "alpha"), tid(t, "bravo")
			w, in := toldWorld(t, a, b)
			w.send(wire.Msg{Kind: wire.KindRemove, Remove: &wire.Remove{Team: b, Farewell: c.farewell, Txn: "tc" + strings.Repeat("a", 32)}}, nil)
			text := w.notice(in)
			if want := "you left team bravo: " + c.words + ". Commands for it are refused"; !strings.Contains(text, want) {
				t.Fatalf("notice %q lacks %q", text, want)
			}
			if !strings.Contains(text, "teams are now: alpha.") {
				t.Fatalf("notice %q", text)
			}
			until(t, func() bool { return toldRoles(w)[b] == "" })
		})
	}
}

// DV6: a role changed is announced alone, with no joined line.
func TestARoleChangeIsAnnounced(t *testing.T) {
	a := tid(t, "alpha")
	w, in := toldWorld(t, a)
	w.link().eng.SetHealth(a, coordinator())
	text := w.notice(in)
	if !strings.Contains(text, "in team alpha you are the coordinator.") || strings.Contains(text, "joined") {
		t.Fatalf("notice %q", text)
	}
	until(t, func() bool { return toldRoles(w)[a] == proto.RoleCoordinator })
}

// DV6: a notice the socket did not take is the next prompt's lines, told then, and only once.
func TestAnUnwrittenNoticeIsThePrompts(t *testing.T) {
	a, b := tid(t, "alpha"), tid(t, "bravo")
	w, in := toldWorld(t, a)
	in.Refuse()
	l := w.link()
	w.hotAdd(b, worker())
	until(t, l.mem.owed.Load)
	text := contextOf(t, w.hook(wire.OpHookPrompt))
	if !strings.Contains(text, "you joined team bravo") || !strings.Contains(text, "a membership notice from teamchat") {
		t.Fatalf("prompt %q", text)
	}
	if w.sessionFile().Told.Roles[b] != proto.RoleWorker {
		t.Fatal("told not written with the prompt's notice")
	}
	if text := contextOf(t, w.hook(wire.OpHookPrompt)); strings.Contains(text, "joined") {
		t.Fatalf("told twice: %q", text)
	}
}

// DV6: a restarted link finds told in session.json and tells nothing twice; a team told but
// not added again is not said to have left (n3e-10 D7).
func TestARestartedLinkTellsNothingTwice(t *testing.T) {
	a, b, c := tid(t, "alpha"), tid(t, "bravo"), tid(t, "charlie")
	w := newLinkWorld(t)
	w.proc.add(os.Getpid(), pidClaude, 2000)
	w.proc.add(pidClaude, pidSession, 1001)
	w.proc.add(pidSession, 50, 1000)
	s := w.startedSession(state.Unusable{})
	s.Socket = new(w.inboxSocket())
	s.FrameTold = true
	s.Told = state.Told{Generation: 1, Roles: map[paths.TeamID]string{a: proto.RoleWorker, c: proto.RoleWorker}}
	data, err := s.Encode()
	check(t, err)
	check(t, w.root.Replace(w.layout.Run(w.run).Session(), data, 0o600))
	in := w.inbox()
	w.ready(a)
	l := w.link()
	w.send(wire.Msg{Kind: wire.KindToken, Token: &wire.Token{Token: heldToken}}, nil)
	until(t, l.eng.Live)
	l.eng.SetHealth(a, worker())
	w.hotAdd(b, worker())
	text := w.notice(in)
	if !strings.Contains(text, "you joined team bravo") || strings.Contains(text, "team alpha") || strings.Contains(text, "left") {
		t.Fatalf("notice %q", text)
	}
	until(t, func() bool { return toldRoles(w)[b] == proto.RoleWorker })
	if toldRoles(w)[c] != proto.RoleWorker {
		t.Fatalf("told %+v", toldRoles(w))
	}
}

// DV6: SessionStart tells a new session its teams, so a notice owed before it is not given.
func TestSessionStartTellsANewSession(t *testing.T) {
	a, b := tid(t, "alpha"), tid(t, "bravo")
	w := newHookWorld(t, a)
	l := w.link()
	l.eng.SetHealth(a, worker())
	w.hook(wire.OpHookSessionStart)
	w.hotAdd(b, worker())
	until(t, l.mem.owed.Load)
	text := contextOf(t, w.hook(wire.OpHookSessionStart))
	if !strings.Contains(text, "team bravo: you are ") {
		t.Fatalf("frame %q", text)
	}
	if l.mem.owed.Load() || w.sessionFile().Told.Roles[b] != proto.RoleWorker {
		t.Fatalf("told %+v", w.sessionFile().Told)
	}
	if text := contextOf(t, w.hook(wire.OpHookPrompt)); strings.Contains(text, "joined") {
		t.Fatalf("prompt %q", text)
	}
}

// DV6: nothing is announced before the session's frame was told; the frame says it all.
func TestNothingIsAnnouncedBeforeTheFrame(t *testing.T) {
	a := tid(t, "alpha")
	w := newHookWorld(t, a)
	l := w.link()
	l.eng.SetHealth(a, worker())
	l.announce(t.Context())
	if l.mem.owed.Load() || len(toldRoles(w)) != 0 {
		t.Fatalf("announced before the frame: told %v", toldRoles(w))
	}
}

func roles(t *testing.T, pairs ...string) map[paths.TeamID]string {
	t.Helper()
	m := map[paths.TeamID]string{}
	for len(pairs) >= 2 {
		m[tid(t, pairs[0])] = pairs[1]
		pairs = pairs[2:]
	}
	return m
}

func mapsEqual(a, b map[paths.TeamID]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// diffMembership: an unknown role keeps the told one; a probe is told quietly; Coordinator is
// a worker's only; a team left needs a remove seen.
func TestDiffMembership(t *testing.T) {
	a, b, c, d := tid(t, "alpha"), tid(t, "bravo"), tid(t, "charlie"), tid(t, "delta")
	teams := []engine.TeamRole{
		{Team: a},
		{Team: b, Role: proto.RoleProbe, Handle: "repo.1+site.podman"},
		{Team: c, Role: proto.RoleCoordinator, Handle: "repo.1+site.podman", Coordinator: "repo.2+host.podman"},
	}
	told := roles(t, "alpha", proto.RoleWorker, "delta", proto.RoleWorker, "echo", proto.RoleWorker)
	m, got, notice := diffMembership(told, teams, map[paths.TeamID]string{d: proto.FarewellParked})
	if !notice {
		t.Fatal("no notice")
	}
	if want := roles(t, "alpha", proto.RoleWorker, "bravo", proto.RoleProbe, "charlie", proto.RoleCoordinator, "echo", proto.RoleWorker); !mapsEqual(got, want) {
		t.Fatalf("roles %v", got)
	}
	if len(m.Left) != 1 || m.Left[0] != (proto.MembershipLeft{Team: d.String(), Reason: proto.FarewellParked}) {
		t.Fatalf("left %+v", m.Left)
	}
	if len(m.Joined) != 1 || m.Joined[0].Team != c.String() || m.Joined[0].Coordinator != "" || len(m.Roles) != 0 {
		t.Fatalf("joined %+v roles %+v", m.Joined, m.Roles)
	}
	if !slices.Equal(m.Teams, []string{"alpha", "bravo", "charlie"}) {
		t.Fatalf("teams %v", m.Teams)
	}
	text, err := m.Render("0123abcd")
	check(t, err)
	if !strings.Contains(text, "you left team delta: parked by the owner.") {
		t.Fatalf("notice %q", text)
	}
}

// diffMembership: nothing changed is no notice; a probe joining is recorded with none.
func TestDiffMembershipQuiet(t *testing.T) {
	a, b := tid(t, "alpha"), tid(t, "bravo")
	same, kept, notice := diffMembership(roles(t, "alpha", proto.RoleWorker), []engine.TeamRole{{Team: a, Role: proto.RoleWorker}}, nil)
	if notice || len(same.Joined)+len(same.Roles)+len(same.Left) != 0 || kept[a] != proto.RoleWorker {
		t.Fatalf("nothing changed: notice %t, %+v, %v", notice, same, kept)
	}
	probe, quiet, notice := diffMembership(roles(t), []engine.TeamRole{{Team: b, Role: proto.RoleProbe}}, nil)
	if notice || len(probe.Joined) != 0 || quiet[b] != proto.RoleProbe {
		t.Fatalf("probe: notice %t, %+v, %v", notice, probe, quiet)
	}
}

// A notice that cannot render is logged and told anyway, so it is never retried.
func TestAnUnrenderableNoticeIsToldAnyway(t *testing.T) {
	a, b := tid(t, "alpha"), tid(t, "bravo")
	w, in := toldWorld(t, a)
	l := w.link()
	l.mem.mu.Lock()
	l.mem.teams = []engine.TeamRole{{Team: a, Role: proto.RoleWorker}, {Team: b, Role: proto.RoleWorker, Handle: "not a handle"}}
	l.mem.known = true
	l.mem.mu.Unlock()
	l.announce(t.Context())
	if !strings.Contains(w.log.String(), "membership notice cannot be told") || toldRoles(w)[b] != proto.RoleWorker {
		t.Fatalf("log %q told %v", w.log.String(), toldRoles(w))
	}
	if len(in.Frames()) != 0 {
		t.Fatalf("frames %+v", in.Frames())
	}
}
