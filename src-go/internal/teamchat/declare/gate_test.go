package declare

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// inputDoc is alpha's one-team input, as `team apply` reads it.
func inputDoc() map[string]any {
	return map[string]any{
		"team":        teamEntry("alpha", 8448),
		"lxc_members": []any{lxcEntry("alpha")},
		"address":     "192.0.2.10",
	}
}

func readInput(t *testing.T, doc map[string]any) Input {
	t.Helper()
	in, err := DecodeInput(encode(t, doc))
	if err != nil {
		t.Fatalf("DecodeInput: %v", err)
	}
	return in
}

func approvedOf(t *testing.T, doc map[string]any) *Gated {
	t.Helper()
	g := readInput(t, doc).Gated()
	return &g
}

// mustTeam is a fixture's team id; an invalid one is a broken test.
func mustTeam(s string) paths.TeamID {
	id, err := paths.ParseTeamID(s)
	if err != nil {
		panic("fixture: " + err.Error())
	}
	return id
}

func docTeam(doc map[string]any) map[string]any { return obj(doc["team"]) }

func TestGatedIsSorted(t *testing.T) {
	doc := inputDoc()
	docTeam(doc)["humans"] = []any{"bob", "alice"}
	second := lxcEntry("alpha")
	second["container"] = "a-box"
	second["human_text"] = false
	doc["lxc_members"] = []any{lxcEntry("alpha"), second}
	g := readInput(t, doc).Gated()
	if !slices.Equal(g.Humans, []string{"alice", "bob"}) {
		t.Fatalf("humans %v", g.Humans)
	}
	if len(g.LXC) != 2 || g.LXC[0].String() != "a-box/agent,human_text=false" ||
		g.LXC[1].String() != "box-1/agent,human_text=true" {
		t.Fatalf("lxc %v", g.LXC)
	}
	if g.URL != "http://192.0.2.10:8448" || g.Coordinator != "myrepo.1+desk.podman" {
		t.Fatalf("scalars %+v", g)
	}
}

func TestParseLXCEntry(t *testing.T) {
	e, err := ParseLXCEntry("box-1/agent,human_text=false")
	if err != nil || e.Container != "box-1" || e.User.String() != "agent" || e.HumanText {
		t.Fatalf("parsed %+v, %v", e, err)
	}
	for _, bad := range []string{
		"", "box-1/agent", "box-1,human_text=true", "box-1/agent,human_text=TRUE", "box-1/agent,human_text=1",
		"-box/agent,human_text=true", "box-1/Agent,human_text=true", "box-1/agent/x,human_text=true",
	} {
		if _, err := ParseLXCEntry(bad); err == nil {
			t.Errorf("ParseLXCEntry(%q) accepted", bad)
		}
	}
}

func TestCompareANewTeam(t *testing.T) {
	got := Compare(nil, readInput(t, inputDoc()))
	want := []Difference{{Field: "team", Old: "-", New: "new"}}
	if !slices.Equal(got, want) {
		t.Fatalf("Compare(nil) = %v", got)
	}
	if line := got[0].Line(mustTeam("alpha")); line != "SECURITY\talpha\tteam\t- -> new" {
		t.Fatalf("line %q", line)
	}
}

func TestCompareUngatedAndReorderedIsNoDifference(t *testing.T) {
	approved := approvedOf(t, inputDoc())
	doc := inputDoc()
	team := docTeam(doc)
	team["humans"] = []any{"bob", "alice"}
	team["name"] = "Alpha"
	team["description"] = "Another description."
	lxc := obj(arr(doc["lxc_members"])[0])
	lxc["checkout"] = "/srv/checkouts/other"
	lxc["site"] = "desk"
	lxc["seat"] = 7
	lxc["service"] = true
	lxc["state"] = "absent"
	if got := Compare(approved, readInput(t, doc)); len(got) != 0 {
		t.Fatalf("differences %v", got)
	}
}

type compareCase struct {
	name string
	edit func(doc map[string]any)
	want []Difference
}

func compareCases() []compareCase {
	return []compareCase{
		{
			"human added", func(d map[string]any) { docTeam(d)["humans"] = []any{"alice", "bob", "carol"} },
			[]Difference{{"humans", "-", "carol"}},
		},
		{
			"human removed", func(d map[string]any) { docTeam(d)["humans"] = []any{"alice"} },
			[]Difference{{"humans", "bob", "-"}},
		},
		{
			"coordinator changed", func(d map[string]any) { docTeam(d)["coordinator"] = "other.2+desk.podman" },
			[]Difference{{"coordinator", "myrepo.1+desk.podman", "other.2+desk.podman"}},
		},
		{
			"coordinator removed", func(d map[string]any) { delete(docTeam(d), "coordinator") },
			[]Difference{{"coordinator", "myrepo.1+desk.podman", "-"}},
		},
		{"allow_from added", func(d map[string]any) {
			docTeam(d)["allow_from"] = []any{"192.0.2.0/24", "198.51.100.0/24"}
		}, []Difference{{"allow_from", "-", "198.51.100.0/24"}}},
		{
			"listen moved", func(d map[string]any) { docTeam(d)["listen"] = []any{"192.0.2.11"} },
			[]Difference{{"listen", "192.0.2.10", "-"}, {"listen", "-", "192.0.2.11"}},
		},
		{
			"address moves the default url", func(d map[string]any) { d["address"] = "192.0.2.99" },
			[]Difference{{"url", "http://192.0.2.10:8448", "http://192.0.2.99:8448"}},
		},
		{
			"trust changed", func(d map[string]any) { obj(docTeam(d)["trust"])["org"] = "other-org" },
			[]Difference{{
				"trust", `{"kind":"github","org":"example-org","repos":[{"repo":"example-org/thing","branches":["main"]}]}`,
				`{"kind":"github","org":"other-org","repos":[{"repo":"example-org/thing","branches":["main"]}]}`,
			}},
		},
		{
			"human_text flipped", func(d map[string]any) { obj(arr(d["lxc_members"])[0])["human_text"] = false },
			[]Difference{{"lxc_members", "box-1/agent,human_text=true", "-"}, {"lxc_members", "-", "box-1/agent,human_text=false"}},
		},
		{"in AG1 order", func(d map[string]any) {
			d["lxc_members"] = []any{}
			docTeam(d)["humans"] = []any{"alice"}
			docTeam(d)["url"] = "https://chat.example.com"
		}, []Difference{
			{"humans", "bob", "-"},
			{"url", "http://192.0.2.10:8448", "https://chat.example.com"},
			{"lxc_members", "box-1/agent,human_text=true", "-"},
		}},
	}
}

func TestCompareEachGatedField(t *testing.T) {
	approved := approvedOf(t, inputDoc())
	for _, c := range compareCases() {
		t.Run(c.name, func(t *testing.T) {
			doc := inputDoc()
			c.edit(doc)
			if got := Compare(approved, readInput(t, doc)); !slices.Equal(got, c.want) {
				t.Fatalf("Compare = %v\nwant %v", got, c.want)
			}
		})
	}
}

// memApprovals is an in-memory approval store.
type memApprovals struct {
	got map[string]Gated
	err error
}

func (m memApprovals) Load(id paths.TeamID) (*Gated, error) {
	if m.err != nil {
		return nil, m.err
	}
	g, ok := m.got[id.String()]
	if !ok {
		return nil, m.err
	}
	return &g, nil
}

func TestCheck(t *testing.T) {
	doc := validDecl()
	doc["teamchat_teams"] = append(teams(doc), map[string]any{"team": "gone", "state": "absent"})
	d, err := Decode(encode(t, doc))
	if err != nil {
		t.Fatal(err)
	}
	alpha := d.Inputs()[0].Gated()
	alpha.Humans = []string{"alice"}
	lines, err := Check(d, memApprovals{got: map[string]Gated{"alpha": alpha}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"SECURITY\talpha\thumans\t- -> bob", "SECURITY\tbeta\tteam\t- -> new"}
	if !slices.Equal(lines, want) {
		t.Fatalf("Check = %q", lines)
	}
	broken := errors.New("unreadable")
	if _, err := Check(d, memApprovals{err: broken}); !errors.Is(err, broken) {
		t.Fatalf("a load failure: %v", err)
	}
}

func TestHold(t *testing.T) {
	in := readInput(t, inputDoc())
	if err := Hold(approvedOf(t, inputDoc()), in); err != nil {
		t.Fatalf("an approved input: %v", err)
	}
	err := Hold(nil, in)
	re, ok := errors.AsType[*RefusedError](err)
	if !ok || len(re.Differences) != 1 || !strings.Contains(err.Error(), "ccy-teams approve alpha") ||
		!strings.Contains(err.Error(), "SECURITY\talpha\tteam\t- -> new") {
		t.Fatalf("an unapproved team: %v", err)
	}
}

func TestApproveWhole(t *testing.T) {
	in := readInput(t, inputDoc())
	for _, prev := range []*Gated{nil, {Humans: []string{"zed"}}} {
		g, err := Approve(prev, in, nil)
		if err != nil || len(Compare(&g, in)) != 0 {
			t.Fatalf("Approve whole: %+v, %v", g, err)
		}
	}
}

type approveCase struct {
	name   string
	edit   func(doc map[string]any)
	change Change
}

func approveCases() []approveCase {
	return []approveCase{
		{
			"add a human", func(d map[string]any) { docTeam(d)["humans"] = []any{"alice", "bob", "carol"} },
			Change{"humans", OpAdd, "carol"},
		},
		{
			"remove a human", func(d map[string]any) { docTeam(d)["humans"] = []any{"alice"} },
			Change{"humans", OpRemove, "bob"},
		},
		{"add a prefix", func(d map[string]any) {
			docTeam(d)["allow_from"] = []any{"192.0.2.0/24", "198.51.100.0/24"}
		}, Change{"allow_from", OpAdd, "198.51.100.0/24"}},
		{
			"remove a listen address", func(d map[string]any) { docTeam(d)["listen"] = []any{"192.0.2.10"} },
			Change{"listen", OpAdd, "192.0.2.10"},
		},
		{
			"set the coordinator", func(d map[string]any) { docTeam(d)["coordinator"] = "other.2+desk.podman" },
			Change{"coordinator", OpSet, "other.2+desk.podman"},
		},
		{
			"remove the coordinator", func(d map[string]any) { delete(docTeam(d), "coordinator") },
			Change{"coordinator", OpRemove, "myrepo.1+desk.podman"},
		},
		{
			"set the url", func(d map[string]any) { docTeam(d)["url"] = "https://chat.example.com" },
			Change{"url", OpSet, "https://chat.example.com"},
		},
		{
			"set the trust", func(d map[string]any) { obj(docTeam(d)["trust"])["org"] = "other-org" },
			Change{"trust", OpSet, `{"kind":"github","org":"other-org","repos":[{"repo":"example-org/thing","branches":["main"]}]}`},
		},
		{"add an lxc member", func(d map[string]any) {
			second := lxcEntry("alpha")
			second["container"] = "box-2"
			d["lxc_members"] = []any{lxcEntry("alpha"), second}
		}, Change{"lxc_members", OpAdd, "box-2/agent,human_text=true"}},
		{
			"remove an lxc member", func(d map[string]any) { d["lxc_members"] = []any{} },
			Change{"lxc_members", OpRemove, "box-1/agent,human_text=true"},
		},
		{
			"set an lxc member's human_text", func(d map[string]any) { obj(arr(d["lxc_members"])[0])["human_text"] = false },
			Change{"lxc_members", OpSet, "box-1/agent,human_text=false"},
		},
	}
}

func TestApproveOneChange(t *testing.T) {
	for _, c := range approveCases() {
		t.Run(c.name, func(t *testing.T) {
			before := inputDoc()
			if c.name == "remove a listen address" {
				docTeam(before)["listen"] = []any{"192.0.2.10", "192.0.2.11"}
				c.change = Change{"listen", OpRemove, "192.0.2.11"}
			}
			approved := approvedOf(t, before)
			after := inputDoc()
			c.edit(after)
			in := readInput(t, after)
			g, err := Approve(approved, in, &c.change)
			if err != nil {
				t.Fatalf("Approve: %v", err)
			}
			if d := Compare(&g, in); len(d) != 0 {
				t.Fatalf("approved value differs from the input: %v", d)
			}
		})
	}
}

func TestApproveRefusesAPlantedEntry(t *testing.T) {
	approved := approvedOf(t, inputDoc())
	doc := inputDoc()
	docTeam(doc)["humans"] = []any{"alice", "bob", "carol", "mallory"}
	_, err := Approve(approved, readInput(t, doc), &Change{"humans", OpAdd, "carol"})
	re, ok := errors.AsType[*RefusedError](err)
	if !ok || !slices.Equal(re.Differences, []Difference{{"humans", "-", "mallory"}}) {
		t.Fatalf("a planted human: %v", err)
	}
	if !strings.Contains(err.Error(), "ccy-teams approve alpha") {
		t.Fatalf("the refusal names no approve: %v", err)
	}
}

func TestApproveATeam(t *testing.T) {
	in := readInput(t, inputDoc())
	whole := string(in.Gated().value().Compact())
	g, err := Approve(nil, in, &Change{"team", OpSet, whole})
	if err != nil || len(Compare(&g, in)) != 0 {
		t.Fatalf("--only team: %+v, %v", g, err)
	}
	other := strings.Replace(whole, `"bob"`, `"carol"`, 1)
	if _, err := Approve(nil, in, &Change{"team", OpSet, other}); err == nil {
		t.Fatal("a team approved as other than declared")
	}
}

func approveRefusals(whole string) []struct {
	name     string
	approved bool
	change   Change
} {
	type c = struct {
		name     string
		approved bool
		change   Change
	}
	return []c{
		{"a field before the team", false, Change{"humans", OpAdd, "carol"}},
		{"the team again", true, Change{"team", OpSet, whole}},
		{"team not by set", false, Change{"team", OpAdd, whole}},
		{"team not json", false, Change{"team", OpSet, "{"}},
		{"team not an approved object", false, Change{"team", OpSet, `{"humans":[]}`}},
		{"an unknown field", true, Change{"name", OpSet, "x"}},
		{"set on a list", true, Change{"humans", OpSet, "carol"}},
		{"add what is there", true, Change{"humans", OpAdd, "bob"}},
		{"remove what is not", true, Change{"humans", OpRemove, "carol"}},
		{"a bad human", true, Change{"humans", OpAdd, "admin"}},
		{"a bad prefix", true, Change{"allow_from", OpAdd, "192.0.2.1/24"}},
		{"a bad address", true, Change{"listen", OpAdd, "0.0.0.0"}},
		{"coordinator by add", true, Change{"coordinator", OpAdd, "other.2+desk.podman"}},
		{"coordinator unchanged", true, Change{"coordinator", OpSet, "myrepo.1+desk.podman"}},
		{"coordinator bad handle", true, Change{"coordinator", OpSet, "alice"}},
		{"coordinator remove another", true, Change{"coordinator", OpRemove, "other.2+desk.podman"}},
		{"url by add", true, Change{"url", OpAdd, "https://chat.example.com"}},
		{"url unchanged", true, Change{"url", OpSet, "http://192.0.2.10:8448"}},
		{"url bad", true, Change{"url", OpSet, "http://x/path"}},
		{"trust unchanged", true, Change{"trust", OpSet, `{"kind":"github","org":"example-org","repos":[{"repo":"example-org/thing","branches":["main"]}]}`}},
		{"trust by remove", true, Change{"trust", OpRemove, "{}"}},
		{"trust not json", true, Change{"trust", OpSet, "{"}},
		{"trust bad", true, Change{"trust", OpSet, `{"kind":"gitlab","repos":[]}`}},
		{"lxc bad entry", true, Change{"lxc_members", OpAdd, "box-2/agent"}},
		{"lxc add a present member", true, Change{"lxc_members", OpAdd, "box-1/agent,human_text=false"}},
		{"lxc remove with other human_text", true, Change{"lxc_members", OpRemove, "box-1/agent,human_text=false"}},
		{"lxc set unchanged", true, Change{"lxc_members", OpSet, "box-1/agent,human_text=true"}},
		{"lxc set a missing member", true, Change{"lxc_members", OpSet, "box-9/agent,human_text=false"}},
		{"an unknown op", true, Change{"humans", Op("drop"), "bob"}},
	}
}

func TestApproveRefuses(t *testing.T) {
	in := readInput(t, inputDoc())
	whole := string(in.Gated().value().Compact())
	for _, c := range approveRefusals(whole) {
		t.Run(c.name, func(t *testing.T) {
			var prev *Gated
			if c.approved {
				prev = approvedOf(t, inputDoc())
			}
			// The input equals the approval, so a refusal with no difference is the change's own:
			// a change wrongly accepted would surface only as a mismatch, and fail here.
			_, err := Approve(prev, in, &c.change)
			if re, ok := errors.AsType[*RefusedError](err); !ok || re.Problem == "" || len(re.Differences) != 0 {
				t.Fatalf("err = %v, want the change itself refused", err)
			}
		})
	}
}

func TestApprovedFileRoundTrip(t *testing.T) {
	alpha := mustTeam("alpha")
	doc := inputDoc()
	delete(docTeam(doc), "coordinator")
	g := readInput(t, doc).Gated()
	raw := EncodeApproved(alpha, g)
	if !strings.Contains(string(raw), `"coordinator":null`) || !strings.HasPrefix(string(raw), `{"v":1,"team":"alpha","approved":{"humans":`) {
		t.Fatalf("encoded %s", raw)
	}
	back, err := DecodeApproved(raw, alpha)
	if err != nil {
		t.Fatalf("DecodeApproved: %v", err)
	}
	if d := diff(g, back); len(d) != 0 {
		t.Fatalf("round trip differs: %v", d)
	}
}

func TestDecodeApprovedRefuses(t *testing.T) {
	alpha := mustTeam("alpha")
	good := string(EncodeApproved(alpha, readInput(t, inputDoc()).Gated()))
	cases := map[string]string{
		"another team":   strings.Replace(good, `"team":"alpha"`, `"team":"beta"`, 1),
		"version":        strings.Replace(good, `"v":1`, `"v":2`, 1),
		"unknown key":    strings.Replace(good, `"v":1`, `"v":1,"x":1`, 1),
		"bad human":      strings.Replace(good, `"bob"`, `"admin"`, 1),
		"repeated human": strings.Replace(good, `"bob"`, `"alice"`, 1),
		"lxc key":        strings.Replace(good, `"human_text":true`, `"human_text":true,"seat":1`, 1),
		"lxc repeated": strings.Replace(good, `"lxc_members":[{"container":"box-1","user":"agent","human_text":true}]`,
			`"lxc_members":[{"container":"box-1","user":"agent","human_text":true},{"container":"box-1","user":"agent","human_text":false}]`, 1),
		"lxc user":    strings.Replace(good, `"user":"agent"`, `"user":"Agent"`, 1),
		"not json":    "{",
		"over cap":    `{"v":1,"pad":"` + strings.Repeat("x", maxApproved) + `"}`,
		"team number": strings.Replace(good, `"team":"alpha"`, `"team":7`, 1),
	}
	for name, raw := range cases {
		if raw == good {
			t.Fatalf("%s: the fixture edit did not apply", name)
		}
		if _, err := DecodeApproved([]byte(raw), alpha); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestFileApprovals(t *testing.T) {
	dir := t.TempDir()
	store := FileApprovals{Path: func(id paths.TeamID) string { return filepath.Join(dir, id.String()+".json") }, GID: os.Getgid()}
	alpha := mustTeam("alpha")
	if g, err := store.Load(alpha); err != nil || g != nil {
		t.Fatalf("no file: %+v, %v", g, err)
	}
	want := readInput(t, inputDoc()).Gated()
	if err := store.Save(alpha, want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "alpha.json"))
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("mode: %v, %v", info, err)
	}
	got, err := store.Load(alpha)
	if err != nil || got == nil || len(diff(want, *got)) != 0 {
		t.Fatalf("Load: %+v, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "beta.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(mustTeam("beta")); err == nil {
		t.Fatal("a broken file was loaded")
	}
	if err := os.Mkdir(filepath.Join(dir, "gamma.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(mustTeam("gamma")); err == nil {
		t.Fatal("a directory was loaded")
	}
}
