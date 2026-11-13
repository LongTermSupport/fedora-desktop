package sessions

import (
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/conversation"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/transcript"
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

// record writes a session record the way a launcher does and returns its path; when the
// session's folder is there, its conversation is recorded too, as its SessionStart hook does
// (conversationOf), with that conversation's transcript.
func record(t *testing.T, h *harness, rec registry.Record) string {
	t.Helper()
	file := recordOnly(t, h, rec)
	if info, err := os.Stat(rec.Dir); err == nil && info.IsDir() {
		converse(t, h, rec)
	}
	return file
}

// recordOnly writes the record and nothing the session's hook would.
func recordOnly(t *testing.T, h *harness, rec registry.Record) string {
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

// conversationOf is the conversation id a test session is in: one per name.
func conversationOf(name string) string {
	return fmt.Sprintf("%08x-0000-4000-8000-000000000000", crc32.ChecksumIEEE([]byte(name)))
}

// converse records the session's conversation (Plan 00203 SPEC-team-add-and-restart C1) and
// writes its transcript; a cc session's folder is made its own git checkout's top.
func converse(t *testing.T, h *harness, rec registry.Record) {
	t.Helper()
	id := conversationOf(rec.Name)
	if rec.Prefix == "cc" {
		h.write(strings.TrimPrefix(filepath.Join(rec.Dir, ".git", "HEAD"), h.root+"/"), "ref: refs/heads/main\n")
	}
	body := `{"v":1,"session":"` + rec.Name + `","session_id":"` + id + `","source":"startup","written_ms":1}`
	h.write(strings.TrimPrefix(filepath.Join(membership.SessionDir(rec.Dir, rec.Name), conversation.File), h.root+"/"), body)
	tdir, err := transcript.Dir(rec.Prefix, rec.Dir, conversation.ClaudeHome(h.app.Getenv))
	if err != nil {
		t.Fatal(err)
	}
	h.write(strings.TrimPrefix(filepath.Join(tdir, id+".jsonl"), h.root+"/"), "{}\n")
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
	record(t, h, registry.Record{Name: "cc-a", Dir: a, Launcher: "/launch/cc", Prefix: "cc", Restore: true, Args: []string{"--bus"}})
	exitCodeIs(t, h.run("restore"), 0, h)
	calls := startedSessions(h)
	if len(calls) != 2 {
		t.Fatalf("two sessions started: %v", h.ran())
	}
	// Spec bug 9.13: the trampoline names the session's own launcher, not always ccy.
	contains(t, "cc-a", strings.Join(calls[0].Args, " "), "--unit ccy-tmux-", "-s cc-a -c "+a+" -- bash -c ",
		" ccy-tmux env CCY_SESSION_RESTORE=1 /launch/cc --bus --resume "+conversationOf("cc-a")+" ; set-hook -g client-attached ", "cc exited with status")
	// Plan 00203 C6: the recorded conversation, by id; the record's own --resume is replaced.
	contains(t, "ccy-b", strings.Join(calls[1].Args, " "),
		" ccy-tmux env CCY_SESSION_RESTORE=1 /launch/ccy --compose start --supervise --resume "+conversationOf("ccy-b")+" ")
	for _, c := range calls {
		if slices.Contains(c.Args, "--continue") || strings.Contains(strings.Join(c.Args, " "), "--continue") {
			t.Fatalf("a restore never continues: %v", c.Args)
		}
	}
	contains(t, "progress", h.stderr(), "restored cc-a in "+a+".\n", "restored ccy-b in "+b+".\n", "ccy session restore: 2 started, 0 failed.\n")
	m := manifestOf(t, h)
	entries := make([]string, 0, len(m.Entries))
	for _, e := range m.Entries {
		entries = append(entries, fmt.Sprintf("%s %s %s resume=%q supervised=%v %s", e.Name, e.Prefix, e.Dir, e.Resume, e.Supervised, e.Going))
	}
	want := []string{
		"cc-a cc " + a + ` resume="` + conversationOf("cc-a") + `" supervised=false pending`,
		"ccy-b ccy " + b + ` resume="` + conversationOf("ccy-b") + `" supervised=true pending`,
	}
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
	contains(t, "dry run", h.stderr(), "would start ccy-a in "+a+": /launch/ccy --supervise --resume "+conversationOf("ccy-a")+"\n",
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

// Plan 00215: an opencode session (ccy --opencode or -oc before any "--") is not brought back,
// even with its restore mark on: it has no Claude conversation to resume, and a resume would
// hand opencode Claude's flags. A word after "--" is claude's and does not count.
func TestRestoreSkipsOpencodeSessions(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	a := realDir(t, h, "a")
	record(t, h, registry.Record{Name: "ccy-o", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true, Args: []string{"--token", "work", "--opencode"}})
	record(t, h, registry.Record{Name: "ccy-s", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true, Args: []string{"-oc"}})
	exitCodeIs(t, h.run("restore"), 0, h)
	contains(t, "skips", h.stderr(),
		"skip ccy-o: an opencode session is not restored (Plan 00215); start it again with ccy --opencode.\n",
		"skip ccy-s: an opencode session is not restored (Plan 00215); start it again with ccy --opencode.\n")
	if len(startedSessions(h)) != 0 {
		t.Fatal("nothing is started")
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
	// fedora-desktop#108: the failure is in the manifest, so verify-restore reports it.
	if got := goingOf(manifestOf(t, h)); !slices.Equal(got, []string{"ccy-b not-started directory-gone", "ccy-c pending "}) {
		t.Fatalf("%q", got)
	}
}

// goingOf is each manifest entry as "<name> <going> <detail>".
func goingOf(m restore.Manifest) []string {
	out := make([]string, 0, len(m.Entries))
	for _, e := range m.Entries {
		out = append(out, e.Name+" "+string(e.Going)+" "+e.Detail)
	}
	return out
}

// Plan 00203 C6, Decision 10: a session is resumed by its recorded conversation or not at all.
// One with none (its hook never wrote it, or a launch without the bus) is a failure that names
// it and says how to bring it back by hand; its record is kept; the others go on.
func TestASessionWithNoRecordedConversationIsNotRestored(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	a, b, c := realDir(t, h, "a"), realDir(t, h, "b"), realDir(t, h, "c")
	none := recordOnly(t, h, registry.Record{Name: "ccy-none", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	noBus := record(t, h, registry.Record{Name: "ccy-nobus", Dir: b, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true, Args: []string{"--no-bus"}})
	record(t, h, registry.Record{Name: "ccy-ok", Dir: c, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	exitCodeIs(t, h.run("restore"), 1, h)
	contains(t, "none", h.stderr(),
		"ERROR: cannot restore ccy-none: ccy-none: no conversation is recorded",
		"resume it by hand: ccy --resume in "+a+", whose picker lists the conversations (the record is kept at "+none+").",
		"ERROR: cannot restore ccy-nobus: ccy-nobus: it was launched without the agent team bus",
		"(the record is kept at "+noBus+").",
		"restored ccy-ok in "+c+".", "ccy session restore: 1 started, 2 failed.")
	calls := startedSessions(h)
	if len(calls) != 1 || !strings.Contains(strings.Join(calls[0].Args, " "), "--resume "+conversationOf("ccy-ok")) {
		t.Fatalf("only ccy-ok starts: %v", h.ran())
	}
	for _, f := range []string{none, noBus} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("the record is kept: %v", err)
		}
	}
	want := []string{"ccy-nobus not-started no-conversation", "ccy-none not-started no-conversation", "ccy-ok pending "}
	if got := goingOf(manifestOf(t, h)); !slices.Equal(got, want) {
		t.Fatalf("%q", got)
	}
}

// Plan 00135 Phase 13 (the owner's decision of 2026-11-11, revising Phase 12 ask 1): a record
// provisioning marked fresh-if-no-conversation, with nothing recorded, is started fresh, under the
// same askpass a resume gets, with no --resume and never --continue; a marked record whose
// conversation is recorded is resumed by its id; an unmarked one with nothing recorded is refused
// as before; a marked one launched without the bus, or whose hook's file is not readable, is
// refused too (the mark covers only "nothing recorded"). The manifest says fresh, and set-going
// leaves the fresh session alone: there is no conversation to compact or continue.
func TestAMarkedRecordWithNoConversationIsStartedFreshUnderTheAskpass(t *testing.T) {
	h, file := passphraseHost(t)
	dir := realDir(t, h, "new")
	recordOnly(t, h, registry.Record{
		Name: "ccy-new", Dir: dir, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true,
		FreshIfNoConversation: true, Compose: "declined", Args: []string{"--ssh-key", "/k", "--continue"},
	})
	recordOnly(t, h, registry.Record{Name: "ccy-plain", Dir: dir, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	record(t, h, registry.Record{Name: "ccy-old", Dir: dir, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true, FreshIfNoConversation: true})
	recordOnly(t, h, registry.Record{
		Name: "ccy-nobus", Dir: dir, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true,
		FreshIfNoConversation: true, Args: []string{"--no-bus"},
	})
	exitCodeIs(t, h.run("restore"), 1, h)
	started := map[string]string{}
	for _, c := range startedSessions(h) {
		line := strings.Join(c.Args, " ")
		for _, name := range []string{"ccy-new", "ccy-old", "ccy-plain", "ccy-nobus", "ccy-proj", "cc-proj"} {
			if strings.Contains(line, " -s "+name+" ") {
				started[name] = line
			}
		}
	}
	contains(t, "fresh", started["ccy-new"],
		" ccy-tmux env CCY_SESSION_RESTORE=1 "+unlockFileEnv+"="+file+" /launch/ccy --ssh-key /k --compose skip --supervise ; ")
	lacks(t, "fresh", started["ccy-new"], "--resume", "--continue")
	contains(t, "marked, recorded", started["ccy-old"], "/launch/ccy --supervise --resume "+conversationOf("ccy-old")+" ")
	if _, ok := started["ccy-plain"]; ok {
		t.Fatal("an unmarked record with no conversation is not started")
	}
	if _, ok := started["ccy-nobus"]; ok {
		t.Fatal("the mark does not cover a launch without the bus")
	}
	contains(t, "says", h.stderr(),
		"started ccy-new fresh in "+dir+" (its record is marked fresh-if-no-conversation): no conversation is recorded (",
		"ERROR: cannot restore ccy-plain: ccy-plain: no conversation is recorded",
		"ERROR: cannot restore ccy-nobus: ccy-nobus: it was launched without the agent team bus",
		"ccy session restore: 4 started, 2 failed.")
	got := map[string]string{}
	for _, e := range manifestOf(t, h).Entries {
		got[e.Name] = fmt.Sprintf("resume=%q %s %s", e.Resume, e.Going, e.Detail)
	}
	want := map[string]string{
		"ccy-new":   `resume="" none fresh`,
		"ccy-old":   `resume="` + conversationOf("ccy-old") + `" pending `,
		"ccy-plain": `resume="" not-started no-conversation`,
		"ccy-nobus": `resume="" not-started no-conversation`,
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: manifest %q, want %q", name, got[name], w)
		}
	}
}

func TestADryRunSaysAMarkedRecordWouldStartFresh(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	a := realDir(t, h, "a")
	recordOnly(t, h, registry.Record{Name: "ccy-new", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true, FreshIfNoConversation: true})
	exitCodeIs(t, h.run("restore", "--dry-run"), 0, h)
	contains(t, "dry run", h.stderr(), "would start ccy-new fresh in "+a+" (no conversation to resume): /launch/ccy --supervise\n")
}

// --check judges a marked never-run record restorable, so the self-update cycle's
// unrestorable-sessions hold does not fire for it; an unmarked one is unrestorable.
func TestRestoreCheckJudgesAMarkedNeverRunRecordRestorable(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	restoreOn(h, true)
	a := realDir(t, h, "a")
	recordOnly(t, h, registry.Record{Name: "ccy-new", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true, FreshIfNoConversation: true})
	exitCodeIs(t, h.run("restore", "--check"), 0, h)
	if h.stdout() != "ccy-new RESTORABLE\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
	contains(t, "says fresh", h.stderr(), "ccy-new will be started fresh (its record is marked fresh-if-no-conversation): no conversation is recorded (")

	recordOnly(t, h, registry.Record{Name: "ccy-plain", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	h.out.Reset()
	exitCodeIs(t, h.run("restore", "--check"), 1, h)
	if h.stdout() != "ccy-new RESTORABLE\nccy-plain UNRESTORABLE no-conversation\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
}

// neverTookATurn records the session's conversation as its SessionStart hook does at startup,
// with no transcript: Claude Code writes none until the first turn.
func neverTookATurn(t *testing.T, h *harness, rec registry.Record) {
	t.Helper()
	converse(t, h, rec)
	tdir, err := transcript.Dir(rec.Prefix, rec.Dir, conversation.ClaudeHome(h.app.Getenv))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(tdir, conversationOf(rec.Name)+".jsonl")); err != nil {
		t.Fatal(err)
	}
}

// fedora-desktop#270: a declared session that never took a turn has a conversation.json naming a
// conversation with no transcript. A record marked fresh-if-no-conversation starts fresh then,
// as it does with nothing recorded: the check calls it RESTORABLE and says why, and the restore
// starts it with no --resume. Unmarked, it is still refused.
func TestAMarkedRecordWhoseSessionNeverTookATurnStartsFresh(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	restoreOn(h, true)
	a := realDir(t, h, "a")
	marked := registry.Record{Name: "ccy-new", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true, FreshIfNoConversation: true}
	recordOnly(t, h, marked)
	neverTookATurn(t, h, marked)
	exitCodeIs(t, h.run("restore", "--check"), 0, h)
	if h.stdout() != "ccy-new RESTORABLE\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
	contains(t, "says fresh", h.stderr(), "ccy-new will be started fresh (its record is marked fresh-if-no-conversation): the recorded conversation "+conversationOf("ccy-new"), "has no transcript")

	h.errOut.Reset()
	exitCodeIs(t, h.run("restore"), 0, h)
	calls := startedSessions(h)
	if len(calls) != 1 {
		t.Fatalf("one session starts: %v", h.ran())
	}
	lacks(t, "fresh", strings.Join(calls[0].Args, " "), "--resume")
	contains(t, "says", h.stderr(), "started ccy-new fresh in "+a+" (its record is marked fresh-if-no-conversation): the recorded conversation")

	plain := registry.Record{Name: "ccy-plain", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true}
	recordOnly(t, h, plain)
	neverTookATurn(t, h, plain)
	h.out.Reset()
	exitCodeIs(t, h.run("restore", "--check"), 1, h)
	if h.stdout() != "ccy-new RESTORABLE\nccy-plain UNRESTORABLE no-conversation\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
}

// fedora-desktop#270: a ccy record whose project has compose files and which holds no compose=
// answer would stop its first restore at the compose question, so --check calls it
// UNRESTORABLE no-compose-answer and names the fix. A provisioned record (marked
// fresh-if-no-conversation) is said to be provisioned without one; only an unmarked one may
// predate the key. An answered record, one launched with --no-network (it meets no compose
// question), a cc session and a project with no compose file are restorable. The restore itself
// still starts it: a person at an attended reboot can answer in the pane.
func TestRestoreCheckRefusesARecordWithNoComposeAnswerInAComposeProject(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	restoreOn(h, true)
	stack, plainDir := realDir(t, h, "stack"), realDir(t, h, "plain")
	h.write("stack/compose.yml", "services: {}\n")
	recs := []registry.Record{
		{Name: "ccy-answered", Dir: stack, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true, Compose: "declined"},
		{Name: "ccy-nonet", Dir: stack, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true, Args: []string{"--no-network"}},
		{Name: "ccy-old", Dir: stack, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true},
		{Name: "ccy-plain", Dir: plainDir, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true},
		{Name: "ccy-prov", Dir: stack, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true, FreshIfNoConversation: true},
		{Name: "cc-stack", Dir: stack, Launcher: "/launch/cc", Prefix: "cc", Restore: true, Args: []string{"--bus"}},
	}
	for _, rec := range recs {
		record(t, h, rec)
	}
	exitCodeIs(t, h.run("restore", "--check"), 1, h)
	want := "cc-stack RESTORABLE\nccy-answered RESTORABLE\nccy-nonet RESTORABLE\nccy-old UNRESTORABLE no-compose-answer\n" +
		"ccy-plain RESTORABLE\nccy-prov UNRESTORABLE no-compose-answer\n"
	if h.stdout() != want {
		t.Fatalf("stdout %q, want %q", h.stdout(), want)
	}
	contains(t, "why", h.stderr(),
		"ERROR: ccy-prov cannot be restored: its record was provisioned without a compose answer, and "+stack+" has compose files (compose.yml), so its first restore would stop at the compose question. Declare it with one: ccy-sessions declare --replace --name ccy-prov --fresh-if-no-conversation "+stack+" -- --compose start|skip ...",
		"ERROR: ccy-old cannot be restored: its record holds no compose answer (a ccy older than 3.89.1 wrote none, and a launch that never met the compose question writes none), and "+stack+" has compose files (compose.yml)",
		"Launch it once with --compose start|skip, or declare it with one: ccy-sessions declare --replace --name ccy-old "+stack+" -- --compose start|skip ...")
	lacks(t, "provisioned", strings.Split(h.stderr(), "ccy-prov cannot")[1], "3.89.1")

	h.errOut.Reset()
	exitCodeIs(t, h.run("restore"), 0, h)
	calls := startedSessions(h)
	if len(calls) != len(recs) {
		t.Fatalf("the restore starts every record (%d), a person can answer in the pane: %v", len(calls), h.ran())
	}
	why := 0
	for _, c := range calls {
		line := strings.Join(c.Args, " ")
		if strings.Contains(line, noComposeWhyEnv+"=") {
			why++
			contains(t, "provisioned, told", line, " -s ccy-prov ", noComposeWhyEnv+"="+noComposeProvisioned+" /launch/ccy")
		}
	}
	if why != 1 {
		t.Fatalf("only the provisioned record's pane is told why it is asked (%d): %v", why, h.ran())
	}
}

// fedora-desktop#108: every record failing is not an empty restore. The manifest names each
// session that did not start, and verify-restore reports them and exits 1; it never says
// "nothing to verify" for them.
func TestARestoreWhereEveryRecordFailsIsReportedByVerify(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	a := realDir(t, h, "a")
	recordOnly(t, h, registry.Record{Name: "ccy-a", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	recordOnly(t, h, registry.Record{Name: "ccy-b", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	exitCodeIs(t, h.run("restore"), 1, h)
	contains(t, "totals", h.stderr(), "ccy session restore: 0 started, 2 failed.")
	h.out.Reset()
	h.errOut.Reset()
	h.tmuxOut("list-panes", "")
	h.fake.On([]string{"ps"}, execx.Result{})
	exitCodeIs(t, h.run("verify-restore"), 1, h)
	if h.stdout() != "ccy-a NOT-STARTED no-conversation\nccy-b NOT-STARTED no-conversation\n" {
		t.Fatalf("stdout %q, stderr %q", h.stdout(), h.stderr())
	}
	lacks(t, "verify", h.stderr(), "nothing to verify")
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
	// It has no name to read, so its file's name stands for it.
	if got := goingOf(manifestOf(t, h)); !slices.Equal(got, []string{"ccy-bad not-started record-unreadable", "ccy-a pending "}) {
		t.Fatalf("%q", got)
	}
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

func TestASessionThatCannotBeStartedIsAFailureAndIsNotStartedInTheManifest(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	a := realDir(t, h, "a")
	file := record(t, h, registry.Record{Name: "ccy-a", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	h.fake.On([]string{"systemd-run"}, execx.Result{ExitCode: 1, Stderr: []byte("Failed to start scope")})
	exitCodeIs(t, h.run("restore"), 1, h)
	contains(t, "start failure", h.stderr(), "Failed to start scope", "ERROR: could not start ccy-a in "+a+" (the record is kept at "+file+").",
		"ccy session restore: 0 started, 1 failed.")
	if got := goingOf(manifestOf(t, h)); !slices.Equal(got, []string{"ccy-a not-started start-failed"}) {
		t.Fatalf("%q", got)
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

// fedora-desktop#108 Ask 2: `restore --check` judges every record the next boot's restore would
// start, live sessions included (they are what a reboot takes down), by the same rules as the
// restore, and starts nothing. One line each on stdout; exit 1 naming each one that cannot be
// restored.
func TestRestoreCheckJudgesEveryRecordLiveOnesIncluded(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	restoreOn(h, true)
	a := realDir(t, h, "a")
	gone := filepath.Join(h.root, "gone")
	record(t, h, registry.Record{Name: "ccy-ok", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	none := recordOnly(t, h, registry.Record{Name: "ccy-live", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	record(t, h, registry.Record{Name: "ccy-gone", Dir: gone, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	record(t, h, registry.Record{Name: "ccy-n", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: false})
	record(t, h, registry.Record{Name: "ccy-o", Dir: a, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true, Args: []string{"--opencode"}})
	regdir, err := registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(regdir, "ccy-bad"), []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.sessions("ccy-live 0 " + a)
	exitCodeIs(t, h.run("restore", "--check"), 1, h)
	want := "ccy-bad UNRESTORABLE record-unreadable\nccy-gone UNRESTORABLE directory-gone\n" +
		"ccy-live UNRESTORABLE no-conversation\nccy-ok RESTORABLE\n"
	if h.stdout() != want {
		t.Fatalf("stdout %q", h.stdout())
	}
	contains(t, "why", h.stderr(), "skip ccy-n: marked no-restore.", "skip ccy-o: an opencode session is not restored",
		"ERROR: ccy-live cannot be restored: ccy-live: no conversation is recorded", "(the record is kept at "+none+").",
		"ERROR: 3 recorded session(s) could not be restored after a reboot: ccy-bad, ccy-gone, ccy-live.")
	if len(startedSessions(h)) != 0 || h.count("tmux") != 0 {
		t.Fatalf("a check starts nothing and asks tmux nothing: %v", h.ran())
	}
	if _, statErr := os.Stat(restore.NewStore(regdir, "").Path); statErr == nil {
		t.Fatal("a check writes no manifest")
	}
}

func TestRestoreCheckPassesWhenEveryRecordCanBeRestored(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	restoreOn(h, true)
	record(t, h, registry.Record{Name: "ccy-ok", Dir: realDir(t, h, "a"), Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	exitCodeIs(t, h.run("restore", "--check"), 0, h)
	if h.stdout() != "ccy-ok RESTORABLE\n" {
		t.Fatalf("stdout %q", h.stdout())
	}
	contains(t, "summary", h.stderr(), "every recorded session can be restored after a reboot (1).")

	h = newHarness(t)
	restoreHost(h)
	restoreOn(h, true)
	exitCodeIs(t, h.run("restore", "--check"), 0, h)
	contains(t, "no registry", h.stderr(), "nothing to restore")
}

// With the restore off, the next boot restores nothing, so no record can hold anything up.
func TestRestoreCheckWithTheRestoreOffHasNothingToJudge(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	recordOnly(t, h, registry.Record{Name: "ccy-x", Dir: realDir(t, h, "a"), Launcher: "/launch/ccy", Prefix: "ccy", Restore: true})
	exitCodeIs(t, h.run("restore", "--check"), 0, h)
	if h.stdout() != "" {
		t.Fatalf("stdout %q", h.stdout())
	}
	contains(t, "off", h.stderr(), "session restore is off for this user (ccy-sessions-restore.service is not enabled), so the next boot restores no session; nothing to check.")
}

func TestRestoreCheckRefusesARegistryThatCannotBeListed(t *testing.T) {
	h := newHarness(t)
	restoreHost(h)
	restoreOn(h, true)
	regdir, err := registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(regdir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(regdir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	exitCodeIs(t, h.run("restore", "--check"), 1, h)
	contains(t, "unlistable", h.stderr(), "is not a readable directory")
	h.env["CCY_STATE_DIR"] = "relative"
	exitCodeIs(t, h.run("restore", "--check"), 1, h)
	contains(t, "relative", h.stderr(), "is not an absolute path")
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
		{[]string{"restore", "--check", "--dry-run"}, "unexpected argument '--dry-run'."},
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
	record(t, h, registry.Record{Name: "cc-proj", Dir: dir, Launcher: "/launch/cc", Prefix: "cc", Restore: true, Args: []string{"--teams", "1@alpha"}})
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
