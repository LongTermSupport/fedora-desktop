package v1

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// populatedWorld is a world and the main room's events.
type populatedWorld struct {
	w      *world
	events []json.RawMessage
}

// populatedWith is a world whose main room holds 230 events of several kinds, a reports room
// with one report and no questions room.
func populatedWith(t *testing.T) populatedWorld {
	t.Helper()
	w := newWorld(t)
	events := textEvents(t, 228)
	events = append(events,
		ev(t, map[string]any{
			"event_id": eventID(900), "type": "agent_bus.status", "sender": agent, "origin_server_ts": fixed.UnixMilli(),
			"state_key": agent, "content": map[string]any{"v": 1, "state": "listening", "until": 1.5e12},
		}),
		json.RawMessage(`{"event_id":"$big","type":"m.room.message","sender":"@a:x","origin_server_ts":12345678901234567,"content":{"body":"n","n":1.000000000000000000001,"esc":"`+escapedAsSent+`"}}`),
	)
	w.hs.addRoom(roomID('m'), events, `[{"type":"m.room.create","state_key":"","content":{}}]`)
	w.write(w.teamDir()+"/room_id.reports", roomID('r'), 0o600)
	report := msgEvent(t, 1, agent, map[string]any{"msgtype": "m.notice", "body": "the report", "agent_bus.report": map[string]any{"v": 1}})
	w.hs.addRoom(roomID('r'), []json.RawMessage{report}, `[]`)
	return populatedWorld{w: w, events: events}
}

func populated(t *testing.T) *world {
	t.Helper()
	return populatedWith(t).w
}

// escapedAsSent is a JSON string's escapes as the server sent them (an e acute and a slash), which
// the export keeps; built from parts so no editor folds the escape.
const escapedAsSent = "\\" + "u00e9" + "\\/"

// E7, E10-E16, E19: a whole export.
func TestExportWritesTheRecord(t *testing.T) {
	p := populatedWith(t)
	w := p.w
	res, err := w.export(Request{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Record != w.recordDir() || res.Copy != "" {
		t.Fatalf("%+v", res)
	}
	wantRooms := []RoomCount{{"main", roomID('m'), 230}, {"reports", roomID('r'), 1}}
	if !slices.Equal(res.Rooms, wantRooms) {
		t.Errorf("rooms %+v", res.Rooms)
	}
	names := checkRecordFiles(t, w, res.Record)
	checkJSONL(t, res.Record+"/events-main.jsonl", p.events)
	// E14: the state as sent.
	if got := read(t, res.Record+"/state-main.json"); got != `[{"type":"m.room.create","state_key":"","content":{}}]` {
		t.Errorf("state %s", got)
	}
	// E15: history.md holds both rooms.
	hist := read(t, res.Record+"/history.md")
	if !strings.Contains(hist, "## main "+roomID('m')) || !strings.Contains(hist, "## reports "+roomID('r')) || !strings.Contains(hist, "REPORT\n    the report") ||
		!strings.Contains(hist, "agent_bus.status: 1 events") {
		t.Errorf("history:\n%s", hist)
	}
	checkDigest(t, res.Record, len(names)-1)
	// E7: GET only; the questions room was never asked for.
	for _, r := range w.hs.seen() {
		if !strings.HasPrefix(r, "GET ") || strings.Contains(r, roomID('q')) {
			t.Errorf("request %s", r)
		}
	}
	// E2/E7: no v1 file changed.
	if got := read(t, w.root+w.teamDir()+"/room_id"); got != roomID('m') {
		t.Error(got)
	}
	// E18: it verifies.
	if path, err := w.exporter().Verify("alpha", ""); err != nil || path != res.Record {
		t.Errorf("verify %s %v", path, err)
	}
}

// checkRecordFiles is E12: root's directory, 0700, files 0600, nothing else beside it. It returns
// the record's file names.
func checkRecordFiles(t *testing.T, w *world, rec string) []string {
	t.Helper()
	info, err := os.Lstat(rec)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("%v %v", info, err)
	}
	names := entries(t, rec)
	if want := []string{"digest.json", "events-main.jsonl", "events-reports.jsonl", "history.md", "state-main.json", "state-reports.json"}; !slices.Equal(names, want) {
		t.Fatalf("files %v", names)
	}
	for _, n := range names {
		fi, err := os.Lstat(rec + "/" + n)
		if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v %v", n, fi, err)
		}
	}
	if beside := entries(t, w.root+paths.AdminStateRoot+"/_v1-export/alpha"); !slices.Equal(beside, []string{fixedStamp}) {
		t.Errorf("beside the record: %v", beside)
	}
	return names
}

// checkJSONL is E13: lossless, one compacted event per line, chronological.
func checkJSONL(t *testing.T, path string, events []json.RawMessage) {
	t.Helper()
	lines := jsonl(t, path)
	if len(lines) != len(events) {
		t.Fatalf("%d lines", len(lines))
	}
	for i, line := range lines {
		var compact bytes.Buffer
		if err := json.Compact(&compact, events[i]); err != nil {
			t.Fatal(err)
		}
		if line != compact.String() {
			t.Fatalf("line %d is %s, want %s", i, line, compact.String())
		}
		var got, want any
		if json.Unmarshal([]byte(line), &got) != nil || json.Unmarshal(events[i], &want) != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("line %d does not re-parse as the event", i)
		}
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, `1.000000000000000000001`) || !strings.Contains(last, escapedAsSent) {
		t.Errorf("not as sent: %s", last)
	}
}

// checkDigest is E16 over the record at rec, which holds files files beside digest.json.
func checkDigest(t *testing.T, rec string, files int) {
	t.Helper()
	var d struct {
		V          int    `json:"v"`
		Team       string `json:"team"`
		ServerName string `json:"server_name"`
		Exported   int64  `json:"exported_at_ms"`
		Rooms      map[string]*struct {
			RoomID string         `json:"room_id"`
			Events int            `json:"events"`
			Types  map[string]int `json:"types"`
		} `json:"rooms"`
		Files map[string]struct {
			Bytes  int64  `json:"bytes"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(read(t, rec+"/digest.json")), &d); err != nil {
		t.Fatal(err)
	}
	if d.V != 1 || d.Team != "alpha" || d.ServerName != alphaServer || d.Exported != fixed.UnixMilli() {
		t.Errorf("%+v", d)
	}
	if m := d.Rooms["main"]; m == nil || m.RoomID != roomID('m') || m.Events != 230 || m.Types["m.room.message"] != 229 || m.Types["agent_bus.status"] != 1 {
		t.Errorf("main %+v", d.Rooms["main"])
	}
	if q, present := d.Rooms["questions"]; !present || q != nil {
		t.Errorf("questions %+v %v", q, present)
	}
	if len(d.Files) != files {
		t.Errorf("files %v", d.Files)
	}
	for name, f := range d.Files {
		data := read(t, rec+"/"+name)
		sum := sha256.Sum256([]byte(data))
		if f.Bytes != int64(len(data)) || f.SHA256 != hex.EncodeToString(sum[:]) {
			t.Errorf("%s: %+v", name, f)
		}
	}
}

// E6: the token is in no file, no output and no error.
func TestE6TokenNeverWritten(t *testing.T) {
	w := populated(t)
	res, err := w.export(Request{CopyTo: w.copyDir(), CopyOwner: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{res.Record, res.Copy} {
		for _, n := range entries(t, dir) {
			if fi, err := os.Lstat(dir + "/" + n); err != nil || !fi.Mode().IsRegular() {
				t.Errorf("%s/%s: %v %v", dir, n, fi, err)
				continue
			}
			if strings.Contains(read(t, dir+"/"+n), leakMarker) {
				t.Errorf("%s/%s holds the token", dir, n)
			}
		}
	}
	if strings.Contains(w.stderr.String(), leakMarker) || strings.Contains(res.Record, leakMarker) {
		t.Error("the output holds the token")
	}
	// Every failure on the way: none names it.
	for name, f := range map[string]func(w *world){
		"rejected": func(w *world) {
			w.hs.answer(http.StatusUnauthorized, `{"errcode":"M_UNKNOWN_TOKEN","error":"`+leakMarker+`"}`)
		},
		"500":  func(w *world) { w.hs.set(func(h *fakeHS) { h.failAfter = 1 }) },
		"down": func(w *world) { w.srv.Close() },
	} {
		w := populated(t)
		f(w)
		_, err := w.export(Request{})
		if err == nil || strings.Contains(err.Error(), leakMarker) || strings.Contains(w.stderr.String(), leakMarker) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// E8: a homeserver not running is refused with the way on.
func TestE8NotRunning(t *testing.T) {
	w := newWorld(t)
	w.srv.Close()
	_, err := w.export(Request{})
	for _, want := range []string{"homeserver of v1 team alpha", "is not running", "absent", "present again"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v lacks %q", err, want)
		}
	}
	if _, serr := os.Lstat(w.recordDir()); !errors.Is(serr, os.ErrNotExist) {
		t.Errorf("a record was left: %v", serr)
	}
}

// E10, E12: a room whose paging fails part-way fails the export and leaves nothing.
func TestE12FailureLeavesNothing(t *testing.T) {
	w := populated(t)
	w.hs.set(func(h *fakeHS) { h.failAfter = 2 })
	if _, err := w.export(Request{}); err == nil {
		t.Fatal("exported")
	}
	if _, err := os.Lstat(w.root + paths.AdminStateRoot + "/_v1-export/alpha"); err == nil {
		if inner := entries(t, w.root+paths.AdminStateRoot+"/_v1-export/alpha"); len(inner) != 0 {
			t.Errorf("left %v", inner)
		}
	}
}

// E12: a failure while the record is written leaves no directory, final or temporary.
func TestE12WriteFailureLeavesNothing(t *testing.T) {
	w := populated(t)
	files := []recordFile{{name: "a", data: []byte("x")}, {name: "no/such/dir", data: []byte("y")}}
	if _, err := writeRecord(w.deps, "alpha", fixedStamp, files); err == nil {
		t.Fatal("written")
	}
	if inner := entries(t, w.root+paths.AdminStateRoot+"/_v1-export/alpha"); len(inner) != 0 {
		t.Errorf("left %v", inner)
	}
}

// E12: an existing final directory is refused before anything is asked, and never overwritten.
func TestE12ExistingRefused(t *testing.T) {
	w := populated(t)
	w.mkdir(paths.AdminStateRoot+"/_v1-export/alpha/"+fixedStamp, 0o700)
	w.write(paths.AdminStateRoot+"/_v1-export/alpha/"+fixedStamp+"/keep", "k", 0o600)
	_, err := w.export(Request{})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("%v", err)
	}
	if got := entries(t, w.recordDir()); !slices.Equal(got, []string{"keep"}) {
		t.Errorf("%v", got)
	}
	if len(w.hs.seen()) != 0 {
		t.Errorf("asked %v", w.hs.seen())
	}
}

// E12: a final directory that appears while the export runs is not overwritten, and the
// temporary one is removed.
func TestE12RaceAtRename(t *testing.T) {
	w := populated(t)
	final := w.recordDir()
	w.hs.set(func(h *fakeHS) {
		h.hook = func(r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/state") {
				if err := os.MkdirAll(final+"/x", 0o700); err != nil {
					panic(err)
				}
			}
		}
	})
	_, err := w.export(Request{})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("%v", err)
	}
	if got := entries(t, w.root+paths.AdminStateRoot+"/_v1-export/alpha"); !slices.Equal(got, []string{fixedStamp}) {
		t.Errorf("beside: %v", got)
	}
	if got := entries(t, final); !slices.Equal(got, []string{"x"}) {
		t.Errorf("final: %v", got)
	}
}

// E12: the state root must be there, and what is under it must be root's directories.
func TestE12Parents(t *testing.T) {
	for name, c := range map[string]struct {
		setup func(w *world)
		want  string
	}{
		"no state root": {func(w *world) { mustRemove(w.t, w.root+paths.AdminStateRoot) }, "is missing"},
		"export dir a symlink": {func(w *world) {
			w.mkdir("/elsewhere", 0o700)
			if err := os.Symlink(w.root+"/elsewhere", w.root+paths.AdminStateRoot+"/_v1-export"); err != nil {
				w.t.Fatal(err)
			}
		}, "not a directory"},
		"team dir a file": {func(w *world) {
			w.mkdir(paths.AdminStateRoot+"/_v1-export", 0o700)
			w.write(paths.AdminStateRoot+"/_v1-export/alpha", "", 0o600)
		}, "not a directory"},
		"export dir not root's": {func(w *world) {
			w.mkdir(paths.AdminStateRoot+"/_v1-export", 0o700)
			w.deps.RootUID = os.Getuid() + 1
			w.users["agent-bus"] = Account{UID: os.Getuid(), GID: os.Getgid()}
		}, "owned by uid"},
	} {
		w := newWorld(t)
		c.setup(w)
		_, err := w.export(Request{})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// E21: two exports, two records, each verifying on its own; verify takes the newest or the one
// named.
func TestE21TwoExports(t *testing.T) {
	w := populated(t)
	first, err := w.export(Request{})
	if err != nil {
		t.Fatal(err)
	}
	later := fixed.Add(36 * time.Hour)
	w.deps.Now = func() time.Time { return later }
	w.hs.addRoom(roomID('m'), textEvents(t, 3), `[]`)
	second, err := w.export(Request{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Record == second.Record || !strings.HasSuffix(second.Record, "/20261125T221112Z") {
		t.Fatalf("%s %s", first.Record, second.Record)
	}
	if got, err := w.exporter().Verify("alpha", ""); err != nil || got != second.Record {
		t.Errorf("newest: %s %v", got, err)
	}
	if got, err := w.exporter().Verify("alpha", fixedStamp); err != nil || got != first.Record {
		t.Errorf("named: %s %v", got, err)
	}
}

// E18: each mismatch is listed.
func TestE18Mismatches(t *testing.T) {
	w := populated(t)
	res, err := w.export(Request{})
	if err != nil {
		t.Fatal(err)
	}
	rec := paths.AdminStateRoot + "/_v1-export/alpha/" + fixedStamp
	w.write(rec+"/history.md", "changed", 0o600)
	mustRemove(t, res.Record+"/state-main.json")
	w.write(rec+"/extra", "", 0o600)
	w.mkdir(rec+"/subdir", 0o700)
	if err := os.Symlink(res.Record+"/events-main.jsonl", res.Record+"/events-questions.jsonl"); err != nil {
		t.Fatal(err)
	}
	_, err = w.exporter().Verify("alpha", "")
	m, ok := errors.AsType[*MismatchError](err)
	if !ok {
		t.Fatalf("%T %v", err, err)
	}
	want := []string{
		"events-questions.jsonl is not a regular file",
		"extra is not listed in digest.json",
		"history.md is changed",
		"state-main.json is missing",
		"subdir is not a regular file",
	}
	if !slices.Equal(m.Problems, want) {
		t.Errorf("got %q", m.Problems)
	}
	if !strings.Contains(m.Error(), "history.md is changed") {
		t.Error(m.Error())
	}
}

// E18: a size change alone is a change, and a listed file that became a directory is named.
func TestE18SizeChange(t *testing.T) {
	w := populated(t)
	res, err := w.export(Request{})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(res.Record+"/events-reports.jsonl", os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	mustRemove(t, res.Record+"/state-reports.json")
	w.mkdir(paths.AdminStateRoot+"/_v1-export/alpha/"+fixedStamp+"/state-reports.json", 0o700)
	_, err = w.exporter().Verify("alpha", fixedStamp)
	want := []string{"events-reports.jsonl is changed", "state-reports.json is not a regular file"}
	if m, ok := errors.AsType[*MismatchError](err); !ok || !slices.Equal(m.Problems, want) {
		t.Fatalf("%v", err)
	}
}

// E18: what verify refuses outright.
func TestE18Refusals(t *testing.T) {
	for name, c := range map[string]struct {
		setup func(w *world, rec string)
		team  string
		stamp string
		want  string
	}{
		"bad team":       {nil, "Alpha", "", "team"},
		"bad stamp":      {nil, "alpha", "x", "stamp"},
		"no record":      {nil, "beta", "", "no export of v1 team beta"},
		"no such stamp":  {nil, "alpha", "20200101T000000Z", "no such file"},
		"digest missing": {func(w *world, rec string) { mustRemove(w.t, rec+"/digest.json") }, "alpha", "", "digest.json"},
		"digest not json": {func(w *world, rec string) {
			mustWrite(w.t, rec+"/digest.json", "{")
		}, "alpha", "", "does not decode"},
		"digest unknown key": {func(w *world, rec string) {
			mustWrite(w.t, rec+"/digest.json", `{"v":1,"team":"alpha","server_name":"s","exported_at_ms":1,"rooms":{},"files":{},"extra":1}`)
		}, "alpha", "", "does not decode"},
		"digest version": {func(w *world, rec string) {
			mustWrite(w.t, rec+"/digest.json", `{"v":2,"team":"alpha","server_name":"s","exported_at_ms":1,"rooms":{},"files":{}}`)
		}, "alpha", "", "version"},
		"digest other team": {func(w *world, rec string) {
			mustWrite(w.t, rec+"/digest.json", `{"v":1,"team":"beta","server_name":"s","exported_at_ms":1,"rooms":{},"files":{}}`)
		}, "alpha", "", "team beta"},
		"digest odd name": {func(w *world, rec string) {
			mustWrite(w.t, rec+"/digest.json", `{"v":1,"team":"alpha","server_name":"s","exported_at_ms":1,"rooms":{},"files":{"../x":{"bytes":1,"sha256":"00"}}}`)
		}, "alpha", "", "names a file"},
		"record a symlink": {func(w *world, rec string) {
			if err := os.Rename(rec, rec+"-moved"); err != nil {
				w.t.Fatal(err)
			}
			if err := os.Symlink(rec+"-moved", rec); err != nil {
				w.t.Fatal(err)
			}
		}, "alpha", fixedStamp, "not a directory"},
	} {
		w := populated(t)
		res, err := w.export(Request{})
		if err != nil {
			t.Fatal(err)
		}
		if c.setup != nil {
			c.setup(w, res.Record)
		}
		_, err = w.exporter().Verify(c.team, c.stamp)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
		if m, ok := errors.AsType[*MismatchError](err); ok && m != nil {
			t.Errorf("%s: a mismatch, not a refusal", name)
		}
	}
}

func (w *world) copyDir() string {
	w.t.Helper()
	dir := w.root + "/archive"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		w.t.Fatal(err)
	}
	return dir
}

// E17: the copy is the human's, mode 0700 and 0600, and the same record.
func TestE17Copy(t *testing.T) {
	w := populated(t)
	res, err := w.export(Request{CopyTo: w.copyDir(), CopyOwner: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Copy != w.copyDir()+"/alpha-"+fixedStamp {
		t.Fatalf("%+v", res)
	}
	info, err := os.Lstat(res.Copy)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || owner(t, info) != os.Getuid() {
		t.Fatalf("%v %v", info, err)
	}
	names := entries(t, res.Copy)
	if !slices.Equal(names, entries(t, res.Record)) {
		t.Fatalf("%v", names)
	}
	for _, n := range names {
		fi, err := os.Lstat(res.Copy + "/" + n)
		if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm() != 0o600 || owner(t, fi) != os.Getuid() {
			t.Errorf("%s: %v %v", n, fi, err)
			continue
		}
		if mine, err := os.Lstat(res.Record + "/" + n); err != nil || !mine.Mode().IsRegular() {
			t.Errorf("%s: %v %v", n, mine, err)
			continue
		}
		if read(t, res.Copy+"/"+n) != read(t, res.Record+"/"+n) {
			t.Errorf("%s differs", n)
		}
	}
	if !strings.Contains(w.stderr.String(), "copied to "+res.Copy) {
		t.Errorf("%s", w.stderr.String())
	}
}

// E17: the copy's refusals; the record stays when the copy fails after it.
func TestE17CopyRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		setup func(w *world) Request
		want  string
		// recordStays is whether the refusal comes after the record was made.
		recordStays bool
	}{
		"unknown owner": {func(w *world) Request {
			return Request{CopyTo: w.copyDir(), CopyOwner: "nobody-here"}
		}, "no account nobody-here", false},
		"missing dir": {func(w *world) Request {
			return Request{CopyTo: w.root + "/nowhere", CopyOwner: "owner"}
		}, "nowhere", false},
		"dir a file": {func(w *world) Request {
			w.write("/afile", "", 0o600)
			return Request{CopyTo: w.root + "/afile", CopyOwner: "owner"}
		}, "not a directory", false},
		"dir someone else's": {func(w *world) Request {
			w.users["owner"] = Account{UID: os.Getuid() + 1, GID: os.Getgid()}
			return Request{CopyTo: w.copyDir(), CopyOwner: "owner"}
		}, "is owned by uid", false},
		"copy exists": {func(w *world) Request {
			dir := w.copyDir()
			if err := os.Mkdir(dir+"/alpha-"+fixedStamp, 0o700); err != nil {
				w.t.Fatal(err)
			}
			return Request{CopyTo: dir, CopyOwner: "owner"}
		}, "the copy", true},
	} {
		w := populated(t)
		req := c.setup(w)
		_, err := w.export(req)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
			continue
		}
		_, serr := os.Lstat(w.recordDir())
		if c.recordStays != (serr == nil) {
			t.Errorf("%s: record %v", name, serr)
		}
		if c.recordStays && !strings.Contains(err.Error(), "the record "+w.recordDir()+" stays") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// E17: a copy that does not verify is removed, and the record stays.
func TestE17CopyVerified(t *testing.T) {
	w := populated(t)
	res, err := w.export(Request{})
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, res.Record+"/history.md", "tampered")
	dir := w.copyDir()
	_, err = copyRecord(w.deps, res.Record, "alpha", fixedStamp, Request{CopyTo: dir, CopyOwner: "owner"})
	if err == nil || !strings.Contains(err.Error(), "history.md is changed") {
		t.Fatalf("%v", err)
	}
	if got := entries(t, dir); len(got) != 0 {
		t.Errorf("left %v", got)
	}
}

// E19: progress goes to the request's progress writer.
func TestE19Progress(t *testing.T) {
	w := populated(t)
	if _, err := w.export(Request{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.stderr.String(), "main "+roomID('m')+": 230 events") {
		t.Errorf("%s", w.stderr.String())
	}
}

// E1: the request's team is checked here too, and the copy's two fields come together.
func TestExportBadRequest(t *testing.T) {
	w := newWorld(t)
	if _, err := w.export(Request{Team: "../x"}); err == nil {
		t.Fatal("accepted")
	}
	if _, err := w.export(Request{CopyTo: "/x"}); err == nil || !strings.Contains(err.Error(), "together") {
		t.Fatalf("%v", err)
	}
}

func TestProgressWriteFailure(t *testing.T) {
	w := newWorld(t)
	_, err := w.exporter().Export(t.Context(), Request{Team: "alpha", Progress: failWriter{}})
	if err == nil || !strings.Contains(err.Error(), "progress") {
		t.Fatalf("%v", err)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, e := range list {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func jsonl(t *testing.T, path string) []string {
	t.Helper()
	var lines []string
	s := bufio.NewScanner(strings.NewReader(read(t, path)))
	s.Buffer(make([]byte, 1<<20), 1<<20)
	for s.Scan() {
		lines = append(lines, s.Text())
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

func mustRemove(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func owner(t *testing.T, fi os.FileInfo) int {
	t.Helper()
	uid, _, err := ownerOf(fi)
	if err != nil {
		t.Fatal(err)
	}
	return uid
}
