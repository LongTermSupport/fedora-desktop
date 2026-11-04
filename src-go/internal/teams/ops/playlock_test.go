package ops

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/playlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// lockStart is the instant the rig's clock begins at; basicNote is what the other play run,
// started 45 seconds earlier, wrote about itself.
var lockStart = time.Date(2026, 10, 16, 12, 0, 5, 0, time.UTC)

const (
	basicNote = "pid=999 what=run.bash playbooks/imports/play-basic-configs.yml since=2026-10-16T11:59:20Z\n"
	basicWho  = "run.bash playbooks/imports/play-basic-configs.yml (pid 999), held since 11:59:20 UTC"
)

// holdElsewhere takes the play lock on a descriptor of its own, as another play run does;
// the returned func lets it go (once; later calls do nothing).
func (r *opsRig) holdElsewhere(t *testing.T, note string) func() {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(r.runDir, playlock.Name), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte(note), 0); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			if err := f.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(release)
	return release
}

// lockFree is whether nobody holds the play lock now.
func (r *opsRig) lockFree(t *testing.T) bool {
	t.Helper()
	w := *r.ops.PlayLock
	lock, err := w.Take(context.Background(), "probe", 0, nil)
	if err != nil {
		return false
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	return true
}

// appliedBeta scripts a play and a read-back that pass for team beta, with no Element profile.
func (r *opsRig) appliedBeta() {
	r.on(opsDeclare+"element-teams", execx.Result{})
	r.statuses = []map[string]declare.Status{{"beta": st("current", "")}}
}

func TestThePlayRunsInsideTheHeldPlayLockHandedDownToIt(t *testing.T) {
	r := newOpsRig(t)
	r.appliedBeta()
	heldDuringPlay := false
	r.fake.OnFunc([]string{opsPlay}, func(execx.Cmd) (execx.Result, error) {
		heldDuringPlay = !r.lockFree(t)
		return execx.Result{}, nil
	})
	if err := r.ops.ApplyTeams(t.Context(), []string{"beta"}); err != nil {
		t.Fatal(err)
	}
	play := r.fake.Calls()[0]
	if len(play.ExtraFiles) != 1 || !slices.Contains(play.Env, "FEDORA_DESKTOP_PLAY_LOCK_FD=3") {
		t.Errorf("the play was not handed the lock: files %v env %v", play.ExtraFiles, play.Env)
	}
	if !heldDuringPlay {
		t.Error("the lock was not held while the play ran")
	}
	if !r.lockFree(t) {
		t.Error("the lock is still held after the apply")
	}
	if strings.Contains(r.said(), "play lock") {
		t.Errorf("a free lock was talked about: %q", r.said())
	}
}

func TestTheElementPlayRunsInsideTheSameHold(t *testing.T) {
	r := newOpsRig(t)
	r.on(opsPlay, execx.Result{})
	r.on(opsDeclare+"element-teams", execx.Result{Stdout: []byte("ELEMENT\tbeta\thttp://192.0.2.10:6167\n")})
	r.on(opsElementPlay, execx.Result{})
	r.files["/home/u/.local/share/applications/agent-bus-beta-element.desktop"] = true
	r.statuses = []map[string]declare.Status{{"beta": st("current", "")}}
	if err := r.ops.ApplyTeams(t.Context(), []string{"beta"}); err != nil {
		t.Fatal(err)
	}
	calls := r.fake.Calls()
	element := calls[len(calls)-1]
	if element.Name != opsElementPlay || len(element.ExtraFiles) != 1 || element.ExtraFiles[0] != calls[0].ExtraFiles[0] {
		t.Errorf("the Element play is not in the apply's hold: %+v", element)
	}
}

func TestRunElementPlayAloneTakesTheLockItself(t *testing.T) {
	r := newOpsRig(t)
	r.on(opsElementPlay, execx.Result{})
	r.files["/home/u/.local/share/applications/agent-bus-beta-element.desktop"] = true
	if err := r.ops.RunElementPlay(t.Context(), []string{"beta"}); err != nil {
		t.Fatal(err)
	}
	if c := r.fake.Calls()[0]; len(c.ExtraFiles) != 1 {
		t.Errorf("the Element play ran without the lock: %+v", c)
	}
}

func TestANestedHoldIsTheSameHold(t *testing.T) {
	r := newOpsRig(t)
	r.on(opsPlay, execx.Result{})
	r.appliedBeta()
	err := r.ops.Locked(t.Context(), "save team beta", func(ctx context.Context) error {
		return r.ops.ApplyTeams(ctx, []string{"beta"})
	})
	if err != nil {
		t.Fatalf("the apply inside a hold could not take its own: %v (said %q)", err, r.said())
	}
	if r.sleeps != 0 {
		t.Errorf("waited %d times on its own hold", r.sleeps)
	}
}

func TestTheHolderLineNamesCcyTeamsAndWhatItIsDoing(t *testing.T) {
	r := newOpsRig(t)
	var note string
	err := r.ops.Locked(t.Context(), "save and apply team beta's settings", func(context.Context) error {
		body, err := os.ReadFile(filepath.Join(r.runDir, playlock.Name))
		note = string(body)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "pid=4321 what=ccy-teams: save and apply team beta's settings since=2026-10-16T12:00:05Z\n"; note != want {
		t.Errorf("note %q, want %q", note, want)
	}
}

func TestWithNoTerminalTheApplyWaitsSayingWhoHoldsTheLockThenRuns(t *testing.T) {
	r := newOpsRig(t)
	r.tty = false
	r.on(opsPlay, execx.Result{})
	r.appliedBeta()
	release := r.holdElsewhere(t, basicNote)
	r.onNap = func(n int) {
		if n == 3 {
			release()
		}
	}
	if code := r.ops.Apply(t.Context(), []string{"beta"}); code != term.ExitOK {
		t.Fatalf("exit %d, said %q", code, r.said())
	}
	for _, want := range []string{
		"The play lock is held by " + basicWho + ", for less than a minute.",
		"ccy-teams waits for it before it can apply beta: up to 30 minutes, until 12:30:05 UTC. Ctrl-C stops waiting, and changes nothing.",
		"The play lock is free after 3 seconds; going on to apply beta.",
		"Applied: beta.",
	} {
		if !strings.Contains(r.said(), want) {
			t.Errorf("missing %q in %q", want, r.said())
		}
	}
	if !r.called(opsPlay) {
		t.Error("the play did not run once the lock was free")
	}
}

func TestTheWaitSaysSoWhenAnotherRunTakesTheLockInTurn(t *testing.T) {
	r := newOpsRig(t)
	r.tty = false
	r.on(opsPlay, execx.Result{})
	r.appliedBeta()
	release := r.holdElsewhere(t, basicNote)
	var second func()
	r.onNap = func(n int) {
		switch n {
		case 2:
			release()
			second = r.holdElsewhere(t, "pid=1000 what=run.bash playbooks/imports/play-podman.yml since=2026-10-16T12:00:07Z\n")
		case 4:
			second()
		}
	}
	if code := r.ops.Apply(t.Context(), []string{"beta"}); code != term.ExitOK {
		t.Fatalf("exit %d, said %q", code, r.said())
	}
	want := "The play lock is now held by run.bash playbooks/imports/play-podman.yml (pid 1000), held since 12:00:07 UTC, for less than a minute."
	if strings.Count(r.said(), want) != 1 {
		t.Errorf("want %q once in %q", want, r.said())
	}
}

func TestAWaitThatReachesItsBoundRunsNothingAndSaysWhatToDo(t *testing.T) {
	r := newOpsRig(t)
	r.tty = false
	r.appliedBeta()
	r.holdElsewhere(t, basicNote)
	if code := r.ops.Apply(t.Context(), []string{"beta"}); code != term.ExitFailure {
		t.Fatalf("exit %d, want 1", code)
	}
	if r.called(opsPlay) {
		t.Error("the play ran without the lock")
	}
	for _, want := range []string{
		"ERROR: gave up waiting for the play lock after 30 minutes: it is still held by " + basicWho + ", for 30 minutes.",
		"ERROR: nothing was applied: beta. Once the other play run has finished, ccy-teams apply beta applies it.",
	} {
		if !strings.Contains(r.said(), want) {
			t.Errorf("missing %q in %q", want, r.said())
		}
	}
}

func TestCtrlCDuringTheWaitRunsNothing(t *testing.T) {
	r := newOpsRig(t)
	r.tty = false
	r.appliedBeta()
	r.holdElsewhere(t, basicNote)
	ctx, cancel := context.WithCancel(t.Context())
	r.onNap = func(int) { cancel() }
	if code := r.ops.Apply(ctx, []string{"beta"}); code != term.ExitCancelled {
		t.Fatalf("exit %d, want 130", code)
	}
	if r.called(opsPlay) {
		t.Error("the play ran")
	}
	if !strings.Contains(r.said(), "Stopped waiting for the play lock; nothing was changed.") {
		t.Errorf("said %q", r.said())
	}
}

func TestAPlayRefusedTheLockIsSaidAsThatNotAsARawFailure(t *testing.T) {
	r := newOpsRig(t)
	r.onExit(opsPlay, playlock.ExitBusy, "")
	err := r.ops.ApplyTeams(t.Context(), []string{"beta"})
	if err == nil {
		t.Fatal("want an error")
	}
	want := "ERROR: play-agent-bus.yml did not run: it was refused the play lock (exit 75, above), which another play run took. " +
		"Once that run has finished, ccy-teams apply beta applies it."
	if !strings.Contains(r.said(), want) {
		t.Errorf("missing %q in %q", want, r.said())
	}
	if strings.Contains(r.said(), "failed (exit 75") {
		t.Errorf("exit 75 reported as a raw failure: %q", r.said())
	}
}

func TestAnUnusableLockIsAFailureNotAWait(t *testing.T) {
	r := newOpsRig(t)
	r.ops.PlayLock.RuntimeDir = filepath.Join(r.runDir, "absent")
	if err := r.ops.ApplyTeams(t.Context(), []string{"beta"}); !errors.Is(err, playlock.ErrNotTaken) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(r.said(), "ERROR: the play lock cannot be used: the runtime directory") || r.sleeps != 0 {
		t.Errorf("said %q after %d sleeps", r.said(), r.sleeps)
	}
}

// blockingFzf answers fzf as a screen left open does: it returns only when its context ends,
// or at once with Esc when leave is set. Every other command goes to the Fake.
type blockingFzf struct {
	*execx.Fake

	mu      sync.Mutex
	leave   bool
	screens []execx.Cmd
}

func (b *blockingFzf) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	if c.Name != "fzf" {
		return b.Fake.Run(ctx, c)
	}
	b.mu.Lock()
	b.screens = append(b.screens, c)
	leave := b.leave
	b.mu.Unlock()
	if leave {
		return execx.Result{ExitCode: 130}, &execx.ExitError{Name: "fzf", Code: 130}
	}
	<-ctx.Done()
	return execx.Result{}, ctx.Err()
}

func (b *blockingFzf) shown() []execx.Cmd {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.screens)
}

func screenText(t *testing.T, c execx.Cmd) string {
	t.Helper()
	var b strings.Builder
	for _, a := range c.Args {
		b.WriteString(a + "\n")
	}
	if c.Stdin != nil {
		body, err := io.ReadAll(c.Stdin)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(body)
	}
	return b.String()
}

func TestOnATerminalTheWaitIsAScreenThatGoesOnByItselfWhenTheLockIsFree(t *testing.T) {
	r := newOpsRig(t)
	screen := &blockingFzf{Fake: r.fake}
	r.ops.Picker.Runner = screen
	r.ops.Picker.Plain, r.ops.Picker.Columns = true, 200
	r.on(opsPlay, execx.Result{})
	r.appliedBeta()
	release := r.holdElsewhere(t, basicNote)
	r.onNap = func(n int) {
		if n == 60 {
			release()
		}
	}
	if code := r.ops.Apply(t.Context(), []string{"beta"}); code != term.ExitOK {
		t.Fatalf("exit %d, said %q", code, r.said())
	}
	shown := screen.shown()
	if len(shown) < 2 {
		t.Fatalf("the screen was drawn %d times; it is drawn again when the minute changes", len(shown))
	}
	first := screenText(t, shown[0])
	for _, want := range []string{
		"Waiting for the play lock",
		"Teams › Waiting for the play lock",
		"before ccy-teams can apply beta",
		"ccy-teams is waiting for the play lock before it can apply beta.",
		"Held by " + basicWho + ", for less than a minute.",
		"It waits up to 30 minutes, until 12:30:05 UTC, and goes on by itself the moment the lock is free.",
		"Stop waiting (nothing is changed)",
	} {
		if !strings.Contains(first, want) {
			t.Errorf("the screen lacks %q:\n%s", want, first)
		}
	}
	if last := screenText(t, shown[len(shown)-1]); !strings.Contains(last, "for 1 minute.") {
		t.Errorf("the redrawn screen does not show the time held:\n%s", last)
	}
	if !r.called(opsPlay) {
		t.Error("the play did not run")
	}
}

func TestStopWaitingOnTheScreenRunsNothingAndHoldsNothing(t *testing.T) {
	r := newOpsRig(t)
	screen := &blockingFzf{Fake: r.fake, leave: true}
	r.ops.Picker.Runner = screen
	r.appliedBeta()
	r.holdElsewhere(t, basicNote)
	if code := r.ops.Apply(t.Context(), []string{"beta"}); code != term.ExitCancelled {
		t.Fatalf("exit %d, want 130; said %q", code, r.said())
	}
	if r.called(opsPlay) {
		t.Error("the play ran")
	}
	for _, want := range []string{
		"Stopped waiting for the play lock; nothing was changed.",
		"nothing was applied: beta. Once the other play run has finished, ccy-teams apply beta applies it.",
	} {
		if !strings.Contains(r.said(), want) {
			t.Errorf("missing %q in %q", want, r.said())
		}
	}
}

func TestRemoveDeclaresNothingAbsentWithoutTheLock(t *testing.T) {
	r := newOpsRig(t)
	r.tty = false
	r.holdElsewhere(t, basicNote)
	code, ok := r.ops.takeDownDeclared(t.Context(), "beta", false)
	if ok || code != term.ExitFailure {
		t.Fatalf("the take-down went ahead without the lock: exit %d, ok %v", code, ok)
	}
	if r.called(opsDeclare + "remove") {
		t.Error("the team was declared absent though its play could not run")
	}
	if !strings.Contains(r.said(), "Nothing was changed: beta is still declared present and running.") {
		t.Errorf("said %q", r.said())
	}
}
