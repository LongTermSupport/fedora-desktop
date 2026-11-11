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

// setFacts puts a verification's role and f into Health, verified, in one write when any of
// them changed, so no reader sees the new role beside the old coordinator (issue 192); and
// says note when it is not the note said last.
func (t *Team) setFacts(role string, f roomFacts, note string) {
	if note != t.factsNote {
		t.factsNote = note
		if note != "" {
			t.log.Print(note)
		}
	}
	h := t.health
	if h.Verified && h.Role == role && same(h.Colour, f.colour) && same(h.Description, f.description) && same(h.Coordinator, f.coordinator) {
		return
	}
	t.setHealth(func(h *team.Health) {
		h.Role, h.Colour, h.Description, h.Coordinator, h.Verified = role, f.colour, f.description, f.coordinator, true
	})
}

func same(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
