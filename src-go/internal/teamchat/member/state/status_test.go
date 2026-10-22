package state

import (
	"strings"
	"testing"
)

const (
	statusTeam = `{"team":"alpha","role":"coordinator","down":false,"failed":null,"delivery":"socket","pending":0,"missing":0}`
	statusDoc  = `{"v":1,"run":"` + sampleRun + `","written_ms":1760000000000,"desired":"applied","unusable_hook_stdin":0,` +
		`"teams":[` + statusTeam + `]}`
)

func TestDecodeStatus(t *testing.T) {
	none := edit(t, statusDoc, `[`+statusTeam+`]`, `[]`)
	accepts(t, DecodeStatus, statusDoc, none,
		edit(t, statusDoc, `"role":"coordinator"`, `"role":null`),
		edit(t, edit(t, statusDoc, `"down":false`, `"down":true`), `"desired":"applied"`, `"desired":"refused"`),
		edit(t, statusDoc, `"teams":[`, `"teams":[`+strings.ReplaceAll(statusTeam, "alpha", "beta")+`,`))
	for _, why := range []string{FailedUnauthorized, FailedForbidden, FailedTrust, FailedBundle} {
		accepts(t, DecodeStatus, edit(t, statusDoc, `"failed":null`, `"failed":"`+why+`"`))
	}
	s, err := DecodeStatus([]byte(statusDoc))
	if err != nil || len(s.Teams) != 1 || *s.Teams[0].Role != "coordinator" {
		t.Fatalf("DecodeStatus = %+v, %v", s, err)
	}
	strictRefusals(t, statusFile, statusDoc, "pending", MaxStatus, DecodeStatus)
	refusesAll(t, statusFile, statusDoc, DecodeStatus, []refusal{
		{`"written_ms":1760000000000`, `"written_ms":-424242`, CodeSchema},
		{`"desired":"applied"`, `"desired":"SECRET"`, CodeSchema},
		{`"unusable_hook_stdin":0`, `"unusable_hook_stdin":-1`, CodeSchema},
		{`[` + statusTeam + `]`, `null`, CodeSchema},
		{`"teams":[`, `"teams":[` + statusTeam + `,`, CodeSchema},
		{`"team":"alpha"`, `"team":"SECRET"`, CodeSchema},
		{`"role":"coordinator"`, `"role":"SECRET"`, CodeSchema},
		{`"down":false`, `"down":null`, CodeSchema},
		{`"failed":null`, `"failed":"SECRET"`, CodeSchema},
		{`"delivery":"socket"`, `"delivery":"SECRET"`, CodeSchema},
		{`"pending":0`, `"pending":-1`, CodeSchema},
		{`"pending":0`, `"pending":2049`, CodeSchema},
		{`"missing":0`, `"missing":-1`, CodeSchema},
		{`"missing":0`, `"missing":17`, CodeSchema},
		{`[` + statusTeam + `]`, `[` + teamsJSON(maxTeams+1, strings.ReplaceAll(statusTeam, "alpha", "%s")) + `]`, CodeSchema},
	})
}

// TestStatusCap holds the largest status.json within its cap: every team at the longest id and
// the widest values.
func TestStatusCap(t *testing.T) {
	team := `{"team":"%s","role":"coordinator","down":false,"failed":"unauthorized","delivery":"socket","pending":2048,"missing":16}`
	doc := edit(t, statusDoc, `[`+statusTeam+`]`, `[`+teamsJSON(maxTeams, team)+`]`)
	doc = edit(t, doc, `"unusable_hook_stdin":0`, `"unusable_hook_stdin":9007199254740991`)
	if len(doc) > MaxStatus/2 {
		t.Fatalf("the largest status.json is %d bytes", len(doc))
	}
	accepts(t, DecodeStatus, doc)
}

func TestStatusRoundTrip(t *testing.T) {
	s, err := DecodeStatus([]byte(statusDoc))
	check(t, err)
	roundTrips(t, s, MaxStatus, DecodeStatus)
}
