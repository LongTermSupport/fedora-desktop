package statusline

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// The role reader (S20 to S24): this member's role in each team, from the bundle
// config.load_bundle validates and the team record inbox.load_cached_record reads. A role that
// cannot be read is absent from the map and gives a reason in the diagnostic. The reasons are
// worded here, without Python's exception names (S37: stderr is compared by shape).
const (
	memberFile       = "member.json"
	tokenFile        = "token"
	stateDir         = "state"
	teamFile         = "team.json"
	memberMaxBytes   = 65536
	tokenMaxBytes    = 4096
	teamRecordMax    = 65536
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

// resolveHome is config.resolve_home: PINGBUS_HOME, else XDG_CONFIG_HOME, else HOME (S20).
func resolveHome(env map[string]string) (string, error) {
	if explicit := env["PINGBUS_HOME"]; explicit != "" {
		if !strings.HasPrefix(explicit, "/") {
			return "", errors.New("PINGBUS_HOME must be an absolute path")
		}
		return pyJoin(explicit, "."), nil
	}
	if xdg := env["XDG_CONFIG_HOME"]; strings.HasPrefix(xdg, "/") {
		return pyJoin(xdg, "pingbus"), nil
	}
	home := env["HOME"]
	if !strings.HasPrefix(home, "/") {
		return "", errors.New("PINGBUS_HOME is unset and HOME is not an absolute path")
	}
	return pyJoin(home, ".config/pingbus"), nil
}

// readRoles is each team's role and the one diagnostic line naming every team that failed
// (S24), or "" when all were read. A failed home is named once with its reason (S20).
func (w *world) readRoles(env map[string]string, teams []string) (map[string]string, string) {
	roles := make(map[string]string, len(teams))
	home, err := resolveHome(env)
	if err != nil {
		return roles, "pingbus status-line: no team's role can be read: " + err.Error()
	}
	var failed []string
	for _, team := range teams {
		role, err := w.roleOf(home, team)
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
func (w *world) roleOf(home, team string) (string, error) {
	if !teamNamePattern.MatchString(team) {
		return "", errors.New("not a team name")
	}
	dir := pyJoin(home, team)
	m, err := w.loadBundle(dir, team)
	if err != nil {
		return "", err
	}
	return w.cachedRole(dir, m)
}

// loadBundle is config.load_bundle for the bundle directory of team (S21).
func (w *world) loadBundle(dir, team string) (member, error) {
	linked := w.note(dir).isLink()
	if linked {
		if err := w.checkLinkedBundle(dir); err != nil {
			return member{}, err
		}
	}
	data, err := w.readMember(pyJoin(dir, memberFile))
	if err != nil {
		return member{}, err
	}
	m, err := parseMember(data, team)
	if err != nil {
		return member{}, err
	}
	if err := w.checkToken(pyJoin(dir, tokenFile)); err != nil {
		return member{}, err
	}
	if m.memberType == hostType {
		if err := w.checkBesideElement(linked); err != nil {
			return member{}, err
		}
	}
	return m, nil
}

// parentOf is pathlib's parent of a joined path.
func parentOf(path string) string {
	i := strings.LastIndexByte(path, '/')
	switch {
	case i < 0:
		return "."
	case i == 0:
		return "/"
	}
	return path[:i]
}

// checkLinkedBundle is config._check_linked_bundle: the link, read one level, must name a real
// directory (not another link) of this user's with mode exactly 0700.
func (w *world) checkLinkedBundle(link string) error {
	text, err := os.Readlink(w.abs(link))
	if err != nil {
		return errors.New("the bundle link cannot be read")
	}
	target := pyJoin(parentOf(link), text)
	st := w.note(target)
	where := "links to " + target
	switch {
	case st.Errno == int(syscall.ENOENT):
		return errors.New(where + ": no such directory")
	case !st.exists():
		return fmt.Errorf("%s: cannot be read (%s)", where, syscall.Errno(st.Errno).Error())
	case st.isLink():
		return errors.New(where + ": a link onto a link is refused")
	case !st.isDir():
		return errors.New(where + ": not a directory")
	case int(st.UID) != w.uid:
		return fmt.Errorf("%s: owned by uid %d, not %d", where, st.UID, w.uid)
	case st.perm() != linkedBundleMode:
		return fmt.Errorf("%s: mode %04o, not %04o", where, st.perm(), linkedBundleMode)
	}
	return nil
}

// readMember is config._read_member_file: a regular file of at most memberMaxBytes (opened
// without O_NOFOLLOW, as the Python opens it), strict UTF-8 JSON with no key twice in any
// object, and an object.
func (w *world) readMember(path string) (pyjson.Object, error) {
	got, err := w.read(path, false, memberMaxBytes)
	switch {
	case isErrno(err, syscall.ENOENT):
		return nil, errors.New("no bundle (member.json is missing)")
	case errors.Is(err, errNotRegular):
		return nil, errors.New("member.json is not a regular file")
	case errors.Is(err, errTooLarge):
		return nil, fmt.Errorf("member.json is larger than %d bytes", memberMaxBytes)
	case err != nil:
		return nil, fmt.Errorf("member.json cannot be read (%w)", err)
	}
	data, err := pyjson.DecodeUTF8(got.raw, pyjson.Options{NoDuplicateKeys: true})
	if err != nil {
		return nil, fmt.Errorf("member.json is not valid JSON (%w)", err)
	}
	object, ok := data.(pyjson.Object)
	if !ok {
		return nil, errors.New("member.json is not a JSON object")
	}
	return object, nil
}

// checkToken is config.read_token: a regular file, never followed through a symlink, of this
// user's, mode 0600 or stricter (setuid, setgid and sticky count), at most tokenMaxBytes of
// printable ASCII with no newline (S21). The token itself is never kept.
func (w *world) checkToken(path string) error {
	got, err := w.read(path, true, tokenMaxBytes)
	switch {
	case isErrno(err, syscall.ENOENT):
		return errors.New("the token file: no such file")
	case errors.Is(err, errTooLarge):
		return fmt.Errorf("the token is longer than %d bytes", tokenMaxBytes)
	case err != nil:
		return fmt.Errorf("the token file: not a regular file (%w)", err)
	}
	mode := got.info.Mode & 0o7777
	switch {
	case int(got.info.Uid) != w.uid:
		return fmt.Errorf("the token file: owned by uid %d, not %d", got.info.Uid, w.uid)
	case mode&^tokenModeAllowed != 0:
		return fmt.Errorf("the token file: mode %04o is looser than 0600", mode)
	case !printablePattern.Match(got.raw):
		return errors.New("the token must be one line of printable ASCII, no spaces, no newline")
	}
	return nil
}

// checkBesideElement is load_bundle's rule for a host member: reached through a link with the
// user's Element acceptance, or with no Element profile under the user's passwd home.
func (w *world) checkBesideElement(linked bool) error {
	home, err := w.userHome(w.uid)
	if err != nil {
		return fmt.Errorf("a host member needs this user's home to look for an Element profile: %w", err)
	}
	if linked && w.elementAccepted(home) {
		return nil
	}
	return w.refuseBesideElement(home)
}

// elementAccepted is config.element_accepted: a regular file of this user's that no one else
// can read or write, in a real directory of theirs under their home.
func (w *world) elementAccepted(home string) bool {
	dir := pyJoin(home, elementAcceptedDir)
	dirInfo := w.note(dir)
	info := w.note(pyJoin(dir, elementAcceptedFile))
	return dirInfo.isDir() && int(dirInfo.UID) == w.uid && info.isRegular() && int(info.UID) == w.uid && info.perm()&0o077 == 0
}

// refuseBesideElement is config._refuse_beside_element: the Flatpak's tree (a dangling link
// counts), or an Element or Element-<profile> entry in the native client's parent directory.
func (w *world) refuseBesideElement(home string) error {
	refusal := errors.New("a host member must not run as a user with an Element profile; run it as a dedicated agent user")
	if w.note(pyJoin(home, elementFlatpakDir)).exists() {
		return refusal
	}
	parent := pyJoin(home, elementNativeParent)
	w.note(parent)
	if !statOf(w.abs(parent), true).isDir() {
		return nil
	}
	entries, err := os.ReadDir(w.abs(parent))
	if err != nil {
		return fmt.Errorf("%s cannot be listed to look for an Element profile (%w)", parent, err)
	}
	for _, entry := range entries {
		if entry.Name() == elementNativeName || strings.HasPrefix(entry.Name(), elementNativeName+"-") {
			return refusal
		}
	}
	return nil
}

// cachedRole is inbox.cached_role: the member's role in the team record last verified (S22).
func (w *world) cachedRole(dir string, m member) (string, error) {
	got, err := w.read(pyJoin(pyJoin(dir, stateDir), teamFile), true, teamRecordMax)
	switch {
	case isErrno(err, syscall.ENOENT):
		return "", errors.New("no verified team record yet (not joined)")
	case isErrno(err, syscall.ELOOP):
		return "", errors.New("the team record is a symlink")
	case errors.Is(err, errNotRegular):
		return "", errors.New("the team record is not a regular file")
	case errors.Is(err, errTooLarge):
		return "", errors.New("the team record is too large")
	case err != nil:
		return "", fmt.Errorf("the team record cannot be read (%w)", err)
	}
	content, err := pyjson.DecodeBytes(got.raw)
	if err != nil {
		return "", errors.New("the team record is not JSON")
	}
	roles, err := parseTeamRecord(content, m.serverName, m.team)
	if err != nil {
		return "", err
	}
	role, ok := roles[m.userID]
	if !ok {
		return "", errors.New("the team record gives this member no role")
	}
	return role, nil
}
