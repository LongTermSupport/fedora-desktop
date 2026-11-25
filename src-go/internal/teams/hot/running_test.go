package hot

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/duty"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Register R51: every host reader of a running session's membership finds it through one lookup,
// v1's membership when the session has one, else its teamchat (v2) desired.json.

// The lookup: v1 when the session has a v1 membership directory, else v2 when its desired.json
// is found, else none; a record whose launch left the bus out is none whatever is under its name.
func TestTheLookupIsV1ElseV2ElseNone(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	rec, err := r.store.Get(session)
	if err != nil {
		t.Fatal(err)
	}
	opened := 0
	r.x.Teamchat = func(string, string) (TeamchatSeats, bool, error) {
		opened++
		return &fakeV2{t: t}, true, nil
	}
	run, err := r.x.Lookup().Running(r.runtime, membership.KindCcy, rec)
	if err != nil || run.Bus != BusV1 || run.Dir != r.dir() || run.V1.Generation != 1 || opened != 0 {
		t.Fatalf("v1: %+v %v, teamchat opened %d", run, err, opened)
	}
	if err = os.RemoveAll(r.dir()); err != nil {
		t.Fatal(err)
	}
	if run, err = r.x.Lookup().Running(r.runtime, membership.KindCcy, rec); err != nil || run.Bus != BusV2 || run.V2 == nil || run.Dir != r.dir() {
		t.Fatalf("v2: %+v %v", run, err)
	}
	r.x.Teamchat = func(string, string) (TeamchatSeats, bool, error) { return nil, false, nil }
	if run, err = r.x.Lookup().Running(r.runtime, membership.KindCcy, rec); err != nil || run.Bus != BusNone {
		t.Fatalf("none: %+v %v", run, err)
	}
	r.x.Teamchat = func(string, string) (TeamchatSeats, bool, error) { return nil, false, errors.New("HOME is not set") }
	if run, err = r.x.Lookup().Running(r.runtime, membership.KindCcy, rec); err == nil || run.Bus != BusV2 {
		t.Fatalf("v2 unreadable: %+v %v", run, err)
	}
	if run, err = (Lookup{}).Running(r.runtime, membership.KindCcy, rec); err != nil || run.Bus != BusNone {
		t.Fatalf("no teamchat opener: %+v %v", run, err)
	}
	rec.Args = []string{"--no-bus"}
	if run, err = r.x.Lookup().Running(r.runtime, membership.KindCcy, rec); err != nil || run.Bus != BusNone {
		t.Fatalf("--no-bus: %+v %v", run, err)
	}
}

// A v1 membership directory whose file is unreadable is v1's error, and one with no file is none.
func TestTheLookupOfABrokenV1Membership(t *testing.T) {
	r := newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
	rec, err := r.store.Get(session)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(r.dir(), "membership.json")
	if err = os.WriteFile(file, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if run, readErr := r.x.Lookup().Running(r.runtime, membership.KindCcy, rec); readErr == nil || run.Bus != BusV1 {
		t.Fatalf("unreadable: %+v %v", run, readErr)
	}
	if err = os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if run, readErr := r.x.Lookup().Running(r.runtime, membership.KindCcy, rec); readErr != nil || run.Bus != BusNone {
		t.Fatalf("no file: %+v %v", run, readErr)
	}
}

// R58, seen live: `session list` showed a running v2 session as "old or --no-bus" while `session
// add` on it worked. It is now a row of its teams, seats and duties, controllable, with a STATE
// from the keeper's reports of its seats, never v1's keeper process.
func TestListShowsARunningV2Session(t *testing.T) {
	r, v2 := newV2Rig(t)
	r.labelled("abc123\trunning\t" + session + "\n")
	v2.desired.Seats[0].Duty = duty.Coordinator
	r.m.alive = false // v1's keeper process: a v2 row must not read it
	if code := r.run(t, "list"); code != term.ExitOK {
		t.Fatalf("exit %d:\n%s", code, r.ErrOut.String())
	}
	if want := "SESSION\t" + session + "\tccy\t" + r.Checkout() + "\talpha/1/coordinator\tok\n"; r.Out.String() != want {
		t.Fatalf("stdout\n%q\nwant\n%q", r.Out.String(), want)
	}
	got, err := r.x.Sessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := Session{
		Name: session, Kind: "ccy", Checkout: r.Checkout(), State: "ok", Controllable: true, V2: true,
		Teams: []Cell{{Team: "alpha", Seat: "1", Role: "coordinator"}},
	}
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("sessions %+v", got)
	}

	v2.desired.Seats[0].Duty = ""
	v2.applied = false
	if code := r.run(t, "list"); code != term.ExitOK ||
		!strings.HasSuffix(r.Out.String(), "\talpha/1/-\t1@alpha not held: the session's keeper has written no report of the seat\n") {
		t.Fatalf("not held: exit %d: %q", code, r.Out.String())
	}
	v2.appErr = errors.New("the seat lock of alpha: permission denied")
	if code := r.run(t, "list"); code != term.ExitOK ||
		!strings.HasSuffix(r.Out.String(), "\tits seats cannot be read: the seat lock of alpha: permission denied\n") {
		t.Fatalf("unreadable seat: exit %d: %q", code, r.Out.String())
	}
}

// A running session with neither bus's membership is still "old or --no-bus"; one whose v2
// desired membership cannot be read says so, naming teamchat.
func TestListOfASessionWithNoV2MembershipOrAnUnreadableOne(t *testing.T) {
	r := v2RigOf(newV2Rig(t))
	r.labelled("abc123\trunning\t" + session + "\n")
	r.x.Teamchat = func(string, string) (TeamchatSeats, bool, error) { return nil, false, nil }
	if code := r.run(t, "list"); code != term.ExitOK ||
		r.Out.String() != "SESSION\t"+session+"\tccy\t-\t-\told or --no-bus: relaunch it to change it while it runs\n" {
		t.Fatalf("none: exit %d: %q", code, r.Out.String())
	}
	r.x.Teamchat = func(string, string) (TeamchatSeats, bool, error) { return nil, false, errors.New("HOME is not set") }
	if code := r.run(t, "list"); code != term.ExitOK ||
		!strings.HasSuffix(r.Out.String(), "\tits teamchat (v2) membership cannot be read: HOME is not set\n") {
		t.Fatalf("unreadable: exit %d: %q", code, r.Out.String())
	}
}

// A running cc session launched on teamchat is listed (it was invisible), with its teams, and
// as one that is not changed while it runs, naming why.
func TestListShowsACCSessionOnTeamchat(t *testing.T) {
	r := newCCRig(t, membership.Seat{Team: "alpha", Seat: "1"})
	if err := os.RemoveAll(r.dir()); err != nil {
		t.Fatal(err)
	}
	r.labelled("")
	v2 := &fakeV2{t: t, applied: true, desired: state.Desired{Seats: []state.DesiredSeat{{Team: mustTeam(t, "alpha"), Seat: "1"}}}}
	r.x.Teamchat = func(checkout, name string) (TeamchatSeats, bool, error) {
		if checkout != r.Checkout() || name != ccSession {
			t.Errorf("opened %s in %s", name, checkout)
		}
		return v2, true, nil
	}
	got, err := r.x.Sessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := Session{
		Name: ccSession, Kind: "cc", Checkout: r.Checkout(), State: v2CCNotBuilt, V2: true,
		Teams: []Cell{{Team: "alpha", Seat: "1", Role: "-"}},
	}
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("sessions %+v\nwant %+v", got, want)
	}
	// Its pane gone, it is not running, so not listed.
	r.m.pane = 0
	if got, err = r.x.Sessions(t.Context()); err != nil || len(got) != 0 {
		t.Fatalf("not running: %+v %v", got, err)
	}
}

// SessionsIn serves the take-down of an agent-bus (v1) team (ops.Remove, H29): a v2 session is
// on teamchat teams, never in a v1 one, so it is not named even in a v2 team of the same name.
func TestSessionsInNamesOnlyV1SessionsOfTheTeam(t *testing.T) {
	r, v2 := newV2Rig(t)
	r.labelled("abc123\trunning\t" + session + "\n")
	in, err := r.x.SessionsIn(t.Context(), "alpha")
	if err != nil || len(in) != 0 || len(v2.calls) != 0 {
		t.Fatalf("in alpha: %v %v, calls %v", in, err, v2.calls)
	}
}

// v2RigOf is the rig of newV2Rig, for a test that changes nothing of its fake.
func v2RigOf(r *rig, v2 *fakeV2) *rig {
	if v2 == nil {
		panic("newV2Rig made no fake")
	}
	return r
}

// Parity: `session add` (resolve) and `session list` (Sessions) agree on whether a session can be
// changed while it runs, for a v1 session, a v2 session and one with neither (R58).
func TestAddAndListAgreeOnWhetherASessionCanBeChanged(t *testing.T) {
	for name, setup := range map[string]func(*testing.T) *rig{
		"v1": func(t *testing.T) *rig {
			t.Helper()
			return newRig(t, membership.Seat{Team: "alpha", Seat: "3"})
		},
		"v2": func(t *testing.T) *rig {
			t.Helper()
			return v2RigOf(newV2Rig(t))
		},
		"none": func(t *testing.T) *rig {
			t.Helper()
			r := v2RigOf(newV2Rig(t))
			r.x.Teamchat = nil
			return r
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := setup(t)
			r.labelled("abc123\trunning\t" + session + "\n")
			rows, err := r.x.Sessions(t.Context())
			if err != nil || len(rows) != 1 {
				t.Fatalf("rows %+v %v", rows, err)
			}
			code := r.run(t, "add", session, "beta", "--yes")
			changed := code != exitConfig
			if rows[0].Controllable != changed {
				t.Fatalf("list says controllable %v, add exits %d:\n%s", rows[0].Controllable, code, r.ErrOut.String())
			}
		})
	}
}

// v1Readers is every call outside the lookup that reads a running session's v1 membership (its
// directory, its desired file, the keeper's report), with why it may: each acts only on a v1
// session the lookup already found, or reads v1's own seats, which a v2 session never holds.
// A new reader of these fails TestEveryVerbOnAV2SessionActsOrRefusesByName until it goes
// through the lookup or is listed here with its reason (register R51).
var v1Readers = map[string]string{
	"teams/hot/running.go membership.DirOf":            "the lookup itself",
	"teams/hot/running.go membership.Read":             "the lookup itself",
	"teams/hot/cc.go membership.CCDir":                 "isCC: locate finds a v1 cc session by its pane; a cc session on teamchat is refused by name (ccOnTeamchat)",
	"teams/hot/cc.go discover.RunningCC":               "the v1 cc sessions; Sessions finds a cc session on teamchat from its record through the lookup",
	"teams/hot/add.go discover.ReadDesired":            "claims on agent-bus seats; a v2 session holds none",
	"teams/hot/add.go membership.ReadReport":           "v1 add, after the lookup found a v1 session",
	"teams/hot/role.go membership.ReadReport":          "v1 role, after the lookup found a v1 session; a v2 session's role is refused by name",
	"teams/hot/list.go membership.ReadReport":          "a v1 row, after the lookup found a v1 session",
	"teams/board/collect.go discover.ReadDesired":      "the agent-bus (v1) seat index; a v2 team's rows come from its status record (board/v2.go)",
	"conversation/seatrecord.go membership.ReadReport": "sessionSeats: v1's report only after the v2 desired.json was read",
	"conversation/seats.go membership.ReadReport":      "v1Sessions; v2Sessions reads the seat store beside it",
}

// v1ReaderNames are the calls that read a session's v1 membership.
var v1ReaderNames = map[string][]string{
	"membership": {"Read", "DirOf", "Dir", "CCDir", "ReadReport"},
	"discover":   {"ReadDesired", "RunningCC"},
}

// v1ReaderCalls is every call of v1ReaderNames in the non-test Go files under internal/teams and
// internal/conversation (the membership and discover packages, which define them, left out), as
// "<path from internal/> <package>.<name>".
func v1ReaderCalls(t *testing.T) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	internal := filepath.Join("..", "..")
	for _, top := range []string{"teams", "conversation"} {
		err := filepath.WalkDir(filepath.Join(internal, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, relErr := filepath.Rel(internal, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			switch {
			case d.IsDir() && (rel == "teams/membership" || rel == "teams/discover"):
				return filepath.SkipDir
			case d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go"):
				return nil
			}
			return v1ReaderCallsIn(path, rel, found)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return found
}

// v1ReaderCallsIn notes each call of v1ReaderNames in the Go file at path (rel from internal/).
func v1ReaderCallsIn(path, rel string, found map[string]bool) error {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return err
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel {
			if pkg, isIdent := sel.X.(*ast.Ident); isIdent && slices.Contains(v1ReaderNames[pkg.Name], sel.Sel.Name) {
				found[rel+" "+pkg.Name+"."+sel.Sel.Name] = true
			}
		}
		return true
	})
	return nil
}

// guardV1ReaderCalls is the class guard's inventory: no call reads a running session's v1
// membership outside the lookup unless v1Readers says why it may, and no entry is stale.
func guardV1ReaderCalls(t *testing.T) {
	t.Helper()
	found := v1ReaderCalls(t)
	for call := range found {
		if _, ok := v1Readers[call]; !ok {
			t.Errorf("%s reads a running session's v1 membership outside the lookup (Lookup.Running): "+
				"read the session through the lookup, or list it in v1Readers with why it is v1's alone", call)
		}
	}
	for _, call := range slices.Sorted(maps.Keys(v1Readers)) {
		if !found[call] {
			t.Errorf("v1Readers lists %s, which no longer reads it: remove the entry", call)
		}
	}
}

// The inventory finds a reader where one is: a sanity check that the walk reads the tree.
func TestTheV1ReaderInventoryFindsTheLookup(t *testing.T) {
	if found := v1ReaderCalls(t); !found["teams/hot/running.go membership.Read"] || !found["teams/board/collect.go discover.ReadDesired"] {
		t.Fatalf("found %v", slices.Sorted(maps.Keys(found)))
	}
}

// guardV2Readers is the readers' half of the class guard: list, the picker's view (Sessions),
// SessionsIn and TakeOut on a v2 session each act on it or refuse it by name.
func guardV2Readers(t *testing.T) {
	t.Helper()
	r, v2 := newV2Rig(t)
	r.labelled("abc123\trunning\t" + session + "\n")
	if code := r.run(t, "list"); code != term.ExitOK || strings.Contains(r.Out.String(), "old or --no-bus") ||
		!strings.Contains(r.Out.String(), "\talpha/1/") {
		t.Fatalf("list: exit %d: %q", code, r.Out.String())
	}
	rows, err := r.x.Sessions(t.Context())
	if err != nil || len(rows) != 1 || !rows[0].Controllable || !rows[0].V2 {
		t.Fatalf("Sessions: %+v %v", rows, err)
	}
	if in, inErr := r.x.SessionsIn(t.Context(), "alpha"); inErr != nil || len(in) != 0 {
		t.Fatalf("SessionsIn: %v %v", in, inErr)
	}
	if r.x.TakeOut(t.Context(), session, "alpha") || !strings.Contains(r.ErrOut.String(), "teamchat") || len(v2.calls) != 0 {
		t.Fatalf("TakeOut acted on a v2 session as v1's:\n%s", r.ErrOut.String())
	}
	if _, statErr := os.Stat(r.dir()); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a v1 membership was written: %v", statErr)
	}
}
