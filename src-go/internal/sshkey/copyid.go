package sshkey

import (
	"context"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// acceptedMark is the line of ssh's -v output that says the host accepts an offered key, before
// ssh has to sign with it (E20).
const acceptedMark = "Server accepts key: "

// copyID is E20: ssh-copy-id's own already-installed check would log in with the tool's login
// key too and find every key installed, so the check is made here, with the key to install
// alone, and ssh-copy-id is run with -f.
func (r *run) copyID(ctx context.Context, inv invocation) term.ExitCode {
	args := r.ownOptions(inv.key, true)
	if !inv.force {
		if done, code := r.alreadyInstalled(ctx, inv); done {
			return code
		}
		args = append([]string{"-f"}, args...)
	}
	return r.replace(r.tool.command(append(args, inv.passed...)))
}

// alreadyInstalled is the check of E20: done when the run ends here, with code.
func (r *run) alreadyInstalled(ctx context.Context, inv invocation) (bool, term.ExitCode) {
	if len(inv.dests) != 1 {
		return false, term.ExitOK
	}
	installed, ok := r.installed(ctx, inv.install, inv.dests[0])
	switch {
	case !ok:
		return true, term.ExitFailure
	case installed:
		r.notef("%s is already installed on %s (the host accepts the key); nothing to do.",
			r.show(publicHalf(inv.install)), inv.dests[0].name)
		return true, term.ExitOK
	}
	return false, term.ExitOK
}

// installed tries a login to d offering the private half of install alone; the host accepting
// the key, whether or not ssh could then sign with it, is "installed".
func (r *run) installed(ctx context.Context, install string, d destination) (bool, bool) {
	priv := strings.TrimSuffix(install, ".pub")
	if err := checkKey(priv); err != nil {
		r.failf("%s is not a readable private key (%v), and the check whether %s is already installed logs in "+
			"with it. Pass -f to install it without the check. Nothing was run.",
			r.show(priv), err, r.show(publicHalf(install)))
		return false, false
	}
	args := append([]string{
		"-T", "-o", "BatchMode=yes", "-o", "ControlPath=none", "-o", optIdentitiesOnly,
		"-o", optNoAgent, "-o", optPublicKeyOnly, "-i", priv,
	}, d.args...)
	config, ok := r.askConfig(ctx, d, args[1:])
	if !ok {
		return false, false
	}
	if extra := r.extraKeys(config, priv); len(extra) > 0 {
		r.failf("ssh's configuration also gives %s the key(s) %s, so a login with %s alone cannot be tried to see "+
			"whether it is already installed. Remove those IdentityFile lines for this host, pass -F with a "+
			"configuration that has none, or pass -f to install it without the check. Nothing was run.",
			d.name, strings.Join(extra, ", "), r.show(priv))
		return false, false
	}
	res, err := r.env.Runner.Run(ctx, execx.Cmd{Name: progSSH, Args: append(append([]string{"-v"}, args...), "exit")})
	stderr := string(res.Stderr)
	switch {
	case err == nil, strings.Contains(stderr, acceptedMark):
		return true, true
	case execx.IsNotInstalled(err):
		r.failf("ssh is not installed.")
		return false, false
	case strings.Contains(stderr, "Permission denied"):
		return false, true
	}
	r.failf("could not log in to %s with %s alone to see whether it is already installed: %s "+
		"Pass -f to install it without the check. Nothing was run.", d.name, r.show(priv), notDebug(stderr, err))
	return false, false
}

// notDebug is ssh's stderr without its -v lines, or the error when nothing else is left.
func notDebug(stderr string, err error) string {
	var kept []string
	for line := range strings.Lines(stderr) {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "debug1:") {
			kept = append(kept, line)
		}
	}
	if len(kept) == 0 {
		return err.Error()
	}
	return strings.TrimSuffix(strings.Join(kept, " "), ".") + "."
}

// publicHalf is the file ssh-copy-id installs for -i path.
func publicHalf(path string) string {
	if strings.HasSuffix(path, ".pub") {
		return path
	}
	return path + ".pub"
}
