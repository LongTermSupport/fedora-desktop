package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"syscall"
)

// v1's names: its state root, its service user and its default server name's suffix.
const (
	v1StateRoot  = "/var/lib/agent-bus"
	v1User       = "agent-bus"
	serverSuffix = ".agent-bus.internal"
)

// The room purposes in the order they are read and written, and each one's room id file.
var (
	purposes  = []string{"main", "reports", "questions"}
	roomFiles = map[string]string{"main": "room_id", "reports": "room_id.reports", "questions": "room_id.questions"}
)

var (
	teamGrammar   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,23}$`)
	roomIDGrammar = regexp.MustCompile(`^![A-Za-z0-9_-]{43}$`)
	serverGrammar = regexp.MustCompile(`^[a-z0-9-]+(\.[a-z0-9-]+)+$`)
)

// Caps on what is read of a v1 file.
const (
	maxTeamFile  = 64 << 10
	maxRoomFile  = 256
	maxTokenFile = 4096
)

// team is what an export reads of a v1 team (E3-E6).
type team struct {
	name       string
	port       int
	serverName string
	// rooms holds each purpose whose room exists (E5).
	rooms map[string]string
	token string
}

// CheckTeam refuses a name that is not a v1 team name: the grammar v1's team file accepts, also
// the name of the team's directory (E1).
func CheckTeam(name string) error {
	if !teamGrammar.MatchString(name) {
		return fmt.Errorf("%q is not a v1 team name (%s)", name, teamGrammar)
	}
	return nil
}

// fileRule is what a v1 file must be: owned by uid, of group gid (any when gid is -1), with mode
// exactly mode, or at most mode when atMost.
type fileRule struct {
	uid, gid int
	mode     os.FileMode
	atMost   bool
	max      int
}

// readTeam reads the team's files as E3-E6 require.
func readTeam(d Deps, name string) (team, error) {
	acct, found, err := d.Lookup(v1User)
	if err != nil {
		return team{}, fmt.Errorf("cannot look up the v1 service user %s: %w", v1User, err)
	}
	if !found {
		return team{}, fmt.Errorf("no v1 service user %s on this host", v1User)
	}
	dir := d.Root + v1StateRoot + "/" + name
	if err := checkDir(dir, acct); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return team{}, fmt.Errorf("no v1 team %s on this host (never made, or purged)", name)
		}
		return team{}, err
	}
	t := team{name: name, rooms: map[string]string{}}
	raw, err := readChecked(dir+"/team.json", fileRule{uid: d.RootUID, gid: acct.GID, mode: 0o640, max: maxTeamFile})
	if err != nil {
		return team{}, err
	}
	if t.port, t.serverName, err = decodeTeamFile(raw, name); err != nil {
		return team{}, fmt.Errorf("%s/team.json %w", dir, err)
	}
	private := fileRule{uid: acct.UID, gid: -1, mode: 0o600, atMost: true}
	for _, purpose := range purposes {
		path := dir + "/" + roomFiles[purpose]
		private.max = maxRoomFile
		id, err := readChecked(path, private)
		if errors.Is(err, fs.ErrNotExist) && purpose != "main" {
			continue
		}
		if err != nil {
			return team{}, err
		}
		if !roomIDGrammar.Match(id) {
			return team{}, fmt.Errorf("%s does not hold one v12 room id and nothing else", path)
		}
		t.rooms[purpose] = string(id)
	}
	info, err := os.Lstat(dir + "/secrets")
	if err != nil {
		return team{}, err
	}
	if !info.IsDir() {
		return team{}, fmt.Errorf("%s/secrets is not a directory (a symlink or another file)", dir)
	}
	private.max = maxTokenFile
	tok, err := readChecked(dir+"/secrets/admin.token", private)
	if err != nil {
		return team{}, err
	}
	if !printable(tok) {
		return team{}, fmt.Errorf("%s/secrets/admin.token does not hold one token of printable ASCII", dir)
	}
	t.token = string(tok)
	return t, nil
}

// checkDir is E3's check of the team's directory: a directory, not a symlink, the v1 user's,
// mode 0700. A missing one is fs.ErrNotExist.
func checkDir(dir string, acct Account) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory (a symlink or another file)", dir)
	}
	uid, _, err := ownerOf(info)
	if err != nil {
		return fmt.Errorf("%s: %w", dir, err)
	}
	if uid != acct.UID {
		return fmt.Errorf("%s is owned by uid %d, not the v1 user %s (uid %d)", dir, uid, v1User, acct.UID)
	}
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("%s has mode %04o, not 0700", dir, info.Mode().Perm())
	}
	return nil
}

// decodeTeamFile is E4: the port, 1024-65535 and required, and the server name, defaulted.
func decodeTeamFile(raw []byte, name string) (int, string, error) {
	var tf struct {
		Port       *int    `json:"port"`
		ServerName *string `json:"server_name"`
	}
	if err := json.Unmarshal(raw, &tf); err != nil {
		return 0, "", fmt.Errorf("does not decode: %w", err)
	}
	if tf.Port == nil || *tf.Port < 1024 || *tf.Port > 65535 {
		return 0, "", errors.New("has no port from 1024 to 65535")
	}
	server := name + serverSuffix
	if tf.ServerName != nil {
		server = *tf.ServerName
		if len(server) > 253 || !serverGrammar.MatchString(server) {
			return 0, "", fmt.Errorf("has a server_name %q that is not a lower-case DNS name", server)
		}
	}
	return *tf.Port, server, nil
}

// readChecked opens path without following a symlink and without waiting on a pipe, and reads it
// only if it is a regular file that keeps rule. A missing file is fs.ErrNotExist.
func readChecked(path string, rule fileRule) (data []byte, err error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ELOOP) {
		return nil, fmt.Errorf("%s is a symlink", path)
	}
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if err := keeps(path, info, rule); err != nil {
		return nil, err
	}
	data, err = io.ReadAll(io.LimitReader(f, int64(rule.max)+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > rule.max {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, rule.max)
	}
	return data, nil
}

// keeps checks an open file's kind, owner, group and mode against rule.
func keeps(path string, info os.FileInfo, rule fileRule) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	uid, gid, err := ownerOf(info)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	perm := info.Mode().Perm()
	switch {
	case uid != rule.uid:
		return fmt.Errorf("%s is owned by uid %d, not %d", path, uid, rule.uid)
	case rule.gid >= 0 && gid != rule.gid:
		return fmt.Errorf("%s has group %d, not %d", path, gid, rule.gid)
	case rule.atMost && perm&^rule.mode != 0:
		return fmt.Errorf("%s has mode %04o, looser than %04o", path, perm, rule.mode)
	case !rule.atMost && perm != rule.mode:
		return fmt.Errorf("%s has mode %04o, not %04o", path, perm, rule.mode)
	}
	return nil
}

// ownerOf is a file's uid and gid.
func ownerOf(info os.FileInfo) (int, int, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, errors.New("the file system gives no owner")
	}
	return int(st.Uid), int(st.Gid), nil
}

// printable is one or more bytes of printable ASCII, no space.
func printable(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}
