//go:build busfailpoints

package wiring

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
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
// not after a hold, and each kind within its cap.
func pt1Bounds(ops []pt1Op) string {
	counts := make([]int, len(pt1Names))
	held, refused := false, false
	for i, op := range ops {
		counts[op.kind]++
		switch {
		case op.n != i:
			return fmt.Sprintf("%v is not numbered %d", op, i)
		case op.kind == pt1Hold && (held || refused), op.kind == pt1Release && !held:
			return fmt.Sprintf("%v with the inbox held %v and refused %v", op, held, refused)
		}
		held = op.kind == pt1Hold || held && op.kind != pt1Release
		refused = refused || op.kind == pt1Refuse
	}
	if held {
		return "it ends with the inbox held"
	}
	for kind, limit := range pt1Limits {
		if limit > 0 && counts[kind] > limit {
			return fmt.Sprintf("%d of %s, at most %d", counts[kind], pt1Names[kind], limit)
		}
	}
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

// The gate's fixed seeds draw every operation between them, so that the list cannot shrink to
// a set that never holds the inbox, restarts the homeserver or kills the link.
func TestPT1FixedSeedsDrawEveryOperation(t *testing.T) {
	drawn := map[pt1Kind]bool{}
	for _, seed := range pt1Fixed {
		for _, op := range pt1Schedule(seed) {
			drawn[op.kind] = true
		}
	}
	for kind, name := range pt1Names {
		if !drawn[pt1Kind(kind)] {
			t.Errorf("the fixed seeds %v never draw %s", pt1Fixed, name)
		}
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
// main once, one link and no notice.
func clean() pt1Seen {
	return pt1Seen{
		posted:     []string{"$a", "$b"},
		recv:       map[string]int{"$a": 1},
		transcript: []pt1Copy{{id: "$b"}},
		linksAlive: 1,
		notices:    map[string]int{"joined": 1},
		sends:      []pt1Sent{{text: "t1", answered: true}, {text: "t2"}},
		inMain:     map[string]int{"t1": 1},
	}
}

// Each property fails on the observation that breaks it and on that alone: a check that fires on
// a clean run, or on the wrong property, is as wrong as one that never fires.
func TestPT1ChecksFireOnTheirOwnProperty(t *testing.T) {
	cases := []struct {
		name   string
		change func(*pt1Seen)
		want   string
	}{
		{"clean", func(*pt1Seen) {}, ""},
		{"an accepted item no channel ever showed", func(o *pt1Seen) { delete(o.recv, "$a") }, "P1"},
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
		{"a disconnected posted twice", func(o *pt1Seen) { o.notices["disconnected"] = 2 }, "P5"},
		{"a join told twice is no goodbye", func(o *pt1Seen) { o.notices["joined"] = 2 }, ""},
		{"two links alive", func(o *pt1Seen) { o.linksAlive = 2 }, "P6"},
	}
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
