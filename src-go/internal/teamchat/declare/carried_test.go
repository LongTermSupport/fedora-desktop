package declare

import (
	"strings"
	"testing"
)

// carriedFrom is a marker as a declaration gives it.
const carriedFrom = "the bus guest before the rebuild"

// SG6: carried_from is optional free text of description's grammar (1-120 characters, trimmed,
// no control characters); its value is kept as given.
func TestCarriedFromIsOptionalFreeText(t *testing.T) {
	d := validDecl()
	team0(d)["carried_from"] = carriedFrom
	got, err := Decode(encode(t, d))
	if err != nil {
		t.Fatal(err)
	}
	if got.Teams[0].CarriedFrom != carriedFrom || got.Teams[1].CarriedFrom != "" {
		t.Errorf("carried_from %q, %q", got.Teams[0].CarriedFrom, got.Teams[1].CarriedFrom)
	}
	// Each bad value, and the text the refusal must not echo ("" where any echo is harmless).
	for _, bad := range []struct {
		value any
		echo  string
	}{
		{"", ""},
		{" the old guest", " the old guest"},
		{"the old\x01guest", "the old\x01guest"},
		{"the old\nguest", "the old\nguest"},
		{strings.Repeat("g", 121), ""},
		{true, ""},
	} {
		doc := validDecl()
		team0(doc)["carried_from"] = bad.value
		_, err := Decode(encode(t, doc))
		wantField(t, err, "teamchat_teams[0].carried_from", bad.echo)
	}
	d = validDecl()
	team0(d)["carried_from"] = strings.Repeat("g", 120)
	if _, err := Decode(encode(t, d)); err != nil {
		t.Errorf("120 characters: %v", err)
	}
}

// SG6: a marked team writes carried_from last in team.json and in its input, and both read it
// back; approved.json does not hold it (it is not a gated field).
func TestCarriedFromRoundTrips(t *testing.T) {
	d := validDecl()
	team0(d)["name"] = "Alpha"
	team0(d)["carried_from"] = carriedFrom
	got, err := Decode(encode(t, d))
	if err != nil {
		t.Fatal(err)
	}
	raw := got.Teams[0].Canonical()
	want := strings.TrimSuffix(canonicalAlpha, "}") + `,"carried_from":"` + carriedFrom + `"}`
	if string(raw) != want {
		t.Fatalf("team.json:\n%s\nwant\n%s", raw, want)
	}
	back, err := DecodeTeamFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.CarriedFrom != carriedFrom || string(back.Canonical()) != want {
		t.Errorf("team.json round trip: %q\n%s", back.CarriedFrom, back.Canonical())
	}
	in := got.Inputs()[0]
	again, err := DecodeInput(in.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if again.Team.CarriedFrom != carriedFrom || string(again.Encode()) != string(in.Encode()) {
		t.Errorf("input round trip:\n%s", again.Encode())
	}
	if strings.Contains(string(EncodeApproved(in.ID(), in.Gated())), "carried_from") {
		t.Error("approved.json holds carried_from")
	}
}

// inputAlpha is alpha's input as `team apply` reads it on stdin, written before the carried_from
// marker existed.
const inputAlpha = `{"team":{"state":"present","team":"alpha","name":"alpha","server_name":"alpha.teamchat.internal","port":8448,` +
	`"description":"The team that builds the thing.","networks":["192.0.2.0/24"],` +
	`"url":"https://192.0.2.10:8448","humans":["alice","bob"],"coordinator":"myrepo.1+desk.podman",` +
	`"trust":{"kind":"github","org":"example-org","repos":[{"repo":"example-org/thing","branches":["main"]}]}},` +
	`"lxc_members":[],"address":"192.0.2.10"}`

// approvedAlpha is alpha's approved.json, written before the carried_from marker existed.
const approvedAlpha = `{"v":2,"team":"alpha","approved":{"humans":["alice","bob"],"coordinator":"myrepo.1+desk.podman",` +
	`"networks":["192.0.2.0/24"],"url":"https://192.0.2.10:8448",` +
	`"trust":{"kind":"github","org":"example-org","repos":[{"repo":"example-org/thing","branches":["main"]}]},"lxc_members":[]}}`

// SG6: a team that declares no carried_from encodes byte for byte as before the marker existed,
// in team.json, in the input and in approved.json.
func TestUnmarkedTeamEncodesAsBefore(t *testing.T) {
	d := validDecl()
	team0(d)["name"] = "Alpha"
	got, err := Decode(encode(t, d))
	if err != nil {
		t.Fatal(err)
	}
	if raw := got.Teams[0].Canonical(); string(raw) != canonicalAlpha {
		t.Errorf("team.json:\n%s\nwant\n%s", raw, canonicalAlpha)
	}
	d = validDecl()
	d["teamchat_lxc_members"] = []any{}
	got, err = Decode(encode(t, d))
	if err != nil {
		t.Fatal(err)
	}
	in := got.Inputs()[0]
	if raw := in.Encode(); string(raw) != inputAlpha {
		t.Errorf("input:\n%s\nwant\n%s", raw, inputAlpha)
	}
	if raw := EncodeApproved(in.ID(), in.Gated()); string(raw) != approvedAlpha {
		t.Errorf("approved.json:\n%s\nwant\n%s", raw, approvedAlpha)
	}
}
