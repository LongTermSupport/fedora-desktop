package proto

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// relayOf is the coordinator's relay to a worker of the human post replyEvent, attached to Q4.
func relayOf(t *testing.T, to ...string) (Outgoing, error) {
	t.Helper()
	return composer(t, coordID).Text(Text{To: to, Text: RelayText(4), Relay: &Relay{Room: PurposeQuestions, ID: replyEvent, Q: 4}}, "", RelayText(4))
}

// A 7.3's relay: the coordinator's quiet text to one member, mentioning it, with the relay key;
// its receiver delivers it as a PEER verdict that carries the key, for the relay path.
func TestComposeRelay(t *testing.T) {
	out := mustSend(t)(relayOf(t, workerID))
	if at(t, out.Content, "msgtype").Str() != MsgNotice || !slices.Equal(userIDs(t, out.Content), []string{workerID}) {
		t.Errorf("relay content %s", out.Content.Compact())
	}
	if got := string(at(t, out.Content, KeyText, "relay").Compact()); got != `{"room":"questions","id":"`+replyEvent+`","q":4}` {
		t.Errorf("relay key %s", got)
	}
	v := receive(t, out, coordID, workerID)
	if v.Outcome != Deliver || v.Content.Text.Relay == nil || *v.Content.Text.Relay != (Relay{Room: PurposeQuestions, ID: replyEvent, Q: 4}) {
		t.Fatalf("the worker's verdict %v %q %+v", v.Outcome, v.Reason, v.Content.Text)
	}
}

// Rule 08: a relay from anyone but the coordinator is `role`; one to a human, to two members or
// to nobody is `target`; a malformed key is `schema`.
func TestRelayRules(t *testing.T) {
	_, err := relayOf(t, workerID, lxcWorkerID)
	refusedWith(t, "two addressees", err, ReasonTarget)
	_, err = relayOf(t, aliceID)
	refusedWith(t, "a human", err, ReasonTarget)
	_, err = composer(t, workerID).Text(Text{To: []string{lxcWorkerID}, Text: "x", Relay: &Relay{Room: PurposeQuestions, ID: replyEvent, Q: 4}}, "", "x")
	refusedWith(t, "a worker's relay", err, ReasonRole)

	forged := `{"msgtype":"m.notice","body":"x","teamchat.text":{"v":1,"to":["` + workerID + `"],"text":"x","relay":{"room":"questions","id":"` + replyEvent + `","q":4}}}`
	if v := judge(t, workerID, PurposeMain).Judge(mustParse(t, event(lxcWorkerID, 0, forged))); v.Outcome != Drop || v.Reason != ReasonRole {
		t.Errorf("a peer's relay: %v %q", v.Outcome, v.Reason)
	}
	for name, key := range map[string]string{
		"room":   `{"room":"main","id":"` + replyEvent + `","q":4}`,
		"id":     `{"room":"questions","id":"x","q":4}`,
		"q":      `{"room":"questions","id":"` + replyEvent + `","q":0}`,
		"shape":  `[]`,
		"no id":  `{"room":"questions","q":4}`,
		"q kind": `{"room":"questions","id":"` + replyEvent + `","q":"4"}`,
	} {
		content := strings.Replace(forged, `{"room":"questions","id":"`+replyEvent+`","q":4}`, key, 1)
		if v := judge(t, workerID, PurposeMain).Judge(mustParse(t, event(coordID, 0, content))); v.Outcome != Drop || v.Reason != ReasonSchema {
			t.Errorf("%s: %v %q", name, v.Outcome, v.Reason)
		}
	}
}

// A 10.6: the relayed post, fetched by the worker from questions, is a HUMAN verdict with the
// relay's account only when it is the post named and a team human's; else the relay is dropped
// `provenance`.
func TestRelayedPost(t *testing.T) {
	relay := receive(t, mustSend(t)(relayOf(t, workerID)), coordID, workerID)
	j := judge(t, workerID, PurposeQuestions)
	v := j.Relayed(relay, mustParse(t, event(aliceID, 90_000_000, humanPost("Use MIT.", ""))))
	if v.Outcome != Deliver || v.Kind != KindHuman || v.Human.Body != "Use MIT." || v.Event.Sender != aliceID {
		t.Fatalf("relayed %v %q %+v", v.Outcome, v.Reason, v)
	}
	if v.Relayed == nil || *v.Relayed != (Relayed{By: coordID, Q: 4}) {
		t.Errorf("relayed account %+v", v.Relayed)
	}
	agentPost := `{"msgtype":"m.notice","body":"Use MIT.","teamchat.thread":{"v":1,"n":4,"close":false}}`
	other := strings.Replace(event(aliceID, 0, humanPost("Use MIT.", "")), replyEvent, otherEvent, 1)
	for name, post := range map[string]string{
		"an agent's post":  event(coordID, 0, agentPost),
		"a worker's text":  event(lxcWorkerID, 0, humanPost("Use MIT.", "")),
		"another event":    other,
		"a notice":         event(aliceID, 0, `{"msgtype":"m.notice","body":"Use MIT."}`),
		"a stranger":       event("@stranger:"+testServer, 0, humanPost("Use MIT.", "")),
		"a carried post":   event(aliceID, 0, `{"msgtype":"m.text","body":"x","teamchat.carried":{}}`),
		"not a message":    `{"type":"m.room.topic","event_id":"` + replyEvent + `","sender":"` + aliceID + `","origin_server_ts":1,"state_key":"","content":{}}`,
		"an edit of other": event(aliceID, 0, `{"msgtype":"m.text","body":"* x","m.new_content":{"msgtype":"m.text","body":"x"},"m.relates_to":{"rel_type":"m.replace","event_id":"`+pingEvent+`"}}`),
	} {
		if v := j.Relayed(relay, mustParse(t, post)); v.Outcome != Drop || v.Reason != ReasonProvenance {
			t.Errorf("%s: %v %q", name, v.Outcome, v.Reason)
		}
	}
}

// A restored relayed item is fetched again by a member that does not coordinate: a human's post
// in questions is delivered, with no relay account (the restart loses it).
func TestFetchedQuestionsPost(t *testing.T) {
	j := judge(t, workerID, PurposeQuestions)
	if v := j.Fetched(mustParse(t, event(aliceID, 90_000_000, humanPost("Use MIT.", "")))); v.Outcome != Deliver || v.Kind != KindHuman || v.Relayed != nil {
		t.Errorf("fetched %v %q", v.Outcome, v.Reason)
	}
	if v := j.Fetched(mustParse(t, event(coordID, 0, humanPost("x", "")))); v.Outcome != Drop {
		t.Errorf("an agent's post fetched: %v", v.Outcome)
	}
}

// The relay's transaction ID is derived from the post and the seat, so it is sent once across
// restarts, and is a store transaction ID.
func TestRelayTxn(t *testing.T) {
	a, b := RelayTxn(replyEvent, workerID), RelayTxn(replyEvent, lxcWorkerID)
	if a != RelayTxn(replyEvent, workerID) || a == b || a == RelayTxn(otherEvent, workerID) {
		t.Errorf("txns %s %s", a, b)
	}
	if !regexp.MustCompile(`^tc[0-9a-f]{32}$`).MatchString(a) {
		t.Errorf("txn %s is not a store txn", a)
	}
}

// A 10.6 and 11.1: a relay is judged by its relay check, never by the receiver's window or its
// age: a burst of relays past recv_per_sender_minute, or one sent late, still reaches the check,
// so no human answer is lost to the coordinator's rate. Its other texts keep both limits.
func TestARelayIsNotRateLimitedOrStale(t *testing.T) {
	j := judge(t, workerID, PurposeMain)
	relay := mustSend(t)(relayOf(t, workerID))
	for i := range j.Roster.Limits.RecvPerSenderMinute + 5 {
		if v := j.Judge(mustParse(t, event(coordID, 0, string(relay.Content.Compact())))); v.Outcome != Deliver || v.Content.Text.Relay == nil {
			t.Fatalf("relay %d of a burst: %v %q", i+1, v.Outcome, v.Reason)
		}
	}
	text := mustSend(t)(composer(t, coordID).Text(Text{To: []string{workerID}, Text: "hello"}, "", "hello"))
	for range j.Roster.Limits.RecvPerSenderMinute {
		j.Judge(mustParse(t, event(coordID, 0, string(text.Content.Compact()))))
	}
	if v := j.Judge(mustParse(t, event(coordID, 0, string(text.Content.Compact())))); v.Outcome != Deliver || v.NotBefore <= j.NowMs {
		t.Errorf("the coordinator's text past the window: %v %q not before %d, want held", v.Outcome, v.Reason, v.NotBefore)
	}
	late := (j.Roster.Limits.HumanMaxAgeS + 1) * 1000
	if v := judge(t, workerID, PurposeMain).Judge(mustParse(t, event(coordID, late, string(relay.Content.Compact())))); v.Outcome != Deliver {
		t.Errorf("a late relay: %v %q", v.Outcome, v.Reason)
	}
	if v := judge(t, workerID, PurposeMain).Judge(mustParse(t, event(coordID, late, string(text.Content.Compact())))); v.Outcome != Drop || v.Reason != ReasonStale {
		t.Errorf("a late text: %v %q, want stale", v.Outcome, v.Reason)
	}
}
