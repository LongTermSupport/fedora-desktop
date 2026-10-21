package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

func newTeam(t *testing.T) Team {
	t.Helper()
	bundle := t.TempDir()
	return Team{Dir: filepath.Join(bundle, "state")}
}

func readBack(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// expectf fails the test with the message when ok is false.
func expectf(t *testing.T, ok bool, format string, args ...any) {
	t.Helper()
	if !ok {
		t.Errorf(format, args...)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// S16: the state directories are made 0700; one that is not a real directory is refused; a
// read creates nothing.
func TestEnsureDirsMakesTheStateTreeAndRefusesALink(t *testing.T) {
	team := newTeam(t)
	_, found, err := team.SyncToken()
	expectf(t, !found && err == nil, "a read before any write: %v %v", found, err)
	_, err = os.Lstat(team.Dir)
	expectf(t, os.IsNotExist(err), "a read made the state directory: %v", err)
	must(t, team.EnsureDirs())
	for _, dir := range []string{team.Dir, team.inboxDir(), team.consumedDir()} {
		info, err := os.Lstat(dir)
		expectf(t, err == nil && info.IsDir() && info.Mode().Perm() == 0o700, "%s: %v", dir, err)
	}
	other := newTeam(t)
	must(t, os.Symlink(t.TempDir(), other.Dir))
	wantClass(t, "a linked state directory", other.EnsureDirs(), ClassFile)
	orphan := Team{Dir: filepath.Join(t.TempDir(), "no", "state")}
	expectf(t, orphan.EnsureDirs() != nil, "a state directory with no parent was made")
}

const mainItemJSON = `{"type":"m.room.message","event_id":"` + testEvent + `","sender":"@a:b","origin_server_ts":5,"content":{"body":"hi"}}`

func mainItem(id string) InboxItem {
	return InboxItem{EventID: id, Event: pyjson.Ordered{
		{Key: "content", Value: pyjson.Ordered{{Key: "body", Value: "hi"}}},
		{Key: "sender", Value: "@a:b"},
		{Key: "type", Value: "m.room.message"},
		{Key: "event_id", Value: id},
		{Key: "origin_server_ts", Value: num("5")},
		{Key: "unsigned", Value: pyjson.Ordered{}},
	}}
}

// S25: the batch's items are durable before the sync token; a refused batch writes nothing.
func TestCommitBatchStoresItemsThenTheToken(t *testing.T) {
	team := newTeam(t)
	item := mainItem(testEvent)
	_, err := team.CommitBatch([]InboxItem{item}, "a b")
	wantClass(t, "a token with a space", err, ClassGrammar)
	_, err = os.Lstat(team.Dir)
	expectf(t, os.IsNotExist(err), "a refused batch wrote the state directory")
	n, err := team.CommitBatch([]InboxItem{item, item}, "s1_2")
	expectf(t, err == nil && n == 1, "%d %v", n, err)
	expectf(t, readBack(t, team.syncPath()) == `{"v":1,"next_batch":"s1_2"}`, "sync.json")
	got := readBack(t, filepath.Join(team.inboxDir(), testEvent+".json"))
	expectf(t, got == mainItemJSON, "the item %s", got)
	n, err = team.CommitBatch([]InboxItem{item}, "s3")
	expectf(t, err == nil && n == 0, "an item already stored: %d %v", n, err)
	token, found, err := team.SyncToken()
	expectf(t, err == nil && found && token == "s3", "the token %q %v %v", token, found, err)
	forgot, err := team.ForgetSyncToken()
	expectf(t, err == nil && forgot, "forget: %v %v", forgot, err)
	forgot, err = team.ForgetSyncToken()
	expectf(t, err == nil && !forgot, "forget again: %v %v", forgot, err)
}

// S25, S26: an item is refused before anything is written.
func TestCommitBatchRefusesABadItem(t *testing.T) {
	team := newTeam(t)
	cases := map[string]struct {
		item InboxItem
		want Class
	}{
		"a bad event ID":           {InboxItem{EventID: "x", Event: mainItem(testEvent).Event}, ClassGrammar},
		"an event_id not the item": {InboxItem{EventID: testEvent, Event: mainItem("$" + strings.Repeat("d", 43)).Event}, ClassGrammar},
		"a main item's question":   {InboxItem{EventID: testEvent, Event: mainItem(testEvent).Event, Question: &Question{N: 1, Headline: "h"}}, ClassGrammar},
		"a question of n 0":        {InboxItem{EventID: testEvent, Event: mainItem(testEvent).Event, Questions: true, Question: &Question{N: 0, Headline: "h"}}, ClassGrammar},
		"an empty headline":        {InboxItem{EventID: testEvent, Event: mainItem(testEvent).Event, Questions: true, Question: &Question{N: 1}}, ClassGrammar},
	}
	for name, c := range cases {
		_, err := team.CommitBatch([]InboxItem{c.item}, "s")
		wantClass(t, name, err, c.want)
	}
	big := mainItem(testEvent)
	big.Event[0].Value = strings.Repeat("x", maxInboxFileBytes)
	_, err := team.CommitBatch([]InboxItem{big}, "s4")
	wantClass(t, "an item over the cap", err, ClassSize)
	float := mainItem(testEvent)
	float.Event[4].Value = num("1.5")
	_, err = team.CommitBatch([]InboxItem{float}, "s4")
	expectf(t, err != nil && classOf(err) == "", "a float: %v", err)
	_, err = os.Lstat(team.Dir)
	expectf(t, os.IsNotExist(err), "a refused batch wrote the state directory")
}

// S26: the listing skips v1's temporaries, counts a refused file, and consume moves an item.
func TestPendingListsItemsAndCountsRefusals(t *testing.T) {
	team := newTeam(t)
	second := "$" + strings.Repeat("b", 43)
	q := mainItem(second)
	q.Questions, q.Question = true, &Question{N: 2, Headline: "why"}
	empty := mainItem("$" + strings.Repeat("e", 43))
	empty.Questions = true
	_, err := team.CommitBatch([]InboxItem{q, mainItem(testEvent), empty}, "s")
	must(t, err)
	writeRaw(t, filepath.Join(team.inboxDir(), ".x.json.0123456789abcdef.tmp"), "junk")
	writeRaw(t, filepath.Join(team.inboxDir(), "junk.json"), "{}")
	writeRaw(t, filepath.Join(team.inboxDir(), "$"+strings.Repeat("c", 43)+".json"), `{"event_id":"x"}`)
	items, rejected, err := team.Pending()
	expectf(t, err == nil && rejected == 2 && len(items) == 3, "%+v %d %v", items, rejected, err)
	expectf(t, items[0].EventID == testEvent && !items[0].Questions, "first %+v", items[0])
	expectf(t, items[1].Questions && items[1].Question.N == 2, "second %+v", items[1])
	expectf(t, items[2].Questions && items[2].Question == nil, "third %+v", items[2])
	expectf(t, pyjson.Canonical(items[0].Event) == pyjson.Canonical(mustDecode(t, mainItemJSON)), "event %v", items[0].Event)
	known, err := team.KnownEventIDs()
	expectf(t, err == nil && len(known) == 4, "known %v %v", known, err)
	in, err := team.InInbox(testEvent)
	expectf(t, err == nil && in, "in inbox: %v %v", in, err)
	if empty, rejected, err := newTeam(t).Pending(); err != nil || len(empty) != 0 || rejected != 0 {
		t.Errorf("no inbox: %v %d %v", empty, rejected, err)
	}
}

func mustDecode(t *testing.T, text string) any {
	t.Helper()
	v, err := decodeJSON("x", []byte(text))
	must(t, err)
	return v
}

// S26: consume is a rename into consumed/; a missing source is "not pending".
func TestConsumeMovesAnItem(t *testing.T) {
	team := newTeam(t)
	_, err := team.CommitBatch([]InboxItem{mainItem(testEvent)}, "s")
	must(t, err)
	moved, err := team.Consume(testEvent)
	expectf(t, err == nil && moved, "consume: %v %v", moved, err)
	moved, err = team.Consume(testEvent)
	expectf(t, err == nil && !moved, "consume again: %v %v", moved, err)
	in, err := team.InInbox(testEvent)
	expectf(t, err == nil && !in, "in inbox after consume: %v %v", in, err)
	_, err = team.Consume("x")
	wantClass(t, "consume of a bad ID", err, ClassGrammar)
	_, err = team.InInbox("x")
	wantClass(t, "in inbox of a bad ID", err, ClassGrammar)
	known, err := team.KnownEventIDs()
	expectf(t, err == nil && len(known) == 1, "known after consume %v %v", known, err)
}

// S26, SB11: what a stored item may hold.
func TestDecodeItemRefusesWhatV1DoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	base := `"type":"m.room.message","event_id":"` + testEvent + `"`
	cases := map[string]struct {
		text string
		want Class
	}{
		"a list":                 {`[]`, ClassType},
		"an unknown key":         {`{` + base + `,"unsigned":{}}`, ClassUnknownKey},
		"no event_id":            {`{"type":"m"}`, ClassMissingKey},
		"a room alone":           {`{` + base + `,"room":"questions"}`, ClassMissingKey},
		"a question alone":       {`{` + base + `,"question":null}`, ClassMissingKey},
		"the main room":          {`{` + base + `,"room":"main","question":null}`, ClassGrammar},
		"a question list":        {`{` + base + `,"room":"questions","question":[]}`, ClassType},
		"a question with a key":  {`{` + base + `,"room":"questions","question":{"n":1}}`, ClassMissingKey},
		"a question of n true":   {`{` + base + `,"room":"questions","question":{"n":true,"headline":"h"}}`, ClassType},
		"a question of n 0":      {`{` + base + `,"room":"questions","question":{"n":0,"headline":"h"}}`, ClassGrammar},
		"a question no headline": {`{` + base + `,"room":"questions","question":{"n":1,"headline":""}}`, ClassGrammar},
	}
	for name, c := range cases {
		path := filepath.Join(dir, testEvent+".json")
		writeRaw(t, path, c.text)
		_, _, err := decodeItem(path, testEvent)
		wantClass(t, name, err, c.want)
	}
	item, found, err := decodeItem(filepath.Join(dir, "absent.json"), testEvent)
	expectf(t, err == nil && !found && item.EventID == "", "an absent item: %v %v", found, err)
}

// S29, S30, S31: the team record, the untrusted reason and the description replace one another
// as v1's do.
func TestTeamRecordUntrustedAndAbout(t *testing.T) {
	team := newTeam(t)
	_, found, err := team.TeamRecord()
	expectf(t, !found && err == nil, "no record: %v %v", found, err)
	record := pyjson.Ordered{{Key: "v", Value: num("1")}, {Key: "team", Value: "alpha"}}
	must(t, team.SaveTeamRecord(record))
	expectf(t, readBack(t, team.teamPath()) == `{"v":1,"team":"alpha"}`, "team.json")
	desc := "the alpha team"
	must(t, team.SaveAbout(&desc))
	got, found, err := team.About()
	expectf(t, err == nil && found && got == desc, "about %q %v %v", got, found, err)
	must(t, team.SaveSideRooms([]SideRoom{{Purpose: "reports", Room: testRoom, State: "join"}}))
	must(t, team.MarkUntrusted("the record is unsigned"))
	onlyFiles(t, team.Dir, "consumed", "inbox", "untrusted.json")
	reason, found, err := team.UntrustedReason()
	expectf(t, err == nil && found && reason == "the record is unsigned", "reason %q %v %v", reason, found, err)
	must(t, team.SaveTeamRecord(record))
	_, found, err = team.UntrustedReason()
	expectf(t, !found && err == nil, "a saved record keeps untrusted.json: %v %v", found, err)
	back, found, err := team.TeamRecord()
	expectf(t, err == nil && found && pyjson.Canonical(back) == pyjson.Canonical(record), "record %v %v %v", back, found, err)
	must(t, team.SaveAbout(nil))
	must(t, team.SaveAbout(nil))
	_, found, err = team.About()
	expectf(t, !found && err == nil, "about after removal: %v %v", found, err)
}

// S29, S30, S31: what the writers refuse.
func TestTeamRecordUntrustedAndAboutRefusals(t *testing.T) {
	team := newTeam(t)
	bad := "two\nlines"
	wantClass(t, "a bad description", team.SaveAbout(&bad), ClassGrammar)
	wantClass(t, "an empty reason", team.MarkUntrusted(""), ClassGrammar)
	expectf(t, team.SaveTeamRecord(pyjson.Ordered{{Key: "x", Value: num("1.5")}}) != nil, "a record with a float")
	huge := pyjson.Ordered{{Key: "x", Value: strings.Repeat("y", maxTeamFileBytes)}}
	wantClass(t, "a record over the cap", team.SaveTeamRecord(huge), ClassSize)
	must(t, team.EnsureDirs())
	writeRaw(t, team.teamPath(), "[]")
	_, _, err := team.TeamRecord()
	wantClass(t, "a team record that is a list", err, ClassType)
	writeRaw(t, team.untrustedPath(), `{"v":1,"reason":""}`)
	_, _, err = team.UntrustedReason()
	wantClass(t, "an empty stored reason", err, ClassGrammar)
	writeRaw(t, team.aboutPath(), `{"v":2,"description":"d"}`)
	_, _, err = team.About()
	wantClass(t, "about of version 2", err, ClassVersion)
	writeRaw(t, team.syncPath(), `{"v":1,"next_batch":"a b"}`)
	_, _, err = team.SyncToken()
	wantClass(t, "a stored token with a space", err, ClassGrammar)
	writeRaw(t, team.syncPath(), `{"v":1}`)
	_, _, err = team.SyncToken()
	wantClass(t, "sync.json with no token", err, ClassMissingKey)
	writeRaw(t, team.syncPath(), `{"v":1,"next_batch":"a"`)
	_, _, err = team.SyncToken()
	wantClass(t, "sync.json cut short", err, ClassSyntax)
}

// S32: the room view keeps the order it was given.
func TestRoomViewRoundTrip(t *testing.T) {
	team := newTeam(t)
	view, err := team.RoomView()
	expectf(t, err == nil && len(view.Members) == 0, "no view: %v %v", view, err)
	view = RoomView{
		Members:  []RoomMember{{UserID: "@z:s", Membership: "join"}, {UserID: "@a:s", Membership: "invite"}},
		Statuses: []RoomStatus{{UserID: "@z:s", UntilMS: 9}},
	}
	must(t, team.SaveRoomView(view))
	got := readBack(t, team.roomPath())
	expectf(t, got == `{"v":1,"members":{"@z:s":"join","@a:s":"invite"},"statuses":{"@z:s":9}}`, "room.json %s", got)
	back, err := team.RoomView()
	expectf(t, err == nil && len(back.Members) == 2 && back.Members[0].UserID == "@z:s" && back.Statuses[0].UntilMS == 9, "%+v %v", back, err)
	refusals := map[string]RoomView{
		"a leave":        {Members: []RoomMember{{UserID: "@z:s", Membership: "leave"}}},
		"a bad user":     {Members: []RoomMember{{UserID: "z", Membership: "join"}}},
		"a member twice": {Members: []RoomMember{{UserID: "@z:s", Membership: "join"}, {UserID: "@z:s", Membership: "invite"}}},
		"a past status":  {Statuses: []RoomStatus{{UserID: "@z:s", UntilMS: -1}}},
	}
	for name, v := range refusals {
		wantClass(t, name, team.SaveRoomView(v), ClassGrammar)
	}
	many := RoomView{Statuses: make([]RoomStatus, roomMembersMax+1)}
	wantClass(t, "too many statuses", team.SaveRoomView(many), ClassSize)
}

// S32: what a stored room view may hold.
func TestRoomViewRefusesWhatV1DoesNotWrite(t *testing.T) {
	team := newTeam(t)
	must(t, team.EnsureDirs())
	cases := map[string]struct {
		text string
		want Class
	}{
		"members a list":   {`{"v":1,"members":[],"statuses":{}}`, ClassType},
		"statuses a list":  {`{"v":1,"members":{},"statuses":[]}`, ClassType},
		"a leave":          {`{"v":1,"members":{"@a:b":"leave"},"statuses":{}}`, ClassGrammar},
		"a negative until": {`{"v":1,"members":{},"statuses":{"@a:b":-1}}`, ClassGrammar},
		"a bad user":       {`{"v":1,"members":{"a":"join"},"statuses":{}}`, ClassGrammar},
		"an extra key":     {`{"v":1,"members":{},"statuses":{},"x":1}`, ClassUnknownKey},
	}
	for name, c := range cases {
		writeRaw(t, team.roomPath(), c.text)
		_, err := team.RoomView()
		wantClass(t, name, err, c.want)
	}
}

// S33: the side rooms keep the order they were given.
func TestSideRoomsRoundTrip(t *testing.T) {
	team := newTeam(t)
	none, err := team.SideRooms()
	expectf(t, err == nil && len(none) == 0, "no rooms: %v %v", none, err)
	reason := "not invited"
	rooms := []SideRoom{{Purpose: "questions", Room: testRoom, State: "pending", Reason: &reason}, {Purpose: "reports", Room: testRoom, State: "join"}}
	must(t, team.SaveSideRooms(rooms))
	want := `{"v":1,"rooms":{"questions":{"room":"` + testRoom + `","state":"pending","reason":"not invited"},"reports":{"room":"` + testRoom + `","state":"join","reason":null}}}`
	expectf(t, readBack(t, team.sideRoomsPath()) == want, "rooms.json")
	got, err := team.SideRooms()
	expectf(t, err == nil && len(got) == 2 && got[0].Purpose == "questions" && *got[0].Reason == reason && got[1].Reason == nil, "%+v %v", got, err)
	wantClass(t, "a main side room", team.SaveSideRooms([]SideRoom{{Purpose: "main", Room: testRoom, State: "join"}}), ClassGrammar)
	twice := []SideRoom{rooms[1], rooms[1]}
	wantClass(t, "a purpose twice", team.SaveSideRooms(twice), ClassGrammar)
	cases := map[string]struct {
		text string
		want Class
	}{
		"rooms a list":     {`{"v":1,"rooms":[]}`, ClassType},
		"a room a list":    {`{"v":1,"rooms":{"reports":[]}}`, ClassType},
		"a room no reason": {`{"v":1,"rooms":{"reports":{"room":"` + testRoom + `","state":"join"}}}`, ClassMissingKey},
		"a room of 1":      {`{"v":1,"rooms":{"reports":{"room":1,"state":"join","reason":null}}}`, ClassType},
		"a state of 1":     {`{"v":1,"rooms":{"reports":{"room":"` + testRoom + `","state":1,"reason":null}}}`, ClassType},
		"a reason of 1":    {`{"v":1,"rooms":{"reports":{"room":"` + testRoom + `","state":"join","reason":1}}}`, ClassType},
		"a bad state":      {`{"v":1,"rooms":{"reports":{"room":"` + testRoom + `","state":"left","reason":null}}}`, ClassGrammar},
		"three rooms":      {`{"v":1,"rooms":{"a":1,"b":2,"c":3}}`, ClassSize},
	}
	for name, c := range cases {
		writeRaw(t, team.sideRoomsPath(), c.text)
		_, err := team.SideRooms()
		wantClass(t, name, err, c.want)
	}
}

// S35: one tab-separated line per drop, a field that fails its grammar written as `-`.
func TestLogsAppendOneLineEachAndCount(t *testing.T) {
	team := newTeam(t)
	n, err := team.DropCount()
	expectf(t, err == nil && n == 0, "%d %v", n, err)
	must(t, team.LogDrop("alpha", testEvent, "@a:b", "schema", 7))
	must(t, team.LogDrop("alpha", "x\ty", "a\nb", "rate", 8))
	must(t, team.LogIgnored("alpha", testEvent, "@h:b", "not-addressed", 9))
	want := "alpha\t" + testEvent + "\t@a:b\tschema\t7\nalpha\t-\t-\trate\t8\n"
	got := readBack(t, filepath.Join(team.Dir, "dropped.log"))
	expectf(t, got == want, "dropped.log %q", got)
	n, err = team.DropCount()
	expectf(t, err == nil && n == 2, "drops %d %v", n, err)
	n, err = team.IgnoredCount()
	expectf(t, err == nil && n == 1, "ignored %d %v", n, err)
	wantClass(t, "not a drop reason", team.LogDrop("alpha", testEvent, "@a:b", "not-addressed", 7), ClassGrammar)
	wantClass(t, "not an unheard reason", team.LogIgnored("alpha", testEvent, "@a:b", "schema", 7), ClassGrammar)
	wantClass(t, "not a team", team.LogDrop("Alpha", testEvent, "@a:b", "schema", 7), ClassGrammar)
	link := filepath.Join(team.Dir, "ignored.log")
	must(t, os.Remove(link))
	must(t, os.Symlink(filepath.Join(team.Dir, "dropped.log"), link))
	wantClass(t, "a linked log", team.LogIgnored("alpha", testEvent, "@a:b", "not-addressed", 7), ClassFile)
	_, err = team.IgnoredCount()
	wantClass(t, "count of a linked log", err, ClassFile)
	linked := Team{Dir: filepath.Join(t.TempDir(), "state")}
	must(t, os.Symlink(t.TempDir(), linked.Dir))
	wantClass(t, "a log in a linked state directory", linked.LogDrop("alpha", testEvent, "@a:b", "schema", 7), ClassFile)
}

// S34: the forge cache, sorted, with or without repos.
func TestForgeCacheRoundTrip(t *testing.T) {
	team := newTeam(t)
	c, err := team.ForgeCache()
	expectf(t, err == nil && len(c.Entries) == 0, "%v %v", c, err)
	owner := "acme"
	private := false
	cache := ForgeCache{
		Entries: []ForgeEntry{{Key: "z", Expiry: 5}, {Key: "a", Expiry: 0}},
		Repos:   []ForgeRepo{{Key: "r", Expiry: 3, Owner: &owner, Private: &private}},
	}
	must(t, team.SaveForgeCache(cache))
	want := `{"entries": {"a": 0, "z": 5}, "repos": {"r": {"default_branch": null, "exp": 3, "fork": null, "owner": "acme", "private": false}}, "v": 1}`
	got := readBack(t, team.forgeCachePath())
	expectf(t, got == want, "forge-cache.json %s", got)
	back, err := team.ForgeCache()
	expectf(t, err == nil && len(back.Entries) == 2 && len(back.Repos) == 1 && *back.Repos[0].Owner == owner && back.Repos[0].Fork == nil && !*back.Repos[0].Private, "%+v %v", back, err)
	twice := ForgeCache{Entries: []ForgeEntry{{Key: "a"}, {Key: "a"}}}
	wantClass(t, "an entry twice", team.SaveForgeCache(twice), ClassGrammar)
	writeRaw(t, team.forgeCachePath(), `{"v":1,"entries":{"k":1}}`)
	back, err = team.ForgeCache()
	expectf(t, err == nil && len(back.Entries) == 1 && back.Repos == nil, "no repos: %+v %v", back, err)
}

// S34: anything but v1's shape is refused.
func TestForgeCacheRefusesWhatV1DoesNotWrite(t *testing.T) {
	team := newTeam(t)
	must(t, team.EnsureDirs())
	repo := func(body string) string { return `{"v":1,"entries":{},"repos":{"r":` + body + `}}` }
	cases := map[string]struct {
		text string
		want Class
	}{
		"a list":            {`[]`, ClassType},
		"no entries":        {`{"v":1}`, ClassMissingKey},
		"an extra key":      {`{"v":1,"entries":{},"x":1}`, ClassUnknownKey},
		"version 2":         {`{"v":2,"entries":{}}`, ClassVersion},
		"entries a list":    {`{"v":1,"entries":[]}`, ClassType},
		"a negative expiry": {`{"v":1,"entries":{"k":-1}}`, ClassGrammar},
		"repos a list":      {`{"v":1,"entries":{},"repos":[]}`, ClassType},
		"a repo a list":     {repo(`[]`), ClassType},
		"a repo short":      {repo(`{"exp":1}`), ClassMissingKey},
		"a repo exp true":   {repo(`{"exp":true,"owner":null,"private":null,"fork":null,"default_branch":null}`), ClassType},
		"an owner of 1":     {repo(`{"exp":1,"owner":1,"private":null,"fork":null,"default_branch":null}`), ClassType},
		"a branch of 1":     {repo(`{"exp":1,"owner":null,"private":null,"fork":null,"default_branch":1}`), ClassType},
		"private of 1":      {repo(`{"exp":1,"owner":null,"private":1,"fork":null,"default_branch":null}`), ClassType},
		"fork of 1":         {repo(`{"exp":1,"owner":null,"private":null,"fork":1,"default_branch":null}`), ClassType},
	}
	for name, c := range cases {
		writeRaw(t, team.forgeCachePath(), c.text)
		_, err := team.ForgeCache()
		wantClass(t, name, err, c.want)
	}
}

// S16: the writers refuse a state directory that is not a real one.
func TestWritersRefuseALinkedStateDirectory(t *testing.T) {
	team := Team{Dir: filepath.Join(t.TempDir(), "state")}
	must(t, os.Symlink(t.TempDir(), team.Dir))
	desc := "d"
	errs := map[string]error{
		"commit":      errOf(team.CommitBatch(nil, "s")),
		"consume":     errOf(team.Consume(testEvent)),
		"record":      team.SaveTeamRecord(pyjson.Ordered{}),
		"untrusted":   team.MarkUntrusted("r"),
		"about":       team.SaveAbout(&desc),
		"room view":   team.SaveRoomView(RoomView{}),
		"side rooms":  team.SaveSideRooms(nil),
		"forge cache": team.SaveForgeCache(ForgeCache{}),
	}
	for name, err := range errs {
		wantClass(t, name, err, ClassFile)
	}
}
