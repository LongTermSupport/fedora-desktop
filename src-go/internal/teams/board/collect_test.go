package board

import (
	"io/fs"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamtest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestCollectTeamsOrdersDeclaredThenUnitsThenStamps(t *testing.T) {
	h := newRig(t)
	h.WriteHostVars(t, teamtest.HostVarsAlpha+
		"  - team: beta\n    description: Second team\n"+
		"  - team: gone\n    port: 6169\n    state: absent\n")
	h.OnDeclare("status", "")
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
	h.WriteHostVars(t, "agent_bus_teams:\n  - team: alpha\n    port: port\n")
	if err := h.c.collectTeams(t.Context(), NewSnapshot()); err == nil {
		t.Fatal("a declare failure fails the collection")
	}
	for _, want := range []string{"declare: line 3: the port is not a number\n", "ERROR: the teams declared in " + h.c.HostVars + ", and whether each is applied as declared, could not be read (the reason is above).\n"} {
		if !strings.Contains(h.ErrOut.String(), want) {
			t.Fatalf("stderr %q lacks %q", h.ErrOut.String(), want)
		}
	}

	h = newRig(t)
	h.WriteHostVars(t, "")
	h.OnDeclare("status", "")
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
	h.WriteHostVars(t, "")
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

	if calls := h.Spawned(); calls["declare.py status"] != 1 || len(calls) != 2 {
		t.Fatalf("the declaration was not read in-process beside one status run: %v", calls)
	}
}

func TestCollectSessionsGathersLiveRecordsAndCheckouts(t *testing.T) {
	h := populated(t)
	checkout := h.Checkout()
	sessions := filepath.Join(h.Root, "home/.local/state/ccy/sessions")
	teamtest.MustWrite(t, filepath.Join(sessions, "ccy-other"),
		"ccy-session-record 1\nname=ccy-other\ndir=/elsewhere\nlauncher=/x/ccy\nprefix=ccy\nrestore=no\narg=--teams=dev@alpha,2@alpha\narg=--\narg=--teams\narg=9@alpha\n")
	teamtest.MustWrite(t, filepath.Join(sessions, "cc-host"),
		"ccy-session-record 1\nname=cc-host\ndir=/hosted\nlauncher=/x/cc\nprefix=cc\nrestore=no\narg=--teams\narg=3@alpha\n")
	teamtest.MustWrite(t, filepath.Join(sessions, "other-skip"),
		"ccy-session-record 1\nname=other-skip\ndir=/skipped\nlauncher=/x/other\nprefix=other\nrestore=no\narg=--teams\narg=4@alpha\n")
	h.On("tmux -L ccy list-sessions", "ccy-proj 0 "+checkout+"\ncc-desk 1 /desk\nplain 0 /plain\n", 0)
	teamtest.MustWrite(t, filepath.Join(sessions, "ccy-bad"), "garbage")
	h.On("git rev-parse --is-inside-work-tree", "true\n", 0)
	h.On("git rev-parse --show-toplevel", checkout+"\n", 0)
	s := NewSnapshot()
	if err := h.c.collectSessions(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	if want := []string{checkout, "/desk", "/hosted", "/elsewhere"}; !reflect.DeepEqual(s.Checkouts, want) {
		t.Fatalf("checkouts %v, want %v", s.Checkouts, want)
	}
	if s.SeatSession[checkout+"|1@alpha"] != "ccy-proj" || !s.SeatRestore[checkout+"|1@alpha"] ||
		s.SeatSession["/elsewhere|dev@alpha"] != "ccy-other" {
		t.Fatalf("seat sessions %v restore %v", s.SeatSession, s.SeatRestore)
	}
	if restores, recorded := s.SeatRestore["/elsewhere|2@alpha"]; !recorded || restores {
		t.Fatalf("a record that says restore=no is recorded as not restoring: %v", s.SeatRestore)
	}
	if s.SeatSession["/hosted|3@alpha"] != "cc-host" {
		t.Fatalf("a cc session's seats are its record's too: %v", s.SeatSession)
	}
	if _, ok := s.SeatSession["/skipped|4@alpha"]; ok {
		t.Fatal("a record of neither launcher names no seat")
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

func TestCollectSessionsLeavesOutAContainersCheckout(t *testing.T) {
	h := populated(t)
	top := h.InLxcCheckout()
	s := NewSnapshot()
	if err := h.c.collectSessions(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(s.Checkouts, top) {
		t.Fatalf("a container's checkout is read as the host's: %v", s.Checkouts)
	}
	if !slices.Contains(s.Checkouts, h.Checkout()) {
		t.Fatalf("the sessions' checkouts are still read: %v", s.Checkouts)
	}
}

func TestCollectSessionsSaysOnceWhenItCannotTellWhoseCheckoutThisIs(t *testing.T) {
	h := populated(t)
	top := h.InLxcCheckout()
	h.Host.Checkouts.ReadFile = func(string) ([]byte, error) { return nil, fs.ErrPermission }
	for range 2 {
		s := NewSnapshot()
		if err := h.c.collectSessions(t.Context(), s); err != nil {
			t.Fatal(err)
		}
		if slices.Contains(s.Checkouts, top) || s.Incomplete == "" {
			t.Fatalf("checkouts %v incomplete %q", s.Checkouts, s.Incomplete)
		}
	}
	if strings.Count(h.ErrOut.String(), "/etc/lxc/lxc.conf could not be read") != 1 {
		t.Fatalf("stderr %q", h.ErrOut.String())
	}
}

func TestCollectSessionsTakesALiveSessionsDesiredMembership(t *testing.T) {
	h := populated(t)
	runtime := t.TempDir()
	h.Env["XDG_RUNTIME_DIR"] = runtime
	desired := func(session, checkout, seat string) {
		teamtest.MustWrite(t, filepath.Join(runtime, "ccy", "membership", session, "membership.json"),
			`{"v":1,"session":"`+session+`","kind":"ccy","checkout":"`+checkout+`","generation":2,"written_ms":1,"seats":[{"team":"alpha","seat":"`+seat+`"}],"farewell":[]}`)
	}
	desired("ccy-proj", h.Checkout(), "7")
	desired("ccy-gone", "/gone", "8")
	s := NewSnapshot()
	if err := h.c.collectSessions(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	if s.SeatSession[h.Checkout()+"|7@alpha"] != "ccy-proj" || s.SeatSession[h.Checkout()+"|1@alpha"] != "ccy-proj" {
		t.Fatalf("a hot-added seat names its live session: %v", s.SeatSession)
	}
	if _, ok := s.SeatSession["/gone|8@alpha"]; ok {
		t.Fatal("the membership of a session that is not running names no seat")
	}
	if _, recorded := s.SeatRestore[h.Checkout()+"|7@alpha"]; recorded {
		t.Fatal("a seat no record names has no restore flag")
	}
}

// Fable's M6 review B1: a plain cc running under the tmux name of an ended cc --teams finds its
// membership left behind; its own launch rewrote the record without --teams or --bus, so that
// membership names no seat.
func TestCollectSessionsSkipsAMembershipLeftUnderAReusedName(t *testing.T) {
	h := populated(t)
	runtime := t.TempDir()
	h.Env["XDG_RUNTIME_DIR"] = runtime
	h.On("tmux -L ccy list-sessions", "ccy-proj 0 "+h.Checkout()+"\ncc-proj 0 "+h.Checkout()+"\n", 0)
	teamtest.MustWrite(t, filepath.Join(runtime, "pingbus", "membership", "cc-proj", "membership.json"),
		`{"v":1,"session":"cc-proj","kind":"cc","checkout":"`+h.Checkout()+`","generation":1,"written_ms":1,"seats":[{"team":"alpha","seat":"9"}],"farewell":[]}`)
	teamtest.MustWrite(t, filepath.Join(h.Root, "home/.local/state/ccy/sessions/cc-proj"),
		"ccy-session-record 1\nname=cc-proj\ndir="+h.Checkout()+"\nlauncher=/x/cc\nprefix=cc\nrestore=yes\narg=--model\narg=x\n")
	s := NewSnapshot()
	if err := h.c.collectSessions(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	if got, ok := s.SeatSession[h.Checkout()+"|9@alpha"]; ok {
		t.Fatalf("a membership left under a plain cc's name names seat 9 for %q", got)
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

func TestCollectMembersKeepsEachKnownSessionCount(t *testing.T) {
	h := populated(t)
	s := NewSnapshot()
	h.c.collectMembers(t.Context(), s, "alpha")
	want := map[string]int{"alpha|owner": 0, "alpha|guest": 2, "alpha|ghost": 1}
	if !reflect.DeepEqual(s.Sessions, want) {
		t.Fatalf("sessions %v", s.Sessions)
	}
	if n, known := s.SessionsOf("alpha", "guest"); !known || n != 2 {
		t.Fatalf("guest: %d %v", n, known)
	}

	h = populated(t)
	h.OnErr(teamtest.SudoBus+"human devices alpha owner", "sudo: a password is required", 1)
	s = NewSnapshot()
	h.c.collectMembers(t.Context(), s, "alpha")
	if _, known := s.SessionsOf("alpha", "owner"); known {
		t.Fatal("a count that could not be read is not a count of none")
	}
	again := NewSnapshot()
	h.c.collectMembers(t.Context(), again, "alpha")
	if n, known := again.SessionsOf("alpha", "guest"); !known || n != 2 {
		t.Fatalf("a kept read still counts: %d %v", n, known)
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
		{Team: "alpha", Member: "(no description)", Kind: KindTeam, State: "up", Applied: "up to date", Session: "-", Restore: "-", Where: "port 6167", Handle: "-", Dir: "-"},
		{Team: "alpha", Member: "1", Kind: "ccy", State: "active", Applied: "-", Session: "ccy-proj", Record: "ccy-proj", Restore: "yes", Where: shown, Handle: "proj.1+local.podman", Dir: checkout, Role: "dev", Held: true},
		{Team: "alpha", Member: "dev", Kind: "ccy", State: "unknown", Applied: "-", Session: "free", Restore: "-", Where: shown, Handle: "proj.dev+local.podman", Dir: checkout},
		{Team: "alpha", Member: "proj.2+local.podman", Kind: "ccy", State: "parked", Applied: "-", Session: "-", Restore: "-", Where: "dev", Handle: "proj.2+local.podman", Dir: "-", Role: "dev"},
		{Team: "alpha", Member: "proj.box+role.lxc", Kind: "lxc", State: "active", Applied: "-", Session: "-", Restore: "-", Where: "ops", Handle: "proj.box+role.lxc", Dir: "-", Role: "ops"},
		{Team: "alpha", Member: "owner", Kind: "human", State: "in room", Applied: "-", Session: "never signed in", Restore: "-", Where: "Element; Enter for choices", Handle: "-", Dir: "-"},
		{Team: "alpha", Member: "guest", Kind: "human", State: "invited", Applied: "-", Session: "2 signed in", Restore: "-", Where: "Element; Enter for choices", Handle: "-", Dir: "-"},
		{Team: "alpha", Member: "ghost", Kind: "human", State: "not in room", Applied: "-", Session: "1 signed in", Restore: "-", Where: "Element; Enter for choices", Handle: "-", Dir: "-"},
	}
	if !reflect.DeepEqual(s.Rows, want) {
		for i := range s.Rows {
			t.Logf("got  %+v", s.Rows[i])
		}
		t.Fatalf("rows differ from the %d expected", len(want))
	}
}

func TestATeamRowsMemberCellIsItsDescription(t *testing.T) {
	h := populated(t)
	h.WriteHostVars(t, teamtest.HostVarsAlpha+"    description: Builds the example site\n")
	s, err := h.c.Collect(t.Context())
	if err != nil {
		t.Fatalf("%v: %s", err, h.ErrOut.String())
	}
	if got := s.Rows[0]; got.Kind != KindTeam || got.Member != "Builds the example site" {
		t.Fatalf("team row %+v", got)
	}
	if text := Layout(s.Rows, 0).Text(s.Rows[0]); !strings.Contains(text, "Builds the example site") {
		t.Fatalf("the plain row shows it: %q", text)
	}
}

func TestAVaultedOrInvalidDescriptionIsSaidNotShownAsNone(t *testing.T) {
	for value, want := range map[string]string{"!vault x": "(description vaulted)", `""`: "(description invalid)"} {
		h := populated(t)
		h.WriteHostVars(t, teamtest.HostVarsAlpha+"    description: "+value+"\n")
		s, err := h.c.Collect(t.Context())
		if err != nil {
			t.Fatalf("%v: %s", err, h.ErrOut.String())
		}
		if got := s.Rows[0].Member; got != want {
			t.Fatalf("%s: team row says %q, want %q", value, got, want)
		}
	}
}

func TestCollectGivesASeatRowItsMembersRole(t *testing.T) {
	h := populated(t)
	h.On(teamtest.SudoBus+"list alpha", strings.Replace(teamtest.BusList,
		"MEMBER\tproj.1+local.podman\tdev", "MEMBER\tproj.1+local.podman\tcoordinator", 1), 0)
	s, err := h.c.Collect(t.Context())
	if err != nil {
		t.Fatalf("%v: %s", err, h.ErrOut.String())
	}
	if got := s.Rows[1].Role; got != "coordinator" {
		t.Fatalf("seat 1 has role %q", got)
	}
	table := Layout(s.Rows, 0)
	if text := table.Text(s.Rows[1]); !strings.Contains(text, "alpha  proj.1+local.podman (coordinator)  ") {
		t.Fatalf("the seat row does not show its handle and that it is the coordinator: %q", text)
	}
	if text := table.Text(s.Rows[2]); strings.Contains(text, "coordinator") || !strings.Contains(text, "alpha  proj.dev+local.podman ") {
		t.Fatalf("a seat that is not the coordinator: %q", text)
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
	// beta is on no address (it listens on nothing and there is no bus address): no URL.
	h.WriteHostVars(t, "agent_bus_teams:\n  - team: alpha\n    port: 6167\n    listen: [\"192.0.2.10\"]\n  - team: beta\n    port: 6168\n")
	h.OnDeclare("status", "STATUS\talpha\tdecide\tabsent\nSTATUS\tbeta\tunbuilt\t-\nSTATUS\tgamma\tunreadable\tbad\n")
	h.On("systemctl list-units", "agent-bus-hs@alpha.service loaded inactive dead x\nagent-bus-hs@gamma.service loaded failed failed x\n", 0)
	h.On("tmux -L ccy list-sessions", "", 0)
	h.OnNotARepository()
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
	// An installed team with no health record says so (Plan 00203); one not installed has none to have.
	never := func(team string) string {
		return ", health: never checked: ccy-teams apply " + team + " starts agent-bus-hs-health@" + team + ".timer"
	}
	want := []string{
		"alpha|down (inactive)|decide: absent|port 6167, members: homeserver down" + never("alpha"),
		"beta|not installed|not built|port 6168, install it: ccy-teams apply beta",
		"gamma|down (failed)|unreadable|port unknown (not declared), members: homeserver down" + never("gamma") +
			", entry unreadable: bad",
	}
	if !reflect.DeepEqual(wheres, want) {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(wheres, "\n"), strings.Join(want, "\n"))
	}
}

func TestCollectNotInstalledUndeclaredPointsAtRemove(t *testing.T) {
	h := newRig(t)
	h.WriteHostVars(t, "")
	h.OnDeclare("status", "STATUS\tzeta\tdecide\tundeclared\n")
	h.On("systemctl list-units", "", 0)
	h.On("tmux -L ccy list-sessions", "", 0)
	h.OnNotARepository()
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
	h.WriteHostVars(t, "")
	h.OnDeclare("status", "")
	h.On("systemctl list-units", "", 0)
	s, err := h.c.Collect(t.Context())
	if err != nil || len(s.Rows) != 0 {
		t.Fatalf("rows %v err %v", s.Rows, err)
	}
}

func TestCollectMarksUnreadableSeats(t *testing.T) {
	h := populated(t)
	h.BreakSeat(t, h.Checkout(), "alpha", "dev")
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
