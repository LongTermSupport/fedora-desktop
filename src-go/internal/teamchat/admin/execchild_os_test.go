package admin

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// TestMain is the test binary, or, started as `ctl-child`, the real child verb over this
// process, so the pipe runs between two processes: root's start is checked by the child itself.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "ctl-child" {
		os.Exit(int(Run(context.Background(), Env{
			Args: os.Args[1:], Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Proc: scratchProcess{OSProcess()},
			Sys: &System{Open: reexecOpen},
		})))
	}
	os.Exit(m.Run())
}

// floodTeam is the team whose Opener, in the re-executed child, writes more than an answer may
// hold before it fails: a child that floods root.
const floodTeam = "flood"

// reexecOpen is the re-executed child's Opener: no guest token copies, so member.disconnected
// answers ok; for floodTeam it floods stdout first.
func reexecOpen(_ context.Context, team paths.TeamID, _ int, _ string) (Opened, error) {
	if team.String() == floodTeam {
		if _, err := os.Stdout.Write(bytes.Repeat([]byte("x"), 2*maxAnswer)); err != nil {
			return Opened{}, err
		}
		return Opened{}, errors.New("flooded")
	}
	return Opened{Deps: apply.Deps{GuestDir: "/nonexistent-guest-copies"}}, nil
}

// scratchAccount is the child's account in these tests: ids no account here holds.
var scratchAccount = User{UID: 4242, GID: 4343}

// scratchProcess is this process with the scratch account as teamchat-ctl's: its ids, groups,
// directory and environment are the real ones the child checks.
type scratchProcess struct{ Process }

func (scratchProcess) LookupUser(name string) (User, bool, error) {
	return scratchAccount, name == paths.CtlUser, nil
}

// osChild is ExecChild over execx.OS, Self a copy of this test binary the scratch account can
// run (the build and its directory are root's alone, mode 0700). It needs root to change ids.
func osChild(t *testing.T) ExecChild {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("starting the child as another account needs root; the Fake covers the start")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	self := filepath.Join(dir, "teamchat-admin")
	copyFile(t, exe, self)
	for _, p := range []string{filepath.Dir(dir), dir, self} {
		if err := os.Chown(p, scratchAccount.UID, scratchAccount.GID); err != nil {
			t.Fatal(err)
		}
	}
	return ExecChild{Runner: execx.OS{}, Self: self, Lookup: func(string) (User, bool, error) { return scratchAccount, true, nil }}
}

// copyFile copies src to dst with src's mode, each opened in its own directory.
func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	info, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	in, err := os.OpenInRoot(filepath.Dir(src), filepath.Base(src))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := in.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	root, err := os.OpenRoot(filepath.Dir(dst))
	if err != nil {
		t.Fatal(err)
	}
	out, openErr := root.OpenFile(filepath.Base(dst), os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if openErr != nil {
		t.Fatal(errors.Join(openErr, root.Close()))
	}
	_, copyErr := io.Copy(out, in)
	if err := errors.Join(copyErr, out.Close(), root.Close()); err != nil {
		t.Fatal(err)
	}
}

// disconnectedRequest is member.disconnected of a member of team name, alpha's input renamed.
func disconnectedRequest(t *testing.T, name string) Request {
	t.Helper()
	doc := alphaDoc()
	docTeam(doc)["team"] = name
	in, err := declare.DecodeInput([]byte(encodeDoc(t, doc)))
	if err != nil {
		t.Fatal(err)
	}
	return Request{Op: OpMemberDisconnected, Team: in.ID(), Args: proto.Obj(
		proto.M(keyTeamFile, parsed(t, in.Team.Canonical())), proto.M(keyHandle, proto.Str(sampleHandle)),
		proto.M(keyRun, proto.Str("0123456789abcdef")), proto.M(keySinceMS, proto.Int(t0ms)),
	)}
}

// TestExecChildOverTwoProcesses is the pipe between two real processes: the start ExecChild
// builds is one the real child accepts, and its answer is root's.
func TestExecChildOverTwoProcesses(t *testing.T) {
	a, err := osChild(t).Ask(t.Context(), disconnectedRequest(t, "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if !a.OK || a.Notice == nil || a.Notice.Posted || a.Notice.Why != whyNoCopy {
		t.Fatalf("answer %+v", a)
	}
}

// TestExecChildOverTwoProcessesRefusesAFlood is CC6 on a real pipe: a child that writes more
// than an answer may hold is drained, not waited on, and refused.
func TestExecChildOverTwoProcessesRefusesAFlood(t *testing.T) {
	_, err := osChild(t).Ask(t.Context(), disconnectedRequest(t, floodTeam))
	if !errors.Is(err, ErrChild) || !strings.Contains(err.Error(), "its answer is over 1048576 bytes") {
		t.Fatalf("a flooding child = %v", err)
	}
}

// TestRealChildRefusesAStartOffTheSpec is CC2 in the real child: a stray variable or another
// working directory is refused before the request is read (78).
func TestRealChildRefusesAStartOffTheSpec(t *testing.T) {
	c := osChild(t)
	spec, err := c.spec()
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		env  []string
		dir  string
		want string
	}{
		"a stray variable":  {append(spec.Env, "STRAY=1"), spec.Dir, "its environment holds 2 variables"},
		"another directory": {spec.Env, "/tmp", "its working directory is not /"},
	}
	for name, tc := range cases {
		var stdout, stderr bytes.Buffer
		_, runErr := execx.OS{}.Run(t.Context(), execx.Cmd{
			Name: spec.Path, Args: spec.Args, Dir: tc.dir, Env: tc.env, CleanEnv: true,
			Credential: &syscall.Credential{Uid: spec.UID, Gid: spec.GID, Groups: spec.Groups},
			Stdin:      strings.NewReader("{}"), Stdout: &stdout, Stderr: &stderr,
		})
		exit, isExit := errors.AsType[*execx.ExitError](runErr)
		if !isExit || exit.Code != int(ExitConfig) || stdout.Len() != 0 ||
			!strings.HasPrefix(stderr.String(), "teamchat-admin: ctl-child was not started by root's child spec: "+tc.want) {
			t.Errorf("%s: %v, stdout %q, stderr %q", name, runErr, stdout.String(), stderr.String())
		}
	}
}
