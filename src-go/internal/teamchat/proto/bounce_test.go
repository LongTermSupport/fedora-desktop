package proto

import (
	"fmt"
	"testing"
)

var sprintf = fmt.Sprintf

// Bounce selection (Plan 00267 DESIGN A, C2): a drop is bounced by the member it addressed,
// read best-effort from the raw content when it does not decode; by the coordinator when no
// addressee can be read and for a human post that reached no agent; never to an outsider, the
// admin or oneself, never for a receipt, and never by a member it did not address.
func TestBounceSelection(t *testing.T) {
	const (
		futureKey = `{"msgtype":"m.text","body":"x","teamchat.future":{"v":1,"to":["` + "@x:y" + `"]}}`
		version2  = `{"msgtype":"m.text","body":"x","teamchat.text":{"v":2,"to":["%s"],"text":"x"}}`
		receiptIn = `{"msgtype":"m.notice","body":"x","teamchat.receipt":{"v":1,"of":"` + "$" + "0000000000000000000000000000000000000000001" + `","to":["%s"],"state":"delivered","at":1}}`
	)
	image := `{"msgtype":"m.image","body":"x"` + mention(workerID) + `}`
	for name, tc := range map[string]struct {
		self, purpose, event string
		reason               string
		bounce               bool
	}{
		"an unknown verb, by its addressee":       {workerID, PurposeMain, event(coordID, 0, pingContent("poke", []string{workerID}, "", "")), ReasonVerb, true},
		"an unknown verb, by a member not in to":  {lxcWorkerID, PurposeMain, event(coordID, 0, pingContent("poke", []string{workerID}, "", "")), "", false},
		"an undecodable text, by its raw to":      {workerID, PurposeMain, event(coordID, 0, sprintf(version2, workerID)), ReasonVersion, true},
		"an undecodable text, not in its raw to":  {lxcWorkerID, PurposeMain, event(coordID, 0, sprintf(version2, workerID)), "", false},
		"no typed key read, by the coordinator":   {coordID, PurposeMain, event(workerID, 0, `{"msgtype":"m.text","body":"x","teamchat.future":{"v":1}}`), ReasonSchema, true},
		"no typed key read, by a worker":          {lxcWorkerID, PurposeMain, event(workerID, 0, `{"msgtype":"m.text","body":"x","teamchat.future":{"v":1}}`), "", false},
		"an unknown key's to is not an addressee": {lxcWorkerID, PurposeMain, event(workerID, 0, futureKey), "", false},
		"an outsider":                            {coordID, PurposeMain, event("@mallory:elsewhere.example", 0, textContent([]string{coordID}, "")), "", false},
		"a human to a member without human text": {lxcWorkerID, PurposeMain, event(aliceID, 0, humanContent(mention(lxcWorkerID))), ReasonSender, true},
		"a human post that reached no agent":     {coordID, PurposeMain, event(aliceID, 0, humanContent(mention(lxcWorkerID))), ReasonUnheard, true},
		"unheard is the coordinator's alone":     {workerID, PurposeMain, event(aliceID, 0, humanContent(mention(lxcWorkerID))), "", false},
		"a human post delivered":                 {workerID, PurposeMain, event(aliceID, 0, humanContent(mention(workerID))), "", false},
		"a stale human post":                     {workerID, PurposeMain, event(aliceID, 40*24*3600*1000, humanContent(mention(workerID))), ReasonStale, true},
		"a human image, by the member it names":  {workerID, PurposeMain, event(aliceID, 0, image), IgnoreType, true},
		"a human image, by another":              {lxcWorkerID, PurposeMain, event(aliceID, 0, image), "", false},
		"an agent post in questions":             {coordID, PurposeQuestions, event(workerID, 0, textContent([]string{coordID}, "")), ReasonSender, true},
		"a receipt in main is never receipted":   {workerID, PurposeMain, event(coordID, 0, sprintf(receiptIn, workerID)), "", false},
		"a bad receipt in reports":               {coordID, PurposeReports, event(workerID, 0, `{"msgtype":"m.notice","body":"x","teamchat.receipt":{"v":2}}`), "", false},
		"the admin's malformed notice":           {workerID, PurposeMain, event(adminID, 0, `{"msgtype":"m.notice","body":"x","teamchat.notice":{"v":2,"to":["`+workerID+`"]}}`), "", false},
		"an own event":                           {coordID, PurposeMain, event(coordID, 0, pingContent("poke", []string{coordID}, "", "")), "", false},
	} {
		j := judge(t, tc.self, tc.purpose)
		v := j.Judge(mustParse(t, tc.event))
		reason, bounce := j.Bounce(v)
		if bounce != tc.bounce || reason != tc.reason {
			t.Errorf("%s: verdict %v %q, Bounce = %q %t, want %q %t", name, v.Outcome, v.Reason, reason, bounce, tc.reason, tc.bounce)
		}
	}
}
