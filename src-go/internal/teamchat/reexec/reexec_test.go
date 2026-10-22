package reexec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

var helpers = Helpers{
	"hold": func(h *Helper) int {
		if err := h.Ready(); err != nil {
			return 2
		}
		return 0
	},
	"args": func(h *Helper) int {
		if !slices.Equal(h.Args, []string{"a b", "-c", "--"}) {
			return 9
		}
		if err := h.Ready(); err != nil {
			return 2
		}
		return 0
	},
	"early": func(*Helper) int { return 3 },
	"garbled": func(h *Helper) int {
		if _, err := io.WriteString(h.out, "hello\n"); err != nil {
			return 2
		}
		if _, err := io.ReadAll(h.in); err != nil {
			return 2
		}
		return 0
	},
	"late": func(h *Helper) int {
		if err := h.Ready(); err != nil {
			return 2
		}
		return 4
	},
}

func TestMain(m *testing.M) { os.Exit(Main(m, helpers)) }

func TestStartRunsTheHelperAsASecondProcess(t *testing.T) {
	p := Start(t, "hold")
	if p.Pid <= 0 || p.Pid == os.Getpid() {
		t.Fatalf("pid %d, this process %d", p.Pid, os.Getpid())
	}
	if err := unix.Kill(p.Pid, 0); err != nil {
		t.Fatalf("the helper is not alive while held: %v", err)
	}
	p.Release()
}

func TestTheHelperGetsItsArgumentsUnchanged(t *testing.T) {
	Start(t, "args", "a b", "-c", "--").Release()
}

func TestKillEndsTheHelperBySIGKILL(t *testing.T) {
	p := Start(t, "hold")
	p.Kill()
	if err := unix.Kill(p.Pid, 0); !errors.Is(err, unix.ESRCH) {
		t.Fatalf("the helper is still there after Kill: %v", err)
	}
}

// A helper that is never released is killed when its test ends, so no process outlives it.
func TestCleanupKillsAHelperLeftHeld(t *testing.T) {
	var p *Process
	t.Run("held", func(t *testing.T) { p = Start(t, "hold") })
	if err := unix.Kill(p.Pid, 0); !errors.Is(err, unix.ESRCH) {
		t.Fatalf("the helper outlived its test: %v", err)
	}
}

func TestAHelperThatEndsBeforeReadyIsAnError(t *testing.T) {
	p, err := start("early", nil)
	if err == nil || !strings.Contains(err.Error(), "before it was ready") || p != nil {
		t.Fatalf("got %v, %v", p, err)
	}
}

func TestAnUnknownHelperIsAnError(t *testing.T) {
	_, err := start("nobody", nil)
	if err == nil || !strings.Contains(err.Error(), `no helper "nobody"`) {
		t.Fatalf("got %v", err)
	}
}

func TestReleaseReportsANonZeroExit(t *testing.T) {
	p, err := start("late", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.release(); err == nil || !strings.Contains(err.Error(), "exited 4") {
		t.Fatalf("got %v", err)
	}
}

func TestKillReportsAHelperThatWasNotKilled(t *testing.T) {
	p, err := start("late", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.release(); err == nil {
		t.Fatal("want the exit 4")
	}
	if err := p.kill(); err == nil || !strings.Contains(err.Error(), "already ended") {
		t.Fatalf("got %v", err)
	}
}

func TestAFirstLineThatIsNotReadyIsAnErrorAndTheHelperIsKilled(t *testing.T) {
	_, err := start("garbled", nil)
	if err == nil || !strings.Contains(err.Error(), "not its ready line") {
		t.Fatalf("got %v", err)
	}
}

// Release and Kill report through the test their helper was started from.
func TestReleaseAndKillFailTheirTest(t *testing.T) {
	rec := &recordTB{TB: t}
	p := Start(rec, "late")
	p.Release()
	p.Kill()
	if len(rec.errs) != 2 || !strings.Contains(rec.errs[0], "exited 4") || !strings.Contains(rec.errs[1], "already ended") {
		t.Fatalf("errors %q", rec.errs)
	}
}

type recordTB struct {
	testing.TB

	errs []string
}

func (r *recordTB) Error(args ...any) { r.errs = append(r.errs, fmt.Sprint(args...)) }

func TestMainReportsAnUnwritableStderr(t *testing.T) {
	if code := mainWith(fakeM(7), env("nobody"), helpers, nil, &bytes.Buffer{}, failWriter{}, nil); code != unknownHelper {
		t.Fatalf("code %d", code)
	}
}

// The child side, run in this process through the seam.
func TestMainRunsTheTestsWhenNoHelperIsNamed(t *testing.T) {
	code := mainWith(fakeM(7), env(""), helpers, nil, &bytes.Buffer{}, &bytes.Buffer{}, nil)
	if code != 7 {
		t.Fatalf("code %d", code)
	}
}

func TestMainRunsTheNamedHelperWithTheArgumentsAfterTheMark(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("go\n")
	argv := []string{"bin", "-test.run=^$", argsMark, "a b", "-c", "--"}
	code := mainWith(fakeM(7), env("args"), helpers, in, &out, &bytes.Buffer{}, argv)
	if code != 0 || !strings.HasPrefix(out.String(), readyWord+" ") {
		t.Fatalf("code %d out %q", code, out.String())
	}
}

func TestMainRefusesAnUnknownHelper(t *testing.T) {
	var errOut bytes.Buffer
	code := mainWith(fakeM(7), env("nobody"), helpers, nil, &bytes.Buffer{}, &errOut, nil)
	if code != unknownHelper || !strings.Contains(errOut.String(), `no helper "nobody"`) {
		t.Fatalf("code %d stderr %q", code, errOut.String())
	}
}

func TestReadyWaitsForTheReleaseLineOrTheEnd(t *testing.T) {
	for _, in := range []string{"go\n", ""} {
		var out bytes.Buffer
		h := &Helper{in: strings.NewReader(in), out: &out, pid: 42}
		if err := h.Ready(); err != nil {
			t.Fatalf("stdin %q: %v", in, err)
		}
		if out.String() != "ready 42\n" {
			t.Fatalf("out %q", out.String())
		}
	}
}

func TestReadyReportsAnUnwritableStdout(t *testing.T) {
	h := &Helper{in: strings.NewReader(""), out: failWriter{}, pid: 1}
	if err := h.Ready(); err == nil {
		t.Fatal("want an error")
	}
}

func TestReadyReportsAnUnreadableStdin(t *testing.T) {
	h := &Helper{in: failReader{}, out: &bytes.Buffer{}, pid: 1}
	if err := h.Ready(); err == nil {
		t.Fatal("want an error")
	}
}

func TestParseReady(t *testing.T) {
	for line, want := range map[string]int{"ready 12\n": 12, "ready 0\n": -1, "ready x\n": -1, "hello\n": -1, "ready 12": -1} {
		pid, err := parseReady(line)
		if want < 0 {
			if err == nil {
				t.Errorf("%q: want an error", line)
			}
			continue
		}
		if err != nil || pid != want {
			t.Errorf("%q: %d, %v", line, pid, err)
		}
	}
}

// A pid is signalled only when it is this process's own child: a pid of 0 or -1 would signal
// a whole group, and a helper's claim alone is not proof.
func TestChildOfAcceptsOnlyThisProcesssChildren(t *testing.T) {
	p := Start(t, "hold")
	if err := childOf(p.Pid); err != nil {
		t.Fatalf("own child refused: %v", err)
	}
	for _, pid := range []int{0, -1, 1, os.Getpid()} {
		if err := childOf(pid); err == nil {
			t.Errorf("pid %d accepted", pid)
		}
	}
	p.Release()
}

func TestStatParent(t *testing.T) {
	for stat, want := range map[string]int{
		"12 (a) b) S 34 5": 34,
		"12 (a) S x 5":     -1,
		"12 a S 34":        -1,
		"12 (a)":           -1,
	} {
		got, err := statParent(stat)
		if want < 0 {
			if err == nil {
				t.Errorf("%q: want an error, got %d", stat, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("%q: %d, %v", stat, got, err)
		}
	}
}

func TestArgsAfterTheMark(t *testing.T) {
	if got := argsOf([]string{"bin", "-x"}); got != nil {
		t.Fatalf("no mark: %q", got)
	}
	if got := argsOf([]string{"bin", argsMark}); len(got) != 0 {
		t.Fatalf("empty: %q", got)
	}
}

func TestDescribeEnd(t *testing.T) {
	if got := describe(&exitInfo{signal: syscall.SIGKILL}); !strings.Contains(got, "killed") {
		t.Fatalf("%q", got)
	}
	if got := describe(&exitInfo{err: errors.New("boom")}); !strings.Contains(got, "boom") {
		t.Fatalf("%q", got)
	}
}

type fakeM int

func (m fakeM) Run() int { return int(m) }

func env(helper string) func(string) string {
	return func(key string) string {
		if key == helperEnv {
			return helper
		}
		return ""
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("broken") }
