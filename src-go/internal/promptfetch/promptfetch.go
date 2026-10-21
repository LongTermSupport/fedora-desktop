// Package promptfetch is the git-prompt-fetch command (Plan 00183, #52): the background fetch
// bash-git-prompt starts from the shell prompt, made unable to ask for anything. Upstream runs
// `GIT_TERMINAL_PROMPT=0 git fetch`, which still lets ssh ask for a key's passphrase on the
// terminal when the key is not in the agent; the prompt hook in ~/.bashrc runs this instead.
//
// Expectations, each pinned by a test:
//
//   - F1 it runs `git -c core.askPass= fetch --quiet` in the checkout with stdin from /dev/null.
//   - F2 the fetch has GIT_TERMINAL_PROMPT=0 and SSH_ASKPASS_REQUIRE=never, GIT_ASKPASS and
//     SSH_ASKPASS removed, and ssh in BatchMode: no passphrase, password, host-key question or
//     askpass window.
//   - F3 BatchMode is added to the ssh command git would have used, never in its place:
//     GIT_SSH_COMMAND from the environment, else core.sshCommand (a per-account key), else
//     GIT_SSH as one quoted word, else ssh.
//   - F4 a core.sshCommand lookup that fails other than "not set" is exit 1 and fetches
//     nothing.
//   - F5 a failed fetch is exit 1 with git's words on stderr.
//   - F6 any argument is a usage error (exit 64), and nothing runs.
package promptfetch

import (
	"context"
	"fmt"
	"io"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/shellword"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// batchMode makes ssh fail rather than ask for a passphrase, a password or a host key.
const batchMode = " -o BatchMode=yes"

// App is the command: the checkout it fetches, the environment it reads, the runner it starts
// git through (by gitx), and where its diagnostics go.
type App struct {
	Dir    string
	Getenv func(string) string
	Runner execx.Runner
	Stderr io.Writer
}

// Run fetches a.Dir's remotes without a way to prompt.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	if len(args) > 0 {
		return a.fail(term.ExitUsage, "takes no arguments; usage: git-prompt-fetch")
	}
	git := gitx.New(a.Runner)
	ssh, err := a.sshCommand(ctx, git)
	if err != nil {
		return a.fail(term.ExitFailure, err.Error())
	}
	if err := git.FetchUnattended(ctx, a.Dir, ssh+batchMode); err != nil {
		return a.fail(term.ExitFailure, err.Error())
	}
	return term.ExitOK
}

// sshCommand is the ssh command git would use for a.Dir, as a shell command line.
func (a App) sshCommand(ctx context.Context, git *gitx.Git) (string, error) {
	if c := a.Getenv("GIT_SSH_COMMAND"); c != "" {
		return c, nil
	}
	configured, set, err := git.SSHCommand(ctx, a.Dir)
	if err != nil {
		return "", fmt.Errorf("read core.sshCommand: %w", err)
	}
	if set {
		return configured, nil
	}
	if program := a.Getenv("GIT_SSH"); program != "" {
		return shellword.Quote(program), nil
	}
	return "ssh", nil
}

func (a App) fail(code term.ExitCode, msg string) term.ExitCode {
	if _, err := fmt.Fprintf(a.Stderr, "git-prompt-fetch: %s\n", msg); err != nil {
		return term.ExitFailure
	}
	return code
}
