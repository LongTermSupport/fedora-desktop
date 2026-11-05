package teamsync

import (
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// roomFacts are what a verification reads of main besides the roster, each nil for none: the
// admin's colour override (B27, n3e-D4), the team's description from main's topic (n3e-D5)
// and the roster's non-parked coordinator's handle (15a).
type roomFacts struct {
	colour, description, coordinator *string
}

// readFacts is main's facts from its state and roster, and the one line a colour event that is
// not taken says, "" for none. A colour that is not taken is the automatic colour, never a loss
// of trust (C9).
func readFacts(st []proto.StateEvent, r proto.Roster, a proto.Anchors) (roomFacts, string) {
	var f roomFacts
	note := ""
	hex, ok := proto.MainColour(st, a.Server)
	switch {
	case !ok:
		note = "the admin's colour override does not decode; the team's own colour is shown"
	case hex != "":
		f.colour = &hex
	case foreignColour(st, a.Server):
		note = "a colour override that is not the admin's, under key \"\", is ignored"
	}
	if d, found := proto.MainDescription(st, a.Server); found {
		f.description = &d
	}
	if id, found := r.Coordinator(); found {
		if h, isAgent := proto.AgentHandle(id, a.Server); isAgent {
			handle := h.String()
			f.coordinator = &handle
		}
	}
	return f, note
}

// foreignColour is a colour event in main that MainColour does not read.
func foreignColour(st []proto.StateEvent, server string) bool {
	for _, e := range st {
		if e.Type == proto.ColourType && (e.Sender != proto.AdminID(server) || e.StateKey != "") {
			return true
		}
	}
	return false
}

// setFacts puts f into Health when it changed, and says note when it is not the note said last.
func (t *Team) setFacts(f roomFacts, note string) {
	if note != t.factsNote {
		t.factsNote = note
		if note != "" {
			t.log.Print(note)
		}
	}
	h := t.health
	if same(h.Colour, f.colour) && same(h.Description, f.description) && same(h.Coordinator, f.coordinator) {
		return
	}
	t.setHealth(func(h *team.Health) { h.Colour, h.Description, h.Coordinator = f.colour, f.description, f.coordinator })
}

func same(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
