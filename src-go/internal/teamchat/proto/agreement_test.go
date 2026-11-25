package proto

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// The class this file guards (issue #422): a composer that accepts what an addressee's receiver
// drops. The sender is told "sent" and the receiver counts a drop nobody sees, so the message is
// lost in silence. The Composer's contract is "refuses what a receiver would drop"; every
// composition here that is accepted is judged by every agent it addresses, and each must take it
// (deliver, or record an answer). A drop at an addressee is the defect, whatever its reason.

// agreementSenders are the agent members that may compose.
func agreementSenders() []string { return []string{coordID, workerID, lxcWorkerID} }

// agreementTargets are every single addressee and every pair, sorted.
func agreementTargets() [][]string {
	ids := []string{aliceID, coordID, workerID, lxcWorkerID, parkedID, probeID}
	var out [][]string
	for i, a := range ids {
		out = append(out, []string{a})
		for _, b := range ids[i+1:] {
			out = append(out, sortedStrs([]string{a, b}))
		}
	}
	return out
}

// takenByAddressees fails for each agent addressee of out whose receiver does not take it.
func takenByAddressees(t *testing.T, what string, out Outgoing, sender string, to []string) {
	t.Helper()
	r := composer(t, sender).Roster
	for _, id := range to {
		if _, agent := r.Members[id]; !agent {
			continue
		}
		v := receive(t, out, sender, id)
		if v.Outcome != Deliver && v.Outcome != Record {
			t.Errorf("%s from %s to %v: composed, but %s's receiver gives %v %q", what, sender, to, id, v.Outcome, v.Reason)
		}
	}
}

func TestComposedTextIsTakenByEveryAddressee(t *testing.T) {
	for _, sender := range agreementSenders() {
		for _, to := range agreementTargets() {
			out, err := composer(t, sender).Text(Text{To: to, Text: "Take task 3.\nSecond line."}, "", "text")
			if err != nil {
				continue
			}
			takenByAddressees(t, "text", out, sender, at(t, out.Content, KeyText, "to").strs())
		}
	}
}

func TestComposedRelayIsTakenByEveryAddressee(t *testing.T) {
	for _, to := range agreementTargets() {
		out, err := relayOf(t, to...)
		if err != nil {
			continue
		}
		takenByAddressees(t, "relay", out, coordID, at(t, out.Content, KeyText, "to").strs())
	}
}

// TestPeerIgnoresHumanText is issue #422: human_text gates a human's words, so an agent's own
// text reaches a worker whose roster entry says human_text false, while a relayed human post
// to it is refused at the sender, and the refusal names why.
func TestPeerIgnoresHumanText(t *testing.T) {
	out := mustSend(t)(composer(t, coordID).Text(Text{To: []string{lxcWorkerID}, Text: "Take task 3."}, "", "say"))
	if v := receive(t, out, coordID, lxcWorkerID); v.Outcome != Deliver || v.Kind != KindPeer {
		t.Errorf("the coordinator's say to a human_text false worker: %v %q", v.Outcome, v.Reason)
	}
	_, err := relayOf(t, lxcWorkerID)
	refusedWith(t, "a relay to a human_text false worker", err, ReasonTarget)
	if err == nil || !strings.Contains(err.Error(), "human_text false") {
		t.Errorf("the refusal does not say why: %v", err)
	}
	_, err = composer(t, workerID).Text(Text{To: []string{workerID}, Text: "x"}, "", "x")
	refusedWith(t, "a text to itself", err, ReasonTarget)
}

func TestComposedPingIsTakenByEveryAddressee(t *testing.T) {
	for _, sender := range agreementSenders() {
		for _, verb := range slices.Sorted(maps.Keys(Verbs)) {
			for _, answers := range []string{"", pingEvent} {
				for _, to := range agreementTargets() {
					ref := ""
					if Verbs[verb].Ref == Required {
						ref = commitRef
					}
					out, err := composer(t, sender).Ping(Ping{Verb: verb, To: to, Ref: ref}, answers, verb)
					if err != nil {
						continue
					}
					takenByAddressees(t, "ping "+verb, out, sender, at(t, out.Content, KeyPing, "to").strs())
				}
			}
		}
	}
}

// strs is a JSON array of strings as a slice.
func (v Value) strs() []string {
	var out []string
	for _, item := range v.Items() {
		out = append(out, item.Str())
	}
	return out
}
