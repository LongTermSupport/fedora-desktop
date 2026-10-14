// Package registry is the ccy session registry: one file per live session, so a reboot can
// be undone. A record is written when a session starts and removed when its command
// returns, so a record still present at boot means "this was running when the machine went
// down". The on-disk format is shared with lib/session-registry.bash, which still writes and
// reads it, so both sides must accept exactly what the other produces.
package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	header      = "ccy-session-record 1"
	keyLauncher = "launcher"
	keyPrefix   = "prefix"
	valYes      = "yes"
	valNo       = "no"
)

// composeOutcomes are the values a record's compose= may hold.
var composeOutcomes = []string{"started", "declined", "running"}

var (
	// ErrNoRecord means the named session has no record.
	ErrNoRecord = errors.New("no session record")
	// ErrUnreadable means a record exists but is malformed. It is reported, never skipped:
	// a guessed record starts the wrong thing in the wrong place.
	ErrUnreadable = errors.New("session record is unreadable")
)

// RelativeStateHomeError is a relative XDG_STATE_HOME. Its text is the bash library's
// sentence verbatim, which the parity suite asserts on.
type RelativeStateHomeError struct {
	Value string
}

func (e *RelativeStateHomeError) Error() string {
	return "XDG_STATE_HOME='" + e.Value + "' is not absolute, so the session registry's location would depend on the current directory."
}

// RelativeBaseError is a relative registry base (CCY_STATE_DIR, or the HOME-derived one). Its
// text is the bash library's sentence verbatim, trailing period included.
type RelativeBaseError struct {
	Value string
}

func (e *RelativeBaseError) Error() string {
	return "session registry base '" + e.Value + "' is not an absolute path."
}

// Record is one session's registry entry. Compose is empty when the record has no answer.
type Record struct {
	Name     string
	Dir      string
	Launcher string
	Prefix   string
	Restore  bool
	Compose  string
	Args     []string
}

// Listing is every record in the registry: the readable ones in name order, and the names of
// the ones that could not be read, so a caller can say what it is missing.
type Listing struct {
	Records    []Record
	Unreadable []string
}

// Store is a registry directory.
type Store struct {
	dir string
}

// New returns the registry stored in dir.
func New(dir string) *Store {
	return &Store{dir: dir}
}

// Locate resolves the registry directory from the environment and returns its Store.
func Locate(getenv func(string) string) (*Store, error) {
	dir, err := Dir(getenv)
	if err != nil {
		return nil, err
	}
	return New(dir), nil
}

// Dir returns the registry directory: $CCY_STATE_DIR/sessions, else
// ${XDG_STATE_HOME:-$HOME/.local/state}/ccy/sessions. A relative base is refused, because the
// location would then depend on the cwd of whoever launched and a restore would read an
// empty directory and report success.
func Dir(getenv func(string) string) (string, error) {
	base := getenv("CCY_STATE_DIR")
	if base == "" {
		var err error
		if base, err = defaultBase(getenv); err != nil {
			return "", err
		}
	}
	if !filepath.IsAbs(base) {
		return "", &RelativeBaseError{Value: base}
	}
	return base + "/sessions", nil
}

// defaultBase is ${XDG_STATE_HOME:-$HOME/.local/state}/ccy.
func defaultBase(getenv func(string) string) (string, error) {
	stateHome := getenv("XDG_STATE_HOME")
	if stateHome != "" && !filepath.IsAbs(stateHome) {
		return "", &RelativeStateHomeError{Value: stateHome}
	}
	if stateHome == "" {
		home := getenv("HOME")
		if home == "" {
			return "", errors.New("neither CCY_STATE_DIR, XDG_STATE_HOME nor HOME is set, so the session registry has no location")
		}
		stateHome = home + "/.local/state"
	}
	return stateHome + "/ccy", nil
}

// Dir is the directory holding the records.
func (s *Store) Dir() string {
	return s.dir
}

func validName(name string) error {
	if name == "" {
		return errors.New("session record name is empty")
	}
	if strings.ContainsRune(name, '/') {
		return fmt.Errorf("session record name '%s' contains a slash and cannot name a file", name)
	}
	return nil
}

// namedValue is a required field and what it holds, so fields are checked in a fixed order.
type namedValue struct{ field, value string }

func validate(r Record) error {
	if err := validName(r.Name); err != nil {
		return err
	}
	for _, f := range []namedValue{{"directory", r.Dir}, {keyLauncher, r.Launcher}, {keyPrefix, r.Prefix}} {
		if f.value == "" {
			return fmt.Errorf("session record for '%s' needs a %s", r.Name, f.field)
		}
	}
	if r.Compose != "" && !slices.Contains(composeOutcomes, r.Compose) {
		return fmt.Errorf("session record: compose outcome '%s' is not one of %s; the record was not written", r.Compose, strings.Join(composeOutcomes, " "))
	}
	for _, v := range slices.Concat([]string{r.Name, r.Dir, r.Launcher, r.Prefix}, r.Args) {
		if strings.Contains(v, "\n") {
			return fmt.Errorf("session record for '%s' cannot hold a value containing a newline; the record was not written", r.Name)
		}
	}
	return nil
}

func render(r Record) string {
	var b strings.Builder
	b.WriteString(header + "\n")
	b.WriteString("name=" + r.Name + "\n")
	b.WriteString("dir=" + r.Dir + "\n")
	b.WriteString("launcher=" + r.Launcher + "\n")
	b.WriteString("prefix=" + r.Prefix + "\n")
	b.WriteString("restore=" + term.YesNo(r.Restore) + "\n")
	if r.Compose != "" {
		b.WriteString("compose=" + r.Compose + "\n")
	}
	for _, a := range r.Args {
		b.WriteString("arg=" + a + "\n")
	}
	return b.String()
}

// Write stores or replaces the record, private to the user and written whole then renamed
// into place, so a reader never sees half a record.
func (s *Store) Write(r Record) error {
	if err := validate(r); err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("could not create the session registry at %s: %w", s.dir, err)
	}
	return fsx.WriteAtomic(filepath.Join(s.dir, r.Name), "session record", []byte(render(r)), 0o600)
}

// Get reads the record named name.
func (s *Store) Get(name string) (Record, error) {
	if err := validName(name); err != nil {
		return Record{}, err
	}
	rec, err := ReadFile(filepath.Join(s.dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, fmt.Errorf("session %s has no record in %s: %w", name, s.dir, ErrNoRecord)
	}
	return rec, err
}

// SetRestore changes whether session name is started again after a reboot, keeping
// everything else in its record. A session with no record is an error: there is nothing to
// restore or keep out. A record gone by the time it would be rewritten belongs to a session
// that ended meanwhile and is not written back.
func (s *Store) SetRestore(name string, restore bool) error {
	rec, err := s.Get(name)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(filepath.Join(s.dir, name)); statErr != nil {
		return fmt.Errorf("session %s ended while its record was being changed; nothing was written: %w", name, ErrNoRecord)
	}
	rec.Restore = restore
	return s.Write(rec)
}

// SetTeams makes seats (the canonical --teams list) the only --teams in session name's record,
// keeping everything else, so a restore or restart of the session replays the seats it was
// launched into. A session with no record is an error.
func (s *Store) SetTeams(name, seats string) error {
	rec, err := s.Get(name)
	if err != nil {
		return err
	}
	kept := make([]string, 0, len(rec.Args)+2)
	for i := 0; i < len(rec.Args); i++ {
		switch {
		case rec.Args[i] == "--teams":
			i++
		case strings.HasPrefix(rec.Args[i], "--teams="):
		default:
			kept = append(kept, rec.Args[i])
		}
	}
	kept = append(kept, "--teams", seats)
	rec.Args = kept
	return s.Write(rec)
}

// Remove deletes a session's record. An absent record is not an error: the session may have
// been started before the registry existed.
func (s *Store) Remove(name string) error {
	if err := validName(name); err != nil {
		return err
	}
	return fsx.RemoveIfPresent(filepath.Join(s.dir, name))
}

// List reads every record. Files matching *.tmp.* are writes in flight and are ignored. An
// absent registry directory is an empty registry.
func (s *Store) List() (Listing, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return Listing{}, nil
	}
	if err != nil {
		return Listing{}, fmt.Errorf("could not list the session registry %s: %w", s.dir, err)
	}
	var list Listing
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			continue
		}
		rec, readErr := ReadFile(filepath.Join(s.dir, e.Name()))
		if readErr != nil {
			list.Unreadable = append(list.Unreadable, e.Name())
			continue
		}
		list.Records = append(list.Records, rec)
	}
	return list, nil
}

func unreadable(path, why string) error {
	return fmt.Errorf("session record %s %s: %w", path, why, ErrUnreadable)
}

// ReadFile parses one record. It is strict: a wrong header, an unknown key, an unknown
// compose outcome, a bad restore value or a missing field is a rejection, never a guess.
func ReadFile(path string) (Record, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return Record{}, fmt.Errorf("session record %s cannot be read: %w", path, err)
	}
	if len(raw) == 0 {
		return Record{}, unreadable(path, "is empty")
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if lines[0] != header {
		return Record{}, unreadable(path, fmt.Sprintf("does not start with '%s'", header))
	}
	var rec Record
	var restore string
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			return Record{}, unreadable(path, "has a line without '=': "+line)
		}
		if key == "restore" {
			restore = value
			continue
		}
		if problem := rec.setField(key, value); problem != "" {
			return Record{}, unreadable(path, problem)
		}
	}
	// The record format's own order, so a record missing several fields always names the same one.
	for _, f := range []namedValue{{"name", rec.Name}, {"dir", rec.Dir}, {keyLauncher, rec.Launcher}, {keyPrefix, rec.Prefix}, {"restore", restore}} {
		if f.value == "" {
			return Record{}, unreadable(path, "is missing its "+f.field+" field")
		}
	}
	if restore != valYes && restore != valNo {
		return Record{}, unreadable(path, fmt.Sprintf("has restore='%s'", restore))
	}
	rec.Restore = restore == valYes
	return rec, nil
}

// setField stores one key=value line, returning why the record is unreadable, or "".
func (r *Record) setField(key, value string) string {
	switch key {
	case "name":
		r.Name = value
	case "dir":
		r.Dir = value
	case keyLauncher:
		r.Launcher = value
	case keyPrefix:
		r.Prefix = value
	case "compose":
		if !slices.Contains(composeOutcomes, value) {
			return fmt.Sprintf("has compose='%s'", value)
		}
		r.Compose = value
	case "arg":
		r.Args = append(r.Args, value)
	default:
		return fmt.Sprintf("has an unknown key '%s'", key)
	}
	return ""
}
