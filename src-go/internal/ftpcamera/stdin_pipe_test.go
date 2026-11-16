package ftpcamera

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/stdinread"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// openPipe makes the kit's stdin an open pipe nothing writes to (an agent's shell's stdin for
// a command given no input), its writer held by the test, timed by the returned fake clock.
func openPipe(t *testing.T, k *kit) *clock.Fake {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := errors.Join(w.Close(), r.Close()); err != nil {
			t.Error(err)
		}
	})
	f := clock.NewFake(0, 0)
	k.pipe, k.stdinClock = r, f
	return f
}

// runPiped runs the kit in the background, lets the fake clock reach the piped answer's bound,
// and returns the run's exit code.
func runPiped(t *testing.T, k *kit, f *clock.Fake, args ...string) term.ExitCode {
	t.Helper()
	app := k.app()
	done := make(chan term.ExitCode, 1)
	go func() { done <- app.Run(context.Background(), args) }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	// The give-up is armed after the notice, so once it is, both are.
	if err := f.AwaitDue(ctx, stdinread.PipedGiveUp); err != nil {
		t.Fatal(err)
	}
	f.Advance(stdinread.NoticeAfter)
	f.Advance(stdinread.PipedGiveUp - stdinread.NoticeAfter)
	select {
	case code := <-done:
		return code
	case <-ctx.Done():
		t.Fatal("the run did not end at the bound")
		return 0
	}
}

// Issue 260: the prune question piped an open pipe nothing writes to says it is waiting, then
// fails naming the bound, and removes nothing.
func TestThePruneAnswerOnAnOpenPipeIsBounded(t *testing.T) {
	k := newKit(t)
	k.realRoot()
	pruneTree(k)
	f := openPipe(t, k)
	if code := runPiped(t, k, f, "--prune"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"ftp-camera: waiting for input on stdin", "within 60 s"} {
		if !strings.Contains(k.err.String(), want) {
			t.Errorf("stderr lacks %q: %q", want, k.err.String())
		}
	}
	k.wantNotRan("sudo -n rm")
}

// Issue 260: a new hotspot password shown on a terminal, its Enter piped an open pipe nothing
// writes to, is cleared at the bound, which the run fails naming.
func TestTheHotspotPasswordsEnterOnAnOpenPipeIsBounded(t *testing.T) {
	k := newKit(t)
	k.colour = true
	k.random = strings.NewReader("\x00\x00\x00\x01\x00\x02")
	f := openPipe(t, k)
	if code := runPiped(t, k, f, "--new-hotspot-password"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.HasSuffix(k.out.String(), term.AltScreenOff) {
		t.Errorf("stdout %q: the password not cleared", k.out.String())
	}
	if !strings.Contains(k.err.String(), "within 60 s") {
		t.Errorf("stderr %q", k.err.String())
	}
}

// Issue 260: the hotspot's Wi-Fi password shown before the server starts, its Enter piped an
// open pipe nothing writes to, is cleared at the bound, and the session fails naming it before
// the FTP server starts.
func TestTheWifiPasswordsEnterOnAnOpenPipeIsBounded(t *testing.T) {
	k := hotspotKit(t, true)
	k.colour = true
	f := openPipe(t, k)
	if code := runPiped(t, k, f, "--hotspot"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	_, shown, entered := strings.Cut(k.out.String(), term.AltScreenOn)
	if !entered || !strings.Contains(shown, term.AltScreenOff) || strings.Contains(k.out.String(), "Starting camera FTP server") {
		t.Errorf("stdout %q: cleared, and no server started", k.out.String())
	}
	if !strings.Contains(k.err.String(), "within 60 s") {
		t.Errorf("stderr %q", k.err.String())
	}
}
