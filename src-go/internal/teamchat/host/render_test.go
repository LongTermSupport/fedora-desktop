package host

import (
	"encoding/json"
	"net/netip"
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

// teamDoc is ops's one-team input: one declared network and the bus address, so the effective
// networks are 192.0.2.0/24 and 192.0.2.10/32, and the one listen address is 192.0.2.10; no LXC
// member.
func teamDoc() map[string]any {
	return map[string]any{
		"team": map[string]any{
			"team": "ops", "state": "present", "port": 8448, "description": "The team that runs things.",
			"networks": []any{"192.0.2.0/24"}, "humans": []any{"alice"},
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
		team: in.Team, listen: addrs("192.0.2.10"), networks: prefixes("192.0.2.0/24", "192.0.2.10/32"),
		devices: []string{deviceUnit("eth0")},
		regKey:  regKey, pin: pin(t), baseUnit: []byte("[Unit]\nDescription=base\n"),
	}
}

func addrs(s ...string) []netip.Addr {
	out := make([]netip.Addr, 0, len(s))
	for _, a := range s {
		out = append(out, netip.MustParseAddr(a))
	}
	return out
}

func prefixes(s ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(s))
	for _, p := range s {
		out = append(out, netip.MustParsePrefix(p))
	}
	return out
}

const wantConfig = `# Rendered by teamchat-admin team apply for team ops; do not edit.
server_name = "ops.teamchat.internal"
address = ["127.0.0.1"]
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

// wantDropIn is the homeserver's drop-in: it binds loopback alone (X18), so its filter allows
// loopback alone and it waits for no device; the terminator's drop-in carries the networks.
const wantDropIn = `# Rendered by teamchat-admin team apply for team ops; do not edit.
[Service]
ExecStart=/usr/local/lib/teamchat/server/tuwunel-1.9.4
MemoryHigh=512M
IPAddressDeny=any
IPAddressAllow=127.0.0.1/32 ::1/128
SocketBindDeny=any
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
	if got := RegKeyOf(r.config); got != regKey {
		t.Errorf("RegKeyOf the config: %q", got)
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

// DropIns, which the unit-text judges outside this package read, is render's two drop-ins.
func TestDropInsAreRendersDropIns(t *testing.T) {
	s := spec(t, input(t, teamDoc()))
	r, err := render(s)
	if err != nil {
		t.Fatal(err)
	}
	server, terminator := DropIns(s.team.ID, s.team.Port, s.pin, s.listen, s.networks, s.devices)
	if string(server) != string(r.dropIn) || string(terminator) != string(r.tlsDropIn) {
		t.Errorf("DropIns:\n%s\n%s\nrender:\n%s\n%s", server, terminator, r.dropIn, r.tlsDropIn)
	}
}

// HS4: a terminator with no listen address and no device allows loopback once and leaves [Unit]
// empty.
func TestRenderDeduplicatesLoopback(t *testing.T) {
	doc := teamDoc()
	s := spec(t, input(t, doc))
	s.listen, s.networks, s.devices = nil, nil, nil
	r, err := render(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(r.tlsDropIn), "[Unit]\n\n[Service]") || !strings.Contains(string(r.tlsDropIn), "IPAddressAllow=127.0.0.1/32 ::1/128\n") {
		t.Errorf("terminator drop-in:\n%s", r.tlsDropIn)
	}
}

// HS4: an IPv6 listen address is allowed as a /128, and given to the terminator as it is.
func TestRenderIPv6Listen(t *testing.T) {
	doc := teamDoc()
	s := spec(t, input(t, doc))
	s.listen, s.networks = addrs("2001:db8::1"), prefixes("2001:db8::/32")
	r, err := render(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(r.tlsDropIn), " 2001:db8::1/128 ") || !strings.Contains(string(r.tlsDropIn), " --listen 2001:db8::1\n") {
		t.Errorf("terminator drop-in:\n%s", r.tlsDropIn)
	}
}

// HS5: the restart set is the base unit, ExecStart and the config; the reload set is the
// cgroup lines; each changes only with what it covers. The networks, the listen addresses and the
// devices are the terminator's (X4, X18), so none of them is in either set.
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
		"base unit": {func(s *renderSpec) { s.baseUnit = []byte("other") }, true, false},
		"version":   {func(s *renderSpec) { s.pin.Version = mustVersion(t, "1.9.5") }, true, false},
		"key":       {func(s *renderSpec) { s.regKey = strings.Repeat("f", 128) }, true, false},
		"cache":     {func(s *renderSpec) { s.pin.CacheCapacityModifier = "1" }, true, false},
		"memory":    {func(s *renderSpec) { s.pin.MemoryHigh = "1G" }, false, true},
		"networks":  {func(s *renderSpec) { s.networks = prefixes("198.51.100.0/24") }, false, false},
		"listen":    {func(s *renderSpec) { s.listen = addrs("192.0.2.10", "198.51.100.1") }, false, false},
		"devices":   {func(s *renderSpec) { s.devices = []string{deviceUnit("eth1")} }, false, false},
		"name":      {func(s *renderSpec) { s.team.Name = "Ops team" }, false, false},
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

// RegKeyOf finds no key in a config without one of the right shape.
func TestRegKeyOfAConfigWithoutOne(t *testing.T) {
	for _, config := range []string{"", "registration_shared_secret = \"short\"\n", "x = 1\n"} {
		if got := RegKeyOf([]byte(config)); got != "" {
			t.Errorf("%q: %q", config, got)
		}
	}
}
