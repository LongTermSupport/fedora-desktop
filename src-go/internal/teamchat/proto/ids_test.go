package proto

import (
	"strings"
	"testing"
)

const testServer = "myteam.teamchat.internal"

func TestTeamID(t *testing.T) {
	for _, ok := range []string{"a", "my-team", "t0", "a" + strings.Repeat("b", 21)} {
		if !ValidTeamID(ok) {
			t.Errorf("ValidTeamID(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "0team", "-a", "Team", "a_b", "a.b", "a" + strings.Repeat("b", 22), "é"} {
		if ValidTeamID(bad) {
			t.Errorf("ValidTeamID(%q) = true", bad)
		}
	}
}

func TestDisplayName(t *testing.T) {
	for _, ok := range []string{"My team", "é", "Équipe · un", strings.Repeat("x", 64), strings.Repeat("é", 64)} {
		if !ValidDisplayName(ok) {
			t.Errorf("ValidDisplayName(%q) = false", ok)
		}
	}
	for _, bad := range []string{
		"", strings.Repeat("x", 65), "a\nb", "a\tb", "a\x7fb", "a\u0085b", "\xff",
		"a" + string(rune(0x2028)) + "b", "a" + string(rune(0x2029)) + "b",
		"a" + string(rune(0x200b)) + "b", "a" + string(rune(0x202e)) + "b",
	} {
		if ValidDisplayName(bad) {
			t.Errorf("ValidDisplayName(%q) = true", bad)
		}
	}
}

func TestRoomAndEventID(t *testing.T) {
	id := strings.Repeat("A", 20) + "a_-0" + strings.Repeat("z", 19)
	if !ValidRoomID("!"+id) || ValidRoomID("$"+id) || ValidRoomID("!"+id+"x") || ValidRoomID("!"+id[1:]+"=") {
		t.Fatal("room ID grammar wrong")
	}
	if !ValidEventID("$"+id) || ValidEventID("!"+id) || ValidEventID("$"+id[1:]) || ValidEventID("$"+id[1:]+"+") {
		t.Fatal("event ID grammar wrong")
	}
}

func TestSmallGrammars(t *testing.T) {
	cases := []struct {
		name string
		fn   func(string) bool
		ok   []string
		bad  []string
	}{
		{"branch", ValidBranch, []string{"main", "feat/x-1.2_y", strings.Repeat("b", 100)}, []string{"", strings.Repeat("b", 101), "a b", "a:b", "é"}},
		{"feature", ValidFeature, []string{"a", "x-1", "a" + strings.Repeat("b", 31)}, []string{"", "1a", "A", "a_b", "a" + strings.Repeat("b", 32)}},
		{"question", ValidQuestion, []string{"Q1", "Q999999999", "Q10"}, []string{"Q0", "Q01", "q1", "Q", "Q1000000000", "Q1 "}},
		{"human", ValidHumanName, []string{"alice", "a", "bob_2-x", "a" + strings.Repeat("b", 31)}, []string{"", "Alice", "1a", "a.b", "a+b", "admin", "humans", "teamchat", "conduit", "tuwunel", "a" + strings.Repeat("b", 32)}},
	}
	for _, c := range cases {
		for _, s := range c.ok {
			if !c.fn(s) {
				t.Errorf("%s(%q) = false", c.name, s)
			}
		}
		for _, s := range c.bad {
			if c.fn(s) {
				t.Errorf("%s(%q) = true", c.name, s)
			}
		}
	}
	if n, ok := ParseQuestion("Q42"); !ok || n != 42 {
		t.Fatalf("ParseQuestion(Q42) = %d, %v", n, ok)
	}
	if _, ok := ParseQuestion("Q0"); ok {
		t.Fatal("ParseQuestion(Q0) accepted")
	}
	if QuestionName(7) != "Q7" {
		t.Fatal("QuestionName(7)")
	}
}

func TestParseHandle(t *testing.T) {
	h, ok := ParseHandle("myrepo.2+desk.podman")
	if !ok || h.Repo != "myrepo" || h.Seat != "2" || h.Site != "desk" || h.Type != "podman" {
		t.Fatalf("ParseHandle = %+v, %v", h, ok)
	}
	if h.String() != "myrepo.2+desk.podman" || h.Short() != "myrepo.2" || h.Prefix().String() != "myrepo+desk.podman" {
		t.Fatalf("formatting = %s %s %s", h, h.Short(), h.Prefix())
	}
	if n, numbered := h.SeatNumber(); !numbered || n != 2 {
		t.Fatal("seat 2 is not numbered 2")
	}
	named, ok := ParseHandle("my_repo-x.bob+site-1.lxc")
	if !ok {
		t.Fatal("named seat refused")
	}
	if _, numbered := named.SeatNumber(); numbered {
		t.Fatal("a named seat is numbered")
	}
}

func TestParseHandleGrammar(t *testing.T) {
	for _, typ := range []string{"podman", "lxc", "docker", "vm", "host"} {
		if _, ok := ParseHandle("r.1+s." + typ); !ok {
			t.Errorf("type %s refused", typ)
		}
	}
	for _, bad := range []string{
		"", "r.1+s.kvm", "r.0+s.host", "r.01+s.host", "r.1234567+s.host", "R.1+s.host", "-r.1+s.host",
		"r.1+-s.host", "r.1+s-.host", "r.1.s.host", "r1+s.host", "r.1+s", "r.a_b+s.host",
		strings.Repeat("r", 49) + ".1+s.host", "r." + strings.Repeat("a", 13) + "+s.host",
		"r.1+" + strings.Repeat("s", 64) + ".host", "r.1+s.host.", "r.1+s.host\n", "r..1+s.host",
	} {
		if _, ok := ParseHandle(bad); ok {
			t.Errorf("ParseHandle(%q) accepted", bad)
		}
	}
	if _, ok := ParseHandle("r.1+" + strings.Repeat("s", 63) + ".host"); !ok {
		t.Error("a 63-character site refused")
	}
}

func TestProbeHandle(t *testing.T) {
	p := ProbeHandle("desk")
	if p.String() != "teamchat.probe+desk.host" || !p.IsProbe() {
		t.Fatalf("ProbeHandle = %s", p)
	}
	for _, s := range []string{"teamchat.probe+desk.podman", "other.probe+desk.host", "teamchat.1+desk.host"} {
		h, ok := ParseHandle(s)
		if !ok || h.IsProbe() {
			t.Errorf("%s: parsed %v, probe %v", s, ok, h.IsProbe())
		}
	}
}

func TestPrefix(t *testing.T) {
	p, ok := ParsePrefix("myrepo+desk.lxc")
	if !ok || p.String() != "myrepo+desk.lxc" {
		t.Fatalf("ParsePrefix = %+v, %v", p, ok)
	}
	h, ok := p.Numbered(12)
	if !ok || h.String() != "myrepo.12+desk.lxc" {
		t.Fatalf("Numbered(12) = %s, %v", h, ok)
	}
	if _, ok := p.Numbered(0); ok {
		t.Fatal("seat 0 accepted")
	}
	if _, ok := p.Numbered(1000000); ok {
		t.Fatal("seat 1000000 accepted")
	}
	if h, ok := p.Named("bob"); !ok || h.String() != "myrepo.bob+desk.lxc" {
		t.Fatal("Named(bob)")
	}
	if _, ok := p.Named("Bob"); ok {
		t.Fatal("Named(Bob) accepted")
	}
	for _, bad := range []string{"myrepo.1+desk.lxc", "myrepo+desk", "+desk.lxc", "myrepo+desk.kvm"} {
		if _, ok := ParsePrefix(bad); ok {
			t.Errorf("ParsePrefix(%q) accepted", bad)
		}
	}
}

func TestNormalise(t *testing.T) {
	repos := map[string]string{
		"MyRepo":                "myrepo",
		"my.repo":               "my-repo",
		"__-x":                  "x",
		"Ünïcode":               "n-code",
		strings.Repeat("a", 60): strings.Repeat("a", 48),
	}
	for in, want := range repos {
		if got, ok := NormaliseRepo(in); !ok || got != want {
			t.Errorf("NormaliseRepo(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "___", "-.-"} {
		if got, ok := NormaliseRepo(bad); ok {
			t.Errorf("NormaliseRepo(%q) = %q, want refused", bad, got)
		}
	}
	if got, ok := NormaliseSite("Desk_One"); !ok || got != "desk-one" {
		t.Errorf("NormaliseSite(Desk_One) = %q, %v", got, ok)
	}
	for _, bad := range []string{"", "-desk", "desk-", strings.Repeat("s", 64)} {
		if got, ok := NormaliseSite(bad); ok {
			t.Errorf("NormaliseSite(%q) = %q, want refused", bad, got)
		}
	}
}

func TestUserIDs(t *testing.T) {
	agent := "@myrepo.1+desk.podman:" + testServer
	cases := []struct {
		id    string
		class IDClass
	}{
		{agent, IDAgent},
		{"@alice:" + testServer, IDHuman},
		{"@admin:" + testServer, IDAdmin},
		{"@teamchat:" + testServer, IDReserved},
		{"@humans:" + testServer, IDReserved},
		{"@conduit:" + testServer, IDReserved},
		{"@alice:other.teamchat.internal", IDNone},
		{"@alice:" + testServer + ":1", IDNone},
		{"alice:" + testServer, IDNone},
		{"@Alice:" + testServer, IDNone},
		{"@:" + testServer, IDNone},
		{"@alice", IDNone},
		{"", IDNone},
	}
	for _, c := range cases {
		if got := ClassifyUserID(c.id, testServer); got != c.class {
			t.Errorf("ClassifyUserID(%q) = %v, want %v", c.id, got, c.class)
		}
	}
	if lp, ok := Localpart(agent, testServer); !ok || lp != "myrepo.1+desk.podman" {
		t.Fatalf("Localpart = %q, %v", lp, ok)
	}
	if _, ok := Localpart("@a:b:"+testServer, testServer); ok {
		t.Fatal("a localpart split past the first colon")
	}
	if UserID("alice", testServer) != "@alice:"+testServer || AdminID(testServer) != "@admin:"+testServer {
		t.Fatal("UserID or AdminID")
	}
	if h, ok := AgentHandle(agent, testServer); !ok || h.Short() != "myrepo.1" {
		t.Fatal("AgentHandle")
	}
	if _, ok := AgentHandle("@alice:"+testServer, testServer); ok {
		t.Fatal("AgentHandle of a human")
	}
}
