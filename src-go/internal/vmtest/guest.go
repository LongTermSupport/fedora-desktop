package vmtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const (
	cloudUser     = "fedora"
	guestHost     = cloudUser + "@127.0.0.1"
	libvirtURI    = "qemu:///session"
	stateShutOff  = "shut off"
	stateCrashed  = "crashed"
	sshWait       = 600 * time.Second
	shutdownWait  = 300 * time.Second
	poll          = 5 * time.Second
	followerLimit = "86400"
	sigtermStatus = 143
)

// guestOpts are the ssh options every guest connection carries (E22). IdentitiesOnly: without
// it ssh offers every key in the user's agent first and sshd's MaxAuthTries locks the session
// out ("Too many authentication failures", measured on the first build).
var guestOpts = []string{
	"-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes", "-o", "StrictHostKeyChecking=no",
	"-o", "UserKnownHostsFile=/dev/null", "-o", "LogLevel=ERROR",
}

// guest is one lab domain reached over the passt forward on port.
type guest struct {
	c      *cli
	key    string
	domain string
	port   int
	// dir holds the domain's console.log, which every boot failure points at.
	dir string
}

func (g *guest) sshArgs(extra []string, remote string) []string {
	args := append(append(append([]string{}, guestOpts...), extra...), "-i", g.key, "-p", strconv.Itoa(g.port), guestHost)
	return append(args, remote)
}

func (g *guest) sshCmd(remote string) execx.Cmd {
	return execx.Cmd{Name: sshBin, Args: g.sshArgs([]string{"-o", "ConnectTimeout=5"}, remote)}
}

// output runs remote in the guest and returns its stdout; its stderr is the CLI's.
func (g *guest) output(ctx context.Context, remote string) (string, error) {
	cmd := g.sshCmd(remote)
	cmd.Stderr = g.c.env.Stderr
	res, err := g.c.env.Runner.Run(ctx, cmd)
	if err != nil {
		return string(res.Stdout), ctxErr(ctx, fmt.Errorf("in the guest, %s: %w", remote, err))
	}
	return string(res.Stdout), nil
}

// do runs remote in the guest for its effect: its output, if any, goes to stderr.
func (g *guest) do(ctx context.Context, remote string) error {
	return g.doWith(ctx, remote, nil)
}

// doWith is do with stdin.
func (g *guest) doWith(ctx context.Context, remote string, stdin io.Reader) error {
	cmd := g.sshCmd(remote)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, g.c.env.Stderr, g.c.env.Stderr
	if _, err := g.c.env.Runner.Run(ctx, cmd); err != nil {
		return ctxErr(ctx, fmt.Errorf("in the guest, %s: %w", remote, err))
	}
	return nil
}

// answers reports whether `ssh true` succeeds; its noise is dropped, as a failed try is expected.
func (g *guest) answers(ctx context.Context) bool {
	cmd := g.sshCmd("true")
	cmd.Stderr = io.Discard
	_, err := g.c.env.Runner.Run(ctx, cmd)
	return err == nil
}

// stream runs remote with keepalives, bounded by limit, its output merged into w. It returns
// the remote status, 124 when the bound ended it (as timeout(1) does), and an error only when
// ssh could not be run at all or the CLI itself was cancelled.
func (g *guest) stream(ctx context.Context, limit time.Duration, remote string, w io.Writer) (int, error) {
	bounded, cancel := g.c.bound(ctx, limit)
	defer cancel()
	cmd := execx.Cmd{Name: sshBin, Args: g.sshArgs([]string{"-o", "ServerAliveInterval=30"}, remote), Stdout: w, Stderr: w}
	_, err := g.c.env.Runner.Run(bounded, cmd)
	return statusOf(ctx, bounded, err)
}

// statusOf is a bounded command's exit status: 124 for its bound, the program's own for a
// refusal, and an error for the CLI's own cancellation or a program that could not start.
func statusOf(parent, bounded context.Context, err error) (int, error) {
	switch {
	case err == nil:
		return 0, nil
	case parent.Err() != nil:
		return 0, ctxErr(parent, err)
	case errors.Is(bounded.Err(), context.DeadlineExceeded):
		return timedOut, nil
	}
	if exit, ok := errors.AsType[*execx.ExitError](err); ok {
		return exit.Code, nil
	}
	return 0, err
}

const timedOut = 124

// scp copies a local file to path in the guest (E22): the content never reaches a command line.
func (g *guest) scp(ctx context.Context, local, remotePath string) error {
	args := append(append([]string{}, guestOpts...), "-i", g.key, "-P", strconv.Itoa(g.port), local, guestHost+":"+remotePath)
	_, err := g.c.env.Runner.Run(ctx, execx.Cmd{Name: "scp", Args: args, Stdout: g.c.env.Stderr, Stderr: g.c.env.Stderr})
	if err != nil {
		return ctxErr(ctx, failf("could not copy %s into the guest: %v", filepath.Base(local), err))
	}
	return nil
}

// state is libvirt's word for the domain.
func (c *cli) domstate(ctx context.Context, domain string) (string, error) {
	res, err := c.env.Runner.Run(ctx, execx.Cmd{Name: virshBin, Args: []string{"-c", libvirtURI, "domstate", domain}})
	if err != nil {
		return "", ctxErr(ctx, err)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// mustBeUp fails at once for a guest that has stopped, so a crash is named as one (E23).
func (g *guest) mustBeUp(ctx context.Context, what string) error {
	state, err := g.c.domstate(ctx, g.domain)
	if err != nil {
		return ctxErr(ctx, failf("cannot read the state of guest %s while waiting for %s", g.domain, what))
	}
	if state == stateShutOff || state == stateCrashed {
		return failf("guest %s is %s while waiting for %s; see %s", g.domain, state, what, filepath.Join(g.dir, "console.log"))
	}
	return nil
}

// waitSSH waits for the guest to answer SSH, by the clock (E24, B12).
func (g *guest) waitSSH(ctx context.Context) error {
	g.c.logf("waiting for SSH on 127.0.0.1:%d (up to %ds)", g.port, int(sshWait.Seconds()))
	start := g.c.env.Now()
	for {
		if err := g.mustBeUp(ctx, "SSH"); err != nil {
			return err
		}
		if g.answers(ctx) {
			break
		}
		if err := g.c.env.Sleep(ctx, poll); err != nil {
			return ctxErr(ctx, err)
		}
		if g.c.env.Now().Sub(start) >= sshWait {
			return failf("guest did not answer SSH within %ds; see %s", int(sshWait.Seconds()), filepath.Join(g.dir, "console.log"))
		}
	}
	g.c.logf("guest answered SSH after ~%ds", int(g.c.env.Now().Sub(start).Seconds()))
	return nil
}

// waitShutoff waits for the domain to power off (E25).
func (g *guest) waitShutoff(ctx context.Context) error {
	start := g.c.env.Now()
	for {
		state, err := g.c.domstate(ctx, g.domain)
		if err != nil {
			return ctxErr(ctx, failf("cannot read the state of guest %s while waiting for it to shut off", g.domain))
		}
		if state == stateShutOff {
			return nil
		}
		if err := g.c.env.Sleep(ctx, poll); err != nil {
			return ctxErr(ctx, err)
		}
		if g.c.env.Now().Sub(start) >= shutdownWait {
			return failf("guest did not shut off within %ds (state: %s)", int(shutdownWait.Seconds()), state)
		}
	}
}

// follower logs a desktop guest's serial socket into console.log until it is stopped (E26).
type follower struct {
	c      *cli
	cancel context.CancelFunc
	done   chan error
	out    *os.File
}

// follow starts the follower on dir's serial socket; its own chatter goes to console-follow.out.
func (c *cli) follow(ctx context.Context, dir string) *follower {
	fctx, cancel := context.WithCancel(ctx)
	f := &follower{c: c, cancel: cancel, done: make(chan error, 1)}
	out, err := os.Create(filepath.Join(dir, "console-follow.out"))
	if err != nil {
		cancel()
		f.done <- fmt.Errorf("cannot create %s: %w", filepath.Join(dir, "console-follow.out"), err)
		return f
	}
	f.out = out
	go func() {
		_, err := c.helper(fctx, helperCall{module: "serial_console", stdout: out, stderr: out, args: []string{
			"--socket", filepath.Join(dir, "serial.sock"), flagLog, filepath.Join(dir, "console.log"),
			"--until", "VMTEST-NEVER-MATCHES", "--follow", "--timeout", followerLimit, "--connect-wait", "120",
		}})
		f.done <- err
	}()
	return f
}

// stop ends the follower and waits for it; nil is a follower never started.
func (f *follower) stop() {
	if f == nil || f.done == nil {
		return
	}
	f.cancel()
	err := <-f.done
	f.done = nil
	if f.out != nil {
		if cerr := f.out.Close(); cerr != nil {
			f.c.logf("console follower output: %v", cerr)
		}
	}
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	if code := helperExitCode(err); code == sigtermStatus {
		return
	} else if code > 0 {
		f.c.logf("console follower exited %d", code)
		return
	}
	f.c.logf("console follower: %v", err)
}

// unlock answers a desktop guest's LUKS prompt on this boot and waits for a login prompt; a
// stall is named with the console's reason (E26). On success the follower is running.
func (g *guest) unlock(ctx context.Context, dir, passphraseFile string) (*follower, error) {
	res, err := g.c.helper(ctx, helperCall{module: "serial_console", args: []string{
		"--socket", filepath.Join(dir, "serial.sock"), flagLog, filepath.Join(dir, "console.log"),
		"--unlock-with", passphraseFile, "--until", "login: ", "--timeout", strconv.Itoa(int(sshWait.Seconds())),
		"--connect-wait", "60", "--domain", g.domain, "--libvirt-uri", libvirtURI,
	}})
	reason := ""
	for line := range strings.SplitSeq(string(res.Stdout), "\n") {
		if strings.HasPrefix(line, "VMTEST-CONSOLE ") {
			g.c.p.Print(g.c.env.Stderr, line+"\n")
		}
		if i := strings.Index(line, "reason="); i >= 0 && reason == "" {
			reason = line[i:]
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctxErr(ctx, err)
		}
		return nil, failf("the guest did not reach userspace (stage: boot): %s", reason)
	}
	return g.c.follow(ctx, dir), nil
}
