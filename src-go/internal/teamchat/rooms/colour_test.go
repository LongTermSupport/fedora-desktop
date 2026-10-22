package rooms

import (
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// colourOf is main's colour event as the server holds it, and whether there is one.
func (w *world) colourOf(t *testing.T, ids proto.RoomIDs) (proto.StateEvent, bool) {
	t.Helper()
	for _, ev := range w.state(t, ids.Main) {
		if ev.Type == proto.ColourType && ev.StateKey == "" {
			return ev, true
		}
	}
	return proto.StateEvent{}, false
}

func (w *world) wantColour(t *testing.T, ids proto.RoomIDs, want string) {
	t.Helper()
	ev, ok := w.colourOf(t, ids)
	if !ok || string(ev.Content.Compact()) != want || ev.Sender != admin {
		t.Fatalf("colour event = %s from %q (present %v), want %q from the admin", ev.Content.Compact(), ev.Sender, ok, want)
	}
}

func TestDeclaredColourReachesTheMainRoom(t *testing.T) {
	w := newWorld(t)
	w.team.Colour = "#2dd4bf"
	ids := w.built(t)
	w.wantColour(t, ids, `{"colour":"#2dd4bf"}`)
	for _, id := range []string{ids.Reports, ids.Questions} {
		for _, ev := range w.state(t, id) {
			if ev.Type == proto.ColourType {
				t.Fatalf("room %s holds a colour event", id)
			}
		}
	}
	w.trusted(t, ids)
	mark := w.mark()
	if res := w.converge(t, ids); len(res.Changed) != 0 {
		t.Fatalf("a second apply changed %v", res.Changed)
	}
	if writes := w.kinds(mark); len(writes) != 0 {
		t.Fatalf("a second apply wrote %v", writes)
	}
}

func TestNoColourWritesNoEvent(t *testing.T) {
	w := newWorld(t)
	ids := w.built(t)
	if _, ok := w.colourOf(t, ids); ok {
		t.Fatal("a team with no colour has a colour event")
	}
}

func TestColourChangesAndClears(t *testing.T) {
	w := newWorld(t)
	ids := w.built(t)

	w.team.Colour = "#2dd4bf"
	mark := w.mark()
	res := w.converge(t, ids)
	if want := []string{"room main state " + proto.ColourType}; !slices.Equal(res.Changed, want) {
		t.Fatalf("adding changed %v, want %v", res.Changed, want)
	}
	if got := w.kinds(mark); !slices.Equal(got, []string{"state"}) {
		t.Fatalf("adding wrote %v", got)
	}
	w.wantColour(t, ids, `{"colour":"#2dd4bf"}`)

	w.team.Colour = "#99f6e4"
	w.converge(t, ids)
	w.wantColour(t, ids, `{"colour":"#99f6e4"}`)

	w.team.Colour = ""
	res = w.converge(t, ids)
	if want := []string{"room main state " + proto.ColourType}; !slices.Equal(res.Changed, want) {
		t.Fatalf("clearing changed %v, want %v", res.Changed, want)
	}
	w.wantColour(t, ids, `{}`)

	mark = w.mark()
	if res := w.converge(t, ids); len(res.Changed) != 0 {
		t.Fatalf("a cleared colour changed %v again", res.Changed)
	}
	if writes := w.kinds(mark); len(writes) != 0 {
		t.Fatalf("a cleared colour wrote %v again", writes)
	}
}

// A colour event another user sent is not the admin's to clear: with no colour declared it is
// left alone, and with one declared the admin's own is written over it.
func TestAMembersColourEventIsNotClearedButIsWrittenOver(t *testing.T) {
	w := newWorld(t)
	ids := w.built(t)
	foreign := func(events []map[string]any) []map[string]any {
		return append(events, map[string]any{
			"type": proto.ColourType, "state_key": "", "sender": alice, "event_id": "$c",
			"content": map[string]any{"colour": "#2dd4bf"},
		})
	}
	via := editing{Client: w.admin, roomID: ids.Main, edit: foreign}
	res, err := Converge(t.Context(), via, &recorder{}, w.team, ids)
	if err != nil || len(res.Changed) != 0 {
		t.Fatalf("a member's colour event: changed %v, %v", res.Changed, err)
	}
	w.team.Colour = "#2dd4bf"
	res, err = Converge(t.Context(), via, &recorder{}, w.team, ids)
	if err != nil || !slices.Equal(res.Changed, []string{"room main state " + proto.ColourType}) {
		t.Fatalf("over a member's colour event: %v, %v", res.Changed, err)
	}
}
