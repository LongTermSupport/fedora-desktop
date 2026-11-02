package lxcstate

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	memberstate "github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

func team(t *testing.T, s string) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func box(t *testing.T, s string) paths.ContainerName {
	t.Helper()
	c, err := paths.ParseContainerName(s)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func guest(t *testing.T, s string) paths.GuestUser {
	t.Helper()
	u, err := paths.ParseGuestUser(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// sample holds two seats of one user on one checkout, a second checkout of the container under
// another user, and a left seat elsewhere.
func sample(t *testing.T) Record {
	t.Helper()
	return Record{V: 1, Team: team(t, "ops"), Members: []Entry{
		{Container: box(t, "box-1"), Checkout: "/srv/a", Seat: 1, User: guest(t, "agent"), Handle: "repo.1+desk.lxc", Status: StatusJoined, SinceMS: 1760000000000},
		{Container: box(t, "box-1"), Checkout: "/srv/a", Seat: 2, User: guest(t, "agent"), Handle: "", Status: StatusOffline, SinceMS: 1760000000001, Why: "offline: container stopped"},
		{Container: box(t, "box-1"), Checkout: "/srv/b", Seat: 1, User: guest(t, "bot"), Handle: "", Status: StatusDeferred, SinceMS: 1760000000002, Why: "seat held"},
		{Container: box(t, "box-3"), Checkout: "/srv/a", Seat: 1, User: guest(t, "agent"), Handle: "other.2+desk.lxc", Status: StatusLeft, SinceMS: 1760000000003, Why: "seat still held; token revoked"},
	}}
}

const sampleJSON = `{"v":1,"team":"ops","members":[` +
	`{"container":"box-1","checkout":"/srv/a","seat":1,"user":"agent","handle":"repo.1+desk.lxc","status":"joined","since_ms":1760000000000,"why":""},` +
	`{"container":"box-1","checkout":"/srv/a","seat":2,"user":"agent","handle":"","status":"offline","since_ms":1760000000001,"why":"offline: container stopped"},` +
	`{"container":"box-1","checkout":"/srv/b","seat":1,"user":"bot","handle":"","status":"deferred","since_ms":1760000000002,"why":"seat held"},` +
	`{"container":"box-3","checkout":"/srv/a","seat":1,"user":"agent","handle":"other.2+desk.lxc","status":"left","since_ms":1760000000003,"why":"seat still held; token revoked"}]}`

func TestRecordRoundTrips(t *testing.T) {
	data, err := sample(t).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != sampleJSON {
		t.Fatalf("encoded\n%s\nwant\n%s", data, sampleJSON)
	}
	got, err := DecodeRecord(data, team(t, "ops"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := got.Encode()
	if err != nil || string(again) != sampleJSON {
		t.Fatalf("round trip %s, %v", again, err)
	}
	empty, err := Record{V: 1, Team: team(t, "ops")}.Encode()
	if err != nil || string(empty) != `{"v":1,"team":"ops","members":[]}` {
		t.Fatalf("empty record %s, %v", empty, err)
	}
}

// Encode writes the entries sorted by container, checkout and seat, whatever order they were set
// in; seat 10 sorts after seat 2.
func TestEncodeSorts(t *testing.T) {
	r := sample(t)
	r.Members[0], r.Members[3] = r.Members[3], r.Members[0]
	data, err := r.Encode()
	if err != nil || string(data) != sampleJSON {
		t.Fatalf("encoded %s, %v", data, err)
	}
	if r.Members[0].Container != box(t, "box-3") {
		t.Fatal("Encode reordered the caller's slice")
	}
	ten := sample(t)
	ten.Members[0].Seat = 10
	if got := ten.normal(); got.Members[1].Seat != 10 {
		t.Fatalf("seat 10 sorts as %+v", got.Members)
	}
}

func TestDecodeRecordRefusals(t *testing.T) {
	ops := team(t, "ops")
	swap := func(old, repl string) string { return strings.Replace(sampleJSON, old, repl, 1) }
	for name, doc := range map[string]string{
		"not json":          `{`,
		"unknown key":       swap(`"v":1,`, `"v":1,"x":1,`),
		"unknown entry":     swap(`"why":""}`, `"why":"","x":1}`),
		"duplicate key":     swap(`"v":1,`, `"v":1,"v":1,`),
		"key case":          swap(`"v":1,`, `"V":1,`),
		"missing key":       swap(`,"why":""}`, `}`),
		"missing members":   `{"v":1,"team":"ops"}`,
		"null members":      `{"v":1,"team":"ops","members":null}`,
		"v 2":               swap(`"v":1,`, `"v":2,`),
		"other team":        swap(`"team":"ops"`, `"team":"dev"`),
		"status":            swap(`"status":"joined"`, `"status":"stopped"`),
		"twice":             swap(`"checkout":"/srv/a","seat":2`, `"checkout":"/srv/a","seat":1`),
		"unsorted":          swap(`"container":"box-1","checkout":"/srv/a","seat":1`, `"container":"box-9","checkout":"/srv/a","seat":1`),
		"no seat":           swap(`"seat":2,`, ``),
		"seat zero":         swap(`"seat":2,`, `"seat":0,`),
		"seat too high":     swap(`"seat":2,`, `"seat":10000,`),
		"checkout relative": swap(`"checkout":"/srv/b"`, `"checkout":"srv/b"`),
		"checkout unclean":  swap(`"checkout":"/srv/b"`, `"checkout":"/srv/b/"`),
		"checkout root":     swap(`"checkout":"/srv/b"`, `"checkout":"/"`),
		"checkout lines":    swap(`"checkout":"/srv/b"`, `"checkout":"/srv/b\nc"`),
		"why two lines":     swap(`"why":"seat held"`, `"why":"seat\nheld"`),
		"why tab":           swap(`"why":"seat held"`, `"why":"seat\theld"`),
		"joined with why":   swap(`"since_ms":1760000000000,"why":""`, `"since_ms":1760000000000,"why":"x"`),
		"joined no handle":  swap(`"handle":"repo.1+desk.lxc"`, `"handle":""`),
		"left no handle":    swap(`"handle":"other.2+desk.lxc"`, `"handle":""`),
		"bad handle":        swap(`"handle":"repo.1+desk.lxc"`, `"handle":"Repo"`),
		"probe handle":      swap(`"handle":"repo.1+desk.lxc"`, `"handle":"probe+desk.probe"`),
		"not lxc handle":    swap(`"handle":"repo.1+desk.lxc"`, `"handle":"repo.1+desk.podman"`),
		"live handle twice": swap(`"handle":"","status":"offline"`, `"handle":"repo.1+desk.lxc","status":"offline"`),
		"since zero":        swap(`"since_ms":1760000000000`, `"since_ms":0`),
		"since negative":    swap(`"since_ms":1760000000000`, `"since_ms":-1`),
		"bad container":     swap(`"container":"box-3"`, `"container":"-x"`),
		"bad user":          swap(`"user":"bot"`, `"user":"Bot"`),
		"trailing":          sampleJSON + ` {}`,
		"over the cap":      sampleJSON + strings.Repeat(" ", MaxRecord),
	} {
		if _, err := DecodeRecord([]byte(doc), ops); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
	bad := sample(t)
	bad.Members[0].Status = "stopped"
	if _, err := bad.Encode(); !errors.Is(err, ErrInvalid) {
		t.Errorf("Encode of a bad record: %v", err)
	}
}

func TestFindAndByHandle(t *testing.T) {
	r := sample(t)
	e, ok := r.Find(box(t, "box-1"), "/srv/a", 2)
	if !ok || e.Status != StatusOffline {
		t.Fatalf("Find = %+v, %v", e, ok)
	}
	for _, miss := range []struct {
		c        string
		checkout string
		seat     int
	}{{"box-9", "/srv/a", 1}, {"box-1", "/srv/c", 1}, {"box-1", "/srv/a", 3}} {
		if _, found := r.Find(box(t, miss.c), miss.checkout, miss.seat); found {
			t.Errorf("Find of a missing entry %+v", miss)
		}
	}
	e, ok = r.ByHandle("repo.1+desk.lxc")
	if !ok || e.Seat != 1 || e.Container != box(t, "box-1") {
		t.Fatalf("ByHandle = %+v, %v", e, ok)
	}
	for _, h := range []string{"", "nobody.1+desk.lxc", "other.2+desk.lxc"} {
		if _, ok := r.ByHandle(h); ok {
			t.Errorf("ByHandle(%q) found an entry", h)
		}
	}
}

// C 3.9.3, LX9: a handle is held by at most one entry that is not left; left entries may share
// it, and ByHandle answers the live one (LP1).
func TestALeftEntrySharesAHandleWithALiveOne(t *testing.T) {
	r := sample(t)
	r.Members[3].Handle = "repo.1+desk.lxc"
	r = r.Set(Entry{
		Container: box(t, "box-4"), Checkout: "/srv/a", Seat: 1, User: guest(t, "agent"),
		Handle: "repo.1+desk.lxc", Status: StatusLeft, Why: "moved",
	}, 1770000000000)
	data, err := r.Encode()
	if err != nil {
		t.Fatalf("a left and a joined entry with one handle: %v", err)
	}
	back, err := DecodeRecord(data, team(t, "ops"))
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := back.ByHandle("repo.1+desk.lxc"); !ok || e.Status != StatusJoined {
		t.Fatalf("ByHandle = %+v, %v, want the joined entry", e, ok)
	}
	two := sample(t)
	two.Members[2].Handle, two.Members[2].Status, two.Members[2].Why = "repo.1+desk.lxc", StatusOffline, "x"
	if _, err := two.Encode(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("two live entries with one handle: %v", err)
	}
}

// The record's map from an entry to its seat: the record's team and the entry's checkout and
// seat, so its key is the declared entry's.
func TestSeatOfAnEntry(t *testing.T) {
	r := sample(t)
	id := r.SeatOf(r.Members[1])
	if id != (SeatID{Team: team(t, "ops"), Checkout: "/srv/a", Seat: 2}) ||
		id.Key() != paths.LXCSeatKey(team(t, "ops"), "/srv/a", 2) {
		t.Fatalf("SeatOf = %+v", id)
	}
	if other := (Record{Team: team(t, "dev")}).SeatOf(r.Members[1]); other.Key() == id.Key() {
		t.Fatal("two teams' seats share a key")
	}
}

// since_ms is kept while the status and the handle stay, and moves when either changes.
func TestSetKeepsSinceWhileStatusAndHandleStay(t *testing.T) {
	r := sample(t)
	c, u := box(t, "box-1"), guest(t, "agent")
	same := Entry{Container: c, Checkout: "/srv/a", Seat: 1, User: u, Handle: "repo.1+desk.lxc", Status: StatusJoined, SinceMS: 5}
	if e := find(t, r.Set(same, 1770000000000), same); e.SinceMS != 1760000000000 {
		t.Errorf("unchanged entry: since %d", e.SinceMS)
	}
	offline := same
	offline.Status, offline.Why = StatusOffline, "offline: unit down"
	if e := find(t, r.Set(offline, 1770000000000), same); e.SinceMS != 1770000000000 || e.Why != "offline: unit down" {
		t.Errorf("status changed: %+v", e)
	}
	moved := same
	moved.Handle = "repo.2+desk.lxc"
	if e := find(t, r.Set(moved, 1770000000000), same); e.SinceMS != 1770000000000 {
		t.Errorf("handle changed: since %d", e.SinceMS)
	}
	why := same
	why.Status, why.Why = StatusDeferred, "a"
	r2 := r.Set(why, 1770000000000)
	why.Why = "b"
	if e := find(t, r2.Set(why, 1780000000000), same); e.SinceMS != 1770000000000 || e.Why != "b" {
		t.Errorf("why changed alone: %+v", e)
	}
	if r.Members[0].SinceMS != 1760000000000 {
		t.Fatal("Set changed the record it was called on")
	}
	other := same
	other.User = guest(t, "bot")
	moved2 := r.Set(other, 1770000000000)
	if e := find(t, moved2, same); e.User != other.User || len(moved2.Members) != 4 {
		t.Errorf("a user move replaces the seat's entry: %+v", e)
	}
}

func find(t *testing.T, r Record, at Entry) Entry {
	t.Helper()
	e, ok := r.Find(at.Container, at.Checkout, at.Seat)
	if !ok {
		t.Fatalf("no entry %s %s %d", at.Container, at.Checkout, at.Seat)
	}
	return e
}

func TestSetAddsInOrderAndDropRemoves(t *testing.T) {
	r := sample(t)
	add := Entry{Container: box(t, "box-0"), Checkout: "/srv/a", Seat: 1, User: guest(t, "agent"), Status: StatusDeferred, Why: "x"}
	r = r.Set(add, 1770000000000)
	if r.Members[0].Container != add.Container || r.Members[0].SinceMS != 1770000000000 || len(r.Members) != 5 {
		t.Fatalf("members %+v", r.Members)
	}
	if _, err := r.Encode(); err != nil {
		t.Fatal(err)
	}
	r = r.Drop(add.Container, add.Checkout, add.Seat).Drop(box(t, "box-9"), "/srv/a", 1)
	if data, err := r.Encode(); err != nil || string(data) != sampleJSON {
		t.Fatalf("after Drop %s, %v", data, err)
	}
	if len(sample(t).Drop(box(t, "box-1"), "/srv/a", 2).Members) != 3 {
		t.Fatal("Drop of an entry")
	}
}

func hostRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{paths.StatusRoot, paths.AdminStateRoot} {
		if err := os.MkdirAll(root+dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestReadAndWriteRecord(t *testing.T) {
	root, ops, gid := hostRoot(t), team(t, "ops"), os.Getgid()
	r, found, err := ReadRecord(root, ops)
	if err != nil || found || r.V != 1 || r.Team != ops || len(r.Members) != 0 {
		t.Fatalf("no record: %+v, %v, %v", r, found, err)
	}
	changed, err := WriteRecord(root, sample(t), gid)
	if err != nil || !changed {
		t.Fatalf("first write: %v, %v", changed, err)
	}
	path := root + paths.LXCRecord(ops)
	checkMode(t, path, 0o640, gid)
	checkMode(t, filepath.Dir(path), fs.ModeDir|0o755, -1)
	old := modTime(t, path).Add(-time.Hour)
	if err = os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	changed, err = WriteRecord(root, sample(t), gid)
	if err != nil || changed {
		t.Fatalf("same bytes: %v, %v", changed, err)
	}
	if !modTime(t, path).Equal(old) {
		t.Fatal("an unchanged record was written")
	}
}

func TestWriteRecordWritesAChange(t *testing.T) {
	root, ops := hostRoot(t), team(t, "ops")
	if _, err := WriteRecord(root, sample(t), -1); err != nil {
		t.Fatal(err)
	}
	got, found, err := ReadRecord(root, ops)
	if err != nil || !found || len(got.Members) != 4 {
		t.Fatalf("read back %+v, %v, %v", got, found, err)
	}
	changed, err := WriteRecord(root, got.Drop(box(t, "box-1"), "/srv/b", 1), -1)
	if err != nil || !changed {
		t.Fatalf("a change: %v, %v", changed, err)
	}
	if got, found, err = ReadRecord(root, ops); err != nil || !found || len(got.Members) != 3 {
		t.Fatalf("after the change %+v, %v", got, err)
	}
}

func TestRecordRefusals(t *testing.T) {
	root, ops := hostRoot(t), team(t, "ops")
	if _, err := WriteRecord(root, Record{V: 1, Team: ops, Members: []Entry{{}}}, -1); !errors.Is(err, ErrInvalid) {
		t.Errorf("write of a bad record: %v", err)
	}
	path := root + paths.LXCRecord(ops)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"v":1,"team":"dev","members":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadRecord(root, ops); !errors.Is(err, ErrInvalid) {
		t.Errorf("another team's record: %v", err)
	}
	if _, err := WriteRecord(root, sample(t), -1); !errors.Is(err, ErrInvalid) {
		t.Errorf("write over an unreadable record: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadRecord(root, ops); err == nil {
		t.Error("a link was read")
	}
	if _, err := WriteRecord(t.TempDir(), sample(t), -1); err == nil {
		t.Error("a write with no status root")
	}
}

func opsSeat(t *testing.T) SeatID {
	t.Helper()
	return SeatID{Team: team(t, "ops"), Checkout: "/srv/a", Seat: 1}
}

func sampleCopy(t *testing.T) Copy {
	t.Helper()
	return Copy{
		V: 1, Team: team(t, "ops"), Checkout: "/srv/a", Seat: 1, Generation: 3,
		Seats:    []memberstate.DesiredSeat{{Team: team(t, "ops"), Seat: "1"}},
		Farewell: []memberstate.Farewell{{Team: team(t, "ops"), Seat: "4", Reason: memberstate.FarewellParked}},
	}
}

const sampleCopyJSON = `{"v":1,"team":"ops","checkout":"/srv/a","seat":1,"generation":3,` +
	`"seats":[{"team":"ops","seat":"1"}],"farewell":[{"team":"ops","seat":"4","reason":"parked"}]}`

func TestCopyRoundTripsAndIsPrivate(t *testing.T) {
	root, c, id := hostRoot(t), box(t, "box-1"), opsSeat(t)
	cp, found, err := ReadCopy(root, c, id)
	if err != nil || found || !emptyCopy(cp) || cp.ID() != id {
		t.Fatalf("no copy: %+v, %v, %v", cp, found, err)
	}
	if err = WriteCopy(root, c, sampleCopy(t)); err != nil {
		t.Fatal(err)
	}
	path := root + paths.LXCDesiredCopy(c, id.Key())
	checkMode(t, path, 0o600, -1)
	checkMode(t, filepath.Dir(path), fs.ModeDir|0o700, -1)
	checkMode(t, filepath.Dir(filepath.Dir(path)), fs.ModeDir|0o700, -1)
	data, found, err := state.ReadFile(path, MaxCopy)
	if err != nil || !found {
		t.Fatal(err)
	}
	if string(data) != sampleCopyJSON {
		t.Fatalf("copy\n%s\nwant\n%s", data, sampleCopyJSON)
	}
	got, found, err := ReadCopy(root, c, id)
	if err != nil || !found || got.Generation != 3 || len(got.Seats) != 1 || len(got.Farewell) != 1 || got.ID() != id {
		t.Fatalf("read back %+v, %v, %v", got, found, err)
	}
}

// Two seats of one user and team are two copies, apart.
func TestTwoSeatsOfOneTeamAreTwoCopies(t *testing.T) {
	root, c := hostRoot(t), box(t, "box-1")
	worker := opsSeat(t)
	worker.Seat = 2
	second := sampleCopy(t)
	second.Seat, second.Generation = worker.Seat, 9
	for _, cp := range []Copy{sampleCopy(t), second} {
		if err := WriteCopy(root, c, cp); err != nil {
			t.Fatal(err)
		}
	}
	first, _, err := ReadCopy(root, c, opsSeat(t))
	if err != nil || first.Generation != 3 {
		t.Fatalf("the first seat's copy: %+v, %v", first, err)
	}
	if got, _, err := ReadCopy(root, c, worker); err != nil || got.Generation != 9 {
		t.Fatalf("the second seat's copy: %+v, %v", got, err)
	}
}

func TestCopyRefusals(t *testing.T) {
	root, c, id := hostRoot(t), box(t, "box-1"), opsSeat(t)
	bad := sampleCopy(t)
	bad.Seats = append(bad.Seats, bad.Seats[0])
	if err := WriteCopy(root, c, bad); !errors.Is(err, ErrInvalid) {
		t.Errorf("a team twice: %v", err)
	}
	unkeyed := sampleCopy(t)
	unkeyed.Checkout = "relative"
	if err := WriteCopy(root, c, unkeyed); !errors.Is(err, ErrInvalid) {
		t.Errorf("a copy of no declared seat: %v", err)
	}
	if err := WriteCopy(root, c, sampleCopy(t)); err != nil {
		t.Fatal(err)
	}
	path := root + paths.LXCDesiredCopy(c, id.Key())
	const head = `{"v":1,"team":"ops","checkout":"/srv/a","seat":1,`
	for name, doc := range map[string]string{
		"unknown key":    head + `"generation":3,"seats":[],"farewell":[],"x":1}`,
		"missing key":    head + `"generation":3,"seats":[]}`,
		"no identity":    `{"v":1,"generation":3,"seats":[],"farewell":[]}`,
		"v 2":            `{"v":2,"team":"ops","checkout":"/srv/a","seat":1,"generation":3,"seats":[],"farewell":[]}`,
		"negative":       head + `"generation":-1,"seats":[],"farewell":[]}`,
		"bad seat":       head + `"generation":3,"seats":[{"team":"ops","seat":"x y"}],"farewell":[]}`,
		"bad reason":     head + `"generation":3,"seats":[],"farewell":[{"team":"ops","seat":"4","reason":"gone"}]}`,
		"null seats":     head + `"generation":3,"seats":null,"farewell":[]}`,
		"duplicate key":  `{"v":1,"v":1,"team":"ops","checkout":"/srv/a","seat":1,"generation":3,"seats":[],"farewell":[]}`,
		"another team":   `{"v":1,"team":"dev","checkout":"/srv/a","seat":1,"generation":3,"seats":[],"farewell":[]}`,
		"another repo":   `{"v":1,"team":"ops","checkout":"/srv/b","seat":1,"generation":3,"seats":[],"farewell":[]}`,
		"another seat":   `{"v":1,"team":"ops","checkout":"/srv/a","seat":2,"generation":3,"seats":[],"farewell":[]}`,
		"bad checkout":   `{"v":1,"team":"ops","checkout":"/srv/a/","seat":1,"generation":3,"seats":[],"farewell":[]}`,
		"bad seat no":    `{"v":1,"team":"ops","checkout":"/srv/a","seat":0,"generation":3,"seats":[],"farewell":[]}`,
		"a team's seat":  head + `"generation":3,"seats":[{"team":"dev","seat":"1"}],"farewell":[]}`,
		"a team's leave": head + `"generation":3,"seats":[],"farewell":[{"team":"dev","seat":"4","reason":"parked"}]}`,
	} {
		if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ReadCopy(root, c, id); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
	if err := os.WriteFile(path, []byte(head+`"generation":3,"seats":[],"farewell":[]}`+strings.Repeat(" ", MaxCopy)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadCopy(root, c, id); err == nil {
		t.Error("a copy over the cap was read")
	}
	if _, _, err := ReadCopy(root, c, SeatID{Team: id.Team, Checkout: "relative", Seat: 1}); !errors.Is(err, ErrInvalid) {
		t.Errorf("a read of no declared seat: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadCopy(root, c, id); err == nil {
		t.Error("a link was read")
	}
	if err := WriteCopy(t.TempDir(), c, sampleCopy(t)); err == nil {
		t.Error("a write with no admin state root")
	}
}

// DeleteCopy removes the seat's copy alone, and a copy already gone is no error (the absent
// phase is retried).
func TestDeleteCopy(t *testing.T) {
	root, c := hostRoot(t), box(t, "box-1")
	worker := sampleCopy(t)
	worker.Seat = 2
	for _, cp := range []Copy{sampleCopy(t), worker} {
		if err := WriteCopy(root, c, cp); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := DeleteCopy(root, c, opsSeat(t).Key()); err != nil {
			t.Fatal(err)
		}
	}
	if _, found, err := ReadCopy(root, c, opsSeat(t)); err != nil || found {
		t.Fatalf("after delete: %v, %v", found, err)
	}
	if _, found, err := ReadCopy(root, c, worker.ID()); err != nil || !found {
		t.Fatalf("the other seat's copy: %v, %v", found, err)
	}
	if err := DeleteCopy(root, box(t, "box-9"), worker.ID().Key()); err != nil {
		t.Errorf("a delete in a container with no copies: %v", err)
	}
	if err := DeleteCopy(t.TempDir()+"/file\x00", c, worker.ID().Key()); err == nil {
		t.Error("a delete of an impossible path")
	}
}

// Next raises generation only on a change, whatever order the seats come in, and keeps the
// copy's seat.
func TestNextRaisesGenerationOnlyOnAChange(t *testing.T) {
	cp := sampleCopy(t)
	same, changed := cp.Next(cp.Seats, cp.Farewell)
	if changed || same.Generation != 3 || same.ID() != cp.ID() {
		t.Fatalf("unchanged: %+v, %v", same, changed)
	}
	next, changed := cp.Next(nil, nil)
	if !changed || next.Generation != 4 || len(next.Seats) != 0 || next.Farewell == nil || next.ID() != cp.ID() {
		t.Fatalf("changed: %+v, %v", next, changed)
	}
	if len(cp.Seats) != 1 {
		t.Fatal("Next changed the copy it was called on")
	}
	first, changed := NewCopy(opsSeat(t)).Next(cp.Seats, nil)
	if !changed || first.Generation != 1 || first.V != 1 || first.Seats[0].Team != team(t, "ops") {
		t.Fatalf("first copy: %+v, %v", first, changed)
	}
}

// Next sorts the farewells without sorting the caller's slice.
func TestNextSortsAFreshSlice(t *testing.T) {
	cp := sampleCopy(t)
	farewell := []memberstate.Farewell{
		{Team: team(t, "ops"), Seat: "5", Reason: memberstate.FarewellParked},
		{Team: team(t, "ops"), Seat: "4", Reason: memberstate.FarewellParked},
	}
	next, changed := cp.Next(nil, farewell)
	if !changed || next.Farewell[0].Seat != "4" || farewell[0].Seat != "5" {
		t.Fatalf("next %+v, caller's %+v", next.Farewell, farewell)
	}
}

// Desired is the guest's desired.json: it decodes as member/state's, for the session lxc-<key>
// GU4 checks.
func TestDesiredPassesTheGuestsDecoder(t *testing.T) {
	cp := sampleCopy(t)
	d := cp.Desired(1760000000000)
	data, err := d.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := memberstate.DecodeDesired(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Session != paths.LXCSession(cp.ID().Key()) || got.Inbound != memberstate.InboundAccept || got.Generation != 3 ||
		got.WrittenMS != 1760000000000 || len(got.Seats) != 1 || len(got.Farewell) != 1 {
		t.Fatalf("desired %+v", got)
	}
	empty := NewCopy(opsSeat(t)).Desired(1760000000000)
	if _, err := empty.Encode(); err != nil {
		t.Fatalf("the empty copy's desired: %v", err)
	}
}

func checkMode(t *testing.T, path string, mode fs.FileMode, gid int) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode() != mode {
		t.Errorf("%s: mode %v, want %v", path, info.Mode(), mode)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("no stat")
	}
	if gid != -1 && int(st.Gid) != gid {
		t.Errorf("%s: gid %d, want %d", path, st.Gid, gid)
	}
}

func modTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}

// emptyCopy is whether cp is the copy of no file: v 1, generation 0, and empty lists.
func emptyCopy(cp Copy) bool {
	return cp.V == 1 && cp.Generation == 0 && cp.Seats != nil && len(cp.Seats) == 0 && cp.Farewell != nil && len(cp.Farewell) == 0
}
