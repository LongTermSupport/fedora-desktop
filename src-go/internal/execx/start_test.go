package execx

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestStartExitCode(t *testing.T) {
	var out bytes.Buffer
	p, err := OS{}.Start(Cmd{Name: "/bin/sh", Args: []string{"-c", "echo hi; exit 3"}, Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	if p.Pid() <= 0 {
		t.Errorf("pid %d", p.Pid())
	}
	st, err := p.Wait()
	if err != nil || st != (ExitStatus{Code: 3}) {
		t.Fatalf("wait %+v, %v", st, err)
	}
	if out.String() != "hi\n" {
		t.Errorf("stdout %q", out.String())
	}
	again, err := p.Wait()
	if err != nil || again != st {
		t.Errorf("a second wait %+v, %v", again, err)
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		t.Errorf("a signal to a child that has gone: %v", err)
	}
}

func TestStartSignalled(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	p, startErr := OS{}.Start(Cmd{Name: "/bin/sh", Args: []string{"-c", "echo up; exec sleep 60"}, Stdout: w})
	if cerr := w.Close(); cerr != nil {
		t.Fatal(cerr)
	}
	if startErr != nil {
		t.Fatal(startErr)
	}
	buf := make([]byte, 3)
	if _, rerr := r.Read(buf); rerr != nil {
		t.Fatal(rerr)
	}
	if serr := p.Signal(syscall.SIGTERM); serr != nil {
		t.Fatal(serr)
	}
	st, err := p.Wait()
	if err != nil || st != (ExitStatus{Code: 128 + int(syscall.SIGTERM), Signal: syscall.SIGTERM}) {
		t.Fatalf("wait %+v, %v", st, err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStartNewSessionAndEnv(t *testing.T) {
	var out bytes.Buffer
	p, err := OS{}.Start(Cmd{
		Name: "/bin/sh", Args: []string{"-c", `echo "$EXECX_START_PROBE"; ps -o sid= -p $$; ps -o pid= -p $$`},
		Env: []string{"EXECX_START_PROBE=yes"}, NewSession: true, Stdout: &out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Wait(); err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(out.String())
	if len(lines) != 3 || lines[0] != "yes" || lines[1] != lines[2] {
		t.Errorf("a new session's child said %q; want its env and its sid equal to its pid", out.String())
	}
}

func TestStartRefusals(t *testing.T) {
	if _, err := (OS{}).Start(Cmd{Name: "execx-no-such-program"}); !IsNotInstalled(err) {
		t.Errorf("a missing program: %v", err)
	}
	if _, err := (OS{}).Start(Cmd{Name: "/bin/sh", Feed: &Feed{}}); !errors.Is(err, ErrFeedUnsupported) {
		t.Errorf("a feed: %v", err)
	}
	if _, err := (OS{}).Start(Cmd{Name: "/bin/sh", CleanEnv: true, Unset: []string{"X"}}); !errors.Is(err, ErrBadCmd) {
		t.Errorf("a contradictory command: %v", err)
	}
	if _, err := (OS{}).Start(Cmd{Name: "/"}); err == nil || IsNotInstalled(err) {
		t.Errorf("a directory as the program: %v", err)
	}
}

type fakeStarted struct{ pid int }

func (f fakeStarted) Pid() int                  { return f.pid }
func (fakeStarted) Wait() (ExitStatus, error)   { return ExitStatus{}, nil }
func (fakeStarted) Signal(syscall.Signal) error { return nil }

func TestFakeStart(t *testing.T) {
	var f Fake
	f.OnStart([]string{"link"}, func(Cmd) (Started, error) { return fakeStarted{pid: 7}, nil })
	p, err := f.Start(Cmd{Name: "link", Args: []string{"--run", "x"}})
	if err != nil || p.Pid() != 7 {
		t.Fatalf("start %v, %v", p, err)
	}
	if _, err := f.Start(Cmd{Name: "other"}); !errors.Is(err, ErrUnexpected) {
		t.Errorf("an unscripted start: %v", err)
	}
	if _, err := f.Start(Cmd{Name: "link", Feed: &Feed{}}); !errors.Is(err, ErrFeedUnsupported) {
		t.Errorf("a feed: %v", err)
	}
	if got := f.Starts(); len(got) != 3 || got[0].Line() != "link --run x" {
		t.Errorf("starts %v", got)
	}
}
