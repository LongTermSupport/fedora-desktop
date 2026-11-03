package proto

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

func message(id, sender string, content pyjson.Object) pyjson.Object {
	return pyjson.Object{"type": "m.room.message", "event_id": id, "sender": sender, "content": content}
}

func inThread(root, body string) pyjson.Object {
	return pyjson.Object{
		"msgtype": "m.text", "body": body,
		"m.relates_to": pyjson.Object{"rel_type": "m.thread", "event_id": root},
	}
}

func TestQuestionIndexStates(t *testing.T) {
	ctx := &Context{ServerName: "server.test", Humans: []string{"@alice:server.test"}}
	closing, err := BuildThreadPost(ThreadPost{Root: eventID('b'), Close: 2})
	if err != nil {
		t.Fatal(err)
	}
	index := BuildQuestionIndex([]any{
		questionEvent(1, eventID('a')),
		questionEvent(2, eventID('b')),
		questionEvent(1, eventID('c')),
		message(eventID('d'), "@alice:server.test", inThread(eventID('a'), "yes")),
		message(eventID('e'), testAgent, closing),
		message(eventID('f'), "@alice:server.test", pyjson.Object{"body": "q2: late"}),
		message(eventID('g'), "@alice:server.test", inThread(eventID('z'), "no root")),
	}, ctx)
	if index.NextNumber() != 3 {
		t.Fatal(index.NextNumber())
	}
	views := index.Views()
	if len(views) != 2 || views[0].State != StateAnswered || views[1].State != StateClosed {
		t.Fatalf("%+v", views)
	}
	if len(views[0].Unanswered) != 1 || len(views[1].Replies) != 1 || len(views[1].Unanswered) != 1 {
		t.Fatalf("%+v", views)
	}
	if q, ok := index.OpenDuplicate("  which?\n"); !ok || q.N != 1 {
		t.Fatal(q, ok)
	}
	if root, seen := index.Attached(eventID('g')); !seen || root != "" {
		t.Fatal(root, seen)
	}
}

func TestQuestionIndexThreadRootNotAString(t *testing.T) {
	ctx := &Context{ServerName: "server.test", Humans: []string{"@alice:server.test"}}
	content := pyjson.Object{"body": "Q1 x", "m.relates_to": pyjson.Object{"rel_type": "m.thread", "event_id": []any{}}}
	index := BuildQuestionIndex([]any{questionEvent(1, eventID('a')), message(eventID('d'), "@alice:server.test", content)}, ctx)
	if root, seen := index.Attached(eventID('d')); !seen || root != eventID('a') {
		t.Fatal(root, seen)
	}
}
