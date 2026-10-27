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
	dir := t.TempDir()
	home, bin := filepath.Join(dir, "home"), filepath.Join(dir, "bin")
	for _, d := range []string{filepath.Join(home, ".cache"), bin} {
		if mkErr := os.MkdirAll(d, 0o750); mkErr != nil {
			t.Fatal(mkErr)
		}
	}
	started := filepath.Join(dir, "started")
	if fifoErr := syscall.Mkfifo(started, 0o600); fifoErr != nil {
		t.Fatal(fifoErr)
	}
	script := "#!/bin/sh\necho up > '" + started + "'\nexec '" + sleep + "' 60\n"
	if writeErr := os.WriteFile(filepath.Join(bin, "du"), []byte(script), 0o700); writeErr != nil {
		t.Fatal(writeErr)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin)
	done := make(chan term.ExitCode, 1)
	go func() { done <- run([]string{"report"}) }()
	// The fifo opens once du has started, so the signal lands while the report waits on it.
	f, err := os.Open(started)
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
