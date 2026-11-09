package teamsync

import (
	"encoding/json"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// putState writes main's state event typ as the admin does.
func (w *world) putState(typ string, content map[string]any) {
	w.t.Helper()
	data, err := json.Marshal(content)
	check(w.t, err)
	if _, err := w.admin().PutState(w.t.Context(), w.main, typ, "", data); err != nil {
		w.t.Fatalf("%s: %v", typ, err)
	}
}

func colourIs(hex string) func(team.Health) bool {
	return func(h team.Health) bool { return h.Colour != nil && *h.Colour == hex }
}

func is(p *string, want string) bool { return p != nil && *p == want }

func text(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// B27, n3e-D4, n3e-D5, 15a: a verification reads the admin's colour override, the team's
// description from main's topic and the roster's coordinator into Health.
func TestAVerificationReadsMainsFacts(t *testing.T) {
	w := newWorld(t)
	w.putState(proto.ColourType, map[string]any{"colour": "#ffffff"})
	w.putState("m.room.topic", map[string]any{"topic": "Ships the widgets"})
	r := w.start()
	w.batch()
	r.poll(t)
	h := w.eng.lastHealth()
	if !is(h.Colour, "#ffffff") || !is(h.Description, "Ships the widgets") || !is(h.Coordinator, coord) {
		t.Fatalf("health colour %v description %v coordinator %v", h.Colour, h.Description, h.Coordinator)
	}
}

// B27: a colour the admin writes while the team runs is read again at once, as the sync carries
// it; a cleared override is none, so the status line falls back to the automatic colour.
func TestAColourChangeIsReadFromTheSync(t *testing.T) {
	w := newWorld(t)
	r := w.start()
	w.batch()
	r.poll(t)
	if h := w.eng.lastHealth(); h.Colour != nil || h.Description != nil || !is(h.Coordinator, coord) {
		t.Fatalf("no override, no description: %+v", h)
	}
	w.putState(proto.ColourType, map[string]any{"colour": "#a78bfa"})
	w.waitHealth(colourIs("#a78bfa"))
	w.putState(proto.ColourType, map[string]any{})
	w.waitHealth(func(h team.Health) bool { return h.Colour == nil && h.Stopped == "" && h.Coordinator != nil })
}

// B27 (C9): a malformed override is no colour and one link.log line, and never a loss of trust:
// the team keeps syncing.
func TestAMalformedColourIsIgnoredAndTheTeamStaysTrusted(t *testing.T) {
	w := newWorld(t)
	w.putState(proto.ColourType, map[string]any{"colour": "#FFFFFF", "extra": 1})
	r := w.start()
	w.batch()
	r.poll(t)
	w.say(alice, human("still here", user(me)))
	if b := w.batch(); len(b.Items) != 1 {
		t.Fatalf("items %+v", b.Items)
	}
	r.poll(t)
	if h := w.eng.lastHealth(); h.Colour != nil || h.Stopped != "" {
		t.Fatalf("health %+v", h)
	}
	if n := w.log.count("colour override does not decode"); n != 1 {
		t.Fatalf("log %q", w.log.String())
	}
}

// B27 (C8, C9): only the admin's event under key "" is the override. A member's (which main's
// power levels refuse, so only the read is tested), or one under another key, is ignored with a
// note.
func TestReadFacts(t *testing.T) {
	a := proto.Anchors{Server: server, Team: teamName, MainRoom: "!main:" + server, Self: user(me)}
	admin := proto.AdminID(server)
	ev := func(key, sender string, content proto.Value) []proto.StateEvent {
		return []proto.StateEvent{{Type: proto.ColourType, StateKey: key, Sender: sender, EventID: "$e", Content: content}}
	}
	hex := proto.Obj(proto.M("colour", proto.Str("#2dd4bf")))
	r := baseRoster()
	for _, c := range []struct {
		name   string
		state  []proto.StateEvent
		colour string
		note   bool
	}{
		{"none", nil, "", false},
		{"the admin's", ev("", admin, hex), "#2dd4bf", false},
		{"cleared", ev("", admin, proto.Obj()), "", false},
		{"a member's", ev("", user(peer), hex), "", true},
		{"another key", ev("x", admin, hex), "", true},
		{"malformed", ev("", admin, proto.Int(3)), "", true},
	} {
		f, note := readFacts(c.state, r, a)
		if got := text(f.colour); got != c.colour || (f.colour == nil) != (c.colour == "") {
			t.Errorf("%s: colour %q", c.name, got)
		}
		if (note != "") != c.note {
			t.Errorf("%s: note %q", c.name, note)
		}
	}
	r.Members[user(coord)] = proto.RosterMember{Role: proto.RoleCoordinator, HumanText: true, Parked: true}
	if f, note := readFacts(nil, r, a); f.coordinator != nil || f.description != nil || note != "" {
		t.Errorf("a parked coordinator is none: %+v %q", f, note)
	}
}

// The facts note is said once per change: the same note twice is one line, a new one another,
// and a good read clears it so a later bad one is said again.
func TestTheFactsNoteIsSaidOncePerChange(t *testing.T) {
	w := newWorld(t)
	tm := w.newTeam()
	for _, n := range []string{"one", "one", "", "one", "two"} {
		tm.setFacts(roomFacts{}, n)
	}
	if got := w.log.count("one"); got != 2 {
		t.Errorf("one said %d times", got)
	}
	if got := w.log.count("two"); got != 1 {
		t.Errorf("two said %d times", got)
	}
}
