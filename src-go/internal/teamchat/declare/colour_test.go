package declare

import (
	"strings"
	"testing"
)

func TestColourIsStoredAsHex(t *testing.T) {
	for _, c := range []struct{ declared, want string }{
		{"teal", "#2dd4bf"},
		{"#2DD4BF", "#2dd4bf"},
		{"#99f6e4", "#99f6e4"},
	} {
		d := validDecl()
		team0(d)["colour"] = c.declared
		got, err := Decode(encode(t, d))
		if err != nil {
			t.Fatalf("colour %q: %v", c.declared, err)
		}
		if got.Teams[0].Colour != c.want || got.Teams[1].Colour != "" {
			t.Fatalf("colour %q stored as %q (second team %q)", c.declared, got.Teams[0].Colour, got.Teams[1].Colour)
		}
	}
}

func TestColourRefusalsNameTheFieldAndC7(t *testing.T) {
	for _, c := range []struct {
		name  string
		value any
		text  string
	}{
		{"too dark", "#000000", "too dark to read on a dark terminal"},
		{"not a colour", "chartreuse", "must be one of teal"},
		{"short hex", "#fff", "or #rrggbb"},
		{"not a string", 7, ""},
		{"null", nil, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := validDecl()
			team0(d)["colour"] = c.value
			_, err := Decode(encode(t, d))
			wantField(t, err, "teamchat_teams[0].colour", "")
			if !strings.Contains(err.Error(), c.text) {
				t.Fatalf("the refusal %q lacks %q", err, c.text)
			}
		})
	}
}

func TestColourReachesTheTeamFile(t *testing.T) {
	d := validDecl()
	team0(d)["colour"] = "teal"
	got, err := Decode(encode(t, d))
	if err != nil {
		t.Fatal(err)
	}
	raw := got.Teams[0].Canonical()
	if !strings.Contains(string(raw), `"colour":"#2dd4bf"`) {
		t.Fatalf("canonical lacks the colour: %s", raw)
	}
	back, err := DecodeTeamFile(raw)
	if err != nil || back.Colour != "#2dd4bf" || string(back.Canonical()) != string(raw) {
		t.Fatalf("round trip: %+v, %v", back, err)
	}
	ins := got.Inputs()
	in, err := DecodeInput(ins[0].Encode())
	if err != nil || in.Team.Colour != "#2dd4bf" {
		t.Fatalf("input round trip: %+v, %v", in.Team, err)
	}
	if strings.Contains(string(got.Teams[1].Canonical()), "colour") {
		t.Fatal("an undeclared colour was written")
	}
}
