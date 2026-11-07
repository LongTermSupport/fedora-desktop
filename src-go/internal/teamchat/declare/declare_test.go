package declare

import (
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// teamEntry is a present team that passes every rule.
func teamEntry(id string, port int) map[string]any {
	return map[string]any{
		"team":        id,
		"state":       "present",
		"port":        port,
		"description": "The team that builds the thing.",
		"networks":    []any{"192.0.2.0/24"},
		"humans":      []any{"alice", "bob"},
		"coordinator": "myrepo.1+desk.podman",
		"trust": map[string]any{
			"kind": "github", "org": "example-org",
			"repos": []any{map[string]any{"repo": "example-org/thing", "branches": []any{"main"}}},
		},
	}
}

// networkList is n distinct host prefixes of 203.0.113.0/24 (n up to 256).
func networkList(n int) []any {
	out := make([]any, 0, n)
	for i := range n {
		out = append(out, "203.0.113."+strconv.Itoa(i)+"/32")
	}
	return out
}

func lxcEntry(team string) map[string]any {
	return map[string]any{
		"team": team, "container": "box-1", "user": "agent", "checkout": "/srv/checkouts/thing", "seat": 1,
	}
}

func validDecl() map[string]any {
	return map[string]any{
		"teamchat_address":       "192.0.2.10",
		"teamchat_teams":         []any{teamEntry("alpha", 8448), teamEntry("beta", 8449)},
		"teamchat_lxc_members":   []any{lxcEntry("alpha")},
		"teamchat_element_teams": []any{map[string]any{"team": "alpha"}},
	}
}

func encode(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// obj and arr are a fixture's nested values; a fixture of another shape is a broken test.
func obj(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		panic("fixture: not an object")
	}
	return m
}

func arr(v any) []any {
	a, ok := v.([]any)
	if !ok {
		panic("fixture: not a list")
	}
	return a
}

func teams(d map[string]any) []any { return arr(d["teamchat_teams"]) }

func team0(d map[string]any) map[string]any { return obj(teams(d)[0]) }

func team1(d map[string]any) map[string]any { return obj(teams(d)[1]) }

func trust0(d map[string]any) map[string]any { return obj(team0(d)["trust"]) }

func lxc0(d map[string]any) map[string]any { return obj(arr(d["teamchat_lxc_members"])[0]) }

func TestDecodeValid(t *testing.T) {
	d, err := Decode(encode(t, validDecl()))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if d.Address != "192.0.2.10" || len(d.Teams) != 2 || len(d.LXC) != 1 || len(d.Element) != 1 {
		t.Fatalf("decoded %+v", d)
	}
	checkDefaults(t, d)
}

func checkDefaults(t *testing.T, d Declaration) {
	t.Helper()
	a := d.Teams[0]
	if a.ID.String() != "alpha" || a.Name != "alpha" || a.ServerName != "alpha.teamchat.internal" ||
		a.URL != "https://192.0.2.10:8448" || a.State != StatePresent || a.Trust.Org != "example-org" {
		t.Fatalf("defaults not filled: %+v", a)
	}
	m := d.LXC[0]
	if !m.HumanText || m.Service || m.State != StatePresent || m.User.String() != "agent" {
		t.Fatalf("LXC defaults: %+v", m)
	}
	if d.Element[0].Team.String() != "alpha" {
		t.Fatalf("element %+v", d.Element)
	}
}

func TestDecodeEmptyAndMinimal(t *testing.T) {
	d, err := Decode([]byte(`{}`))
	if err != nil || len(d.Teams) != 0 || d.Address != "" {
		t.Fatalf("empty: %+v, %v", d, err)
	}
	d, err = Decode([]byte(`{"teamchat_teams":[{"team":"gone","state":"absent","purge":true}]}`))
	if err != nil || d.Teams[0].State != StateAbsent || !d.Teams[0].Purge {
		t.Fatalf("absent: %+v, %v", d, err)
	}
}

// wantField asserts err is a FieldError naming field, and that its text does not echo value.
func wantField(t *testing.T, err error, field, value string) {
	t.Helper()
	fe, ok := errors.AsType[*FieldError](err)
	if !ok {
		t.Fatalf("err = %v, want a FieldError at %s", err, field)
	}
	if fe.Field != field {
		t.Fatalf("field %q (%v), want %q", fe.Field, err, field)
	}
	if value != "" && strings.Contains(err.Error(), value) {
		t.Fatalf("the error %q echoes the value %q", err, value)
	}
}

type refusal struct {
	name, field, value string
	edit               func(d map[string]any)
}

func teamRefusals() []refusal {
	return []refusal{
		{"unknown top key", "teamchat_extra", "", func(d map[string]any) { d["teamchat_extra"] = 1 }},
		{"unknown team key", "teamchat_teams[0].forge_api", "", func(d map[string]any) { team0(d)["forge_api"] = "x" }},
		{"element base_url", "teamchat_element_teams[0].base_url", "evil", func(d map[string]any) {
			d["teamchat_element_teams"] = []any{map[string]any{"team": "alpha", "base_url": "http://evil"}}
		}},
		{"missing humans", "teamchat_teams[0].humans", "", func(d map[string]any) { delete(team0(d), "humans") }},
		{"missing state", "teamchat_teams[0].state", "", func(d map[string]any) { delete(team0(d), "state") }},
		{"string port", "teamchat_teams[0].port", "", func(d map[string]any) { team0(d)["port"] = "8448" }},
		{"number name", "teamchat_teams[0].name", "", func(d map[string]any) { team0(d)["name"] = 7 }},
		{"null coordinator", "teamchat_teams[0].coordinator", "", func(d map[string]any) { team0(d)["coordinator"] = nil }},
		{"bad team id", "teamchat_teams[0].team", "Alpha", func(d map[string]any) { team0(d)["team"] = "Alpha" }},
		{"reserved team id", "teamchat_teams[0].team", "", func(d map[string]any) { team0(d)["team"] = "ctl" }},
		{"duplicate team", "teamchat_teams[1].team", "", func(d map[string]any) { team1(d)["team"] = "alpha" }},
		{"duplicate port", "teamchat_teams[1].port", "", func(d map[string]any) { team1(d)["port"] = 8448 }},
		{"bad state", "teamchat_teams[0].state", "", func(d map[string]any) { team0(d)["state"] = "gone" }},
		{"purge present", "teamchat_teams[0].purge", "", func(d map[string]any) { team0(d)["purge"] = true }},
		{"port zero", "teamchat_teams[0].port", "", func(d map[string]any) { team0(d)["port"] = 0 }},
		{"server name", "teamchat_teams[0].server_name", "", func(d map[string]any) { team0(d)["server_name"] = "Upper.example" }},
		{"server name one label", "teamchat_teams[0].server_name", "", func(d map[string]any) { team0(d)["server_name"] = "local" }},
		{"description", "teamchat_teams[0].description", "", func(d map[string]any) { team0(d)["description"] = " padded" }},
		{"display name", "teamchat_teams[0].name", "", func(d map[string]any) { team0(d)["name"] = "a\u200eb" }},
		{"no humans", "teamchat_teams[0].humans", "", func(d map[string]any) { team0(d)["humans"] = []any{} }},
		{"duplicate human", "teamchat_teams[0].humans[1]", "", func(d map[string]any) { team0(d)["humans"] = []any{"alice", "alice"} }},
		{"reserved human", "teamchat_teams[0].humans[1]", "", func(d map[string]any) { team0(d)["humans"] = []any{"alice", "tuwunel"} }},
		{"reserved conduit", "teamchat_teams[0].humans[0]", "", func(d map[string]any) { team0(d)["humans"] = []any{"conduit"} }},
		{"coordinator handle", "teamchat_teams[0].coordinator", "", func(d map[string]any) { team0(d)["coordinator"] = "alice" }},
		{"coordinator probe", "teamchat_teams[0].coordinator", "", func(d map[string]any) {
			team0(d)["coordinator"] = "teamchat.probe+desk.host"
		}},
		{"trust key", "teamchat_teams[0].trust.forge_api", "", func(d map[string]any) {
			trust0(d)["forge_api"] = "https://api.github.com"
		}},
		{"trust kind", "teamchat_teams[0].trust", "", func(d map[string]any) { trust0(d)["kind"] = "gitlab" }},
		{"trust missing", "teamchat_teams[0].trust", "", func(d map[string]any) { delete(team0(d), "trust") }},
		{"teams not a list", "teamchat_teams", "", func(d map[string]any) { d["teamchat_teams"] = map[string]any{} }},
		{"entry not an object", "teamchat_teams[0]", "", func(d map[string]any) { teams(d)[0] = "alpha" }},
	}
}

func networkRefusals() []refusal {
	return []refusal{
		// X1: a team declares networks; listen and allow_from are unknown keys (the addresses are
		// derived at apply time).
		{"listen is not a key", "teamchat_teams[0].listen", "", func(d map[string]any) {
			team0(d)["listen"] = []any{"192.0.2.10"}
		}},
		{"allow_from is not a key", "teamchat_teams[0].allow_from", "", func(d map[string]any) {
			team0(d)["allow_from"] = []any{"192.0.2.0/24"}
		}},
		{"networks missing", "teamchat_teams[0].networks", "", func(d map[string]any) { delete(team0(d), "networks") }},
		{"networks not a list", "teamchat_teams[0].networks", "", func(d map[string]any) { team0(d)["networks"] = "192.0.2.0/24" }},
		{"networks host bits", "teamchat_teams[0].networks[0]", "", func(d map[string]any) { team0(d)["networks"] = []any{"192.0.2.1/24"} }},
		{"networks not a prefix", "teamchat_teams[0].networks[0]", "", func(d map[string]any) { team0(d)["networks"] = []any{"x"} }},
		{"networks a bare address", "teamchat_teams[0].networks[0]", "", func(d map[string]any) { team0(d)["networks"] = []any{"192.0.2.1"} }},
		{"networks not canonical", "teamchat_teams[0].networks[0]", "", func(d map[string]any) { team0(d)["networks"] = []any{"2001:DB8::/32"} }},
		{"networks zone", "teamchat_teams[0].networks[0]", "", func(d map[string]any) { team0(d)["networks"] = []any{"fe80::1%eth0/64"} }},
		{"networks duplicate", "teamchat_teams[0].networks[1]", "", func(d map[string]any) {
			team0(d)["networks"] = []any{"192.0.2.0/24", "192.0.2.0/24"}
		}},
		{"networks over 32", "teamchat_teams[0].networks", "", func(d map[string]any) { team0(d)["networks"] = networkList(33) }},
		{"url path", "teamchat_teams[0].url", "", func(d map[string]any) { team0(d)["url"] = "http://192.0.2.10:8448/x" }},
		{"url user", "teamchat_teams[0].url", "", func(d map[string]any) { team0(d)["url"] = "http://u@192.0.2.10:8448" }},
		{"url scheme", "teamchat_teams[0].url", "", func(d map[string]any) { team0(d)["url"] = "ftp://192.0.2.10" }},
		{"url host", "teamchat_teams[0].url", "", func(d map[string]any) { team0(d)["url"] = "http://Bad_Host:1" }},
		{"url port", "teamchat_teams[0].url", "", func(d map[string]any) { team0(d)["url"] = "http://192.0.2.10:0" }},
		{"url query", "teamchat_teams[0].url", "", func(d map[string]any) { team0(d)["url"] = "http://192.0.2.10?a=b" }},
		{"url unparsable", "teamchat_teams[0].url", "", func(d map[string]any) { team0(d)["url"] = "http://[::1" }},
		{"no url, no address", "teamchat_teams[0].url", "", func(d map[string]any) { delete(d, "teamchat_address") }},
		{"address wildcard", "teamchat_address", "", func(d map[string]any) { d["teamchat_address"] = "::" }},
	}
}

func memberRefusals() []refusal {
	return []refusal{
		{"lxc team absent", "teamchat_lxc_members[0].team", "", func(d map[string]any) {
			d["teamchat_lxc_members"] = []any{lxcEntry("nosuch")}
		}},
		{"lxc container", "teamchat_lxc_members[0].container", "", func(d map[string]any) { lxc0(d)["container"] = "-box" }},
		{"lxc user", "teamchat_lxc_members[0].user", "", func(d map[string]any) { lxc0(d)["user"] = "Root" }},
		{"lxc checkout", "teamchat_lxc_members[0].checkout", "", func(d map[string]any) { lxc0(d)["checkout"] = "/srv/checkouts/../x" }},
		{"lxc checkout root", "teamchat_lxc_members[0].checkout", "", func(d map[string]any) { lxc0(d)["checkout"] = "/" }},
		{"lxc site", "teamchat_lxc_members[0].site", "", func(d map[string]any) { lxc0(d)["site"] = "Desk" }},
		{"lxc checkout two lines", "teamchat_lxc_members[0].checkout", "", func(d map[string]any) { lxc0(d)["checkout"] = "/srv/a\nb" }},
		{"lxc checkout tab", "teamchat_lxc_members[0].checkout", "", func(d map[string]any) { lxc0(d)["checkout"] = "/srv/a\tb" }},
		{"lxc checkout escape", "teamchat_lxc_members[0].checkout", "", func(d map[string]any) { lxc0(d)["checkout"] = "/srv/\x1b[31mx" }},
		{"lxc checkout NUL", "teamchat_lxc_members[0].checkout", "", func(d map[string]any) { lxc0(d)["checkout"] = "/srv/a\x00b" }},
		{"lxc checkout bidi", "teamchat_lxc_members[0].checkout", "", func(d map[string]any) { lxc0(d)["checkout"] = "/srv/a\u202eb" }},
		{"lxc service checkout a unit cannot carry", "teamchat_lxc_members[0].checkout", "", func(d map[string]any) {
			lxc0(d)["checkout"], lxc0(d)["service"] = "/srv/a b", true
		}},
		{"lxc seat", "teamchat_lxc_members[0].seat", "", func(d map[string]any) { lxc0(d)["seat"] = 0 }},
		{"lxc seat missing", "teamchat_lxc_members[0].seat", "", func(d map[string]any) { delete(lxc0(d), "seat") }},
		{"lxc seat too high", "teamchat_lxc_members[0].seat", "", func(d map[string]any) { lxc0(d)["seat"] = 10000 }},
		{"lxc state", "teamchat_lxc_members[0].state", "", func(d map[string]any) { lxc0(d)["state"] = "gone" }},
		{"lxc human_text string", "teamchat_lxc_members[0].human_text", "", func(d map[string]any) { lxc0(d)["human_text"] = "yes" }},
		{"lxc duplicate", "teamchat_lxc_members[1]", "", func(d map[string]any) {
			d["teamchat_lxc_members"] = []any{lxcEntry("alpha"), lxcEntry("alpha")}
		}},
		{"lxc duplicate under another user", "teamchat_lxc_members[1]", "", func(d map[string]any) {
			other := lxcEntry("alpha")
			other["user"] = "agent2"
			d["teamchat_lxc_members"] = []any{lxcEntry("alpha"), other}
		}},
		{"element unknown team", "teamchat_element_teams[0].team", "", func(d map[string]any) {
			d["teamchat_element_teams"] = []any{map[string]any{"team": "nosuch"}}
		}},
		{"element twice", "teamchat_element_teams[1].team", "", func(d map[string]any) {
			d["teamchat_element_teams"] = []any{map[string]any{"team": "alpha"}, map[string]any{"team": "alpha"}}
		}},
	}
}

func TestDecodeRefusesNamingTheField(t *testing.T) {
	for _, group := range [][]refusal{teamRefusals(), networkRefusals(), memberRefusals()} {
		for _, c := range group {
			t.Run(c.name, func(t *testing.T) {
				d := validDecl()
				c.edit(d)
				_, err := Decode(encode(t, d))
				wantField(t, err, c.field, c.value)
			})
		}
	}
}

// X1: a team declares 0 to 32 networks, each a canonical masked CIDR, IPv4 and IPv6 alike.
func TestNetworksBoundsAndFamilies(t *testing.T) {
	for name, nets := range map[string][]any{
		"none":       {},
		"thirty-two": networkList(32),
		"both families": {
			"192.0.2.0/24", "198.51.100.7/32", "2001:db8::/32", "2001:db8:1::5/128",
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := validDecl()
			team0(d)["networks"] = nets
			got, err := Decode(encode(t, d))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if len(got.Teams[0].Networks) != len(nets) {
				t.Fatalf("networks %v, want %d entries", got.Teams[0].Networks, len(nets))
			}
		})
	}
}

// X6 (the part the declaration alone can judge): the url host is an IP literal or a DNS name, and
// whether an IP literal is a derived listen address is judged where the listen set is known, in
// `team apply`; here neither a name nor an address outside the networks is refused.
func TestURLHostIsAnAddressOrANameHere(t *testing.T) {
	for _, url := range []string{"https://chat.example.com:8448", "https://203.0.113.9:8448", "https://[2001:db8::1]:8448"} {
		d := validDecl()
		team0(d)["url"] = url
		if _, err := Decode(encode(t, d)); err != nil {
			t.Errorf("url %s refused: %v", url, err)
		}
	}
}

// X12 (PR #128 review S2): plain http is for a loopback IP literal alone, by matrix's one rule
// (127.0.0.0/8 and ::1), so `declare check` refuses any other http url, naming the field; and the
// url a team gets when it declares none is https.
func TestPlainHTTPIsForALoopbackLiteralAlone(t *testing.T) {
	for url, refused := range map[string]bool{
		"http://192.0.2.10:8448":       true,
		"http://chat.example.com:8448": true,
		"http://[2001:db8::1]:8448":    true,
		"http://127.0.0.1:8448":        false,
		"http://127.0.0.2:8448":        false,
		"http://[::1]:8448":            false,
		"https://192.0.2.10:8448":      false,
	} {
		d := validDecl()
		team0(d)["url"] = url
		_, err := Decode(encode(t, d))
		switch {
		case refused:
			wantField(t, err, "teamchat_teams[0].url", "")
			if err != nil && !strings.Contains(err.Error(), "plain http is only for a loopback IP literal") {
				t.Errorf("url %s: the refusal does not say why: %v", url, err)
			}
		case err != nil:
			t.Errorf("url %s refused: %v", url, err)
		}
	}
	for address, want := range map[string]string{"192.0.2.10": "https://192.0.2.10:8448", "2001:db8::1": "https://[2001:db8::1]:8448"} {
		d := validDecl()
		d["teamchat_address"] = address
		got, err := Decode(encode(t, d))
		if err != nil {
			t.Fatalf("address %s: %v", address, err)
		}
		if got.Teams[0].URL != want {
			t.Errorf("address %s: default url %s, want %s", address, got.Teams[0].URL, want)
		}
	}
}

func TestDecodeStrictJSON(t *testing.T) {
	for _, raw := range []string{
		`{"teamchat_teams":[],"teamchat_teams":[]}`,
		`{} {}`,
		`[]`,
		`{"teamchat_teams":[{"team":"a","state":"absent","port":1.5}]}`,
	} {
		if _, err := Decode([]byte(raw)); err == nil {
			t.Fatalf("Decode(%s) accepted", raw)
		}
	}
	if _, err := Decode([]byte(`{"x":"` + strings.Repeat("a", maxInput) + `"}`)); err == nil ||
		!strings.Contains(err.Error(), "over") {
		t.Fatalf("an oversized input: %v", err)
	}
}

func TestAbsentTeamChecksWhatItCarries(t *testing.T) {
	_, err := Decode([]byte(`{"teamchat_teams":[{"team":"gone","state":"absent","humans":["admin"]}]}`))
	wantField(t, err, "teamchat_teams[0].humans[0]", "")
	_, err = Decode([]byte(`{"teamchat_teams":[{"team":"gone","state":"absent","purge":"yes"}]}`))
	wantField(t, err, "teamchat_teams[0].purge", "")
}

func TestLXCEntryOfAnAbsentTeamIsRefused(t *testing.T) {
	d := validDecl()
	team0(d)["state"] = "absent"
	_, err := Decode(encode(t, d))
	wantField(t, err, "teamchat_lxc_members[0].team", "")
}

func TestOneUserHoldsManySeats(t *testing.T) {
	otherRepo := lxcEntry("alpha")
	otherRepo["checkout"] = "/srv/checkouts/other"
	worker := lxcEntry("alpha")
	worker["seat"] = 2
	d := validDecl()
	d["teamchat_lxc_members"] = []any{lxcEntry("alpha"), otherRepo, worker}
	got, err := Decode(encode(t, d))
	if err != nil {
		t.Fatalf("two repos and two seats of one user: %v", err)
	}
	if len(got.LXC) != 3 || got.LXC[1].Checkout != "/srv/checkouts/other" || got.LXC[2].Seat != 2 {
		t.Fatalf("decoded %+v", got.LXC)
	}
}

func TestExplicitValuesKept(t *testing.T) {
	d := validDecl()
	team0(d)["name"] = "Alpha team"
	team0(d)["server_name"] = "alpha.example.org"
	team0(d)["url"] = "https://[2001:db8::1]:8448"
	m := lxc0(d)
	m["site"], m["seat"], m["human_text"], m["service"], m["state"] = "desk", 3, false, true, "absent"
	got, err := Decode(encode(t, d))
	if err != nil {
		t.Fatal(err)
	}
	a := got.Teams[0]
	if a.Name != "Alpha team" || a.ServerName != "alpha.example.org" || a.URL != "https://[2001:db8::1]:8448" {
		t.Fatalf("explicit values: %+v", a)
	}
	if l := got.LXC[0]; l.Site != "desk" || l.Seat != 3 || l.HumanText || !l.Service || l.State != StateAbsent {
		t.Fatalf("explicit LXC values: %+v", l)
	}
}

func TestInputs(t *testing.T) {
	d, err := Decode(encode(t, validDecl()))
	if err != nil {
		t.Fatal(err)
	}
	ins := d.Inputs()
	if len(ins) != 2 || ins[0].Team.ID.String() != "alpha" || len(ins[0].LXC) != 1 || len(ins[1].LXC) != 0 {
		t.Fatalf("inputs %+v", ins)
	}
	raw := ins[0].Encode()
	back, err := DecodeInput(raw)
	if err != nil {
		t.Fatalf("DecodeInput(Encode): %v\n%s", err, raw)
	}
	if string(back.Encode()) != string(raw) {
		t.Fatalf("round trip:\n%s\n%s", back.Encode(), raw)
	}
}

func TestDecodeInputRefuses(t *testing.T) {
	team := teamEntry("alpha", 8448)
	cases := []struct {
		name, field string
		in          map[string]any
	}{
		{"absent team", "team.state", map[string]any{"team": map[string]any{"team": "alpha", "state": "absent"}}},
		{"other team's lxc", "lxc_members[0].team", map[string]any{
			"team": team, "address": "192.0.2.10", "lxc_members": []any{lxcEntry("beta")},
		}},
		{"unknown key", "extra", map[string]any{"team": team, "address": "192.0.2.10", "extra": 1}},
		{"no team", "team", map[string]any{"address": "192.0.2.10"}},
		{"no url", "team.url", map[string]any{"team": team}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := DecodeInput(encode(t, c.in))
			wantField(t, err, c.field, "")
		})
	}
	if _, err := DecodeInput([]byte(`{`)); err == nil {
		t.Fatal("broken JSON accepted")
	}
}

const canonicalAlpha = `{"v":2,"team":"alpha","name":"Alpha","server_name":"alpha.teamchat.internal","port":8448,` +
	`"description":"The team that builds the thing.","networks":["192.0.2.0/24"],` +
	`"url":"https://192.0.2.10:8448","humans":["alice","bob"],"coordinator":"myrepo.1+desk.podman",` +
	`"trust":{"kind":"github","org":"example-org","repos":[{"repo":"example-org/thing","branches":["main"]}]}}`

func TestCanonicalTeamFile(t *testing.T) {
	d := validDecl()
	team0(d)["name"] = "Alpha"
	got, err := Decode(encode(t, d))
	if err != nil {
		t.Fatal(err)
	}
	raw := got.Teams[0].Canonical()
	if string(raw) != canonicalAlpha {
		t.Fatalf("canonical:\n%s\nwant\n%s", raw, canonicalAlpha)
	}
	back, err := DecodeTeamFile(raw)
	if err != nil {
		t.Fatalf("DecodeTeamFile: %v", err)
	}
	if string(back.Canonical()) != canonicalAlpha {
		t.Fatalf("round trip:\n%s", back.Canonical())
	}
	delete(team0(d), "coordinator")
	got, err = Decode(encode(t, d))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got.Teams[0].Canonical()), "coordinator") {
		t.Fatal("an undeclared coordinator was written")
	}
}

func TestDecodeTeamFileRefuses(t *testing.T) {
	for _, bad := range []string{
		strings.Replace(canonicalAlpha, `"v":2`, `"v":3`, 1),
		strings.Replace(canonicalAlpha, `"port":8448,`, ``, 1),
		strings.Replace(canonicalAlpha, `"v":2,`, `"v":2,"state":"present",`, 1),
		strings.Replace(canonicalAlpha, `"v":2,`, ``, 1),
		strings.Replace(canonicalAlpha, `"networks":["192.0.2.0/24"],`, ``, 1),
		strings.Replace(canonicalAlpha, `"networks":["192.0.2.0/24"],`, `"listen":["192.0.2.10"],"allow_from":["192.0.2.0/24"],`, 1),
		`{`,
	} {
		if _, err := DecodeTeamFile([]byte(bad)); err == nil {
			t.Fatalf("DecodeTeamFile accepted %s", bad)
		}
	}
}

// v1Alpha is alpha's team.json as version 1 wrote it, with listen and allow_from.
const v1Alpha = `{"v":1,"team":"alpha","name":"Alpha","server_name":"alpha.teamchat.internal","port":8448,` +
	`"description":"The team that builds the thing.","listen":["192.0.2.10"],"allow_from":["192.0.2.0/24"],` +
	`"url":"http://192.0.2.10:8448","humans":["alice","bob"],"coordinator":"myrepo.1+desk.podman",` +
	`"trust":{"kind":"github","org":"example-org","repos":[{"repo":"example-org/thing","branches":["main"]}]}}`

// X1: a version 1 file is refused, and the refusal names the version, whatever else the file holds.
func TestDecodeTeamFileRefusesVersion1ByName(t *testing.T) {
	_, err := DecodeTeamFile([]byte(v1Alpha))
	wantField(t, err, "team.json.v", "")
	if err == nil || !strings.Contains(err.Error(), "version 1") {
		t.Fatalf("the refusal does not name the version: %v", err)
	}
	_, err = DecodeTeamFile([]byte(strings.Replace(canonicalAlpha, `"v":2`, `"v":7`, 1)))
	if err == nil || !strings.Contains(err.Error(), "version 7") {
		t.Fatalf("a version 7 file: %v", err)
	}
}

// X1: team.json lists the keys in a fixed order with networks where listen and allow_from were,
// and carries neither of them.
func TestTeamFileKeysAreTheVersion2List(t *testing.T) {
	d, err := Decode(encode(t, validDecl()))
	if err != nil {
		t.Fatal(err)
	}
	raw := string(d.Teams[0].Canonical())
	for _, gone := range []string{`"listen"`, `"allow_from"`} {
		if strings.Contains(raw, gone) {
			t.Fatalf("team.json carries %s: %s", gone, raw)
		}
	}
	if !strings.HasPrefix(raw, `{"v":2,`) || !strings.Contains(raw, `"networks":["192.0.2.0/24"]`) {
		t.Fatalf("team.json %s", raw)
	}
	if !slices.Equal(d.Teams[0].Networks, []string{"192.0.2.0/24"}) {
		t.Fatalf("networks %v", d.Teams[0].Networks)
	}
}

func TestFieldErrorText(t *testing.T) {
	err := error(&FieldError{Field: "teamchat_teams[0].port", Problem: "is not an integer"})
	if err.Error() != "declaration: teamchat_teams[0].port is not an integer" {
		t.Fatalf("text %q", err)
	}
	// A key is input too: one with a control character is quoted, so a refusal never forges a
	// CHANGED or SECURITY line in what the play prints.
	_, err = Decode([]byte(`{"teamchat_teams":[{"team":"alpha","state":"absent","x\nCHANGED\tforged\u2028":1}]}`))
	if err == nil || strings.ContainsAny(err.Error(), "\n\t\u2028") ||
		!strings.Contains(err.Error(), `teamchat_teams[0]."x\nCHANGED\tforged`+`\u2028" is not a known key`) {
		t.Fatalf("a control character in a key: %q", err)
	}
}
