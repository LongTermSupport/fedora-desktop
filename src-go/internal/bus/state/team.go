package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

const (
	// maxStateFileBytes caps every state file v1 reads through inbox.read_json_file.
	maxStateFileBytes  = 1 << 20
	maxTeamFileBytes   = 65536
	maxInboxFileBytes  = 6 * 65536
	maxForgeCacheBytes = 16 << 20
	maxSyncTokenLen    = 4096
	roomMembersMax     = 4096
	absentField        = "-"
	roomKey            = "room"
	questionKey        = "question"
	questionsRoom      = "questions"
	reasonKey          = "reason"
	droppedLog         = "dropped.log"
	ignoredLog         = "ignored.log"
)

var (
	// storedKeys are the event keys an inbox item keeps, in the order v1 writes them.
	storedKeys      = []string{"type", "event_id", "sender", "origin_server_ts", "content"}
	inboxNamePat    = regexp.MustCompile(`^(\$[A-Za-z0-9_-]{43})\.json$`)
	v1TmpNamePat    = regexp.MustCompile(`^\..+\.[0-9a-f]{16}\.tmp$`)
	roomMemberships = []string{"join", "invite"}
	// sidePurposes are the side rooms' purposes (protocol.PURPOSES); TODO(bus/proto).
	sidePurposes   = []string{"reports", questionsRoom}
	sideRoomStates = []string{"join", "pending", "untrusted", "refused"}
)

// Team is one team's state directory, `<team>/state/` (S9).
type Team struct {
	Dir string
}

func itemPath(dir, eventID string) string { return filepath.Join(dir, eventID+".json") }

// EnsureDirs makes `state/`, `inbox/` and `consumed/` 0700, refusing one that is not a real
// directory (S16). Readers never call it.
func (t Team) EnsureDirs() error {
	for _, dir := range []string{t.Dir, t.inboxDir(), t.consumedDir()} {
		if err := ensureDir(dir); err != nil {
			return err
		}
	}
	return nil
}

func ensureDir(dir string) error {
	if err := os.Mkdir(dir, dirMode); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("make %s: %w", dir, err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return refuse(dir, ClassFile, "not a real directory")
	}
	if err := os.Chmod(dir, dirMode); err != nil {
		return fmt.Errorf("chmod %s: %w", dir, err)
	}
	return nil
}

// field is a key checkKeys has already required (nil when absent).
func field(o pyjson.Ordered, key string) any {
	if v, ok := o.Get(key); ok {
		return v
	}
	return nil
}

// readVersioned is a compact state file's object: an object of exactly `v` and keys, `v` the
// integer 1. found is false when the file is absent.
func readVersioned(path string, keys ...string) (pyjson.Ordered, bool, error) {
	o, found, err := readObject(path, maxStateFileBytes)
	if err != nil || !found {
		return nil, found, err
	}
	if err := checkKeys(path, o, append([]string{"v"}, keys...), nil, unknownFirst); err != nil {
		return nil, true, err
	}
	if err := version(path, field(o, "v")); err != nil {
		return nil, true, err
	}
	return o, true, nil
}

// readObject is a JSON object file, or found false when it is absent.
func readObject(path string, limit int) (pyjson.Ordered, bool, error) {
	raw, found, err := readRegular(path, limit)
	if err != nil || !found {
		return nil, found, err
	}
	v, err := decodeJSON(path, raw)
	if err != nil {
		return nil, true, err
	}
	o, err := object(path, v)
	return o, true, err
}

// saveCompact writes `{"v":1, members...}` compact (S17).
func saveCompact(path string, members ...pyjson.Member) error {
	data, err := pyjson.Encode(append(pyjson.Ordered{{Key: "v", Value: 1}}, members...), pyjson.Compact)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return writeAtomic(path, data)
}

// removeFile removes path, reporting whether it was there.
func removeFile(path string) (bool, error) {
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("remove %s: %w", path, err)
	}
	return true, nil
}

func isSyncToken(s string) bool { return len(s) <= maxSyncTokenLen && isPrintableToken(s) }

// SyncToken is the saved `next_batch` (S25); found is false before the first sync.
func (t Team) SyncToken() (string, bool, error) {
	path := t.syncPath()
	o, found, err := readVersioned(path, "next_batch")
	if err != nil || !found {
		return "", found, err
	}
	token, err := strField(path, "next_batch", field(o, "next_batch"), isSyncToken)
	return token, true, err
}

// ForgetSyncToken removes `sync.json`, so the next sync is a first sync; whether there was one.
func (t Team) ForgetSyncToken() (bool, error) {
	removed, err := removeFile(t.syncPath())
	if err != nil || !removed {
		return removed, err
	}
	return true, fsyncDir(t.Dir)
}

// Question is a questions-room item's numbered question.
type Question struct {
	N        int64
	Headline string
}

// InboxItem is one stored event: its event ID, the event's stored keys and, for a
// questions-room item, its question (nil for null).
type InboxItem struct {
	EventID   string
	Event     pyjson.Ordered
	Questions bool
	Question  *Question
}

func (q *Question) value() any {
	if q == nil {
		return pyjson.Null{}
	}
	return pyjson.Ordered{{Key: "n", Value: q.N}, {Key: "headline", Value: q.Headline}}
}

func isHeadline(s string) bool {
	runes, ok := pyjson.CodePoints(s)
	return ok && len(runes) > 0 && len(runes) <= headlineMax
}

// stored is the item as v1 writes it: the event's stored keys in v1's order, then for a
// questions-room item its room and question.
func (item InboxItem) stored(path string) ([]byte, error) {
	if !isEventID(item.EventID) {
		return nil, refuse(path, ClassGrammar, "not an event ID")
	}
	if field(item.Event, "event_id") != any(item.EventID) {
		return nil, refuse(path, ClassGrammar, "the event's event_id is not the item's")
	}
	out := pyjson.Ordered{}
	for _, k := range storedKeys {
		if v, ok := item.Event.Get(k); ok {
			out = append(out, pyjson.Member{Key: k, Value: v})
		}
	}
	switch q := item.Question; {
	case !item.Questions && q != nil:
		return nil, refuse(path, ClassGrammar, "only a questions-room item carries a question")
	case q != nil && (q.N < 1 || !isHeadline(q.Headline)):
		return nil, refuse(path, ClassGrammar, "a question is a positive n and a 1-120 character headline")
	case item.Questions:
		out = append(out, pyjson.Member{Key: roomKey, Value: questionsRoom}, pyjson.Member{Key: questionKey, Value: q.value()})
	}
	data, err := pyjson.Encode(out, pyjson.Compact)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", path, err)
	}
	if len(data) > maxInboxFileBytes {
		return nil, refuse(path, ClassSize, fmt.Sprintf("larger than %d bytes", maxInboxFileBytes))
	}
	return data, nil
}

// CommitBatch stores a sync batch's items, makes them durable, then saves next as the sync
// token (S25); how many were new. Every item is checked before anything is written.
func (t Team) CommitBatch(items []InboxItem, next string) (int, error) {
	if !isSyncToken(next) {
		return 0, refuse(t.syncPath(), ClassGrammar, "a sync token is 1 to 4096 printable ASCII characters")
	}
	prepared := make([][]byte, len(items))
	for i, item := range items {
		data, err := item.stored(itemPath(t.inboxDir(), item.EventID))
		if err != nil {
			return 0, err
		}
		prepared[i] = data
	}
	if err := t.EnsureDirs(); err != nil {
		return 0, err
	}
	known, err := t.KnownEventIDs()
	if err != nil {
		return 0, err
	}
	written := 0
	for i, item := range items {
		if _, seen := known[item.EventID]; seen {
			continue
		}
		if err := writeAtomic(itemPath(t.inboxDir(), item.EventID), prepared[i]); err != nil {
			return written, err
		}
		known[item.EventID] = struct{}{}
		written++
	}
	return written, saveCompact(t.syncPath(), pyjson.Member{Key: "next_batch", Value: next})
}

// names lists a directory, empty when it is absent.
func names(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", dir, err)
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name()
	}
	return out, nil
}

// KnownEventIDs are the event IDs in the inbox or already consumed.
func (t Team) KnownEventIDs() (map[string]struct{}, error) {
	found := map[string]struct{}{}
	for _, dir := range []string{t.inboxDir(), t.consumedDir()} {
		listed, err := names(dir)
		if err != nil {
			return nil, err
		}
		for _, name := range listed {
			if m := inboxNamePat.FindStringSubmatch(name); m != nil {
				found[m[1]] = struct{}{}
			}
		}
	}
	return found, nil
}

// Pending is every inbox item whose stored form this package accepts, in event ID order, and
// how many stored files it refused (S26); v1's temporaries are skipped. Whether an item's event
// is acceptable, and the delivery order by origin_server_ts, are the caller's (bus/proto).
func (t Team) Pending() ([]InboxItem, int, error) {
	listed, err := names(t.inboxDir())
	if err != nil {
		return nil, 0, err
	}
	sort.Strings(listed)
	var items []InboxItem
	rejected := 0
	for _, name := range listed {
		if v1TmpNamePat.MatchString(name) {
			continue
		}
		m := inboxNamePat.FindStringSubmatch(name)
		if m == nil {
			rejected++
			continue
		}
		item, found, err := decodeItem(filepath.Join(t.inboxDir(), name), m[1])
		var refused *Error
		switch {
		case errors.As(err, &refused):
			rejected++
		case err != nil:
			return nil, 0, err
		case found:
			items = append(items, item)
		}
	}
	return items, rejected, nil
}

// decodeItem reads one stored item (S26, SB11); found is false when it went away meanwhile.
func decodeItem(path, eventID string) (InboxItem, bool, error) {
	o, found, err := readObject(path, maxInboxFileBytes)
	if err != nil || !found {
		return InboxItem{}, found, err
	}
	allowed := append(slices.Clone(storedKeys), roomKey, questionKey)
	if err := checkKeys(path, o, []string{"event_id"}, allowed, unknownFirst); err != nil {
		return InboxItem{}, true, err
	}
	if field(o, "event_id") != any(eventID) {
		return InboxItem{}, true, refuse(path, ClassGrammar, "event_id is not the file's")
	}
	item := InboxItem{EventID: eventID}
	room, hasRoom := o.Get(roomKey)
	q, hasQuestion := o.Get(questionKey)
	switch {
	case hasRoom != hasQuestion:
		return InboxItem{}, true, refuse(path, ClassMissingKey, "room and question come together")
	case hasRoom && room != any(questionsRoom):
		return InboxItem{}, true, refuse(path, ClassGrammar, "a stored item's room is the questions room")
	case hasRoom:
		question, set, err := decodeQuestion(path, q)
		if err != nil {
			return InboxItem{}, true, err
		}
		item.Questions, item.Question = true, ptr(question, set)
	}
	for _, m := range o {
		if m.Key != roomKey && m.Key != questionKey {
			item.Event = append(item.Event, m)
		}
	}
	return item, true, nil
}

// decodeQuestion is a stored question; set is false for null.
func decodeQuestion(path string, v any) (Question, bool, error) {
	if _, isNull := v.(pyjson.Null); isNull {
		return Question{}, false, nil
	}
	o, err := object(path, v)
	if err != nil {
		return Question{}, false, err
	}
	if err = checkKeys(path, o, []string{"n", "headline"}, nil, unknownFirst); err != nil {
		return Question{}, false, err
	}
	n, err := intField(path, "n", field(o, "n"), 1)
	if err != nil {
		return Question{}, false, err
	}
	headline, err := strField(path, "headline", field(o, "headline"), isHeadline)
	if err != nil {
		return Question{}, false, err
	}
	return Question{N: n, Headline: headline}, true, nil
}

// InInbox is whether the item is still in the inbox.
func (t Team) InInbox(eventID string) (bool, error) {
	if !isEventID(eventID) {
		return false, refuse(t.inboxDir(), ClassGrammar, "not an event ID")
	}
	_, err := os.Lstat(itemPath(t.inboxDir(), eventID))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", eventID, err)
	}
	return true, nil
}

// Consume moves an item into `consumed/`, both directories fsynced; false when it was not
// pending.
func (t Team) Consume(eventID string) (bool, error) {
	if !isEventID(eventID) {
		return false, refuse(t.inboxDir(), ClassGrammar, "not an event ID")
	}
	if err := t.EnsureDirs(); err != nil {
		return false, err
	}
	err := os.Rename(itemPath(t.inboxDir(), eventID), itemPath(t.consumedDir(), eventID))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("consume %s: %w", eventID, err)
	}
	return true, errors.Join(fsyncDir(t.consumedDir()), fsyncDir(t.inboxDir()))
}

func (t Team) inboxDir() string       { return filepath.Join(t.Dir, "inbox") }
func (t Team) consumedDir() string    { return filepath.Join(t.Dir, "consumed") }
func (t Team) syncPath() string       { return filepath.Join(t.Dir, "sync.json") }
func (t Team) teamPath() string       { return filepath.Join(t.Dir, "team.json") }
func (t Team) untrustedPath() string  { return filepath.Join(t.Dir, "untrusted.json") }
func (t Team) aboutPath() string      { return filepath.Join(t.Dir, "about.json") }
func (t Team) roomPath() string       { return filepath.Join(t.Dir, "room.json") }
func (t Team) sideRoomsPath() string  { return filepath.Join(t.Dir, "rooms.json") }
func (t Team) forgeCachePath() string { return filepath.Join(t.Dir, "forge-cache.json") }

// removeFiles removes each path that is there, then makes the directory durable once.
func (t Team) removeFiles(paths ...string) error {
	for _, path := range paths {
		if _, err := removeFile(path); err != nil {
			return err
		}
	}
	return fsyncDir(t.Dir)
}
