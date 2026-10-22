// Package status is a team's status record, `/var/lib/teamchat-status/teams/<team>/status.json`
// (Plan 00209 SPEC-n2e.md SR1-SR5, DESIGN-v2-C-host.md 3.11.3, DC4): its strict schema, its
// atomic write through teamchat/state, the seq rule every writer follows, and the bounded read
// ccy-teams' board uses, which never shows an expired record as current.
package status

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// ErrInvalid is a record that breaks SR1.
var ErrInvalid = errors.New("status: invalid")

const (
	// MaxSize caps a record, written or read (n2e-D1).
	MaxSize = 1 << 20
	// Lifetime is how long a record is current: expires_ms is written_ms plus this.
	Lifetime = 300 * time.Second
	// EventAge bounds the age of the events a writer keeps (n2e-D2).
	EventAge = 24 * time.Hour
	// MaxEvents bounds their number.
	MaxEvents = 32
	// FileMode is the record's mode; its group is the owner group.
	FileMode fs.FileMode = 0o640
)

// The values SR1 allows.
const (
	OriginV2        = "v2"
	OriginMigration = "migration"
	ProbeOK         = "ok"
	ProbeMissed     = "missed"
	ProbeNotRun     = "not-run"
	RunOK           = "ok"
	RunFailed       = "failed"
	RunNone         = "none"
)

var (
	unitStates  = []string{wordActive, "reloading", "inactive", "failed", "activating", "deactivating", "maintenance", "refreshing"}
	memberships = []string{"join", "invite", "leave", "ban", "knock"}
	purposes    = []string{proto.PurposeMain, proto.PurposeReports, proto.PurposeQuestions}
	roles       = []string{proto.RoleCoordinator, proto.RoleWorker, proto.RoleProbe}
	memberState = []string{wordActive, "parked", "retired", "lost", "claimed"}
	humanState  = []string{wordActive, "removed"}
	kindRe      = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
)

const wordActive = "active"

// KindLXC is the member kind whose entry carries online.
const KindLXC = "lxc"

// Record is status.json (C 3.11.3).
type Record struct {
	V      int          `json:"v"`
	Team   paths.TeamID `json:"team"`
	Name   string       `json:"name"`
	Origin string       `json:"origin"`
	Set    *paths.SetID `json:"set"`
	// Migration is area D's, written by its migrate verbs and kept as written.
	Migration  json.RawMessage `json:"migration"`
	Seq        int64           `json:"seq"`
	WrittenMS  int64           `json:"written_ms"`
	ExpiresMS  int64           `json:"expires_ms"`
	Homeserver Homeserver      `json:"homeserver"`
	Members    []Member        `json:"members"`
	Humans     []Human         `json:"humans"`
	Backup     Backup          `json:"backup"`
	Disk       Disk            `json:"disk"`
	Restore    *Restore        `json:"restore"`
	Problems   []Problem       `json:"problems"`
	Events     []Event         `json:"events"`
}

// Homeserver is the unit and the probe at the tick.
type Homeserver struct {
	Unit      string `json:"unit"`
	SinceMS   int64  `json:"since_ms"`
	NRestarts int64  `json:"nrestarts"`
	Probe     string `json:"probe"`
}

// Member is one agent; Online is on an LXC member's entry only.
type Member struct {
	Handle string            `json:"handle"`
	Kind   string            `json:"kind"`
	Role   string            `json:"role"`
	State  string            `json:"state"`
	Online *bool             `json:"online,omitempty"`
	Rooms  map[string]string `json:"rooms"`
}

// Human is one human.
type Human struct {
	Name       string            `json:"name"`
	State      string            `json:"state"`
	Rooms      map[string]string `json:"rooms"`
	Devices    int64             `json:"devices"`
	LastSeenMS int64             `json:"last_seen_ms"`
}

// Backup is the newest usable backup (nil for none) and the last run.
type Backup struct {
	Newest  *Newest `json:"newest"`
	LastRun string  `json:"last_run"`
}

// Newest is one backup.
type Newest struct {
	ID     int64 `json:"id"`
	TimeMS int64 `json:"time_ms"`
}

// Disk is the free space of /var/lib's filesystem.
type Disk struct {
	FreeBytes int64 `json:"free_bytes"`
	FreePct   int64 `json:"free_pct"`
}

// Restore is restore.json's step, copied.
type Restore struct {
	Step   string `json:"step"`
	Backup int64  `json:"backup"`
	Pre    int64  `json:"pre"`
}

// Problem is one thing wrong and what to run.
type Problem struct {
	What string `json:"what"`
	Fix  string `json:"fix"`
}

// Event is one thing that happened.
type Event struct {
	AtMS int64  `json:"at_ms"`
	What string `json:"what"`
}

var topKeys = []string{
	"v", "team", "name", "origin", "set", "migration", "seq", "written_ms", "expires_ms", "homeserver",
	"members", "humans", "backup", "disk", "restore", "problems", "events",
}

// Decode reads team's record strictly (SR1).
func Decode(data []byte, team paths.TeamID) (Record, error) {
	if len(data) > MaxSize {
		return Record{}, invalid("larger than %d bytes", MaxSize)
	}
	v, err := proto.Parse(data)
	if err != nil {
		return Record{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if v.Kind() != proto.KindObject {
		return Record{}, invalid("not an object")
	}
	if err := keysLowercase(v, false); err != nil {
		return Record{}, err
	}
	for _, key := range topKeys {
		if _, ok := v.Get(key); !ok {
			return Record{}, invalid("no %s", key)
		}
	}
	var rec Record
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rec); err != nil {
		return Record{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Record{}, invalid("content after the value")
	}
	if err := rec.check(team); err != nil {
		return Record{}, err
	}
	return rec, sameWritten(rec, v)
}

// sameWritten requires every nested key too: the record written again must be the one read.
func sameWritten(rec Record, read proto.Value) error {
	again, err := json.Marshal(rec.normal())
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if parsed, err := proto.Parse(again); err != nil || !parsed.Equal(read) {
		return invalid("a key is missing")
	}
	return nil
}

// keysLowercase refuses any key outside lowercase ASCII and `_`, since encoding/json matches a
// field name case-insensitively; inside the migration, area D's object, keys are left alone.
func keysLowercase(v proto.Value, inMigration bool) error {
	if v.Kind() == proto.KindObject {
		for _, m := range v.Members() {
			if !inMigration && strings.IndexFunc(m.Key, func(r rune) bool { return (r < 'a' || r > 'z') && r != '_' }) >= 0 {
				return invalid("a key that is not lowercase ASCII")
			}
			if err := keysLowercase(m.Val, inMigration || m.Key == "migration"); err != nil {
				return err
			}
		}
	}
	for _, item := range v.Items() {
		if err := keysLowercase(item, inMigration); err != nil {
			return err
		}
	}
	return nil
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func validName(s string) bool {
	_, err := paths.ParseDisplayName(s)
	return err == nil
}

func (h Homeserver) check() error {
	if !slices.Contains(unitStates, h.Unit) || h.SinceMS < 0 || h.NRestarts < 0 {
		return invalid("homeserver")
	}
	if h.Probe != ProbeOK && h.Probe != ProbeMissed && h.Probe != ProbeNotRun {
		return invalid("homeserver.probe")
	}
	return nil
}

func checkRooms(rooms map[string]string) error {
	if rooms == nil {
		return invalid("rooms is not an object")
	}
	for purpose, membership := range rooms {
		if !slices.Contains(purposes, purpose) || !slices.Contains(memberships, membership) {
			return invalid("a room membership")
		}
	}
	return nil
}

func (b Backup) check() error {
	if b.Newest != nil && (b.Newest.ID < 0 || b.Newest.TimeMS < 0) {
		return invalid("backup.newest")
	}
	if b.LastRun != RunOK && b.LastRun != RunFailed && b.LastRun != RunNone {
		return invalid("backup.last_run")
	}
	return nil
}

func (d Disk) check() error {
	if d.FreeBytes < 0 || d.FreePct < 0 || d.FreePct > 100 {
		return invalid("disk")
	}
	return nil
}

// OneLine is a non-empty printable line of at most 512 bytes: no control or format character
// and no line or paragraph separator, so a reader printing it can never be given a second line.
func OneLine(s string) bool {
	if s == "" || len(s) > 512 || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r == unicode.ReplacementChar || unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) || r == ' ' || r == ' ' {
			return false
		}
	}
	return true
}

// NewProblem is a problem, refused unless both parts are one line.
func NewProblem(what, fix string) (Problem, error) {
	if !OneLine(what) || !OneLine(fix) {
		return Problem{}, invalid("a problem is not two lines of text")
	}
	return Problem{What: what, Fix: fix}, nil
}

// Encode is the record as written; it must decode under SR1.
func (r Record) Encode() ([]byte, error) {
	data, err := json.Marshal(r.normal())
	if err != nil {
		return nil, fmt.Errorf("status: %w", err)
	}
	if _, err := Decode(data, r.Team); err != nil {
		return nil, err
	}
	return data, nil
}

func (r Record) check(team paths.TeamID) error {
	switch {
	case r.V != 1:
		return invalid("v is not 1")
	case r.Team != team:
		return invalid("the record is not this team's")
	case !validName(r.Name):
		return invalid("name")
	case r.Origin != OriginV2 && r.Origin != OriginMigration:
		return invalid("origin")
	case r.Seq < 0 || r.WrittenMS < 0:
		return invalid("seq or written_ms is negative")
	case r.ExpiresMS != r.WrittenMS+Lifetime.Milliseconds():
		return invalid("expires_ms is not written_ms plus %s", Lifetime)
	}
	if len(r.Migration) > 0 && string(r.Migration) != "null" && r.Migration[0] != '{' {
		return invalid("migration is neither an object nor null")
	}
	checks := []func() error{r.Homeserver.check, r.checkMembers, r.checkHumans, r.Backup.check, r.Disk.check, r.checkRestore, r.checkLines}
	for _, check := range checks {
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}

func (r Record) checkMembers() error {
	for _, m := range r.Members {
		if _, ok := proto.ParseHandle(m.Handle); !ok {
			return invalid("a member handle")
		}
		if !kindRe.MatchString(m.Kind) || !slices.Contains(roles, m.Role) || !slices.Contains(memberState, m.State) {
			return invalid("member %s", m.Handle)
		}
		if (m.Kind == KindLXC) != (m.Online != nil) {
			return invalid("member %s: online is on an LXC member's entry only, and always there", m.Handle)
		}
		if err := checkRooms(m.Rooms); err != nil {
			return err
		}
	}
	return nil
}

func (r Record) checkHumans() error {
	for _, h := range r.Humans {
		if !proto.ValidHumanName(h.Name) || !slices.Contains(humanState, h.State) || h.Devices < 0 || h.LastSeenMS < 0 {
			return invalid("a human")
		}
		if err := checkRooms(h.Rooms); err != nil {
			return err
		}
	}
	return nil
}

func (r Record) checkRestore() error {
	if r.Restore != nil && (!OneLine(r.Restore.Step) || r.Restore.Backup < 0 || r.Restore.Pre < 0) {
		return invalid("restore")
	}
	return nil
}

func (r Record) checkLines() error {
	for _, p := range r.Problems {
		if !OneLine(p.What) || !OneLine(p.Fix) {
			return invalid("a problem is not two lines of text")
		}
	}
	for _, e := range r.Events {
		if !OneLine(e.What) || e.AtMS < 0 {
			return invalid("an event")
		}
	}
	return nil
}

// normal fills the lists and the migration so they encode as [] and null, never omitted.
func (r Record) normal() Record {
	if r.Members == nil {
		r.Members = []Member{}
	}
	if r.Humans == nil {
		r.Humans = []Human{}
	}
	if r.Problems == nil {
		r.Problems = []Problem{}
	}
	if r.Events == nil {
		r.Events = []Event{}
	}
	if len(r.Migration) == 0 {
		r.Migration = json.RawMessage("null")
	}
	return r
}

// Next is the record a writer starts from after prev (nil for a record missing or unreadable,
// seq 0): seq raised, the times from now, the old events dropped (SR3). The writer then sets
// what it owns.
func Next(prev *Record, team paths.TeamID, now time.Time) Record {
	var next Record
	if prev != nil {
		next = *prev
		next.Events = slices.Clone(prev.Events)
	}
	next.V, next.Team = 1, team
	next.Seq++
	next.WrittenMS = now.UnixMilli()
	next.ExpiresMS = now.Add(Lifetime).UnixMilli()
	next.Events = KeepEvents(next.Events, now)
	return next
}

// KeepEvents is events less those older than EventAge, newest first, at most MaxEvents.
func KeepEvents(events []Event, now time.Time) []Event {
	floor := now.Add(-EventAge).UnixMilli()
	kept := slices.DeleteFunc(slices.Clone(events), func(e Event) bool { return e.AtMS < floor })
	slices.SortStableFunc(kept, func(a, b Event) int {
		switch {
		case a.AtMS > b.AtMS:
			return -1
		case a.AtMS < b.AtMS:
			return 1
		}
		return 0
	})
	if len(kept) > MaxEvents {
		kept = kept[:MaxEvents]
	}
	return kept
}

// Write replaces the record at path, 0640 with group gid (-1 keeps the process's), atomically
// (SR2).
func Write(path string, r Record, gid int) error {
	data, err := r.Encode()
	if err != nil {
		return err
	}
	return state.WriteFile(path, data, FileMode, gid)
}

// Read is team's record at path; found is false when there is none (SR4).
func Read(path string, team paths.TeamID) (Record, bool, error) {
	data, found, err := state.ReadFile(path, MaxSize)
	if err != nil || !found {
		return Record{}, false, err
	}
	rec, err := Decode(data, team)
	if err != nil {
		return Record{}, false, fmt.Errorf("%s: %w", path, err)
	}
	return rec, true, nil
}

// View is what a reader shows of a record: current or not, and every problem to show.
type View struct {
	Record   Record
	Current  bool
	Problems []Problem
}

// The problems a reader adds (SR5).
const (
	FixHealth   = "systemctl status teamchat-health.timer"
	WhatMissing = "no status record yet"
	WhatBroken  = "the status record cannot be read"
)

// Shown is what a reader shows of what Read answered at now: an expired, missing or unreadable
// record is a problem and never current (SR5).
func Shown(r Record, found bool, readErr error, now time.Time) View {
	switch {
	case readErr != nil:
		return View{Problems: []Problem{{What: WhatBroken, Fix: FixHealth}}}
	case !found:
		return View{Problems: []Problem{{What: WhatMissing, Fix: FixHealth}}}
	}
	v := View{Record: r, Current: now.UnixMilli() < r.ExpiresMS}
	if !v.Current {
		since := time.UnixMilli(r.WrittenMS).UTC().Format(time.RFC3339)
		v.Problems = append(v.Problems, Problem{What: "health has not run since " + since, Fix: FixHealth})
	}
	v.Problems = append(v.Problems, r.Problems...)
	return v
}
