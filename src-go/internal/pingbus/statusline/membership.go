package statusline

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

const (
	membershipVar      = "PINGBUS_MEMBERSHIP"
	membershipOff      = "off"
	noMembership       = "this session's membership is unreadable (pingbus status)"
	desiredFile        = "membership.json"
	appliedFile        = "applied.json"
	maxMembershipBytes = 65536
	downDir            = "down"
	downFreshFor       = 300 * time.Second
)

// teamNamePattern is a team name (S17, S18).
var teamNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,23}$`)

// seat is one team and seat name from a membership file.
type seat struct{ team, name string }

// readObject is statusline._read_object: a JSON object from a regular file never followed
// through a symlink, of at most maxMembershipBytes of strict UTF-8 (S17); nil otherwise.
func (w *world) readObject(path string) pyjson.Object {
	got, err := w.read(path, true, maxMembershipBytes)
	if err != nil {
		return nil
	}
	data, err := pyjson.DecodeUTF8(got.raw, pyjson.Options{})
	if err != nil {
		return nil
	}
	object, ok := data.(pyjson.Object)
	if !ok {
		return nil
	}
	return object
}

// seatsOf is statusline._pairs: the seats of a membership object (S17), with heldOnly only
// those whose state is "held". False when the object is not one the keeper wrote.
func seatsOf(data pyjson.Object, heldOnly bool) ([]seat, bool) {
	if data == nil || !pyjson.EqualsInt(data["v"], 1) {
		return nil, false
	}
	list, ok := data["seats"].([]any)
	if !ok {
		return nil, false
	}
	var seats []seat
	for _, item := range list {
		object, isObject := item.(pyjson.Object)
		if !isObject {
			return nil, false
		}
		team, teamOK := object["team"].(string)
		name, seatOK := object["seat"].(string)
		if !teamOK || !seatOK || !teamNamePattern.MatchString(team) {
			return nil, false
		}
		if state, isText := object["state"].(string); !heldOnly || (isText && state == "held") {
			seats = append(seats, seat{team, name})
		}
	}
	return seats, true
}

// membershipTeams is the teams the session is on under PINGBUS_MEMBERSHIP: the seats held in
// the applied file that the desired file names, in desired order (S16, S17). It reports false
// when a file cannot be read; an empty list (true) means no applied file yet.
func (w *world) membershipTeams(env map[string]string) ([]string, bool) {
	desired, ok := seatsOf(w.readObject(pyJoin(env[membershipVar], desiredFile)), false)
	home := env["PINGBUS_HOME"]
	if !ok || !strings.HasPrefix(home, "/") {
		return nil, false
	}
	applied := pyJoin(home, appliedFile)
	// Python's os.path.lexists: any failure to look is "no applied file yet".
	if !w.note(applied).exists() {
		return []string{}, true
	}
	held, ok := seatsOf(w.readObject(applied), true)
	if !ok {
		return nil, false
	}
	teams := []string{}
	for _, want := range desired {
		if slices.Contains(held, want) {
			teams = append(teams, want.team)
		}
	}
	return teams, true
}

// sessionRecordPath is the record of a session: PINGBUS_SESSIONS_DIR, else the per-uid default.
func (w *world) sessionRecordPath(env map[string]string, sessionID string) string {
	dir := env[sessionsDirVar]
	if dir == "" {
		dir = fmt.Sprintf("/tmp/pingbus-sessions-%d", w.uid)
	}
	return pyJoin(dir, sessionID)
}

// sessionTeams is the teams recorded for this session, or false when there is no usable record:
// no id, no file, a file that is not this user's alone or is over maxRecord bytes, or contents
// that are not distinct ASCII team names (S18).
func (w *world) sessionTeams(env map[string]string, sessionID string) ([]string, bool) {
	if sessionID == "" {
		return nil, false
	}
	path := w.sessionRecordPath(env, sessionID)
	st := w.note(path)
	if !st.isRegular() || int(st.UID) != w.uid || st.Mode&0o077 != 0 || st.Size > maxRecord {
		return nil, false
	}
	got, err := w.read(path, true, maxRecord)
	if err != nil || !isASCII(got.raw) {
		return nil, false
	}
	return distinctTeams(string(got.raw))
}

func isASCII(raw []byte) bool {
	for _, c := range raw {
		if c >= 0x80 {
			return false
		}
	}
	return true
}

// distinctTeams splits a record's content into team names, which must be distinct and match.
func distinctTeams(content string) ([]string, bool) {
	teams := strings.Split(content, ",")
	seen := map[string]bool{}
	for _, team := range teams {
		if seen[team] || !teamNamePattern.MatchString(team) {
			return nil, false
		}
		seen[team] = true
	}
	return teams, true
}

// downUntil is the watcher's H26 mark for team: when PINGBUS_HOME/down/<team> is a regular file
// (lstat, never followed) whose mtime is less than 300 s old, the moment it stops counting
// (S25, S34); the zero time when the team is not down.
func (w *world) downUntil(env map[string]string, team string) time.Time {
	home := env["PINGBUS_HOME"]
	if !strings.HasPrefix(home, "/") {
		return time.Time{}
	}
	st := w.note(pyJoin(pyJoin(home, downDir), team))
	if !st.isRegular() {
		return time.Time{}
	}
	until := time.Unix(0, st.Mtime).Add(downFreshFor)
	if !w.now.Before(until) {
		return time.Time{}
	}
	return until
}
