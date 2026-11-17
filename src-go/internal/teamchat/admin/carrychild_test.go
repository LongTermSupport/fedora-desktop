package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/carry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
)

var _ carry.Child = CarryChild{}

// TestCarryChildAsksEachOpAndReadsItsAnswer is SC-C1 to SC-C3 end to end: the args encoded as
// the child reads them, the answers decoded strictly by root.
func TestCarryChildAsksEachOpAndReadsItsAnswer(t *testing.T) {
	w := withMember(t)
	c := CarryChild{Live: inProcessChild{w: w}}
	team := alpha(t)
	want := w.wantTail(t)
	live := w.liveRoster(t)
	epoch := restore.Epoch{RestoreEpoch: live.RestoreEpoch, Rev: live.Rev}
	got, err := c.Tail(t.Context(), team, w.teamFile(t))
	if err != nil || !slices.Equal(got.Rooms, want) || got.Epoch != epoch {
		t.Fatalf("Tail = %+v, %v", got, err)
	}
	port, err := paths.ParsePort(stagedPort)
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.Check(t.Context(), team, carry.CheckArgs{TeamFile: w.teamFile(t), Port: port, Tail: want})
	if err != nil || !slices.Equal(p.Found, slices.Repeat([]bool{true}, len(want))) || p.Epoch != epoch {
		t.Fatalf("Check = %+v, %v", p, err)
	}
	names, err := c.Place(t.Context(), team, carry.PlaceArgs{
		TeamFile: w.teamFile(t), Roster: w.roster(t).Encode(), Rooms: apply.EncodeRooms(team, w.rooms(t)), Token: "carried-token",
		Guest: []carry.GuestToken{{Handle: sampleHandle, Token: "guest-token"}}, Lost: []carry.LostExport{lostExport("restore-2-lost.jsonl", "x\n")},
		Stream: strings.NewReader("x\n"),
	})
	if err != nil || len(names) != 5 || len(w.placed) != 4 || string(w.placed[3].Data) != "guest-token" || !slices.Equal(w.lostRead, []string{"x\n"}) {
		t.Fatalf("Place = %q, %v; placed %q, lost %q", names, err, w.placed, w.lostRead)
	}
}

func lostExport(name, data string) carry.LostExport {
	sum := sha256.Sum256([]byte(data))
	return carry.LostExport{Name: name, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}

// TestCarryChildPlaceStreamsOnlyWithLostExports is condition 1 of the ruling (sc-D11) on root's
// side: a stream goes with carry.place alone, and only with the lost exports it carries.
func TestCarryChildPlaceStreamsOnlyWithLostExports(t *testing.T) {
	w := withMember(t)
	c := CarryChild{Live: inProcessChild{w: w}}
	team := alpha(t)
	args := carry.PlaceArgs{TeamFile: w.teamFile(t), Roster: w.roster(t).Encode(), Rooms: apply.EncodeRooms(team, w.rooms(t)), Token: "t"}
	withLost := args
	withLost.Lost = []carry.LostExport{lostExport("restore-2-lost.jsonl", "x\n")}
	if _, err := c.Place(t.Context(), team, withLost); err == nil || w.placed != nil {
		t.Fatalf("lost exports with no stream were asked: %v", err)
	}
	withStream := args
	withStream.Stream = strings.NewReader("x\n")
	if _, err := c.Place(t.Context(), team, withStream); err == nil || w.placed != nil {
		t.Fatalf("a stream with no lost export was asked: %v", err)
	}
	if _, err := c.Place(t.Context(), team, args); err != nil || len(w.placed) != 3 {
		t.Fatalf("no lost export, no stream: %v, placed %q", err, w.placed)
	}
}

// TestCarryChildFailures: the child's failure is a ChildError of its class; an answer that does
// not fit the op, or files root cannot encode, fail before or after the ask.
func TestCarryChildFailures(t *testing.T) {
	w := withMember(t)
	c := CarryChild{Live: inProcessChild{w: w}}
	team := alpha(t)
	w.removeRooms(t)
	_, err := c.Tail(t.Context(), team, w.teamFile(t))
	if ce, ok := errors.AsType[*ChildError](err); !ok || ce.Class != ClassConfig {
		t.Fatalf("Tail with no rooms record = %v", err)
	}
	if _, err := c.Place(t.Context(), team, carry.PlaceArgs{TeamFile: w.teamFile(t), Roster: w.roster(t).Encode(), Rooms: []byte(`{`), Token: "t"}); err == nil || w.placed != nil {
		t.Fatalf("rooms root cannot parse were asked: %v", err)
	}
	w2 := withMember(t)
	w2.lie = func(a *Answer) { a.Tail = nil }
	if _, err := (CarryChild{Live: inProcessChild{w: w2}}).Tail(t.Context(), team, w2.teamFile(t)); err == nil {
		t.Fatal("a tail answer with no tail was taken")
	}
	if _, err := c.Tail(t.Context(), team, []byte(`[]`)); err == nil {
		t.Fatal("a team file that is not an object was sent")
	}
	if _, err := c.Check(t.Context(), team, carry.CheckArgs{TeamFile: w.teamFile(t), Tail: []carry.TailEntry{{Room: proto.PurposeMain, EventID: "$e", TsMS: 1}}}); err == nil {
		t.Fatal("a check with no port was asked")
	}
}
