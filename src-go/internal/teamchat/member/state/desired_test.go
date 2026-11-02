package state

import (
	"strings"
	"testing"
)

const desiredDoc = `{"v":1,"session":"s1","generation":3,"written_ms":0,"seats":[{"team":"alpha","seat":"2"}],` +
	`"farewell":[{"team":"beta","seat":"1","reason":"released"}],"inbound":"accept"}`

func TestDecodeDesired(t *testing.T) {
	empty := edit(t, edit(t, desiredDoc, `[{"team":"alpha","seat":"2"}]`, `[]`), `[{"team":"beta","seat":"1","reason":"released"}]`, `[]`)
	accepts(t, DecodeDesired, desiredDoc, empty,
		edit(t, empty, `"accept"`, `"refuse"`),
		edit(t, desiredDoc, `"team":"beta","seat":"1"`, `"team":"alpha","seat":"1"`),
		edit(t, desiredDoc, `"seats":[`, `"seats":[{"team":"beta","seat":"lead"},`))
	for _, reason := range []string{FarewellReleased, FarewellRemoved, FarewellParked} {
		accepts(t, DecodeDesired, edit(t, desiredDoc, `"released"`, `"`+reason+`"`))
	}
	d, err := DecodeDesired([]byte(desiredDoc))
	if err != nil || d.Session.String() != "s1" || d.Generation != 3 || d.Seats[0].Team.String() != "alpha" || d.Farewell[0].Reason != FarewellReleased {
		t.Fatalf("DecodeDesired = %+v, %v", d, err)
	}
	strictRefusals(t, desiredFile, desiredDoc, "generation", MaxDesired, DecodeDesired)
	refusesAll(t, desiredFile, desiredDoc, DecodeDesired, []refusal{
		{`"session":"s1"`, `"session":"../SECRET"`, CodeSchema},
		{`"session":"s1"`, `"session":null`, CodeSchema},
		{`"generation":3`, `"generation":-1`, CodeSchema},
		{`"written_ms":0`, `"written_ms":-424242`, CodeSchema},
		{`[{"team":"alpha","seat":"2"}]`, `null`, CodeSchema},
		{`[{"team":"beta","seat":"1","reason":"released"}]`, `null`, CodeSchema},
		{`{"team":"alpha","seat":"2"}`, `{"team":"SECRET","seat":"2"}`, CodeSchema},
		{`{"team":"alpha","seat":"2"}`, `{"team":"alpha"}`, CodeSchema},
		{`"released"`, `"SECRET"`, CodeSchema},
		{`"inbound":"accept"`, `"inbound":"SECRET"`, CodeSchema},
		{`"seat":"2"`, `"seat":"SECRET"`, CodeSeat},
		{`"seat":"1"`, `"seat":"0"`, CodeSeat},
		{`"seats":[`, `"seats":[{"team":"alpha","seat":"3"},`, CodeSeat},
		{`"seats":[`, `"seats":[{"team":"alpha","seat":"2"},`, CodeSeat},
		{`"team":"beta","seat":"1"`, `"team":"alpha","seat":"2"`, CodeSeat},
		{`"farewell":[`, `"farewell":[{"team":"beta","seat":"1","reason":"parked"},`, CodeSeat},
		{`[{"team":"alpha","seat":"2"}]`, `[` + teamsJSON(maxDesiredSeats+1, `{"team":"%s","seat":"2"}`) + `]`, CodeSchema},
		{`[{"team":"beta","seat":"1","reason":"released"}]`, `[` + teamsJSON(maxDesiredSeats+1, `{"team":"%s","seat":"1","reason":"released"}`) + `]`, CodeSchema},
	})
}

// TestDesiredCap holds the largest desired.json within its cap: every seat and farewell at the
// longest team id and seat.
func TestDesiredCap(t *testing.T) {
	seat := strings.Repeat("s", 12)
	doc := edit(t, desiredDoc, `[{"team":"alpha","seat":"2"}]`, `[`+teamsJSON(maxDesiredSeats, `{"team":"%s","seat":"`+seat+`"}`)+`]`)
	doc = edit(t, doc, `[{"team":"beta","seat":"1","reason":"released"}]`,
		`[`+teamsJSON(maxDesiredSeats, `{"team":"%s","seat":"`+strings.Repeat("f", 12)+`","reason":"released"}`)+`]`)
	doc = edit(t, doc, `"session":"s1"`, `"session":"`+strings.Repeat("s", 128)+`"`)
	doc = edit(t, doc, `"generation":3,"written_ms":0`, `"generation":9007199254740991,"written_ms":253402300799999`)
	if len(doc) > MaxDesired/2 {
		t.Fatalf("the largest desired.json is %d bytes", len(doc))
	}
	accepts(t, DecodeDesired, doc)
}

func TestDesiredRoundTrip(t *testing.T) {
	d, err := DecodeDesired([]byte(desiredDoc))
	check(t, err)
	roundTrips(t, d, MaxDesired, DecodeDesired)
}
