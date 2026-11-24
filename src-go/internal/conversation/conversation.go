// Package conversation reads which Claude conversation a recorded session is in, so a restore
// after a reboot and a team restart resume that exact conversation by id, never with
// `--continue` (Plan 00203 SPEC-team-add-and-restart C1-C6, Decision 10).
//
// A session's record is on the system its teams are live on, and Locate is the one place that
// says where (Plan 00209 DESIGN-host-cc-v2.md section 4; rule project_conversation_record):
//
//   - agent-bus (v1), or a launch with no --teams list: the pingbus SessionStart hook
//     (helpers/pingbus/hooks.py) writes the id on every session start, so it follows a /clear, a
//     resume and a compaction: `conversation.json` beside the seat keeper's report
//     (membership.SessionDir), in the session's checkout as the host sees it.
//   - teamchat (v2): the session's link writes `conversation.json` in its state root, the seat
//     store's `sessions/<session>` directory, for a ccy session (bound into its container) and a
//     host cc session alike, decoded by the member's own codec (state.DecodeConversation).
//
// The system is system.Resolve's over the record's --teams list in its checkout, the decision
// the launch took; a list on both systems is refused. There is no fallback from one record to
// the other. Only a launch that carried the agent team bus has a record, so a record whose launch
// did not owns no file, and one left under its reused name is an earlier session's.
package conversation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/discover"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/transcript"
)

// File is the name of a session's conversation record on both systems.
const File = "conversation.json"

// Where is what a host reader needs to find a session's conversation: Claude Code's home, for a
// host session's transcripts, and the environment, which names the owner's seat store.
type Where struct {
	ClaudeHome string
	Getenv     func(string) string
}

// Here is Where for the environment getenv reads.
func Here(getenv func(string) string) Where {
	return Where{ClaudeHome: ClaudeHome(getenv), Getenv: getenv}
}

// Location is a session's conversation record: its path, the system it is on and how its bytes
// give the conversation id.
type Location struct {
	Path   string
	System string
	decode func(data []byte, session string) (entry, error)
}

// Locate is where the session of rec keeps its conversation record, by the system its teams are
// live on: teamchat's in the seat store, agent-bus's (or a launch's with no list) in its
// checkout. A list on both systems is system.One's refusal.
func Locate(rec registry.Record, getenv func(string) string) (Location, error) {
	checkout, err := checkoutOf(rec)
	if err != nil {
		return Location{}, err
	}
	sys, err := systemOf(rec, checkout, getenv)
	if err != nil {
		return Location{}, err
	}
	if sys == system.Teamchat {
		store, storeErr := seatStore(getenv)
		if storeErr != nil {
			return Location{}, storeErr
		}
		name, nameErr := paths.ParseSessionName(rec.Name)
		if nameErr != nil {
			return Location{}, fmt.Errorf("the session name %q is not a teamchat session's: %w", rec.Name, nameErr)
		}
		layout, layoutErr := paths.CCYLayout().WithState(store.Session(name))
		if layoutErr != nil {
			return Location{}, layoutErr
		}
		return Location{Path: layout.ConversationFile(), System: system.Teamchat, decode: decodeV2}, nil
	}
	return Location{Path: filepath.Join(membership.SessionDir(checkout, rec.Name), File), System: system.AgentBus, decode: decodeV1}, nil
}

// systemOf is the one system of the record's --teams list in checkout: agent-bus for no list or
// one a v2 team cannot be in, else system.Resolve's (which refuses a list on both). The declared
// v1 teams are not read: the launch refused a name on both systems, so the status records and
// the checkout's pool decide as they did then.
func systemOf(rec registry.Record, checkout string, getenv func(string) string) (string, error) {
	teams, ok := teamsOf(discover.RecordTeams(rec))
	if !ok {
		return system.AgentBus, nil
	}
	store, err := seatStore(getenv)
	if err != nil {
		return "", err
	}
	lines, err := system.Resolve(context.Background(), teams, system.Sources{
		Status:    system.StatusRecord,
		PoolHolds: poolHolds(store, checkout),
		Declared:  func(context.Context) ([]string, error) { return nil, nil },
	})
	if err != nil {
		return "", fmt.Errorf("which system its teams are on: %w", err)
	}
	return system.One(lines)
}

// poolHolds is system.PoolHolds for checkout with its links resolved, as the launch keyed it; a
// checkout that is gone holds no seat.
func poolHolds(store paths.Store, checkout string) func(paths.TeamID) (bool, error) {
	return func(team paths.TeamID) (bool, error) {
		resolved, err := filepath.EvalSymlinks(checkout)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("the checkout %s: %w", checkout, err)
		}
		key, err := paths.CheckoutKeyOf(resolved)
		if err != nil {
			return false, fmt.Errorf("the checkout key of %s: %w", resolved, err)
		}
		return system.PoolHolds(store, key)(team)
	}
}

// seatStore is the owner's seat store, from HOME and XDG_STATE_HOME.
func seatStore(getenv func(string) string) (paths.Store, error) {
	stateHome, err := paths.StateHome(getenv("HOME"), getenv("XDG_STATE_HOME"))
	if err != nil {
		return paths.Store{}, fmt.Errorf("the state home: %w", err)
	}
	return paths.SeatStore(stateHome)
}

// teamsOf is the team of each item of a --teams list (spaces dropped, as agent-bus drops them),
// and false when the list is empty or an item is not one a v2 team could be.
func teamsOf(list string) ([]string, bool) {
	if list == "" {
		return nil, false
	}
	var teams []string
	for item := range strings.SplitSeq(list, ",") {
		item = strings.Join(strings.Fields(item), "")
		_, team, found := strings.Cut(item, "@")
		if !found {
			team = item
		}
		if _, err := paths.ParseTeamID(team); err != nil {
			return nil, false
		}
		teams = append(teams, team)
	}
	return teams, true
}

// maxBytes bounds what is read; the record is about 150 bytes.
const maxBytes = 4096

// sources are the SessionStart sources the hook names; any other is "other".
var sources = []string{"startup", "resume", "clear", "compact", "other"}

// recordV1 is the hook's record, field for field (SPEC C1).
type recordV1 struct {
	V         int    `json:"v"`
	Session   string `json:"session"`
	SessionID string `json:"session_id"`
	Source    string `json:"source"`
	WrittenMS *int64 `json:"written_ms"`
}

// ClaudeHome is where Claude Code keeps a host session's transcripts (cc's).
func ClaudeHome(getenv func(string) string) string {
	if dir := getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	return getenv("HOME") + "/.claude"
}

// Transcript is the file of the conversation the session of rec is in (For's id, in the
// session's transcript directory), never the newest in a folder several sessions share. Its
// error does not name the session: the caller's line already does.
func Transcript(rec registry.Record, where Where) (string, error) {
	id, err := For(rec, where)
	if err != nil {
		return "", errors.New(strings.TrimPrefix(err.Error(), rec.Name+": "))
	}
	dir, err := transcript.Dir(rec.Prefix, rec.Dir, where.ClaudeHome)
	if err != nil {
		return "", err
	}
	return transcript.For(dir, id)
}

// For is the conversation id the session of rec is to resume: the hook's record, refused
// unless rec's launch carried the bus, the record is exactly the hook's for this session with a
// Claude session id, and that conversation's own transcript is there (never the newest in the
// folder, which several sessions share). Every error names the session and how to resume it
// by hand. where is Here's.
func For(rec registry.Record, where Where) (string, error) {
	id, err := read(rec, where)
	if err != nil {
		return "", byHand(rec, err)
	}
	return id, nil
}

// byHand is a lookup's error for rec, naming the session and how to resume it by hand.
func byHand(rec registry.Record, err error) error {
	return fmt.Errorf("%s: %w; resume it by hand: %s --resume in %s, whose picker lists the conversations",
		rec.Name, err, rec.Prefix, rec.Dir)
}

func read(rec registry.Record, where Where) (string, error) {
	r, err := readRecorded(rec, where)
	return r.id, err
}

// entry is what a session's record gives: the conversation id and when it was written.
type entry struct {
	id        string
	writtenMS int64
}

// readRecorded is read with the record's written_ms.
func readRecorded(rec registry.Record, where Where) (entry, error) {
	if !discover.LaunchedWithBus(rec) {
		return entry{}, errors.New("it was launched without the agent team bus (ccy --no-bus, or a cc without --teams or --bus), so its conversation is not recorded")
	}
	loc, err := Locate(rec, where.Getenv)
	if err != nil {
		return entry{}, err
	}
	data, err := readFile(loc.Path)
	if err != nil {
		return entry{}, err
	}
	r, err := loc.decode(data, rec.Name)
	if err != nil {
		return entry{}, fmt.Errorf("%s: %w", loc.Path, err)
	}
	if !registry.SessionID(r.id) {
		return entry{}, fmt.Errorf("%s holds %q, which is not a Claude session id", loc.Path, r.id)
	}
	dir, err := transcript.Dir(rec.Prefix, rec.Dir, where.ClaudeHome)
	if err != nil {
		return entry{}, err
	}
	if _, err := transcript.For(dir, r.id); err != nil {
		return entry{}, fmt.Errorf("the recorded conversation %s (%s) has no transcript (%w): %w", r.id, loc.Path, errNoTranscript, err)
	}
	return r, nil
}

// decodeV2 is the id in a teamchat session's record. The record is in the session's own
// directory, so the path is the name check.
func decodeV2(data []byte, _ string) (entry, error) {
	r, err := state.DecodeConversation(data)
	if err != nil {
		return entry{}, fmt.Errorf("not the session's teamchat record: %w", errors.Join(err, errNotARecord))
	}
	return entry{id: r.SessionID, writtenMS: r.WrittenMS}, nil
}

// checkoutOf is the checkout the session's hook wrote under: ccy's is its folder (mounted as
// /workspace), cc's the git checkout around its folder (cc-teams.bash takes the top level).
func checkoutOf(rec registry.Record) (string, error) {
	if rec.Prefix != membership.KindCC {
		return rec.Dir, nil
	}
	for dir := filepath.Clean(rec.Dir); ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		if dir == filepath.Dir(dir) {
			return "", fmt.Errorf("%s is not in a git checkout, where cc records its conversation", rec.Dir)
		}
	}
}

// readFile is the record's bytes: a regular file, never a link, bounded.
func readFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w (%s is not there)", errNotRecorded, path)
	}
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", path, err)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err = errors.Join(err, f.Close()); err != nil {
		return nil, fmt.Errorf("could not read %s: %w", path, err)
	}
	if len(data) > maxBytes {
		return nil, fmt.Errorf("%s is larger than a record: %w", path, errNotARecord)
	}
	return data, nil
}

// decodeV1 is the id in the pingbus hook's record, which must name session.
func decodeV1(data []byte, session string) (entry, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var r recordV1
	if err := dec.Decode(&r); err != nil || dec.More() {
		return entry{}, fmt.Errorf("not the SessionStart hook's record: %w", errors.Join(err, errNotARecord))
	}
	if r.V != 1 || r.WrittenMS == nil || *r.WrittenMS < 0 || !slices.Contains(sources, r.Source) {
		return entry{}, errors.New("not a version 1 record of the SessionStart hook")
	}
	if r.Session != session {
		return entry{}, fmt.Errorf("names the session %q, not this one", r.Session)
	}
	return entry{id: r.SessionID, writtenMS: *r.WrittenMS}, nil
}

var errNotARecord = errors.New("not one version 1 record")

// errNotRecorded is For's error when the session's hook has recorded nothing: the file is not
// there. It and errNoTranscript are the two forms of "no conversation" StartPlan starts a record
// marked fresh-if-no-conversation fresh on; a launch without the bus or a file that is not the
// hook's is refused whatever the mark.
var errNotRecorded = errors.New("no conversation is recorded")

// errNoTranscript is For's error when the recorded conversation has no transcript: a session
// that never took a turn, since Claude Code writes none until its first.
var errNoTranscript = errors.New("none is written until the session's first turn")
