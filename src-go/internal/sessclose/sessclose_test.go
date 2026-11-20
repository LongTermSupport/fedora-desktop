package sessclose

import (
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/screen"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

var rule = strings.Repeat("─", 60)

var (
	paneReady = strings.Join([]string{" ▐▛███▜▌   Claude Code", "", rule, "❯ ", rule, "  ⏵⏵ bypass permissions on"}, "\n")
	paneBusy  = strings.Join([]string{"✻ Compacting conversation… (12s · esc to interrupt)", "", rule, "❯ ", rule}, "\n")
	paneTyped = strings.Join([]string{rule, "❯ half a thought", rule}, "\n")
	// paneStarting draws no box: screen.ClaudeState reads it NotDrawn.
	paneStarting = "Building image...\n"
)

// fakeSession is one tmux session: its pane, and what a typed line does to it.
type fakeSession struct {
	name, pane, typed string
	// compactLooks is how many looks a compaction stays busy for; stuck never ends it.
	compactLooks int
	stuck        bool
	// noBoundary ends the compaction on the pane without writing its compact_boundary.
	noBoundary bool
	// turn is a turn running when the session is first looked at: busy for busyLeft looks,
	// then ready with nothing written to the transcript.
	turn     bool
	busyLeft int
	// afterTurn is the pane a turn leaves when it ends ("" for a ready box).
	afterTurn       string
	enters, escapes int
	// server is the key of the tmux server the session is on, "" for ccy's; ignoreExit leaves
	// it running after /exit.
	server     string
	ignoreExit bool
}

// world is a scripted tmux server, the session records, the transcripts and a clock that
// moves only when the Closer sleeps.
type world struct {
	t        *testing.T
	fake     *execx.Fake
	store    *registry.Store
	now      time.Time
	sessions []*fakeSession
	// boundary is when each transcript's last compaction finished (Unix seconds), last whether
	// nothing grew it since; readErr fails every read.
	boundary map[string]int64
	last     map[string]bool
	readErr  error
	findErr  error
	// listErr fails the session list of the servers it names, by key.
	listErr map[string]bool
	c       *Closer
}

func newWorld(t *testing.T, names ...string) *world {
	t.Helper()
	w := &world{
		t: t, fake: &execx.Fake{}, store: registry.New(t.TempDir()), now: time.Unix(1_800_000_000, 0),
		boundary: map[string]int64{}, last: map[string]bool{}, listErr: map[string]bool{},
	}
	for _, n := range names {
		w.sessions = append(w.sessions, &fakeSession{name: n, pane: paneReady, compactLooks: 3})
		w.record(n)
	}
	w.fakeTmux()
	w.c = &Closer{
		Tmux:        &tmuxccy.Tmux{Runner: w.fake, Term: &term.Term{Err: io.Discard}},
		Registry:    func() (*registry.Store, error) { return w.store, nil },
		Transcripts: w,
		Transcript: func(rec registry.Record) (string, error) {
			if w.findErr != nil {
				return "", w.findErr
			}
			return "/t/" + rec.Name + ".jsonl", nil
		},
		Now:   func() time.Time { return w.now },
		Sleep: func(d time.Duration) { w.now = w.now.Add(d) },
		Waits: DefaultWaits,
	}
	return w
}

func (w *world) CompactedLast(path string) (bool, error) {
	if w.readErr != nil {
		return false, w.readErr
	}
	return w.last[path], nil
}

func (w *world) CompactedSince(path string, since int64) (bool, error) {
	if w.readErr != nil {
		return false, w.readErr
	}
	at, ok := w.boundary[path]
	return ok && at >= since, nil
}

func (w *world) record(name string, args ...string) {
	if err := w.store.Write(registry.Record{Name: name, Dir: "/srv/proj", Launcher: "/x/ccy", Prefix: "ccy", Restore: true, Args: args}); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) find(name string) *fakeSession {
	for _, s := range w.sessions {
		if s.name == name {
			return s
		}
	}
	return nil
}

func targetOf(args []string) string {
	for i, a := range args {
		if a == "-t" && i+1 < len(args) {
			return strings.TrimSuffix(strings.TrimPrefix(args[i+1], "="), ":")
		}
	}
	return ""
}

func gone(name string) execx.Result {
	return execx.Result{ExitCode: 1, Stderr: []byte("can't find session: " + name)}
}

func (w *world) fakeTmux() { w.fakeServer("", "-L", "ccy") }

// fakeServer scripts the tmux server reached with server (its -L or -S argument), holding the
// sessions whose server field is key ("" for ccy's).
func (w *world) fakeServer(key string, server ...string) {
	on := func(cmd string) []string { return append(append([]string{"tmux"}, server...), cmd) }
	w.fake.OnFunc(on("list-sessions"), func(execx.Cmd) (execx.Result, error) {
		if w.listErr[key] {
			return execx.Result{ExitCode: 1, Stderr: []byte("lxc-attach: container is not running")}, nil
		}
		var b strings.Builder
		for _, s := range w.sessions {
			if s.server == key {
				b.WriteString(s.name + " 0 /srv/proj\n")
			}
		}
		return execx.Result{Stdout: []byte(b.String())}, nil
	})
	w.fake.OnFunc(on("capture-pane"), func(c execx.Cmd) (execx.Result, error) {
		s := w.find(targetOf(c.Args))
		if s == nil {
			return gone(targetOf(c.Args)), nil
		}
		w.look(s)
		return execx.Result{Stdout: []byte(s.pane + "\n")}, nil
	})
	w.fake.OnFunc(on("send-keys"), func(c execx.Cmd) (execx.Result, error) {
		s := w.find(targetOf(c.Args))
		if s == nil {
			return gone(targetOf(c.Args)), nil
		}
		w.press(s, c.Args[len(c.Args)-2:])
		return execx.Result{}, nil
	})
	w.fake.OnFunc(on("kill-session"), func(c execx.Cmd) (execx.Result, error) {
		s := w.find(targetOf(c.Args))
		if s == nil {
			return gone(targetOf(c.Args)), nil
		}
		w.end(s)
		return execx.Result{}, nil
	})
}

// look is a compaction (or, with turn, a turn) running on: busy for its looks, then ready, a
// compaction's boundary written.
func (w *world) look(s *fakeSession) {
	if s.pane != paneBusy || s.busyLeft < 0 || s.stuck {
		return
	}
	if s.busyLeft > 0 {
		s.busyLeft--
		return
	}
	s.pane, s.busyLeft = paneReady, -1
	if s.turn {
		s.turn = false
		if s.afterTurn != "" {
			s.pane = s.afterTurn
		}
		return
	}
	if !s.noBoundary {
		w.boundary["/t/"+s.name+".jsonl"] = w.now.Unix()
	}
}

func (w *world) end(s *fakeSession) {
	w.sessions = slices.DeleteFunc(w.sessions, func(x *fakeSession) bool { return x == s })
}

func (w *world) press(s *fakeSession, last []string) {
	switch {
	case last[0] == "-l":
		s.typed += last[1]
	case last[1] == "Escape":
		s.escapes++
		if s.pane == paneBusy {
			s.pane = paneReady
		}
	case last[1] == "Enter":
		s.enters++
		typed := s.typed
		s.typed = ""
		switch {
		case typed == "/exit" && !s.ignoreExit:
			w.end(s)
		case strings.HasPrefix(typed, "/compact "):
			s.pane, s.busyLeft = paneBusy, s.compactLooks
		}
	}
}

func (w *world) typedInto(name string) []string {
	var got []string
	for _, c := range w.fake.Calls() {
		if c.Name == "tmux" && slices.Contains(c.Args, "send-keys") && targetOf(c.Args) == name && c.Args[len(c.Args)-2] == "-l" {
			got = append(got, c.Args[len(c.Args)-1])
		}
	}
	return got
}

func (w *world) killed(name string) bool {
	return w.fake.Called("tmux -L ccy kill-session -t =" + name)
}

func (w *world) close(targets ...Target) []Result { return w.c.Close(w.t.Context(), targets) }

func one(t *testing.T, got []Result) Result {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("results %v", got)
	}
	return got[0]
}

// C1, C4, C6: a session is compacted, its compaction seen in its transcript, then sent /exit.
func TestASessionIsCompactedThenExited(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	r := one(t, w.close(Target{Name: "ccy-proj"}))
	if r.Outcome != Exited || !r.Compacted || r.Name != "ccy-proj" {
		t.Fatalf("result %+v", r)
	}
	if got := w.typedInto("ccy-proj"); !slices.Equal(got, []string{CompactText, "/exit"}) {
		t.Fatalf("typed %q", got)
	}
	if r.Note != "compacted first" {
		t.Fatalf("note %q", r.Note)
	}
	if w.killed("ccy-proj") {
		t.Fatal("a clean exit was killed")
	}
}

// C3: a conversation that has not grown since its last compaction goes straight to /exit.
func TestAConversationCompactedLastGoesStraightToExit(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	w.last["/t/ccy-proj.jsonl"] = true
	r := one(t, w.close(Target{Name: "ccy-proj"}))
	if r.Outcome != Exited || r.Compacted || !slices.Equal(w.typedInto("ccy-proj"), []string{"/exit"}) {
		t.Fatalf("result %+v typed %q", r, w.typedInto("ccy-proj"))
	}
	if r.Note != "" {
		t.Fatalf("note %q", r.Note)
	}
}

// C2: NoCompact sends /exit alone and reads no transcript.
func TestNoCompactOnlyExits(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	w.readErr = errors.New("must not be read")
	r := one(t, w.close(Target{Name: "ccy-proj", NoCompact: true}))
	if r.Outcome != Exited || !slices.Equal(w.typedInto("ccy-proj"), []string{"/exit"}) || r.Note != "" {
		t.Fatalf("result %+v typed %q", r, w.typedInto("ccy-proj"))
	}
}

// C7: a compaction that does not complete in time is a loud failure: /exit is not typed, the
// session is left running, never killed.
func TestACompactionThatNeverEndsFailsAndLeavesTheSessionRunning(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	w.find("ccy-proj").stuck = true
	r := one(t, w.close(Target{Name: "ccy-proj"}))
	if r.Outcome != Failed || w.killed("ccy-proj") || slices.Contains(w.typedInto("ccy-proj"), "/exit") {
		t.Fatalf("result %+v killed %v typed %q", r, w.killed("ccy-proj"), w.typedInto("ccy-proj"))
	}
	want := "its /compact did not complete within 600 s, so /exit was not typed and it is still running: attach it (ccy-sessions)"
	if r.Note != want {
		t.Fatalf("note %q", r.Note)
	}
}

// C7: a transcript that cannot be found or read fails the close before anything is typed, and
// names --no-compact.
func TestATranscriptThatCannotBeReadFailsBeforeAnythingIsTyped(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*world)
	}{
		{"not found", func(w *world) { w.findErr = errors.New("no conversation recorded") }},
		{"unreadable", func(w *world) { w.readErr = errors.New("permission denied") }},
	} {
		w := newWorld(t, "ccy-proj")
		tc.set(w)
		r := one(t, w.close(Target{Name: "ccy-proj"}))
		if r.Outcome != Failed || len(w.typedInto("ccy-proj")) != 0 || w.killed("ccy-proj") {
			t.Fatalf("%s: result %+v typed %q", tc.name, r, w.typedInto("ccy-proj"))
		}
		if !strings.Contains(r.Note, "nothing was typed") || !strings.Contains(r.Note, "--no-compact") {
			t.Fatalf("%s: note %q", tc.name, r.Note)
		}
	}
}

// C5: a session launched without the bus records no conversation: its compaction's end is read
// from its pane, and the note says so.
func TestASessionWithoutTheBusIsWatchedOnItsPane(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	w.record("ccy-proj", "--no-bus")
	w.findErr = errors.New("must not be looked up")
	w.find("ccy-proj").noBoundary = true
	r := one(t, w.close(Target{Name: "ccy-proj"}))
	if r.Outcome != Exited || !r.Compacted || !slices.Equal(w.typedInto("ccy-proj"), []string{CompactText, "/exit"}) {
		t.Fatalf("result %+v typed %q", r, w.typedInto("ccy-proj"))
	}
	if !strings.HasPrefix(r.Note, "compacted first (its end read from its pane: it was launched without the agent team bus") {
		t.Fatalf("note %q", r.Note)
	}
}

// C5: a session with no record is watched on its pane too.
func TestASessionWithNoRecordIsWatchedOnItsPane(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	if err := w.store.Remove("ccy-proj"); err != nil {
		t.Fatal(err)
	}
	w.find("ccy-proj").noBoundary = true
	r := one(t, w.close(Target{Name: "ccy-proj"}))
	if r.Outcome != Exited || !r.Compacted || !strings.Contains(r.Note, "it has no session record") {
		t.Fatalf("result %+v", r)
	}
}

// C6: a compaction whose pane is idle again with no boundary in the transcript fails, after
// the settle bound, without /exit.
func TestACompactionWithNoBoundaryFails(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	w.find("ccy-proj").noBoundary = true
	r := one(t, w.close(Target{Name: "ccy-proj"}))
	if r.Outcome != Failed || slices.Contains(w.typedInto("ccy-proj"), "/exit") || w.killed("ccy-proj") {
		t.Fatalf("result %+v typed %q", r, w.typedInto("ccy-proj"))
	}
	if !strings.Contains(r.Note, "no compact_boundary in its transcript within 30 s") {
		t.Fatalf("note %q", r.Note)
	}
}

// C4 (Plan 00257 Q52): a session in a turn is never interrupted: its turn is waited for, past
// the prompt bound and within the turn bound (ready at 200 s), then it is compacted and exits.
// No Escape is sent.
func TestABusySessionIsWaitedForNeverInterrupted(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	s := w.find("ccy-proj")
	s.pane, s.busyLeft, s.turn = paneBusy, 200, true
	start := w.now
	r := one(t, w.close(Target{Name: "ccy-proj"}))
	if r.Outcome != Exited || !r.Compacted || s.escapes != 0 || w.killed("ccy-proj") {
		t.Fatalf("result %+v escapes %d killed %v", r, s.escapes, w.killed("ccy-proj"))
	}
	if got := w.typedInto("ccy-proj"); !slices.Equal(got, []string{CompactText, "/exit"}) {
		t.Fatalf("typed %q", got)
	}
	if w.now.Sub(start) < 200*DefaultWaits.Poll {
		t.Fatalf("the turn was not waited for: %v", w.now.Sub(start))
	}
}

// C2, C4 (Q52): a --no-compact close waits out a running turn too, then types /exit alone.
func TestANoCompactCloseWaitsForARunningTurn(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	s := w.find("ccy-proj")
	s.pane, s.busyLeft, s.turn = paneBusy, 200, true
	r := one(t, w.close(Target{Name: "ccy-proj", NoCompact: true}))
	if r.Outcome != Exited || s.escapes != 0 || !slices.Equal(w.typedInto("ccy-proj"), []string{"/exit"}) {
		t.Fatalf("result %+v escapes %d typed %q", r, s.escapes, w.typedInto("ccy-proj"))
	}
}

// C8 (Q52): a pane that never draws a prompt (starting, a dialog, hung) and was never seen in a
// turn keeps the prompt bound, 30 s: stopped then, untyped.
func TestAPaneWithNoPromptIsStoppedAtThePromptBound(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	w.find("ccy-proj").pane = paneStarting
	start := w.now
	r := one(t, w.close(Target{Name: "ccy-proj"}))
	if r.Outcome != Stopped || len(w.typedInto("ccy-proj")) != 0 || !w.killed("ccy-proj") {
		t.Fatalf("result %+v typed %q", r, w.typedInto("ccy-proj"))
	}
	if r.Note != "its prompt was not ready within 30 s, so its tmux session was killed" {
		t.Fatalf("note %q", r.Note)
	}
	if took := w.now.Sub(start); took < DefaultWaits.Prompt || took > DefaultWaits.Prompt+DefaultWaits.Poll {
		t.Fatalf("stopped after %v", took)
	}
}

// C8 (Q52): a turn seen running keeps the turn bound even when its pane then shows no box (a
// dialog of the turn, a redraw): the bound a turn earned is never cut back to the prompt's.
func TestATurnThatLeavesNoPromptKeepsTheTurnBound(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	s := w.find("ccy-proj")
	s.pane, s.busyLeft, s.turn, s.afterTurn = paneBusy, 10, true, paneStarting
	start := w.now
	r := one(t, w.close(Target{Name: "ccy-proj"}))
	if r.Outcome != Stopped || len(w.typedInto("ccy-proj")) != 0 {
		t.Fatalf("result %+v typed %q", r, w.typedInto("ccy-proj"))
	}
	if r.Note != "its prompt was not ready within 300 s, so its tmux session was killed" {
		t.Fatalf("note %q", r.Note)
	}
	if took := w.now.Sub(start); took < DefaultWaits.Turn || took > DefaultWaits.Turn+DefaultWaits.Poll {
		t.Fatalf("stopped after %v", took)
	}
}

// C4, C8 (Q52): a host session still in its turn at the prompt bound is stopped as before, and
// was never sent Escape nor typed into.
func TestABusySessionPastTheBoundIsStoppedNeverInterrupted(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	s := w.find("ccy-proj")
	s.pane, s.stuck = paneBusy, true
	start := w.now
	r := one(t, w.close(Target{Name: "ccy-proj"}))
	if r.Outcome != Stopped || s.escapes != 0 || len(w.typedInto("ccy-proj")) != 0 || !w.killed("ccy-proj") {
		t.Fatalf("result %+v escapes %d typed %q", r, s.escapes, w.typedInto("ccy-proj"))
	}
	if r.Note != "a turn was still running after 300 s, so its tmux session was killed" {
		t.Fatalf("note %q", r.Note)
	}
	if took := w.now.Sub(start); took < DefaultWaits.Turn || took > DefaultWaits.Turn+DefaultWaits.Poll {
		t.Fatalf("stopped after %v", took)
	}
}

// C4: unsent text is never typed over, compacting or not.
func TestUnsentTextIsLeftAlone(t *testing.T) {
	for _, noCompact := range []bool{false, true} {
		w := newWorld(t, "ccy-proj")
		w.find("ccy-proj").pane = paneTyped
		r := one(t, w.close(Target{Name: "ccy-proj", NoCompact: noCompact}))
		if r.Outcome != Failed || len(w.typedInto("ccy-proj")) != 0 || w.killed("ccy-proj") {
			t.Fatalf("no-compact %v: result %+v", noCompact, r)
		}
	}
}

// C8: several sessions are closed together, each with its own result, in the targets' order.
func TestSessionsAreClosedTogether(t *testing.T) {
	w := newWorld(t, "ccy-a", "ccy-b")
	w.last["/t/ccy-b.jsonl"] = true
	got := w.close(Target{Name: "ccy-a"}, Target{Name: "ccy-b"})
	if len(got) != 2 || got[0].Name != "ccy-a" || got[1].Name != "ccy-b" || got[0].Outcome != Exited || got[1].Outcome != Exited {
		t.Fatalf("results %+v", got)
	}
	if !got[0].Compacted || got[1].Compacted || w.killed("ccy-b") || !slices.Equal(w.typedInto("ccy-b"), []string{"/exit"}) {
		t.Fatalf("results %+v typed %q", got, w.typedInto("ccy-b"))
	}
}

// C9: Live is the sessions on ccy's tmux server, and Limits the bounds the Closer works to.
func TestLiveAndLimits(t *testing.T) {
	w := newWorld(t, "ccy-a", "cc-b")
	live, err := w.c.Live(t.Context())
	if err != nil || !live["ccy-a"] || !live["cc-b"] || len(live) != 2 {
		t.Fatalf("live %v err %v", live, err)
	}
	if w.c.Limits() != DefaultWaits || DefaultWaits.Compact != 600*time.Second || DefaultWaits.Prompt != 30*time.Second ||
		DefaultWaits.Turn != 300*time.Second {
		t.Fatalf("limits %+v", w.c.Limits())
	}
}

// C4: TurnWait says, as a sentence, how long a turn is waited for.
func TestTurnWait(t *testing.T) {
	if got := TurnWait(DefaultWaits); got != "A session in the middle of a turn is given up to 300 s to finish it; it is never interrupted." {
		t.Fatalf("%q", got)
	}
}

// C10: Words is the bound as a sentence reads it.
func TestSeconds(t *testing.T) {
	for d, want := range map[time.Duration]string{60 * time.Second: "60 s", 1500 * time.Millisecond: "1.5 s"} {
		if got := Seconds(d); got != want {
			t.Fatalf("%v: %q", d, got)
		}
	}
	if strconv.Itoa(len(CompactText)) == "0" || !strings.HasPrefix(CompactText, "/compact ") {
		t.Fatalf("compact text %q", CompactText)
	}
}

// Quiet: idle after a busy look at once; idle with no busy look only after the settle bound;
// any other look starts the bound again.
func TestQuiet(t *testing.T) {
	var q Quiet
	now := time.Unix(1_800_000_000, 0)
	settle := 30 * time.Second
	if q.Idle(screen.Ready, now, settle) || q.Idle(screen.Ready, now.Add(29*time.Second), settle) {
		t.Fatal("idle before the settle bound")
	}
	if !q.Idle(screen.Ready, now.Add(30*time.Second), settle) {
		t.Fatal("not idle at the settle bound")
	}
	q.Reset()
	if q.Idle(screen.Busy, now, settle) || !q.Idle(screen.Ready, now, settle) {
		t.Fatal("not idle after a busy look")
	}
	q.Reset()
	q.Saw(screen.Ready)
	if q.Idle(screen.Ready, now, settle) {
		t.Fatal("a ready look elsewhere counted as busy")
	}
	q.Saw(screen.Busy)
	if !q.Idle(screen.Ready, now, settle) {
		t.Fatal("a busy look elsewhere did not count")
	}
	q.Reset()
	q.Idle(screen.Ready, now, settle)
	q.Idle(screen.Typed, now.Add(20*time.Second), settle)
	if q.Idle(screen.Ready, now.Add(31*time.Second), settle) {
		t.Fatal("a typed look did not start the bound again")
	}
}

// windDownBy is the wind-down's deadline in the tests: ten minutes from the world's start.
func (w *world) windDownBy() time.Time { return w.now.Add(10 * time.Minute) }

// kept is whether the session's record is still there.
func (w *world) kept(name string) bool {
	_, err := w.store.Get(name)
	return err == nil
}

// W1: Compact compacts a session and leaves it running: no /exit, nothing killed, its record
// kept. An idle box is given the settle bound before /compact, so a warning being typed into
// it lands first.
func TestCompactLeavesTheSessionRunning(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	start := w.now
	r := w.c.Compact(t.Context(), "ccy-proj", w.windDownBy())
	if r.Outcome != Compacted || !r.Compacted || r.Busy || r.Name != "ccy-proj" {
		t.Fatalf("result %+v", r)
	}
	if got := w.typedInto("ccy-proj"); !slices.Equal(got, []string{WindDownText}) {
		t.Fatalf("typed %q", got)
	}
	if w.killed("ccy-proj") || w.find("ccy-proj") == nil || !w.kept("ccy-proj") {
		t.Fatal("the session was ended or its record removed")
	}
	if w.now.Sub(start) < DefaultWaits.Settle {
		t.Fatalf("an idle box was typed into after %v, before the settle bound", w.now.Sub(start))
	}
	if !strings.HasPrefix(WindDownText, "/compact ") {
		t.Fatalf("wind-down text %q", WindDownText)
	}
}

// W2: a session in a turn is never interrupted: the turn is waited for, then it is compacted,
// and the result says it was busy.
func TestCompactWaitsForARunningTurn(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	s := w.find("ccy-proj")
	s.pane, s.busyLeft, s.turn = paneBusy, 5, true
	r := w.c.Compact(t.Context(), "ccy-proj", w.windDownBy())
	if r.Outcome != Compacted || !r.Busy || s.escapes != 0 || !slices.Equal(w.typedInto("ccy-proj"), []string{WindDownText}) {
		t.Fatalf("result %+v escapes %d typed %q", r, s.escapes, w.typedInto("ccy-proj"))
	}
}

// W3: a turn still running at the deadline is not interrupted: not finished, nothing typed,
// nothing killed.
func TestCompactLeavesATurnRunningPastTheDeadline(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	s := w.find("ccy-proj")
	s.pane, s.busyLeft = paneBusy, -1
	by := w.windDownBy()
	r := w.c.Compact(t.Context(), "ccy-proj", by)
	if r.Outcome != Unfinished || !r.Busy || s.escapes != 0 || len(w.typedInto("ccy-proj")) != 0 || w.killed("ccy-proj") {
		t.Fatalf("result %+v escapes %d typed %q", r, s.escapes, w.typedInto("ccy-proj"))
	}
	if !strings.Contains(r.Note, "still in a turn") || w.now.Before(by) || w.now.After(by.Add(DefaultWaits.Poll)) {
		t.Fatalf("note %q at %v (deadline %v)", r.Note, w.now, by)
	}
}

// W3: a compaction still running at the deadline is not finished, and the session is left
// running.
func TestCompactWhoseCompactionOutlastsTheDeadline(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	w.find("ccy-proj").stuck = true
	r := w.c.Compact(t.Context(), "ccy-proj", w.windDownBy())
	if r.Outcome != Unfinished || r.Compacted || w.killed("ccy-proj") || !w.kept("ccy-proj") {
		t.Fatalf("result %+v", r)
	}
	if !strings.Contains(r.Note, "compaction had not finished") {
		t.Fatalf("note %q", r.Note)
	}
}

// W3: a compaction that ends on the deadline's own look counts.
func TestCompactThatEndsAtTheDeadlineCounts(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	s := w.find("ccy-proj")
	s.compactLooks = 10
	by := w.now.Add(DefaultWaits.Settle + 11*DefaultWaits.Poll)
	r := w.c.Compact(t.Context(), "ccy-proj", by)
	if r.Outcome != Compacted {
		t.Fatalf("result %+v at %v (deadline %v)", r, w.now, by)
	}
}

// W4: what is not compacted is skipped or left unfinished with why, and nothing is typed or
// killed: a conversation compacted last, a box holding unsent text to the deadline, a session
// whose claude is not running, one that ended, one whose transcript cannot be read.
func TestCompactSkipsWhatItCannotOrNeedNotCompact(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		outcome    Outcome
		set        func(*world, *fakeSession)
	}{
		{"compacted last", "nothing since its last compaction", Skipped, func(w *world, _ *fakeSession) { w.last["/t/ccy-proj.jsonl"] = true }},
		{"unsent text", "unsent text", Unfinished, func(_ *world, s *fakeSession) { s.pane = paneTyped }},
		{"launcher ended", "claude is not running in it", Skipped, func(_ *world, s *fakeSession) { s.pane = "ccy ended. " + screen.EndedText }},
		{"gone", "it ended", Skipped, func(w *world, s *fakeSession) { w.end(s) }},
		{"unreadable transcript", "nothing was typed", Failed, func(w *world, _ *fakeSession) { w.readErr = errors.New("permission denied") }},
	} {
		w := newWorld(t, "ccy-proj")
		tc.set(w, w.find("ccy-proj"))
		r := w.c.Compact(t.Context(), "ccy-proj", w.windDownBy())
		if r.Outcome != tc.outcome || len(w.typedInto("ccy-proj")) != 0 || w.killed("ccy-proj") || !w.kept("ccy-proj") {
			t.Fatalf("%s: result %+v typed %q", tc.name, r, w.typedInto("ccy-proj"))
		}
		if !strings.Contains(r.Note, tc.want) || strings.Contains(r.Note, "--no-compact") {
			t.Fatalf("%s: note %q", tc.name, r.Note)
		}
	}
}

// W5: a session with no recorded conversation is watched on its pane, as a close is.
func TestCompactReadsTheEndFromThePaneWithoutTheBus(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	w.record("ccy-proj", "--no-bus")
	w.find("ccy-proj").noBoundary = true
	r := w.c.Compact(t.Context(), "ccy-proj", w.windDownBy())
	if r.Outcome != Compacted || !strings.Contains(r.Note, "its end read from its pane") {
		t.Fatalf("result %+v", r)
	}
}

// W5: failures after /compact went in name the session as still running, never /exit.
func TestCompactFailuresAfterTheCompactLeaveItRunning(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	w.find("ccy-proj").noBoundary = true
	r := w.c.Compact(t.Context(), "ccy-proj", w.windDownBy())
	if r.Outcome != Failed || !strings.HasSuffix(r.Note, "so it is still running: attach it (ccy-sessions)") || strings.Contains(r.Note, "/exit") {
		t.Fatalf("result %+v", r)
	}
}

// W6: CompactAll watches several sessions together against the one deadline, each with its
// own result, in order.
func TestCompactAllWatchesThemTogether(t *testing.T) {
	w := newWorld(t, "ccy-a", "cc-b")
	b := w.find("cc-b")
	b.pane, b.busyLeft, b.turn = paneBusy, 4, true
	got := w.c.CompactAll(t.Context(), []string{"ccy-a", "cc-b"}, w.windDownBy())
	if len(got) != 2 || got[0].Name != "ccy-a" || got[1].Name != "cc-b" || got[0].Outcome != Compacted || got[1].Outcome != Compacted {
		t.Fatalf("results %+v", got)
	}
	if got[0].Busy || !got[1].Busy {
		t.Fatalf("busy %+v", got)
	}
}

// guestSock is a v2 LXC seat's own tmux server, reached in the tests as the host's is.
const guestSock = "/run/teamchat-session-0123456789abcdef/tmux.sock"

// guestSession is the seat's tmux session in its container.
const guestSession = "teamchat-session-alpha-proj-1-0123456789abcdef"

// guest adds a session in an LXC container, on its own server, with no session record, and
// returns its target.
func (w *world) guest() Target {
	w.sessions = append(w.sessions, &fakeSession{name: guestSession, pane: paneReady, compactLooks: 3, server: guestSock})
	w.fakeServer(guestSock, "-S", guestSock)
	tm := &tmuxccy.Tmux{Runner: w.fake, Term: &term.Term{Err: io.Discard}, SocketPath: guestSock}
	return Target{Name: "box1/0123456789abcdef", Guest: &Guest{Tmux: tm, Session: guestSession, Where: "container box1"}}
}

// G1 (SPEC-team-close TC2, TC8): a session in an LXC container is closed on its own server:
// /compact, its end read from its pane (its conversation is in the container), then /exit. No
// session record is read for it.
func TestAGuestIsCompactedOnItsPaneThenExited(t *testing.T) {
	w := newWorld(t)
	w.c.Registry = func() (*registry.Store, error) { return nil, errors.New("must not be read") }
	target := w.guest()
	r := one(t, w.close(target))
	if r.Outcome != Exited || !r.Compacted || r.Name != target.Name {
		t.Fatalf("result %+v", r)
	}
	if got := w.typedInto(guestSession); !slices.Equal(got, []string{CompactText, "/exit"}) {
		t.Fatalf("typed %q", got)
	}
	if !strings.Contains(r.Note, "its end read from its pane: it runs in container box1, whose conversation this host does not read") {
		t.Fatalf("note %q", r.Note)
	}
	for _, c := range w.fake.Calls() {
		if c.Args[0] != "-S" || c.Args[1] != guestSock {
			t.Fatalf("a command off the guest's server: %v", c.Args)
		}
	}
}

// G2 (TC9): a session in a container that does not end after /exit is never killed: it fails,
// named as still running there.
func TestAGuestThatDoesNotEndIsLeftRunning(t *testing.T) {
	w := newWorld(t)
	target := w.guest()
	w.find(guestSession).ignoreExit = true
	r := one(t, w.close(target))
	if r.Outcome != Failed || w.fake.Called("tmux -S "+guestSock+" kill-session") {
		t.Fatalf("result %+v", r)
	}
	want := "exit refused: it did not end within 60 s of /exit, so it was left running in container box1: attach it there and type /exit"
	if r.Note != want {
		t.Fatalf("note %q", r.Note)
	}
}

// G1 (TC8, Q52): a session in a container in a turn is waited for, never interrupted, then
// compacted and exited.
func TestAGuestInATurnIsWaitedForThenClosed(t *testing.T) {
	w := newWorld(t)
	target := w.guest()
	s := w.find(guestSession)
	s.pane, s.busyLeft, s.turn = paneBusy, 120, true
	r := one(t, w.close(target))
	if r.Outcome != Exited || !r.Compacted || s.escapes != 0 || !slices.Equal(w.typedInto(guestSession), []string{CompactText, "/exit"}) {
		t.Fatalf("result %+v escapes %d typed %q", r, s.escapes, w.typedInto(guestSession))
	}
}

// G3 (TC9, TC12, Q52): a session in a container whose prompt is not ready within the bound fails
// as busy past the bound, untouched by a kill and never sent Escape.
func TestAGuestBusyPastTheBoundIsLeftRunning(t *testing.T) {
	w := newWorld(t)
	target := w.guest()
	s := w.find(guestSession)
	s.pane, s.stuck = paneBusy, true
	r := one(t, w.close(target))
	if r.Outcome != Failed || s.escapes != 0 || len(w.typedInto(guestSession)) != 0 || w.fake.Called("tmux -S "+guestSock+" kill-session") {
		t.Fatalf("result %+v escapes %d", r, s.escapes)
	}
	want := "busy past the bound: a turn was still running after 300 s, so it was left running in container box1: attach it there and type /exit"
	if r.Note != want {
		t.Fatalf("note %q", r.Note)
	}
}

// G2 (TC12, Q52): a session in a container that never draws a prompt is not "busy": it fails at
// the prompt bound, 30 s, as having no prompt, left running.
func TestAGuestWithNoPromptFailsAtThePromptBound(t *testing.T) {
	w := newWorld(t)
	target := w.guest()
	w.find(guestSession).pane = paneStarting
	start := w.now
	r := one(t, w.close(target))
	if r.Outcome != Failed || len(w.typedInto(guestSession)) != 0 || w.fake.Called("tmux -S "+guestSock+" kill-session") {
		t.Fatalf("result %+v", r)
	}
	want := "no prompt: its prompt was not ready within 30 s, so it was left running in container box1: attach it there and type /exit"
	if r.Note != want {
		t.Fatalf("note %q", r.Note)
	}
	if took := w.now.Sub(start); took > DefaultWaits.Prompt+DefaultWaits.Poll {
		t.Fatalf("failed after %v", took)
	}
}

// G4 (TC12): a container whose server cannot be listed fails only its own sessions, as
// unreachable; the host's are closed.
func TestAnUnreachableGuestFailsOnlyItself(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	target := w.guest()
	w.listErr[guestSock] = true
	got := w.close(Target{Name: "ccy-proj"}, target)
	if len(got) != 2 || got[0].Outcome != Exited || got[1].Outcome != Failed {
		t.Fatalf("results %+v", got)
	}
	if !strings.HasPrefix(got[1].Note, "unreachable: ") || !strings.Contains(got[1].Note, "container is not running") {
		t.Fatalf("note %q", got[1].Note)
	}
}

// G5 (TC9): a session in a container whose claude has ended is left as it is, never killed.
func TestAGuestWhoseClaudeEndedIsNotKilled(t *testing.T) {
	w := newWorld(t)
	target := w.guest()
	w.find(guestSession).pane = "\nccy exited with status 1. Press Enter to close this session.\n"
	r := one(t, w.close(target))
	if r.Outcome != Failed || w.fake.Called("tmux -S "+guestSock+" kill-session") || len(w.typedInto(guestSession)) != 0 {
		t.Fatalf("result %+v", r)
	}
	if !strings.Contains(r.Note, "claude is not running in it") || !strings.Contains(r.Note, "nothing was typed or stopped") {
		t.Fatalf("note %q", r.Note)
	}
}
