package restore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// newStore is a Store in a temporary state directory with a boot id file holding boot-one.
func newStore(t *testing.T) *Store {
	t.Helper()
	state := t.TempDir()
	bootFile := filepath.Join(state, "boot_id")
	if err := os.WriteFile(bootFile, []byte("boot-one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// One level down, so the manifest's directory is the store's to create (and its mode the
	// store's to set), not the test's temp directory, whose mode depends on the machine.
	return NewStore(filepath.Join(state, "ccy", "sessions"), bootFile)
}

func entryA() Entry {
	return Entry{Name: "ccy-a", Prefix: "ccy", Dir: "/p/odd dir=x", Resume: "", Supervised: true, Going: GoingCompact, At: "1700000000", Detail: "250000", Transcript: "/t x.jsonl"}
}

func entryB() Entry {
	return Entry{Name: "cc-b", Prefix: "cc", Dir: "/d2", Resume: "0a1b", Supervised: false, Going: GoingPending}
}

func TestPathIsBesideTheRegistry(t *testing.T) {
	s := NewStore("/state/ccy/sessions", "/boot")
	if s.Path != "/state/ccy/last-restore" {
		t.Fatalf("got %q", s.Path)
	}
}

func TestWriteThenReadRoundTripAndFormat(t *testing.T) {
	s := newStore(t)
	if err := s.Write([]Entry{entryA(), entryB()}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	want := "ccy-restore-manifest 2\nboot=boot-one\n" +
		"name=ccy-a\nprefix=ccy\ndir=/p/odd dir=x\nresume=\nsupervised=yes\ngoing=compact\nat=1700000000\ndetail=250000\ntranscript=/t x.jsonl\n" +
		"name=cc-b\nprefix=cc\ndir=/d2\nresume=0a1b\nsupervised=no\ngoing=pending\nat=\ndetail=\ntranscript=\n"
	if string(raw) != want {
		t.Fatalf("file:\n%q\nwant:\n%q", raw, want)
	}
	info, err := os.Stat(s.Path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, %v", info.Mode(), err)
	}
	dirInfo, err := os.Stat(filepath.Dir(s.Path))
	if err != nil || dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v, %v", dirInfo.Mode(), err)
	}
	got, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	if got.Boot != "boot-one" || !slices.Equal(got.Entries, []Entry{entryA(), entryB()}) {
		t.Fatalf("got %+v", got)
	}
	if got.Index("cc-b") != 1 || got.Index("nobody") != -1 {
		t.Fatal("Index")
	}
	left, err := filepath.Glob(s.Path + ".tmp.*")
	if err != nil || len(left) != 0 {
		t.Fatalf("temp files %v, %v", left, err)
	}
}

func TestWriteRefusals(t *testing.T) {
	s := newStore(t)
	bad := entryA()
	bad.Dir = "/d\nname=b"
	if err := s.Write([]Entry{bad}); err == nil || !strings.Contains(err.Error(), "cannot hold a value with a newline in it") {
		t.Fatalf("got %v", err)
	}
	keep := entryA()
	keep.Going = GoingKeep
	if err := s.Write([]Entry{keep}); err == nil || !strings.Contains(err.Error(), "going") {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(s.Path); err == nil {
		t.Fatal("a refused write left a manifest")
	}
	noBoot := NewStore(filepath.Join(t.TempDir(), "sessions"), filepath.Join(t.TempDir(), "absent"))
	if err := noBoot.Write(nil); err == nil || !strings.Contains(err.Error(), "boot's id could not be read") {
		t.Fatalf("got %v", err)
	}
}

func TestWriteFailsWhenTheDirectoryCannotBeMade(t *testing.T) {
	state := t.TempDir()
	blocker := filepath.Join(state, "ccy")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	bootFile := filepath.Join(state, "boot_id")
	if err := os.WriteFile(bootFile, []byte("b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(filepath.Join(blocker, "sessions"), bootFile)
	if err := s.Write(nil); err == nil || !strings.Contains(err.Error(), "could not create the directory for the restore manifest") {
		t.Fatalf("got %v", err)
	}
}

func TestBootID(t *testing.T) {
	s := newStore(t)
	if id, err := s.BootID(); err != nil || id != "boot-one" {
		t.Fatalf("got %q, %v", id, err)
	}
	if err := os.WriteFile(s.BootIDFile, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BootID(); err == nil || err.Error() != "this boot's id could not be read from "+s.BootIDFile {
		t.Fatalf("got %v", err)
	}
	s.BootIDFile = ""
	if s.bootFile() != "/proc/sys/kernel/random/boot_id" {
		t.Fatal("default boot id file")
	}
}

func TestReadIsStrict(t *testing.T) {
	entry := "name=a\nprefix=ccy\ndir=/d\nresume=\nsupervised=yes\ngoing=pending\nat=\ndetail=\ntranscript=\n"
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"wrong header", "something else\nboot=b\n", "does not start with 'ccy-restore-manifest 2' and is not read"},
		{"older version", "ccy-restore-manifest 1\nboot=b\nname=a\n", "written by another version of ccy-sessions; this one reads 'ccy-restore-manifest 2'"},
		{"no boot", "ccy-restore-manifest 2\n" + entry, "names no boot"},
		{"header only", "ccy-restore-manifest 2\n", "names no boot"},
		{"cut short", "ccy-restore-manifest 2\nboot=b\nname=a\nprefix=ccy\ndir=/d\nresume=\nsupervised=yes\ngoing=pending\n", "ends part-way through an entry"},
		{"out of order", "ccy-restore-manifest 2\nboot=b\nprefix=ccy\nname=a\n", "has 'prefix' where 'name' was expected and is not read"},
		{"unknown key", "ccy-restore-manifest 2\nboot=b\n" + entry + "extra=x\n", "has 'extra' where 'name' was expected"},
		{"bad going", "ccy-restore-manifest 2\nboot=b\n" + strings.Replace(entry, "going=pending", "going=maybe", 1), "has going=maybe, which is none of none, pending, compacting, compact, continue or untouched, and is not read"},
		{"bad supervised", "ccy-restore-manifest 2\nboot=b\n" + strings.Replace(entry, "supervised=yes", "supervised=maybe", 1), "has supervised=maybe, which is neither yes nor no, and is not read"},
		{"bad at", "ccy-restore-manifest 2\nboot=b\n" + strings.Replace(entry, "at=", "at=soon", 1), "has at=soon, which is not a time in seconds, and is not read"},
		{"no equals", "ccy-restore-manifest 2\nboot=b\nname", "has a line without '=': name"},
		{"empty", "", "is empty"},
		{"second boot after an entry", "ccy-restore-manifest 2\nboot=b\n" + entry + "boot=c\n", "has 'boot' where 'name' was expected"},
	}
	for _, c := range cases {
		s := newStore(t)
		if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(s.Path, []byte(c.content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Read(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
	}
}

func TestReadIgnoresBlankLinesAndMissingFinalNewline(t *testing.T) {
	s := newStore(t)
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := "ccy-restore-manifest 2\n\nboot=b\n\nname=a\nprefix=cc\ndir=/d\nresume=\nsupervised=no\ngoing=none\nat=\ndetail=\ntranscript="
	if err := os.WriteFile(s.Path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read()
	if err != nil || got.Boot != "b" || len(got.Entries) != 1 || got.Entries[0].Going != GoingNone {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestReadMissingAndUnreadable(t *testing.T) {
	s := newStore(t)
	_, err := s.Read()
	if !errors.Is(err, ErrNoManifest) || err.Error() != "no session restore has been recorded ("+s.Path+" does not exist)" {
		t.Fatalf("got %v", err)
	}
	if err = os.MkdirAll(s.Path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Read(); err == nil || !strings.Contains(err.Error(), "cannot be read") {
		t.Fatalf("got %v", err)
	}
}

type mergeResult struct{ warn, err error }

func merge(s *Store, entries []Entry) mergeResult {
	var r mergeResult
	r.err = s.Merge(entries, func(w error) { r.warn = w })
	return r
}

func TestMergeWithoutAWarnCallbackStillReplaces(t *testing.T) {
	s := newStore(t)
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path, []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Merge([]Entry{entryB()}, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Read(); err != nil || len(got.Entries) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestMergeKeepsAndReplaces(t *testing.T) {
	s := newStore(t)
	if r := merge(s, []Entry{entryA(), entryB()}); r.err != nil || r.warn != nil {
		t.Fatalf("first merge: %+v", r)
	}
	// set-going moved ccy-a on; a second restore finds it running and says keep.
	moved := entryA()
	moved.Going = GoingContinue
	moved.At = "1700000005"
	if err := s.Write([]Entry{moved, entryB()}); err != nil {
		t.Fatal(err)
	}
	keepA := Entry{Name: "ccy-a", Prefix: "ccy", Dir: "/other", Going: GoingKeep}
	keepNew := Entry{Name: "ccy-new", Prefix: "ccy", Dir: "/n", Going: GoingKeep}
	started := Entry{Name: "ccy-c", Prefix: "ccy", Dir: "/c", Supervised: true, Going: GoingPending}
	if r := merge(s, []Entry{keepA, keepNew, started}); r.err != nil || r.warn != nil {
		t.Fatalf("second merge: %+v", r)
	}
	got, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	wantNew := Entry{Name: "ccy-new", Prefix: "ccy", Dir: "/n", Going: GoingNone}
	if !slices.Equal(got.Entries, []Entry{moved, wantNew, started}) {
		t.Fatalf("got %+v", got.Entries)
	}
}

func TestMergeStartsAfreshFromAnOtherBootOrAnUnreadableManifest(t *testing.T) {
	s := newStore(t)
	if r := merge(s, []Entry{entryA()}); r.err != nil {
		t.Fatal(r.err)
	}
	if err := os.WriteFile(s.BootIDFile, []byte("boot-two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := entryA()
	keep.Going = GoingKeep
	if r := merge(s, []Entry{keep}); r.err != nil || r.warn != nil {
		t.Fatalf("other boot: %+v", r)
	}
	got, err := s.Read()
	if err != nil || got.Boot != "boot-two" || got.Entries[0].Going != GoingNone {
		t.Fatalf("got %+v, %v", got, err)
	}
	if err = os.WriteFile(s.Path, []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := merge(s, []Entry{entryB()}); r.err != nil || r.warn == nil || !strings.Contains(r.warn.Error(), "does not start with") {
		t.Fatalf("unreadable: %+v", r)
	}
	got, err = s.Read()
	if err != nil || len(got.Entries) != 1 || got.Entries[0].Name != "cc-b" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestMergeFailsWithoutABootID(t *testing.T) {
	s := newStore(t)
	s.BootIDFile = filepath.Join(t.TempDir(), "absent")
	if err := s.Merge(nil, nil); err == nil || !strings.Contains(err.Error(), "boot's id could not be read") {
		t.Fatalf("got %v", err)
	}
}

func TestUpdate(t *testing.T) {
	s := newStore(t)
	if err := s.Write([]Entry{entryA(), entryB()}); err != nil {
		t.Fatal(err)
	}
	change := Change{Going: GoingContinue, At: "1700000001", Detail: "4000", Transcript: "/t/c.jsonl"}
	if err := s.Update("boot-one", "cc-b", GoingPending, change); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	b := got.Entries[1]
	if b.Going != GoingContinue || b.At != "1700000001" || b.Detail != "4000" || b.Transcript != "/t/c.jsonl" || b.Resume != "0a1b" {
		t.Fatalf("got %+v", b)
	}
	if got.Entries[0] != entryA() {
		t.Fatalf("the other entry changed: %+v", got.Entries[0])
	}
	for name, args := range map[string]struct {
		boot, session string
		expect        Going
	}{
		"moved on":          {"boot-one", "cc-b", GoingPending},
		"another boot":      {"another-boot", "cc-b", GoingContinue},
		"unknown session":   {"boot-one", "nobody", GoingPending},
		"wrong expectation": {"boot-one", "ccy-a", GoingPending},
	} {
		if err = s.Update(args.boot, args.session, args.expect, change); !errors.Is(err, ErrNoLongerApplies) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	if _, statErr := os.Stat(s.Path + ".lock"); statErr != nil {
		t.Fatalf("the lock file sits beside the manifest: %v", statErr)
	}
}

func TestUpdateWithoutAManifest(t *testing.T) {
	s := newStore(t)
	if err := s.Update("boot-one", "x", GoingPending, Change{}); !errors.Is(err, ErrNoManifest) {
		t.Fatalf("got %v", err)
	}
}

func TestLockedTimesOutWhileAnotherHoldsTheLock(t *testing.T) {
	s := newStore(t)
	s.LockWait = 250 * time.Millisecond
	entered := false
	err := s.Locked(func() error {
		inner := *s
		inner.LockWait = 150 * time.Millisecond
		return inner.Locked(func() error { entered = true; return nil })
	})
	if err == nil || entered || !strings.Contains(err.Error(), "was not released within 0 s; nothing was written") {
		t.Fatalf("entered %v, err %v", entered, err)
	}
	// Released: free again.
	if err = s.Locked(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestLockedReturnsTheFunctionsError(t *testing.T) {
	s := newStore(t)
	want := errors.New("boom")
	if err := s.Locked(func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}

func TestLockedCannotOpenTheLock(t *testing.T) {
	s := newStore(t)
	if err := os.MkdirAll(s.Path+".lock", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.Locked(func() error { return nil }); err == nil || !strings.Contains(err.Error(), "could not open the restore manifest's lock") {
		t.Fatalf("got %v", err)
	}
}

func TestSetGoingLock(t *testing.T) {
	s := newStore(t)
	release, err := s.LockSetGoing(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.LockSetGoing(150 * time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "another set-going still holds "+s.Path+".set-going.lock after 0s; nothing was set going by this one") {
		t.Fatalf("got %v", err)
	}
	if err = release(); err != nil {
		t.Fatal(err)
	}
	second, err := s.LockSetGoing(0)
	if err != nil {
		t.Fatalf("a zero wait still tries once: %v", err)
	}
	if err = second(); err != nil {
		t.Fatal(err)
	}
}

func TestSetGoingLockCreatesTheStateDirectoryAThisBootWithNoRestoreLacks(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "state", "last-restore")}
	release, err := s.LockSetGoing(0)
	if err != nil {
		t.Fatalf("a missing directory is not a reason to fail the lock: %v", err)
	}
	if err = release(); err != nil {
		t.Fatal(err)
	}
}

func TestSetGoingLockCannotCreateItsDirectory(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "state")
	if err := os.WriteFile(blocker, []byte("a file"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Store{Path: filepath.Join(blocker, "last-restore")}
	if _, err := s.LockSetGoing(0); err == nil || !strings.Contains(err.Error(), "could not create the directory for the restore manifest") {
		t.Fatalf("got %v", err)
	}
}

func TestSetGoingLockCannotOpen(t *testing.T) {
	s := newStore(t)
	if err := os.MkdirAll(s.Path+".set-going.lock", 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LockSetGoing(0); err == nil || !strings.Contains(err.Error(), "could not open set-going's lock") {
		t.Fatalf("got %v", err)
	}
}

func TestAtSeconds(t *testing.T) {
	if n, err := (Entry{}).AtSeconds(); err != nil || n != 0 {
		t.Fatalf("got %d, %v", n, err)
	}
	if n, err := (Entry{At: "1700000000"}).AtSeconds(); err != nil || n != 1700000000 {
		t.Fatalf("got %d, %v", n, err)
	}
	if _, err := (Entry{At: "99999999999999999999999"}).AtSeconds(); err == nil {
		t.Fatal("an overflowing time is an error")
	}
}

// --- format compatibility with the bash library, both directions ---

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for dir := wd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if _, statErr := os.Stat(filepath.Join(dir, "files/var/local/claude-yolo/lib/session-registry.bash")); statErr == nil {
			return dir
		}
	}
	t.Fatalf("repo root not found above %s", wd)
	return ""
}

// bash runs a snippet with the real registry library sourced against the Store's state.
func bash(t *testing.T, s *Store, snippet string) string {
	t.Helper()
	lib := filepath.Join(repoRoot(t), "files/var/local/claude-yolo/lib/session-registry.bash")
	script := "set -euo pipefail\nprint_error() { printf 'ERROR: %s\\n' \"$*\" >&2; }\nsource \"$2\"\nsource \"$1\"\n" + snippet
	res, err := execx.OS{}.Run(context.Background(), execx.Cmd{
		Name: "bash", Args: []string{"-c", script, "bash", lib, filepath.Join(repoRoot(t), "files/var/local/claude-yolo/lib/common-pure.bash")},
		Env: []string{"CCY_STATE_DIR=" + filepath.Dir(s.Path), "CCY_BOOT_ID_FILE=" + s.BootIDFile},
	})
	if err != nil {
		t.Fatalf("bash: %v\nstderr: %s", err, res.Stderr)
	}
	return string(res.Stdout)
}

func TestBashWritesGoReads(t *testing.T) {
	s := newStore(t)
	bash(t, s, `ccy_restore_manifest_write "ccy-odd" ccy "/odd dir=x" "" yes compact 1700000000 250000 "/t x.jsonl" "cc-two" cc /d2 0a1b no pending "" "" ""`)
	got, err := s.Read()
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{Name: "ccy-odd", Prefix: "ccy", Dir: "/odd dir=x", Supervised: true, Going: GoingCompact, At: "1700000000", Detail: "250000", Transcript: "/t x.jsonl"},
		{Name: "cc-two", Prefix: "cc", Dir: "/d2", Resume: "0a1b", Going: GoingPending},
	}
	if got.Boot != "boot-one" || !slices.Equal(got.Entries, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestGoWritesBashReadsAndUpdates(t *testing.T) {
	s := newStore(t)
	if err := s.Write([]Entry{entryA(), entryB()}); err != nil {
		t.Fatal(err)
	}
	out := bash(t, s, `ccy_restore_manifest_read
printf '%s|%s|%s|%s|%s|%s\n' "$RM_BOOT" "${RM_NAMES[*]}" "${RM_DIRS[0]}" "${RM_GOING[0]}" "${RM_SUPERVISED[*]}" "${RM_TRANSCRIPT[0]}"
ccy_restore_manifest_update boot-one cc-b pending continue 1700000001 4000 /t/c.jsonl`)
	if out != "boot-one|ccy-a cc-b|/p/odd dir=x|compact|yes no|/t x.jsonl\n" {
		t.Fatalf("bash saw %q", out)
	}
	got, err := s.Read()
	if err != nil || got.Entries[1].Going != GoingContinue || got.Entries[1].At != "1700000001" {
		t.Fatalf("go saw %+v, %v", got, err)
	}
	if err = s.Update("boot-one", "cc-b", GoingContinue, Change{Going: GoingUntouched, Detail: "x"}); err != nil {
		t.Fatal(err)
	}
	out = bash(t, s, `ccy_restore_manifest_read; printf '%s|%s\n' "${RM_GOING[1]}" "${RM_DETAIL[1]}"`)
	if out != "untouched|x\n" {
		t.Fatalf("bash saw %q", out)
	}
}

func TestVerdictAndGoingStateAgreeWithBash(t *testing.T) {
	s := newStore(t)
	out := bash(t, s, `ccy_restore_verdict ccy 1 "Use same configuration? [Y/n] " - started
ccy_restore_verdict cc 1 "claude" - failed:x
ccy_restore_going_state compacting 100 5 220 120 no
ccy_restore_going_state untouched "" "" 1 1 -`)
	want := "WAITING-AT-PROMPT quick-launch\nNOT-SET-GOING x\nfailed:compact-not-started\nfailed:no-reason-recorded\n"
	if out != want {
		t.Fatalf("bash said %q", out)
	}
	if got := Judge("ccy", true, "Use same configuration? [Y/n] ", "-", "started").String(); got != "WAITING-AT-PROMPT quick-launch" {
		t.Fatalf("got %q", got)
	}
	if got := GoingState(GoingCompacting, 100, "5", 220, 120, TookNo); got != "failed:compact-not-started" {
		t.Fatalf("got %q", got)
	}
}
