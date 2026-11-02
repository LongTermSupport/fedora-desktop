package host

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// Pin is what the host gives every team's homeserver: the pinned Tuwunel version, and the
// memory ceiling and cache modifier H-C1 measures (C 3.2.3, 3.2.4).
type Pin struct {
	Version               paths.ServerVersion
	MemoryHigh            string
	CacheCapacityModifier string
}

// loopbackListen is the address every homeserver binds besides its team's listen addresses.
const loopbackListen = "127.0.0.1"

// loopbackAllow is what every drop-in allows besides the listen addresses and allow_from.
var loopbackAllow = []string{"127.0.0.1/32", "::1/128"}

// The digest set members (C 3.5.2 step 3).
const (
	digestBaseUnit   = "base_unit"
	digestExecStart  = "exec_start"
	digestConfig     = "config"
	digestIPAllow    = "ip_address_allow"
	digestSocketBind = "socket_bind_allow"
	digestMemory     = "memory_high"
	digestUnit       = "unit"
)

// digests are the restart and reload digest sets units.json keeps.
type digests struct {
	Restart, Reload map[string]string
}

func (d digests) sameRestart(o digests) bool { return maps.Equal(d.Restart, o.Restart) }

func (d digests) sameReload(o digests) bool { return maps.Equal(d.Reload, o.Reload) }

// renderSpec is what one render reads: the team, its allow_from with the bridge added, the
// device unit of each interface carrying a listen address, the registration key, the pin and
// the installed base unit's text.
type renderSpec struct {
	team      declare.Team
	allowFrom []string
	devices   []string
	regKey    string
	pin       Pin
	baseUnit  []byte
}

// rendered is a team's files in memory and their digest sets.
type rendered struct {
	config, dropIn, teamFile []byte
	digests                  digests
}

var (
	regKeyGrammar = regexp.MustCompile(`^[0-9a-f]{128}$`)
	regKeyLine    = regexp.MustCompile(`(?m)^registration_shared_secret = "([0-9a-f]{128})"$`)
	memoryGrammar = regexp.MustCompile(`^[1-9][0-9]*[KMGT]?$`)
	cacheGrammar  = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)
	ifaceGrammar  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,15}$`)
)

// render is step 3's in-memory render (C 3.2.3, 3.2.4, DR8).
func render(s renderSpec) (rendered, error) {
	if err := s.check(); err != nil {
		return rendered{}, err
	}
	d := dropInSpec{
		team: s.team.ID, version: s.pin.Version, memoryHigh: s.pin.MemoryHigh, port: s.team.Port,
		listen: s.team.Listen, allowFrom: s.allowFrom, devices: s.devices,
	}
	config := renderConfig(s)
	r := rendered{config: config, dropIn: d.text(), teamFile: s.team.Canonical()}
	r.digests = digests{
		Restart: map[string]string{
			digestBaseUnit: digest(s.baseUnit), digestExecStart: digest([]byte(d.execStart())),
			digestConfig: digest(config),
		},
		Reload: map[string]string{
			digestIPAllow: digest([]byte(d.ipAllow())), digestSocketBind: digest([]byte(d.socketBind())),
			digestMemory: digest([]byte(d.memory())), digestUnit: digest([]byte(d.unit())),
		},
	}
	return r, nil
}

func (s renderSpec) check() error {
	var problems []error
	if !regKeyGrammar.MatchString(s.regKey) {
		problems = append(problems, errors.New("the registration key is not 128 lowercase hex digits"))
	}
	if s.pin.Version == (paths.ServerVersion{}) {
		problems = append(problems, errors.New("no Tuwunel version is pinned"))
	}
	if !memoryGrammar.MatchString(s.pin.MemoryHigh) {
		problems = append(problems, fmt.Errorf("MemoryHigh %q is not a positive systemd size", s.pin.MemoryHigh))
	}
	if f, err := strconv.ParseFloat(s.pin.CacheCapacityModifier, 64); err != nil || f <= 0 || !cacheGrammar.MatchString(s.pin.CacheCapacityModifier) {
		problems = append(problems, fmt.Errorf("cache_capacity_modifier %q is not a positive decimal", s.pin.CacheCapacityModifier))
	}
	return errors.Join(problems...)
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func header(team paths.TeamID) string {
	return "# Rendered by " + paths.AdminBinary + " team apply for team " + team.String() + "; do not edit.\n"
}

// renderConfig is the Tuwunel config: bare keys, then the login limit tables (C 3.2.4). Every
// string in it is printable ASCII (a DNS name, a fixed path, hex), so Go's quoting is TOML's.
func renderConfig(s renderSpec) []byte {
	const off = "false"
	t := s.team
	q := strconv.Quote
	listen := []string{q(loopbackListen)}
	for _, a := range t.Listen {
		if a != loopbackListen {
			listen = append(listen, q(a))
		}
	}
	keys := [][2]string{
		{"server_name", q(t.ServerName)},
		{"address", "[" + strings.Join(listen, ", ") + "]"},
		{"port", strconv.Itoa(t.Port)},
		{"database_path", q(paths.HomeserverDatabase(t.ID))},
		{"database_backup_path", q(paths.HomeserverBackups(t.ID))},
		{"database_backups_to_keep", "7"},
		{"allow_registration", off},
		{"allow_guest_registration", off},
		{"registration_shared_secret", q(s.regKey)},
		{"grant_admin_to_first_user", off},
		{"login_via_token", off},
		{"login_via_existing_session", off},
		{"allow_federation", off},
		{"trusted_servers", "[]"},
		{"federate_admin_room", off},
		{"admin_escape_commands", off},
		{"allow_encryption", off},
		{"allow_room_creation", off},
		{"auto_accept_invites", off},
		{"new_user_displayname_suffix", `""`},
		{"client_sync_timeout_min", "0"},
		{"default_room_version", q(proto.RoomVersion)},
		{"sentry", off},
		{"log", `"warn"`},
		{"admin_signal_execute", `["server backup-database"]`},
		{"error_on_unknown_config_opts", "true"},
		{"rocksdb_allow_fallocate", off},
		{"max_request_size", "10000000"},
		{"enable_set_displayname", off},
		{"presence_idle_timeout_s", "120"},
		{"presence_offline_timeout_s", "60"},
		{"cache_capacity_modifier", s.pin.CacheCapacityModifier},
	}
	var b strings.Builder
	b.WriteString(header(t.ID))
	for _, kv := range keys {
		b.WriteString(kv[0] + " = " + kv[1] + "\n")
	}
	b.WriteString("\n[rate_limiting.login.failed]\nper_second = 0.0033\nburst_count = 3\n")
	b.WriteString("\n[rate_limiting.login.account]\nper_second = 0.006\nburst_count = 10\n")
	return []byte(b.String())
}

// regKeyOf is the registration key a rendered config holds, or "" when it holds none.
func regKeyOf(config []byte) string {
	m := regKeyLine.FindSubmatch(config)
	if m == nil {
		return ""
	}
	return string(m[1])
}

// dropInSpec is what a drop-in says (C 3.2.3).
type dropInSpec struct {
	team       paths.TeamID
	version    paths.ServerVersion
	memoryHigh string
	port       int
	listen     []string
	allowFrom  []string
	devices    []string
}

func (d dropInSpec) unit() string {
	if len(d.devices) == 0 {
		return "[Unit]\n"
	}
	devices := strings.Join(d.devices, " ")
	return "[Unit]\nAfter=" + devices + "\nWants=" + devices + "\n"
}

func (d dropInSpec) execStart() string { return "ExecStart=" + paths.ServerBinary(d.version) + "\n" }

func (d dropInSpec) memory() string { return "MemoryHigh=" + d.memoryHigh + "\n" }

func (d dropInSpec) ipAllow() string {
	allow := slices.Clone(loopbackAllow)
	add := func(p string) {
		if !slices.Contains(allow, p) {
			allow = append(allow, p)
		}
	}
	for _, a := range d.listen {
		if addr, err := netip.ParseAddr(a); err == nil {
			add(netip.PrefixFrom(addr, addr.BitLen()).String())
		}
	}
	for _, p := range d.allowFrom {
		add(p)
	}
	return "IPAddressAllow=" + strings.Join(allow, " ") + "\n"
}

func (d dropInSpec) socketBind() string { return "SocketBindAllow=tcp:" + strconv.Itoa(d.port) + "\n" }

func (d dropInSpec) text() []byte {
	return []byte(header(d.team) + d.unit() + "\n[Service]\n" + d.execStart() + d.memory() + d.ipAllow() + d.socketBind())
}

// validInterface is whether name is a Linux interface name a device unit can be made of.
func validInterface(name string) bool {
	return ifaceGrammar.MatchString(name) && name != "." && name != ".."
}

// deviceUnit is systemd's device unit of a network interface: `-` escaped, and a leading `.`.
func deviceUnit(iface string) string {
	escaped := strings.ReplaceAll(iface, "-", `\x2d`)
	if rest, ok := strings.CutPrefix(escaped, "."); ok {
		escaped = `\x2e` + rest
	}
	return "sys-subsystem-net-devices-" + escaped + ".device"
}
