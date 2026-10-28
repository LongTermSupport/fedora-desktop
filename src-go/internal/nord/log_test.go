package nord

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E21: a changing command logs every line it prints on stderr, stamped, the password never.
func TestConnectIsLogged(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	wantCode(t, w.run("--debug", "connect", "uk1"), term.ExitOK)
	log := w.logText()
	for _, want := range []string{
		"2026-10-28T09:30:00Z Importing uk1...\n",
		"2026-10-28T09:30:00Z nord: run: nmcli connection up uuid ",
		"2026-10-28T09:30:00Z Connected to uk1.\n",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	w.wantNoPassword()
	info, err := os.Stat(filepath.Join(w.home, ".local/share/nordvpn/nord.log"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("log mode %v", info.Mode().Perm())
	}
}

// E21: a read-only command writes no log.
func TestReadOnlyCommandsDoNotLog(t *testing.T) {
	w := newWorld(t)
	for _, cmd := range []string{"list", "list-active", "status"} {
		w.run(cmd)
	}
	if _, err := os.Stat(filepath.Join(w.home, ".local/share/nordvpn/nord.log")); !os.IsNotExist(err) {
		t.Errorf("a log was written: %v", err)
	}
}

// E21: the log's directory is made when missing; a log over 1 MiB is rotated.
func TestLogDirectoryAndRotation(t *testing.T) {
	w := newWorld(t)
	if err := os.RemoveAll(filepath.Join(w.home, ".local/share/nordvpn")); err != nil {
		t.Fatal(err)
	}
	wantCode(t, w.run("disconnect"), term.ExitOK)
	if !strings.Contains(w.logText(), "No NordVPN connection is active.") {
		t.Errorf("log %q", w.logText())
	}
	big := bytes.Repeat([]byte("x"), maxLogSize+1)
	w.file(".local/share/nordvpn/nord.log", string(big))
	wantCode(t, w.run("disconnect"), term.ExitOK)
	old, err := os.ReadFile(filepath.Join(w.home, ".local/share/nordvpn/nord.log.old"))
	if err != nil || len(old) != len(big) {
		t.Fatalf("nord.log.old: %d bytes, %v", len(old), err)
	}
	if got := w.logText(); !strings.HasPrefix(got, "2026-10-28T09:30:00Z No NordVPN") {
		t.Errorf("new log %q", got)
	}
}

// E21: a write to the log that fails is kept, stops further writes and is the close's error.
func TestLogWriteFailureIsKept(t *testing.T) {
	l := &runLog{w: failCloser{}, now: func() time.Time { return time.Time{} }}
	l.line("one\ntwo")
	l.line("three")
	if err := l.close(); !errors.Is(err, errWrite) {
		t.Errorf("close: %v", err)
	}
}

type failCloser struct{ failWriter }

func (failCloser) Close() error { return nil }

// E21: a log that cannot be opened stops the command before it acts.
func TestUnwritableLogStopsTheCommand(t *testing.T) {
	w := newWorld(t)
	w.imported("nordvpn-uk1").active = "activated"
	if err := os.RemoveAll(filepath.Join(w.home, ".local/share/nordvpn")); err != nil {
		t.Fatal(err)
	}
	w.file(".local/share/nordvpn", "a file where the directory should be")
	wantCode(t, w.run("disconnect"), term.ExitFailure)
	w.wantErr("nord: cannot write the log ~/.local/share/nordvpn/nord.log: ")
	wantChanges(t, w)
}
