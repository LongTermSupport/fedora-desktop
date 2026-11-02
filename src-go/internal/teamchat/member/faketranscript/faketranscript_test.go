package faketranscript

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/proof"
)

const (
	session  = "0c9e7a52-3f1d-4b8e-9a6c-2d4f8b1e7a90"
	envelope = "teamchat delivery 0a1b2c3d: 1 item from <alpha:w1>\n\"quoted\" & \\ done"
	big      = 1 << 20
)

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// world is a transcript in a private directory, on a fake clock.
type world struct {
	tr  *Transcript
	clk *clock.Fake
}

func newWorld(t *testing.T) world {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "project")
	check(t, os.Mkdir(dir, 0o700))
	clk := clock.NewFake(time.Hour, 0)
	tr, err := New(dir, session, clk)
	check(t, err)
	return world{tr: tr, clk: clk}
}

func lines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	check(t, err)
	if len(data) == 0 {
		return nil
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Fatalf("%s does not end in a newline: %q", path, data)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// record is every field either shape may hold; an unknown one fails the decode.
type record struct {
	Type    string `json:"type"`
	Message *struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Origin *struct {
		Kind string `json:"kind"`
	} `json:"origin"`
	Attachment *struct {
		Type   string `json:"type"`
		Prompt string `json:"prompt"`
		Origin struct {
			Kind string `json:"kind"`
		} `json:"origin"`
	} `json:"attachment"`
}

func decode(t *testing.T, line string) record {
	t.Helper()
	var r record
	dec := json.NewDecoder(strings.NewReader(line))
	dec.DisallowUnknownFields()
	check(t, dec.Decode(&r))
	return r
}

func TestNewMakesAnEmptyPrivateFileNamedForTheSession(t *testing.T) {
	w := newWorld(t)
	if filepath.Base(w.tr.Path()) != session+".jsonl" {
		t.Fatalf("Path = %s, want <dir>/%s.jsonl", w.tr.Path(), session)
	}
	st, err := os.Lstat(w.tr.Path())
	check(t, err)
	if !st.Mode().IsRegular() || st.Mode().Perm() != 0o600 || st.Size() != 0 {
		t.Fatalf("%s is %v, %d bytes; want an empty regular file, mode 0600", w.tr.Path(), st.Mode(), st.Size())
	}
	if _, err := New(filepath.Dir(w.tr.Path()), session, clock.NewFake(0, 0)); err == nil {
		t.Fatal("a second New over the same session succeeded")
	}
}

func TestNewRefusesASessionIDThatIsNotOneName(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"", ".", "..", "a/b", "a\x00b"} {
		if _, err := New(dir, id, clock.NewFake(0, 0)); !errors.Is(err, ErrSessionID) {
			t.Fatalf("New(%q) = %v, want ErrSessionID", id, err)
		}
	}
}

func TestRecordIdleIsOneUserLine(t *testing.T) {
	w := newWorld(t)
	w.tr.Record(envelope, Idle, 0)
	check(t, w.tr.Err())
	got := lines(t, w.tr.Path())
	if len(got) != 1 {
		t.Fatalf("%d lines, want 1", len(got))
	}
	r := decode(t, got[0])
	if r.Type != "user" || r.Message == nil || r.Message.Role != "user" || r.Message.Content != envelope ||
		r.Origin == nil || r.Origin.Kind != "peer" || r.Attachment != nil {
		t.Fatalf("record = %s, want the idle user shape holding the envelope", got[0])
	}
	if escaped := "\\" + "u003c"; strings.Contains(got[0], escaped) || !strings.Contains(got[0], "<alpha:w1>") {
		t.Fatalf("record %s escapes <, which Claude Code does not", got[0])
	}
}

func TestRecordBusyIsOneQueuedCommandLine(t *testing.T) {
	w := newWorld(t)
	w.tr.Record(envelope, Busy, 0)
	got := lines(t, w.tr.Path())
	if len(got) != 1 {
		t.Fatalf("%d lines, want 1", len(got))
	}
	r := decode(t, got[0])
	if r.Type != "attachment" || r.Attachment == nil || r.Attachment.Type != "queued_command" ||
		r.Attachment.Prompt != envelope || r.Attachment.Origin.Kind != "peer" || r.Message != nil || r.Origin != nil {
		t.Fatalf("record = %s, want the busy queued_command shape holding the envelope", got[0])
	}
}

func TestBothShapesHoldTheNeedleAProofScanFinds(t *testing.T) {
	for name, mode := range map[string]Mode{"idle": Idle, "busy": Busy} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			w.tr.Noise(2)
			w.tr.Record(envelope, mode, 0)
			w.tr.Noise(2)
			got, err := proof.Scan(w.tr.Path(), proof.Mark{}, []string{"0a1b2c3d"}, big)
			check(t, err)
			if len(got.Found) != 1 {
				t.Fatalf("Scan = %+v, want the needle found", got)
			}
		})
	}
}

func TestRecordAfterADelayWaitsForTheFakeClock(t *testing.T) {
	w := newWorld(t)
	w.tr.Record(envelope, Idle, 5*time.Second)
	w.clk.Advance(4 * time.Second)
	if got := lines(t, w.tr.Path()); len(got) != 0 {
		t.Fatalf("%d lines before the delay, want none", len(got))
	}
	w.clk.Advance(time.Second)
	if got := lines(t, w.tr.Path()); len(got) != 1 {
		t.Fatalf("%d lines after the delay, want 1", len(got))
	}
}

func TestRecordNeverWritesNothing(t *testing.T) {
	w := newWorld(t)
	w.tr.Record(envelope, Busy, Never)
	if w.clk.Waiters() != 0 {
		t.Fatalf("Never armed %d waiters, want none", w.clk.Waiters())
	}
	w.clk.Advance(24 * time.Hour)
	if got := lines(t, w.tr.Path()); len(got) != 0 {
		t.Fatalf("%d lines, want none", len(got))
	}
}

func TestNoiseIsValidJSONWithNoNeedle(t *testing.T) {
	w := newWorld(t)
	w.tr.Noise(3)
	got := lines(t, w.tr.Path())
	if len(got) != 3 {
		t.Fatalf("%d lines, want 3", len(got))
	}
	for _, line := range got {
		if !json.Valid([]byte(line)) || strings.Contains(line, "teamchat delivery") {
			t.Fatalf("noise line %s is not JSON without a needle", line)
		}
	}
}

func inode(t *testing.T, path string) uint64 {
	t.Helper()
	got, err := proof.Scan(path, proof.Mark{}, nil, big)
	check(t, err)
	return got.End.Ino
}

func TestRotateIsANewInodeAtThePath(t *testing.T) {
	w := newWorld(t)
	w.tr.Record(envelope, Idle, 0)
	before := inode(t, w.tr.Path())
	w.tr.Record(envelope, Busy, time.Second)
	check(t, w.tr.Rotate())
	if after := inode(t, w.tr.Path()); after == before {
		t.Fatal("Rotate kept the inode")
	}
	if got := lines(t, w.tr.Path()); len(got) != 0 {
		t.Fatalf("%d lines in the rotated file, want none", len(got))
	}
	w.clk.Advance(time.Second)
	if got := lines(t, w.tr.Path()); len(got) != 1 {
		t.Fatalf("%d lines after a delayed record, want it in the new file", len(got))
	}
}

func TestARotateThatCannotRenameLeavesNoCopy(t *testing.T) {
	w := newWorld(t)
	path := w.tr.Path()
	check(t, os.Remove(path))
	check(t, os.Mkdir(path, 0o700))
	check(t, os.WriteFile(filepath.Join(path, "held"), nil, 0o600))
	if err := w.tr.Rotate(); err == nil {
		t.Fatal("Rotate over a non-empty directory succeeded")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	check(t, err)
	if len(entries) != 1 {
		t.Fatalf("%d entries beside the transcript, want the rotation's copy removed", len(entries))
	}
}

func TestTruncateKeepsTheInode(t *testing.T) {
	w := newWorld(t)
	w.tr.Record(envelope, Idle, 0)
	before := inode(t, w.tr.Path())
	check(t, w.tr.Truncate())
	if inode(t, w.tr.Path()) != before || len(lines(t, w.tr.Path())) != 0 {
		t.Fatal("Truncate did not empty the same inode")
	}
}

func TestClearGivesANewPathInTheSameDirectory(t *testing.T) {
	w := newWorld(t)
	old := w.tr.Path()
	w.tr.Record(envelope, Idle, 0)
	const next = "5b2d8e1f-9c3a-4f7e-8d6b-1a2c3e4f5a6b"
	path, err := w.tr.Clear(next)
	check(t, err)
	if path == old || filepath.Dir(path) != filepath.Dir(old) || filepath.Base(path) != next+".jsonl" || w.tr.Path() != path {
		t.Fatalf("Clear = %s (Path %s), want a new path beside %s", path, w.tr.Path(), old)
	}
	w.tr.Record(envelope, Busy, 0)
	if len(lines(t, old)) != 1 || len(lines(t, path)) != 1 {
		t.Fatal("after Clear, want the old file kept and the new record in the new file")
	}
	if _, err := w.tr.Clear(next); err == nil {
		t.Fatal("Clear to the current session succeeded")
	}
	if _, err := w.tr.Clear("../x"); !errors.Is(err, ErrSessionID) {
		t.Fatalf("Clear(../x) = %v, want ErrSessionID", err)
	}
}

func TestAttachRecordsEachAcceptedFrameInTheModeOfTheMoment(t *testing.T) {
	w := newWorld(t)
	var accepted func(string)
	mode := Idle
	w.tr.Attach(func(f func(content string)) { accepted = f }, func() Mode { return mode }, time.Second)
	accepted("teamchat delivery 0a1b2c3d: first")
	mode = Busy
	accepted("teamchat delivery deadbeef: second")
	w.clk.Advance(time.Second)
	got := lines(t, w.tr.Path())
	if len(got) != 2 || decode(t, got[0]).Type != "user" || decode(t, got[1]).Type != "attachment" {
		t.Fatalf("lines = %q, want an idle then a busy record", got)
	}
}

func TestAnUnknownModeIsKeptForErrAndWritesNothing(t *testing.T) {
	w := newWorld(t)
	w.tr.Record(envelope, Busy+1, 0)
	w.tr.Record(envelope, Idle, 0)
	if err := w.tr.Err(); err == nil || !strings.Contains(err.Error(), "mode 2") {
		t.Fatalf("Err = %v, want the unknown mode named", err)
	}
	if got := lines(t, w.tr.Path()); len(got) != 1 {
		t.Fatalf("%d lines, want only the idle record", len(got))
	}
}

func TestAFailedWriteIsKeptForErr(t *testing.T) {
	w := newWorld(t)
	check(t, os.RemoveAll(filepath.Dir(w.tr.Path())))
	w.tr.Record(envelope, Idle, 0)
	w.tr.Noise(1)
	if err := w.tr.Err(); err == nil {
		t.Fatal("Err = nil after a write into a removed directory")
	}
	if err := w.tr.Rotate(); err == nil {
		t.Fatal("Rotate into a removed directory succeeded")
	}
	if err := w.tr.Truncate(); err == nil {
		t.Fatal("Truncate of a removed file succeeded")
	}
	if _, err := New(filepath.Dir(w.tr.Path()), session, clock.NewFake(0, 0)); err == nil {
		t.Fatal("New in a removed directory succeeded")
	}
}
