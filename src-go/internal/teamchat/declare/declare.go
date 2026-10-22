// Package declare is the host's reader of the v2 declarations (Plan 00209,
// DESIGN-v2-C-host.md 3.8; SPEC-n2b.md DR1-DR8 and AG1-AG7): the types `declare check`,
// `team apply` and `team approve` share, their strict JSON reader, the canonical `team.json`,
// and the approval gate of DC18 over a team's security fields (`approved.json`, the comparison
// and the approve logic). It reads JSON only: the host_vars YAML reader is
// declare/hostvars, which `teamchat-admin` never imports (DC17).
package declare

import (
	"fmt"
	"net/netip"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// maxInput is the largest declaration read (DR2).
const maxInput = 1 << 20

// State is a team's or an LXC member's declared state.
type State string

// The declared states.
const (
	StatePresent State = "present"
	StateAbsent  State = "absent"
)

// The declaration's own keys (C 3.8). The top-level keys are paths' host_vars names.
const (
	keyTeam, keyName, keyState, keyPurge       = "team", "name", "state", "purge"
	keyPort, keyServerName, keyDescription     = "port", "server_name", "description"
	keyListen, keyAllowFrom, keyURL            = "listen", "allow_from", "url"
	keyHumans, keyCoordinator, keyTrust        = "humans", "coordinator", "trust"
	keyContainer, keyUser, keyCheckout         = "container", "user", "checkout"
	keySite, keySeat, keyHumanText, keyService = "site", "seat", "human_text", "service"
	keyLXCMembers, keyAddress, keyVersion      = "lxc_members", "address", "v"
)

// Declaration is the v2 keys of host_vars.
type Declaration struct {
	// Address is teamchat_address, empty when undeclared.
	Address string
	Teams   []Team
	LXC     []LXCMember
	Element []ElementTeam
}

// Team is one teamchat_teams entry, its defaults filled. An absent team holds only what it
// declared.
type Team struct {
	ID          paths.TeamID
	State       State
	Purge       bool
	Name        string
	Port        int
	ServerName  string
	Description string
	Listen      []string
	AllowFrom   []string
	// URL is the members' base URL: the declared one, or http://<address>:<port> (DR3).
	URL         string
	Humans      []string
	Coordinator string
	Trust       proto.Forge
}

// LXCMember is one teamchat_lxc_members entry.
type LXCMember struct {
	Team      paths.TeamID
	Container string
	User      paths.GuestUser
	Checkout  string
	Site      string
	Seat      int
	HumanText bool
	Service   bool
	State     State
}

// ElementTeam is one teamchat_element_teams entry: the team alone (C 3.10).
type ElementTeam struct {
	Team paths.TeamID
}

// Decode reads a declaration strictly (DR1-DR6).
func Decode(raw []byte) (Declaration, error) {
	v, err := parse(raw)
	if err != nil {
		return Declaration{}, err
	}
	return FromValue(v)
}

// parse is proto's strict JSON, capped at maxInput.
func parse(raw []byte) (proto.Value, error) {
	if len(raw) > maxInput {
		return proto.Value{}, fmt.Errorf("declaration: over %d bytes", maxInput)
	}
	v, err := proto.Parse(raw)
	if err != nil {
		return proto.Value{}, fmt.Errorf("declaration: %w", err)
	}
	return v, nil
}

// FromValue reads a declaration from an already parsed value, the YAML reader's way in.
func FromValue(v proto.Value) (Declaration, error) {
	root := node{v: v}
	f, err := root.object(nil, paths.HostVarAddress, paths.HostVarTeams, paths.HostVarLXCMembers, paths.HostVarElementTeams)
	if err != nil {
		if root.v.Kind() != proto.KindObject {
			return Declaration{}, &FieldError{Field: "the declaration", Problem: "is not an object"}
		}
		return Declaration{}, err
	}
	var d Declaration
	if f.has(paths.HostVarAddress) {
		if d.Address, err = f.at(paths.HostVarAddress).checked(validAddress, "is not an IP address in canonical form, or is unspecified"); err != nil {
			return Declaration{}, err
		}
	}
	if d.Teams, err = readTeams(f, paths.HostVarTeams, d.Address); err != nil {
		return Declaration{}, err
	}
	if d.LXC, err = readLXC(f, paths.HostVarLXCMembers, d.presentTeams()); err != nil {
		return Declaration{}, err
	}
	if d.Element, err = readElement(f, d.presentTeams()); err != nil {
		return Declaration{}, err
	}
	return d, nil
}

func (d Declaration) presentTeams() map[string]bool {
	present := map[string]bool{}
	for _, t := range d.Teams {
		if t.State == StatePresent {
			present[t.ID.String()] = true
		}
	}
	return present
}

// optList is k's list, empty when k is absent.
func (f fields) optList(k string) ([]node, error) {
	if !f.has(k) {
		return nil, nil
	}
	return f.at(k).list()
}

func readTeams(f fields, key, address string) ([]Team, error) {
	items, err := f.optList(key)
	if err != nil {
		return nil, err
	}
	out := make([]Team, 0, len(items))
	ids, ports := map[string]bool{}, map[int]bool{}
	for _, item := range items {
		t, err := readTeam(item, address)
		if err != nil {
			return nil, err
		}
		if ids[t.ID.String()] {
			return nil, item.key(keyTeam).fail("is declared twice")
		}
		ids[t.ID.String()] = true
		if t.Port != 0 {
			if ports[t.Port] {
				return nil, item.key(keyPort).fail("is another team's port")
			}
			ports[t.Port] = true
		}
		out = append(out, t)
	}
	return out, nil
}

var teamOptional = []string{
	keyName, keyPurge, keyPort, keyServerName, keyDescription, keyListen, keyAllowFrom, keyURL,
	keyHumans, keyCoordinator, keyTrust,
}

// readTeam reads one team entry (DR3).
func readTeam(n node, address string) (Team, error) {
	f, err := n.object([]string{keyTeam, keyState}, teamOptional...)
	if err != nil {
		return Team{}, err
	}
	var t Team
	if t.ID, err = teamID(f.at(keyTeam)); err != nil {
		return Team{}, err
	}
	if t.State, err = state(f.at(keyState)); err != nil {
		return Team{}, err
	}
	if t.Purge, err = f.optBool(keyPurge, false); err != nil {
		return Team{}, err
	}
	if t.Purge && t.State != StateAbsent {
		return Team{}, f.at(keyPurge).fail("is allowed only with state absent")
	}
	if t.State == StatePresent {
		for _, k := range []string{keyPort, keyDescription, keyListen, keyAllowFrom, keyHumans, keyTrust} {
			if !f.has(k) {
				return Team{}, f.at(k).fail("is required for a present team")
			}
		}
	}
	if err := readValues(&t, f, address); err != nil {
		return Team{}, err
	}
	return t, nil
}

func teamID(n node) (paths.TeamID, error) {
	s, err := n.str()
	if err != nil {
		return paths.TeamID{}, err
	}
	id, err := paths.ParseTeamID(s)
	if err != nil {
		return paths.TeamID{}, n.fail("is not a valid team id, or is reserved")
	}
	return id, nil
}

func state(n node) (State, error) {
	s, err := n.str()
	if err != nil {
		return "", err
	}
	if State(s) != StatePresent && State(s) != StateAbsent {
		return "", n.fail("is not present or absent")
	}
	return State(s), nil
}

// readValues reads every other key the entry carries, and fills a present team's defaults.
func readValues(t *Team, f fields, address string) error {
	var err error
	readers := []struct {
		key  string
		read func(node) error
	}{
		{keyName, func(n node) error { t.Name, err = n.checked(validDisplayName, "is not a display name"); return err }},
		{keyPort, func(n node) error { t.Port, err = port(n); return err }},
		{keyServerName, func(n node) error {
			t.ServerName, err = n.checked(validServerName, "is not a lower-case DNS name of two labels or more")
			return err
		}},
		{keyDescription, func(n node) error {
			t.Description, err = n.checked(proto.ValidDescription, "is not 1-120 characters, trimmed, free of control characters")
			return err
		}},
		{keyListen, func(n node) error { t.Listen, err = n.listen(); return err }},
		{keyAllowFrom, func(n node) error { t.AllowFrom, err = n.allowFrom(); return err }},
		{keyURL, func(n node) error { t.URL, err = n.url(); return err }},
		{keyHumans, func(n node) error { t.Humans, err = n.humans(); return err }},
		{keyCoordinator, func(n node) error { t.Coordinator, err = n.coordinator(); return err }},
		{keyTrust, func(n node) error { t.Trust, err = n.trust(); return err }},
	}
	for _, r := range readers {
		if f.has(r.key) {
			if err := r.read(f.at(r.key)); err != nil {
				return err
			}
		}
	}
	if t.State != StatePresent {
		return nil
	}
	if t.Name == "" {
		t.Name = t.ID.String()
	}
	if t.ServerName == "" {
		t.ServerName = paths.ServerName(t.ID)
	}
	if t.URL == "" {
		if address == "" {
			return f.at(keyURL).fail("is required when teamchat_address is not declared")
		}
		t.URL = defaultURL(address, t.Port)
	}
	return nil
}

func validDisplayName(s string) bool {
	_, err := paths.ParseDisplayName(s)
	return err == nil
}

// defaultURL is http://<address>:<port>, brackets around an IPv6 address.
func defaultURL(address string, port int) string {
	host := address
	if a, err := netip.ParseAddr(address); err == nil && a.Is6() {
		host = "[" + address + "]"
	}
	return "http://" + host + ":" + strconv.Itoa(port)
}

func port(n node) (int, error) {
	i, err := n.integer()
	if err != nil {
		return 0, err
	}
	p, err := paths.ParsePort(int(i))
	if err != nil || int64(p.Int()) != i {
		return 0, n.fail("is not a port, 1-65535")
	}
	return p.Int(), nil
}

var lxcOptional = []string{keySite, keySeat, keyHumanText, keyService, keyState}

// readLXC reads the LXC entries (DR4); each names a present team.
func readLXC(f fields, key string, present map[string]bool) ([]LXCMember, error) {
	items, err := f.optList(key)
	if err != nil {
		return nil, err
	}
	out := make([]LXCMember, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		m, err := readLXCMember(item, present)
		if err != nil {
			return nil, err
		}
		k := m.Team.String() + "\x00" + m.Container + "\x00" + m.User.String()
		if seen[k] {
			return nil, item.fail("repeats another entry's team, container and user")
		}
		seen[k] = true
		out = append(out, m)
	}
	return out, nil
}

func readLXCMember(n node, present map[string]bool) (LXCMember, error) {
	f, err := n.object([]string{keyTeam, keyContainer, keyUser, keyCheckout}, lxcOptional...)
	if err != nil {
		return LXCMember{}, err
	}
	m := LXCMember{HumanText: true, State: StatePresent}
	if m.Team, err = teamID(f.at(keyTeam)); err != nil {
		return LXCMember{}, err
	}
	if !present[m.Team.String()] {
		return LXCMember{}, f.at(keyTeam).fail("is not a team declared present")
	}
	if m.Container, err = f.at(keyContainer).checked(validContainer, "is not a container name"); err != nil {
		return LXCMember{}, err
	}
	if m.User, err = guestUser(f.at(keyUser)); err != nil {
		return LXCMember{}, err
	}
	if m.Checkout, err = f.at(keyCheckout).checked(validCheckout, "is not an absolute, clean path below /"); err != nil {
		return LXCMember{}, err
	}
	return m, readLXCOptional(&m, f)
}

func readLXCOptional(m *LXCMember, f fields) error {
	var err error
	if f.has(keySite) {
		if m.Site, err = f.at(keySite).checked(validSite, "is not a site label in its normal form"); err != nil {
			return err
		}
	}
	if f.has(keySeat) {
		n := f.at(keySeat)
		seat, seatErr := n.integer()
		if seatErr != nil {
			return seatErr
		}
		if seat < 1 || seat > maxSeat {
			return n.fail("is not a seat number, 1-9999")
		}
		m.Seat = int(seat)
	}
	if m.HumanText, err = f.optBool(keyHumanText, true); err != nil {
		return err
	}
	if m.Service, err = f.optBool(keyService, false); err != nil {
		return err
	}
	if f.has(keyState) {
		if m.State, err = state(f.at(keyState)); err != nil {
			return err
		}
	}
	return nil
}

func guestUser(n node) (paths.GuestUser, error) {
	s, err := n.str()
	if err != nil {
		return paths.GuestUser{}, err
	}
	u, err := paths.ParseGuestUser(s)
	if err != nil {
		return paths.GuestUser{}, n.fail("is not a user name")
	}
	return u, nil
}

// readElement reads the Element entries (DR5): a present team each, at most once.
func readElement(f fields, present map[string]bool) ([]ElementTeam, error) {
	items, err := f.optList(paths.HostVarElementTeams)
	if err != nil {
		return nil, err
	}
	out := make([]ElementTeam, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		ef, err := item.object([]string{keyTeam})
		if err != nil {
			return nil, err
		}
		id, err := teamID(ef.at(keyTeam))
		if err != nil {
			return nil, err
		}
		switch {
		case !present[id.String()]:
			return nil, ef.at(keyTeam).fail("is not a team declared present")
		case seen[id.String()]:
			return nil, ef.at(keyTeam).fail("has a profile already")
		}
		seen[id.String()] = true
		out = append(out, ElementTeam{Team: id})
	}
	return out, nil
}
