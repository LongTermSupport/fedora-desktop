package statusline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"
)

// The role reader (S20 to S24): this member's role in each team, from the bundle config.py
// validates and the team record inbox.py caches. A role that cannot be read is absent from the
// map and gives a reason in the diagnostic. Port status: the checks marked GAP in the spec are
// not yet ported (see the plan's draft report); a file Python refuses for one of them is read here.
const (
	memberFile       = "member.json"
	tokenFile        = "token"
	stateDir         = "state"
	teamFile         = "team.json"
	memberMaxBytes   = 65536
	tokenMaxBytes    = 4096
	teamRecordMax    = 65536
	roleCoordinator  = "coordinator"
	roleWorker       = "worker"
	roleLegacyLeader = "orchestrator"
)

var (
	printablePattern = regexp.MustCompile(`^[\x21-\x7e]+$`)
	memberKeys       = []string{"protocol", "team", "user_id", "server_name", "base_url", "plain_http_hosts", "token_file", "admin", "room"}
	memberOptional   = []string{"human_text", "limits"}
)

// member is what a bundle's member.json and token make of one team.
type member struct {
	team, userID, stateDir string
}

// resolveHome is config.resolve_home: PINGBUS_HOME, else XDG_CONFIG_HOME, else HOME (S20).
func resolveHome(env map[string]string) (string, error) {
	if explicit := env["PINGBUS_HOME"]; explicit != "" {
		if !strings.HasPrefix(explicit, "/") {
			return "", errors.New("PINGBUS_HOME must be an absolute path")
		}
		return explicit, nil
	}
	if xdg := env["XDG_CONFIG_HOME"]; strings.HasPrefix(xdg, "/") {
		return filepath.Join(xdg, "pingbus"), nil
	}
	home := env["HOME"]
	if !strings.HasPrefix(home, "/") {
		return "", errors.New("PINGBUS_HOME is unset and HOME is not an absolute path")
	}
	return filepath.Join(home, ".config", "pingbus"), nil
}

// readRoles is each team's role and the one diagnostic line naming every team that failed
// (S24), or "" when all were read. A failed home is named once with its reason (S20).
func readRoles(env map[string]string, teams []string) (map[string]string, string) {
	roles := make(map[string]string, len(teams))
	home, err := resolveHome(env)
	if err != nil {
		return roles, "pingbus status-line: no team's role can be read: " + err.Error()
	}
	var failed []string
	for _, team := range teams {
		role, err := roleOf(home, team)
		if err != nil {
			failed = append(failed, team+": "+err.Error())
			continue
		}
		roles[team] = role
	}
	if len(failed) == 0 {
		return roles, ""
	}
	return roles, "pingbus status-line: the role cannot be read (" + strings.Join(failed, "; ") + ")"
}

// roleOf is one team's role for this member: its bundle loaded, then its cached record read.
func roleOf(home, team string) (string, error) {
	m, err := loadBundle(home, team)
	if err != nil {
		return "", err
	}
	return cachedRole(m)
}

// loadBundle is config.load_bundle for home/<team> (S21), with the checks listed in the spec's
// section 4 that are ported; the rest are GAP.
func loadBundle(home, team string) (member, error) {
	if !teamNamePattern.MatchString(team) {
		return member{}, errors.New("not a team name")
	}
	dir := filepath.Join(home, team)
	if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if err := checkLinkedBundle(dir); err != nil {
			return member{}, err
		}
	}
	raw, err := readMember(filepath.Join(dir, memberFile))
	if err != nil {
		return member{}, err
	}
	object, err := memberObject(raw)
	if err != nil {
		return member{}, err
	}
	m, err := parseMember(object, team)
	if err != nil {
		return member{}, err
	}
	m.stateDir = filepath.Join(dir, stateDir)
	if err := checkToken(filepath.Join(dir, tokenFile)); err != nil {
		return member{}, err
	}
	return m, nil
}

// readMember reads member.json: a regular file of at most memberMaxBytes.
func readMember(path string) ([]byte, error) {
	raw, err := readRegular(path, memberMaxBytes)
	if err != nil {
		return nil, fmt.Errorf("member.json %w", err)
	}
	return raw, nil
}

// memberObject decodes member.json: strict UTF-8, no duplicate key anywhere, a JSON object.
func memberObject(raw []byte) (map[string]any, error) {
	if err := refuseDuplicateKeys(raw); err != nil {
		return nil, fmt.Errorf("member.json is not valid JSON (ValueError): %w", err)
	}
	data, err := decodeJSON(raw)
	if err != nil {
		return nil, errors.New("member.json is not valid JSON (JSONDecodeError)")
	}
	object, ok := data.(map[string]any)
	if !ok {
		return nil, errors.New("member.json is not a JSON object")
	}
	return object, nil
}

// checkLinkedBundle is S21's rule for a bundle reached through a link: a real directory, this
// user's, mode exactly 0700.
func checkLinkedBundle(link string) error {
	target, err := filepath.EvalSymlinks(link)
	if err != nil {
		return fmt.Errorf("the linked bundle cannot be followed (%s)", pythonExceptionName(err))
	}
	info, err := os.Lstat(target)
	if err != nil || !info.IsDir() {
		return errors.New("the linked bundle is not a directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() || info.Mode().Perm() != 0o700 {
		return errors.New("the linked bundle is not owned by this user with mode 0700")
	}
	return nil
}

// readRegular reads a regular file of at most limit bytes, never followed through a symlink,
// and words each failure the way the Python does.
func readRegular(path string, limit int) ([]byte, error) {
	raw, err := readNoFollow(path, int64(limit))
	switch {
	case err == nil:
		return raw, nil
	case errors.Is(err, errNotRegular):
		return nil, err
	case errors.Is(err, errTooLarge):
		return nil, fmt.Errorf("is larger than %d bytes", limit)
	}
	return nil, fmt.Errorf("cannot be read (%s)", pythonExceptionName(err))
}

// refuseDuplicateKeys fails on a key that appears twice in any object (S21, member.json only).
func refuseDuplicateKeys(raw []byte) error {
	if !utf8.Valid(raw) {
		return errors.New("invalid UTF-8")
	}
	return walkValue(json.NewDecoder(bytes.NewReader(raw)))
}

// walkValue reads one JSON value from dec, failing on a duplicate key in any object of it.
func walkValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	if delim == '{' {
		err = walkObject(dec)
	} else {
		err = walkArray(dec)
	}
	if err != nil {
		return err
	}
	_, err = dec.Token() // the closing delimiter
	return err
}

func walkObject(dec *json.Decoder) error {
	seen := map[string]bool{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return err
		}
		name, isName := key.(string)
		if !isName {
			return errors.New("object key is not a string")
		}
		if seen[name] {
			return fmt.Errorf("duplicate key %q", name)
		}
		seen[name] = true
		if err := walkValue(dec); err != nil {
			return err
		}
	}
	return nil
}

func walkArray(dec *json.Decoder) error {
	for dec.More() {
		if err := walkValue(dec); err != nil {
			return err
		}
	}
	return nil
}

// parseMember applies _parse_member's key checks and the ones the port has (S21). GAP: base_url,
// plain_http_hosts, user_id's handle, admin, room and limits are not yet validated.
func parseMember(object map[string]any, team string) (member, error) {
	for _, key := range memberKeys {
		if _, ok := object[key]; !ok {
			return member{}, fmt.Errorf("member.json lacks %q", key)
		}
	}
	for key := range object {
		if !slices.Contains(memberKeys, key) && !slices.Contains(memberOptional, key) {
			return member{}, fmt.Errorf("member.json has an unknown key %q", key)
		}
	}
	if !isInt(object["protocol"], 1) {
		return member{}, errors.New("member.json protocol is not 1")
	}
	if object["team"] != team {
		return member{}, errors.New("member.json names another team")
	}
	userID, ok := object["user_id"].(string)
	if !ok {
		return member{}, errors.New("member.json user_id is not a string")
	}
	if object["token_file"] != tokenFile {
		return member{}, errors.New("member.json token_file is not \"token\"")
	}
	return member{team: team, userID: userID}, nil
}

// isInt is Python's `type(v) is int and v == want`: a JSON integer literal, not 1.0 or true.
func isInt(v any, want int) bool {
	n, ok := v.(json.Number)
	return ok && n.String() == strconv.Itoa(want)
}

// checkToken is the token file: regular, this user's, mode 0600 or stricter, at most 4096
// bytes, printable ASCII with no trailing newline (S21).
func checkToken(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("token is not a regular file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return errors.New("token is not owned by this user")
	}
	if info.Mode().Perm()&0o177 != 0 {
		return errors.New("token is open to other users")
	}
	raw, err := readNoFollow(path, tokenMaxBytes)
	if err != nil || !printablePattern.Match(raw) {
		return errors.New("token is not a printable token")
	}
	return nil
}

// cachedRole is the member's role in the team record last verified (inbox.load_cached_record).
// GAP: parse_team_record's field checks are not yet ported; only the role lookup is.
func cachedRole(m member) (string, error) {
	raw, err := readRegular(filepath.Join(m.stateDir, teamFile), teamRecordMax)
	if err != nil {
		return "", fmt.Errorf("team record: %w", err)
	}
	data, err := decodeJSON(raw)
	if err != nil {
		return "", errors.New("team record is not JSON")
	}
	object, ok := data.(map[string]any)
	if !ok {
		return "", errors.New("team record is not a JSON object")
	}
	roles, ok := object["roles"].(map[string]any)
	if !ok {
		return "", errors.New("team record has no roles")
	}
	role, isRole := roles[m.userID].(string)
	if !isRole {
		return "", errors.New("the team record gives this member no role")
	}
	if role == roleLegacyLeader {
		role = roleCoordinator
	}
	if role != roleCoordinator && role != roleWorker {
		return "", errors.New("the team record gives this member no role")
	}
	return role, nil
}
