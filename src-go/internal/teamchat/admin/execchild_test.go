package admin

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const testSelf = "/usr/libexec/teamchat/teamchat-admin.1"

var ctlAccount = User{UID: 990, GID: 991}

func lookupCtl(name string) (User, bool, error) {
	if name != paths.CtlUser {
		return User{}, false, nil
	}
	return ctlAccount, true, nil
}

func parkRequest(t *testing.T) Request {
	t.Helper()
	return Request{Op: apply.OpMemberPark, Team: alpha(t), Args: proto.Obj(proto.M("handle", proto.Str(sampleHandle)))}
}

// childAnswering is an ExecChild over a fake whose child prints stdout and stderr and exits
// code; the request it read is put in *stdin.
func childAnswering(stdout, stderr string, code int, stdin *[]byte) ExecChild {
	f := &execx.Fake{}
	f.OnFunc([]string{testSelf, "ctl-child"}, func(c execx.Cmd) (execx.Result, error) {
		raw, err := io.ReadAll(c.Stdin)
		if err != nil {
			return execx.Result{}, err
		}
		if stdin != nil {
			*stdin = raw
		}
		return execx.Result{Stdout: []byte(stdout), Stderr: []byte(stderr), ExitCode: code}, nil
	})
	return ExecChild{Runner: f, Self: testSelf, Lookup: lookupCtl}
}

// TestExecChildStartsTheChildBySpec is CS1: the account's ids and no groups, `/`, PATH alone and
// nothing inherited, the request on stdin.
func TestExecChildStartsTheChildBySpec(t *testing.T) {
	var stdin []byte
	c := childAnswering(`{"v":1,"ok":true,"changed":["member x parked"]}`+"\n", "", 0, &stdin)
	a, err := c.Ask(t.Context(), parkRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if !a.OK || !slices.Equal(a.Changed, []string{"member x parked"}) {
		t.Fatalf("answer %+v", a)
	}
	want, err := EncodeRequest(parkRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stdin, want) {
		t.Fatalf("stdin %s", stdin)
	}
	f, isFake := c.Runner.(*execx.Fake)
	if !isFake {
		t.Fatal("not the fake")
	}
	checkChildCmd(t, f.Calls()[0])
}

// checkChildCmd is CS1's start: the binary as ctl-child in /, PATH alone, the account's ids
// and no groups, and the output captured by the run.
func checkChildCmd(t *testing.T, got execx.Cmd) {
	t.Helper()
	if got.Name != testSelf || !slices.Equal(got.Args, []string{"ctl-child"}) || got.Dir != "/" {
		t.Fatalf("command %q in %q", got.Line(), got.Dir)
	}
	if !got.CleanEnv || !slices.Equal(got.Env, []string{"PATH=/usr/bin:/bin"}) || len(got.Unset) != 0 {
		t.Fatalf("environment %v %q", got.CleanEnv, got.Env)
	}
	if want := (syscall.Credential{Uid: 990, Gid: 991, Groups: []uint32{}}); got.Credential == nil || !reflect.DeepEqual(*got.Credential, want) {
		t.Fatalf("credential %+v", got.Credential)
	}
	if got.Stdout == nil || got.Stderr == nil || len(got.ExtraFiles) != 0 {
		t.Fatal("the child's output is not captured by the run, or it inherits files")
	}
}

func TestExecChildAFailedAnswerWithExitOneIsTheAnswer(t *testing.T) {
	c := childAnswering(`{"v":1,"ok":false,"error":{"class":"refused","message":"no"}}`+"\n", "", 1, nil)
	a, err := c.Ask(t.Context(), parkRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if a.OK || a.Error.Class != ClassRefused || a.ExitCode() != ExitRefused {
		t.Fatalf("answer %+v", a)
	}
}

// TestExecChildRefusesWhatBreaksTheRules is CS4: the exit code is never passed through; an
// answer and an exit that disagree, another code, a signal, an answer that does not decode, and
// a child that cannot start are ErrChild, which the verbs report as 1.
func TestExecChildRefusesWhatBreaksTheRules(t *testing.T) {
	okAnswer := `{"v":1,"ok":true,"changed":[]}` + "\n"
	failed := `{"v":1,"ok":false,"error":{"class":"failed","message":"no"}}` + "\n"
	cases := map[string]struct {
		stdout string
		code   int
	}{
		"ok with exit 1":     {okAnswer, 1},
		"failed with exit 0": {failed, 0},
		"exit 2":             {failed, 2},
		"exit 78":            {okAnswer, 78},
		"no answer":          {"", 0},
		"not json":           {"hello\n", 0},
		"two answers":        {okAnswer + okAnswer, 0},
		"no newline":         {strings.TrimSuffix(okAnswer, "\n"), 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := childAnswering(tc.stdout, "the child's own words\n", tc.code, nil)
			_, err := c.Ask(t.Context(), parkRequest(t))
			if !errors.Is(err, ErrChild) || ExitCodeOf(err) != term.ExitFailure {
				t.Fatalf("got %v", err)
			}
			if !strings.Contains(err.Error(), "the child's own words") || strings.Contains(err.Error(), "\n") {
				t.Fatalf("the stderr is not shown on one line: %q", err)
			}
		})
	}
	for name, startErr := range map[string]error{
		"signal":       &execx.ExitError{Name: testSelf, Code: -1, Signal: syscall.SIGKILL},
		"cannot start": errors.New("exec format error"),
	} {
		t.Run(name, func(t *testing.T) {
			f := &execx.Fake{}
			f.OnError([]string{testSelf}, startErr)
			_, err := ExecChild{Runner: f, Self: testSelf, Lookup: lookupCtl}.Ask(t.Context(), parkRequest(t))
			if !errors.Is(err, ErrChild) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

// TestExecChildCapsTheAnswerAndStderr is CS3: an answer over maxAnswer is refused, and neither
// stream is held past its cap.
func TestExecChildCapsTheAnswerAndStderr(t *testing.T) {
	big := `{"v":1,"ok":true,"changed":["` + strings.Repeat("a", maxAnswer) + `"]}` + "\n"
	c := childAnswering(big, strings.Repeat("e", 3*maxLine), 0, nil)
	_, err := c.Ask(t.Context(), parkRequest(t))
	if !errors.Is(err, ErrChild) || !strings.Contains(err.Error(), "over") {
		t.Fatalf("got %v", err)
	}
	if len(err.Error()) > 2*maxLine {
		t.Fatalf("the error holds %d bytes", len(err.Error()))
	}
	w := &capped{limit: 4}
	for _, chunk := range []string{"ab", "cdef", "gh"} {
		if n, err := w.Write([]byte(chunk)); err != nil || n != len(chunk) {
			t.Fatalf("write %d %v", n, err)
		}
	}
	if w.buf.String() != "abcd" || !w.over {
		t.Fatalf("held %q, over %v", w.buf.String(), w.over)
	}
}

// TestExecChildAccountFailuresAreConfig is CS1: no account, an account of root's, a lookup that
// fails and a binary path that is not absolute and clean are 78, and no child starts.
func TestExecChildAccountFailuresAreConfig(t *testing.T) {
	lookups := map[string]func(string) (User, bool, error){
		"no account": func(string) (User, bool, error) { return User{}, false, nil },
		"root uid":   func(string) (User, bool, error) { return User{UID: 0, GID: 991}, true, nil },
		"root gid":   func(string) (User, bool, error) { return User{UID: 990, GID: 0}, true, nil },
		"lookup":     func(string) (User, bool, error) { return User{}, false, errors.New("nss is down") },
	}
	for name, lookup := range lookups {
		t.Run(name, func(t *testing.T) {
			f := &execx.Fake{}
			_, err := ExecChild{Runner: f, Self: testSelf, Lookup: lookup}.Ask(t.Context(), parkRequest(t))
			if ExitCodeOf(err) != ExitConfig || len(f.Calls()) != 0 {
				t.Fatalf("got %v (exit %d), %d calls", err, ExitCodeOf(err), len(f.Calls()))
			}
			if name == "no account" && !strings.Contains(err.Error(), paths.CtlUser) {
				t.Fatalf("the error does not name the account: %v", err)
			}
		})
	}
	f := &execx.Fake{}
	if _, err := (ExecChild{Runner: f, Self: "teamchat-admin", Lookup: lookupCtl}).Ask(t.Context(), parkRequest(t)); ExitCodeOf(err) != ExitConfig {
		t.Fatalf("a relative self: %v", err)
	}
	if _, err := (ExecChild{Runner: f, Self: testSelf, Lookup: lookupCtl}).Ask(t.Context(), Request{Op: "Bad Op"}); err == nil || errors.Is(err, ErrChild) {
		t.Fatalf("a request that does not encode: %v", err)
	}
}

// TestExecChildEndsWithTheContext is CS5: a context that ends is the context's error, never the
// child's answer.
func TestExecChildEndsWithTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	f := &execx.Fake{}
	f.OnFunc([]string{testSelf}, func(execx.Cmd) (execx.Result, error) {
		cancel()
		return execx.Result{Stdout: []byte(`{"v":1,"ok":true,"changed":[]}` + "\n")}, nil
	})
	_, err := ExecChild{Runner: f, Self: testSelf, Lookup: lookupCtl}.Ask(ctx, parkRequest(t))
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrChild) {
		t.Fatalf("got %v", err)
	}
}

// deadlineRunner records whether a run's context carries a deadline.
type deadlineRunner struct {
	fake    *execx.Fake
	bounded bool
}

func (r *deadlineRunner) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	_, r.bounded = ctx.Deadline()
	return r.fake.Run(ctx, c)
}

// TestExecChildIsBoundedByChildWait is CS5's bound: every child runs under a deadline of
// ChildWait, and a child that outlives it is the deadline's error, never its answer.
func TestExecChildIsBoundedByChildWait(t *testing.T) {
	if ChildWait != 10*time.Minute {
		t.Fatalf("ChildWait = %v", ChildWait)
	}
	c := childAnswering(`{"v":1,"ok":true,"changed":[]}`+"\n", "", 0, nil)
	fake, isFake := c.Runner.(*execx.Fake)
	if !isFake {
		t.Fatal("not the fake")
	}
	r := &deadlineRunner{fake: fake}
	c.Runner = r
	if _, err := c.Ask(t.Context(), parkRequest(t)); err != nil || !r.bounded {
		t.Fatalf("err %v, bounded %v", err, r.bounded)
	}
	c.bound = func(ctx context.Context) (context.Context, context.CancelFunc) {
		return context.WithDeadline(ctx, time.Time{})
	}
	_, err := c.Ask(t.Context(), parkRequest(t))
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrChild) {
		t.Fatalf("got %v", err)
	}
}

// TestSelfPathIsTheRunningBinary is CS2.
func TestSelfPathIsTheRunningBinary(t *testing.T) {
	self, err := SelfPath()
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if self != exe {
		t.Fatalf("self %q, executable %q", self, exe)
	}
	readlink := func(target string, err error) func(string) (string, error) {
		return func(link string) (string, error) {
			if link != "/proc/self/exe" {
				t.Fatalf("read %s", link)
			}
			return target, err
		}
	}
	for name, rl := range map[string]func(string) (string, error){
		"relative":  readlink("teamchat-admin", nil),
		"unclean":   readlink("/usr/libexec/../bin/teamchat-admin", nil),
		"deleted":   readlink("/usr/libexec/teamchat/teamchat-admin.1 (deleted)", nil),
		"no answer": readlink("", errors.New("no /proc")),
	} {
		if _, err := selfPath(rl); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if got, err := selfPath(readlink(testSelf, nil)); err != nil || got != testSelf {
		t.Fatalf("got %q, %v", got, err)
	}
}
