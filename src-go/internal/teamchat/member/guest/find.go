package guest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// The programs `guest find-claude` answers the path of (R77).
const (
	ProgramClaude = "claude"
	ProgramNode   = "node"
)

// OpFind is `guest find-claude` (Plan 00265 R77, Plan 00266 "Finding the claude command"): the
// user's own interactive login shell asked where claude and node are, each answered as a FOUND
// line. It changes nothing and acts on no seat.
var OpFind = register(Op{Verb: "guest find-claude", Finds: []string{ProgramClaude, ProgramNode}})

const (
	// findWait bounds the login shell, its rc files included.
	findWait = 20 * time.Second
	// maxShellOutput caps what the login shell may print; rc noise past it is a refusal.
	maxShellOutput = 64 << 10
	// maxPasswd bounds the passwd database read.
	maxPasswd = 1 << 20

	findBegin = "teamchat-find-claude-begin"
	findEnd   = "teamchat-find-claude-end"

	// findScript prints the two answers between the markers, so nothing an rc file prints is
	// read; an answer is empty when command -v finds nothing.
	findScript = `printf '\n%s\n' ` + findBegin + `; printf 'claude=%s\n' "$(command -v claude)"; ` +
		`printf 'node=%s\n' "$(command -v node)"; printf '%s\n' ` + findEnd

	msgLoginShell exits.Template = "guest user {} has no login shell in {} that is an absolute path to an executable file"
	msgShellFails exits.Template = "the login shell {} of guest user {} {}, asked where claude is"
	msgNoClaude   exits.Template = "the interactive login shell of guest user {} found no claude (command -v claude); install claude so that shell finds it"
	msgNotAPath   exits.Template = "command -v {} in the login shell of guest user {} answered {}, which is not a clean absolute path to an executable file of that name"
	msgNoAnswer   exits.Template = "the login shell {} of guest user {} printed no answer between its markers"

	causeShellBound exits.Template = "did not end within 20 s"
	causeShellExit  exits.Template = "exited {}"
	causeShellOver  exits.Template = "printed over 64 KiB"
	causeShellRun   exits.Template = "could not be run"
)

// Found is the paths the login shell answered: claude always, node when it finds one (Claude
// Code's native build needs none).
type Found struct {
	Claude string
	Node   string
}

// Shell is the lookup's adapters: the runner, the passwd database the login shell is read from,
// and the system part of the PATH the shell starts from. NewShell is the real one.
type Shell struct {
	Run        execx.Runner
	Passwd     string
	SystemDirs []string
}

// NewShell is the lookup of the real guest.
func NewShell() Shell { return Shell{Run: execx.OS{}, Passwd: "/etc/passwd", SystemDirs: SystemDirs} }

// Find asks id's own login shell, from the passwd database, as `<shell> -l -i -c`, where claude
// and node are: an interactive login reads the rc files that load nvm. It runs with a clean
// environment, no input, in a session of its own, bounded by findWait. Only the lines between
// the markers are read. Each answer must be a clean absolute path, of the program's own name, to
// an executable file. Every refusal is 78 claude, naming what was found.
func (s Shell) Find(ctx context.Context, id *Identity) (Found, error) {
	name := exits.Name(id.User.String())
	shell, ok := s.loginShell(id.User)
	if !ok {
		return Found{}, exits.New(exits.CodeClaude, nil, msgLoginShell, name, exits.Name(s.Passwd))
	}
	out, cause := s.ask(ctx, id, shell)
	if cause != nil {
		return Found{}, exits.New(exits.CodeClaude, nil, msgShellFails, exits.Name(shell), name, *cause)
	}
	claude, node, ok := answers(out)
	switch {
	case !ok:
		return Found{}, exits.New(exits.CodeClaude, nil, msgNoAnswer, exits.Name(shell), name)
	case claude == "":
		return Found{}, exits.New(exits.CodeClaude, nil, msgNoClaude, name)
	}
	for _, a := range []struct{ program, path string }{{ProgramClaude, claude}, {ProgramNode, node}} {
		if a.path != "" && !programAt(a.path, a.program) {
			return Found{}, exits.New(exits.CodeClaude, nil, msgNotAPath, exits.Name(a.program), name, exits.Name(a.path))
		}
	}
	return Found{Claude: claude, Node: node}, nil
}

// loginShell is u's shell field in the passwd database: an absolute, clean path to an
// executable regular file. False when the database cannot be read, has no entry for u, or the
// field is anything else.
func (s Shell) loginShell(u paths.GuestUser) (string, bool) {
	data, err := readBounded(s.Passwd, maxPasswd)
	if err != nil {
		return "", false
	}
	for line := range strings.Lines(string(data)) {
		f := strings.Split(strings.TrimSuffix(line, "\n"), ":")
		if len(f) == 7 && f[0] == u.String() {
			shell := f[6]
			return shell, paths.ValidGuestPath(shell) && executableFile(shell)
		}
	}
	return "", false
}

var errPasswdSize = errors.New("over its bound")

// readBounded is the file at p, refused past limit bytes.
func readBounded(p string, limit int64) (data []byte, err error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", p, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close %s: %w", p, closeErr))
		}
	}()
	data, err = io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s: %w", p, errPasswdSize)
	}
	return data, nil
}

var errShellBound = errors.New("the login shell did not end within its bound")

// ask runs the find script in shell as id, its stdout capped, its stderr dropped (an
// interactive shell with no terminal complains there). A failure is the cause msgShellFails
// gives.
func (s Shell) ask(ctx context.Context, id *Identity, shell string) ([]byte, *exits.Fill) {
	bounded, cancel := context.WithTimeoutCause(ctx, findWait, errShellBound)
	defer cancel()
	home, user := id.Root.Path(), id.User.String()
	stdout := &limited{limit: maxShellOutput}
	_, err := s.Run.Run(bounded, execx.Cmd{
		Name: shell,
		Args: []string{"-l", "-i", "-c", findScript},
		Env: []string{
			"HOME=" + home, "USER=" + user, "LOGNAME=" + user, "SHELL=" + shell,
			"PATH=" + UnitPath(home, s.SystemDirs), "TERM=dumb",
		},
		CleanEnv:   true,
		NewSession: true,
		Stdin:      strings.NewReader(""),
		Stdout:     stdout,
		Stderr:     io.Discard,
	})
	var cause exits.Fill
	switch exit, isExit := errors.AsType[*execx.ExitError](err); {
	case err == nil && !stdout.over:
		return stdout.buf.Bytes(), nil
	case err == nil:
		cause = exits.Format(causeShellOver)
	case bounded.Err() != nil:
		cause = exits.Format(causeShellBound)
	case isExit && exit.Signal == 0:
		cause = exits.Format(causeShellExit, exits.Count(int64(exit.Code)))
	default:
		cause = exits.Format(causeShellRun)
	}
	return nil, &cause
}

// answers is the claude and node answers of the last marked block of out; false when there is
// none, or it is not exactly the two lines between the markers.
func answers(out []byte) (claude, node string, ok bool) {
	lines := strings.Split(string(out), "\n")
	begin := -1
	for i, l := range lines {
		if l == findBegin {
			begin = i
		}
	}
	if begin < 0 || begin+3 >= len(lines) || lines[begin+3] != findEnd {
		return "", "", false
	}
	claude, okClaude := strings.CutPrefix(lines[begin+1], ProgramClaude+"=")
	node, okNode := strings.CutPrefix(lines[begin+2], ProgramNode+"=")
	return claude, node, okClaude && okNode
}

// programAt is p a clean absolute path of the unit grammar, named program, to an executable
// regular file (a link is followed, as exec follows it).
func programAt(p, program string) bool {
	return paths.ValidUnitPath(p) && filepath.Base(p) == program && executableFile(p)
}

func executableFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular() && unix.Access(p, unix.X_OK) == nil
}

// limited keeps the first limit bytes written to it and notes any beyond; it never fails a
// write, so the shell is not cut off mid-output.
type limited struct {
	buf   bytes.Buffer
	limit int
	over  bool
}

func (l *limited) Write(p []byte) (int, error) {
	room := l.limit - l.buf.Len()
	if len(p) > room {
		l.over = true
		l.buf.Write(p[:max(room, 0)])
		return len(p), nil
	}
	l.buf.Write(p)
	return len(p), nil
}
