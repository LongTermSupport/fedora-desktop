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
)

// fakeSession is one tmux session: its pane, and what a typed line does to it.
type fakeSession struct {
	name, pane, typed string
	// compactLooks is how many looks a compaction stays busy for; stuck never ends it.
	compactLooks int
	stuck        bool
	// noBoundary ends the compaction on the pane without writing its compact_boundary.
	noBoundary      bool
	busyLeft        int
	enters, escapes int
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
	c        *Closer
}

func newWorld(t *testing.T, names ...string) *world {
	t.Helper()
	w := &world{
		t: t, fake: &execx.Fake{}, store: registry.New(t.TempDir()), now: time.Unix(1_800_000_000, 0),
		boundary: map[string]int64{}, last: map[string]bool{},
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

func (w *world) fakeTmux() {
	w.fake.OnFunc([]string{"tmux", "-L", "ccy", "list-sessions"}, func(execx.Cmd) (execx.Result, error) {
		var b strings.Builder
		for _, s := range w.sessions {
			b.WriteString(s.name + " 0 /srv/proj\n")
		}
		return execx.Result{Stdout: []byte(b.String())}, nil
	})
	w.fake.OnFunc([]string{"tmux", "-L", "ccy", "capture-pane"}, func(c execx.Cmd) (execx.Result, error) {
		s := w.find(targetOf(c.Args))
		if s == nil {
			return gone(targetOf(c.Args)), nil
		}
		w.look(s)
		return execx.Result{Stdout: []byte(s.pane + "\n")}, nil
	})
	w.fake.OnFunc([]string{"tmux", "-L", "ccy", "send-keys"}, func(c execx.Cmd) (execx.Result, error) {
		s := w.find(targetOf(c.Args))
		if s == nil {
			return gone(targetOf(c.Args)), nil
		}
		w.press(s, c.Args[len(c.Args)-2:])
		return execx.Result{}, nil
	})
	w.fake.OnFunc([]string{"tmux", "-L", "ccy", "kill-session"}, func(c execx.Cmd) (execx.Result, error) {
		s := w.find(targetOf(c.Args))
		if s == nil {
			return gone(targetOf(c.Args)), nil
		}
		w.end(s)
		return execx.Result{}, nil
	})
}

// look is a compaction running on: busy for its looks, then ready, its boundary written.
func (w *world) look(s *fakeSession) {
	if s.pane != paneBusy || s.busyLeft < 0 || s.stuck {
		return
	}
	if s.busyLeft > 0 {
		s.busyLeft--
		return
	}
	s.pane, s.busyLeft = paneReady, -1
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
		case typed == "/exit":
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

// C4: a busy session is interrupted once before /compact is typed.
func TestABusySessionIsInterruptedBeforeTheCompact(t *testing.T) {
	w := newWorld(t, "ccy-proj")
	s := w.find("ccy-proj")
	s.pane, s.busyLeft = paneBusy, -1
	r := one(t, w.close(Target{Name: "ccy-proj"}))
	if r.Outcome != Exited || s.escapes != 1 || !slices.Equal(w.typedInto("ccy-proj"), []string{CompactText, "/exit"}) {
		t.Fatalf("result %+v escapes %d typed %q", r, s.escapes, w.typedInto("ccy-proj"))
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
	if w.c.Limits() != DefaultWaits || DefaultWaits.Compact != 600*time.Second {
		t.Fatalf("limits %+v", w.c.Limits())
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
