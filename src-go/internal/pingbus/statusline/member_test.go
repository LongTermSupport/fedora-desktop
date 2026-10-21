package statusline

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// S21: check_base_url's verdict for each URL, recorded from the oracle's Python with
// plain_http_hosts 127.0.0.1, ::1 and ::ffff:102:304. Where the Python crashes (urlsplit's
// ValueError on a bracket, uncaught), Go refuses: the departure the plan records.
func TestCheckBaseURLAgreesWithThePython(t *testing.T) {
	plain := []string{"127.0.0.1", "::1", "::ffff:102:304"}
	cases := [][2]string{
		{"http://127.0.0.1:1", "ok"},
		{"https://example.org", "ok"},
		{"https://example.org:8448", "ok"},
		{"https://Example.ORG", "ok"},
		{"HTTPS://example.org", "ok"},
		{"http://localhost", "refused"},
		{"http://127.0.0.1", "ok"},
		{"http://127.0.0.2", "refused"},
		{"https://127.0.0.1", "ok"},
		{"https://[::1]", "ok"},
		{"http://[::1]:80", "ok"},
		{"https://[::1]x", "crash"},
		{"https://x[::1]", "crash"},
		{"https://[1.2.3.4]", "crash"},
		{"https://[v1.x]", "ok"},
		{"https://[vz.x]", "crash"},
		{"https://[::1", "crash"},
		{"https://::1]", "crash"},
		{"https://[fe80::1%25eth0]", "refused"},
		{"https://user@example.org", "refused"},
		{"https://example.org/", "refused"},
		{"https://example.org/x", "refused"},
		{"https://example.org:", "ok"},
		{"https://example.org:65535", "ok"},
		{"https://example.org:65536", "refused"},
		{"https://example.org:0080", "ok"},
		{"https://example.org:8a", "refused"},
		{"https://example.org:8:9", "refused"},
		{"https:example.org", "refused"},
		{"ftp://example.org", "refused"},
		{"https://", "refused"},
		{"https://:80", "refused"},
		{"https://ex_ample.org", "refused"},
		{"http://[::ffff:1.2.3.4]", "ok"},
		{"https://-example.org", "refused"},
		{"https://a..b", "refused"},
		{"https://a.b.", "refused"},
		{"h ttps://x", "refused"},
		{"1https://x", "refused"},
		{"https://example.org?", "refused"},
		{"https://éx", "refused"},
		{"https://[::1]:80:90", "refused"},
		{"https://[::1]]:80", "crash"},
		{"https://]x[", "crash"},
		{"https://[v1.x]:80", "ok"},
		{"https://[fe80::1%]", "crash"},
		{"https://example.org:" + strings.Repeat("0", 5000) + "1", "refused"},
	}
	for _, c := range cases {
		err := checkBaseURL(c[0], plain)
		if (err == nil) != (c[1] == "ok") {
			t.Errorf("checkBaseURL(%q): %v, want %s", c[0], err, c[1])
		}
	}
	if checkBaseURL(pyjson.Number{Lit: "1", Kind: pyjson.Int}, plain) == nil {
		t.Error("a number is a base URL")
	}
}

func goodMember() pyjson.Object {
	return pyjson.Object{
		"protocol":         pyjson.Number{Lit: "1", Kind: pyjson.Int},
		"team":             "alpha",
		"user_id":          "@myrepo.1+workstation.podman:alpha.example.org",
		"server_name":      "alpha.example.org",
		"base_url":         "https://alpha.example.org",
		"plain_http_hosts": []any{"127.0.0.1"},
		"token_file":       "token",
		"admin":            "@admin:alpha.example.org",
		"room":             "!" + strings.Repeat("R", 43),
	}
}

// S21: _parse_member accepts a well-formed member and reads its user and type.
func TestParseMemberAcceptsAGoodMember(t *testing.T) {
	m := goodMember()
	m["human_text"] = false
	m["limits"] = pyjson.Object{"send_burst": pyjson.Number{Lit: "30", Kind: pyjson.Int}}
	got, err := parseMember(m, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got.userID != "@myrepo.1+workstation.podman:alpha.example.org" || got.memberType != "podman" || got.serverName != "alpha.example.org" {
		t.Errorf("member: %+v", got)
	}
	host := goodMember()
	host["user_id"] = "@myrepo.desk+workstation.host:alpha.example.org"
	if got, err := parseMember(host, "alpha"); err != nil || got.memberType != "host" {
		t.Errorf("host member: %+v %v", got, err)
	}
}

// S21: every refusal of _parse_member, one change from a good member each.
func TestParseMemberRefusesWhatThePythonRefuses(t *testing.T) {
	num := func(lit string, kind pyjson.Kind) pyjson.Number { return pyjson.Number{Lit: lit, Kind: kind} }
	cases := map[string]func(pyjson.Object){
		"unknown key":          func(m pyjson.Object) { m["extra"] = true },
		"missing key":          func(m pyjson.Object) { delete(m, "room") },
		"protocol float":       func(m pyjson.Object) { m["protocol"] = num("1.0", pyjson.Float) },
		"protocol bool":        func(m pyjson.Object) { m["protocol"] = true },
		"protocol 2":           func(m pyjson.Object) { m["protocol"] = num("2", pyjson.Int) },
		"other team":           func(m pyjson.Object) { m["team"] = "beta" },
		"server upper":         func(m pyjson.Object) { m["server_name"] = "Alpha.example.org" },
		"server long":          func(m pyjson.Object) { m["server_name"] = strings.Repeat("a.", 127) + "a" },
		"server number":        func(m pyjson.Object) { m["server_name"] = num("1", pyjson.Int) },
		"user other server":    func(m pyjson.Object) { m["user_id"] = "@myrepo.1+workstation.podman:beta.example.org" },
		"user human":           func(m pyjson.Object) { m["user_id"] = "@alice:alpha.example.org" },
		"user reserved":        func(m pyjson.Object) { m["user_id"] = "@admin:alpha.example.org" },
		"user bad type":        func(m pyjson.Object) { m["user_id"] = "@myrepo.1+workstation.kvm:alpha.example.org" },
		"user no at":           func(m pyjson.Object) { m["user_id"] = "myrepo.1+workstation.podman:alpha.example.org" },
		"user number":          func(m pyjson.Object) { m["user_id"] = num("1", pyjson.Int) },
		"admin other":          func(m pyjson.Object) { m["admin"] = "@root:alpha.example.org" },
		"room short":           func(m pyjson.Object) { m["room"] = "!" + strings.Repeat("R", 42) },
		"room number":          func(m pyjson.Object) { m["room"] = num("1", pyjson.Int) },
		"token file":           func(m pyjson.Object) { m["token_file"] = "/etc/token" },
		"plain not list":       func(m pyjson.Object) { m["plain_http_hosts"] = "127.0.0.1" },
		"plain not ip":         func(m pyjson.Object) { m["plain_http_hosts"] = []any{"localhost"} },
		"plain zone":           func(m pyjson.Object) { m["plain_http_hosts"] = []any{"fe80::1%eth0"} },
		"plain not canonical":  func(m pyjson.Object) { m["plain_http_hosts"] = []any{"::ffff:1.2.3.4"} },
		"plain twice":          func(m pyjson.Object) { m["plain_http_hosts"] = []any{"127.0.0.1", "127.0.0.1"} },
		"plain number":         func(m pyjson.Object) { m["plain_http_hosts"] = []any{num("1", pyjson.Int)} },
		"base http unlisted":   func(m pyjson.Object) { m["base_url"] = "http://192.0.2.1" },
		"human text null":      func(m pyjson.Object) { m["human_text"] = pyjson.Null{} },
		"human text number":    func(m pyjson.Object) { m["human_text"] = num("1", pyjson.Int) },
		"limits null":          func(m pyjson.Object) { m["limits"] = pyjson.Null{} },
		"limits list":          func(m pyjson.Object) { m["limits"] = []any{} },
		"limits unknown key":   func(m pyjson.Object) { m["limits"] = pyjson.Object{"speed": num("1", pyjson.Int)} },
		"limits out of bounds": func(m pyjson.Object) { m["limits"] = pyjson.Object{"send_burst": num("31", pyjson.Int)} },
		"limits low":           func(m pyjson.Object) { m["limits"] = pyjson.Object{"wait_timeout_s": num("0", pyjson.Int)} },
		"limits float":         func(m pyjson.Object) { m["limits"] = pyjson.Object{"send_burst": num("3.0", pyjson.Float)} },
		"limits bool":          func(m pyjson.Object) { m["limits"] = pyjson.Object{"send_burst": true} },
		"limits huge": func(m pyjson.Object) {
			m["limits"] = pyjson.Object{"send_burst": num(strings.Repeat("9", 30), pyjson.Int)}
		},
	}
	for name, change := range cases {
		m := goodMember()
		change(m)
		if _, err := parseMember(m, "alpha"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// S21: the handle grammar of protocol.parse_handle.
func TestParseHandle(t *testing.T) {
	good := map[string]string{
		"myrepo.1+workstation.podman":               "podman",
		"my_repo-2.desk+host-1.lxc":                 "lxc",
		"r.999999+h.docker":                         "docker",
		"r.abcdefghijkl+h.vm":                       "vm",
		"0.a1+" + strings.Repeat("h", 63) + ".host": "host",
	}
	for handle, kind := range good {
		if got, ok := parseHandle(handle); !ok || got != kind {
			t.Errorf("parseHandle(%q) = %q, %v", handle, got, ok)
		}
	}
	for _, bad := range []string{
		"", "repo.0+h.podman", "repo.1234567+h.podman", "Repo.1+h.podman", "repo.1+h.kvm",
		"repo.1-h.podman", "repo.abcdefghijklm+h.vm", "_r.1+h.vm", "repo.1+-h.vm", "repo.1+h-.vm", "repo.1+h.vm.vm",
	} {
		if _, ok := parseHandle(bad); ok {
			t.Errorf("parseHandle(%q) accepted", bad)
		}
	}
}
