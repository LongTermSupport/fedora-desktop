package records

import "testing"

// SPEC-n3f section 6's example lines, each kind byte for byte, key order included. Two keys the
// examples leave out are in the records: `redacted` on a question, which the bullet under the
// examples and G4 name, and `restored` on a timeout, which the built record has carried since
// n3b (B 2.2.2's example omits it too).
func TestSection6RecordsAreBytePinned(t *testing.T) {
	a := teamID(t, "alpha")
	const headline = "Which licence for the new docs site?"
	cases := map[string]struct {
		rec  Record
		want string
	}{
		"a question": {
			Question{Team: a, N: 4, State: "open", ID: new(id43), Headline: headline, AskedBy: []string{"repo.2"}, Re: []string{id43}, AskedMS: new(int64(1760000000000)), Unanswered: []string{id43}},
			`{"kind":"question","v":1,"team":"alpha","n":4,"state":"open","id":"` + id43 + `","headline":"` + headline + `","asked_by":["repo.2"],"re":["` + id43 + `"],"asked_ms":1760000000000,"remind_after_s":null,"unanswered":["` + id43 + `"],"unanswered_more":0,"redacted":false}`,
		},
		"a question with a reminder, more unanswered than listed": {
			Question{Team: a, N: 5, State: "answered", ID: new(id43), Headline: "h", AskedBy: []string{"repo.2", "repo.3"}, Re: []string{id43, id43}, AskedMS: new(int64(1)), RemindAfterS: new(int64(600)), Unanswered: []string{id43}, UnansweredMore: 3},
			`{"kind":"question","v":1,"team":"alpha","n":5,"state":"answered","id":"` + id43 + `","headline":"h","asked_by":["repo.2","repo.3"],"re":["` + id43 + `","` + id43 + `"],"asked_ms":1,"remind_after_s":600,"unanswered":["` + id43 + `"],"unanswered_more":3,"redacted":false}`,
		},
		"a queued question has no id and no asked_ms": {
			Question{Team: a, N: 6, State: "open", Headline: "h", AskedBy: []string{"repo.2"}},
			`{"kind":"question","v":1,"team":"alpha","n":6,"state":"open","id":null,"headline":"h","asked_by":["repo.2"],"re":[],"asked_ms":null,"remind_after_s":null,"unanswered":[],"unanswered_more":0,"redacted":false}`,
		},
		"a redacted question keeps its number and nothing else": {
			Question{Team: a, N: 7, State: "closed", Redacted: true},
			`{"kind":"question","v":1,"team":"alpha","n":7,"state":"closed","id":null,"headline":"","asked_by":[],"re":[],"asked_ms":null,"remind_after_s":null,"unanswered":[],"unanswered_more":0,"redacted":true}`,
		},
		"an unattached post": {
			Unattached{Team: a, ID: id43, From: "alice", SentMS: 1760000000000},
			`{"kind":"unattached","v":1,"team":"alpha","id":"` + id43 + `","from":"alice","sent_ms":1760000000000}`,
		},
		"a human post attached to a question": {
			Human{Team: a, ID: id43, From: "alice", FromID: "@alice:srv", SentMS: 1760000000000, Room: "questions", Text: "MIT, please.", Question: new("Q4"), Headline: new(headline)},
			`{"kind":"human","v":1,"team":"alpha","id":"` + id43 + `","from":"alice","from_id":"@alice:srv","sent_ms":1760000000000,"room":"questions","text":"MIT, please.","question":"Q4","headline":"` + headline + `","revises":null,"restored":false}`,
		},
		"a human post no question holds": {
			Human{Team: a, ID: id43, From: "alice", FromID: "@alice:srv", SentMS: 1, Room: "questions", Text: "hello", Question: new(QuestionNone)},
			`{"kind":"human","v":1,"team":"alpha","id":"` + id43 + `","from":"alice","from_id":"@alice:srv","sent_ms":1,"room":"questions","text":"hello","question":"none","headline":null,"revises":null,"restored":false}`,
		},
		"a reminder's timeout has a question and no verb": {
			Timeout{Team: a, ID: id43, Silent: "humans", Question: new("Q4")},
			`{"kind":"timeout","v":1,"team":"alpha","id":"` + id43 + `","silent":"humans","question":"Q4","verb":null,"ref":null,"restored":false}`,
		},
		"a ping's timeout has a verb and no question": {
			Timeout{Team: a, ID: id43, Silent: "repo.3+site.lxc", Verb: new("review"), Ref: new("r")},
			`{"kind":"timeout","v":1,"team":"alpha","id":"` + id43 + `","silent":"repo.3+site.lxc","question":null,"verb":"review","ref":"r","restored":false}`,
		},
		"a joined questions room counts what was not echoed": {
			Room{Team: a, Purpose: "questions", State: "joined", Uncounted: new(int64(0))},
			`{"kind":"room","v":1,"team":"alpha","purpose":"questions","state":"joined","check":null,"uncounted":0}`,
		},
		"an untrusted reports room names the failed check": {
			Room{Team: a, Purpose: "reports", State: "untrusted", Check: new("join_rules")},
			`{"kind":"room","v":1,"team":"alpha","purpose":"reports","state":"untrusted","check":"join_rules","uncounted":null}`,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := Encode(c.rec)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Errorf("got  %s\nwant %s", got, c.want)
			}
		})
	}
}

// A redacted question is a number and nothing else (QX7): its text was redacted, so a record that
// says `redacted` and still carries any of it is refused, whichever field leaks.
func TestARedactedQuestionCarriesNothingButItsNumber(t *testing.T) {
	a := teamID(t, "alpha")
	leaks := map[string]func(*Question){
		"an open state":       func(q *Question) { q.State = "open" },
		"an id":               func(q *Question) { q.ID = new(id43) },
		"a headline":          func(q *Question) { q.Headline = "the redacted text" },
		"an asker":            func(q *Question) { q.AskedBy = []string{"repo.2"} },
		"a main id":           func(q *Question) { q.Re = []string{id43} },
		"an asked time":       func(q *Question) { q.AskedMS = new(int64(1)) },
		"a reminder":          func(q *Question) { q.RemindAfterS = new(int64(60)) },
		"an unanswered":       func(q *Question) { q.Unanswered = []string{id43} },
		"an unanswered count": func(q *Question) { q.UnansweredMore = 1 },
	}
	for name, leak := range leaks {
		q := Question{Team: a, N: 3, State: "closed", Redacted: true}
		leak(&q)
		if got, err := Encode(q); err == nil {
			t.Errorf("a redacted question with %s encoded: %s", name, got)
		}
	}
	if _, err := Encode(Question{Team: a, N: 3, State: "closed", Redacted: true}); err != nil {
		t.Errorf("a clean redacted question was refused: %v", err)
	}
}

// Human.Question and Timeout.Question are strings, so the type does not keep a bad label out: the
// encoder does, with proto's A 2.2 question name. `none` is a human post's alone, and a headline
// is the attached question's, so it only goes with a Q<n>.
func TestAQuestionLabelIsRefusedUnlessItIsOneOfTheThree(t *testing.T) {
	a := teamID(t, "alpha")
	human := func(q, h *string) Human {
		return Human{Team: a, ID: id43, From: "alice", FromID: "@alice:s", SentMS: 1, Room: "questions", Text: "t", Question: q, Headline: h}
	}
	refused := map[string]Record{
		"a human question of 4":       human(new("4"), nil),
		"a human question of q4":      human(new("q4"), nil),
		"a human question of Q0":      human(new("Q0"), nil),
		"a human question of Q04":     human(new("Q04"), nil),
		"a human question of Q":       human(new("Q"), nil),
		"a human question of empty":   human(new(""), nil),
		"a human question of Q4 NL":   human(new("Q4\n"), nil),
		"a headline with no question": human(nil, new("h")),
		"a headline with none":        human(new(QuestionNone), new("h")),
		"a timeout question of none":  Timeout{Team: a, ID: id43, Silent: "humans", Question: new(QuestionNone)},
		"a timeout question of 4":     Timeout{Team: a, ID: id43, Silent: "humans", Question: new("4")},
	}
	for name, rec := range refused {
		if got, err := Encode(rec); err == nil {
			t.Errorf("%s encoded: %s", name, got)
		}
	}
	for name, rec := range map[string]Record{
		"a human in main":              human(nil, nil),
		"a human with none":            human(new(QuestionNone), nil),
		"a human with Q4":              human(new("Q4"), new("h")),
		"a human with Q4, no headline": human(new("Q4"), nil),
		"a timeout with Q123456789":    Timeout{Team: a, ID: id43, Silent: "humans", Question: new("Q123456789")},
	} {
		if _, err := Encode(rec); err != nil {
			t.Errorf("%s was refused: %v", name, err)
		}
	}
}

// A history record's item goes through the same checks as the record alone.
func TestAHistoryItemKeepsItsQuestionChecks(t *testing.T) {
	a := teamID(t, "alpha")
	bad := Human{Team: a, ID: id43, From: "alice", FromID: "@alice:s", SentMS: 1, Room: "questions", Text: "t", Question: new("4")}
	if got, err := Encode(History{Team: a, Dir: "in", Item: bad}); err == nil {
		t.Errorf("encoded %s", got)
	}
}
