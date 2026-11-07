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

// loopbackListen is the address every homeserver binds besides the team's derived listen addresses.
const loopbackListen = "127.0.0.1"

// loopbackAllow is what every drop-in allows besides the listen addresses and the effective networks.
var loopbackAllow = []string{"127.0.0.1/32", "::1/128"}

// The digest set members (C 3.5.2 step 3).
const (
	digestBaseUnit   = "base_unit"
	digestExecStart  = "exec_start"
	digestConfig     = "config"
	digestIPAllow    = "ip_address_allow"
	digestSocketBind = "socket_bind_allow"
	digestMemory     = "memory_high"
)

// digests are the restart and reload digest sets units.json keeps.
type digests struct {
	Restart, Reload map[string]string
}

func (d digests) sameRestart(o digests) bool { return maps.Equal(d.Restart, o.Restart) }

func (d digests) sameReload(o digests) bool { return maps.Equal(d.Reload, o.Reload) }

// renderSpec is what one render reads: the team, the listen addresses and effective networks
// derived at apply time (X3, X4), which are the terminator's alone (X18), the device unit of each
// interface carrying a listen address, the registration key, the pin and the installed base
// unit's text.
type renderSpec struct {
	team     declare.Team
	listen   []netip.Addr
	networks []netip.Prefix
	devices  []string
	regKey   string
	pin      Pin
	baseUnit []byte
}

// rendered is a team's files in memory and their digest sets: the homeserver's config and
// drop-in, the terminator's drop-in (which follows the networks, X4) and team.json.
type rendered struct {
	config, dropIn, tlsDropIn, teamFile []byte
	digests                             digests
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
	d, tls := dropInSpecs(s.team.ID, s.team.Port, s.pin, s.listen, s.networks, s.devices)
	config := renderConfig(s)
	r := rendered{config: config, dropIn: d.text(), tlsDropIn: tls.text(), teamFile: s.team.Canonical()}
	r.digests = digests{
		Restart: map[string]string{
			digestBaseUnit: digest(s.baseUnit), digestExecStart: digest([]byte(d.execStart())),
			digestConfig: digest(config),
		},
		Reload: map[string]string{
			digestIPAllow: digest([]byte(d.ipAllow())), digestSocketBind: digest([]byte(d.socketBind())),
			digestMemory: digest([]byte(d.memory())),
		},
	}
	return r, nil
}

// dropInSpecs are a team's two drop-ins, the homeserver's and the terminator's, as render makes them.
func dropInSpecs(team paths.TeamID, port int, pin Pin, listen []netip.Addr, networks []netip.Prefix, devices []string) (dropInSpec, tlsDropInSpec) {
	return dropInSpec{team: team, version: pin.Version, memoryHigh: pin.MemoryHigh, port: port},
		tlsDropInSpec{team: team, port: port, listen: listen, networks: networks, devices: devices}
}

// DropIns are the texts of a team's two drop-ins, the homeserver's then the terminator's, as
// render makes them: what the unit-text judges outside this package read, so that every unit
// text the Go code makes is judged in one place (v1names' units test).
func DropIns(team paths.TeamID, port int, pin Pin, listen []netip.Addr, networks []netip.Prefix, devices []string) ([]byte, []byte) {
	d, tls := dropInSpecs(team, port, pin, listen, networks, devices)
	return d.text(), tls.text()
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

// renderConfig is the Tuwunel config: bare keys, then the login limit tables (C 3.2.4). The
// homeserver binds loopback alone and stays plain there; the team's TLS terminator is what listens
// on the derived addresses (X18). Every string in it is printable ASCII (a DNS name, a fixed path,
// hex), so Go's quoting is TOML's.
func renderConfig(s renderSpec) []byte {
	const off = "false"
	t := s.team
	q := strconv.Quote
	keys := [][2]string{
		{"server_name", q(t.ServerName)},
		{"address", "[" + q(loopbackListen) + "]"},
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

// RegKeyOf is the registration key a rendered config holds, or "" when it holds none; a
// restore's bootstrap reads it from the installed config (RS7).
func RegKeyOf(config []byte) string {
	m := regKeyLine.FindSubmatch(config)
	if m == nil {
		return ""
	}
	return string(m[1])
}

// dropInSpec is what the homeserver's drop-in says (C 3.2.3): its binary, its memory ceiling, the
// loopback it may talk to and the port it may bind. Nothing of it depends on the networks.
type dropInSpec struct {
	team       paths.TeamID
	version    paths.ServerVersion
	memoryHigh string
	port       int
}

func (d dropInSpec) execStart() string { return "ExecStart=" + paths.ServerBinary(d.version) + "\n" }

func (d dropInSpec) memory() string { return "MemoryHigh=" + d.memoryHigh + "\n" }

func (d dropInSpec) ipAllow() string { return ipAllowLine(nil, nil) }

func (d dropInSpec) socketBind() string { return socketBindLine(d.port) }

func (d dropInSpec) text() []byte {
	return []byte(header(d.team) + "[Service]\n" + d.execStart() + d.memory() + d.ipAllow() + d.socketBind())
}

// ipAllowLine is a unit's IPAddressAllow: loopback, then each listen address as a host prefix and
// each network, once each.
func ipAllowLine(listen []netip.Addr, networks []netip.Prefix) string {
	allow := slices.Clone(loopbackAllow)
	add := func(p netip.Prefix) {
		if !slices.Contains(allow, p.String()) {
			allow = append(allow, p.String())
		}
	}
	for _, a := range listen {
		add(netip.PrefixFrom(a, a.BitLen()))
	}
	for _, p := range networks {
		add(p)
	}
	return "IPAddressAllow=" + strings.Join(allow, " ") + "\n"
}

// socketBindLine is a unit's SocketBindAllow: the team's port, and no other.
func socketBindLine(port int) string { return "SocketBindAllow=tcp:" + strconv.Itoa(port) + "\n" }

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
