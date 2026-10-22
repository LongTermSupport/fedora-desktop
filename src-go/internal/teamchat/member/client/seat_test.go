package client

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	seedRun = "fedcba9876543210"
	sinceMS = 1_750_000_000_000
	// deadRoom is a main room id for a bundle whose homeserver is never reached.
	deadRoom = "!abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
)

// seatCase is a host-held bundle directory for seat disconnected.
type seatCase struct {
	t   *testing.T
	dir string
}

func newSeatCase(t *testing.T) seatCase {
	t.Helper()
	return seatCase{t: t, dir: filepath.Join(t.TempDir(), hsTeam)}
}

// stderrWriter is a stderr that may fail every write.
type stderrWriter struct {
	buf bytes.Buffer
	err error
}

func (s *stderrWriter) Write(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	return s.buf.Write(p)
}

// do runs seat disconnected on c.dir with the seed given, stderr to w.
func (c seatCase) do(w *stderrWriter, seed string) term.ExitCode {
	c.t.Helper()
	inv, err := cli.Parse([]string{"seat", "disconnected", "--bundle", c.dir, "--since", strconv.Itoa(sinceMS), "--seed", seed})
	if err != nil {
		c.t.Fatalf("parse: %v", err)
	}
	e := cli.Env{Stdout: &bytes.Buffer{}, Stderr: w, Getenv: func(string) string { return "" }}
	return seatDisconnected(e, inv, hsDeps{clock: clock.NewFake(boot0, 1_770_000_000_000), wait: noWait{}})
}

func lastObject(t *testing.T, stderr string) exits.Error {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	e, err := exits.Decode([]byte(lines[len(lines)-1]))
	if err != nil {
		t.Fatalf("stderr %q: %v", stderr, err)
	}
	return e
}

func TestSeatDisconnectedPostsOnce(t *testing.T) {
	h := newHS(t).rooms(t)
	c := newSeatCase(t)
	writeSeat(t, c.dir, bundleFor(t, h.srv.URL(), h.main), h.token)
	for range 2 {
		var w stderrWriter
		if code := c.do(&w, seedRun); code != term.ExitOK || w.buf.Len() != 0 {
			t.Fatalf("exit %d, stderr %q", code, w.buf.String())
		}
	}
	if n := h.disconnects(t); n != 1 {
		t.Errorf("%d disconnected notices, want 1", n)
	}
	if n := h.sends(); n != 1 {
		t.Errorf("%d sends, want 1: the second run finds the first's notice", n)
	}
}

func TestSeatDisconnectedNotPossibleIsOneLine(t *testing.T) {
	h := newHS(t).rooms(t)
	c := newSeatCase(t)
	writeSeat(t, c.dir, bundleFor(t, h.srv.URL(), h.main), "not-a-token-the-server-knows")
	var w stderrWriter
	if code := c.do(&w, seedRun); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.buf.String())
	}
	got := w.buf.String()
	if strings.Count(got, "\n") != 1 || !strings.Contains(got, "token was refused") || !strings.Contains(got, seedRun) {
		t.Errorf("stderr %q, want one line naming the run and the refused token", got)
	}
	if strings.Contains(got, "not-a-token") {
		t.Errorf("stderr %q holds the token", got)
	}
	if n := h.sends(); n != 0 {
		t.Errorf("%d sends", n)
	}
}

func TestSeatDisconnectedDeadTerminal(t *testing.T) {
	h := newHS(t).rooms(t)
	c := newSeatCase(t)
	writeSeat(t, c.dir, bundleFor(t, h.srv.URL(), h.main), "not-a-token-the-server-knows")
	for _, errno := range []syscall.Errno{syscall.EPIPE, syscall.EIO, syscall.EBADF} {
		w := stderrWriter{err: errno}
		if code := c.do(&w, seedRun); code != term.ExitOK {
			t.Errorf("stderr %v: exit %d, want 0", errno, code)
		}
	}
}

func TestSeatDisconnectedUnreachable(t *testing.T) {
	c := newSeatCase(t)
	writeSeat(t, c.dir, bundleFor(t, deadURL(t), deadRoom), "some-token")
	var w stderrWriter
	if code := c.do(&w, seedRun); code != 20 {
		t.Fatalf("exit %d, want 20; stderr %q", code, w.buf.String())
	}
	if e := lastObject(t, w.buf.String()); e.Code != exits.CodeUnreachable || e.Team == nil || e.Team.String() != hsTeam {
		t.Errorf("error %+v", e)
	}
}

func TestSeatDisconnectedRefusals(t *testing.T) {
	cases := []struct {
		name, seed, code string
		prepare          func(t *testing.T, c seatCase)
	}{
		{"bad seed", "not-a-run", exits.CodeUsage, func(t *testing.T, c seatCase) {
			t.Helper()
			writeSeat(t, c.dir, bundleFor(t, deadURL(t), deadRoom), "some-token")
		}},
		{"no bundle directory", seedRun, exits.CodeBundle, func(*testing.T, seatCase) {}},
		{"no token", seedRun, exits.CodeBundle, func(t *testing.T, c seatCase) {
			t.Helper()
			writeSeat(t, c.dir, bundleFor(t, deadURL(t), deadRoom), "")
		}},
		{"a shared parent", seedRun, exits.CodeBundle, func(t *testing.T, c seatCase) {
			t.Helper()
			writeSeat(t, c.dir, bundleFor(t, deadURL(t), deadRoom), "some-token")
			if err := os.Chmod(filepath.Dir(c.dir), 0o777); err != nil {
				t.Fatal(err)
			}
		}},
		{"no parent", seedRun, exits.CodeBundle, func(t *testing.T, c seatCase) {
			t.Helper()
			if err := os.Remove(filepath.Dir(c.dir)); err != nil {
				t.Fatal(err)
			}
		}},
		{"not a seat.json", seedRun, exits.CodeBundle, func(t *testing.T, c seatCase) {
			t.Helper()
			writeSeat(t, c.dir, bundleFor(t, deadURL(t), deadRoom), "some-token")
			writeFile(t, filepath.Join(c.dir, "seat.json"), []byte(`{"v":1}`))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newSeatCase(t)
			tc.prepare(t, c)
			var w stderrWriter
			code := c.do(&w, tc.seed)
			e := lastObject(t, w.buf.String())
			if e.Code != tc.code || code != e.Exit {
				t.Errorf("exit %d, error %+v; want code %s", code, e, tc.code)
			}
		})
	}
}

func TestSeatDisconnectedTheBound(t *testing.T) {
	h := newHS(t).rooms(t)
	c := newSeatCase(t)
	writeSeat(t, c.dir, bundleFor(t, h.srv.URL(), h.main), h.token)
	inv, err := cli.Parse([]string{"seat", "disconnected", "--bundle", c.dir, "--since", "1", "--seed", seedRun})
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(boot0, 1_770_000_000_000)
	var w stderrWriter
	stalled := hsDeps{clock: clk, wait: noWait{}, armed: func() { clk.Advance(seatBound) }}
	if code := seatDisconnected(cli.Env{Stderr: &w}, inv, stalled); code != 20 {
		t.Fatalf("exit %d, want 20 once the bound passes; stderr %q", code, w.buf.String())
	}
	if n := h.sends(); n != 0 {
		t.Errorf("%d sends after the bound", n)
	}
}

func TestSeatDisconnectedExported(t *testing.T) {
	h := newHS(t).rooms(t)
	c := newSeatCase(t)
	writeSeat(t, c.dir, bundleFor(t, h.srv.URL(), h.main), h.token)
	inv, err := cli.Parse([]string{"seat", "disconnected", "--bundle", c.dir, "--since", "1", "--seed", seedRun})
	if err != nil {
		t.Fatal(err)
	}
	var w stderrWriter
	if code := SeatDisconnected(cli.Env{Stderr: &w}, inv); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.buf.String())
	}
	if n := h.disconnects(t); n != 1 {
		t.Errorf("%d notices", n)
	}
}
