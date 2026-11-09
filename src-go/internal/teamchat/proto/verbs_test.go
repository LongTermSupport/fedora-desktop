package proto

import (
	"slices"
	"testing"
)

func TestVerbTableIsA81(t *testing.T) {
	type row struct {
		ref      Need
		forms    []string
		relation Need
		answer   bool
		senders  []SenderClass
		external bool
	}
	cw := []SenderClass{SenderCoordinator, SenderWorker}
	want := map[string]row{
		"fetch":   {Required, []string{RefPath, RefCommit}, Forbidden, true, []SenderClass{SenderCoordinator}, true},
		"sync":    {Required, []string{RefPath, RefCommit}, Forbidden, true, []SenderClass{SenderCoordinator}, false},
		"review":  {Required, []string{RefPath, RefCommit, RefPR}, Forbidden, true, cw, true},
		"run-qa":  {Required, []string{RefCommit, RefPR}, Forbidden, true, cw, false},
		"halt":    {Forbidden, nil, Forbidden, true, []SenderClass{SenderCoordinator}, false},
		"check":   {Forbidden, nil, Forbidden, true, []SenderClass{SenderProbe}, false},
		"ack":     {Forbidden, nil, Required, false, cw, false},
		"nack":    {Optional, nil, Required, false, cw, true},
		"done":    {Required, nil, Optional, false, cw, true},
		"blocked": {Required, nil, Optional, false, cw, true},
	}
	if len(Verbs) != len(want) {
		t.Fatalf("%d verbs", len(Verbs))
	}
	for name, w := range want {
		v, ok := Verbs[name]
		if !ok {
			t.Fatalf("verb %s missing", name)
		}
		got := row{v.Ref, v.Forms, v.Relation, v.ExpectsAnswer, v.Senders, v.External}
		if got.ref != w.ref || !slices.Equal(got.forms, w.forms) || got.relation != w.relation ||
			got.answer != w.answer || !slices.Equal(got.senders, w.senders) || got.external != w.external {
			t.Errorf("%s = %+v, want %+v", name, got, w)
		}
	}
}

func TestVerbRuleHelpers(t *testing.T) {
	if !Verbs["done"].AllowsForm(RefIssue) || Verbs["run-qa"].AllowsForm(RefPath) || Verbs["halt"].AllowsForm(RefCommit) {
		t.Fatal("AllowsForm wrong")
	}
	if !Verbs["review"].SentBy(SenderWorker) || Verbs["fetch"].SentBy(SenderWorker) || Verbs["ack"].SentBy(SenderProbe) {
		t.Fatal("SentBy wrong")
	}
	for v, want := range map[string]bool{"ack": true, "nack": true, "done": true, "blocked": true, "review": false, "check": false} {
		if IsAnswerVerb(v) != want {
			t.Errorf("IsAnswerVerb(%s) = %v", v, !want)
		}
	}
}

func TestAddressableFor(t *testing.T) {
	r := testRoster(t)
	r.Members[probeID] = RosterMember{Role: RoleProbe}
	cases := []struct {
		verb       string
		id         string
		probeCheck bool
		want       bool
	}{
		{"review", workerID, false, true},
		{"review", coordID, false, true},
		{"review", aliceID, false, false},
		{"review", parkedID, false, false},
		{"done", aliceID, false, true},
		{"ack", aliceID, false, true},
		{"check", probeID, false, true},
		{"check", aliceID, false, false},
		{"check", workerID, false, true},
		{"ack", probeID, false, false},
		{"ack", probeID, true, true},
		{"review", probeID, true, false},
		{"done", UserID("bob", testServer), false, false},
	}
	for _, c := range cases {
		if got := r.AddressableFor(c.verb, c.id, c.probeCheck); got != c.want {
			t.Errorf("AddressableFor(%s, %s, %v) = %v", c.verb, c.id, c.probeCheck, got)
		}
	}
}

func TestDeadlinesAndStaleness(t *testing.T) {
	l := DefaultLimits()
	if d, ok := PingDeadlineMs(1000, "review", l); !ok || d != 1000+900_000 {
		t.Fatalf("review deadline %d, %v", d, ok)
	}
	if d, ok := PingDeadlineMs(1000, "halt", l); !ok || d != 1000+300_000 {
		t.Fatalf("halt deadline %d, %v", d, ok)
	}
	if _, ok := PingDeadlineMs(1000, "done", l); ok {
		t.Fatal("done has a deadline")
	}
	if !AnswerOnTime(5000, 5000) || AnswerOnTime(5001, 5000) {
		t.Fatal("AnswerOnTime is not at-or-before")
	}
	if TimeoutDue(5000, 35_000) || !TimeoutDue(5000, 35_001) {
		t.Fatal("TimeoutDue is not deadline plus 30 s grace")
	}
	if TimeoutKey("$abc", "myrepo.2+desk.podman") != "$abc~myrepo.2+desk.podman" {
		t.Fatal("TimeoutKey")
	}
}

func TestStaleness(t *testing.T) {
	l := DefaultLimits()
	if PingStale(0, "halt", 300_000, l) || !PingStale(0, "halt", 300_001, l) {
		t.Fatal("halt staleness")
	}
	if PingStale(0, "done", 900_000, l) || !PingStale(0, "done", 900_001, l) {
		t.Fatal("a verb with no answer is stale after ack_timeout_s")
	}
	if PingStale(0, "nosuch", 1, l) {
		t.Fatal("unknown verb stale")
	}
	if TextStale(0, 86_400_000, l) || !TextStale(0, 86_400_001, l) {
		t.Fatal("text staleness")
	}
}
