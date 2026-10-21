package state

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// TeamRecord is `team.json`'s object (S29, SB10); found is false when the team is not joined.
// Its meaning is bus/proto's parse_team_record.
func (t Team) TeamRecord() (pyjson.Ordered, bool, error) {
	return readObject(t.teamPath(), maxTeamFileBytes)
}

// SaveTeamRecord replaces `team.json` with the record the syncer verified; the room is trusted
// again, so `untrusted.json` goes.
func (t Team) SaveTeamRecord(record pyjson.Ordered) error {
	path := t.teamPath()
	data, err := pyjson.Encode(record, pyjson.Compact)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if len(data) > maxTeamFileBytes {
		return refuse(path, ClassSize, fmt.Sprintf("larger than %d bytes", maxTeamFileBytes))
	}
	if err := t.EnsureDirs(); err != nil {
		return err
	}
	if err := writeAtomic(path, data); err != nil {
		return err
	}
	return t.removeFiles(t.untrustedPath())
}

// UntrustedReason is why the room is not trusted (S30); found is false when it is.
func (t Team) UntrustedReason() (string, bool, error) {
	path := t.untrustedPath()
	o, found, err := readVersioned(path, reasonKey)
	if err != nil || !found {
		return "", found, err
	}
	reason, err := strField(path, reasonKey, field(o, reasonKey), isReason)
	return reason, true, err
}

// MarkUntrusted records why the room stopped being trusted and removes `team.json`,
// `about.json` and `rooms.json`.
func (t Team) MarkUntrusted(reason string) error {
	path := t.untrustedPath()
	if !isReason(reason) {
		return refuse(path, ClassGrammar, "a reason is 1 to 200 printable ASCII characters")
	}
	if err := t.EnsureDirs(); err != nil {
		return err
	}
	if err := saveCompact(path, pyjson.Member{Key: reasonKey, Value: reason}); err != nil {
		return err
	}
	return t.removeFiles(t.teamPath(), t.aboutPath(), t.sideRoomsPath())
}

// About is the team's cached description (S31); found is false when it has none.
func (t Team) About() (string, bool, error) {
	path := t.aboutPath()
	o, found, err := readVersioned(path, "description")
	if err != nil || !found {
		return "", found, err
	}
	description, err := strField(path, "description", field(o, "description"), isDescription)
	return description, true, err
}

// SaveAbout replaces `about.json`, or removes it for nil.
func (t Team) SaveAbout(description *string) error {
	path := t.aboutPath()
	if description == nil {
		if _, err := removeFile(path); err != nil {
			return err
		}
		return nil
	}
	if !isDescription(*description) {
		return refuse(path, ClassGrammar, "a description is 1 to 120 characters, one line")
	}
	if err := t.EnsureDirs(); err != nil {
		return err
	}
	return saveCompact(path, pyjson.Member{Key: "description", Value: *description})
}

// RoomMember is one joined or invited member of the main room.
type RoomMember struct {
	UserID     string
	Membership string
}

// RoomStatus is one member's accepted `listening` status, until UntilMS.
type RoomStatus struct {
	UserID  string
	UntilMS int64
}

// RoomView is `room.json` (S32), in the file's order.
type RoomView struct {
	Members  []RoomMember
	Statuses []RoomStatus
}

func isMembership(s string) bool { return slices.Contains(roomMemberships, s) }

// RoomView is the saved view, empty before the first save.
func (t Team) RoomView() (RoomView, error) {
	path := t.roomPath()
	o, found, err := readVersioned(path, "members", "statuses")
	if err != nil || !found {
		return RoomView{}, err
	}
	members, err := mapField(path, "members", field(o, "members"), roomMembersMax)
	if err != nil {
		return RoomView{}, err
	}
	statuses, err := mapField(path, "statuses", field(o, "statuses"), roomMembersMax)
	if err != nil {
		return RoomView{}, err
	}
	var view RoomView
	for _, m := range members {
		membership, err := strField(path, "a membership", m.Value, isMembership)
		if err != nil {
			return RoomView{}, err
		}
		view.Members = append(view.Members, RoomMember{UserID: m.Key, Membership: membership})
	}
	for _, m := range statuses {
		until, err := intField(path, "a status", m.Value, 0)
		if err != nil {
			return RoomView{}, err
		}
		view.Statuses = append(view.Statuses, RoomStatus{UserID: m.Key, UntilMS: until})
	}
	return view, checkRoomUsers(path, view)
}

func checkRoomUsers(path string, view RoomView) error {
	members := make([]string, 0, len(view.Members))
	for _, m := range view.Members {
		members = append(members, m.UserID)
	}
	statuses := make([]string, 0, len(view.Statuses))
	for _, s := range view.Statuses {
		statuses = append(statuses, s.UserID)
	}
	for _, ids := range [][]string{members, statuses} {
		if len(ids) > roomMembersMax {
			return refuse(path, ClassSize, fmt.Sprintf("more than %d members", roomMembersMax))
		}
		if !distinct(ids) || slices.ContainsFunc(ids, func(id string) bool { return !isUserIDText(id) }) {
			return refuse(path, ClassGrammar, "a member is not a distinct user ID")
		}
	}
	return nil
}

func distinct(items []string) bool {
	seen := make(map[string]struct{}, len(items))
	for _, s := range items {
		if _, dup := seen[s]; dup {
			return false
		}
		seen[s] = struct{}{}
	}
	return true
}

// SaveRoomView replaces `room.json`.
func (t Team) SaveRoomView(view RoomView) error {
	path := t.roomPath()
	if err := checkRoomUsers(path, view); err != nil {
		return err
	}
	members, statuses := pyjson.Ordered{}, pyjson.Ordered{}
	for _, m := range view.Members {
		if !isMembership(m.Membership) {
			return refuse(path, ClassGrammar, "a membership is join or invite")
		}
		members = append(members, pyjson.Member{Key: m.UserID, Value: m.Membership})
	}
	for _, s := range view.Statuses {
		if s.UntilMS < 0 {
			return refuse(path, ClassGrammar, "a status is a time")
		}
		statuses = append(statuses, pyjson.Member{Key: s.UserID, Value: s.UntilMS})
	}
	if err := t.EnsureDirs(); err != nil {
		return err
	}
	return saveCompact(path, pyjson.Member{Key: "members", Value: members}, pyjson.Member{Key: "statuses", Value: statuses})
}

// SideRoom is one side room the syncer met (S33).
type SideRoom struct {
	Purpose string
	Room    string
	State   string
	Reason  *string
}

func checkSideRoom(path string, r SideRoom) error {
	if !slices.Contains(sidePurposes, r.Purpose) || !isRoomID(r.Room) ||
		!slices.Contains(sideRoomStates, r.State) || (r.Reason != nil && !isReason(*r.Reason)) {
		return refuse(path, ClassGrammar, "a malformed side room")
	}
	return nil
}

func anyText(string) bool { return true }

// SideRooms are the side rooms in the file's order, none before the first save.
func (t Team) SideRooms() ([]SideRoom, error) {
	path := t.sideRoomsPath()
	o, found, err := readVersioned(path, "rooms")
	if err != nil || !found {
		return nil, err
	}
	rooms, err := mapField(path, "rooms", field(o, "rooms"), len(sidePurposes))
	if err != nil {
		return nil, err
	}
	out := make([]SideRoom, 0, len(rooms))
	for _, m := range rooms {
		r, err := decodeSideRoom(path, m)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func decodeSideRoom(path string, m pyjson.Member) (SideRoom, error) {
	entry, err := object(path, m.Value)
	if err != nil {
		return SideRoom{}, err
	}
	if err = checkKeys(path, entry, []string{roomKey, "state", reasonKey}, nil, unknownFirst); err != nil {
		return SideRoom{}, err
	}
	room, err := strField(path, roomKey, field(entry, roomKey), anyText)
	if err != nil {
		return SideRoom{}, err
	}
	state, err := strField(path, "state", field(entry, "state"), anyText)
	if err != nil {
		return SideRoom{}, err
	}
	reason, set, err := nullableStr(path, reasonKey, field(entry, reasonKey), anyText)
	if err != nil {
		return SideRoom{}, err
	}
	r := SideRoom{Purpose: m.Key, Room: room, State: state, Reason: ptr(reason, set)}
	return r, checkSideRoom(path, r)
}

// SaveSideRooms replaces `rooms.json`, keyed by purpose in the order given.
func (t Team) SaveSideRooms(rooms []SideRoom) error {
	path := t.sideRoomsPath()
	keyed := pyjson.Ordered{}
	purposes := make([]string, 0, len(rooms))
	for _, r := range rooms {
		if err := checkSideRoom(path, r); err != nil {
			return err
		}
		purposes = append(purposes, r.Purpose)
		keyed = append(keyed, pyjson.Member{Key: r.Purpose, Value: pyjson.Ordered{
			{Key: roomKey, Value: r.Room}, {Key: "state", Value: r.State}, {Key: reasonKey, Value: nullable(r.Reason)},
		}})
	}
	if !distinct(purposes) {
		return refuse(path, ClassGrammar, "a purpose named twice")
	}
	if err := t.EnsureDirs(); err != nil {
		return err
	}
	return saveCompact(path, pyjson.Member{Key: "rooms", Value: keyed})
}

// isLogUser is inbox._LOG_USER_RE: `@` and 1 to 254 printable ASCII characters.
func isLogUser(s string) bool {
	return len(s) >= 2 && len(s) <= userIDTextMax && s[0] == '@' && isPrintableToken(s[1:])
}

// LogDrop appends one `dropped.log` line (S35).
func (t Team) LogDrop(team, eventID, sender, reason string, nowMS int64) error {
	if !isDropReason(reason) {
		return refuse(filepath.Join(t.Dir, droppedLog), ClassGrammar, "not a drop reason")
	}
	return t.appendLog(droppedLog, team, eventID, sender, reason, nowMS)
}

// LogIgnored appends one `ignored.log` line for a human's message that reached no agent.
func (t Team) LogIgnored(team, eventID, sender, reason string, nowMS int64) error {
	if !isUnheardReason(reason) {
		return refuse(filepath.Join(t.Dir, ignoredLog), ClassGrammar, "not an unheard reason")
	}
	return t.appendLog(ignoredLog, team, eventID, sender, reason, nowMS)
}

// DropCount is how many lines `dropped.log` holds.
func (t Team) DropCount() (int, error) { return countLines(filepath.Join(t.Dir, droppedLog)) }

// IgnoredCount is how many lines `ignored.log` holds.
func (t Team) IgnoredCount() (int, error) { return countLines(filepath.Join(t.Dir, ignoredLog)) }

// appendLog writes `team\tevent_id\tsender\treason\tms\n`, an event ID or sender that fails its
// grammar written `-`, so nothing an event chose can add a field or a line.
func (t Team) appendLog(name, team, eventID, sender, reason string, nowMS int64) error {
	path := filepath.Join(t.Dir, name)
	if !isTeamName(team) {
		return refuse(path, ClassGrammar, "not a team name")
	}
	if !isEventID(eventID) {
		eventID = absentField
	}
	if !isLogUser(sender) {
		sender = absentField
	}
	if err := t.EnsureDirs(); err != nil {
		return err
	}
	file, err := openRegular(path, unix.O_WRONLY|unix.O_CREAT|unix.O_APPEND)
	if err != nil {
		return err
	}
	line := strings.Join([]string{team, eventID, sender, reason, strconv.FormatInt(nowMS, 10)}, "\t") + "\n"
	_, err = file.WriteString(line)
	return errors.Join(err, file.Close())
}

func countLines(path string) (int, error) {
	file, err := openRegular(path, unix.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer closeQuietly(file)
	count := 0
	buf := make([]byte, 1<<16)
	for {
		n, err := file.Read(buf)
		count += bytes.Count(buf[:n], []byte{'\n'})
		if errors.Is(err, io.EOF) {
			return count, nil
		}
		if err != nil {
			return 0, fmt.Errorf("read %s: %w", path, err)
		}
	}
}

// ForgeEntry is one positive forge answer and its expiry (Unix seconds).
type ForgeEntry struct {
	Key    string
	Expiry int64
}

// ForgeRepo is one repository's kept metadata answer and its expiry.
type ForgeRepo struct {
	Key           string
	Expiry        int64
	Owner         *string
	Private       *bool
	Fork          *bool
	DefaultBranch *string
}

// ForgeCache is `forge-cache.json` (S34), in the file's (sorted) order.
type ForgeCache struct {
	Entries []ForgeEntry
	Repos   []ForgeRepo
}

// ForgeCache is the saved cache, empty when there is none.
func (t Team) ForgeCache() (ForgeCache, error) {
	path := t.forgeCachePath()
	o, found, err := readObject(path, maxForgeCacheBytes)
	if err != nil || !found {
		return ForgeCache{}, err
	}
	if err = checkKeys(path, o, []string{"v", "entries"}, []string{"repos"}, unknownFirst); err != nil {
		return ForgeCache{}, err
	}
	if err = version(path, field(o, "v")); err != nil {
		return ForgeCache{}, err
	}
	entries, err := mapField(path, "entries", field(o, "entries"), maxForgeCacheBytes)
	if err != nil {
		return ForgeCache{}, err
	}
	var cache ForgeCache
	for _, m := range entries {
		var exp int64
		if exp, err = intField(path, "an expiry", m.Value, 0); err != nil {
			return ForgeCache{}, err
		}
		cache.Entries = append(cache.Entries, ForgeEntry{Key: m.Key, Expiry: exp})
	}
	if repos, ok := o.Get("repos"); ok {
		cache.Repos, err = decodeRepos(path, repos)
	}
	return cache, err
}

func decodeRepos(path string, v any) ([]ForgeRepo, error) {
	repos, err := mapField(path, "repos", v, maxForgeCacheBytes)
	if err != nil {
		return nil, err
	}
	out := make([]ForgeRepo, 0, len(repos))
	for _, m := range repos {
		repo, err := decodeRepo(path, m)
		if err != nil {
			return nil, err
		}
		out = append(out, repo)
	}
	return out, nil
}

func decodeRepo(path string, m pyjson.Member) (ForgeRepo, error) {
	o, err := object(path, m.Value)
	if err != nil {
		return ForgeRepo{}, err
	}
	if err = checkKeys(path, o, []string{"exp", "owner", "private", "fork", "default_branch"}, nil, unknownFirst); err != nil {
		return ForgeRepo{}, err
	}
	repo := ForgeRepo{Key: m.Key}
	if repo.Expiry, err = intField(path, "exp", field(o, "exp"), 0); err != nil {
		return ForgeRepo{}, err
	}
	owner, ownerSet, err := nullableStr(path, "owner", field(o, "owner"), anyText)
	if err != nil {
		return ForgeRepo{}, err
	}
	branch, branchSet, err := nullableStr(path, "default_branch", field(o, "default_branch"), anyText)
	if err != nil {
		return ForgeRepo{}, err
	}
	private, privateSet, err := nullableBool(path, "private", field(o, "private"))
	if err != nil {
		return ForgeRepo{}, err
	}
	fork, forkSet, err := nullableBool(path, "fork", field(o, "fork"))
	if err != nil {
		return ForgeRepo{}, err
	}
	repo.Owner, repo.DefaultBranch = ptr(owner, ownerSet), ptr(branch, branchSet)
	repo.Private, repo.Fork = ptr(private, privateSet), ptr(fork, forkSet)
	return repo, nil
}

// nullableBool is v as null (set false) or a bool.
func nullableBool(path, name string, v any) (bool, bool, error) {
	if _, isNull := v.(pyjson.Null); isNull {
		return false, false, nil
	}
	b, err := boolField(path, name, v)
	return b, err == nil, err
}

func boolOrNull(b *bool) any {
	if b == nil {
		return pyjson.Null{}
	}
	return *b
}

// SaveForgeCache replaces `forge-cache.json`, sorted with no newline (S17), `repos` always
// written, as v1 writes it.
func (t Team) SaveForgeCache(cache ForgeCache) error {
	path := t.forgeCachePath()
	entries, repos := pyjson.Object{}, pyjson.Object{}
	for _, e := range cache.Entries {
		entries[e.Key] = e.Expiry
	}
	for _, r := range cache.Repos {
		repos[r.Key] = pyjson.Object{
			"exp": r.Expiry, "owner": nullable(r.Owner), "private": boolOrNull(r.Private),
			"fork": boolOrNull(r.Fork), "default_branch": nullable(r.DefaultBranch),
		}
	}
	if len(entries) != len(cache.Entries) || len(repos) != len(cache.Repos) {
		return refuse(path, ClassGrammar, "a key given twice")
	}
	data, err := pyjson.Encode(pyjson.Object{"v": 1, "entries": entries, "repos": repos}, pyjson.SortedBare)
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := t.EnsureDirs(); err != nil {
		return err
	}
	return writeAtomic(path, data)
}
