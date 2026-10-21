package hooks

// Future home: bus/state (config.py): the home, the active teams and the bundles (H16 to
// H18). Adapted from internal/pingbus/statusline/roles.go, which reads the same files for the
// status line; here every refusal is a configError, as config.ConfigError is.

import (
	"errors"
	"os"
	"slices"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

const (
	memberFile       = "member.json"
	tokenFile        = "token"
	stateDir         = "state"
	memberMaxBytes   = 65536
	tokenMaxBytes    = 4096
	tokenModeAllowed = 0o600
	linkedBundleMode = 0o700
	roleCoordinator  = "coordinator"
	roleWorker       = "worker"
	roleLegacyLeader = "orchestrator"
	hostType         = "host"

	// Where Element keeps a human's Matrix session, and the user's own record that they accept
	// a host seat beside it (config.py).
	elementFlatpakDir   = ".var/app/im.riot.Riot"
	elementNativeParent = ".config"
	elementNativeName   = "Element"
	elementAcceptedDir  = ".config/pingbus"
	elementAcceptedFile = "element-accepted"
)

// world is what the readers take from the machine: this user's uid and the passwd home of a
// uid (a fake in tests; HB6).
type world struct {
	uid      int
	userHome func(uid int) (string, error)
}

// resolveHome is config.resolve_home (H16).
func resolveHome(env map[string]string) (string, error) {
	if explicit := env["PINGBUS_HOME"]; explicit != "" {
		if !strings.HasPrefix(explicit, "/") {
			return "", configErr("PINGBUS_HOME must be an absolute path")
		}
		return pyJoin(explicit, "."), nil
	}
	if xdg := env["XDG_CONFIG_HOME"]; strings.HasPrefix(xdg, "/") {
		return pyJoin(xdg, "pingbus"), nil
	}
	home := env["HOME"]
	if !strings.HasPrefix(home, "/") {
		return "", configErr("PINGBUS_HOME is unset and HOME is not an absolute path")
	}
	return pyJoin(home, ".config/pingbus"), nil
}

// activeTeams is config.active_teams (H17) for the resolved home.
func activeTeams(env map[string]string, home string) ([]string, error) {
	if marker, set := env[membershipVar]; set {
		return membershipTeams(marker, home)
	}
	raw := env["PINGBUS_TEAMS"]
	if raw == "" {
		return nil, configErr("PINGBUS_TEAMS is not set: no team is active")
	}
	teams := strings.Split(raw, ",")
	for i, name := range teams {
		if !teamNamePattern.MatchString(name) {
			return nil, configErr("PINGBUS_TEAMS entry %d is not a team name", i+1)
		}
		if slices.Contains(teams[:i], name) {
			return nil, configErr("PINGBUS_TEAMS lists a team more than once")
		}
	}
	return teams, nil
}

func membershipTeams(marker, home string) ([]string, error) {
	teams, err := readMembershipTeams(marker, home)
	if membership, ok := errors.AsType[*membershipError](err); ok {
		return nil, configErr("%s", membership.msg)
	}
	if err != nil {
		return nil, err
	}
	if len(teams) == 0 {
		return nil, configErr("no team is active: this session is on no team, or its seat keeper has not taken its seats yet")
	}
	return teams, nil
}

func readMembershipTeams(marker, home string) ([]string, error) {
	dir, err := mountDir(marker)
	if err != nil {
		return nil, err
	}
	d, err := readDesired(dir)
	if err != nil {
		return nil, err
	}
	a, err := readApplied(home)
	if err != nil {
		return nil, err
	}
	return activeOf(d, a), nil
}

// loadActive is config.load_active for the hooks: every active bundle, validated, then
// check_distinct.
func (w *world) loadActive(env map[string]string) ([]member, error) {
	home, err := resolveHome(env)
	if err != nil {
		return nil, err
	}
	teams, err := activeTeams(env, home)
	if err != nil {
		return nil, err
	}
	members := make([]member, 0, len(teams))
	for _, team := range teams {
		m, err := w.loadBundle(home, team)
		if err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, w.checkDistinct(members)
}

// checkDistinct refuses two bundles that share an account or a token.
func (w *world) checkDistinct(members []member) error {
	users, tokens := map[string]string{}, map[string]string{}
	for _, m := range members {
		if first, seen := users[m.userID]; seen && first != m.team {
			return configErr("teams %s and %s: the bundles share one user_id", first, m.team)
		}
		users[m.userID] = m.team
		token, err := w.readToken(m)
		if err != nil {
			return err
		}
		if first, seen := tokens[token]; seen && first != m.team {
			return configErr("teams %s and %s: the bundles share one token", first, m.team)
		}
		tokens[token] = m.team
	}
	return nil
}

// loadBundle is config.load_bundle(home, team) (H18).
func (w *world) loadBundle(home, team string) (member, error) {
	if !teamNamePattern.MatchString(team) {
		return member{}, configErr("not a team name")
	}
	dir := pyJoin(home, team)
	var info syscall.Stat_t
	linked := syscall.Lstat(dir, &info) == nil && info.Mode&syscall.S_IFMT == syscall.S_IFLNK
	if linked {
		if err := w.checkLinkedBundle(team, dir); err != nil {
			return member{}, err
		}
	}
	m, err := readMember(team, dir)
	if err != nil {
		return member{}, err
	}
	if _, err := w.readToken(m); err != nil {
		return member{}, err
	}
	if m.memberType == hostType {
		if err := w.checkBesideElement(team, linked); err != nil {
			return member{}, err
		}
	}
	return m, nil
}

// readMember is config.read_member: member.json validated for team, no token read.
func readMember(team, dir string) (member, error) {
	path := pyJoin(dir, memberFile)
	data, err := readMemberFile(team, path)
	if err != nil {
		return member{}, err
	}
	m, err := parseMember(data, team)
	if err != nil {
		return member{}, configErr("team %s: %s: %s", team, path, err.Error())
	}
	m.bundleDir = dir
	return m, nil
}

// readMemberFile is config._read_member_file: followed through a link, never blocking, a
// regular file of at most memberMaxBytes, strict UTF-8 JSON with no key twice, an object.
func readMemberFile(team, path string) (pyjson.Object, error) {
	where := "team " + team + ": " + path
	got, err := readFollowing(path, memberMaxBytes)
	switch {
	case isErrno(err, syscall.ENOENT):
		return nil, configErr("%s: no bundle (member.json is missing)", where)
	case errors.Is(err, errNotRegular):
		return nil, configErr("%s: member.json is not a regular file", where)
	case errors.Is(err, errTooLarge):
		return nil, configErr("%s: member.json is larger than %d bytes", where, memberMaxBytes)
	case err != nil:
		return nil, configErr("%s: member.json cannot be read (%s)", where, strerror(err))
	}
	data, err := pyjson.DecodeUTF8(got.raw, pyjson.Options{NoDuplicateKeys: true})
	if err != nil {
		return nil, configErr("%s: member.json is not valid JSON", where)
	}
	object, ok := data.(pyjson.Object)
	if !ok {
		return nil, configErr("%s: member.json is not a JSON object", where)
	}
	return object, nil
}

// readToken is config.read_token under open_private_file: no symlink, regular, this user's,
// mode 0600 or stricter, one line of printable ASCII.
func (w *world) readToken(m member) (string, error) {
	path := m.tokenPath()
	where := "team " + m.team + ": the token file " + path
	got, err := readRegular(path, tokenMaxBytes)
	switch {
	case isErrno(err, syscall.ENOENT):
		return "", configErr("%s: no such file", where)
	case errors.Is(err, errNotRegular):
		return "", configErr("%s: not a regular file", where)
	case errors.Is(err, errTooLarge):
		return "", configErr("%s: the token is longer than %d bytes", where, tokenMaxBytes)
	case err != nil:
		return "", configErr("%s: not a regular file (%s)", where, strerror(err))
	}
	mode := got.info.Mode & 0o7777
	switch {
	case int(got.info.Uid) != w.uid:
		return "", configErr("%s: owned by uid %d, not %d", where, got.info.Uid, w.uid)
	case mode&^tokenModeAllowed != 0:
		return "", configErr("%s: mode %04o is looser than 0600", where, mode)
	case !printablePattern.Match(got.raw):
		return "", configErr("%s: the token must be one line of printable ASCII, no spaces, no newline", where)
	}
	return string(got.raw), nil
}

// checkLinkedBundle is config._check_linked_bundle: the link, read one level, must name a real
// directory (not another link) of this user's with mode exactly 0700.
func (w *world) checkLinkedBundle(team, link string) error {
	text, err := os.Readlink(link)
	if err != nil {
		return err
	}
	target := pyJoin(parentOf(link), text)
	where := "team " + team + ": " + link + " links to " + target
	var info syscall.Stat_t
	err = syscall.Lstat(target, &info)
	mode := info.Mode & 0o7777
	switch {
	case isErrno(err, syscall.ENOENT):
		return configErr("%s: no such directory", where)
	case err != nil:
		return configErr("%s: cannot be read (%s)", where, strerror(err))
	case info.Mode&syscall.S_IFMT == syscall.S_IFLNK:
		return configErr("%s: a link onto a link is refused", where)
	case info.Mode&syscall.S_IFMT != syscall.S_IFDIR:
		return configErr("%s: not a directory", where)
	case int(info.Uid) != w.uid:
		return configErr("%s: owned by uid %d, not %d", where, info.Uid, w.uid)
	case mode != linkedBundleMode:
		return configErr("%s: mode %04o, not %04o", where, mode, linkedBundleMode)
	}
	return nil
}

// checkBesideElement is load_bundle's rule for a host member: reached through a link with the
// user's Element acceptance, or with no Element profile under the user's passwd home. A uid
// with no passwd entry is refused (HB6).
func (w *world) checkBesideElement(team string, linked bool) error {
	home, err := w.userHome(w.uid)
	if err != nil {
		return configErr("team %s: a host member needs this user's home to look for an Element profile: %s", team, err.Error())
	}
	if linked && w.elementAccepted(home) {
		return nil
	}
	return refuseBesideElement(team, home)
}

// elementAccepted is config.element_accepted.
func (w *world) elementAccepted(home string) bool {
	dir := pyJoin(home, elementAcceptedDir)
	var dirInfo, info syscall.Stat_t
	if syscall.Lstat(dir, &dirInfo) != nil || syscall.Lstat(pyJoin(dir, elementAcceptedFile), &info) != nil {
		return false
	}
	return dirInfo.Mode&syscall.S_IFMT == syscall.S_IFDIR && int(dirInfo.Uid) == w.uid &&
		info.Mode&syscall.S_IFMT == syscall.S_IFREG && int(info.Uid) == w.uid && info.Mode&0o077 == 0
}

// refuseBesideElement is config._refuse_beside_element: the Flatpak's tree (a dangling link
// counts), or an Element or Element-<profile> entry in the native client's parent directory.
func refuseBesideElement(team, home string) error {
	refusal := configErr("team %s: a host member must not run as a user with an Element profile; run it as a dedicated agent user", team)
	var info syscall.Stat_t
	if syscall.Lstat(pyJoin(home, elementFlatpakDir), &info) == nil {
		return refusal
	}
	parent := pyJoin(home, elementNativeParent)
	if !isDirectory(parent) {
		return nil
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return configErr("team %s: %s cannot be listed to look for an Element profile (%s)", team, parent, strerror(err))
	}
	for _, entry := range entries {
		if entry.Name() == elementNativeName || strings.HasPrefix(entry.Name(), elementNativeName+"-") {
			return refusal
		}
	}
	return nil
}

// isDirectory is pathlib's is_dir: true only when the path, followed, is a directory; any
// failure to stat it is false.
func isDirectory(path string) bool {
	var info syscall.Stat_t
	return syscall.Stat(path, &info) == nil && info.Mode&syscall.S_IFMT == syscall.S_IFDIR
}
