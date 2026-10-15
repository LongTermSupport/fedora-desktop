package vmtest

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// cmdVerify checks a bridge response's signature under the host-only key: the only place one
// can be checked (§6.6 rule 11, E60).
func (c *cli) cmdVerify(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return usagef("usage: vmtest verify <run-id>")
	}
	l, err := c.loadLab()
	if err != nil {
		return err
	}
	checkout, err := l.needCheckout()
	if err != nil {
		return err
	}
	res, err := c.env.Runner.Run(ctx, execx.Cmd{Name: "systemd-escape", Args: []string{"--path", checkout}, Stderr: c.env.Stderr})
	slug := strings.TrimSpace(string(res.Stdout))
	if err != nil || slug == "" {
		return ctxErr(ctx, failf("systemd-escape could not name %s: %v", checkout, err))
	}
	key := filepath.Join(c.env.Home, ".config", "vmtest-bridge", slug, "response.key")
	if !readable(key) {
		return failf("no bridge signing key at %s; re-run %s", key, playName)
	}
	_, err = c.helper(ctx, helperCall{
		module: "verify_response", stdout: c.env.Stdout,
		args: []string{"--checkout", checkout, "--key", key, "--run-id", args[0]},
	})
	if code := helperExitCode(err); code > 0 {
		return &exitStatusError{code: term.ExitCode(code)}
	} else if err != nil {
		return ctxErr(ctx, err)
	}
	c.logf("verdict of record: %s", filepath.Join(c.env.Home, ".local", "state", "vmtest-bridge", slug, "service.log"))
	return nil
}
