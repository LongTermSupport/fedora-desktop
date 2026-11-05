//go:build tuwunel

package tuwunelhs

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestStartAnswersVersions(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), StartLimit)
	defer cancel()
	hs := Start(ctx, t, "example.org")
	if len(hs.RegKey) != 64 {
		t.Fatalf("registration key %d characters, want 64", len(hs.RegKey))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hs.Base+versionsPath, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("versions: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("versions answered %d", resp.StatusCode)
	}
}

// StartWith places the database where it is told, takes the extra settings, and Signal reaches
// the server's own process: a SIGUSR2 with admin_signal_execute makes Tuwunel's backup 1.
func TestStartWithPlacesTheDatabaseAndSignals(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), StartLimit)
	defer cancel()
	dir := t.TempDir()
	backups := filepath.Join(dir, "backups")
	hs := StartWith(ctx, t, "example.org", Options{
		DatabasePath: filepath.Join(dir, "db"),
		Extra:        fmt.Sprintf("database_backup_path = %q\nadmin_signal_execute = [\"server backup-database\"]\n", backups),
	})
	if hs.Base != "http://"+net.JoinHostPort(Loopback, strconv.Itoa(hs.Port)) {
		t.Fatalf("base %s, port %d", hs.Base, hs.Port)
	}
	if info, err := os.Stat(filepath.Join(dir, "db")); err != nil || !info.IsDir() {
		t.Fatalf("the database is not where it was placed: %v", err)
	}
	if err := hs.Signal(syscall.SIGUSR2); err != nil {
		t.Fatal(err)
	}
	clock := Clock{}
	for {
		if _, err := os.Stat(filepath.Join(backups, "meta", "1")); err == nil {
			return
		}
		if err := clock.Sleep(ctx, 100*time.Millisecond); err != nil {
			t.Fatalf("no backup 1 after the signal: %v", err)
		}
	}
}

// Stop ends the server with the signal given and waits for it; the database can then be opened
// again by a later run, which takes Args (RT2 proves them with --restore-backup). A SIGKILL is
// the test's own, so the cleanup does not report it.
func TestStopThenStartAgainOnTheSamePath(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*StartLimit)
	defer cancel()
	db := filepath.Join(t.TempDir(), "db")
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		hs := StartWith(ctx, t, "example.org", Options{DatabasePath: db})
		if err := hs.Stop(sig); err != nil {
			t.Fatalf("stop by %v: %v", sig, err)
		}
		if hs.answers(ctx) {
			t.Fatalf("the server answers after its stop by %v", sig)
		}
		if err := hs.Stop(sig); err == nil {
			t.Fatalf("a second stop by %v found a server", sig)
		}
	}
	hs := StartWith(ctx, t, "example.org", Options{DatabasePath: db, Args: []string{"-O", "max_request_size=20000000"}})
	if !hs.answers(ctx) {
		t.Fatal("the run with an option does not answer")
	}
}

func TestClockSleepEndsWithTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := (Clock{}).Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep = %v", err)
	}
	if (Clock{}).Now().IsZero() {
		t.Fatal("Now is zero")
	}
}
