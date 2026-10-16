package sessions

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/transcript"
)

const (
	floor        = "150000"
	compactText  = "/compact This session was restored after a reboot. After compacting, carry on with the work that was in progress."
	continueText = "continue"
)

var rule = strings.Repeat("─", 60)

// promptScreen is Claude's empty input box, drawn.
var promptScreen = strings.Join([]string{"  Claude Code", rule, "❯ ", rule, "  ⏵⏵ bypass permissions on (shift+tab to cycle)"}, "\n") + "\n"

// goingHost is a host for set-going: the floor set, no waiting, and the tmux server answering
// capture-pane from screens and send-keys with success.
func goingHost(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.env["CCY_RESTORE_COMPACT_FLOOR_TOKENS"] = floor
	h.env["CCY_SESSIONS_SET_GOING_WAIT"] = "0"
	h.env["CCY_SESSIONS_SUBMIT_DELAY"] = "0"
	h.fake.On([]string{"tmux", "-L", "ccy", "send-keys"}, execx.Result{})
	// This boot began an hour before the harness clock.
	procStat := filepath.Join(h.root, "proc-stat")
	content := fmt.Sprintf("cpu  1 2 3 4\nbtime %d\nprocesses 9\n", h.clock.Add(-time.Hour).Unix())
	if err := os.WriteFile(procStat, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	h.env["CCY_PROC_STAT_FILE"] = procStat
	return h
}

// showScreen sets what a session's pane shows, for the escape-keeping capture set-going takes and
// the plain one verify-restore takes.
func showScreen(h *harness, name, text string) {
	h.fake.On([]string{"tmux", "-L", "ccy", "capture-pane", "-p", "-e", "-t", "=" + name + ":"}, execx.Result{Stdout: []byte(text)})
	h.fake.On([]string{"tmux", "-L", "ccy", "capture-pane", "-p", "-t", "=" + name + ":"}, execx.Result{Stdout: []byte(text)})
}

// writeTranscript writes a conversation whose last assistant message re-sends tokens.
func writeTranscript(t *testing.T, path string, tokens int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"type":"assistant","message":{"model":"claude-opus-5-5","usage":{"input_tokens":2,"cache_creation_input_tokens":0,"cache_read_input_tokens":%d}}}`+"\n", tokens-2)
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
}

// appendTo adds lines to a file.
func appendTo(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Clean(path), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}

// stamp is a transcript timestamp for the harness clock plus offset.
func stamp(h *harness, offset time.Duration) string {
	return h.clock.Add(offset).UTC().Format("2006-01-02T15:04:05.000Z")
}

func tookInput(h *harness, path string) {
	appendTo(h.t, path, `{"type":"user","timestamp":"`+stamp(h, 0)+`","message":{"content":"continue"}}`)
}

func manifestStore(t *testing.T, h *harness) *restore.Store {
	t.Helper()
	regdir, err := registry.Dir(h.app.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	return restore.NewStore(regdir, h.env["CCY_BOOT_ID_FILE"])
}

// writeEntries writes the manifest for this boot.
func writeEntries(t *testing.T, h *harness, entries ...restore.Entry) {
	t.Helper()
	if err := manifestStore(t, h).Write(entries); err != nil {
		t.Fatal(err)
	}
}

// pending is a restored session waiting to be set going.
func pending(name, prefix, dir string) restore.Entry {
	return restore.Entry{Name: name, Prefix: prefix, Dir: dir, Supervised: prefix == "ccy", Going: restore.GoingPending}
}

func entryNamed(t *testing.T, h *harness, name string) restore.Entry {
	t.Helper()
	m, err := manifestStore(t, h).Read()
	if err != nil {
		t.Fatal(err)
	}
	i := m.Index(name)
	if i < 0 {
		t.Fatalf("%s is not in the manifest", name)
	}
	return m.Entries[i]
}

// typedInto is what was sent to the session's pane, as "literal text|Enter".
func typedInto(h *harness, name string) string {
	var typed []string
	for _, c := range h.fake.Calls() {
		line := c.Line()
		if !strings.HasPrefix(line, "tmux -L ccy send-keys -t ="+name+": ") {
			continue
		}
		typed = append(typed, strings.TrimPrefix(line, "tmux -L ccy send-keys -t ="+name+": "))
	}
	return strings.Join(typed, "|")
}

func ccyTranscript(dir string) string {
	return dir + "/.claude/ccy/projects/-workspace/conversation.jsonl"
}

func TestSetGoingTypesEachRestoredSessionItsFirstInput(t *testing.T) {
	h := goingHost(t)
	a, b := realDir(t, h, "a"), realDir(t, h, "b")
	tA := ccyTranscript(a)
	writeTranscript(t, tA, 208525)
	h.env["HOME"] = filepath.Join(h.root, "home")
	tC, err := transcript.Dir("cc", b, h.env["HOME"]+"/.claude")
	if err != nil {
		t.Fatal(err)
	}
	tC += "/conversation.jsonl"
	writeTranscript(t, tC, 5000)
	none := pending("ccy-b", "ccy", b)
	none.Going = restore.GoingNone
	writeEntries(t, h, pending("ccy-a", "ccy", a), pending("cc-c", "cc", b), none, pending("ccy-d", "ccy", b), pending("ccy-w", "ccy", a))
	h.sessions("ccy-a 0 "+a, "cc-c 0 "+b, "ccy-b 0 "+b, "ccy-d 0 "+b, "ccy-w 0 "+a)
	showScreen(h, "ccy-a", promptScreen)
	showScreen(h, "cc-c", promptScreen)
	showScreen(h, "ccy-b", promptScreen)
	showScreen(h, "ccy-d", "ccy exited with status 1. Press Enter to close this session.\n")
	showScreen(h, "ccy-w", "Found previous launch configuration\nUse same configuration? [Y/n]\n")

	exitCodeIs(t, h.run("set-going"), 1, h)
	if h.stdout() != "" {
		t.Fatalf("set-going prints nothing on stdout: %q", h.stdout())
	}
	// Above the floor: /compact then Enter on its own; below it: continue; a session the restore
	// found running, a dead one and one at a prompt are never typed into.
	wantTyped := map[string]string{"ccy-a": "-l " + compactText + "|Enter", "cc-c": "-l continue|Enter", "ccy-b": "", "ccy-d": "", "ccy-w": ""}
	for name, want := range wantTyped {
		if got := typedInto(h, name); got != want {
			t.Fatalf("%s was typed %q, want %q", name, got, want)
		}
	}
	contains(t, "report", h.stderr(),
		"left ccy-d untouched: launcher-exited\n",
		"left ccy-w alone, as expected: waiting-at-prompt-quick-launch-after-0s; "+whyWaitingAtPrompt+"\n",
		"set ccy-a going: compact (context 208525 tokens, compact floor 150000, supervised: yes).\n",
		"set cc-c going: continue (context 5000 tokens, compact floor 150000, supervised: no).\n",
		"1 restored session(s) were left alone as expected, each named above; that is not a fault.\n",
		"ERROR: 1 restored session(s) could not be set going: ccy-d (launcher-exited). verify-restore reports them.\n")
	at := strconv.FormatInt(h.clock.Unix(), 10)
	wantEntries := map[string]string{
		"ccy-a": "compact|" + at + "|208525|" + tA, "cc-c": "continue|" + at + "|5000|" + tC,
		"ccy-d": "untouched||launcher-exited|", "ccy-w": "untouched||waiting-at-prompt-quick-launch-after-0s|",
		"ccy-b": "none|||",
	}
	for name, want := range wantEntries {
		e := entryNamed(t, h, name)
		atField := ""
		if !e.At.IsZero() {
			atField = strconv.FormatInt(e.AtUnix(), 10)
		}
		if got := strings.Join([]string{string(e.Going), atField, e.Detail, e.Transcript}, "|"); got != want {
			t.Fatalf("%s: entry %q, want %q", name, got, want)
		}
	}

	typedBefore := h.count("tmux -L ccy send-keys")
	exitCodeIs(t, h.run("set-going"), 0, h)
	if h.count("tmux -L ccy send-keys") != typedBefore {
		t.Fatal("a second run types nothing")
	}
	contains(t, "second run", h.stderr(), "No session this boot's restore started is waiting to be set going.")
}

func TestSetGoingTakesNoArguments(t *testing.T) {
	h := goingHost(t)
	exitCodeIs(t, h.run("set-going", "now"), 64, h)
	if h.stderr() != "ccy-sessions: 'set-going' takes no arguments. See ccy-sessions --help.\n" {
		t.Fatalf("%q", h.stderr())
	}
}

func TestSetGoingWithoutAFloorLeavesEverythingAlone(t *testing.T) {
	h := goingHost(t)
	a := realDir(t, h, "a")
	h.env["CCY_RESTORE_COMPACT_FLOOR_TOKENS"] = ""
	writeEntries(t, h, pending("ccy-a", "ccy", a))
	h.sessions("ccy-a 0 " + a)
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "no floor", h.stderr(), "ccy_restore_compact_floor_tokens", "left ccy-a untouched: no-compact-floor",
		"CCY_RESTORE_COMPACT_FLOOR_TOKENS is '', not a whole number of tokens")
	if h.count("tmux -L ccy send-keys") != 0 {
		t.Fatal("nothing typed")
	}
	if e := entryNamed(t, h, "ccy-a"); e.Going != restore.GoingUntouched || e.Detail != "no-compact-floor" {
		t.Fatalf("%+v", e)
	}
	for _, bad := range []string{"0", "12x", "-5", "99999999999999999999"} {
		h = goingHost(t)
		h.env["CCY_RESTORE_COMPACT_FLOOR_TOKENS"] = bad
		writeEntries(t, h, pending("ccy-a", "ccy", a))
		exitCodeIs(t, h.run("set-going"), 1, h)
	}
}

// A session a person has, or must take, is an expected outcome: named with why, never a failure
// of the unit. A prompt that never draws is a fault.
func TestSetGoingLeavesABusyOrAttachedOrTypedOrUndrawnSessionAlone(t *testing.T) {
	cases := []struct {
		name     string
		screen   string
		attached int
		want     string
		exit     term.ExitCode
	}{
		{"attached", promptScreen, 1, "left ccy-a alone, as expected: attached; " + whyAttached, 0},
		{"typed", rule + "\n❯ half a thought\n" + rule + "\n", 0, "left ccy-a alone, as expected: input-box-not-empty; " + whyTyped, 0},
		{"not drawn", "Building image...\n", 0, "left ccy-a untouched: prompt-not-drawn-after-0s", 1},
	}
	for _, c := range cases {
		h := goingHost(t)
		a := realDir(t, h, "a")
		writeTranscript(t, ccyTranscript(a), 5000)
		writeEntries(t, h, pending("ccy-a", "ccy", a))
		h.sessions(fmt.Sprintf("ccy-a %d %s", c.attached, a))
		showScreen(h, "ccy-a", c.screen)
		exitCodeIs(t, h.run("set-going"), c.exit, h)
		contains(t, c.name, h.stderr(), c.want+"\n")
		if typedInto(h, "ccy-a") != "" {
			t.Fatalf("%s: nothing typed", c.name)
		}
		if e := entryNamed(t, h, "ccy-a"); e.Going != restore.GoingUntouched {
			t.Fatalf("%s: recorded %+v, want untouched", c.name, e)
		}
	}
}

const busyScreen = "✻ Thinking… (3s · esc to interrupt)\n"

// busyHost is one pending session ccy-a in <root>/a whose transcript is written; its screen is
// the test's to set.
func busyHost(t *testing.T) *harness {
	t.Helper()
	h := goingHost(t)
	a := realDir(t, h, "a")
	writeTranscript(t, ccyTranscript(a), 5000)
	writeEntries(t, h, pending("ccy-a", "ccy", a))
	h.sessions("ccy-a 0 " + a)
	return h
}

func busyTranscript(h *harness) string {
	return ccyTranscript(filepath.Join(h.root, "a"))
}

// A spinner is not evidence of work: busy is expected only when the transcript shows input
// taken since this boot began.
func TestSetGoingLeavesABusySessionThatTookInputSinceTheBootAlone(t *testing.T) {
	h := busyHost(t)
	tookInput(h, busyTranscript(h))
	showScreen(h, "ccy-a", busyScreen+promptScreen)
	exitCodeIs(t, h.run("set-going"), 0, h)
	contains(t, "busy with input", h.stderr(), "left ccy-a alone, as expected: busy-before-set-going; "+whyBusy+"\n")
	if typedInto(h, "ccy-a") != "" {
		t.Fatal("nothing typed over its work")
	}
	if e := entryNamed(t, h, "ccy-a"); e.Going != restore.GoingUntouched || e.Detail != "busy-before-set-going" {
		t.Fatalf("%+v", e)
	}
}

// A spinner with no input since the boot (a launch still loading) is waited on, and the session
// is set going once its prompt is idle.
func TestSetGoingWaitsOnABusySessionWithNoInputAndSetsItGoingWhenIdle(t *testing.T) {
	h := busyHost(t)
	// Input from before this boot is the conversation's past, not work since the restore.
	appendTo(t, busyTranscript(h), `{"type":"user","timestamp":"`+stamp(h, -2*time.Hour)+`","message":{"content":"old"}}`)
	h.env["CCY_SESSIONS_SET_GOING_WAIT"] = "30"
	looks := 0
	h.fake.OnFunc([]string{"tmux", "-L", "ccy", "capture-pane"}, func(execx.Cmd) (execx.Result, error) {
		looks++
		if looks < 2 {
			return execx.Result{Stdout: []byte(busyScreen + promptScreen)}, nil
		}
		return execx.Result{Stdout: []byte(promptScreen)}, nil
	})
	exitCodeIs(t, h.run("set-going"), 0, h)
	if typedInto(h, "ccy-a") != "-l continue|Enter" || looks != 2 {
		t.Fatalf("typed %q after %d looks", typedInto(h, "ccy-a"), looks)
	}
}

// Still busy with no input when the wait runs out: a fault (a hung turn looks like this).
func TestSetGoingFailsForABusySessionWithNoInputAtTheDeadline(t *testing.T) {
	h := busyHost(t)
	h.env["CCY_SESSIONS_SET_GOING_WAIT"] = "5"
	showScreen(h, "ccy-a", busyScreen+promptScreen)
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "busy at deadline", h.stderr(), "left ccy-a untouched: busy-without-input-after-5s\n",
		"ERROR: 1 restored session(s) could not be set going: ccy-a (busy-without-input-after-5s).")
}

// Without this boot's start time a spinner cannot be judged: a fault, named.
func TestSetGoingFailsForABusySessionWhenTheBootTimeCannotBeRead(t *testing.T) {
	h := busyHost(t)
	tookInput(h, busyTranscript(h))
	missing := filepath.Join(h.root, "no-proc-stat")
	h.env["CCY_PROC_STAT_FILE"] = missing
	showScreen(h, "ccy-a", busyScreen+promptScreen)
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "no boot time", h.stderr(), "this boot's start time could not be read from "+missing,
		"left ccy-a untouched: boot-time-unreadable\n")
}

// A look set-going does not know is never an expected outcome: leaving it alone is a fault.
func TestAnUnknownLookIsNeverExpected(t *testing.T) {
	for _, k := range []lookKind{lookKind(99), lookReady, lookUnreadable, lookDead, lookNotDrawn, lookBusy} {
		if why := (look{kind: k}).expectedWhy(); why != "" {
			t.Fatalf("look %d: why %q, want none", k, why)
		}
	}
}

// A session still at a launch prompt when the wait runs out (a desktop's SSH passphrase nobody
// typed) is handed to a person: the unit succeeds, and says so.
func TestSetGoingHandsASessionAtAPromptToAPersonWithoutFailing(t *testing.T) {
	h := goingHost(t)
	h.env["CCY_SESSIONS_SET_GOING_WAIT"] = "1200"
	a := realDir(t, h, "a")
	writeEntries(t, h, pending("ccy-w", "ccy", a))
	h.sessions("ccy-w 0 " + a)
	showScreen(h, "ccy-w", "Enter passphrase for key 'the-key':\n")
	exitCodeIs(t, h.run("set-going"), 0, h)
	contains(t, "handed", h.stderr(),
		"left ccy-w alone, as expected: waiting-at-prompt-ssh-passphrase-after-1200s; "+whyWaitingAtPrompt+"\n",
		"1 restored session(s) were left alone as expected, each named above; that is not a fault.\n")
	if strings.Contains(h.stderr(), "ERROR") {
		t.Fatalf("an expected outcome is not an error: %q", h.stderr())
	}
	if e := entryNamed(t, h, "ccy-w"); e.Going != restore.GoingUntouched || e.Detail != "waiting-at-prompt-ssh-passphrase-after-1200s" {
		t.Fatalf("%+v", e)
	}
}

// Every fault is named in the failure, and an expected outcome beside them is not.
func TestSetGoingNamesEveryFaultAndFailsForThem(t *testing.T) {
	h := goingHost(t)
	a := realDir(t, h, "a")
	writeEntries(t, h, pending("ccy-d", "ccy", a), pending("ccy-g", "ccy", a), pending("ccy-w", "ccy", a))
	h.sessions("ccy-d 0 "+a, "ccy-w 1 "+a)
	showScreen(h, "ccy-d", "ccy exited with status 1. Press Enter to close this session.\n")
	showScreen(h, "ccy-w", promptScreen)
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "faults", h.stderr(),
		"left ccy-d untouched: launcher-exited\n",
		"left ccy-g untouched: session-not-running\n",
		"left ccy-w alone, as expected: attached; "+whyAttached+"\n",
		"ERROR: 2 restored session(s) could not be set going: ccy-d (launcher-exited), ccy-g (session-not-running). verify-restore reports them.\n")
}

func TestSetGoingKeepsWaitingForAPromptThatDrawsLater(t *testing.T) {
	h := goingHost(t)
	h.env["CCY_SESSIONS_SET_GOING_WAIT"] = "30"
	a := realDir(t, h, "a")
	writeTranscript(t, ccyTranscript(a), 5000)
	writeEntries(t, h, pending("ccy-a", "ccy", a))
	h.sessions("ccy-a 0 " + a)
	looks := 0
	h.fake.OnFunc([]string{"tmux", "-L", "ccy", "capture-pane"}, func(execx.Cmd) (execx.Result, error) {
		looks++
		if looks < 3 {
			return execx.Result{Stdout: []byte("Found previous launch configuration\nUse same configuration? [Y/n]\n")}, nil
		}
		return execx.Result{Stdout: []byte(promptScreen)}, nil
	})
	exitCodeIs(t, h.run("set-going"), 0, h)
	if typedInto(h, "ccy-a") != "-l continue|Enter" || looks != 3 {
		t.Fatalf("typed %q after %d looks", typedInto(h, "ccy-a"), looks)
	}
	// Two polls of five seconds, and the pause before Enter, which is nothing here.
	if len(h.pauses) != 3 || h.pauses[0] != 5*time.Second || h.pauses[1] != 5*time.Second || h.pauses[2] != 0 {
		t.Fatalf("pauses %v", h.pauses)
	}
}

func TestSetGoingGivesUpAtTheDeadlineNamingWhatItWaitedOn(t *testing.T) {
	h := goingHost(t)
	h.env["CCY_SESSIONS_SET_GOING_WAIT"] = "5"
	a := realDir(t, h, "a")
	writeEntries(t, h, pending("ccy-a", "ccy", a))
	h.sessions("ccy-a 0 " + a)
	showScreen(h, "ccy-a", "Building image...\n")
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "deadline", h.stderr(), "left ccy-a untouched: prompt-not-drawn-after-5s\n")
}

func TestSetGoingReasonsTranscriptsCannotBeUsed(t *testing.T) {
	h := goingHost(t)
	a, b := realDir(t, h, "a"), realDir(t, h, "b")
	writeTranscript(t, ccyTranscript(b), 5000)
	writeEntries(t, h, pending("ccy-a", "ccy", a), pending("ccy-b", "ccy", b))
	h.sessions("ccy-a 0 "+a, "ccy-b 0 "+b)
	showScreen(h, "ccy-a", promptScreen)
	showScreen(h, "ccy-b", promptScreen)
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "no transcript", h.stderr(), "there is no transcript directory "+a, "left ccy-a untouched: no-transcript\n")
	if typedInto(h, "ccy-b") != "-l continue|Enter" {
		t.Fatalf("the next session is still set going: %q", typedInto(h, "ccy-b"))
	}

	h = goingHost(t)
	appendTo(t, ccyTranscript(a), `{"type":"user","message":{"content":"hi"}}`)
	writeEntries(t, h, pending("ccy-a", "ccy", a))
	h.sessions("ccy-a 0 " + a)
	showScreen(h, "ccy-a", promptScreen)
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "no size", h.stderr(), "records no context size", "left ccy-a untouched: context-unreadable\n")
}

func TestSetGoingReadsAResumedConversationNotTheNewest(t *testing.T) {
	h := goingHost(t)
	a := realDir(t, h, "a")
	old := filepath.Join(a, ".claude/ccy/projects/-workspace/0a1b-old.jsonl")
	writeTranscript(t, old, 300000)
	if err := os.Chtimes(old, time.Unix(1_000, 0), time.Unix(1_000, 0)); err != nil {
		t.Fatal(err)
	}
	writeTranscript(t, ccyTranscript(a), 5000)
	e := pending("ccy-a", "ccy", a)
	e.Resume = "0a1b-old"
	writeEntries(t, h, e)
	h.sessions("ccy-a 0 " + a)
	showScreen(h, "ccy-a", promptScreen)
	exitCodeIs(t, h.run("set-going"), 0, h)
	if typedInto(h, "ccy-a") != "-l "+compactText+"|Enter" || entryNamed(t, h, "ccy-a").Transcript != old {
		t.Fatalf("typed %q entry %+v", typedInto(h, "ccy-a"), entryNamed(t, h, "ccy-a"))
	}

	h = goingHost(t)
	e.Resume = "no-such-conversation"
	writeEntries(t, h, e)
	h.sessions("ccy-a 0 " + a)
	showScreen(h, "ccy-a", promptScreen)
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "missing", h.stderr(), "left ccy-a untouched: no-transcript\n")
	if typedInto(h, "ccy-a") != "" {
		t.Fatal("not read from the newest")
	}
}

func TestSetGoingTypesIntoOneSessionPerConversation(t *testing.T) {
	h := goingHost(t)
	a := realDir(t, h, "a")
	writeTranscript(t, ccyTranscript(a), 5000)
	writeEntries(t, h, pending("ccy-a", "ccy", a), pending("ccy-a-2", "ccy", a))
	h.sessions("ccy-a 0 "+a, "ccy-a-2 0 "+a)
	showScreen(h, "ccy-a", promptScreen)
	showScreen(h, "ccy-a-2", promptScreen)
	exitCodeIs(t, h.run("set-going"), 0, h)
	contains(t, "shared", h.stderr(),
		"left ccy-a-2 alone, as expected: shares-conversation-with-ccy-a; only one session may drive a conversation, and ccy-a is set going on it\n")
	if typedInto(h, "ccy-a-2") != "" || typedInto(h, "ccy-a") != "-l continue|Enter" {
		t.Fatal("only the first session drives the conversation")
	}
}

func TestSetGoingLeavesASessionWhoseKeysCannotBeSent(t *testing.T) {
	h := goingHost(t)
	a := realDir(t, h, "a")
	writeTranscript(t, ccyTranscript(a), 5000)
	writeEntries(t, h, pending("ccy-a", "ccy", a))
	h.sessions("ccy-a 0 " + a)
	showScreen(h, "ccy-a", promptScreen)
	h.tmuxFail("send-keys", "pane is dead")
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "send", h.stderr(), "typing into ccy-a failed: pane is dead", "left ccy-a untouched: send-failed\n")

	h = goingHost(t)
	writeEntries(t, h, pending("ccy-a", "ccy", a))
	h.sessions("ccy-a 0 " + a)
	showScreen(h, "ccy-a", promptScreen)
	h.fake.OnFunc([]string{"tmux", "-L", "ccy", "send-keys"}, func(c execx.Cmd) (execx.Result, error) {
		if c.Args[len(c.Args)-1] == "Enter" {
			return execx.Result{ExitCode: 1, Stderr: []byte("pane is dead")}, nil
		}
		return execx.Result{}, nil
	})
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "enter", h.stderr(), "pressing Enter in ccy-a failed: pane is dead", "left ccy-a untouched: send-failed\n")
}

func TestSetGoingNamesASessionGoneBeforeItsPromptDrew(t *testing.T) {
	h := goingHost(t)
	a := realDir(t, h, "a")
	writeEntries(t, h, pending("ccy-a", "ccy", a))
	h.sessions()
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "gone", h.stderr(), "left ccy-a untouched: session-not-running\n")
}

func TestSetGoingNamesAScreenThatCannotBeRead(t *testing.T) {
	h := goingHost(t)
	a := realDir(t, h, "a")
	writeEntries(t, h, pending("ccy-a", "ccy", a))
	h.sessions("ccy-a 0 " + a)
	h.tmuxFail("capture-pane", "can't find pane")
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "screen", h.stderr(), "the screen of ccy-a could not be read", "left ccy-a untouched: screen-unreadable\n")
}

func TestSetGoingRefusesWithoutThisBootsManifest(t *testing.T) {
	h := goingHost(t)
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "none", h.stderr(), "no session restore has been recorded",
		"this boot's restore recorded no sessions, so nothing can be set going; journalctl --user -u ccy-sessions-restore -b --no-pager | cat says why.")

	h = goingHost(t)
	a := realDir(t, h, "a")
	if err := os.WriteFile(h.env["CCY_BOOT_ID_FILE"], []byte("earlier-boot\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeEntries(t, h, pending("ccy-a", "ccy", a))
	if err := os.WriteFile(h.env["CCY_BOOT_ID_FILE"], []byte("this-boot\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.sessions("ccy-a 0 " + a)
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "earlier boot", h.stderr(), "the restore manifest is from an earlier boot", "journalctl --user -u ccy-sessions-restore")
	if h.count("tmux -L ccy send-keys") != 0 {
		t.Fatal("nothing typed")
	}

	h = goingHost(t)
	writeEntries(t, h, pending("ccy-a", "ccy", a))
	h.env["CCY_BOOT_ID_FILE"] = filepath.Join(h.root, "no-boot-id")
	h.app.cfg.BootIDFile = ""
	h.env["CCY_BOOT_ID_FILE"] = filepath.Join(h.root, "no-boot-id")
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "no boot id", h.stderr(), "this boot's id could not be read")
}

func TestSetGoingNamesASessionListThatCannotBeRead(t *testing.T) {
	h := goingHost(t)
	a := realDir(t, h, "a")
	writeEntries(t, h, pending("ccy-a", "ccy", a))
	h.tmuxFail("list-sessions", "lost server")
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "list", h.stderr(), "the live session list could not be read, so no further session can be set going.",
		"left ccy-a untouched: session-list-unreadable\n")
}

func TestSetGoingWaitsForAnotherSetGoingAndGivesUpAtItsDeadline(t *testing.T) {
	h := goingHost(t)
	a := realDir(t, h, "a")
	writeEntries(t, h, pending("ccy-a", "ccy", a))
	lock, err := os.OpenFile(manifestStore(t, h).Path+".set-going.lock", os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := lock.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "lock", h.stderr(), "another set-going still holds", "nothing was set going by this one")
}

func TestSetGoingMentionsAnEntryAnotherRestoreRewrote(t *testing.T) {
	h := goingHost(t)
	a := realDir(t, h, "a")
	writeTranscript(t, ccyTranscript(a), 5000)
	writeEntries(t, h, pending("ccy-a", "ccy", a))
	h.sessions("ccy-a 0 " + a)
	showScreen(h, "ccy-a", promptScreen)
	h.fake.OnFunc([]string{"tmux", "-L", "ccy", "send-keys"}, func(execx.Cmd) (execx.Result, error) {
		writeEntries(t, h, restore.Entry{Name: "ccy-a", Prefix: "ccy", Dir: a, Going: restore.GoingNone})
		return execx.Result{}, nil
	})
	exitCodeIs(t, h.run("set-going"), 0, h)
	contains(t, "rewritten", h.stderr(), "ccy-a: its entry in the restore manifest changed while it was being set going (a second restore this boot); that entry stands.")
}

func TestSetGoingStopsWhenTheManifestCannotBeUpdated(t *testing.T) {
	h := goingHost(t)
	a := realDir(t, h, "a")
	writeTranscript(t, ccyTranscript(a), 5000)
	writeEntries(t, h, pending("ccy-a", "ccy", a))
	h.sessions("ccy-a 0 " + a)
	showScreen(h, "ccy-a", promptScreen)
	h.fake.OnFunc([]string{"tmux", "-L", "ccy", "send-keys"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{}, os.Remove(manifestStore(t, h).Path)
	})
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "update", h.stderr(), "the restore manifest could not be updated for ccy-a.")
}

// unsupervised is a cc session above the floor: /compact now, continue after it ends.
func unsupervised(t *testing.T, h *harness) string {
	t.Helper()
	b := realDir(t, h, "b")
	conv, err := transcript.Dir("cc", b, h.env["HOME"]+"/.claude")
	if err != nil {
		t.Fatal(err)
	}
	conv += "/conversation.jsonl"
	writeTranscript(t, conv, 300000)
	writeEntries(t, h, pending("cc-c", "cc", b))
	h.sessions("cc-c 0 " + b)
	showScreen(h, "cc-c", promptScreen)
	return conv
}

func TestAnUnsupervisedSessionIsTypedContinueOnceItsCompactionIsInTheTranscript(t *testing.T) {
	h := goingHost(t)
	h.env["CCY_SESSIONS_SET_GOING_WAIT"] = "5"
	conv := unsupervised(t, h)
	// The boundary is written ahead of the size it left and dated a minute from now, so the next
	// look sees the compaction finished.
	boundary := `{"type":"system","subtype":"compact_boundary","timestamp":"` + stamp(h, time.Minute) + `","compactMetadata":{"postTokens":9000}}`
	assistant := `{"type":"assistant","message":{"model":"m","usage":{"input_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":300000}}}`
	if err := os.WriteFile(conv, []byte(boundary+"\n"+assistant+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	exitCodeIs(t, h.run("set-going"), 0, h)
	if got := typedInto(h, "cc-c"); got != "-l "+compactText+"|Enter|-l continue|Enter" {
		t.Fatalf("typed %q", got)
	}
	if e := entryNamed(t, h, "cc-c"); e.Going != restore.GoingContinue {
		t.Fatalf("%+v", e)
	}
	contains(t, "after compaction", h.stderr(), "set cc-c going after its compaction: continue.\n")
}

func TestACompactionThatNeverFinishesLeavesTheSessionAlone(t *testing.T) {
	h := goingHost(t)
	unsupervised(t, h)
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "unfinished", h.stderr(), "left cc-c untouched: compaction-not-finished-after-0s\n")
	if e := entryNamed(t, h, "cc-c"); e.Going != restore.GoingUntouched {
		t.Fatalf("%+v", e)
	}
}

// compacting is a cc session already typed /compact, with the boundary now in its transcript.
func compacting(t *testing.T, h *harness) string {
	t.Helper()
	b := realDir(t, h, "b")
	conv, err := transcript.Dir("cc", b, h.env["HOME"]+"/.claude")
	if err != nil {
		t.Fatal(err)
	}
	conv += "/conversation.jsonl"
	writeTranscript(t, conv, 300000)
	appendTo(t, conv, `{"type":"system","subtype":"compact_boundary","timestamp":"`+stamp(h, time.Minute)+`","compactMetadata":{"postTokens":9000}}`)
	e := pending("cc-c", "cc", b)
	e.Going, e.At, e.Detail, e.Transcript = restore.GoingCompacting, time.Unix(h.clock.Unix(), 0), "300000", conv
	writeEntries(t, h, e)
	h.sessions("cc-c 0 " + b)
	return conv
}

func TestAfterACompactionWhatTheScreenShowsDecidesWhatHappens(t *testing.T) {
	cases := []struct {
		name     string
		screen   string
		attached int
		want     string
		going    restore.Going
		exit     term.ExitCode
	}{
		{"busy", "✻ Thinking… (3s · esc to interrupt)\n" + promptScreen, 0, "cc-c is already working after its compaction; nothing more typed.", restore.GoingCompact, 0},
		{"attached", promptScreen, 1, "left cc-c alone, as expected: attached-after-compaction; " + whyAttached + "\n", restore.GoingUntouched, 0},
		{"typed", rule + "\n❯ half\n" + rule + "\n", 0, "left cc-c alone, as expected: typed-after-compaction; " + whyTyped + "\n", restore.GoingUntouched, 0},
		{"dead", "cc exited with status 1. Press Enter to close this session.\n", 0, "left cc-c untouched: launcher-exited-after-compaction\n", restore.GoingUntouched, 1},
		{"waiting", "Select token\n", 0, "left cc-c alone, as expected: token-select-after-compaction; " + whyWaitingAfterCompaction + "\n", restore.GoingUntouched, 0},
	}
	for _, c := range cases {
		h := goingHost(t)
		compacting(t, h)
		h.sessions(fmt.Sprintf("cc-c %d %s", c.attached, filepath.Join(h.root, "b")))
		showScreen(h, "cc-c", c.screen)
		exitCodeIs(t, h.run("set-going"), c.exit, h)
		contains(t, c.name, h.stderr(), c.want)
		if got := entryNamed(t, h, "cc-c").Going; got != c.going {
			t.Fatalf("%s: going %s", c.name, got)
		}
		if typedInto(h, "cc-c") != "" {
			t.Fatalf("%s: nothing is typed", c.name)
		}
	}
}

func TestAfterACompactionAPromptThatHasNotComeBackIsWaitedFor(t *testing.T) {
	h := goingHost(t)
	compacting(t, h)
	showScreen(h, "cc-c", "Compacting conversation...\n")
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "not back", h.stderr(), "left cc-c untouched: prompt-not-back-after-compaction-after-0s\n")
}

func TestAfterACompactionTheOtherEndingsAreNamed(t *testing.T) {
	h := goingHost(t)
	compacting(t, h)
	h.sessions()
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "gone", h.stderr(), "left cc-c untouched: session-ended-while-compacting\n")

	h = goingHost(t)
	conv := compacting(t, h)
	if err := os.Remove(conv); err != nil {
		t.Fatal(err)
	}
	showScreen(h, "cc-c", promptScreen)
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "unreadable", h.stderr(), "left cc-c untouched: context-unreadable\n")

	h = goingHost(t)
	compacting(t, h)
	h.tmuxFail("capture-pane", "can't find pane")
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "screen", h.stderr(), "left cc-c untouched: unreadable-after-compaction\n")

	h = goingHost(t)
	compacting(t, h)
	showScreen(h, "cc-c", promptScreen)
	h.tmuxFail("send-keys", "pane is dead")
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "send", h.stderr(), "left cc-c untouched: send-failed-after-compaction\n")
}

func TestSetGoingHonoursTheConfiguredClaudeConfigHome(t *testing.T) {
	h := goingHost(t)
	b := realDir(t, h, "b")
	cfgHome := filepath.Join(h.root, "claude-config")
	h.env["CLAUDE_CONFIG_DIR"] = cfgHome
	conv, err := transcript.Dir("cc", b, cfgHome)
	if err != nil {
		t.Fatal(err)
	}
	writeTranscript(t, conv+"/conversation.jsonl", 100)
	writeEntries(t, h, pending("cc-c", "cc", b))
	h.sessions("cc-c 0 " + b)
	showScreen(h, "cc-c", promptScreen)
	exitCodeIs(t, h.run("set-going"), 0, h)
	if typedInto(h, "cc-c") != "-l continue|Enter" {
		t.Fatalf("typed %q", typedInto(h, "cc-c"))
	}
}

func TestSetGoingTakesAnUnknownLauncherAsDead(t *testing.T) {
	h := goingHost(t)
	a := realDir(t, h, "a")
	writeEntries(t, h, pending("zz-a", "zz", a))
	h.sessions("zz-a 0 " + a)
	showScreen(h, "zz-a", promptScreen)
	exitCodeIs(t, h.run("set-going"), 1, h)
	contains(t, "unknown launcher", h.stderr(), "left zz-a untouched: unknown-launcher-zz\n")
}
