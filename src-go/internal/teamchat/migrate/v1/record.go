package v1

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// exportDir holds the records under teamchat-admin's state root. Its name starts with an
// underscore, which no team id can, so a walk of the state root that parses team ids skips it
// (as it skips _carry and _lxc).
const exportDir = "/_v1-export"

// stampLayout is a record's name: its UTC time to the second (E12).
const stampLayout = "20060102T150405Z"

var stampGrammar = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z$`)

// The record's file names.
const (
	digestName  = "digest.json"
	historyName = "history.md"
	maxDigest   = 1 << 20
)

// noType counts the events whose type cannot be read.
const noType = "(no type)"

// CheckStamp refuses a name that is not a record's stamp.
func CheckStamp(s string) error {
	if !stampGrammar.MatchString(s) {
		return fmt.Errorf("%q is not an export stamp (YYYYMMDDTHHMMSSZ)", s)
	}
	return nil
}

// teamRecords is the directory of a team's records.
func teamRecords(d Deps, teamName string) string {
	return d.Root + paths.AdminStateRoot + exportDir + "/" + teamName
}

func recordPath(d Deps, teamName, stamp string) string { return teamRecords(d, teamName) + "/" + stamp }

// absent refuses a record path that exists (E12: never overwritten).
func absent(path string) error {
	_, err := os.Lstat(path)
	switch {
	case err == nil:
		return fmt.Errorf("%s already exists: a record is never overwritten", path)
	case errors.Is(err, fs.ErrNotExist):
		return nil
	}
	return err
}

// recordFile is one file of a record.
type recordFile struct {
	name string
	data []byte
}

// digest is digest.json (E16).
type digest struct {
	V            int                    `json:"v"`
	Team         string                 `json:"team"`
	ServerName   string                 `json:"server_name"`
	ExportedAtMS int64                  `json:"exported_at_ms"`
	Rooms        map[string]*roomDigest `json:"rooms"`
	Files        map[string]fileDigest  `json:"files"`
}

type roomDigest struct {
	RoomID string         `json:"room_id"`
	Events int            `json:"events"`
	Types  map[string]int `json:"types"`
}

type fileDigest struct {
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// recordFiles is every file of the record, digest.json last (E13-E16).
func recordFiles(t team, at time.Time, rooms []roomRecord) ([]recordFile, error) {
	var files []recordFile
	d := digest{V: 1, Team: t.name, ServerName: t.serverName, ExportedAtMS: at.UnixMilli(), Rooms: map[string]*roomDigest{}, Files: map[string]fileDigest{}}
	for _, p := range purposes {
		d.Rooms[p] = nil
	}
	for _, r := range rooms {
		var jsonl bytes.Buffer
		raws := make([][]byte, 0, len(r.events))
		for _, e := range r.events {
			if err := json.Compact(&jsonl, e); err != nil {
				return nil, fmt.Errorf("an event of the %s room: %w", r.purpose, err)
			}
			jsonl.WriteByte('\n')
			raws = append(raws, e)
		}
		d.Rooms[r.purpose] = &roomDigest{RoomID: r.id, Events: len(r.events), Types: typeCounts(raws)}
		files = append(files,
			recordFile{name: "events-" + r.purpose + ".jsonl", data: jsonl.Bytes()},
			recordFile{name: "state-" + r.purpose + ".json", data: r.state})
	}
	files = append(files, recordFile{name: historyName, data: []byte(renderHistory(t.name, t.serverName, at, rooms))})
	for _, f := range files {
		d.Files[f.name] = digestOf(f.data)
	}
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(files, recordFile{name: digestName, data: append(raw, '\n')}), nil
}

func digestOf(data []byte) fileDigest {
	sum := sha256.Sum256(data)
	return fileDigest{Bytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}

// typeCounts is the number of events of each type.
func typeCounts(events [][]byte) map[string]int {
	counts := map[string]int{}
	for _, e := range events {
		var t struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(e, &t) != nil || t.Type == "" {
			t.Type = noType
		}
		counts[t.Type]++
	}
	return counts
}

// writeRecord writes files into a temporary directory beside the record's final name, syncs
// them, and renames the directory into place, never over one that exists (E12). Any failure
// removes the temporary directory.
func writeRecord(d Deps, teamName, stamp string, files []recordFile) (rec string, err error) {
	parent, err := recordParent(d, teamName)
	if err != nil {
		return "", err
	}
	final := parent + "/" + stamp
	tmp, err := os.MkdirTemp(parent, "."+stamp+".part-")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.RemoveAll(tmp))
		}
	}()
	if err := os.Chmod(tmp, 0o700); err != nil {
		return "", err
	}
	for _, f := range files {
		if err := writeSynced(tmp+"/"+f.name, f.data); err != nil {
			return "", err
		}
	}
	if err := syncDir(tmp); err != nil {
		return "", err
	}
	err = unix.Renameat2(unix.AT_FDCWD, tmp, unix.AT_FDCWD, final, unix.RENAME_NOREPLACE)
	if errors.Is(err, unix.EEXIST) || errors.Is(err, unix.ENOTEMPTY) {
		return "", fmt.Errorf("%s already exists: a record is never overwritten", final)
	}
	if err != nil {
		return "", fmt.Errorf("rename the record into place at %s: %w", final, err)
	}
	return final, syncDir(parent)
}

// recordParent makes the team's records directory below teamchat-admin's state root, which must
// exist; each directory made or found is root's, 0700, and not a symlink.
func recordParent(d Deps, teamName string) (string, error) {
	base := d.Root + paths.AdminStateRoot
	if info, err := os.Lstat(base); err != nil {
		return "", fmt.Errorf("teamchat-admin's state root %s is missing (v2's play makes it): %w", base, err)
	} else if !info.IsDir() {
		return "", fmt.Errorf("teamchat-admin's state root %s is not a directory", base)
	}
	dir := base
	for _, name := range []string{exportDir, "/" + teamName} {
		dir += name
		if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		info, err := os.Lstat(dir)
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%s is not a directory (a symlink or another file)", dir)
		}
		uid, _, err := ownerOf(info)
		if err != nil {
			return "", err
		}
		if uid != d.RootUID {
			return "", fmt.Errorf("%s is owned by uid %d, not root's %d", dir, uid, d.RootUID)
		}
	}
	return dir, nil
}

// writeSynced creates path, 0600, writes data and syncs it.
func writeSynced(path string, data []byte) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

func syncDir(dir string) (err error) {
	f, err := os.OpenFile(dir, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	return f.Sync()
}

// newest is the newest record's stamp in dir.
func newest(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	best := ""
	for _, e := range entries {
		if e.IsDir() && stampGrammar.MatchString(e.Name()) && e.Name() > best {
			best = e.Name()
		}
	}
	if best == "" {
		return "", fmt.Errorf("no record in %s", dir)
	}
	return best, nil
}

// MismatchError is a record whose files do not match its digest: one problem a line (E18).
type MismatchError struct {
	Record   string
	Problems []string
}

func (e *MismatchError) Error() string {
	return fmt.Sprintf("the record %s does not match its digest: %s", e.Record, strings.Join(e.Problems, "; "))
}

// verifyDir re-reads the record at dir against its digest (E18) and returns the digest's bytes. A
// digest that cannot be read is an error; files that do not match are a *MismatchError.
func verifyDir(dir, teamName string) ([]byte, error) {
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory (a symlink or another file)", dir)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	raw, err := readIn(root, digestName, maxDigest)
	if err != nil {
		return nil, fmt.Errorf("%s/%s: %w", dir, digestName, err)
	}
	d, err := decodeDigest(raw, teamName)
	if err != nil {
		return nil, fmt.Errorf("%s/%s %w", dir, digestName, err)
	}
	var problems []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		switch name := e.Name(); {
		case name == digestName:
		case !e.Type().IsRegular():
			problems = append(problems, name+" is not a regular file")
		case !hasFile(d, name):
			problems = append(problems, name+" is not listed in "+digestName)
		}
	}
	for name, want := range d.Files {
		data, err := readIn(root, name, want.Bytes)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			problems = append(problems, name+" is missing")
		case errors.Is(err, errNotRegular):
			// Named above, as every entry that is not a regular file is.
		case errors.Is(err, errTooLarge):
			problems = append(problems, name+" is changed")
		case err != nil:
			return nil, fmt.Errorf("%s/%s: %w", dir, name, err)
		case digestOf(data) != want:
			problems = append(problems, name+" is changed")
		}
	}
	if len(problems) > 0 {
		slices.Sort(problems)
		return nil, &MismatchError{Record: dir, Problems: problems}
	}
	return raw, nil
}

func hasFile(d digest, name string) bool {
	_, ok := d.Files[name]
	return ok
}

// decodeDigest reads digest.json strictly: version 1, the team asked for, plain file names.
func decodeDigest(raw []byte, teamName string) (digest, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var d digest
	if err := dec.Decode(&d); err != nil {
		return digest{}, fmt.Errorf("does not decode: %w", err)
	}
	if d.V != 1 {
		return digest{}, fmt.Errorf("is version %d, not 1", d.V)
	}
	if d.Team != teamName {
		return digest{}, fmt.Errorf("is of team %s, not %s", d.Team, teamName)
	}
	for name := range d.Files {
		if name == "" || name == digestName || strings.ContainsAny(name, "/\x00") || strings.HasPrefix(name, ".") {
			return digest{}, fmt.Errorf("names a file %q that cannot be in a record", name)
		}
	}
	return d, nil
}

var (
	errNotRegular = errors.New("not a regular file")
	errTooLarge   = errors.New("larger than expected")
)

// readIn reads name in root, a regular file of at most limit bytes, without following a symlink
// and without waiting on a pipe.
func readIn(root *os.Root, name string, limit int64) (data []byte, err error) {
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ELOOP) {
		return nil, errNotRegular
	}
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errNotRegular
	}
	data, err = io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errTooLarge
	}
	return data, nil
}

// copyTarget checks E17's target before anything is read: the owner an existing account, the
// directory an existing directory that account owns.
func copyTarget(d Deps, req Request) (Account, error) {
	acct, found, err := d.Lookup(req.CopyOwner)
	if err != nil {
		return Account{}, fmt.Errorf("cannot look up the copy's owner %s: %w", req.CopyOwner, err)
	}
	if !found {
		return Account{}, fmt.Errorf("--copy-owner: no account %s on this host", req.CopyOwner)
	}
	info, err := os.Lstat(req.CopyTo)
	if err != nil {
		return Account{}, fmt.Errorf("--copy-to: %w", err)
	}
	if !info.IsDir() {
		return Account{}, fmt.Errorf("--copy-to: %s is not a directory (a symlink or another file)", req.CopyTo)
	}
	uid, _, err := ownerOf(info)
	if err != nil {
		return Account{}, err
	}
	if uid != acct.UID {
		return Account{}, fmt.Errorf("--copy-to: %s is owned by uid %d, not %s (uid %d)", req.CopyTo, uid, req.CopyOwner, acct.UID)
	}
	return acct, nil
}

// copyRecord copies the record rec to <CopyTo>/<team>-<stamp>/ (E17): each file 0600, the
// directory 0700, all the owner's, verified against the record's digest while the directory is
// still root's, then given to the owner. A failed copy is removed; the record stays.
func copyRecord(d Deps, rec, teamName, stamp string, req Request) (dst string, err error) {
	acct, err := copyTarget(d, req)
	if err != nil {
		return "", fmt.Errorf("%w; the record %s stays", err, rec)
	}
	target := req.CopyTo + "/" + teamName + "-" + stamp
	if err := os.Mkdir(target, 0o700); err != nil {
		return "", fmt.Errorf("the copy: %w; the record %s stays", err, rec)
	}
	defer func() {
		if err != nil {
			err = fmt.Errorf("the copy to %s failed: %w; the record %s stays", target, errors.Join(err, os.RemoveAll(target)), rec)
		}
	}()
	want, err := verifyDir(rec, teamName)
	if err != nil {
		return "", err
	}
	if err := fillCopy(rec, target, want, acct); err != nil {
		return "", err
	}
	got, err := verifyDir(target, teamName)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(got, want) {
		return "", errors.New("the copy's digest.json is not the record's")
	}
	return target, giveDir(target, acct)
}

// fillCopy writes each file of the record's digest, and the digest, into dst through dst's root,
// so nothing in dst can lead a write outside it.
func fillCopy(rec, dst string, digestRaw []byte, acct Account) (err error) {
	var d digest
	if err := json.Unmarshal(digestRaw, &d); err != nil {
		return err
	}
	src, err := os.OpenRoot(rec)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, src.Close()) }()
	out, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, out.Close()) }()
	names := []string{digestName}
	for name := range d.Files {
		names = append(names, name)
	}
	for _, name := range names {
		limit := int64(maxDigest)
		if f, ok := d.Files[name]; ok {
			limit = f.Bytes
		}
		data, err := readIn(src, name, limit)
		if err != nil {
			return fmt.Errorf("%s/%s: %w", rec, name, err)
		}
		if err := writeOwned(out, name, data, acct); err != nil {
			return fmt.Errorf("%s/%s: %w", dst, name, err)
		}
	}
	return nil
}

func writeOwned(root *os.Root, name string, data []byte, acct Account) (err error) {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Chown(acct.UID, acct.GID); err != nil {
		return err
	}
	return f.Sync()
}

// giveDir gives the finished copy's directory to its owner, mode 0700.
func giveDir(dir string, acct Account) (err error) {
	f, err := os.OpenFile(dir, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	if err := f.Chown(acct.UID, acct.GID); err != nil {
		return err
	}
	if err := f.Chmod(0o700); err != nil {
		return err
	}
	return f.Sync()
}
