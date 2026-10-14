package board

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestCollectTeamsOrdersDeclaredThenUnitsThenStamps(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("declared", teamtest.DeclaredAlpha+
		"TEAM\tbeta\t-\tpresent\tbeta.agent-bus.internal\t-\n"+
		"TEAM\tgone\t6169\tabsent\tgone.agent-bus.internal\t-\n")
	h.On("systemctl list-units", teamtest.UnitAlpha+"agent-bus-hs@gamma.service loaded failed failed Homeserver gamma\nother.service loaded active running x\n", 0)
	teamtest.MustWrite(t, filepath.Join(h.c.AppliedDir, "delta.json"), "{}")
	teamtest.MustWrite(t, filepath.Join(h.c.AppliedDir, "alpha.json"), "{}")
	teamtest.MustWrite(t, filepath.Join(h.c.AppliedDir, "note.txt"), "")
	s := NewSnapshot()
	if err := h.c.collectTeams(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	if want := []string{"alpha", "beta", "gamma", "delta"}; !reflect.DeepEqual(s.Teams, want) {
		t.Fatalf("teams %v, want %v", s.Teams, want)
	}
	if s.Port["alpha"] != "6167" || s.Port["beta"] != "-" || s.Port["gamma"] != "" || s.Port["gone"] != "" {
		t.Fatalf("ports %v", s.Port)
	}
	if s.URL["alpha"] != "http://192.0.2.10:6167" || s.URL["beta"] != "" || s.Server["alpha"] != "alpha.agent-bus.internal" {
		t.Fatalf("url %v server %v", s.URL, s.Server)
	}
	if !s.Loaded["alpha"] || !s.Loaded["gamma"] || s.Loaded["beta"] {
		t.Fatalf("loaded %v", s.Loaded)
	}
}

func TestCollectTeamsReportsWhatItCouldNotRead(t *testing.T) {
	h := newRig(t)
	h.On("systemctl list-units", "", 0)
	h.OnErr(h.DeclarePrefix("overview"), "declare: bad shape", 78)
	if err := h.c.collectTeams(t.Context(), NewSnapshot()); err == nil {
		t.Fatal("a declare failure fails the collection")
	}
	for _, want := range []string{"declare: bad shape\n", "ERROR: the teams declared in " + h.c.HostVars + ", and whether each is applied as declared, could not be read (the reason is above).\n"} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}

	h = newRig(t)
	h.OnDeclare("declared", "")
	h.OnErr("systemctl list-units", "boom", 1)
	if err := h.c.collectTeams(t.Context(), NewSnapshot()); err == nil {
		t.Fatal("a systemctl failure fails the collection")
	}
	if !strings.Contains(h.ErrOut.String(), "ERROR: systemctl could not list the agent-bus homeserver units.\n") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}

	h = newRig(t)
	h.Host.RootDir = filepath.Join(t.TempDir(), "gone")
	if err := h.c.collectTeams(t.Context(), NewSnapshot()); err == nil || !strings.Contains(h.ErrOut.String(), "has gone") {
		t.Fatalf("a missing checkout is refused: %v %q", err, h.ErrOut.String())
	}
}

func TestCollectTeamsReadsStatusAndWords(t *testing.T) {
	h := newRig(t)
	h.On("systemctl list-units", "agent-bus-hs@b.service loaded active running x\nagent-bus-hs@a.service loaded active running x\n", 0)
	h.OnDeclare("status", "STATUS\ta\tcurrent\t-\nSTATUS\tb\toutdated\tunstamped\nSTATUS\tc\toutdated\tconfig,software\n"+
		"STATUS\td\toutdated\tconfig\nSTATUS\te\tunbuilt\t-\nSTATUS\tf\tremoved\t-\nSTATUS\tg\tdecide\tabsent\n"+
		"STATUS\th\tdecide\tundeclared\nSTATUS\ti\tunreadable\tbad yaml\n")
	s := NewSnapshot()
	if err := h.c.collectTeams(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"a": "up to date", "b": "out of date", "c": "out of date: both", "d": "out of date: config",
		"e": "not built", "f": "removed", "g": "decide: absent", "h": "decide: undeclared",
		"i": "unreadable", "zz": "unknown",
	}
	for team, word := range want {
		if got := s.StatusWord(team); got != word {
			t.Errorf("statusWord(%s) = %q, want %q", team, got, word)
		}
	}
	var installed []string
	for _, c := range h.Fake.Calls() {
		for i, a := range c.Args {
			if a == "--installed" {
				installed = append(installed, c.Args[i+1])
			}
		}
	}
	if !reflect.DeepEqual(installed, []string{"a", "b"}) {
		t.Fatalf("--installed %v", installed)
	}

	if calls := h.Spawned(); calls["declare.py overview"] != 1 || len(calls) != 2 {
		t.Fatalf("declaration and status were not read in one run: %v", calls)
	}
}

func TestCollectSessionsGathersLiveRecordsAndCheckouts(t *testing.T) {
	h := populated(t)
	checkout := h.Checkout()
	sessions := filepath.Join(h.Root, "home/.local/state/ccy/sessions")
	teamtest.MustWrite(t, filepath.Join(sessions, "ccy-other"),
		"ccy-session-record 1\nname=ccy-other\ndir=/elsewhere\nlauncher=/x/ccy\nprefix=ccy\nrestore=no\narg=--teams=dev@alpha,2@alpha\narg=--\narg=--teams\narg=9@alpha\n")
	teamtest.MustWrite(t, filepath.Join(sessions, "cc-skip"),
		"ccy-session-record 1\nname=cc-skip\ndir=/skipped\nlauncher=/x/cc\nprefix=cc\nrestore=no\n")
	teamtest.MustWrite(t, filepath.Join(sessions, "ccy-bad"), "garbage")
	h.On("git rev-parse --is-inside-work-tree", "true\n", 0)
	h.On("git rev-parse --show-toplevel", checkout+"\n", 0)
	s := NewSnapshot()
	if err := h.c.collectSessions(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	if want := []string{checkout, "/elsewhere"}; !reflect.DeepEqual(s.Checkouts, want) {
		t.Fatalf("checkouts %v, want %v", s.Checkouts, want)
	}
	if s.SeatSession[checkout+"|1@alpha"] != "ccy-proj" || !s.SeatRestore[checkout+"|1@alpha"] ||
		s.SeatSession["/elsewhere|dev@alpha"] != "ccy-other" {
		t.Fatalf("seat sessions %v restore %v", s.SeatSession, s.SeatRestore)
	}
	if restores, recorded := s.SeatRestore["/elsewhere|2@alpha"]; !recorded || restores {
		t.Fatalf("a record that says restore=no is recorded as not restoring: %v", s.SeatRestore)
	}
	if _, ok := s.SeatSession["/elsewhere|9@alpha"]; ok {
		t.Fatal("launcher args after -- are not the launcher's")
	}
	if _, live := s.Live["ccy-proj"]; !live {
		t.Fatalf("live %v", s.Live)
	}
	if s.Incomplete != "a session record is unreadable" || !strings.Contains(h.ErrOut.String(), "ccy-bad could not be read; its seats show no session.") {
		t.Fatalf("incomplete %q stderr %q", s.Incomplete, h.ErrOut.String())
	}
}

func TestCollectSessionsFailures(t *testing.T) {
	h := newRig(t)
	h.OnErr("tmux -L ccy list-sessions", "protocol version mismatch", 1)
	if err := h.c.collectSessions(t.Context(), NewSnapshot()); err == nil || !strings.Contains(h.ErrOut.String(), "ERROR: ") {
		t.Fatalf("a tmux failure is reported: %v %q", err, h.ErrOut.String())
	}

	h = newRig(t)
	h.OnErr("tmux -L ccy list-sessions", "no server running on /tmp/x", 1)
	h.Env["XDG_STATE_HOME"] = "relative/dir"
	if err := h.c.collectSessions(t.Context(), NewSnapshot()); err == nil || !strings.Contains(h.ErrOut.String(), "XDG_STATE_HOME='relative/dir' is not absolute") {
		t.Fatalf("a relative state home is refused: %v %q", err, h.ErrOut.String())
	}
}

func TestCollectMembers(t *testing.T) {
	h := populated(t)
	s := NewSnapshot()
	h.c.collectMembers(t.Context(), s, "alpha")
	if len(s.Members["alpha"].Humans) != 3 || s.MembersUnknown["alpha"] != "" {
		t.Fatalf("roster %+v unknown %q", s.Members["alpha"], s.MembersUnknown["alpha"])
	}
	want := map[string]string{"alpha|owner": "never signed in", "alpha|guest": "2 signed in", "alpha|ghost": "1 signed in"}
	if !reflect.DeepEqual(s.SignedIn, want) {
		t.Fatalf("signed in %v", s.SignedIn)
	}

	h = populated(t)
	h.OnErr(teamtest.SudoBus+"list alpha", "sudo: a password is required", 1)
	s = NewSnapshot()
	h.c.collectMembers(t.Context(), s, "alpha")
	if s.MembersUnknown["alpha"] != "need sudo" || s.NeedSudo != " alpha" {
		t.Fatalf("unknown %q needSudo %q", s.MembersUnknown["alpha"], s.NeedSudo)
	}

	h = populated(t)
	h.OnErr(teamtest.SudoBus+"list alpha", "agent-bus: no admin token", 78)
	s = NewSnapshot()
	h.c.collectMembers(t.Context(), s, "alpha")
	if s.MembersUnknown["alpha"] != "read failed (above)" || !strings.Contains(h.ErrOut.String(), "no admin token") {
		t.Fatalf("unknown %q stderr %q", s.MembersUnknown["alpha"], h.ErrOut.String())
	}

	h = populated(t)
	h.OnErr(teamtest.SudoBus+"human devices alpha owner", "sudo: a password is required", 1)
	h.OnErr(teamtest.SudoBus+"human devices alpha guest", "boom", 70)
	s = NewSnapshot()
	h.c.collectMembers(t.Context(), s, "alpha")
	if s.SignedIn["alpha|owner"] != "need sudo" || s.SignedIn["alpha|guest"] != "unknown" || s.NeedSudo != " alpha" {
		t.Fatalf("signed in %v needSudo %q", s.SignedIn, s.NeedSudo)
	}
}

func TestCollectBuildsTheRows(t *testing.T) {
	h := populated(t)
	checkout := h.Checkout()
	s, err := h.c.Collect(t.Context())
	if err != nil {
		t.Fatalf("%v: %s", err, h.ErrOut.String())
	}
	shown := term.AbbrevHome(checkout, h.c.Home)
	want := []Row{
		{Team: "alpha", Member: "-", Kind: KindTeam, State: "up", Applied: "up to date", Session: "-", Restore: "-", Where: "port 6167", Handle: "-", Dir: "-"},
		{Team: "alpha", Member: "1", Kind: "ccy", State: "active", Applied: "-", Session: "ccy-proj", Record: "ccy-proj", Restore: "yes", Where: shown, Handle: "proj.1+local.podman", Dir: checkout},
		{Team: "alpha", Member: "dev", Kind: "ccy", State: "unknown", Applied: "-", Session: "free", Restore: "-", Where: shown, Handle: "proj.dev+local.podman", Dir: checkout},
		{Team: "alpha", Member: "proj.2+local.podman", Kind: "ccy", State: "parked", Applied: "-", Session: "-", Restore: "-", Where: "dev", Handle: "proj.2+local.podman", Dir: "-"},
		{Team: "alpha", Member: "proj.box+role.lxc", Kind: "lxc", State: "active", Applied: "-", Session: "-", Restore: "-", Where: "ops", Handle: "proj.box+role.lxc", Dir: "-"},
		{Team: "alpha", Member: "owner", Kind: "human", State: "in room", Applied: "-", Session: "never signed in", Restore: "-", Where: "Element; Enter: new password", Handle: "-", Dir: "-"},
		{Team: "alpha", Member: "guest", Kind: "human", State: "invited", Applied: "-", Session: "2 signed in", Restore: "-", Where: "Element; Enter: new password", Handle: "-", Dir: "-"},
		{Team: "alpha", Member: "ghost", Kind: "human", State: "not in room", Applied: "-", Session: "1 signed in", Restore: "-", Where: "Element; Enter: new password", Handle: "-", Dir: "-"},
	}
	if !reflect.DeepEqual(s.Rows, want) {
		for i := range s.Rows {
			t.Logf("got  %+v", s.Rows[i])
		}
		t.Fatalf("rows differ from the %d expected", len(want))
	}
}

func TestCollectReadsEachSourceOncePerPass(t *testing.T) {
	h := populated(t)
	if _, err := h.c.Collect(t.Context()); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, c := range h.Fake.Calls() {
		counts[c.Name+" "+strings.Join(c.Args[:min(2, len(c.Args))], " ")]++
	}
	for _, prefix := range []string{"systemctl list-units", "tmux -L ccy list-sessions"} {
		n := 0
		for _, c := range h.Fake.Calls() {
			if strings.HasPrefix(c.Line(), prefix) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s ran %d times in one pass", prefix, n)
		}
	}
}

func TestCollectNotInstalledAndDownTeams(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("declared", teamtest.DeclaredAlpha+"TEAM\tbeta\t6168\tpresent\tbeta.agent-bus.internal\t-\n")
	h.OnDeclare("status", "STATUS\talpha\tdecide\tabsent\nSTATUS\tbeta\tunbuilt\t-\nSTATUS\tgamma\tunreadable\tbad\n")
	h.On("systemctl list-units", "agent-bus-hs@alpha.service loaded inactive dead x\nagent-bus-hs@gamma.service loaded failed failed x\n", 0)
	h.On("tmux -L ccy list-sessions", "", 0)
	h.On("git rev-parse --is-inside-work-tree", "", 128)
	s, err := h.c.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var wheres []string
	for _, r := range s.Rows {
		if r.Kind == "team" {
			wheres = append(wheres, r.Team+"|"+r.State+"|"+r.Applied+"|"+r.Where)
		}
	}
	want := []string{
		"alpha|down (inactive)|decide: absent|port 6167, members: homeserver down",
		"beta|not installed|not built|port 6168, install it: ccy-teams apply beta",
		"gamma|down (failed)|unreadable|port unknown (not declared), members: homeserver down, entry unreadable: bad",
	}
	if !reflect.DeepEqual(wheres, want) {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(wheres, "\n"), strings.Join(want, "\n"))
	}
}

func TestCollectNotInstalledUndeclaredPointsAtRemove(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("declared", "")
	h.OnDeclare("status", "STATUS\tzeta\tdecide\tundeclared\n")
	h.On("systemctl list-units", "", 0)
	h.On("tmux -L ccy list-sessions", "", 0)
	h.On("git rev-parse --is-inside-work-tree", "", 128)
	teamtest.MustWrite(t, filepath.Join(h.c.AppliedDir, "zeta.json"), "{}")
	s, err := h.c.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Rows) != 1 || s.Rows[0].Where != "port unknown (not declared), not declared present: ccy-teams remove zeta takes it down" {
		t.Fatalf("rows %+v", s.Rows)
	}
}

func TestCollectWithNoTeamsHasNoRows(t *testing.T) {
	h := newRig(t)
	h.OnDeclare("declared", "")
	h.On("systemctl list-units", "", 0)
	s, err := h.c.Collect(t.Context())
	if err != nil || len(s.Rows) != 0 {
		t.Fatalf("rows %v err %v", s.Rows, err)
	}
}

func TestCollectMarksUnreadableSeats(t *testing.T) {
	h := populated(t)
	h.Fake.On([]string{"agent-bus", "seat", "list"}, execx.Result{
		Stdout:   []byte("SEAT\talpha\t1\theld\tself\tproj.1+local.podman\n"),
		Stderr:   []byte("agent-bus: seat 7 unreadable"),
		ExitCode: 78,
	})
	s, err := h.c.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if s.Incomplete != "a seat is unreadable" {
		t.Fatalf("incomplete %q", s.Incomplete)
	}
	if !strings.Contains(h.ErrOut.String(), "ERROR: agent-bus seat list could not read every seat in "+h.Checkout()+" (above); those are left out.") {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
	if s.Rows[0].Where != "port 6167, a seat is unreadable (above)" {
		t.Fatalf("where %q", s.Rows[0].Where)
	}
	if s.Rows[1].Member != "1" {
		t.Fatalf("the seats it did print are used: %+v", s.Rows)
	}
}

func TestCollectSeatInAStoppedSession(t *testing.T) {
	h := populated(t)
	h.On("tmux -L ccy list-sessions", "", 0)
	s, err := h.c.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if s.Rows[1].Session != "ccy-proj(stopped)" || s.Rows[1].Restore != "yes" || s.Rows[1].Record != "ccy-proj" {
		t.Fatalf("row %+v", s.Rows[1])
	}
}

func TestCollectMembersNeedSudoShowsInSeatsAndHumans(t *testing.T) {
	h := populated(t)
	h.OnErr(teamtest.SudoBus+"list alpha", "sudo: a password is required", 1)
	s, err := h.c.Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if s.NeedSudo != " alpha" || s.Rows[0].Where != "port 6167, members: need sudo" || s.Rows[2].State != "need sudo" {
		t.Fatalf("needSudo %q rows %+v", s.NeedSudo, s.Rows)
	}
}

func TestRefreshReadsTeamsAndStatusOnly(t *testing.T) {
	h := populated(t)
	s, err := h.c.Refresh(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Teams, []string{"alpha"}) || s.StatusWord("alpha") != "up to date" {
		t.Fatalf("snapshot %+v", s)
	}
	if h.Fake.Called("tmux") || h.Fake.Called("sudo") {
		t.Fatal("refresh does not read sessions or members")
	}
}

func TestStatusesIsTheAppliedStateOfEveryTeam(t *testing.T) {
	h := populated(t)
	got, err := h.c.Statuses(t.Context())
	if err != nil || len(got) != 1 || got["alpha"].State != "current" {
		t.Fatalf("%+v %v", got, err)
	}
	broken := newRig(t)
	if _, err := broken.c.Statuses(t.Context()); err == nil {
		t.Fatal("no error from a runner that answers nothing")
	}
}
