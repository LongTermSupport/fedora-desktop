package sessions

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/restore"
)

// restoreHost fakes the tools a restore starts a session with: tmux, systemd-run and
// systemd-escape are on PATH, and no session is live.
func restoreHost(h *harness) {
	h.app.Tmux.LookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	h.fake.OnFunc([]string{"systemd-escape"}, func(c execx.Cmd) (execx.Result, error) {
		return execx.Result{Stdout: []byte(c.Args[len(c.Args)-1] + "\n")}, nil
	})
	h.fake.On([]string{"systemd-run"}, execx.Result{})
	h.sessions()
}

// record writes a session record the way a launcher does and returns its path.
func record(t *testing.T, h *harness, rec registry.Record) string {
	t.Helper()
	dir, err := registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err = registry.New(dir).Write(rec); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, rec.Name)
}

// project makes a project directory for a record and returns it.
func realDir(t *testing.T, h *harness, name string) string {
	t.Helper()
	dir := filepath.Join(h.root, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	return dir
}

func manifestOf(t *testing.T, h *harness) restore.Manifest {
	t.Helper()
	regdir, err := registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	m, err := restore.NewStore(regdir, h.env["CCY_BOOT_ID_FILE"]).Read()
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// startedSessions is the systemd-run calls, one per session started.
func startedSessions(h *harness) []execx.Cmd {
	var calls []execx.Cmd
	for _, c := range h.fake.Calls() {
		if c.Name == "systemd-run" {
			calls = append(calls, c)
		}
	}
	return calls
}

func TestRestoreStartsEachRecordedSessionInNameOrderAndWritesThePendingManifest(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	a, b := realDir(t, h, "a"), realDir(t, h, "b")
	record(t, h, registry.Record{
		Name: "ccy-b", Dir: b, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true, Compose: "started",
		Args: []string{"--resume", "0a1b-old"},
	})
	record(t, h, registry.Record{Name: "cc-a", Dir: a, Launcher: "/launch/cc", Prefix: "cc", Restore: true})
	exitCodeIs(t, h.run("restore"), 0, h)
	calls := startedSessions(h)
	if len(calls) != 2 {
		t.Fatalf("two sessions started: %v", h.ran())
	}
	// Spec bug 9.13: the trampoline names the session's own launcher, not always ccy.
	contains(t, "cc-a", strings.Join(calls[0].Args, " "), "--unit ccy-tmux-", "-s cc-a -c "+a+" -- bash -c ",
		" ccy-tmux env CCY_SESSION_RESTORE=1 /launch/cc --continue ; set-hook -g client-attached ", "cc exited with status")
	contains(t, "ccy-b", strings.Join(calls[1].Args, " "),
		" ccy-tmux env CCY_SESSION_RESTORE=1 /launch/ccy --resume 0a1b-old --compose start --supervise ")
	contains(t, "progress", h.stderr(), "restored cc-a in "+a+".\n", "restored ccy-b in "+b+".\n", "ccy session restore: 2 started, 0 failed.\n")
	m := manifestOf(t, h)
	entries := make([]string, 0, len(m.Entries))
	for _, e := range m.Entries {
		entries = append(entries, fmt.Sprintf("%s %s %s resume=%q supervised=%v %s", e.Name, e.Prefix, e.Dir, e.Resume, e.Supervised, e.Going))
	}
	want := []string{"cc-a cc " + a + ` resume="" supervised=false pending`, "ccy-b ccy " + b + ` resume="0a1b-old" supervised=true pending`}
	if m.Boot != "this-boot" || !slices.Equal(entries, want) {
		t.Fatalf("manifest %+v", m)
	}
	if h.stdout() != "" {
		t.Fatalf("stdout %q", h.stdout())
	}
}

func TestRestoreDryRunSaysWhatItWouldStartAndWritesNothing(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	a := realDir(t, h, "a")
	record(t, h, registry.Record{Name: "ccy-a", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	exitCodeIs(t, h.run("restore", "--dry-run"), 0, h)
	contains(t, "dry run", h.stderr(), "would start ccy-a in "+a+": /launch/ccy --supervise --continue\n",
		"ccy session restore: 0 started, 0 failed.\n")
	if len(startedSessions(h)) != 0 {
		t.Fatal("a dry run starts nothing")
	}
	regdir, err := registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(restore.NewStore(regdir, "").Path); statErr == nil {
		t.Fatal("a dry run writes no manifest")
	}
}

func TestRestoreSkipsNoRestoreRecordsAndRunningSessionsKeepingTheirManifestEntry(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	a := realDir(t, h, "a")
	record(t, h, registry.Record{Name: "ccy-a", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	record(t, h, registry.Record{Name: "ccy-n", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: false})
	h.sessions("ccy-a 0 " + a)
	// An earlier restore this boot already set ccy-a going.
	regdir, err := registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	store := restore.NewStore(regdir, h.env["CCY_BOOT_ID_FILE"])
	if err = store.Write([]restore.Entry{{Name: "ccy-a", Prefix: "ccy", Dir: a, Supervised: true, Going: restore.GoingContinue, At: time.Unix(123, 0), Detail: "5000", Transcript: "/t"}}); err != nil {
		t.Fatal(err)
	}
	exitCodeIs(t, h.run("restore"), 0, h)
	contains(t, "skips", h.stderr(), "skip ccy-n: marked no-restore.\n", "skip ccy-a: already running.\n")
	if len(startedSessions(h)) != 0 {
		t.Fatal("nothing is started")
	}
	m := manifestOf(t, h)
	if len(m.Entries) != 1 || m.Entries[0].Going != restore.GoingContinue || m.Entries[0].AtUnix() != 123 {
		t.Fatalf("the earlier restore's entry is kept: %+v", m)
	}
}

func TestARunningSessionWithNoEarlierEntryIsRecordedAsNone(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	a := realDir(t, h, "a")
	record(t, h, registry.Record{Name: "ccy-a", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	h.sessions("ccy-a 1 " + a)
	exitCodeIs(t, h.run("restore"), 0, h)
	if m := manifestOf(t, h); len(m.Entries) != 1 || m.Entries[0].Going != restore.GoingNone {
		t.Fatalf("%+v", m)
	}
}

func TestARecordWhoseDirectoryIsGoneFailsThatSessionOnly(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	a := realDir(t, h, "a")
	gone := filepath.Join(h.root, "gone")
	file := record(t, h, registry.Record{Name: "ccy-b", Dir: gone, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	record(t, h, registry.Record{Name: "ccy-c", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	exitCodeIs(t, h.run("restore"), 1, h)
	contains(t, "gone", h.stderr(), "ERROR: cannot restore ccy-b: its directory no longer exists: "+gone+
		" (the record is kept at "+file+"; remove it if the project is gone).",
		"restored ccy-c in "+a+".", "ccy session restore: 1 started, 1 failed.")
	if m := manifestOf(t, h); len(m.Entries) != 1 || m.Entries[0].Name != "ccy-c" {
		t.Fatalf("%+v", m)
	}
}

func TestAnUnreadableRecordCountsAsAFailureAndTheRestGoOn(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	a := realDir(t, h, "a")
	record(t, h, registry.Record{Name: "ccy-a", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	regdir, err := registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(regdir, "ccy-bad")
	if err = os.WriteFile(bad, []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A write in flight is not a record.
	if err = os.WriteFile(filepath.Join(regdir, "ccy-a.tmp.77"), []byte("half"), 0o600); err != nil {
		t.Fatal(err)
	}
	exitCodeIs(t, h.run("restore"), 1, h)
	contains(t, "bad record", h.stderr(), "session record "+bad, "restored ccy-a in "+a+".", "ccy session restore: 1 started, 1 failed.")
}

func TestARecordWhoseArgumentsCannotBeWorkedOutIsAFailure(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	a := realDir(t, h, "a")
	regdir, err := registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(regdir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A compose outcome the launcher never writes: the reader refuses it, so it reads as unreadable.
	file := filepath.Join(regdir, "ccy-x")
	body := "ccy-session-record 1\nname=ccy-x\ndir=" + a + "\nlauncher=/l\nprefix=ccy\nrestore=yes\ncompose=weird\n"
	if err = os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	exitCodeIs(t, h.run("restore"), 1, h)
	contains(t, "compose", h.stderr(), "compose='weird'")
}

func TestARegistryThatDoesNotExistIsAnEmptyOneAndStillGetsAManifest(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	exitCodeIs(t, h.run("restore"), 0, h)
	regdir, err := registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	contains(t, "absent", h.stderr(), "ccy session registry: nothing to restore ("+regdir+" does not exist).")
	if m := manifestOf(t, h); m.Boot != "this-boot" || len(m.Entries) != 0 {
		t.Fatalf("%+v", m)
	}
	h = newHarness(t)
	restoreHost(h)
	exitCodeIs(t, h.run("restore", "--dry-run"), 0, h)
	contains(t, "dry absent", h.stderr(), "does not exist")
}

func TestAnEmptyRegistryHasNothingToRestore(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	regdir, err := registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(regdir, 0o700); err != nil {
		t.Fatal(err)
	}
	exitCodeIs(t, h.run("restore"), 0, h)
	contains(t, "empty", h.stderr(), "ccy session registry: nothing to restore (no records in "+regdir+").")
	if m := manifestOf(t, h); len(m.Entries) != 0 {
		t.Fatalf("%+v", m)
	}
}

func TestARegistryThatCannotBeListedIsAnErrorNotAnEmptyOne(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	regdir, err := registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(regdir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(regdir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	exitCodeIs(t, h.run("restore"), 1, h)
	contains(t, "not a dir", h.stderr(), "ERROR: the session registry "+regdir+" is not a readable directory, so the sessions recorded there cannot be listed; nothing is restored.")

	if os.Getuid() == 0 {
		return // root reads any directory
	}
	h = newHarness(t)
	restoreHost(h)
	regdir, err = registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(regdir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(regdir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		chmod(t, regdir, 0o700)
	})
	exitCodeIs(t, h.run("restore"), 1, h)
	contains(t, "unreadable dir", h.stderr(), "is not a readable directory")
}

func TestARelativeRegistryBaseIsRefused(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	h.env["CCY_STATE_DIR"] = "relative/state"
	exitCodeIs(t, h.run("restore"), 1, h)
	contains(t, "relative", h.stderr(), "session registry base 'relative/state' is not an absolute path.")
}

func TestNothingIsRestoredOnTopOfALiveSetThatCannotBeRead(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	a := realDir(t, h, "a")
	record(t, h, registry.Record{Name: "ccy-a", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	h.tmuxFail("list-sessions", "lost server")
	exitCodeIs(t, h.run("restore"), 1, h)
	contains(t, "live set", h.stderr(), "ERROR: the live session list could not be read, so nothing is restored: starting sessions on top of an unknown live set could double every one of them.")
	if len(startedSessions(h)) != 0 {
		t.Fatal("nothing started")
	}
}

func TestASessionThatCannotBeStartedIsAFailureAndIsLeftOutOfTheManifest(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	a := realDir(t, h, "a")
	file := record(t, h, registry.Record{Name: "ccy-a", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	h.fake.On([]string{"systemd-run"}, execx.Result{ExitCode: 1, Stderr: []byte("Failed to start scope")})
	exitCodeIs(t, h.run("restore"), 1, h)
	contains(t, "start failure", h.stderr(), "Failed to start scope", "ERROR: could not start ccy-a in "+a+" (the record is kept at "+file+").",
		"ccy session restore: 0 started, 1 failed.")
	if m := manifestOf(t, h); len(m.Entries) != 0 {
		t.Fatalf("%+v", m)
	}
}

func TestAManifestThatCannotBeWrittenIsAFailure(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	a := realDir(t, h, "a")
	record(t, h, registry.Record{Name: "ccy-a", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	h.env["CCY_BOOT_ID_FILE"] = filepath.Join(h.root, "no-such-boot-id")
	h.app.cfg.BootIDFile = h.env["CCY_BOOT_ID_FILE"]
	exitCodeIs(t, h.run("restore"), 1, h)
	contains(t, "manifest", h.stderr(), "this boot's id could not be read from "+h.env["CCY_BOOT_ID_FILE"], "1 started, 1 failed")
}

func TestAnUnreadableEarlierManifestIsReplacedWithANote(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	a := realDir(t, h, "a")
	record(t, h, registry.Record{Name: "ccy-a", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	regdir, err := registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(restore.NewStore(regdir, "").Path, []byte("junk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	exitCodeIs(t, h.run("restore"), 0, h)
	contains(t, "replaced", h.stderr(), "does not start with")
	if m := manifestOf(t, h); len(m.Entries) != 1 {
		t.Fatalf("%+v", m)
	}
}

func TestRestoreUsageMistakes(t *testing.T) {
	cases := []struct {
		args []string
		msg  string
	}{
		{[]string{"restore", "--now"}, "unknown restore option '--now'."},
		// Spec bug 9.12: an empty word is a usage error here, not a late failure in a library.
		{[]string{"restore", ""}, "unknown restore option ''."},
		{[]string{"restore", "--dry-run", "x"}, "unexpected argument 'x'."},
	}
	for _, c := range cases {
		h := newHarness(t)
		exitCodeIs(t, h.run(c.args...), 64, h)
		if want := "ccy-sessions: " + c.msg + " See ccy-sessions --help.\n"; h.stderr() != want {
			t.Fatalf("%v: %q", c.args, h.stderr())
		}
	}
}

// passphrase scripts the server's restore: a ccy and a cc record, and a passphrase file.
func passphraseHost(t *testing.T) (*harness, string) {
	t.Helper()
	h := newHarness(t)
	restoreHost(h)
	dir := realDir(t, h, "proj")
	record(t, h, registry.Record{Name: "ccy-proj", Dir: dir, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true, Args: []string{"--ssh-key", "/k"}})
	record(t, h, registry.Record{Name: "cc-proj", Dir: dir, Launcher: "/launch/cc", Prefix: "cc", Restore: true})
	file := h.write("restore-ssh-passphrase", "fixture pass'phrase 7")
	h.env[unlockFileEnv] = file
	return h, file
}

func TestThePassphraseFileReachesCcySessionsOnlyAndLeavesTheEnvironment(t *testing.T) {
	h, file := passphraseHost(t)
	exitCodeIs(t, h.run("restore"), 0, h)
	calls := startedSessions(h)
	if len(calls) != 2 {
		t.Fatalf("%v", h.ran())
	}
	cc, ccy := strings.Join(calls[0].Args, " "), strings.Join(calls[1].Args, " ")
	if !strings.Contains(ccy, " ccy-tmux env CCY_SESSION_RESTORE=1 "+unlockFileEnv+"="+file+" /launch/ccy ") {
		t.Fatalf("the ccy session is given the path: %s", ccy)
	}
	if strings.Contains(cc, unlockFileEnv) {
		t.Fatalf("a cc session loads no key and is not given the file: %s", cc)
	}
	if strings.Contains(ccy, "fixture pass") {
		t.Fatal("the path is passed, never the passphrase")
	}
	if _, still := h.env[unlockFileEnv]; still {
		t.Fatal("the variable is removed from this process before tmux can inherit it")
	}
}

func TestABadPassphraseFileStartsNothing(t *testing.T) {
	cases := []struct {
		name  string
		setup func(h *harness, file string)
		want  string
	}{
		{"missing", func(h *harness, file string) {
			if err := os.Remove(file); err != nil {
				h.t.Fatal(err)
			}
		}, "the SSH key passphrase file for session restore is missing: "},
		{"loose mode", func(h *harness, file string) {
			chmod(h.t, file, 0o640)
		}, "has mode 640; it must be readable by you alone (600)."},
		{"empty", func(h *harness, file string) {
			if err := os.WriteFile(file, nil, 0o600); err != nil {
				h.t.Fatal(err)
			}
		}, "is unreadable or empty."},
		{"someone else's", func(h *harness, _ string) {
			h.app.Getuid = func() int { return os.Getuid() + 1 }
		}, "is owned by uid "},
		{"a directory", func(h *harness, file string) {
			if err := os.Remove(file); err != nil {
				h.t.Fatal(err)
			}
			if err := os.Mkdir(file, 0o700); err != nil {
				h.t.Fatal(err)
			}
		}, "is missing: "},
	}
	for _, c := range cases {
		h, file := passphraseHost(t)
		c.setup(h, file)
		exitCodeIs(t, h.run("restore"), 1, h)
		contains(t, c.name, h.stderr(), c.want, "play-claude-yolo.yml",
			"ERROR: nothing is restored: every ccy session would stop at its SSH key passphrase prompt.")
		if len(startedSessions(h)) != 0 {
			t.Fatalf("%s: nothing started", c.name)
		}
	}
}

func TestAReadOnlyPassphraseFileIsAcceptedAndASetuidOneIsNot(t *testing.T) {
	h, file := passphraseHost(t)
	if err := os.Chmod(file, 0o400); err != nil {
		t.Fatal(err)
	}
	exitCodeIs(t, h.run("restore"), 0, h)

	h, file = passphraseHost(t)
	if err := os.Chmod(file, os.ModeSetuid|0o600); err != nil {
		t.Fatal(err)
	}
	exitCodeIs(t, h.run("restore"), 1, h)
	contains(t, "setuid", h.stderr(), "has mode 4600")
}

func TestAnUnstattablePassphraseFileIsReported(t *testing.T) {
	h, file := passphraseHost(t)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file, file); err != nil {
		t.Fatal(err)
	}
	exitCodeIs(t, h.run("restore"), 1, h)
	contains(t, "loop", h.stderr(), "nothing is restored")
}
