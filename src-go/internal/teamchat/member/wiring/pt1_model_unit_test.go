//go:build busfailpoints

package wiring

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/failpoint"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/testfiles"
)

// PT1's pure half (SPEC-n3 PT1, section 11): the schedule a seed gives, the transcript read back
// into copies, and the P-checks over what a run saw. The real-process half is pt1_run_test.go.

// The schedule is a function of its seed alone, and keeps the bounds every run relies on.
func TestPT1ScheduleIsAFunctionOfItsSeed(t *testing.T) {
	for seed := uint64(1); seed <= 200; seed++ {
		ops := pt1Schedule(seed)
		if again := pt1Schedule(seed); !slices.Equal(ops, again) {
			t.Fatalf("seed %d gave two schedules: %v and %v", seed, ops, again)
		}
		if ops[0].kind != pt1Say {
			t.Fatalf("seed %d starts with %v: nothing would be owed", seed, ops[0])
		}
		if why := pt1Bounds(ops); why != "" {
			t.Fatalf("seed %d: %s: %v", seed, why, ops)
		}
	}
	if slices.Equal(pt1Schedule(1), pt1Schedule(2)) {
		t.Fatal("two seeds gave one schedule")
	}
}

// pt1Bounds is why a schedule breaks the bounds a run relies on, "" when it keeps them: its ops
// numbered by place, the inbox held only when it is free and released before the end, a refusal
// not after a hold, each kind within its cap, a removal only while the inbox is free and
// followed by nothing that needs the seat but a post (so P4 has an input), and an end or a death
// of CMD only as the last operation.
func pt1Bounds(ops []pt1Op) string {
	var s pt1Walk
	for i, op := range ops {
		if why := s.step(op, i, i == len(ops)-1); why != "" {
			return why
		}
	}
	switch {
	case s.held:
		return "it ends with the inbox held"
	case s.removed && !s.posted:
		return "the seat was removed and nothing was posted after it"
	}
	for kind, limit := range pt1Limits {
		if limit > 0 && s.counts[kind] > limit {
			return fmt.Sprintf("%d of %s, at most %d", s.counts[kind], pt1Names[kind], limit)
		}
	}
	return ""
}

// pt1Walk is pt1Bounds' state along a schedule: how many of each kind, whether the inbox is held
// or refused, whether the seat is removed, and whether a post has followed the removal.
type pt1Walk struct {
	counts                         [len(pt1Names)]int
	held, refused, removed, posted bool
}

// inboxBroken is whether op needs the inbox in a state it is not in: a hold while held or
// refused, a release while free, a removal while held.
func (s *pt1Walk) inboxBroken(op pt1Op) bool {
	return op.kind == pt1Hold && (s.held || s.refused) || op.kind == pt1Release && !s.held || op.kind == pt1Remove && s.held
}

// step is why op, the i-th of the schedule, breaks a bound, "" when it keeps them; it then takes
// op into the state.
func (s *pt1Walk) step(op pt1Op, i int, last bool) string {
	s.counts[op.kind]++
	switch {
	case op.n != i:
		return fmt.Sprintf("%v is not numbered %d", op, i)
	case s.inboxBroken(op):
		return fmt.Sprintf("%v with the inbox held %v and refused %v", op, s.held, s.refused)
	case s.removed && !slices.Contains(pt1PostRemoval, op.kind) && !op.kind.terminal():
		return fmt.Sprintf("%v after the seat was removed, which needs the seat", op)
	case op.kind.terminal() && !last:
		return fmt.Sprintf("%v is an ending and is not last", op)
	}
	s.held = op.kind == pt1Hold || s.held && op.kind != pt1Release
	s.refused = s.refused || op.kind == pt1Refuse
	s.posted = s.posted || s.removed && (op.kind == pt1Say || op.kind == pt1Peer)
	s.removed = s.removed || op.kind == pt1Remove
	return ""
}

// Over many seeds every operation is drawn: a kind no seed reaches would be a harness that
// never interleaves it.
func TestPT1ScheduleDrawsEveryOperation(t *testing.T) {
	drawn := map[pt1Kind]bool{}
	for seed := uint64(1); seed <= 200; seed++ {
		for _, op := range pt1Schedule(seed) {
			drawn[op.kind] = true
		}
	}
	for kind, name := range pt1Names {
		if !drawn[pt1Kind(kind)] {
			t.Errorf("no seed of 200 draws %s", name)
		}
	}
}

// The gate's fixed seeds draw every operation between them, so that the list cannot shrink to a
// set that never holds the inbox, restarts the homeserver, kills the link, removes the seat or
// ends the session; and the first eight, the point rows' seeds, draw none of the removal and the
// endings, so that a row's armed failpoint is the only fault of its run beside the schedule's.
func TestPT1FixedSeedsDrawEveryOperation(t *testing.T) {
	drawn := map[pt1Kind]bool{}
	for i, seed := range pt1Fixed {
		for _, op := range pt1Schedule(seed) {
			drawn[op.kind] = true
			if i < len(pt1Provokers) && (op.kind == pt1Remove || op.kind.terminal()) {
				t.Errorf("fixed seed %d is a point row's and draws %v", seed, op)
			}
		}
	}
	for kind, name := range pt1Names {
		if !drawn[pt1Kind(kind)] {
			t.Errorf("the fixed seeds %v never draw %s", pt1Fixed, name)
		}
	}
}

// failpointNames are the names failpoint/names.go declares, read from its source: the one list.
func failpointNames(t *testing.T) []failpoint.Name {
	t.Helper()
	src, err := testfiles.Try("../../failpoint", "names.go")
	check(t, err)
	file, err := parser.ParseFile(token.NewFileSet(), "names.go", src, 0)
	check(t, err)
	var names []failpoint.Name
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			val, ok := spec.(*ast.ValueSpec)
			if !ok || len(val.Values) != 1 {
				t.Fatalf("names.go: a constant that is not one name and one value: %v", spec)
			}
			lit, ok := val.Values[0].(*ast.BasicLit)
			if !ok {
				t.Fatalf("names.go: %v is not a string literal", val.Names)
			}
			name, err := strconv.Unquote(lit.Value)
			check(t, err)
			names = append(names, failpoint.Name(name))
		}
	}
	return names
}

// Every failpoint the code names is either made to fire by a row of TestPT1Points or has its
// reason in pt1Unreached: a point added to names.go with no decision here fails, so the list PT1
// covers cannot fall behind the list the code has.
func TestPT1EveryFailpointIsReachedOrSaidWhy(t *testing.T) {
	names := failpointNames(t)
	if len(names) < 16 {
		t.Fatalf("names.go declares %d failpoints, fewer than the 16 known when PT1 was built", len(names))
	}
	for _, name := range names {
		_, reached := pt1Provokers[name]
		why, unreached := pt1Unreached[name]
		switch {
		case reached == unreached:
			t.Errorf("%s: reached by a row %v, unreached with a reason %v: it must be exactly one", name, reached, unreached)
		case unreached && why == "":
			t.Errorf("%s: unreached with no reason", name)
		}
	}
	if len(pt1Provokers)+len(pt1Unreached) != len(names) {
		t.Errorf("PT1 knows %d + %d failpoints, names.go has %d", len(pt1Provokers), len(pt1Unreached), len(names))
	}
}

// envelope is a delivery's text as proto.Delivery renders it, entries given as id and repeat.
func envelope(entries ...pt1Copy) string {
	lines := []string{"teamchat delivery 0a1b2c3d: team alpha, 1 item"}
	for i, e := range entries {
		marker := "0a1b2c3d " + strconv.Itoa(i+1) + "/" + strconv.Itoa(len(entries)) + ">>"
		lines = append(lines, "<<teamchat "+marker, "kind: peer", "id: "+e.id)
		if e.repeat {
			lines = append(lines, "repeat: this may be a second copy of an earlier delivery; the id is the same")
		}
		lines = append(lines, "body: hello", "<<end teamchat "+marker)
	}
	return strings.Join(lines, "\n")
}

// pt1Line is the transcript line of an idle user turn holding content, as faketranscript writes it.
func pt1Line(content string) string {
	data, err := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}})
	if err != nil {
		panic(err)
	}
	return string(data)
}

func TestPT1ReadsTheTranscriptBackIntoCopies(t *testing.T) {
	first := pt1Line(envelope(pt1Copy{id: "$a"}, pt1Copy{id: "$b"}))
	again := pt1Line(envelope(pt1Copy{id: "$a", repeat: true}))
	got, err := pt1Copies(first + "\n" + again + "\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []pt1Copy{{id: "$a"}, {id: "$b"}, {id: "$a", repeat: true}}
	if !slices.Equal(got, want) {
		t.Fatalf("copies %v, want %v", got, want)
	}
}

func TestPT1TranscriptLinesThatAreNoUserTurnAreNoCopies(t *testing.T) {
	got, err := pt1Copies(`{"type":"assistant","message":{"role":"assistant","content":"id: $x"}}` + "\n\n")
	if err != nil || len(got) != 0 {
		t.Fatalf("copies %v, err %v", got, err)
	}
	if _, err := pt1Copies("not json\n"); err == nil {
		t.Fatal("a line that is not JSON was read")
	}
}

// clean is what a run that went right saw: two items, one recv'd and one delivered, a send in
// main once, one link, and a session that ended with its goodbye posted once.
func clean() pt1Seen {
	return pt1Seen{
		posted:     []string{"$a", "$b"},
		recv:       map[string]int{"$a": 1},
		transcript: []pt1Copy{{id: "$b"}},
		linksAlive: 1,
		notices:    map[string]int{"joined": 1, "left other": 1},
		want:       "left other",
		sends:      []pt1Sent{{text: "t1", answered: true}, {text: "t2"}},
		inMain:     map[string]int{"t1": 1},
	}
}

// removedRun is clean after the seat was released: the leave posted once, the transcript as it
// was at the removal, and one item posted after it that no channel showed.
func removedRun() pt1Seen {
	o := clean()
	o.removed, o.atRemoval = true, len(o.transcript)
	o.afterRemoval = []string{"$c"}
	o.notices, o.want = map[string]int{"joined": 1, "left released": 1}, "left released"
	return o
}

// pt1Case is one observation a check is shown on: how it differs from clean, and the property its
// one finding names, "" for none.
type pt1Case struct {
	name   string
	change func(*pt1Seen)
	want   string
}

// pt1RunCases judges each case: a check that fires on a clean run, or on the wrong property, is
// as wrong as one that never fires.
func pt1RunCases(t *testing.T, cases []pt1Case) {
	t.Helper()
	for _, c := range cases {
		o := clean()
		c.change(&o)
		got := pt1Check(o)
		switch {
		case c.want == "" && len(got) != 0:
			t.Errorf("%s: unexpected findings %v", c.name, got)
		case c.want != "" && (len(got) != 1 || !strings.HasPrefix(got[0], c.want)):
			t.Errorf("%s: findings %v, want one for %s", c.name, got, c.want)
		}
	}
}

// P4 after a removal, and P2 for an item a failed recv handed back.
func TestPT1RemovalAndReturnedItemChecks(t *testing.T) {
	pt1RunCases(t, []pt1Case{
		{"a removed run", func(o *pt1Seen) { *o = removedRun() }, ""},
		{"an item posted after the removal that reached the transcript", func(o *pt1Seen) {
			*o = removedRun()
			o.transcript = append(o.transcript, pt1Copy{id: "$c"})
		}, "P4"},
		{"the store written again after the keeper deleted it", func(o *pt1Seen) {
			*o = removedRun()
			o.storeBack = true
		}, "P4"},
		{"an item posted after the removal that recv printed", func(o *pt1Seen) {
			*o = removedRun()
			o.recv["$c"] = 1
		}, "P4"},
		{"a copy after the removal of an item posted before it", func(o *pt1Seen) {
			*o = removedRun()
			o.transcript = append(o.transcript, pt1Copy{id: "$a", repeat: true})
		}, "P4"},
		{"an item a failed recv handed back, delivered with no repeat", func(o *pt1Seen) {
			delete(o.recv, "$a")
			o.marked = map[string]int{"$a": 0}
			o.transcript = append(o.transcript, pt1Copy{id: "$a"})
		}, "P2"},
		{"the same, delivered with the repeat mark", func(o *pt1Seen) {
			delete(o.recv, "$a")
			o.marked = map[string]int{"$a": 0}
			o.transcript = append(o.transcript, pt1Copy{id: "$a", repeat: true})
		}, ""},
		{"an item delivered before a failed recv handed it back, and again with the mark", func(o *pt1Seen) {
			delete(o.recv, "$a")
			o.marked = map[string]int{"$a": 1}
			o.transcript = append(o.transcript, pt1Copy{id: "$a"}, pt1Copy{id: "$a", repeat: true})
		}, ""},
	})
}

// Each property fails on the observation that breaks it and on that alone.
func TestPT1ChecksFireOnTheirOwnProperty(t *testing.T) {
	pt1RunCases(t, []pt1Case{
		{"clean", func(*pt1Seen) {}, ""},
		{"an accepted item no channel ever showed", func(o *pt1Seen) { delete(o.recv, "$a") }, "P1"},
		{"an accepted item in an awaiting entry, in limbo until missing", func(o *pt1Seen) {
			delete(o.recv, "$a")
			o.awaiting = []string{"$a"}
		}, ""},
		{"a second transcript copy not marked repeat", func(o *pt1Seen) {
			o.transcript = append(o.transcript, pt1Copy{id: "$b"})
		}, "P2"},
		{"a second transcript copy marked repeat", func(o *pt1Seen) {
			o.transcript = append(o.transcript, pt1Copy{id: "$b", repeat: true})
		}, ""},
		{"a recv'd item delivered again with no mark and no crash", func(o *pt1Seen) {
			o.transcript = append(o.transcript, pt1Copy{id: "$a"})
		}, "P11"},
		{"the same, after a crash that explains one copy", func(o *pt1Seen) {
			o.transcript = append(o.transcript, pt1Copy{id: "$a"})
			o.crashes = 1
		}, ""},
		{"an item recv'd twice with no crash", func(o *pt1Seen) { o.recv["$a"] = 2 }, "P11"},
		{"an answered send that never reached main", func(o *pt1Seen) { o.inMain["t1"] = 0 }, "P1 send"},
		{"a send posted twice", func(o *pt1Seen) { o.inMain["t1"] = 2 }, "P1 send"},
		{"a send that failed and was posted once", func(o *pt1Seen) { o.inMain["t2"] = 1 }, ""},
		{"a send that failed and was posted twice", func(o *pt1Seen) { o.inMain["t2"] = 2 }, "P1 send"},
		{"a goodbye posted twice", func(o *pt1Seen) { o.notices["left other"] = 2 }, "P5"},
		{"a disconnected posted twice", func(o *pt1Seen) {
			o.want, o.notices = "disconnected", map[string]int{"disconnected": 2}
		}, "P5"},
		{"a goodbye of two reasons", func(o *pt1Seen) { o.notices["left released"] = 1 }, "P5"},
		{"the end's goodbye never posted", func(o *pt1Seen) { delete(o.notices, "left other") }, "P5"},
		{"a goodbye in a run that did not end", func(o *pt1Seen) { o.want = "" }, "P5"},
		{"a run that did not end and posted nothing", func(o *pt1Seen) {
			o.want, o.notices = "", map[string]int{"joined": 1}
		}, ""},
		{"a join told twice is no goodbye", func(o *pt1Seen) { o.notices["joined"] = 2 }, ""},
		{"two links alive", func(o *pt1Seen) { o.linksAlive = 2 }, "P6"},
	})
}
