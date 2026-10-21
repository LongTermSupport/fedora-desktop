package gitx

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// configNotSet is git config --get's exit status for a key with no value.
const configNotSet = 1

// SSHCommand is the repository's core.sshCommand at dir; set is false when it has none. Any
// refusal other than "not set", or git being unable to start, is an error.
func (g *Git) SSHCommand(ctx context.Context, dir string) (string, bool, error) {
	res, err := g.run(ctx, dir, "config", "--get", "core.sshCommand")
	if err != nil {
		if exitErr, ok := errors.AsType[*execx.ExitError](err); ok && exitErr.Code == configNotSet {
			return "", false, nil
		}
		return "", false, err
	}
	command := strings.TrimSpace(string(res.Stdout))
	return command, command != "", nil
}

// FetchUnattended fetches the remotes of the checkout at dir with nobody to answer: ssh runs
// as sshCommand (the caller puts it in BatchMode), git asks on no terminal and through no
// askpass program, and stdin is /dev/null.
func (g *Git) FetchUnattended(ctx context.Context, dir, sshCommand string) error {
	_, err := g.runner.Run(ctx, execx.Cmd{
		Name: gitProgram,
		Args: []string{"-c", "core.askPass=", "fetch", "--quiet"},
		Dir:  dir,
		Env: []string{
			"GIT_TERMINAL_PROMPT=0",
			"SSH_ASKPASS_REQUIRE=never",
			"GIT_SSH_COMMAND=" + sshCommand,
		},
		Unset: []string{"GIT_ASKPASS", "SSH_ASKPASS"},
	})
	if err != nil {
		return fmt.Errorf("git fetch: %w", err)
	}
	return nil
}
