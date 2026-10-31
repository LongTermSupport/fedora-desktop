package netpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const usage = `usage: ccy-network-policy launch --name NAME --image IMAGE -- ENGINE run [OPTION...] IMAGE [COMMAND...]
       ccy-network-policy refresh
       ccy-network-policy where

launch   runs a ccy session's container under its network policy (the bash launcher calls it)
refresh  re-reads every live session's networks and replaces its allow sets (ccy --connect)
where    prints this checkout's policy file on the host
`

// App is one run of the ccy-network-policy command.
type App struct {
	Args   []string
	Getenv func(string) string
	Runner execx.Runner
	Fg     execx.Foregrounder
	// TakeSecret moves a token the run passes by name (-e NAME) out of this process's
	// environment (execx.TakeSecret), so only the container's create or run is given it.
	TakeSecret func(string) (execx.Secret, bool, error)
	LookPath   func(string) (string, error)
	Ifaces     func() ([]Iface, error)
	Resolve    func(ctx context.Context, host string) ([]netip.Addr, error)
	HostsFile  string // the host's hosts file, read for the lxc keyword
	Dir        string // the checkout the launch runs in
	Stdout     io.Writer
	Stderr     io.Writer
}

// launch is a parsed `launch` command line.
type launch struct {
	name, image string
	docker      bool // the engine is docker, where no policy can be held (N7)
	opts, cmd   []string
	tokens      []execx.Secret // the -e NAME values, for the container's create or run alone (N10)
}

// The two engines ccy drives; every policy step is podman's, the only one that can hold it.
const (
	podman = "podman"
	docker = "docker"
)

// state is what a running session's refresh needs, kept beside its context file.
type state struct {
	Inputs  Inputs  `json:"inputs"`
	Context Context `json:"context"`
}

// errUsage is a command line this command does not take.
var errUsage = errors.New("usage")

// Run runs the subcommand the arguments name.
func (a App) Run(ctx context.Context) term.ExitCode {
	if len(a.Args) == 0 {
		return a.usage()
	}
	switch a.Args[0] {
	case "launch":
		l, err := parseLaunch(a.Args[1:])
		if err != nil {
			return a.usage()
		}
		if l.tokens, err = a.takeTokens(l.opts); err != nil {
			return a.fail("ccy: network policy: " + err.Error() + ". The session was not started.")
		}
		return a.launch(ctx, l)
	case "refresh":
		if len(a.Args) == 1 {
			return a.refresh(ctx)
		}
	case "where":
		if len(a.Args) == 1 {
			return a.print(a.Stdout, HostFile(a.Getenv("HOME"), a.Dir)+"\n")
		}
	}
	return a.usage()
}

func parseLaunch(args []string) (launch, error) {
	var l launch
	for len(args) > 0 && args[0] != "--" {
		if len(args) < 2 {
			return launch{}, errUsage
		}
		switch args[0] {
		case "--name":
			l.name = args[1]
		case "--image":
			l.image = args[1]
		default:
			return launch{}, errUsage
		}
		args = args[2:]
	}
	if len(args) < 3 || l.name == "" || l.image == "" || args[2] != "run" {
		return launch{}, errUsage
	}
	switch filepath.Base(args[1]) {
	case podman:
	case docker:
		l.docker = true
	default:
		return launch{}, errUsage
	}
	rest := args[3:]
	i := slices.Index(rest, l.image)
	if i < 0 {
		return launch{}, errUsage
	}
	l.opts, l.cmd = rest[:i], rest[i+1:]
	return l, nil
}

// takeTokens is every variable the run passes by name only (-e NAME, --env NAME), moved out
// of this process's environment (N10).
func (a App) takeTokens(opts []string) ([]execx.Secret, error) {
	var out []execx.Secret
	for i := 0; i+1 < len(opts); i++ {
		if (opts[i] != "-e" && opts[i] != "--env") || strings.Contains(opts[i+1], "=") {
			continue
		}
		s, ok, err := a.TakeSecret(opts[i+1])
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, s)
		}
		i++
	}
	return out, nil
}

func (a App) launch(ctx context.Context, l launch) term.ExitCode {
	allow, hostFile, err := a.allowList()
	if err != nil {
		return a.fail("ccy: network policy: " + err.Error() + ". The session was not started.")
	}
	runtime := a.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		return a.notStarted(errors.New("XDG_RUNTIME_DIR is not set, so there is nowhere private to keep the session's policy"))
	}
	dir := filepath.Join(runtime, "ccy", "network-policy", l.name)
	if dirErr := errors.Join(os.RemoveAll(dir), os.MkdirAll(dir, 0o700)); dirErr != nil {
		return a.notStarted(fmt.Errorf("the policy directory %s: %w", dir, dirErr))
	}
	defer a.cleanup(dir)
	cx := Context{State: Enforced, Entries: allow.Entries, HostFile: hostFile}
	switch {
	case allow.Off:
		cx.State = Off
	case l.docker:
		cx.State, cx.Reason = Unavailable, "the Docker engine runs containers as root, where ccy cannot hold a policy without root"
	default:
		if _, lookErr := a.LookPath("nft"); lookErr != nil {
			cx.State, cx.Reason = Unavailable, "nft is not installed on the host (play-claude-yolo.yml installs it)"
		}
	}
	if cx.State != Enforced {
		return a.unprotected(l, dir, cx)
	}
	in, err := a.gather(ctx, l, allow, &cx)
	if err != nil {
		return a.notStarted(err)
	}
	mount, err := a.writeContext(dir, cx)
	if err != nil {
		return a.notStarted(err)
	}
	args := slices.Concat([]string{"create"}, l.opts, mount, []string{l.image}, withContext(l.cmd, cx.Text()))
	if _, createErr := a.Runner.Run(ctx, execx.Cmd{Name: podman, Args: args, Secrets: l.tokens}); createErr != nil {
		return a.notStarted(fmt.Errorf("podman create: %w", createErr))
	}
	p, err := a.hold(ctx, l, in, dir, cx)
	if err != nil {
		if _, rmErr := a.Runner.Run(ctx, execx.Cmd{Name: podman, Args: []string{"rm", "-f", "-t", "0", l.name}}); rmErr != nil {
			err = errors.Join(err, fmt.Errorf("and removing the container failed: %w", rmErr))
		}
		return a.notStarted(err)
	}
	a.say(fmt.Sprintf("✓ Network policy held in %s's own network namespace: the private ranges and this host refused; %d allowed range(s), %d port rule(s). Owner's list: %s\n",
		l.name, len(p.Allow4)+len(p.Allow6), len(p.Ports), hostFile))
	return a.foreground(execx.Cmd{Name: podman, Args: AttachArgs(l.name)})
}

// AttachArgs is the podman command line (after the program) a held session runs in the
// foreground: what ccy-sessions finds in a session's process tree as its container's client.
func AttachArgs(name string) []string {
	return []string{"start", "--attach", "--interactive", name}
}

// allowList is the owner's list (N3): the host file, then the host shell's setting.
func (a App) allowList() (Allow, string, error) {
	hostFile := HostFile(a.Getenv("HOME"), a.Dir)
	var allow Allow
	text, err := os.ReadFile(filepath.Clean(hostFile))
	switch {
	case err == nil:
		if allow, err = ParseFile(text); err != nil {
			return Allow{}, hostFile, fmt.Errorf("%s: %w", hostFile, err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return Allow{}, hostFile, fmt.Errorf("reading %s: %w", hostFile, err)
	}
	shell, err := ParseList(a.Getenv(AllowVar))
	if err != nil {
		return Allow{}, hostFile, fmt.Errorf("the host shell's %w", err)
	}
	return allow.Merge(shell), hostFile, nil
}

// gather is everything the host knows before the container exists, checked by computing the
// policy once without the nameservers, so a bad entry stops the launch before create.
func (a App) gather(ctx context.Context, l launch, allow Allow, cx *Context) (Inputs, error) {
	ifaces, err := a.Ifaces()
	if err != nil {
		return Inputs{}, err
	}
	h := ReadHost(ifaces)
	in := Inputs{Allow: allow, Resolved: map[string][]netip.Addr{}, Bus: h.Bus, HostAddrs: h.Global}
	if allow.LXC {
		hosts, readErr := os.ReadFile(a.HostsFile)
		if readErr != nil {
			return Inputs{}, fmt.Errorf("lxc is in the allow-list: %w", readErr)
		}
		in.LXC = LXCAddrs(hosts, h.LXCPrefixes)
	}
	for _, n := range allow.Names {
		if _, done := in.Resolved[n.Name]; done {
			continue
		}
		addrs, lookupErr := a.Resolve(ctx, n.Name)
		if lookupErr != nil {
			return Inputs{}, fmt.Errorf("the allowed host name %s did not resolve on the host: %w", n.Name, lookupErr)
		}
		in.Resolved[n.Name] = addrs
	}
	nets, err := a.projectNetworks(ctx, optionNetworks(l.opts))
	if err != nil {
		return Inputs{}, err
	}
	for _, n := range nets {
		in.Subnets = append(in.Subnets, n.Subnets...)
	}
	if _, err := Compute(in); err != nil {
		return Inputs{}, err
	}
	cx.Bus, cx.Networks, cx.LXC = h.Bus, nets, len(in.LXC)
	return in, nil
}

// optionNetworks is every --network (or --net) value of the run options.
func optionNetworks(opts []string) []string {
	var out []string
	for i, o := range opts {
		for _, flag := range []string{"--network", "--net"} {
			if v, ok := strings.CutPrefix(o, flag+"="); ok {
				out = append(out, v)
			} else if o == flag && i+1 < len(opts) {
				out = append(out, opts[i+1])
			}
		}
	}
	return out
}

// projectNetworks is the joined networks whose subnets the policy opens (N2): every named
// network but the shared bridge and podman's own modes. A mode that shares another
// namespace (host, container:, ns:) cannot hold a policy apart and is refused.
func (a App) projectNetworks(ctx context.Context, names []string) ([]Network, error) {
	var out []Network
	for _, value := range names {
		mode, _, hasOpts := strings.Cut(value, ":")
		switch mode {
		case "host", "container", "ns":
			return nil, fmt.Errorf("--network %s shares a network namespace, where this session's policy could not be held apart", value)
		case "none", "podman", "bridge", "pasta", "slirp4netns", "private":
			continue
		}
		if hasOpts {
			value = mode
		}
		res, err := a.Runner.Run(ctx, execx.Cmd{Name: podman, Args: []string{"network", "inspect", value}})
		if err != nil {
			return nil, fmt.Errorf("the network %s: %w", value, err)
		}
		var nets []struct {
			Subnets []struct {
				Subnet netip.Prefix `json:"subnet"`
			} `json:"subnets"`
		}
		if err := json.Unmarshal(res.Stdout, &nets); err != nil || len(nets) != 1 {
			return nil, fmt.Errorf("the network %s: podman network inspect printed something unreadable", value)
		}
		n := Network{Name: value}
		for _, s := range nets[0].Subnets {
			n.Subnets = append(n.Subnets, s.Subnet.Masked())
		}
		out = append(out, n)
	}
	return out, nil
}

func (a App) inspect(ctx context.Context, name string) (container, error) {
	res, err := a.Runner.Run(ctx, execx.Cmd{Name: podman, Args: []string{"inspect", name}})
	if err != nil {
		return container{}, err
	}
	var cs []container
	if err := json.Unmarshal(res.Stdout, &cs); err != nil || len(cs) != 1 {
		return container{}, fmt.Errorf("podman inspect %s printed something unreadable", name)
	}
	return cs[0], nil
}

// hold is N5 and N6 on the created container: init, the capability guard, the nameservers,
// the table applied and read back, and the state a refresh needs.
func (a App) hold(ctx context.Context, l launch, in Inputs, dir string, cx Context) (Policy, error) {
	if _, err := a.Runner.Run(ctx, execx.Cmd{Name: podman, Args: []string{"init", l.name}}); err != nil {
		return Policy{}, fmt.Errorf("podman init: %w", err)
	}
	c, err := a.inspect(ctx, l.name)
	if err != nil {
		return Policy{}, err
	}
	if c.HostConfig.Privileged {
		return Policy{}, errors.New("the container is privileged, so it could lift its own policy")
	}
	for _, capName := range []string{"CAP_NET_ADMIN", "CAP_SYS_ADMIN"} {
		if slices.Contains(c.EffectiveCaps, capName) {
			return Policy{}, fmt.Errorf("the container holds %s, so it could lift its own policy", capName)
		}
	}
	if c.State.Pid <= 0 {
		return Policy{}, errors.New("the initialised container has no pid to enter")
	}
	resolv, err := os.ReadFile(c.ResolvConfPath)
	if err != nil {
		return Policy{}, fmt.Errorf("the container's resolv.conf: %w", err)
	}
	in.Nameservers = Nameservers(resolv)
	p, err := Compute(in)
	if err != nil {
		return Policy{}, err
	}
	if err := a.apply(ctx, c.State.Pid, p.Ruleset(), func(out []byte) error { return Verify(p, out) }); err != nil {
		return Policy{}, err
	}
	return p, a.writeState(dir, state{Inputs: in, Context: cx})
}

// apply runs an nft script in the container's own namespace and checks what it reads back.
func (a App) apply(ctx context.Context, pid int, script string, check func([]byte) error) error {
	enter := []string{"unshare", "nsenter", "-t", strconv.Itoa(pid), "-n", "nft"}
	if _, err := a.Runner.Run(ctx, execx.Cmd{Name: podman, Args: append(slices.Clone(enter), "-f", "-"), Stdin: strings.NewReader(script)}); err != nil {
		return fmt.Errorf("applying the policy: %w", err)
	}
	res, err := a.Runner.Run(ctx, execx.Cmd{Name: podman, Args: append(enter, "-j", "list", "table", "inet", Table)})
	if err != nil {
		return fmt.Errorf("reading the policy back: %w", err)
	}
	if err := check(res.Stdout); err != nil {
		return fmt.Errorf("the policy read back is not the one applied: %w", err)
	}
	return nil
}

// refresh is N9.
func (a App) refresh(ctx context.Context) term.ExitCode {
	runtime := a.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		return a.fail("ccy: network policy: XDG_RUNTIME_DIR is not set, so the live sessions' policies cannot be found")
	}
	base := filepath.Join(runtime, "ccy", "network-policy")
	entries, err := os.ReadDir(base)
	if errors.Is(err, os.ErrNotExist) {
		return term.ExitOK
	}
	if err != nil {
		return a.fail("ccy: network policy: " + err.Error())
	}
	code := term.ExitOK
	for _, e := range entries {
		if err := a.refreshOne(ctx, filepath.Join(base, e.Name()), e.Name()); err != nil {
			a.say(fmt.Sprintf("ccy: network policy: could not refresh %s: %v. Its allow sets are as they were; relaunch it to be sure.\n", e.Name(), err))
			code = term.ExitFailure
		}
	}
	return code
}

// refreshOne is one session's refresh. An unprotected one (N7) is skipped by its state before
// podman is asked anything: a Docker session is not podman's, and would read as gone.
func (a App) refreshOne(ctx context.Context, dir, name string) error {
	st, stErr := readState(dir)
	if stErr == nil && st.Context.State != Enforced {
		return nil
	}
	c, err := a.inspect(ctx, name)
	var exit *execx.ExitError
	if errors.As(err, &exit) && strings.Contains(strings.ToLower(exit.Stderr), "no such") {
		return os.RemoveAll(dir)
	}
	if err != nil {
		return err
	}
	if !c.State.Running {
		return nil
	}
	if stErr != nil {
		return stErr
	}
	names := make([]string, 0, len(c.NetworkSettings.Networks))
	for n := range c.NetworkSettings.Networks {
		names = append(names, n)
	}
	slices.Sort(names)
	nets, err := a.projectNetworks(ctx, names)
	if err != nil {
		return err
	}
	st.Inputs.Subnets = nil
	for _, n := range nets {
		st.Inputs.Subnets = append(st.Inputs.Subnets, n.Subnets...)
	}
	p, err := Compute(st.Inputs)
	if err != nil {
		return err
	}
	if err := a.apply(ctx, c.State.Pid, p.AllowUpdate(), func(out []byte) error { return VerifyAllow(p, out) }); err != nil {
		return err
	}
	st.Context.Networks = nets
	if _, err := a.writeContext(dir, st.Context); err != nil {
		return err
	}
	a.say(fmt.Sprintf("✓ Network policy of %s refreshed: %d allowed range(s)\n", name, len(p.Allow4)+len(p.Allow6)))
	return a.writeState(dir, st)
}

// writeContext writes the context in place (a bind mount holds the inode, so it is never
// replaced) and returns the mount that shows it inside, read-only. The container's root is
// the host user in rootless podman, so 0600 is readable through the mount.
func (a App) writeContext(dir string, cx Context) ([]string, error) {
	path := filepath.Join(dir, "context")
	if err := os.WriteFile(path, []byte(cx.Text()), 0o600); err != nil {
		return nil, fmt.Errorf("the context file: %w", err)
	}
	opts := "ro"
	if relabel := a.Getenv("CCY_MOUNT_RELABEL"); relabel != "" {
		opts += "," + relabel
	}
	return []string{"-v", path + ":" + ContextPath + ":" + opts}, nil
}

func readState(dir string) (state, error) {
	var st state
	data, err := os.ReadFile(filepath.Clean(filepath.Join(dir, "state.json")))
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return st, fmt.Errorf("its state: %w", err)
	}
	return st, nil
}

func (a App) writeState(dir string, st state) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0o600); err != nil {
		return fmt.Errorf("the policy state: %w", err)
	}
	return nil
}

// withContext gives Claude Code the context in its system prompt (N8), joined to one the
// launcher already passes; another agent's command is left as it is (it reads the file).
func withContext(cmd []string, text string) []string {
	if len(cmd) == 0 || cmd[0] != "claude" {
		return cmd
	}
	out := slices.Clone(cmd)
	if i := slices.Index(out, "--append-system-prompt"); i >= 0 && i+1 < len(out) {
		out[i+1] += "\n\n" + text
		return out
	}
	return slices.Insert(out, 1, "--append-system-prompt", text)
}

// unprotected is N7: the run as the launcher gave it, with the context, and a warning.
func (a App) unprotected(l launch, dir string, cx Context) term.ExitCode {
	mount, err := a.writeContext(dir, cx)
	if err != nil {
		return a.notStarted(err)
	}
	if err := a.writeState(dir, state{Context: cx}); err != nil {
		return a.notStarted(err)
	}
	if cx.State == Off {
		a.say("Network policy: off for this checkout (" + AllowVar + "=off): this session reaches the LAN and this host.\n")
	} else {
		rule := strings.Repeat("═", 80)
		a.say(rule + "\nWARNING: this ccy session runs with NO network policy.\n  " + cx.Reason +
			".\n  It can reach the LAN, this host's own services and the other sessions.\n" + rule + "\n")
	}
	args := slices.Concat([]string{"run"}, l.opts, mount, []string{l.image}, withContext(l.cmd, cx.Text()))
	if l.docker {
		return a.foreground(execx.Cmd{Name: docker, Args: args, Secrets: l.tokens})
	}
	return a.foreground(execx.Cmd{Name: podman, Args: args, Secrets: l.tokens})
}

func (a App) foreground(c execx.Cmd) term.ExitCode {
	exit, err := a.Fg.Foreground(c)
	if err != nil {
		return a.fail("ccy: could not start " + c.Name + ": " + err.Error())
	}
	return term.ExitCode(exit.Code)
}

func (a App) cleanup(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		a.say("ccy: network policy: could not remove " + dir + ": " + err.Error() + "\n")
	}
}

// notStarted is a launch the policy stopped, and the way out the owner has (N7).
func (a App) notStarted(err error) term.ExitCode {
	return a.fail("ccy: network policy: " + err.Error() + ". The session was not started.\n" +
		"  To start it with no network policy (it then reaches the LAN and this host), add the line " + AllowVar + "=off to " +
		HostFile(a.Getenv("HOME"), a.Dir) + ", or set " + AllowVar + "=off in the host shell for one launch.")
}

func (a App) usage() term.ExitCode {
	a.say(usage)
	return term.ExitUsage
}

func (a App) fail(msg string) term.ExitCode {
	a.say(msg + "\n")
	return term.ExitFailure
}

func (a App) say(text string) { a.print(a.Stderr, text) }

func (a App) print(w io.Writer, text string) term.ExitCode {
	var p term.Printer
	p.Print(w, text)
	if p.Err() != nil {
		return term.ExitFailure
	}
	return term.ExitOK
}
