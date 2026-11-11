package reexec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/leakcheck"
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
	// skips finds its environment unable to do what it is for, and says so.
	"skips": func(*Helper) int {
		if _, err := fmt.Fprintln(os.Stderr, "no mount here"); err != nil {
			return 2
		}
		return ExitSkip
	},
	"garbled": func(h *Helper) int {
		if _, err := io.WriteString(h.out, "hello\n"); err != nil {
			return 2
		}
		if _, err := io.ReadAll(h.in); err != nil {
			return 2
		}
		return 0
	},
	"files": func(h *Helper) int {
		f := os.NewFile(3, "given")
		if _, err := f.WriteString("from the helper\n"); err != nil {
			return 5
		}
		if err := errors.Join(f.Close(), h.Ready()); err != nil {
			return 2
		}
		return 0
	},
	// userns is ready only as root of a user namespace whose 0 is Args[0], the parent's uid.
	"userns": func(h *Helper) int {
		m, err := os.ReadFile("/proc/self/uid_map")
		if err != nil || strings.Join(strings.Fields(string(m)), " ") != "0 "+h.Args[0]+" 1" || os.Getuid() != 0 {
			return 6
		}
		if err := h.Ready(); err != nil {
			return 2
		}
		return 0
	},
	"echo": func(h *Helper) int {
		if _, err := io.WriteString(h.out, strings.Join(h.Args, ",")); err != nil {
			return 2
		}
		return 5
	},
	"late": func(h *Helper) int {
		if err := h.Ready(); err != nil {
			return 2
		}
		return 4
	},
}

// The leak check holds that every Start's run of its helper has ended once its test has, on
// each path a test takes: ready, ended early, a first line that is not ready, released, killed.
func TestMain(m *testing.M) { os.Exit(Main(leakcheck.Tests(m), helpers)) }

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

func TestCommandIsAFreeRunningHelper(t *testing.T) {
	c := Command(t, "echo", "a b", "--")
	var out bytes.Buffer
	c.Stdout = &out
	res, err := execx.OS{}.Run(t.Context(), c)
	if res.ExitCode != 5 || out.String() != "a b,--" {
		t.Fatalf("the helper exited %d (%v) and said %q", res.ExitCode, err, out.String())
	}
}

func TestStartInUserNamespaceRunsTheHelperAsItsRoot(t *testing.T) {
	p := StartInUserNamespace(t, "userns", strconv.Itoa(os.Getuid()))
	if p.Pid <= 0 || p.Pid == os.Getpid() {
		t.Fatalf("pid %d, this process %d", p.Pid, os.Getpid())
	}
	p.Release()
}

// skipRecorder is a test whose Skipf is recorded and ends the goroutine it is called in, as
// testing's does.
type skipRecorder struct {
	testing.TB

	skipped string
}

func (r *skipRecorder) Helper() {}

func (r *skipRecorder) Skipf(format string, args ...any) {
	r.skipped = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

// A helper that finds its environment unable to do what it is for (a kernel that allows a user
// namespace but no mount in it, as an AppArmor-restricted CI runner does) ends its test as a
// skip that shows what the helper said, as a kernel with no user namespace does.
func TestAHelperThatCannotDoItsWorkHereSkipsItsTest(t *testing.T) {
	rec := &skipRecorder{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		StartInUserNamespace(rec, "skips")
		rec.skipped = "the helper was started"
	}()
	<-done
	if strings.Contains(rec.skipped, "no unprivileged user namespace") {
		t.Skip(rec.skipped)
	}
	if !strings.Contains(rec.skipped, "no mount here") || strings.Contains(rec.skipped, "started") {
		t.Errorf("the test was ended with %q", rec.skipped)
	}
}

// Only a helper's own exit ExitSkip is a skip: any other code, a signal and a failure to run
// are a failure, so a helper that is broken is never taken for an environment that is limited.
func TestOnlyTheSkipExitIsASkip(t *testing.T) {
	for name, c := range map[string]struct {
		end  *exitInfo
		want bool
	}{
		"the skip exit":     {&exitInfo{code: ExitSkip, stderr: "no mount here"}, true},
		"another exit":      {&exitInfo{code: 3}, false},
		"the unknown exit":  {&exitInfo{code: unknownHelper}, false},
		"a signal":          {&exitInfo{code: ExitSkip, signal: syscall.SIGKILL}, false},
		"a failure to run":  {&exitInfo{code: ExitSkip, err: syscall.EPERM}, false},
		"no exit code kept": {&exitInfo{}, false},
	} {
		said, got := skippedBy(endedEarly("h", c.end))
		if got != c.want || (got && said != "no mount here") {
			t.Errorf("%s: skip %v saying %q, want %v", name, got, said, c.want)
		}
	}
	if _, got := skippedBy(errors.New("something else")); got {
		t.Error("an error that is not an end is a skip")
	}
	if _, got := skippedBy(nil); got {
		t.Error("no error is a skip")
	}
}

// A helper that could not start keeps the reason it could not, so a caller can tell a refused
// namespace from a helper that failed.
func TestAHelperThatCouldNotStartKeepsItsError(t *testing.T) {
	end := &exitInfo{err: syscall.EPERM, stderr: "x"}
	if err := endedEarly("h", end); !errors.Is(err, syscall.EPERM) || !strings.Contains(err.Error(), `"h"`) {
		t.Fatalf("got %v", err)
	}
	if err := endedEarly("h", &exitInfo{code: 3}); !strings.Contains(err.Error(), "exited 3") {
		t.Fatalf("got %v", err)
	}
}

func TestTheHelperGetsItsArgumentsUnchanged(t *testing.T) {
	Start(t, "args", "a b", "-c", "--").Release()
}

func TestStartFilesGivesTheHelperDescriptorThreeOn(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	p := StartFiles(t, []*os.File{w}, "files")
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	p.Release()
	got, err := io.ReadAll(r)
	if err = errors.Join(err, r.Close()); err != nil {
		t.Fatal(err)
	}
	if string(got) != "from the helper\n" {
		t.Fatalf("read %q", got)
	}
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
	p, err := start("early", nil, nil, false)
	if err == nil || !strings.Contains(err.Error(), "before it was ready") || p != nil {
		t.Fatalf("got %v, %v", p, err)
	}
}

func TestAnUnknownHelperIsAnError(t *testing.T) {
	_, err := start("nobody", nil, nil, false)
	if err == nil || !strings.Contains(err.Error(), `no helper "nobody"`) {
		t.Fatalf("got %v", err)
	}
}

func TestReleaseReportsANonZeroExit(t *testing.T) {
	p, err := start("late", nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.release(); err == nil || !strings.Contains(err.Error(), "exited 4") {
		t.Fatalf("got %v", err)
	}
}

// A helper someone else killed can be reaped before its runner closes done: kill then meets
// ESRCH for a pid that has ended, which is the end it was asked for, not a failure.
func TestKillOfAHelperReapedBeforeDoneWaitsForItsEnd(t *testing.T) {
	gone, err := start("hold", nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if killErr := unix.Kill(gone.Pid, unix.SIGKILL); killErr != nil {
		t.Fatal(killErr)
	}
	<-gone.done
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	p := &Process{Pid: gone.Pid, stdin: w, done: make(chan struct{})}
	// The runner's end comes only once kill has signalled and closed stdin, as a real runner's
	// can: done is still open when the signal meets the reaped pid.
	runnerErr := make(chan error, 1)
	go func() {
		_, readErr := io.ReadAll(r)
		runnerErr <- errors.Join(readErr, r.Close())
		p.end = gone.end
		close(p.done)
	}()
	t.Cleanup(func() {
		if e := <-runnerErr; e != nil {
			t.Error(e)
		}
	})
	if err = p.kill(); err != nil {
		t.Fatalf("kill of a reaped helper whose runner had not yet closed done: %v", err)
	}
}

// The cleanup decides once whether the helper still runs. A helper the test itself killed can
// be reaped between that decision and the signal, and that end is not the cleanup's failure,
// however it ended: what the cleanup holds is only that no helper outlives its test.
func TestStopOfAHelperThatHasEndedReportsNothing(t *testing.T) {
	p, err := start("late", nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.release(); err == nil {
		t.Fatal("want the exit 4")
	}
	if err := p.stop(); err != nil {
		t.Fatalf("stop of a helper that had already ended: %v", err)
	}
}

// A helper reaped before its runner closes done, with an end that was not SIGKILL, is stopped
// with no error: kill would call that end a failure, stop does not judge how it ended.
func TestStopOfAHelperReapedBeforeDoneAcceptsAnyEnd(t *testing.T) {
	gone, err := start("late", nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if relErr := gone.release(); relErr == nil {
		t.Fatal("want the exit 4")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	p := &Process{Pid: gone.Pid, stdin: w, done: make(chan struct{})}
	runnerErr := make(chan error, 1)
	go func() {
		_, readErr := io.ReadAll(r)
		runnerErr <- errors.Join(readErr, r.Close())
		p.end = gone.end
		close(p.done)
	}()
	t.Cleanup(func() {
		if e := <-runnerErr; e != nil {
			t.Error(e)
		}
	})
	if err := p.stop(); err != nil {
		t.Fatalf("stop of a reaped helper that exited 4: %v", err)
	}
}

func TestKillReportsAHelperThatWasNotKilled(t *testing.T) {
	p, err := start("late", nil, nil, false)
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
	_, err := start("garbled", nil, nil, false)
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
