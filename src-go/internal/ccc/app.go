// Package ccc is the ccc command (Plan 00233): Claude Code in a rootless podman container, for
// someone who does not write code, always in the one folder ~/CCC, with nothing from the host
// but that folder, the agent's own state and its sign-in token. It is ccy without the GitHub
// coupling: ccy's image, token pool and permission mode, no gh, no SSH, no agent bus.
//
// What it does, numbered as its tests are:
//
//   - C1. -h/--help prints the usage on stdout, exit 0. --claude (the default agent),
//     --continue and --token NAME are taken; anything else, a second agent flag, or a token
//     name outside letters, digits, _ and - is a usage error, exit 64, nothing run.
//   - C2. Built without the image version it needs (-X main.RequiredVersion), or started with
//     no terminal: exit 1, nothing run.
//   - C3. ~/CCC, wherever ccc is started: made 0700 when missing; refused when it is a symbolic
//     link or not a folder. A folder that is not a repository is made one (branch main, its own
//     user.name and user.email, an ignore file and the agent's guide file when absent, a first
//     commit); files already there are kept. A repository is left as it is. No remote, ever.
//   - C4. claude-yolo:latest must exist and carry a claude-yolo-version label at least the
//     required version, else exit 1 naming the play that builds it.
//   - C5. The token: the usable file of the name (ccc, or --token NAME) with the latest expiry
//     in ccy's pool, mentioned when it expires within 14 days. None named ccc: a sign-in
//     (setup-token in a throwaway container with nothing mounted), its token saved as
//     ccc.<today+90>.token, 0600. A failed sign-in is offered again [Y/n], at most 3 sign-ins;
//     declined or end of input is exit 130. None for --token NAME: exit 1, no sign-in.
//   - C6. The agent's state, ~/.local/share/ccc/<agent>, made 0700 with onboarding, the
//     permission bypass and /workspace's trust recorded; other keys kept.
//   - C7. The session: podman with only the folder and the state mounted, the token by name,
//     tini as the entrypoint (not ccy's, which needs GitHub), the agent with its permission
//     prompts off, run through the ctrl+z guard.
//   - C8. After the session, a changed folder is committed as "ccc session <start>"; a failed
//     commit is exit 1. Otherwise the exit status is the session's.
package ccc

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tokens"
)

// Play builds ccc and the image it runs.
const Play = "playbooks/imports/play-claude-code-consumer.yml"

const usage = `usage: ccc [--claude] [--continue] [--token NAME]

Claude Code in a safe container, always in the folder ~/CCC.

  --claude        run Claude Code (the default)
  --continue      carry on with the last conversation
  --token NAME    sign in with the saved token NAME instead of ccc's own
  -h, --help      show this help
`

// App is one run. Every field is a seam a test replaces.
type App struct {
	Runner     execx.Runner
	Foreground execx.Foregrounder
	Guard      Guard
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	// IsTerminal reports whether a stream is a terminal; it is asked of Stdin.
	IsTerminal func(any) bool
	Home       string
	Now        func() time.Time
	User       Identity
	// TimeZone is the host's zone name, given to the session; empty when unknown.
	TimeZone string
	// Term is the person's TERM.
	Term string
	// Pid names the session's container, so two windows never share a name.
	Pid int
	// RequiredVersion is the oldest image this build runs, ccy's REQUIRED_CONTAINER_VERSION.
	RequiredVersion string
	// SetupErr is a failure found while these fields were gathered (no home directory, no
	// account); it stops the run before anything else.
	SetupErr error

	answers *bufio.Reader
}

// stopError is a failure already reported, carrying the exit code to end with.
type stopError struct{ code term.ExitCode }

func (e stopError) Error() string { return "stopped with exit " + strconv.Itoa(int(e.code)) }

// Run is one ccc run.
func (a *App) Run(ctx context.Context, args []string) term.ExitCode {
	code, err := a.run(ctx, args)
	if err == nil {
		return code
	}
	if stop, ok := errors.AsType[stopError](err); ok {
		return stop.code
	}
	a.sayf("ccc: %v", err)
	return term.ExitFailure
}

func (a *App) run(ctx context.Context, args []string) (term.ExitCode, error) {
	o, err := parseArgs(args)
	if err != nil {
		a.sayf("ccc: %v\n%s", err, strings.TrimRight(usage, "\n"))
		return term.ExitUsage, nil
	}
	if o.Help {
		if _, werr := io.WriteString(a.Stdout, usage); werr != nil {
			return term.ExitFailure, fmt.Errorf("write the usage: %w", werr)
		}
		return term.ExitOK, nil
	}
	if a.SetupErr != nil {
		return term.ExitFailure, a.SetupErr
	}
	if a.RequiredVersion == "" {
		return term.ExitFailure, fmt.Errorf("this ccc was built without the image version it needs; re-run %s", Play)
	}
	if !a.IsTerminal(a.Stdin) {
		return term.ExitFailure, errors.New("ccc needs a terminal: run it in a terminal window, or open Claude from the app grid")
	}
	a.answers = bufio.NewReader(a.Stdin)
	git := gitx.New(a.Runner)
	folder := filepath.Join(a.Home, FolderName)
	if err = prepareFolder(ctx, git, folder, o.Agent, a.User); err != nil {
		return term.ExitFailure, err
	}
	if err = a.checkImage(ctx); err != nil {
		return term.ExitFailure, err
	}
	token, err := a.token(o)
	if err != nil {
		return term.ExitFailure, err
	}
	state := filepath.Join(a.Home, ".local", "share", "ccc", o.Agent.Name)
	if err = prepareState(state); err != nil {
		return term.ExitFailure, err
	}
	secret, err := execx.NewSecret(o.Agent.LoginEnv, token)
	if err != nil {
		return term.ExitFailure, err
	}
	started := a.Now()
	exit, err := a.Guard.Run(execx.Cmd{
		Name: podmanProgram,
		Args: sessionArgs(session{
			Agent: o.Agent, Folder: folder, State: state, Continue: o.Continue,
			Name:     "ccc-" + o.Agent.Name + "-" + strconv.Itoa(a.Pid),
			TimeZone: a.TimeZone, Term: a.Term,
		}),
		Secrets: []execx.Secret{secret},
	})
	if err != nil {
		return term.ExitFailure, fmt.Errorf("run the session: %w", err)
	}
	if err := snapshot(ctx, git, folder, started); err != nil {
		return term.ExitFailure, err
	}
	return term.ExitCode(exit.Code), nil
}

// snapshot commits whatever the session changed in the folder.
func snapshot(ctx context.Context, git *gitx.Git, folder string, started time.Time) error {
	dirty, err := git.Dirty(ctx, folder)
	if err != nil || !dirty {
		return err
	}
	return git.CommitAll(ctx, folder, "ccc session "+started.Format("2006-01-02 15:04"))
}

// checkImage holds the image to the version this build needs.
func (a *App) checkImage(ctx context.Context) error {
	eng, err := engine.New(engine.Podman, a.Runner)
	if err != nil {
		return err
	}
	have, err := eng.ImageInspect(ctx, image, `{{ index .Config.Labels "`+imageVersionLabel+`" }}`)
	if errors.Is(err, engine.ErrNoImage) {
		return fmt.Errorf("the Claude container image %s is not built yet; re-run %s", image, Play)
	}
	if err != nil {
		return err
	}
	if !versionAtLeast(have, a.RequiredVersion) {
		return fmt.Errorf("the Claude container image is version %q and ccc needs %s or later; re-run %s", have, a.RequiredVersion, Play)
	}
	return nil
}

// token is the sign-in token the session runs with (C5).
func (a *App) token(o options) (string, error) {
	name := o.Token
	if name == "" {
		name = tokenName
	}
	dir := poolDir(a.Home)
	pool, err := tokens.ReadPool(dir)
	if err != nil {
		return "", err
	}
	today := a.Now()
	if file, ok := pickToken(pool.Files, name, today); ok {
		if days := file.DaysLeft(today); days <= renewSoon {
			a.sayf("ccc: the saved sign-in %q expires in %d days; ccc will ask you to sign in again after that.", name, days)
		}
		return readToken(filepath.Join(dir, file.FileName))
	}
	if o.Token != "" {
		return "", fmt.Errorf("no usable token named %q in %s", o.Token, dir)
	}
	return a.signIn(o.Agent, dir)
}

// signIn makes a new token in a throwaway container and saves it in the pool.
func (a *App) signIn(agent Agent, dir string) (string, error) {
	a.sayf("ccc: first, sign in to Claude. A link will appear: open it, sign in, and paste the code it gives you back here.")
	for attempt := 1; ; attempt++ {
		var out bytes.Buffer
		exit, err := a.Foreground.Foreground(execx.Cmd{Name: podmanProgram, Args: signInArgs(agent), Stdout: io.MultiWriter(a.Stderr, &out)})
		if err != nil {
			return "", fmt.Errorf("start the sign-in: %w", err)
		}
		if value, ok := extractToken(out.String()); ok && exit.Code == 0 {
			path, saveErr := saveToken(dir, tokenName, value, a.Now())
			if saveErr != nil {
				return "", saveErr
			}
			a.sayf("ccc: signed in; the sign-in is saved in %s", path)
			return value, nil
		}
		a.sayf("ccc: the sign-in did not finish, so there is no token to save.")
		if attempt == maxSignIns {
			return "", fmt.Errorf("gave up after %d sign-ins; run ccc again when you are ready", maxSignIns)
		}
		again, err := a.askAgain()
		if err != nil {
			return "", err
		}
		if !again {
			a.sayf("ccc: cancelled.")
			return "", stopError{code: term.ExitCancelled}
		}
	}
}

// maxAnswers bounds the re-asks of an answer ccc cannot read.
const maxAnswers = 3

// askAgain asks whether to try the sign-in again: empty, y or yes is yes; n or no is no;
// anything else is asked again, up to maxAnswers times. End of input is a cancel.
func (a *App) askAgain() (bool, error) {
	for range maxAnswers {
		a.sayf("Try signing in again? [Y/n] ")
		line, err := a.answers.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, fmt.Errorf("read the answer: %w", err)
		}
		if errors.Is(err, io.EOF) && line == "" {
			a.sayf("ccc: cancelled.")
			return false, stopError{code: term.ExitCancelled}
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		a.sayf("Please answer y or n.")
	}
	a.sayf("ccc: cancelled.")
	return false, stopError{code: term.ExitCancelled}
}

// say writes one line to stderr; a write that fails there has nowhere left to be reported.
func (a *App) sayf(format string, args ...any) {
	if _, err := fmt.Fprintf(a.Stderr, format+"\n", args...); err != nil {
		return
	}
}
