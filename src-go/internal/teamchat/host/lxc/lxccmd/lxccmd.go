// Package lxccmd is the lxc programs teamchat-admin runs as root, with no sudo (Plan 00209
// SPEC-n3 LX3, TB3, C 3.9): `lxc-ls -1` for the containers there are, `lxc-info` for one
// container's state, init pid, addresses and settings (n3-D22), and `lxc-attach` for a guest
// op run as the member's user with the host-written kit's binary (Guest).
//
// Every line a program prints is parsed by a closed grammar, and anything else fails the call
// naming the program and the container (ErrOutput). A failing lxc-info or lxc-ls is an error
// whose text carries its stderr's last line with controls shown as spaces; that text is never
// parsed, so a missing container is known only by its absence from Containers. lxc-attach's
// output is the guest's, so no byte of it reaches an error (see Guest).
//
// It is the second owner of lxc-ls, lxc-info and lxc-attach beside internal/lxcfreeze, which
// runs them through sudo and links the TUI that teamchat-admin may not.
package lxccmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

var (
	// ErrRefused is an argument refused before any program runs.
	ErrRefused = errors.New("lxccmd: refused")
	// ErrOutput is a program's output that is not its grammar.
	ErrOutput = errors.New("lxccmd: output refused")
)

// LXC is the lxc programs as root (no sudo: teamchat-admin is root), through Runner.
type LXC struct {
	Runner execx.Runner
}

// State is a container's state as lxc-info prints it.
type State string

// The states lxc-info prints; any other is ErrOutput.
const (
	Running  State = "RUNNING"
	Stopped  State = "STOPPED"
	Starting State = "STARTING"
	Stopping State = "STOPPING"
	Aborting State = "ABORTING"
	Freezing State = "FREEZING"
	Frozen   State = "FROZEN"
	Thawed   State = "THAWED"
)

var states = []State{Running, Stopped, Starting, Stopping, Aborting, Freezing, Frozen, Thawed}

// lxcInfo is the program every per-container fact comes from.
const lxcInfo = "lxc-info"

// pidMax is the kernel's ceiling on pid_max (PID_MAX_LIMIT, 2^22 on 64-bit), which no host's
// pid_max exceeds.
const pidMax = 4194304

// maxStderrText bounds the stderr a failing lxc-ls or lxc-info adds to its error.
const maxStderrText = 200

// containerName is a container name's grammar, the same as declare's for a declared container.
var containerName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// configKey is an LXC setting's name.
var configKey = regexp.MustCompile(`^lxc(\.[a-z0-9_]+)+$`)

var pidText = regexp.MustCompile(`^[1-9][0-9]{0,6}$`)

func checkContainer(c string) error {
	if !containerName.MatchString(c) {
		return fmt.Errorf("%w: %q is not a container name", ErrRefused, c)
	}
	return nil
}

// Containers is every container `lxc-ls -1` lists, each by the container name grammar.
func (l LXC) Containers(ctx context.Context) ([]string, error) {
	out, err := l.run(ctx, "", execx.Cmd{Name: "lxc-ls", Args: []string{"-1"}})
	if err != nil {
		return nil, err
	}
	lines, err := valueLines(out)
	if err != nil {
		return nil, outputErr("lxc-ls", "", err.Error())
	}
	seen := map[string]bool{}
	for _, c := range lines {
		if !containerName.MatchString(c) || seen[c] {
			return nil, outputErr("lxc-ls", "", "a line that is not a new container name")
		}
		seen[c] = true
	}
	return lines, nil
}

// State is `lxc-info -n c -sH`.
func (l LXC) State(ctx context.Context, c string) (State, error) {
	v, err := l.oneValue(ctx, c, "-sH")
	if err != nil {
		return "", err
	}
	for _, s := range states {
		if string(s) == v {
			return s, nil
		}
	}
	return "", outputErr(lxcInfo, c, "not a state")
}

// InitPID is `lxc-info -n c -pH`: the container's init, above 1 and within pid_max. A
// stopped container prints no pid, which is ErrOutput; ask State first.
func (l LXC) InitPID(ctx context.Context, c string) (int, error) {
	v, err := l.oneValue(ctx, c, "-pH")
	if err != nil {
		return 0, err
	}
	if !pidText.MatchString(v) {
		return 0, outputErr(lxcInfo, c, "not a pid")
	}
	pid, err := strconv.Atoi(v)
	if err != nil || pid <= 1 || pid > pidMax {
		return 0, outputErr(lxcInfo, c, "not an init pid")
	}
	return pid, nil
}

// Addresses is `lxc-info -n c -iH`: one address a line, each in its canonical form.
func (l LXC) Addresses(ctx context.Context, c string) ([]netip.Addr, error) {
	if err := checkContainer(c); err != nil {
		return nil, err
	}
	out, err := l.run(ctx, c, execx.Cmd{Name: lxcInfo, Args: []string{"-n", c, "-iH"}})
	if err != nil {
		return nil, err
	}
	lines, err := valueLines(out)
	if err != nil {
		return nil, outputErr(lxcInfo, c, err.Error())
	}
	addrs := make([]netip.Addr, 0, len(lines))
	for _, line := range lines {
		a, perr := netip.ParseAddr(line)
		if perr != nil || a.String() != line {
			return nil, outputErr(lxcInfo, c, "not an address")
		}
		addrs = append(addrs, a)
	}
	return addrs, nil
}

// Config is `lxc-info -n c -c K ...` (n3-D22): each key's value as LXC resolves it, includes
// and all. lxc-info prints `K = V` for each key, an unset one as `K = `; a setting with many
// entries (lxc.idmap) prints its further entries one a line after the first, and those join
// the value with newlines. Every key asked for must be printed exactly once.
func (l LXC) Config(ctx context.Context, c string, keys ...string) (map[string]string, error) {
	if err := checkContainer(c); err != nil {
		return nil, err
	}
	if err := checkKeys(keys); err != nil {
		return nil, err
	}
	args := []string{"-n", c}
	for _, k := range keys {
		args = append(args, "-c", k)
	}
	out, err := l.run(ctx, c, execx.Cmd{Name: lxcInfo, Args: args})
	if err != nil {
		return nil, err
	}
	got, err := parseConfig(out, keys)
	if err != nil {
		return nil, outputErr(lxcInfo, c, err.Error())
	}
	return got, nil
}

func checkKeys(keys []string) error {
	if len(keys) == 0 {
		return fmt.Errorf("%w: no setting asked for", ErrRefused)
	}
	seen := map[string]bool{}
	for _, k := range keys {
		if !configKey.MatchString(k) || seen[k] {
			return fmt.Errorf("%w: %q is not a new setting name", ErrRefused, k)
		}
		seen[k] = true
	}
	return nil
}

func parseConfig(out []byte, keys []string) (map[string]string, error) {
	if err := plainText(out); err != nil {
		return nil, err
	}
	got := map[string]string{}
	var current string
	for line := range strings.SplitSeq(strings.TrimSuffix(string(out), "\n"), "\n") {
		if k, v, ok := keyLine(line); ok {
			if !slices.Contains(keys, k) {
				return nil, errors.New("a setting not asked for")
			}
			if _, dup := got[k]; dup {
				return nil, errors.New("a setting printed twice")
			}
			got[k], current = v, k
			continue
		}
		if current == "" {
			return nil, errors.New("a line before any setting")
		}
		if line != "" {
			got[current] = strings.TrimPrefix(got[current]+"\n"+line, "\n")
		}
	}
	for _, k := range keys {
		if _, ok := got[k]; !ok {
			return nil, errors.New("a setting not printed")
		}
	}
	return got, nil
}

// keyLine reads a `K = V` line whose K is a setting's name.
func keyLine(line string) (string, string, bool) {
	k, v, found := strings.Cut(line, " = ")
	if !found {
		k, found = strings.CutSuffix(line, " =")
	}
	if !found || !configKey.MatchString(k) {
		return "", "", false
	}
	return k, v, true
}

func (l LXC) oneValue(ctx context.Context, c, flag string) (string, error) {
	if err := checkContainer(c); err != nil {
		return "", err
	}
	out, err := l.run(ctx, c, execx.Cmd{Name: lxcInfo, Args: []string{"-n", c, flag}})
	if err != nil {
		return "", err
	}
	lines, err := valueLines(out)
	if err != nil {
		return "", outputErr(lxcInfo, c, err.Error())
	}
	if len(lines) != 1 {
		return "", outputErr(lxcInfo, c, "not one value")
	}
	return lines[0], nil
}

// valueLines is out's lines: printable text, no empty line, the last ended by a newline.
func valueLines(out []byte) ([]string, error) {
	if len(out) == 0 {
		return nil, nil
	}
	if err := plainText(out); err != nil {
		return nil, err
	}
	if !bytes.HasSuffix(out, []byte("\n")) {
		return nil, errors.New("no newline at the end")
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if slices.Contains(lines, "") {
		return nil, errors.New("an empty line")
	}
	return lines, nil
}

// plainText refuses any control byte but a newline, and anything that is not ASCII.
func plainText(out []byte) error {
	for _, b := range out {
		if b != '\n' && (b < 0x20 || b > 0x7e) {
			return errors.New("a control or non-ASCII byte")
		}
	}
	return nil
}

func outputErr(prog, c, why string) error {
	if c == "" {
		return fmt.Errorf("%w: %s: %s", ErrOutput, prog, why)
	}
	return fmt.Errorf("%w: %s for container %s: %s", ErrOutput, prog, c, why)
}

// run runs an lxc program for its stdout. A failing one's error carries its stderr's last
// line, shown safely, for the person reading it.
func (l LXC) run(ctx context.Context, c string, cmd execx.Cmd) ([]byte, error) {
	res, err := l.Runner.Run(ctx, cmd)
	if err == nil {
		return res.Stdout, nil
	}
	what := cmd.Name
	if c != "" {
		what = cmd.Name + " for container " + c
	}
	if ee, ok := errors.AsType[*execx.ExitError](err); ok {
		return nil, fmt.Errorf("%s: exit %d: %s", what, ee.Code, stderrText(res.Stderr))
	}
	return nil, fmt.Errorf("%s: %w", what, err)
}

// stderrText is stderr's last line, controls shown as spaces, bounded.
func stderrText(stderr []byte) string {
	s := strings.TrimRight(string(stderr), "\n")
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	if len(s) > maxStderrText {
		s = s[:maxStderrText]
	}
	return s
}
