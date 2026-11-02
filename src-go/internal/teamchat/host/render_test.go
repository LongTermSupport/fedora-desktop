package host

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// regKey is a registration shared key of the right shape: 128 lowercase hex digits.
var regKey = strings.Repeat("0123456789abcdef", 8)

func pin(t *testing.T) Pin {
	t.Helper()
	return Pin{Version: mustVersion(t, "1.9.4"), MemoryHigh: "512M", CacheCapacityModifier: "0.5"}
}

func mustVersion(t *testing.T, s string) paths.ServerVersion {
	t.Helper()
	v, err := paths.ParseServerVersion(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// teamDoc is ops's one-team input: one listen address, one allowed network, no LXC member.
func teamDoc() map[string]any {
	return map[string]any{
		"team": map[string]any{
			"team": "ops", "state": "present", "port": 8448, "description": "The team that runs things.",
			"listen": []any{"192.0.2.10"}, "allow_from": []any{"192.0.2.0/24"}, "humans": []any{"alice"},
			"trust": map[string]any{
				"kind": "github", "org": "example-org",
				"repos": []any{map[string]any{"repo": "example-org/thing", "branches": []any{"main"}}},
			},
		},
		"lxc_members": []any{},
		"address":     "192.0.2.10",
	}
}

func docTeam(doc map[string]any) map[string]any {
	m, ok := doc["team"].(map[string]any)
	if !ok {
		panic("fixture: no team")
	}
	return m
}

func input(t *testing.T, doc map[string]any) declare.Input {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	in, err := declare.DecodeInput(raw)
	if err != nil {
		t.Fatalf("DecodeInput: %v", err)
	}
	return in
}

func spec(t *testing.T, in declare.Input) renderSpec {
	t.Helper()
	return renderSpec{
		team: in.Team, allowFrom: in.Team.AllowFrom, devices: []string{deviceUnit("eth0")},
		regKey: regKey, pin: pin(t), baseUnit: []byte("[Unit]\nDescription=base\n"),
	}
}

const wantConfig = `# Rendered by teamchat-admin team apply for team ops; do not edit.
server_name = "ops.teamchat.internal"
address = ["127.0.0.1", "192.0.2.10"]
port = 8448
database_path = "/var/lib/teamchat/ops/db"
database_backup_path = "/var/lib/teamchat/ops/backups"
database_backups_to_keep = 7
allow_registration = false
allow_guest_registration = false
registration_shared_secret = "` + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" + `"
grant_admin_to_first_user = false
login_via_token = false
login_via_existing_session = false
allow_federation = false
trusted_servers = []
federate_admin_room = false
admin_escape_commands = false
allow_encryption = false
allow_room_creation = false
auto_accept_invites = false
new_user_displayname_suffix = ""
client_sync_timeout_min = 0
default_room_version = "12"
sentry = false
log = "warn"
admin_signal_execute = ["server backup-database"]
error_on_unknown_config_opts = true
rocksdb_allow_fallocate = false
max_request_size = 10000000
enable_set_displayname = false
presence_idle_timeout_s = 120
presence_offline_timeout_s = 60
cache_capacity_modifier = 0.5

[rate_limiting.login.failed]
per_second = 0.0033
burst_count = 3

[rate_limiting.login.account]
per_second = 0.006
burst_count = 10
`

const wantDropIn = `# Rendered by teamchat-admin team apply for team ops; do not edit.
[Unit]
After=sys-subsystem-net-devices-eth0.device
Wants=sys-subsystem-net-devices-eth0.device

[Service]
ExecStart=/usr/local/lib/teamchat/server/tuwunel-1.9.4
MemoryHigh=512M
IPAddressAllow=127.0.0.1/32 ::1/128 192.0.2.10/32 192.0.2.0/24
SocketBindAllow=tcp:8448
`

// HS4: the config and the drop-in are C 3.2.3's and 3.2.4's, and team.json is declare's.
func TestRenderTheTeam(t *testing.T) {
	in := input(t, teamDoc())
	r, err := render(spec(t, in))
	if err != nil {
		t.Fatal(err)
	}
	if string(r.config) != wantConfig {
		t.Errorf("config:\n%s\nwant:\n%s", r.config, wantConfig)
	}
	if string(r.dropIn) != wantDropIn {
		t.Errorf("drop-in:\n%s\nwant:\n%s", r.dropIn, wantDropIn)
	}
	if string(r.teamFile) != string(in.Team.Canonical()) {
		t.Errorf("team.json %s", r.teamFile)
	}
	if got := regKeyOf(r.config); got != regKey {
		t.Errorf("regKeyOf the config: %q", got)
	}
}

// HS4: every key the config sets is one the pinned Tuwunel documents (error_on_unknown_config_opts
// would stop it at start otherwise), and the bare keys come before the tables.
func TestConfigKeysAreThePinnedTuwunels(t *testing.T) {
	known := strings.Fields(string(readFile(t, "testdata/tuwunel-1.9.4-keys.txt")))
	r, err := render(spec(t, input(t, teamDoc())))
	if err != nil {
		t.Fatal(err)
	}
	key := regexp.MustCompile(`^([a-z0-9_]+) = `)
	var headers []string
	for line := range strings.SplitSeq(string(r.config), "\n") {
		if strings.HasPrefix(line, "[") {
			headers = append(headers, line)
			continue
		}
		m := key.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if !slices.Contains(known, m[1]) {
			t.Errorf("key %s is not one Tuwunel 1.9.4 documents", m[1])
		}
		if len(headers) == 0 && (m[1] == "per_second" || m[1] == "burst_count") {
			t.Errorf("%s is a bare key", m[1])
		}
	}
	if want := []string{"[rate_limiting.login.failed]", "[rate_limiting.login.account]"}; !slices.Equal(headers, want) {
		t.Errorf("tables %q, want %q", headers, want)
	}
}

// HS4: listen holding loopback binds it once, and allows it once; no device leaves [Unit] empty.
func TestRenderDeduplicatesLoopback(t *testing.T) {
	doc := teamDoc()
	docTeam(doc)["listen"] = []any{"127.0.0.1"}
	docTeam(doc)["allow_from"] = []any{}
	s := spec(t, input(t, doc))
	s.devices = nil
	r, err := render(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(r.config), "address = [\"127.0.0.1\"]\n") {
		t.Errorf("config:\n%s", r.config)
	}
	if !strings.Contains(string(r.dropIn), "[Unit]\n\n[Service]") || !strings.Contains(string(r.dropIn), "IPAddressAllow=127.0.0.1/32 ::1/128\n") {
		t.Errorf("drop-in:\n%s", r.dropIn)
	}
}

// HS4: an IPv6 listen address is allowed as a /128.
func TestRenderIPv6Listen(t *testing.T) {
	doc := teamDoc()
	docTeam(doc)["listen"] = []any{"2001:db8::1"}
	r, err := render(spec(t, input(t, doc)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(r.dropIn), " 2001:db8::1/128 ") {
		t.Errorf("drop-in:\n%s", r.dropIn)
	}
}

// HS5: the restart set is the base unit, ExecStart and the config; the reload set is the
// cgroup lines and [Unit]; each changes only with what it covers.
func TestDigestSets(t *testing.T) {
	in := input(t, teamDoc())
	base, err := render(spec(t, in))
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		change          func(*renderSpec)
		restart, reload bool
	}{
		"base unit":  {func(s *renderSpec) { s.baseUnit = []byte("other") }, true, false},
		"version":    {func(s *renderSpec) { s.pin.Version = mustVersion(t, "1.9.5") }, true, false},
		"key":        {func(s *renderSpec) { s.regKey = strings.Repeat("f", 128) }, true, false},
		"cache":      {func(s *renderSpec) { s.pin.CacheCapacityModifier = "1" }, true, false},
		"memory":     {func(s *renderSpec) { s.pin.MemoryHigh = "1G" }, false, true},
		"allow_from": {func(s *renderSpec) { s.allowFrom = []string{"198.51.100.0/24"} }, false, true},
		"devices":    {func(s *renderSpec) { s.devices = []string{deviceUnit("eth1")} }, false, true},
		"name":       {func(s *renderSpec) { s.team.Name = "Ops team" }, false, false},
	} {
		s := spec(t, in)
		c.change(&s)
		r, err := render(s)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := !r.digests.sameRestart(base.digests); got != c.restart {
			t.Errorf("%s: restart set changed %v, want %v", name, got, c.restart)
		}
		if got := !r.digests.sameReload(base.digests); got != c.reload {
			t.Errorf("%s: reload set changed %v, want %v", name, got, c.reload)
		}
	}
}

// HS4: what cannot be rendered safely is refused.
func TestRenderRefusals(t *testing.T) {
	in := input(t, teamDoc())
	for name, change := range map[string]func(*renderSpec){
		"short key":          func(s *renderSpec) { s.regKey = "abc" },
		"upper-case key":     func(s *renderSpec) { s.regKey = strings.ToUpper(regKey) },
		"memory not a size":  func(s *renderSpec) { s.pin.MemoryHigh = "lots" },
		"memory zero":        func(s *renderSpec) { s.pin.MemoryHigh = "0" },
		"cache not a number": func(s *renderSpec) { s.pin.CacheCapacityModifier = "1e3" },
		"cache zero":         func(s *renderSpec) { s.pin.CacheCapacityModifier = "0.0" },
		"no version":         func(s *renderSpec) { s.pin.Version = paths.ServerVersion{} },
	} {
		s := spec(t, in)
		change(&s)
		if _, err := render(s); err == nil {
			t.Errorf("%s: rendered", name)
		}
	}
}

// HS4: a device unit is systemd's escaped name of the interface.
func TestDeviceUnit(t *testing.T) {
	for iface, want := range map[string]string{
		"eth0":      "sys-subsystem-net-devices-eth0.device",
		"teamchat0": "sys-subsystem-net-devices-teamchat0.device",
		"br-lan":    `sys-subsystem-net-devices-br\x2dlan.device`,
		".hidden":   `sys-subsystem-net-devices-\x2ehidden.device`,
		"wl_p.1":    "sys-subsystem-net-devices-wl_p.1.device",
	} {
		if got := deviceUnit(iface); got != want {
			t.Errorf("%s: %s, want %s", iface, got, want)
		}
	}
	for _, bad := range []string{"", ".", "..", "a/b", "a b", "eth0:1", strings.Repeat("a", 16)} {
		if validInterface(bad) {
			t.Errorf("%q is valid", bad)
		}
	}
}

// regKeyOf finds no key in a config without one of the right shape.
func TestRegKeyOfAConfigWithoutOne(t *testing.T) {
	for _, config := range []string{"", "registration_shared_secret = \"short\"\n", "x = 1\n"} {
		if got := regKeyOf([]byte(config)); got != "" {
			t.Errorf("%q: %q", config, got)
		}
	}
}
