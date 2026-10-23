package team_test

import (
	"context"
	"errors"
	"fmt"
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

// fakeForge is the shape D4 and D6a test against.
type fakeForge struct{ result team.ForgeResult }

func (f fakeForge) Check(context.Context, proto.ForgeCheck, proto.Forge) (team.ForgeResult, error) {
	return f.result, nil
}

var _ team.Forge = fakeForge{}

// n3d-D10, n3d-D11: the forge's two failures are distinct errors, each matched through a wrap,
// and a check's outcome is proven or one of proto's forge reasons.
func TestN3dForgeErrorsAndOutcomes(t *testing.T) {
	if errors.Is(team.ErrForgeLogin, team.ErrForgeDown) || errors.Is(team.ErrForgeDown, team.ErrForgeLogin) {
		t.Fatal("the forge errors are one error")
	}
	if !errors.Is(fmt.Errorf("check: %w", team.ErrForgeDown), team.ErrForgeDown) {
		t.Fatal("a wrapped ErrForgeDown is not matched")
	}
	got, err := fakeForge{team.ForgeResult{Outcome: team.ForgeProven}}.Check(t.Context(), proto.ForgeCheck{}, proto.Forge{})
	if err != nil || got.Outcome != "proven" {
		t.Fatalf("Check = %+v, %v", got, err)
	}
}

// n3d-D9: an answer to an own ping and an own ping's echo ride on the batch.
func TestN3dBatchCarriesAnswersAndEchoes(t *testing.T) {
	b := team.Batch{
		Answered: []team.Answer{{Ping: "$p", From: "@a:s", Verb: proto.VerbAck, ID: "$a", OriginMS: 2}},
		Echoes:   []team.Echo{{ID: "$p", OriginMS: 1}},
	}
	if b.Answered[0].Ping != b.Echoes[0].ID || b.Answered[0].OriginMS <= b.Echoes[0].OriginMS {
		t.Fatalf("batch %+v", b)
	}
}

func TestAReceiveCheckCarriesItsSpend(t *testing.T) {
	if _, ok := team.ReceiveSpend(context.Background()); ok {
		t.Fatal("a send's context is a receive check")
	}
	asked := 0
	ctx := team.WithReceive(context.Background(), func() bool { asked++; return asked == 1 })
	spend, ok := team.ReceiveSpend(ctx)
	if !ok || !spend() || spend() || asked != 2 {
		t.Fatalf("ReceiveSpend = %t; asked %d", ok, asked)
	}
}
