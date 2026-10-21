package hooks

// Future home: bus/state (membership.py): the desired and applied membership files, the
// told records and their writers (H17, H83).

import (
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

const (
	membershipVar      = "PINGBUS_MEMBERSHIP"
	membershipOff      = "off"
	desiredFile        = "membership.json"
	appliedFile        = "applied.json"
	toldFile           = "told.json"
	toldRolesFile      = "told-roles.json"
	toldLockFile       = "told.lock"
	maxMembershipBytes = 65536
	maxSeats           = 64
	maxFarewells       = 256
	unknownRole        = "-"
	defaultFarewell    = "removed"
	noHolder           = "-"

	keyTeam       = "team"
	keyGeneration = "generation"
	keyRoles      = "roles"
	keyHandle     = "handle"
	keyReason     = "reason"
	keyRole       = "role"
	keySeat       = "seat"
)

var (
	teamNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,23}$`)
	seatPattern     = regexp.MustCompile(`^(?:[1-9][0-9]{0,5}|[a-z][a-z0-9]{0,11})$`)
	sessionPattern  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.@+-]{0,254}$`)
	holderPattern   = regexp.MustCompile(`^[a-z-]{1,32}$`)

	membershipKinds  = []string{"ccy", "cc"}
	farewellReasons  = map[string]string{defaultFarewell: "removed by the owner"}
	membershipRoles  = []string{roleWorker, roleCoordinator}
	refusedWhy       = []string{"held-elsewhere", "no-bundle", "bundle-invalid", "not-desired"}
	desiredKeys      = []string{"v", "session", "kind", "checkout", keyGeneration, "written_ms", "seats", "farewell"}
	appliedKeys      = []string{"v", keyGeneration, "seats", "refused"}
	appliedOptional  = []string{"told", "inbound"}
	toldKeys         = []string{"v", keyGeneration, "teams", "left"}
	toldRolesKeys    = []string{"v", keyRoles}
	membershipStates = []string{"held"}
)

type seatEntry struct{ team, seat string }

type farewell struct{ team, seat, reason string }

// desired is membership.Desired, the host's membership file.
type desired struct {
	session, kind, checkout string
	generation              pyjson.Number
	seats                   []seatEntry
	farewell                []farewell
}

type appliedSeat struct{ team, seat, handle, state string }

// applied is membership.Applied, the keeper's file in the session home (its seats; the rest
// is only checked).
type applied struct {
	seats []appliedSeat
}

type toldTeam struct{ team, seat, handle, role string }

type toldLeft struct{ team, reason string }

// toldRecord is membership.ToldRecord: what the session was last told about its teams.
type toldRecord struct {
	generation pyjson.Number
	teams      []toldTeam
	left       []toldLeft
}

func (r toldRecord) equal(other toldRecord) bool {
	return numberRepr(r.generation) == numberRepr(other.generation) && slices.Equal(r.teams, other.teams) && slices.Equal(r.left, other.left)
}

func (r toldRecord) json() pyobj {
	teams := make([]any, len(r.teams))
	for i, team := range r.teams {
		teams[i] = pyobj{{keyTeam, team.team}, {keySeat, team.seat}, {keyHandle, team.handle}, {keyRole, team.role}}
	}
	left := make([]any, len(r.left))
	for i, entry := range r.left {
		left[i] = pyobj{{keyTeam, entry.team}, {keyReason, entry.reason}}
	}
	return pyobj{{"v", 1}, {keyGeneration, r.generation}, {"teams", teams}, {"left", left}}
}

// text is a value already checked to be a string.
func text(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// number is a value already checked to be a number.
func number(value any) pyjson.Number {
	if n, ok := value.(pyjson.Number); ok {
		return n
	}
	return pyjson.Number{}
}

func intNumber(n int64) pyjson.Number {
	return pyjson.Number{Lit: strconv.FormatInt(n, 10), Kind: pyjson.Int}
}

// isIntAtLeast is membership._is_int: `type(v) is int and v >= low`.
func isIntAtLeast(value any, low int64) bool {
	n, ok := value.(pyjson.Number)
	if !ok || n.Kind != pyjson.Int {
		return false
	}
	if v, fits := n.Int64(); fits {
		return v >= low
	}
	return !strings.HasPrefix(n.Lit, "-")
}

// pyIn is Python's `value in container` for a tuple of strings.
func pyIn(value any, container []string) bool {
	text, ok := value.(string)
	return ok && slices.Contains(container, text)
}

// farewellReason is `value in FAREWELL_REASONS`, a dict: a list or an object cannot be hashed,
// which v1 raises as a TypeError (internal).
func farewellReason(value any) (bool, error) {
	switch v := value.(type) {
	case []any, pyjson.Object:
		return false, internalErr("TypeError")
	case string:
		_, ok := farewellReasons[v]
		return ok, nil
	}
	return false, nil
}

// readMembershipJSON is membership._read_json: no symlink, regular, at most 65536 bytes, strict
// UTF-8 JSON with no key twice; found is false when the file is absent.
func readMembershipJSON(path string) (any, bool, error) {
	got, err := readRegular(path, maxMembershipBytes)
	switch {
	case isErrno(err, syscall.ENOENT):
		return nil, false, nil
	case errors.Is(err, errNotRegular):
		return nil, false, membershipErr("%s: not a regular file", path)
	case errors.Is(err, errTooLarge):
		return nil, false, membershipErr("%s: larger than %d bytes", path, maxMembershipBytes)
	case err != nil:
		return nil, false, membershipErr("%s: cannot be read (%s)", path, strerror(err))
	}
	data, err := pyjson.DecodeUTF8(got.raw, pyjson.Options{NoDuplicateKeys: true})
	if err != nil {
		return nil, false, membershipErr("%s: not one JSON object", path)
	}
	return data, true, nil
}

// membershipObject is membership._object.
func membershipObject(data any, keys []string, where string, optional ...string) (pyjson.Object, error) {
	object, ok := data.(pyjson.Object)
	if !ok {
		return nil, membershipErr("%s: not an object", where)
	}
	for _, key := range keys {
		if _, present := object[key]; !present {
			return nil, membershipErr("%s: missing %s", where, key)
		}
	}
	for key := range object {
		if !slices.Contains(keys, key) && !slices.Contains(optional, key) {
			return nil, membershipErr("%s: unknown key %s", where, key)
		}
	}
	return object, nil
}

func membershipList(object pyjson.Object, key, where string, most int) ([]any, error) {
	list, ok := object[key].([]any)
	if !ok || len(list) > most {
		return nil, membershipErr("%s: %s is not a list of at most %d", where, key, most)
	}
	return list, nil
}

func membershipVersion(object pyjson.Object, where string) error {
	if n, ok := object["v"].(pyjson.Number); !ok || !n.IsInt(1) {
		return membershipErr("%s: v is not 1", where)
	}
	return nil
}

func isTeamName(value any) bool { return matches(teamNamePattern, value) }
func isSeatName(value any) bool { return matches(seatPattern, value) }

func seatNames(item pyjson.Object, key, where string) (string, string, error) {
	if !isTeamName(item[keyTeam]) || !isSeatName(item[keySeat]) {
		return "", "", membershipErr("%s: %s: an entry's team or seat is not a name", where, key)
	}
	return text(item[keyTeam]), text(item[keySeat]), nil
}

// isCheckout is membership's `/[^\x00-\x1f\x7f]{0,4095}`, counted in code points.
func isCheckout(value any) bool {
	text, ok := value.(string)
	if !ok || !strings.HasPrefix(text, "/") || pyLen(text) > 4096 {
		return false
	}
	return !strings.ContainsFunc(text, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

// parseDesired is membership.parse_desired.
func parseDesired(data any, where string) (desired, error) {
	object, err := membershipObject(data, desiredKeys, where)
	if err != nil {
		return desired{}, err
	}
	if err = membershipVersion(object, where); err != nil {
		return desired{}, err
	}
	switch {
	case !matches(sessionPattern, object["session"]):
		return desired{}, membershipErr("%s: session is not a session name", where)
	case !pyIn(object["kind"], membershipKinds):
		return desired{}, membershipErr("%s: kind is not one of ccy, cc", where)
	case !isCheckout(object["checkout"]):
		return desired{}, membershipErr("%s: checkout is not an absolute path", where)
	case !isIntAtLeast(object[keyGeneration], 1):
		return desired{}, membershipErr("%s: generation is not a positive integer", where)
	case !isIntAtLeast(object["written_ms"], 0):
		return desired{}, membershipErr("%s: written_ms is not a non-negative integer", where)
	}
	d := desired{
		session: text(object["session"]), kind: text(object["kind"]), checkout: text(object["checkout"]),
		generation: number(object[keyGeneration]),
	}
	if d.seats, err = desiredSeats(object, where); err != nil {
		return desired{}, err
	}
	d.farewell, err = desiredFarewells(object, where)
	return d, err
}

func desiredSeats(object pyjson.Object, where string) ([]seatEntry, error) {
	items, err := membershipList(object, "seats", where, maxSeats)
	if err != nil {
		return nil, err
	}
	var seats []seatEntry
	for _, item := range items {
		entry, err := membershipObject(item, []string{keyTeam, keySeat}, where+": seats")
		if err != nil {
			return nil, err
		}
		team, seat, err := seatNames(entry, "seats", where)
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(seats, func(s seatEntry) bool { return s.team == team }) {
			return nil, membershipErr("%s: seats: one seat per team, and team %s has two", where, team)
		}
		seats = append(seats, seatEntry{team, seat})
	}
	return seats, nil
}

func desiredFarewells(object pyjson.Object, where string) ([]farewell, error) {
	items, err := membershipList(object, "farewell", where, maxFarewells)
	if err != nil {
		return nil, err
	}
	var found []farewell
	for _, item := range items {
		entry, err := membershipObject(item, []string{keyTeam, keySeat, keyReason}, where+": farewell")
		if err != nil {
			return nil, err
		}
		team, seat, err := seatNames(entry, "farewell", where)
		if err != nil {
			return nil, err
		}
		known, err := farewellReason(entry[keyReason])
		if err != nil {
			return nil, err
		}
		if !known {
			return nil, membershipErr("%s: farewell: the reason is not one of removed", where)
		}
		found = append(found, farewell{team, seat, text(entry[keyReason])})
	}
	return found, nil
}

// parseApplied is membership.parse_applied.
func parseApplied(data any, where string) (applied, error) {
	object, err := membershipObject(data, appliedKeys, where, appliedOptional...)
	if err != nil {
		return applied{}, err
	}
	if err = membershipVersion(object, where); err != nil {
		return applied{}, err
	}
	if !isIntAtLeast(object[keyGeneration], 0) {
		return applied{}, membershipErr("%s: generation is not a non-negative integer", where)
	}
	var result applied
	if result.seats, err = appliedSeats(object, where); err != nil {
		return applied{}, err
	}
	if err := appliedRefused(object, where); err != nil {
		return applied{}, err
	}
	if told, present := object["told"]; present {
		if err := appliedTold(told, where); err != nil {
			return applied{}, err
		}
	}
	if inbound, present := object["inbound"]; present {
		if _, isBool := inbound.(bool); !isBool {
			return applied{}, membershipErr("%s: inbound is not true or false", where)
		}
	}
	return result, nil
}

func appliedSeats(object pyjson.Object, where string) ([]appliedSeat, error) {
	items, err := membershipList(object, "seats", where, maxSeats)
	if err != nil {
		return nil, err
	}
	var seats []appliedSeat
	for _, item := range items {
		entry, err := membershipObject(item, []string{keyTeam, keySeat, keyHandle, "state"}, where+": seats")
		if err != nil {
			return nil, err
		}
		team, seat, err := seatNames(entry, "seats", where)
		if err != nil {
			return nil, err
		}
		handle, isText := entry[keyHandle].(string)
		if _, isHandle := parseHandle(handle); !isText || !isHandle || !pyIn(entry["state"], membershipStates) {
			return nil, membershipErr("%s: seats: an entry's handle or state is refused", where)
		}
		seats = append(seats, appliedSeat{team, seat, handle, text(entry["state"])})
	}
	return seats, nil
}

func appliedRefused(object pyjson.Object, where string) error {
	items, err := membershipList(object, "refused", where, maxSeats)
	if err != nil {
		return err
	}
	for _, item := range items {
		entry, err := membershipObject(item, []string{keyTeam, keySeat, "why", "holder"}, where+": refused")
		if err != nil {
			return err
		}
		if _, _, err := seatNames(entry, "refused", where); err != nil {
			return err
		}
		holder, isText := entry["holder"].(string)
		if !pyIn(entry["why"], refusedWhy) || !isText || (holder != noHolder && !holderPattern.MatchString(holder)) {
			return membershipErr("%s: refused: an entry's why or holder is refused", where)
		}
	}
	return nil
}

func appliedTold(data any, where string) error {
	where += ": told"
	object, err := membershipObject(data, []string{keyGeneration, keyRoles}, where)
	if err != nil {
		return err
	}
	if !isIntAtLeast(object[keyGeneration], 0) {
		return membershipErr("%s: generation is not a non-negative integer", where)
	}
	items, err := membershipList(object, keyRoles, where, maxSeats)
	if err != nil {
		return err
	}
	for _, item := range items {
		entry, err := membershipObject(item, []string{keyTeam, keyHandle, keyRole}, where+": roles")
		if err != nil {
			return err
		}
		handle, isText := entry[keyHandle].(string)
		if _, isHandle := parseHandle(handle); !isTeamName(entry[keyTeam]) || !isText || !isHandle || !pyIn(entry[keyRole], membershipRoles) {
			return membershipErr("%s: a role entry is not a team, a handle and a role", where)
		}
	}
	return nil
}

// parseTold is membership.parse_told.
func parseTold(data any, where string) (toldRecord, error) {
	object, err := membershipObject(data, toldKeys, where)
	if err != nil {
		return toldRecord{}, err
	}
	if err = membershipVersion(object, where); err != nil {
		return toldRecord{}, err
	}
	if !isIntAtLeast(object[keyGeneration], 0) {
		return toldRecord{}, membershipErr("%s: generation is not a non-negative integer", where)
	}
	record := toldRecord{generation: number(object[keyGeneration])}
	if record.teams, err = toldTeams(object, where); err != nil {
		return toldRecord{}, err
	}
	record.left, err = toldLefts(object, where)
	return record, err
}

func toldTeams(object pyjson.Object, where string) ([]toldTeam, error) {
	items, err := membershipList(object, "teams", where, maxSeats)
	if err != nil {
		return nil, err
	}
	var teams []toldTeam
	for _, item := range items {
		entry, err := membershipObject(item, []string{keyTeam, keySeat, keyHandle, keyRole}, where+": teams")
		if err != nil {
			return nil, err
		}
		team, seat, err := seatNames(entry, "teams", where)
		if err != nil {
			return nil, err
		}
		handle, isText := entry[keyHandle].(string)
		if _, isHandle := parseHandle(handle); !isText || !isHandle || !pyIn(entry[keyRole], append(slices.Clone(membershipRoles), unknownRole)) {
			return nil, membershipErr("%s: teams: an entry's handle or role is refused", where)
		}
		teams = append(teams, toldTeam{team, seat, handle, text(entry[keyRole])})
	}
	return teams, nil
}

func toldLefts(object pyjson.Object, where string) ([]toldLeft, error) {
	items, err := membershipList(object, "left", where, maxFarewells)
	if err != nil {
		return nil, err
	}
	var left []toldLeft
	for _, item := range items {
		entry, err := membershipObject(item, []string{keyTeam, keyReason}, where+": left")
		if err != nil {
			return nil, err
		}
		if !isTeamName(entry[keyTeam]) {
			return nil, membershipErr("%s: left: an entry's team or reason is refused", where)
		}
		known, err := farewellReason(entry[keyReason])
		if err != nil {
			return nil, err
		}
		if !known {
			return nil, membershipErr("%s: left: an entry's team or reason is refused", where)
		}
		left = append(left, toldLeft{text(entry[keyTeam]), text(entry[keyReason])})
	}
	return left, nil
}

// parseToldRoles is membership.parse_told_roles.
func parseToldRoles(data any, where string) (map[string]string, error) {
	object, err := membershipObject(data, toldRolesKeys, where)
	if err != nil {
		return nil, err
	}
	if err := membershipVersion(object, where); err != nil {
		return nil, err
	}
	roles, ok := object[keyRoles].(pyjson.Object)
	if !ok || len(roles) > maxSeats {
		return nil, membershipErr("%s: roles is not an object of at most %d", where, maxSeats)
	}
	result := make(map[string]string, len(roles))
	for team, role := range roles {
		if !isTeamName(team) || !pyIn(role, membershipRoles) {
			return nil, membershipErr("%s: roles: an entry's team or role is refused", where)
		}
		result[team] = text(role)
	}
	return result, nil
}

// readDesired is membership.read_desired: its absence is refused.
func readDesired(dir string) (desired, error) {
	path := pyJoin(dir, desiredFile)
	data, found, err := readMembershipJSON(path)
	if err != nil {
		return desired{}, err
	}
	if !found {
		return desired{}, membershipErr("%s: %s: no such file: the host writes it at launch", membershipVar, path)
	}
	return parseDesired(data, path)
}

// readApplied is membership.read_applied: nil before the keeper writes it.
func readApplied(home string) (*applied, error) {
	path := pyJoin(home, appliedFile)
	data, found, err := readMembershipJSON(path)
	if err != nil || !found {
		return nil, err
	}
	result, err := parseApplied(data, path)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// readTold is membership.read_told: nil before anything was told.
func readTold(home string) (*toldRecord, error) {
	path := pyJoin(home, toldFile)
	data, found, err := readMembershipJSON(path)
	if err != nil || !found {
		return nil, err
	}
	record, err := parseTold(data, path)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// readToldRoles is membership.read_told_roles: nil before any role was told.
func readToldRoles(home string) (map[string]string, error) {
	path := pyJoin(home, toldRolesFile)
	data, found, err := readMembershipJSON(path)
	if err != nil || !found {
		return nil, err
	}
	return parseToldRoles(data, path)
}

func writeTold(home string, record toldRecord) error {
	return writePrivateJSON(pyJoin(home, toldFile), record.json())
}

func writeToldRoles(home string, roles map[string]string) error {
	object := make(pyobj, 0, len(roles))
	for team, role := range roles {
		object = append(object, pykv{team, role})
	}
	return writePrivateJSON(pyJoin(home, toldRolesFile), pyobj{{"v", 1}, {keyRoles, object}})
}

// activeOf is membership.active: the desired teams whose seat is held in applied, in desired
// order.
func activeOf(d desired, a *applied) []string {
	if a == nil {
		return nil
	}
	var teams []string
	for _, entry := range d.seats {
		if slices.ContainsFunc(a.seats, func(s appliedSeat) bool {
			return s.state == "held" && s.team == entry.team && s.seat == entry.seat
		}) {
			teams = append(teams, entry.team)
		}
	}
	return teams
}

// mountDir is membership.mount_dir: off and a relative path are refused.
func mountDir(marker string) (string, error) {
	if marker == membershipOff {
		return "", membershipErr("%s is off: this session was launched without the bus (--no-bus)", membershipVar)
	}
	if !strings.HasPrefix(marker, "/") {
		return "", membershipErr("%s must be an absolute path", membershipVar)
	}
	return pyJoin(marker, "."), nil
}
