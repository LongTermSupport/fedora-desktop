package team_test

import (
	"reflect"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/team"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// gap 14: Judge reads the engine's View as its Lookup, from memory.
var _ proto.Lookup = team.View{}

func TestViewIsJudgesLookup(t *testing.T) {
	related := proto.Related{Sender: "@repo.2+site.podman:ops.teamchat.internal", RoomID: "!main", Verb: "ask"}
	ping := proto.OwnPing{Verb: "ask", To: []string{"repo.3+site.lxc"}, OriginTs: 1_760_000_000_000}
	v := team.View{
		SeenIDs:  map[string]bool{"$pending": true},
		Events:   map[string]proto.Related{"$root": related},
		OwnPings: map[string]proto.OwnPing{"$ping": ping},
	}
	if !v.Seen("$pending") || v.Seen("$other") {
		t.Error("Seen is not the view's seen ids")
	}
	if got, ok := v.Event("$root"); !ok || !reflect.DeepEqual(got, related) {
		t.Errorf("Event: %+v, %v", got, ok)
	}
	if _, ok := v.Event("$other"); ok {
		t.Error("Event found an id the view does not hold")
	}
	if got, ok := v.OwnPing("$ping"); !ok || !reflect.DeepEqual(got, ping) {
		t.Errorf("OwnPing: %+v, %v", got, ok)
	}
	if _, ok := v.OwnPing("$other"); ok {
		t.Error("OwnPing found an id the view does not hold")
	}
}

// A zero View knows nothing, and reading it never panics.
func TestAZeroViewKnowsNothing(t *testing.T) {
	var v team.View
	_, event := v.Event("$x")
	_, ping := v.OwnPing("$x")
	if v.Seen("$x") || event || ping {
		t.Error("a zero view knows an id")
	}
}
