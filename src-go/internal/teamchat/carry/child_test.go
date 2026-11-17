package carry

import (
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
)

// SC-C1, SC-C3: the tail on the wire is the records' own tail, decoded strictly.
func TestTailOnTheWireIsTheRecordsTail(t *testing.T) {
	tail := sampleSeal(t).Tail
	v := EncodeTail(tail)
	want := `[{"room":"main","event_id":"$abc:ops.teamchat.internal","ts_ms":1699999999000}]`
	if got := string(v.Compact()); got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	got, err := DecodeTail(v)
	if err != nil || len(got) != 1 || got[0] != tail[0] {
		t.Fatalf("DecodeTail = %+v, %v", got, err)
	}
	for name, raw := range map[string]string{
		"empty":     `[]`,
		"object":    `{}`,
		"extra":     `[{"room":"main","event_id":"$a","ts_ms":1,"x":1}]`,
		"negative":  `[{"room":"main","event_id":"$a","ts_ms":-1}]`,
		"space":     `[{"room":"main","event_id":"$a b","ts_ms":1}]`,
		"twice":     `[{"room":"main","event_id":"$a","ts_ms":1},{"room":"main","event_id":"$b","ts_ms":2}]`,
		"no events": `[{"room":"main","ts_ms":1}]`,
	} {
		v, err := proto.Parse([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeTail(v); err == nil {
			t.Errorf("%s: a bad tail decoded", name)
		}
	}
}

// SC2: the roster event read with the tail must be roster.json's rev and restore epoch, or the
// seal is refused (70), saying to run team apply first.
func TestTailAgreesWithTheRoster(t *testing.T) {
	tail := Tail{Rooms: sampleSeal(t).Tail, Epoch: restore.Epoch{RestoreEpoch: 2, Rev: 9}}
	if err := tail.Agrees(RosterFacts{Seq: 30, Rev: 9, RestoreEpoch: 2}); err != nil {
		t.Fatalf("an agreeing roster: %v", err)
	}
	for name, r := range map[string]RosterFacts{
		"rev":   {Seq: 30, Rev: 8, RestoreEpoch: 2},
		"epoch": {Seq: 30, Rev: 9, RestoreEpoch: 3},
	} {
		err := tail.Agrees(r)
		if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "run team apply first") {
			t.Errorf("%s: %v", name, err)
		}
	}
}
