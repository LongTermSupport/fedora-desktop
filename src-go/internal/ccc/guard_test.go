package ccc

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestStripSuspendDropsOnlyTheSuspendByte(t *testing.T) {
	for in, want := range map[string]string{"": "", "abc": "abc", "\x1a": "", "a\x1ab\x1a\x1ac": "abc", "\x03\x04": "\x03\x04"} {
		if got := string(stripSuspend([]byte(in))); got != want {
			t.Fatalf("%q: got %q", in, got)
		}
	}
}

func TestTheGuardRunsTheChildOnATerminalAndFiltersItsInput(t *testing.T) {
	out := &bytes.Buffer{}
	g := &PtyGuard{Foreground: execx.OS{}, In: strings.NewReader("ab\x1acd\n"), Out: out, InFd: -1}
	exit, err := g.Run(execx.Cmd{Name: "sh", Args: []string{"-c", `test -t 0 || exit 9; IFS= read -r line; printf '<%s>' "$line"; exit 4`}})
	if err != nil {
		t.Fatal(err)
	}
	if exit.Code != 4 {
		t.Fatalf("exit %d, output %q", exit.Code, out.String())
	}
	if !strings.Contains(out.String(), "<abcd>") {
		t.Fatalf("output %q", out.String())
	}
}

func TestTheGuardGivesTheSessionTheTerminalsSizeAndPutsTheModeBack(t *testing.T) {
	outer, person, err := openPty()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cerr := errors.Join(outer.Close(), person.Close()); cerr != nil {
			t.Error(cerr)
		}
	})
	fd := int(person.Fd())
	if err = unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Row: 31, Col: 101}); err != nil {
		t.Fatal(err)
	}
	before, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	g := &PtyGuard{Foreground: execx.OS{}, In: strings.NewReader(""), Out: out, InFd: fd}
	if _, err = g.Run(execx.Cmd{Name: "stty", Args: []string{"size"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "31 101") {
		t.Fatalf("output %q", out.String())
	}
	after, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil || after.Lflag != before.Lflag || after.Iflag != before.Iflag {
		t.Fatalf("mode not restored: %v", err)
	}
}

func TestTheGuardPassesAWindowChangeOn(t *testing.T) {
	outer, person, err := openPty()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cerr := errors.Join(outer.Close(), person.Close()); cerr != nil {
			t.Error(cerr)
		}
	})
	g := &PtyGuard{Foreground: execx.OS{}, In: strings.NewReader(""), Out: &bytes.Buffer{}, InFd: int(person.Fd())}
	exit, err := g.Run(execx.Cmd{Name: "sh", Args: []string{"-c", `kill -WINCH "$PPID"; sleep 0.3`}})
	if err != nil || exit.Code != 0 {
		t.Fatalf("exit %d, %v", exit.Code, err)
	}
}

func TestTheGuardRefusesAnInputThatIsNotATerminal(t *testing.T) {
	g := &PtyGuard{Foreground: execx.OS{}, In: strings.NewReader(""), Out: &bytes.Buffer{}, InFd: int(os.Stdin.Fd())}
	if _, err := unix.IoctlGetTermios(g.InFd, unix.TCGETS); err == nil {
		t.Skip("the test's stdin is a terminal")
	}
	if _, err := g.Run(execx.Cmd{Name: "true"}); err == nil {
		t.Fatal("want an error")
	}
}

func TestTheGuardReportsAChildThatCannotStart(t *testing.T) {
	g := &PtyGuard{Foreground: execx.OS{}, In: strings.NewReader(""), Out: &bytes.Buffer{}, InFd: -1}
	if _, err := g.Run(execx.Cmd{Name: "/nonexistent/program"}); err == nil {
		t.Fatal("want an error")
	}
}
