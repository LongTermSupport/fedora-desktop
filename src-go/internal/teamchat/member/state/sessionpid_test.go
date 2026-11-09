package state

import (
	"strings"
	"testing"
)

const (
	sampleKit     = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	sessionPIDDoc = `{"v":1,"run":"` + sampleRun + `","keeper":{"pid":10,"start_ticks":5},"session":null,"kit":"` + sampleKit + `",` +
		`"launch":"claude","inbound":"accept","kind":"ccy"}`
)

func TestDecodeSessionPID(t *testing.T) {
	started := edit(t, sessionPIDDoc, `"session":null`, `"session":{"pid":11,"start_ticks":6}`)
	accepts(t, DecodeSessionPID, sessionPIDDoc, started,
		edit(t, edit(t, sessionPIDDoc, `"claude"`, `"plain"`), `"accept"`, `"refuse"`))
	for _, kind := range memberKinds {
		accepts(t, DecodeSessionPID, edit(t, sessionPIDDoc, `"ccy"`, `"`+kind+`"`))
	}
	p, err := DecodeSessionPID([]byte(started))
	if err != nil || p.Keeper.PID != 10 || p.Session.PID != 11 || p.Kit.String() != sampleKit || p.Launch != LaunchClaude {
		t.Fatalf("DecodeSessionPID = %+v, %v", p, err)
	}
	strictRefusals(t, sessionPIDFile, sessionPIDDoc, "start_ticks", MaxSessionPID, DecodeSessionPID)
	refusesAll(t, sessionPIDFile, sessionPIDDoc, DecodeSessionPID, []refusal{
		{`"keeper":{"pid":10,"start_ticks":5}`, `"keeper":null`, CodeSchema},
		{`"pid":10`, `"pid":0`, CodeSchema},
		{`"pid":10`, `"pid":424242424`, CodeSchema},
		{`"start_ticks":5`, `"start_ticks":-1`, CodeSchema},
		{`"session":null`, `"session":{"pid":0,"start_ticks":6}`, CodeSchema},
		{`"session":null`, `"session":{"pid":10,"start_ticks":5}`, CodeSchema},
		{`"kit":"` + sampleKit + `"`, `"kit":"current"`, CodeSchema},
		{`"kit":"` + sampleKit + `"`, `"kit":null`, CodeSchema},
		{`"claude"`, `"SECRET"`, CodeSchema},
		{`"accept"`, `"SECRET"`, CodeSchema},
		{`"ccy"`, `"SECRET"`, CodeSchema},
	})
}

// TestSessionPIDCap holds the largest session.pid within its cap.
func TestSessionPIDCap(t *testing.T) {
	big := `{"pid":4194304,"start_ticks":9007199254740991}`
	doc := edit(t, sessionPIDDoc, `{"pid":10,"start_ticks":5}`, big)
	doc = edit(t, doc, `"session":null`, `"session":`+strings.Replace(big, "4194304", "4194303", 1))
	doc = edit(t, doc, `"launch":"claude","inbound":"accept","kind":"ccy"`, `"launch":"claude","inbound":"refuse","kind":"other"`)
	if len(doc) > MaxSessionPID/2 {
		t.Fatalf("the largest session.pid is %d bytes", len(doc))
	}
	accepts(t, DecodeSessionPID, doc)
}

func TestSessionPIDRoundTrip(t *testing.T) {
	p, err := DecodeSessionPID([]byte(sessionPIDDoc))
	check(t, err)
	roundTrips(t, p, MaxSessionPID, DecodeSessionPID)
}
