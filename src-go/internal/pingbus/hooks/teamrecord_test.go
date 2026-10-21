package hooks

import (
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

const testServer = "alpha.example.org"

func goodRecord() pyjson.Object {
	return pyjson.Object{
		"v":         pyjson.Number{Lit: "1", Kind: pyjson.Int},
		"team":      "alpha",
		"humans":    []any{"@alice:" + testServer},
		"roles":     pyjson.Object{"@myrepo.1+workstation.podman:" + testServer: "coordinator", "@orch.1+workstation.podman:" + testServer: "orchestrator"},
		"repos":     []any{pyjson.Object{"repo": "example-org/myrepo", "branches": []any{"main"}}},
		"forge_api": "https://api.github.com",
	}
}

// S22: parse_team_record accepts a good record and reads the legacy orchestrator as coordinator.
func TestParseTeamRecordAcceptsAGoodRecord(t *testing.T) {
	record, err := parseTeamRecord(goodRecord(), testServer, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if record.roles["@orch.1+workstation.podman:"+testServer] != roleCoordinator {
		t.Errorf("roles: %v", record.roles)
	}
	// The validator's context: humans, repos with their branches, no org, no prefixes.
	if !isTheGoodRecord(record) {
		t.Errorf("record: %+v", record)
	}
	withOrg := goodRecord()
	withOrg["org"] = "example-org"
	withOrg["repos"] = []any{}
	withOrg["exclude"] = []any{"secret"}
	withOrg["path_prefixes"] = []any{"docs/", "README.md"}
	withOrg["forge_api"] = "https://ghe.example.org/api/v3"
	got, err := parseTeamRecord(withOrg, testServer, "alpha")
	if err != nil || !got.hasOrg || got.org != "example-org" || !slices.Equal(got.exclude, []string{"secret"}) ||
		!slices.Equal(got.pathPrefixes, []string{"docs/", "README.md"}) || len(got.repos) != 0 {
		t.Errorf("org record: %+v %v", got, err)
	}
	anyBranch := goodRecord()
	anyBranch["repos"] = []any{pyjson.Object{"repo": "example-org/myrepo"}}
	if got, err := parseTeamRecord(anyBranch, testServer, "alpha"); err != nil || got.repos["example-org/myrepo"] == nil || len(got.repos["example-org/myrepo"]) != 0 {
		t.Errorf("any branch: %+v %v", got, err)
	}
}

// The reasons are v1's words: the goodbye's loss of trust writes them into untrusted.json.
func TestParseTeamRecordReasonsAreThePythonWords(t *testing.T) {
	cases := map[string]func(pyjson.Object){
		"team record keys":        func(r pyjson.Object) { r["extra"] = true },
		"team record version":     func(r pyjson.Object) { r["v"] = true },
		"team name":               func(r pyjson.Object) { r["team"] = "beta" },
		"list size or duplicates": func(r pyjson.Object) { r["humans"] = []any{} },
		"humans":                  func(r pyjson.Object) { r["humans"] = []any{"@admin:" + testServer} },
		"roles":                   func(r pyjson.Object) { r["roles"] = []any{} },
		"path_prefixes":           func(r pyjson.Object) { r["path_prefixes"] = []any{"../x"} },
		"forge_api":               func(r pyjson.Object) { r["forge_api"] = "http://x" },
		"org":                     func(r pyjson.Object) { r["org"] = "Example" },
		"repos entry":             func(r pyjson.Object) { r["repos"] = []any{pyjson.Object{"repo": "a/b.git"}} },
		"branch name":             func(r pyjson.Object) { r["repos"] = []any{pyjson.Object{"repo": "a/b", "branches": []any{"a b"}}} },
		"neither an org nor a repository is trusted": func(r pyjson.Object) { r["repos"] = []any{} },
		"exclude": func(r pyjson.Object) { r["exclude"] = []any{"x"} },
	}
	for want, change := range cases {
		r := goodRecord()
		change(r)
		if _, err := parseTeamRecord(r, testServer, "alpha"); err == nil || err.Error() != want {
			t.Errorf("%s: %v", want, err)
		}
	}
}

func isTheGoodRecord(r teamRecord) bool {
	return r.team == "alpha" && r.humans["@alice:"+testServer] && len(r.humans) == 1 &&
		slices.Equal(r.repos["example-org/myrepo"], []string{"main"}) && !r.hasOrg && r.forgeAPI == "https://api.github.com"
}

// S22: each refusal of parse_team_record, one change from a good record each.
func TestParseTeamRecordRefusesWhatThePythonRefuses(t *testing.T) {
	num := func(lit string) pyjson.Number { return pyjson.Number{Lit: lit, Kind: pyjson.Int} }
	humans := func(n int) []any {
		list := make([]any, n)
		for i := range list {
			list[i] = "@h" + strings.Repeat("a", i+1) + ":" + testServer
		}
		return list
	}
	roles := func(n int) pyjson.Object {
		object := pyjson.Object{}
		for i := range n {
			object["@r.1+h"+strings.Repeat("a", i+1)+".vm:"+testServer] = "worker"
		}
		return object
	}
	cases := map[string]func(pyjson.Object){
		"unknown key":        func(r pyjson.Object) { r["extra"] = true },
		"missing key":        func(r pyjson.Object) { delete(r, "forge_api") },
		"v float":            func(r pyjson.Object) { r["v"] = pyjson.Number{Lit: "1.0", Kind: pyjson.Float} },
		"v bool":             func(r pyjson.Object) { r["v"] = true },
		"other team":         func(r pyjson.Object) { r["team"] = "beta" },
		"no humans":          func(r pyjson.Object) { r["humans"] = []any{} },
		"too many humans":    func(r pyjson.Object) { r["humans"] = humans(17) },
		"human twice":        func(r pyjson.Object) { r["humans"] = []any{"@alice:" + testServer, "@alice:" + testServer} },
		"human an agent":     func(r pyjson.Object) { r["humans"] = []any{"@myrepo.1+workstation.podman:" + testServer} },
		"human reserved":     func(r pyjson.Object) { r["humans"] = []any{"@admin:" + testServer} },
		"human other server": func(r pyjson.Object) { r["humans"] = []any{"@alice:beta.example.org"} },
		"roles a list":       func(r pyjson.Object) { r["roles"] = []any{} },
		"too many roles":     func(r pyjson.Object) { r["roles"] = roles(65) },
		"role unknown":       func(r pyjson.Object) { r["roles"] = pyjson.Object{"@r.1+h.vm:" + testServer: "boss"} },
		"role a number":      func(r pyjson.Object) { r["roles"] = pyjson.Object{"@r.1+h.vm:" + testServer: num("1")} },
		"role of a human":    func(r pyjson.Object) { r["roles"] = pyjson.Object{"@alice:" + testServer: "worker"} },
		"prefixes empty":     func(r pyjson.Object) { r["path_prefixes"] = []any{} },
		"prefix bad":         func(r pyjson.Object) { r["path_prefixes"] = []any{"../x"} },
		"prefix dot":         func(r pyjson.Object) { r["path_prefixes"] = []any{"a/./b"} },
		"prefix nine deep":   func(r pyjson.Object) { r["path_prefixes"] = []any{"a/b/c/d/e/f/g/h/i"} },
		"forge http":         func(r pyjson.Object) { r["forge_api"] = "http://api.github.com" },
		"forge space":        func(r pyjson.Object) { r["forge_api"] = "https://api.github.com/a b" },
		"forge nbsp":         func(r pyjson.Object) { r["forge_api"] = "https://api github.com" },
		"forge unit sep":     func(r pyjson.Object) { r["forge_api"] = "https://api.github.com/\u001f" },
		"forge no host":      func(r pyjson.Object) { r["forge_api"] = "https:///x" },
		"org null":           func(r pyjson.Object) { r["org"] = pyjson.Null{} },
		"org upper":          func(r pyjson.Object) { r["org"] = "Example" },
		"repos a string":     func(r pyjson.Object) { r["repos"] = "example-org/myrepo" },
		"repo extra key":     func(r pyjson.Object) { r["repos"] = []any{pyjson.Object{"repo": "a/b", "x": true}} },
		"repo .git":          func(r pyjson.Object) { r["repos"] = []any{pyjson.Object{"repo": "a/b.git"}} },
		"repo dot":           func(r pyjson.Object) { r["repos"] = []any{pyjson.Object{"repo": "a/.."}} },
		"repo twice": func(r pyjson.Object) {
			r["repos"] = []any{pyjson.Object{"repo": "a/b"}, pyjson.Object{"repo": "a/b", "branches": []any{"x"}}}
		},
		"repo same entry":      func(r pyjson.Object) { r["repos"] = []any{pyjson.Object{"repo": "a/b"}, pyjson.Object{"repo": "a/b"}} },
		"branches empty":       func(r pyjson.Object) { r["repos"] = []any{pyjson.Object{"repo": "a/b", "branches": []any{}}} },
		"branch bad":           func(r pyjson.Object) { r["repos"] = []any{pyjson.Object{"repo": "a/b", "branches": []any{"a b"}}} },
		"neither org nor repo": func(r pyjson.Object) { r["repos"] = []any{} },
		"exclude without org":  func(r pyjson.Object) { r["exclude"] = []any{"x"} },
		"exclude listed": func(r pyjson.Object) {
			r["org"] = "a"
			r["repos"] = []any{pyjson.Object{"repo": "a/b"}}
			r["exclude"] = []any{"b"}
		},
		"not an object": func(r pyjson.Object) { r["humans"] = nil },
	}
	for name, change := range cases {
		r := goodRecord()
		change(r)
		if _, err := parseTeamRecord(r, testServer, "alpha"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := parseTeamRecord([]any{}, testServer, "alpha"); err == nil {
		t.Error("a list is a team record")
	}
}

// S22: _list_of compares entries as json.dumps(sort_keys=True) does: 1 and 1.0 differ, and two
// objects with their keys in another order are the same.
func TestListOfComparesEntriesByTheirCanonicalJSON(t *testing.T) {
	one := pyjson.Number{Lit: "1", Kind: pyjson.Int}
	float := pyjson.Number{Lit: "1.0", Kind: pyjson.Float}
	if _, ok := listOf([]any{one, float}, 1, 2); !ok {
		t.Error("1 and 1.0 read as the same entry")
	}
	if _, ok := listOf([]any{pyjson.Object{"a": one, "b": one}, pyjson.Object{"b": one, "a": one}}, 1, 2); ok {
		t.Error("the same object twice read as distinct")
	}
	if _, ok := listOf([]any{one, one, one}, 1, 2); ok {
		t.Error("a list over its bound accepted")
	}
}
