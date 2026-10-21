package statusline

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
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

// errNotRegular is a path that opened but is not a regular file.
var errNotRegular = errors.New("is not a regular file")

// seat is one team and seat name from a membership file.
type seat struct{ team, name string }

// openNoFollow opens a path without following a final symlink and without blocking on a FIFO.
func openNoFollow(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

// readNoFollow reads a regular file of at most limit bytes, never followed through a symlink.
// It returns errNotRegular, a read error, or errTooLarge.
func readNoFollow(path string, limit int64) ([]byte, error) {
	file, err := openNoFollow(path)
	if err != nil {
		return nil, err
	}
	raw, readErr := readOpened(file, limit)
	closeErr := file.Close()
	if readErr != nil {
		return nil, readErr
	}
	return raw, closeErr
}

var errTooLarge = errors.New("is larger than the limit")

func readOpened(file *os.File, limit int64) ([]byte, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errNotRegular
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, errTooLarge
	}
	return raw, nil
}

// readObject is a JSON object from a regular file, never followed through a symlink, of at
// most maxMembershipBytes that is strict UTF-8. Nil otherwise (S17).
func readObject(path string) map[string]any {
	raw, err := readNoFollow(path, maxMembershipBytes)
	if err != nil || !utf8.Valid(raw) {
		return nil
	}
	data, err := decodeJSON(raw)
	if err != nil {
		return nil
	}
	object, ok := data.(map[string]any)
	if !ok {
		return nil
	}
	return object
}

// isVersionOne is Python's `v == 1`, which holds for 1, 1.0 and true.
func isVersionOne(v any) bool {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		return err == nil && f == 1
	case bool:
		return x
	}
	return false
}

// seatsOf reads the seats of a membership object (S17). With heldOnly, only seats whose state
// is "held" count. False when the object is not one the keeper wrote.
func seatsOf(data map[string]any, heldOnly bool) ([]seat, bool) {
	if data == nil || !isVersionOne(data["v"]) {
		return nil, false
	}
	list, ok := data["seats"].([]any)
	if !ok {
		return nil, false
	}
	var seats []seat
	for _, item := range list {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		team, teamOK := object["team"].(string)
		name, seatOK := object["seat"].(string)
		if !teamOK || !seatOK || !teamNamePattern.MatchString(team) {
			return nil, false
		}
		if !heldOnly || object["state"] == "held" {
			seats = append(seats, seat{team, name})
		}
	}
	return seats, true
}

// membershipTeams is the teams the session is on under PINGBUS_MEMBERSHIP: the seats held in
// the applied file that the desired file names, in desired order (S16, S17). It reports false
// when a file cannot be read; an empty list (true) means no applied file yet.
func membershipTeams(env map[string]string) ([]string, bool) {
	desired, ok := seatsOf(readObject(filepath.Join(env[membershipVar], desiredFile)), false)
	home := env["PINGBUS_HOME"]
	if !ok || !strings.HasPrefix(home, "/") {
		return nil, false
	}
	applied := filepath.Join(home, appliedFile)
	// Python's os.path.lexists: any failure to look is "no applied file yet".
	if _, err := os.Lstat(applied); err != nil {
		return []string{}, true
	}
	held, ok := seatsOf(readObject(applied), true)
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
func sessionRecordPath(env map[string]string, sessionID string) string {
	dir := env[sessionsDirVar]
	if dir == "" {
		dir = fmt.Sprintf("/tmp/pingbus-sessions-%d", os.Getuid())
	}
	return filepath.Join(dir, sessionID)
}

// sessionTeams is the teams recorded for this session, or false when there is no usable record:
// no id, no file, a file that is not this user's alone, or contents that are not distinct team
// names (S18).
func sessionTeams(env map[string]string, sessionID string) ([]string, bool) {
	if sessionID == "" {
		return nil, false
	}
	path := sessionRecordPath(env, sessionID)
	if !ownRecord(path) {
		return nil, false
	}
	raw, err := readNoFollow(path, maxRecord)
	if err != nil || !isASCII(raw) {
		return nil, false
	}
	return distinctTeams(string(raw))
}

// ownRecord is S18's test of the record file: a regular file, not followed, that only this user
// can read or write, and of at most maxRecord bytes.
func ownRecord(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > maxRecord {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}

func isASCII(raw []byte) bool {
	for _, c := range raw {
		if c >= utf8.RuneSelf {
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

// isDown is the watcher's H26 mark for team: a regular file under PINGBUS_HOME/down, not
// followed, whose mtime is less than 300 s old (S25).
func isDown(env map[string]string, team string) bool {
	home := env["PINGBUS_HOME"]
	if !strings.HasPrefix(home, "/") {
		return false
	}
	info, err := os.Lstat(filepath.Join(home, downDir, team))
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return time.Since(info.ModTime()) < downFreshFor
}
