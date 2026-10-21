package declare

import (
	"fmt"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/pycompat"
)

// The keys of an agent_bus_lxc_members entry (members.py REQUIRED_KEYS, OPTIONAL_KEYS).
var (
	lxcRequired = []string{"container", keyTeam, "user"}
	lxcOptional = []string{"host", "checkout", "seat", "role", "human_text", "repo", "state", "service"}
)

// The roles (protocol.py ROLES) and the variable a member's env file sets once.
const (
	roleCoordinator = "coordinator"
	roleWorker      = "worker"
	roleVar         = "HOOKS_DAEMON_HOSTNAME"
)

// optional is a string an entry may leave out (python's None).
type optional struct {
	text string
	set  bool
}

// member is members.py's Member, as parse_members makes it.
type member struct {
	team, container, user string
	host, checkout, seat  optional
	repo                  optional
	role, state           string
	humanText, service    bool
}

func (m member) label() string { return m.user + "@" + m.container }

// LxcMembers is declare.py lxc-members (V23): every agent_bus_lxc_members entry read as the
// play reads it and judged by members.parse_members, in file order.
func (h HostVars) LxcMembers() ([]LxcMember, error) {
	hv, err := parse(string(h), lxcKey)
	if err != nil {
		return nil, err
	}
	entries := make([]*pycompat.Dict, 0, len(hv.teams))
	for _, e := range hv.teams {
		d, err := entryOf(e)
		if err != nil {
			return nil, err
		}
		entries = append(entries, d)
	}
	found := make([]member, 0, len(entries))
	for i, d := range entries {
		m, err := parseEntry(i+1, d)
		if err != nil {
			return nil, err
		}
		found = append(found, m)
	}
	if err := checkBetween(found); err != nil {
		return nil, err
	}
	out := make([]LxcMember, 0, len(found))
	for _, m := range found {
		lm := LxcMember{
			Team: m.team, Container: m.container, User: m.user, Host: m.host.text, Role: m.role,
			State: m.state, Checkout: m.checkout.text, Seat: m.seat.text,
		}
		if err := printable(lm.Checkout); err != nil {
			return nil, err
		}
		out = append(out, lm)
	}
	return out, nil
}

// entryOf is declare.py's entry_of: every key's value, in the entry's order.
func entryOf(e entry) (*pycompat.Dict, error) {
	d := pycompat.NewDict()
	for _, k := range e.raw {
		v, err := value(k.rest, k.nested, fmt.Sprintf("%s's %s on line %d", e.team, k.key, k.index+1))
		if err != nil {
			return nil, err
		}
		d.Set(k.key, v)
	}
	return d, nil
}

// optionalString is an entry's string key: unset when it is absent, refused when it is not a
// string.
func optionalString(d *pycompat.Dict, key, where, refusal string) (optional, error) {
	v, ok := d.Get(key)
	if !ok {
		return optional{}, nil
	}
	s, ok := v.(pycompat.Str)
	if !ok {
		return optional{}, refusedf("%s: %s", where, refusal)
	}
	return optional{text: string(s), set: true}, nil
}

func flagOf(d *pycompat.Dict, key string, otherwise bool, where string) (bool, error) {
	v, ok := d.Get(key)
	if !ok {
		return otherwise, nil
	}
	b, ok := v.(pycompat.Bool)
	if !ok {
		return false, refusedf("%s: %s must be true or false", where, key)
	}
	return bool(b), nil
}

// keyProblem is members.py's missing and unknown keys of an entry, or "".
func keyProblem(d *pycompat.Dict) string {
	keys := d.Keys()
	var missing, unknown []string
	for _, k := range lxcRequired {
		if !slices.Contains(keys, k) {
			missing = append(missing, k)
		}
	}
	for _, k := range keys {
		if !slices.Contains(lxcRequired, k) && !slices.Contains(lxcOptional, k) {
			unknown = append(unknown, k)
		}
	}
	if len(missing) == 0 && len(unknown) == 0 {
		return ""
	}
	slices.Sort(unknown)
	nothing := func(s []string) string {
		if len(s) == 0 {
			return "nothing"
		}
		return strings.Join(s, ", ")
	}
	return "missing " + nothing(missing) + "; unknown " + nothing(unknown)
}

// parseEntry is members.py's _parse_entry: the entry's keys, then each value in v1's order.
func parseEntry(position int, d *pycompat.Dict) (member, error) {
	where := fmt.Sprintf("agent_bus_lxc_members entry %d", position)
	if problem := keyProblem(d); problem != "" {
		return member{}, refusedf("%s: %s", where, problem)
	}
	m, err := identityOf(d, where)
	if err != nil {
		return member{}, err
	}
	if err = placeOf(d, where, &m); err != nil {
		return member{}, err
	}
	if m.seat, err = seatOf(d, where); err != nil {
		return member{}, err
	}
	m.role = roleWorker
	if v, ok := d.Get("role"); ok {
		s, isStr := v.(pycompat.Str)
		if isStr && s == "orchestrator" {
			s = roleCoordinator
		}
		if !isStr || s != roleCoordinator && s != roleWorker {
			return member{}, refusedf("%s: role must be one of %s, %s", where, roleCoordinator, roleWorker)
		}
		m.role = string(s)
	}
	if m.humanText, err = flagOf(d, "human_text", true, where); err != nil {
		return member{}, err
	}
	m.service, err = flagOf(d, "service", false, where)
	return m, err
}

// identityOf is the entry's state, team, container and user, which keyProblem has seen set.
func identityOf(d *pycompat.Dict, where string) (member, error) {
	state, ok := d.Get("state")
	if !ok {
		state = pycompat.Str(statePresent)
	}
	if !pycompat.Equal(state, pycompat.Str(statePresent)) && !pycompat.Equal(state, pycompat.Str(stateAbsent)) {
		return member{}, refusedf("%s: state must be present or absent", where)
	}
	m := member{state: pycompat.PyStr(state)}
	for _, f := range []struct {
		key  string
		into *string
		ok   func(string) bool
		why  string
	}{
		{keyTeam, &m.team, teamNameRE.MatchString, "team must match " + teamNamePattern},
		{"container", &m.container, containerRE.MatchString, "container must be an LXC container name"},
		{"user", &m.user, userRE.MatchString, "user must be a user name"},
	} {
		v, has := d.Get(f.key)
		s, isStr := v.(pycompat.Str)
		if !has || !isStr || !f.ok(string(s)) {
			return member{}, refusedf("%s: %s", where, f.why)
		}
		*f.into = string(s)
	}
	return m, nil
}

// placeOf is the entry's host, checkout and repo, and the rule that a member needs a place.
func placeOf(d *pycompat.Dict, where string, m *member) error {
	host, err := optionalString(d, "host", where, "host must be a string")
	if err != nil {
		return err
	}
	if host.set {
		if host.text, err = resolveHost(host.text); err != nil {
			return refusedf("%s: host: %v", where, err)
		}
		m.host = host
	}
	if m.checkout, err = optionalString(d, "checkout", where, "checkout must be an absolute path inside the container"); err != nil {
		return err
	}
	if m.checkout.set && !strings.HasPrefix(m.checkout.text, "/") {
		return refusedf("%s: checkout must be an absolute path inside the container", where)
	}
	repo, err := optionalString(d, "repo", where, "repo must be a string")
	if err != nil {
		return err
	}
	if repo.set {
		if repo.text, err = normaliseRepo(repo.text); err != nil {
			return refusedf("%s: repo: %v", where, err)
		}
		m.repo = repo
	}
	if m.state == statePresent && !m.checkout.set {
		return refusedf("%s: a present member needs checkout, where suggest-handle runs", where)
	}
	if m.state == stateAbsent && !m.checkout.set && !m.repo.set {
		return refusedf("%s: an absent member needs checkout or repo, to know its handle", where)
	}
	return nil
}

// seatOf is the entry's seat: an integer is its decimal text; it must fit the seat grammar.
func seatOf(d *pycompat.Dict, where string) (optional, error) {
	v, ok := d.Get("seat")
	if !ok {
		return optional{}, nil
	}
	if n, isInt := v.(pycompat.Int); isInt {
		v = pycompat.Str(n.V.String())
	}
	s, isStr := v.(pycompat.Str)
	if !isStr || !seatRE.MatchString(string(s)) {
		return optional{}, refusedf("%s: seat must match %s", where, seatPattern)
	}
	return optional{text: string(s), set: true}, nil
}

type seatKey struct{ team, host, seat string }

type session struct{ container, user string }

// checkBetween is members.py's _check_between: the rules between entries, in its order. A
// host or seat the entry leaves out is not judged.
func checkBetween(found []member) error {
	entries := map[[3]string]int{}
	hosts := map[session]string{}
	seats := map[seatKey][]member{}
	coordinators := map[string]int{}
	sessions := map[session]member{}
	for i, m := range found {
		position, s := i+1, session{m.container, m.user}
		if err := checkSession(sessions, s, m); err != nil {
			return err
		}
		key := [3]string{m.container, m.user, m.team}
		if first, seen := entries[key]; seen {
			return refusedf("agent_bus_lxc_members entries %d and %d both declare %s in team %s", first, position, m.label(), m.team)
		}
		entries[key] = position
		if err := checkHostAndSeat(hosts, seats, s, m); err != nil {
			return err
		}
		if m.state == statePresent && m.role == roleCoordinator {
			if first, seen := coordinators[m.team]; seen {
				return refusedf("agent_bus_lxc_members entries %d and %d both declare the coordinator of team %s; a team has one", first, position, m.team)
			}
			coordinators[m.team] = position
		}
	}
	return nil
}

// checkSession is the rule that one user's present entries agree on service, and with
// service on one checkout.
func checkSession(sessions map[session]member, s session, m member) error {
	if m.state != statePresent {
		return nil
	}
	first, seen := sessions[s]
	if !seen {
		sessions[s] = m
		return nil
	}
	if first.service != m.service {
		return refusedf("agent_bus_lxc_members: the entries of %s differ on service; the user runs one session for all its teams, so set it alike on each", m.label())
	}
	if m.service && first.checkout != m.checkout {
		return refusedf("agent_bus_lxc_members: the entries of %s with service: true name two checkouts; its one supervised session runs in one checkout", m.label())
	}
	return nil
}

// checkHostAndSeat is the rule of one host per user and container, and of one container per
// team, host and seat (within one repo, or when either repo is not known).
func checkHostAndSeat(hosts map[session]string, seats map[seatKey][]member, s session, m member) error {
	if !m.host.set {
		return nil
	}
	if first, seen := hosts[s]; !seen {
		hosts[s] = m.host.text
	} else if first != m.host.text {
		return refusedf("agent_bus_lxc_members gives %s two hosts; its env file sets one %s", m.label(), roleVar)
	}
	if !m.seat.set {
		return nil
	}
	k := seatKey{m.team, m.host.text, m.seat.text}
	clash := func(other member) bool {
		return other.container != m.container && (!other.repo.set || !m.repo.set || other.repo.text == m.repo.text)
	}
	if slices.ContainsFunc(seats[k], clash) {
		return refusedf("agent_bus_lxc_members gives seat %s of team %s to two containers; set seat on one", m.seat.text, m.team)
	}
	seats[k] = append(seats[k], m)
	return nil
}
