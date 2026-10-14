// Package restore is the record of what a boot-time session restore brought up, and the
// verdict on each session it restored. The manifest is the restore's own account of every
// session that should now be up: a restored session's registry record is removed by its
// trampoline the moment its launcher returns, so a session that failed to come back leaves no
// record behind to check. The on-disk format is shared with lib/session-registry.bash, which
// still reads and writes it, so both sides must accept exactly what the other produces.
package restore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	manifestHeader   = "ccy-restore-manifest 2"
	headerPrefix     = "ccy-restore-manifest "
	defaultBootFile  = "/proc/sys/kernel/random/boot_id"
	defaultLockWait  = 60 * time.Second
	lockPollInterval = 100 * time.Millisecond
	dirMode          = 0o700
	fileMode         = 0o600
	valYes           = "yes"
	valNo            = "no"
)

// manifestKeys is the nine lines of an entry, in order.
var manifestKeys = [...]string{"name", "prefix", "dir", "resume", "supervised", "going", "at", "detail", "transcript"}

// Going is how far a restored session has been set going.
type Going string

// The words an entry's going= may hold, and GoingKeep, which is only ever an input to Merge.
const (
	// GoingNone is a session found already running: a person started it, nothing is typed into it.
	GoingNone Going = "none"
	// GoingPending is restored, not yet decided.
	GoingPending Going = "pending"
	// GoingCompacting is a session with no supervisor: /compact typed at At, `continue` to follow once it ends.
	GoingCompacting Going = "compacting"
	// GoingCompact is /compact typed at At (a supervisor carries the session on).
	GoingCompact Going = "compact"
	// GoingContinue is `continue` typed at At.
	GoingContinue Going = "continue"
	// GoingUntouched is left alone; Detail says why.
	GoingUntouched Going = "untouched"
	// GoingKeep, given to Merge, takes the entry this boot's manifest already holds for that name.
	GoingKeep Going = "keep"
)

func (g Going) valid() bool {
	switch g {
	case GoingNone, GoingPending, GoingCompacting, GoingCompact, GoingContinue, GoingUntouched:
		return true
	case GoingKeep:
		return false
	}
	return false
}

// Entry is one session the restore should have brought up, and what set-going did to it.
type Entry struct {
	Name       string
	Prefix     string
	Dir        string
	Resume     string // the conversation id the launch names with --resume, or empty
	Supervised bool   // a ccy supervisor carries the session on after a compaction
	Going      Going
	At         time.Time // when the last input was typed; the zero time is none (on disk: Unix seconds in digits, or empty)
	Detail     string    // the context size in tokens, or the reason it was left alone
	Transcript string    // the conversation's file
}

// AtUnix is At in Unix seconds; an entry with none is 0, as bash arithmetic reads an empty value.
func (e Entry) AtUnix() int64 {
	if e.At.IsZero() {
		return 0
	}
	return e.At.Unix()
}

// atField is At as the manifest holds it: digits, or empty for none.
func (e Entry) atField() string {
	if e.At.IsZero() {
		return ""
	}
	return strconv.FormatInt(e.At.Unix(), 10)
}

// Manifest is the manifest as read: the boot it was written in and its entries in order.
type Manifest struct {
	Boot    string
	Entries []Entry
}

// Index is the position of the named session, or -1.
func (m Manifest) Index(name string) int {
	for i, e := range m.Entries {
		if e.Name == name {
			return i
		}
	}
	return -1
}

var (
	// ErrNoManifest means no restore has been recorded.
	ErrNoManifest = errors.New("no session restore has been recorded")
	// ErrNoLongerApplies means an update found the entry gone, moved on, or from another boot.
	ErrNoLongerApplies = errors.New("the restore manifest entry no longer applies")
)

// Store is the manifest beside the registry and the locks around it.
type Store struct {
	// Path is the manifest; the locks sit beside it.
	Path string
	// BootIDFile is where this boot's id is read; empty means the kernel's.
	BootIDFile string
	// LockWait is how long a write waits for the manifest's lock.
	LockWait time.Duration
}

// NewStore is the manifest that belongs to the registry directory (<state>/ccy/sessions): it
// lives beside it, never inside it, because a restore reads every file in the registry.
func NewStore(registryDir, bootIDFile string) *Store {
	return &Store{
		Path:       strings.TrimSuffix(registryDir, "/sessions") + "/last-restore",
		BootIDFile: bootIDFile,
		LockWait:   defaultLockWait,
	}
}

// BootID is this boot's id: the first line of the boot id file.
func (s *Store) BootID() (string, error) {
	file := s.bootFile()
	raw, err := os.ReadFile(filepath.Clean(file))
	if err != nil {
		return "", fmt.Errorf("this boot's id could not be read from %s", file)
	}
	id, _, _ := strings.Cut(string(raw), "\n")
	if id == "" {
		return "", fmt.Errorf("this boot's id could not be read from %s", file)
	}
	return id, nil
}

// Read parses the manifest strictly: each entry is every key in order, going one of its words,
// supervised yes or no, at digits or empty, and anything else is a rejection.
func (s *Store) Read() (Manifest, error) {
	raw, err := os.ReadFile(filepath.Clean(s.Path))
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, fmt.Errorf("%w (%s does not exist)", ErrNoManifest, s.Path)
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("the restore manifest %s cannot be read: %w", s.Path, err)
	}
	return parse(s.Path, string(raw))
}

func parse(path, content string) (Manifest, error) {
	if content == "" {
		return Manifest{}, fmt.Errorf("the restore manifest %s is empty", path)
	}
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	if err := checkHeader(path, lines[0]); err != nil {
		return Manifest{}, err
	}
	var m Manifest
	var cur Entry
	next := 0
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			return Manifest{}, fmt.Errorf("the restore manifest %s has a line without '=': %s", path, line)
		}
		if key == "boot" && m.Boot == "" && next == 0 && len(m.Entries) == 0 {
			m.Boot = value
			continue
		}
		if key != manifestKeys[next] {
			return Manifest{}, fmt.Errorf("the restore manifest %s has '%s' where '%s' was expected and is not read", path, key, manifestKeys[next])
		}
		if err := setEntryField(&cur, path, key, value); err != nil {
			return Manifest{}, err
		}
		next = (next + 1) % len(manifestKeys)
		if next == 0 {
			m.Entries = append(m.Entries, cur)
			cur = Entry{}
		}
	}
	if m.Boot == "" {
		return Manifest{}, fmt.Errorf("the restore manifest %s names no boot", path)
	}
	if next != 0 {
		return Manifest{}, fmt.Errorf("the restore manifest %s ends part-way through an entry", path)
	}
	return m, nil
}

// checkHeader refuses a first line that is not the current header, naming a manifest another
// version of ccy-sessions wrote.
func checkHeader(path, first string) error {
	if first == manifestHeader {
		return nil
	}
	if strings.HasPrefix(first, headerPrefix) {
		return fmt.Errorf("the restore manifest %s is '%s', written by another version of ccy-sessions; this one reads '%s'. The next restore (at the next boot) writes it again", path, first, manifestHeader)
	}
	return fmt.Errorf("the restore manifest %s does not start with '%s' and is not read", path, manifestHeader)
}

// setEntryField stores one validated manifest line.
func setEntryField(e *Entry, path, key, value string) error {
	switch key {
	case "name":
		e.Name = value
	case "prefix":
		e.Prefix = value
	case "dir":
		e.Dir = value
	case "resume":
		e.Resume = value
	case "supervised":
		if value != valYes && value != valNo {
			return fmt.Errorf("the restore manifest %s has supervised=%s, which is neither yes nor no, and is not read", path, value)
		}
		e.Supervised = value == valYes
	case "going":
		if !Going(value).valid() {
			return fmt.Errorf("the restore manifest %s has going=%s, which is none of none, pending, compacting, compact, continue or untouched, and is not read", path, value)
		}
		e.Going = Going(value)
	case "at":
		at, err := parseAt(value)
		if err != nil {
			return fmt.Errorf("the restore manifest %s has at=%s, which is not a time in seconds, and is not read", path, value)
		}
		e.At = at
	case "detail":
		e.Detail = value
	case "transcript":
		e.Transcript = value
	}
	return nil
}

// parseAt is a manifest's at= value: digits, or empty for no time.
func parseAt(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	if strings.Trim(value, "0123456789") != "" {
		return time.Time{}, errors.New("not digits")
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("not a time in seconds: %w", err)
	}
	return time.Unix(n, 0), nil
}

func yesNo(b bool) string {
	if b {
		return valYes
	}
	return valNo
}

// values is the entry's nine values in manifest order.
func (e Entry) values() [len(manifestKeys)]string {
	return [...]string{e.Name, e.Prefix, e.Dir, e.Resume, yesNo(e.Supervised), string(e.Going), e.atField(), e.Detail, e.Transcript}
}

// Write records the sessions a restore brought up, stamped with this boot's id, written whole
// then moved into place like a record. A value holding a newline is refused: it would read back
// as two lines. A caller that read the manifest first holds the lock around both (see Locked).
func (s *Store) Write(entries []Entry) error {
	for _, e := range entries {
		if !e.Going.valid() {
			return fmt.Errorf("the restore manifest cannot hold going=%s for %s", e.Going, e.Name)
		}
		for _, v := range e.values() {
			if strings.Contains(v, "\n") {
				return fmt.Errorf("the restore manifest cannot hold a value with a newline in it: %s", v)
			}
		}
	}
	boot, err := s.BootID()
	if err != nil {
		return err
	}
	if mkErr := os.MkdirAll(filepath.Dir(s.Path), dirMode); mkErr != nil {
		return fmt.Errorf("could not create the directory for the restore manifest %s: %w", s.Path, mkErr)
	}
	var b strings.Builder
	b.WriteString(manifestHeader + "\nboot=" + boot + "\n")
	for _, e := range entries {
		for i, v := range e.values() {
			b.WriteString(manifestKeys[i] + "=" + v + "\n")
		}
	}
	tmp := fmt.Sprintf("%s.tmp.%d", s.Path, os.Getpid())
	if writeErr := os.WriteFile(tmp, []byte(b.String()), fileMode); writeErr != nil {
		return errors.Join(fmt.Errorf("could not write the restore manifest %s: %w", s.Path, writeErr), removeIfPresent(tmp))
	}
	if renameErr := os.Rename(tmp, s.Path); renameErr != nil {
		return errors.Join(fmt.Errorf("could not move the restore manifest into place at %s: %w", s.Path, renameErr), removeIfPresent(tmp))
	}
	return nil
}

func removeIfPresent(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Locked runs fn holding the manifest's lock (flock on the file beside it, waited for up to
// LockWait). Every read-modify-write of the manifest goes through here.
func (s *Store) Locked(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), dirMode); err != nil {
		return fmt.Errorf("could not create the directory for the restore manifest %s: %w", s.Path, err)
	}
	lock := s.Path + ".lock"
	release, err := acquire(lock, s.LockWait,
		"could not open the restore manifest's lock "+lock,
		fmt.Sprintf("the restore manifest's lock %s was not released within %d s; nothing was written", lock, int(s.LockWait.Seconds())))
	if err != nil {
		return err
	}
	return errors.Join(fn(), release())
}

// LockSetGoing takes set-going's own lock, waiting up to wait, and returns the function that
// releases it. A wait of 0 still tries once.
func (s *Store) LockSetGoing(wait time.Duration) (func() error, error) {
	if err := os.MkdirAll(filepath.Dir(s.Path), dirMode); err != nil {
		return nil, fmt.Errorf("could not create the directory for the restore manifest %s: %w", s.Path, err)
	}
	lock := s.Path + ".set-going.lock"
	return acquire(lock, wait,
		"could not open set-going's lock "+lock,
		fmt.Sprintf("another set-going still holds %s after %ds; nothing was set going by this one", lock, int(wait.Seconds())))
}

// acquire opens the lock file for append and takes an exclusive flock on it, polling until wait
// has passed. openMsg and busyMsg are the whole errors for the two ways it can fail.
func acquire(path string, wait time.Duration, openMsg, busyMsg string) (func() error, error) {
	f, err := os.OpenFile(filepath.Clean(path), os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", openMsg, err)
	}
	deadline := time.Now().Add(wait)
	for {
		lockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if lockErr == nil {
			return f.Close, nil
		}
		if !errors.Is(lockErr, syscall.EWOULDBLOCK) {
			return nil, errors.Join(fmt.Errorf("could not lock %s: %w", path, lockErr), f.Close())
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, errors.Join(errors.New(busyMsg), f.Close())
		}
		time.Sleep(min(lockPollInterval, remaining))
	}
}

// Merge is the restore's write, under the lock. An entry whose going is GoingKeep (a session
// found already running) takes the entry this boot's manifest already holds for that name, so a
// second restore leaves an earlier one's set-going state alone; with none there, it is written
// as GoingNone. No manifest, one from an earlier boot, or one that cannot be read starts empty:
// the entries this restore knows are the ones that should be up. warn, if not nil, is told why
// an existing manifest was replaced because it could not be read; that is not a failure.
func (s *Store) Merge(entries []Entry, warn func(error)) error {
	return s.Locked(func() error {
		boot, err := s.BootID()
		if err != nil {
			return err
		}
		var held Manifest
		if _, statErr := os.Stat(s.Path); statErr == nil {
			m, readErr := s.Read()
			switch {
			case readErr != nil && warn != nil:
				warn(readErr)
			case readErr == nil && m.Boot == boot:
				held = m
			}
		}
		out := make([]Entry, 0, len(entries))
		for _, e := range entries {
			if e.Going == GoingKeep {
				if i := held.Index(e.Name); i >= 0 {
					e = held.Entries[i]
				} else {
					e.Going = GoingNone
				}
			}
			out = append(out, e)
		}
		return s.Write(out)
	})
}

// Change is the four fields set-going records about an entry.
type Change struct {
	Going      Going
	At         time.Time
	Detail     string
	Transcript string
}

// Update sets one entry's outcome under the lock, but only while the manifest is still boot's
// and the entry still says expect: a second restore may have rewritten it meanwhile.
// ErrNoLongerApplies, changing nothing, says it no longer applies.
func (s *Store) Update(boot, name string, expect Going, change Change) error {
	return s.Locked(func() error {
		m, err := s.Read()
		if err != nil {
			return err
		}
		i := m.Index(name)
		if m.Boot != boot || i < 0 || m.Entries[i].Going != expect {
			return ErrNoLongerApplies
		}
		m.Entries[i].Going = change.Going
		m.Entries[i].At = change.At
		m.Entries[i].Detail = change.Detail
		m.Entries[i].Transcript = change.Transcript
		return s.Write(m.Entries)
	})
}

func (s *Store) bootFile() string {
	if s.BootIDFile == "" {
		return defaultBootFile
	}
	return s.BootIDFile
}
