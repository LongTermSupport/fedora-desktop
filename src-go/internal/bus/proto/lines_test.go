package proto

import (
	"errors"
	"testing"
)

func TestFormatLineRefusesEveryControlByte(t *testing.T) {
	for b := range 0x80 {
		_, err := FormatLine(LineSent, Some("team"), Some("a"+string(rune(b))))
		control := b < 0x20 || b == 0x7f
		if control != errors.Is(err, ErrLine) {
			t.Errorf("byte %#x: %v", b, err)
		}
	}
}

func TestOnlyDroppedGoesToStderr(t *testing.T) {
	for kind := range lineFields {
		if LineOnStderr(kind) != (kind == LineDropped) {
			t.Errorf("%s", kind)
		}
	}
}

func TestItemLineCarriesWhicheverItemIsSet(t *testing.T) {
	ev := "$" + "A" + "bcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
	human := &HumanMessage{EventID: ev, Sender: "@alice:server.test", TS: 1, Text: "hi"}
	line, err := ItemLine("myteam", "server.test", Outcome{Verdict: Accept, Human: human}, RoomMain, nil)
	if err != nil || line != "HUMAN\t1\tmyteam\t"+ev+"\talice\t1\t\"hi\"\t-" {
		t.Fatalf("%q %v", line, err)
	}
	peer := &PeerText{EventID: ev, Sender: "@app.1+desk.podman:server.test", TS: 2, Text: "x"}
	if line, err = ItemLine("myteam", "server.test", Outcome{Verdict: Accept, Peer: peer}, RoomMain, nil); err != nil {
		t.Fatalf("%q %v", line, err)
	}
	if _, err := ItemLine("myteam", "server.test", Outcome{Verdict: Drop}, RoomMain, nil); !errors.Is(err, ErrLine) {
		t.Fatal(err)
	}
}

func TestQuestionsRoomReplyNeedsAQuestionNumberFromOne(t *testing.T) {
	ev := "$" + "B" + "bcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
	ctx := &Context{ServerName: "server.test"}
	human := &HumanMessage{EventID: ev, Sender: "@alice:server.test"}
	_, _, err := ReplyLine("myteam", ctx, Outcome{Verdict: Accept, Human: human}, RoomQuestions, &QuestionTag{N: 0})
	if !errors.Is(err, ErrLine) {
		t.Fatal(err)
	}
}

func TestUnansweredCountIsNeverNegative(t *testing.T) {
	ev := "$" + "C" + "bcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
	q := &QuestionItem{N: 1, State: QuestionOpen, EventID: ev, Unanswered: -1}
	if _, err := QuestionLine("myteam", "server.test", q); !errors.Is(err, ErrLine) {
		t.Fatal(err)
	}
}
