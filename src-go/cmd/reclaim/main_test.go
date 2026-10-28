package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunDispatchesThroughTheApp(t *testing.T) {
	if code := run([]string{"--help"}); code != 0 {
		t.Fatalf("--help: exit %d", code)
	}
	// An unknown argument is a usage error before anything runs (SPEC-reclaim E2).
	if code := run([]string{"--frobnicate"}); code != 64 {
		t.Fatalf("--frobnicate: exit %d", code)
	}
}

// E30: the command runs under SignalContext, so a Ctrl-C ends the run with 130 instead of
// killing the process: a du the report is waiting on is stopped and the run says so.
func TestCtrlCEndsTheRun(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	// Every file is reached through an os.Root over the test's own temporary directory, by
	// names fixed here, so no path the test opens comes from outside it.
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := root.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	const homeName, binName, startedName = "home", "bin", "started"
	for _, d := range []string{filepath.Join(homeName, ".cache"), binName} {
		if mkErr := root.MkdirAll(d, 0o750); mkErr != nil {
			t.Fatal(mkErr)
		}
	}
	started := filepath.Join(dir, startedName)
	if fifoErr := syscall.Mkfifo(started, 0o600); fifoErr != nil {
		t.Fatal(fifoErr)
	}
	// The fake du is written owner-only and then made executable: PATH lookup needs the bit.
	du := filepath.Join(binName, "du")
	script := "#!/bin/sh\necho up > '" + started + "'\nexec '" + sleep + "' 60\n"
	if writeErr := root.WriteFile(du, []byte(script), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if chErr := root.Chmod(du, 0o700); chErr != nil {
		t.Fatal(chErr)
	}
	t.Setenv("HOME", filepath.Join(dir, homeName))
	t.Setenv("PATH", filepath.Join(dir, binName))
	done := make(chan term.ExitCode, 1)
	go func() { done <- run([]string{"report"}) }()
	// The fifo opens once du has started, so the signal lands while the report waits on it.
	f, err := root.Open(startedName)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	if code := <-done; code != term.ExitCancelled {
		t.Errorf("exit %d after Ctrl-C, want %d", code, term.ExitCancelled)
	}
}
