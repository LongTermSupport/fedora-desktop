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

func sample(t *testing.T) Record {
	t.Helper()
	return Record{V: 1, Team: team(t, "ops"), Members: []Entry{
		{Container: box(t, "box-1"), User: guest(t, "agent"), Handle: "repo.1+desk.lxc", Status: StatusJoined, SinceMS: 1760000000000},
		{Container: box(t, "box-1"), User: guest(t, "bot"), Handle: "", Status: StatusOffline, SinceMS: 1760000000001, Why: "offline: container stopped"},
		{Container: box(t, "box-2"), User: guest(t, "agent"), Handle: "", Status: StatusDeferred, SinceMS: 1760000000002, Why: "seat held"},
		{Container: box(t, "box-3"), User: guest(t, "agent"), Handle: "other.2+desk.lxc", Status: StatusLeft, SinceMS: 1760000000003, Why: "seat still held; token revoked"},
	}}
}

const sampleJSON = `{"v":1,"team":"ops","members":[` +
	`{"container":"box-1","user":"agent","handle":"repo.1+desk.lxc","status":"joined","since_ms":1760000000000,"why":""},` +
	`{"container":"box-1","user":"bot","handle":"","status":"offline","since_ms":1760000000001,"why":"offline: container stopped"},` +
	`{"container":"box-2","user":"agent","handle":"","status":"deferred","since_ms":1760000000002,"why":"seat held"},` +
	`{"container":"box-3","user":"agent","handle":"other.2+desk.lxc","status":"left","since_ms":1760000000003,"why":"seat still held; token revoked"}]}`

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

// Encode writes the entries sorted by container and user, whatever order they were set in.
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
}

func TestDecodeRecordRefusals(t *testing.T) {
	ops := team(t, "ops")
	swap := func(old, repl string) string { return strings.Replace(sampleJSON, old, repl, 1) }
	for name, doc := range map[string]string{
		"not json":         `{`,
		"unknown key":      swap(`"v":1,`, `"v":1,"x":1,`),
		"unknown entry":    swap(`"why":""}`, `"why":"","x":1}`),
		"duplicate key":    swap(`"v":1,`, `"v":1,"v":1,`),
		"key case":         swap(`"v":1,`, `"V":1,`),
		"missing key":      swap(`,"why":""}`, `}`),
		"missing members":  `{"v":1,"team":"ops"}`,
		"null members":     `{"v":1,"team":"ops","members":null}`,
		"v 2":              swap(`"v":1,`, `"v":2,`),
		"other team":       swap(`"team":"ops"`, `"team":"dev"`),
		"status":           swap(`"status":"joined"`, `"status":"stopped"`),
		"twice":            swap(`"user":"bot"`, `"user":"agent"`),
		"unsorted":         swap(`"container":"box-1","user":"agent"`, `"container":"box-9","user":"agent"`),
		"why two lines":    swap(`"why":"seat held"`, `"why":"seat\nheld"`),
		"why tab":          swap(`"why":"seat held"`, `"why":"seat\theld"`),
		"joined with why":  swap(`"since_ms":1760000000000,"why":""`, `"since_ms":1760000000000,"why":"x"`),
		"joined no handle": swap(`"handle":"repo.1+desk.lxc"`, `"handle":""`),
		"left no handle":   swap(`"handle":"other.2+desk.lxc"`, `"handle":""`),
		"bad handle":       swap(`"handle":"repo.1+desk.lxc"`, `"handle":"Repo"`),
		"probe handle":     swap(`"handle":"repo.1+desk.lxc"`, `"handle":"probe+desk.probe"`),
		"not lxc handle":   swap(`"handle":"repo.1+desk.lxc"`, `"handle":"repo.1+desk.podman"`),
		"handle twice":     swap(`"handle":"other.2+desk.lxc"`, `"handle":"repo.1+desk.lxc"`),
		"since zero":       swap(`"since_ms":1760000000000`, `"since_ms":0`),
		"since negative":   swap(`"since_ms":1760000000000`, `"since_ms":-1`),
		"bad container":    swap(`"container":"box-2"`, `"container":"-x"`),
		"bad user":         swap(`"user":"bot"`, `"user":"Bot"`),
		"trailing":         sampleJSON + ` {}`,
		"over the cap":     sampleJSON + strings.Repeat(" ", MaxRecord),
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
	e, ok := r.Find(box(t, "box-1"), guest(t, "bot"))
	if !ok || e.Status != StatusOffline {
		t.Fatalf("Find = %+v, %v", e, ok)
	}
	if _, missing := r.Find(box(t, "box-9"), guest(t, "bot")); missing {
		t.Fatal("Find of a missing entry")
	}
	e, ok = r.ByHandle("other.2+desk.lxc")
	if !ok || e.Container != box(t, "box-3") {
		t.Fatalf("ByHandle = %+v, %v", e, ok)
	}
	for _, h := range []string{"", "nobody.1+desk.lxc"} {
		if _, ok := r.ByHandle(h); ok {
			t.Errorf("ByHandle(%q) found an entry", h)
		}
	}
}

// since_ms is kept while the status and the handle stay, and moves when either changes.
func TestSetKeepsSinceWhileStatusAndHandleStay(t *testing.T) {
	r := sample(t)
	c, u := box(t, "box-1"), guest(t, "agent")
	same := Entry{Container: c, User: u, Handle: "repo.1+desk.lxc", Status: StatusJoined, SinceMS: 5}
	if e := find(t, r.Set(same, 1770000000000), c, u); e.SinceMS != 1760000000000 {
		t.Errorf("unchanged entry: since %d", e.SinceMS)
	}
	offline := same
	offline.Status, offline.Why = StatusOffline, "offline: unit down"
	if e := find(t, r.Set(offline, 1770000000000), c, u); e.SinceMS != 1770000000000 || e.Why != "offline: unit down" {
		t.Errorf("status changed: %+v", e)
	}
	moved := same
	moved.Handle = "repo.2+desk.lxc"
	if e := find(t, r.Set(moved, 1770000000000), c, u); e.SinceMS != 1770000000000 {
		t.Errorf("handle changed: since %d", e.SinceMS)
	}
	why := same
	why.Status, why.Why = StatusDeferred, "a"
	r2 := r.Set(why, 1770000000000)
	why.Why = "b"
	if e := find(t, r2.Set(why, 1780000000000), c, u); e.SinceMS != 1770000000000 || e.Why != "b" {
		t.Errorf("why changed alone: %+v", e)
	}
	if r.Members[0].SinceMS != 1760000000000 {
		t.Fatal("Set changed the record it was called on")
	}
}

func find(t *testing.T, r Record, c paths.ContainerName, u paths.GuestUser) Entry {
	t.Helper()
	e, ok := r.Find(c, u)
	if !ok {
		t.Fatalf("no entry %s/%s", c, u)
	}
	return e
}

func TestSetAddsInOrderAndDropRemoves(t *testing.T) {
	r := sample(t)
	add := Entry{Container: box(t, "box-0"), User: guest(t, "agent"), Status: StatusDeferred, Why: "x"}
	r = r.Set(add, 1770000000000)
	if r.Members[0].Container != add.Container || r.Members[0].SinceMS != 1770000000000 || len(r.Members) != 5 {
		t.Fatalf("members %+v", r.Members)
	}
	if _, err := r.Encode(); err != nil {
		t.Fatal(err)
	}
	r = r.Drop(add.Container, add.User).Drop(box(t, "box-9"), add.User)
	if data, err := r.Encode(); err != nil || string(data) != sampleJSON {
		t.Fatalf("after Drop %s, %v", data, err)
	}
	if len(sample(t).Drop(box(t, "box-1"), guest(t, "agent")).Members) != 3 {
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
	changed, err := WriteRecord(root, got.Drop(box(t, "box-1"), guest(t, "bot")), -1)
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

func sampleCopy(t *testing.T) Copy {
	t.Helper()
	return Copy{
		V: 1, Generation: 3,
		Seats:    []memberstate.DesiredSeat{{Team: team(t, "dev"), Seat: "2"}, {Team: team(t, "ops"), Seat: "1"}},
		Farewell: []memberstate.Farewell{{Team: team(t, "qa"), Seat: "4", Reason: memberstate.FarewellParked}},
	}
}

func TestCopyRoundTripsAndIsPrivate(t *testing.T) {
	root, c, u := hostRoot(t), box(t, "box-1"), guest(t, "agent")
	cp, found, err := ReadCopy(root, c, u)
	if err != nil || found || !emptyCopy(cp) {
		t.Fatalf("no copy: %+v, %v, %v", cp, found, err)
	}
	if err = WriteCopy(root, c, u, sampleCopy(t)); err != nil {
		t.Fatal(err)
	}
	path := root + paths.LXCDesiredCopy(c, u)
	checkMode(t, path, 0o600, -1)
	checkMode(t, filepath.Dir(path), fs.ModeDir|0o700, -1)
	checkMode(t, filepath.Dir(filepath.Dir(path)), fs.ModeDir|0o700, -1)
	data, found, err := state.ReadFile(path, MaxCopy)
	if err != nil || !found {
		t.Fatal(err)
	}
	const want = `{"v":1,"generation":3,"seats":[{"team":"dev","seat":"2"},{"team":"ops","seat":"1"}],"farewell":[{"team":"qa","seat":"4","reason":"parked"}]}`
	if string(data) != want {
		t.Fatalf("copy\n%s\nwant\n%s", data, want)
	}
	got, found, err := ReadCopy(root, c, u)
	if err != nil || !found || got.Generation != 3 || len(got.Seats) != 2 || len(got.Farewell) != 1 {
		t.Fatalf("read back %+v, %v, %v", got, found, err)
	}
}

func TestCopyRefusals(t *testing.T) {
	root, c, u := hostRoot(t), box(t, "box-1"), guest(t, "agent")
	bad := sampleCopy(t)
	bad.Seats = append(bad.Seats, bad.Seats[0])
	if err := WriteCopy(root, c, u, bad); !errors.Is(err, ErrInvalid) {
		t.Errorf("a team twice: %v", err)
	}
	if err := WriteCopy(root, c, u, sampleCopy(t)); err != nil {
		t.Fatal(err)
	}
	path := root + paths.LXCDesiredCopy(c, u)
	for name, doc := range map[string]string{
		"unknown key":   `{"v":1,"generation":3,"seats":[],"farewell":[],"x":1}`,
		"missing key":   `{"v":1,"generation":3,"seats":[]}`,
		"v 2":           `{"v":2,"generation":3,"seats":[],"farewell":[]}`,
		"negative":      `{"v":1,"generation":-1,"seats":[],"farewell":[]}`,
		"bad seat":      `{"v":1,"generation":3,"seats":[{"team":"ops","seat":"x y"}],"farewell":[]}`,
		"bad reason":    `{"v":1,"generation":3,"seats":[],"farewell":[{"team":"qa","seat":"4","reason":"gone"}]}`,
		"null seats":    `{"v":1,"generation":3,"seats":null,"farewell":[]}`,
		"duplicate key": `{"v":1,"v":1,"generation":3,"seats":[],"farewell":[]}`,
		"unsorted":      `{"v":1,"generation":3,"seats":[{"team":"ops","seat":"1"},{"team":"dev","seat":"2"}],"farewell":[]}`,
	} {
		if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ReadCopy(root, c, u); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want ErrInvalid", name, err)
		}
	}
	if err := os.WriteFile(path, []byte(`{"v":1,"generation":3,"seats":[],"farewell":[]}`+strings.Repeat(" ", MaxCopy)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadCopy(root, c, u); err == nil {
		t.Error("a copy over the cap was read")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadCopy(root, c, u); err == nil {
		t.Error("a link was read")
	}
	if err := WriteCopy(t.TempDir(), c, u, sampleCopy(t)); err == nil {
		t.Error("a write with no admin state root")
	}
}

// Next raises generation only on a change, whatever order the seats come in.
func TestNextRaisesGenerationOnlyOnAChange(t *testing.T) {
	cp := sampleCopy(t)
	reordered := []memberstate.DesiredSeat{cp.Seats[1], cp.Seats[0]}
	same, changed := cp.Next(reordered, cp.Farewell)
	if changed || same.Generation != 3 {
		t.Fatalf("unchanged: %+v, %v", same, changed)
	}
	next, changed := cp.Next(cp.Seats[:1], nil)
	if !changed || next.Generation != 4 || len(next.Seats) != 1 || next.Farewell == nil || len(next.Farewell) != 0 {
		t.Fatalf("changed: %+v, %v", next, changed)
	}
	if len(cp.Seats) != 2 || cp.Seats[0].Team != team(t, "dev") {
		t.Fatal("Next changed the copy it was called on")
	}
	first, changed := (Copy{}).Next(reordered, nil)
	if !changed || first.Generation != 1 || first.V != 1 || first.Seats[0].Team != team(t, "dev") {
		t.Fatalf("first copy: %+v, %v", first, changed)
	}
	if reordered[0].Team != team(t, "ops") {
		t.Fatal("Next sorted the caller's slice")
	}
}

// Desired is the guest's desired.json: it decodes as member/state's, for the session GU4 checks.
func TestDesiredPassesTheGuestsDecoder(t *testing.T) {
	u := guest(t, "agent")
	d := sampleCopy(t).Desired(u, 1760000000000)
	data, err := d.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := memberstate.DecodeDesired(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Session != paths.LXCSession(u) || got.Inbound != memberstate.InboundAccept || got.Generation != 3 ||
		got.WrittenMS != 1760000000000 || len(got.Seats) != 2 || len(got.Farewell) != 1 {
		t.Fatalf("desired %+v", got)
	}
	empty := (Copy{}).Desired(u, 1760000000000)
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
