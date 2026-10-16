// Package containername is `ccy-teams internal claim-container-name <project> <suffix>
// <owner-pid>`: the launcher's container name, chosen and claimed in one step.
//
// The launcher picks the lowest free name (<project>_<suffix>, then _1, _2 ...) long before
// the engine creates the container, so two launches at the same moment once both took the same
// name and the second failed at create ("already in use"). A choice is now a claim: a file
// named after the container in $XDG_RUNTIME_DIR/ccy/container-names/ holding the launcher's pid
// and start time, written under an flock. A name is taken while a container of that name
// exists or while the process that claimed it still runs; a claim whose process has gone is
// removed. The lock is held only while the claims are read and one is written, never while the
// session runs. The claimant's own earlier claim is given up first, so the restart path, which
// execs the launcher again in the same process, can choose again.
package containername

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessprobe"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	sub      = "claim-container-name"
	dirMode  = 0o700
	fileMode = 0o600
	// defaultEngine is common.bash's: CONTAINER_ENGINE when the launcher has not set one.
	defaultEngine = "podman"
)

var (
	// projectName is what get_project_name can make that is also a file name: no slash, no
	// leading dot.
	projectName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,199}$`)
	suffixName  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
)

// Claim is the claim-container-name command. ProcRoot is where processes are looked up.
type Claim struct {
	Host     *host.Host
	ProcRoot string
}

// New is the command over a host and the real /proc.
func New(h *host.Host) *Claim { return &Claim{Host: h, ProcRoot: "/proc"} }

// owner is the process a claim belongs to: its pid and its start time, so a pid used again by
// another process does not hold the claim.
type owner struct {
	pid   int
	start int64
}

func (o owner) line() string { return fmt.Sprintf("%d %d\n", o.pid, o.start) }

// Run is `ccy-teams internal claim-container-name <project> <suffix> <owner-pid>`.
func (c *Claim) Run(ctx context.Context, args []string) term.ExitCode {
	base, pid, err := parse(args)
	if err != nil {
		c.say("ccy-teams: internal " + sub + ": " + err.Error() + ". See ccy-teams --help.")
		return term.ExitUsage
	}
	runtimeDir := c.Host.Getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(runtimeDir) {
		c.say("ccy-teams: internal " + sub + ": XDG_RUNTIME_DIR is not an absolute path (" + strconv.Quote(runtimeDir) +
			"), so there is nowhere to claim the container name. Launch from a desktop login session.")
		return term.ExitFailure
	}
	name, err := c.claim(ctx, runtimeDir, base, pid)
	if err != nil {
		c.say("ccy-teams: internal " + sub + ": " + err.Error())
		return term.ExitFailure
	}
	c.Host.Print(c.Host.Payload, name+"\n")
	return term.ExitOK
}

func parse(args []string) (string, int, error) {
	if len(args) != 3 {
		return "", 0, errors.New("takes <project> <suffix> <owner-pid>")
	}
	if !projectName.MatchString(args[0]) {
		return "", 0, fmt.Errorf("%q is not a project name", args[0])
	}
	if !suffixName.MatchString(args[1]) {
		return "", 0, fmt.Errorf("%q is not a container name suffix", args[1])
	}
	pid, err := strconv.Atoi(args[2])
	if err != nil || pid < 1 {
		return "", 0, fmt.Errorf("%q is not a process id", args[2])
	}
	return args[0] + "_" + args[1], pid, nil
}

func (c *Claim) claim(ctx context.Context, runtimeDir, base string, pid int) (string, error) {
	me, alive, err := c.process(pid)
	if err != nil {
		return "", err
	}
	if !alive {
		return "", fmt.Errorf("process %d, which the name would be claimed for, is not running", pid)
	}
	// The containers are listed before the lock is taken: a container made after the list was
	// read was claimed first, by a launcher that is still running.
	existing, err := c.containers(ctx)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(runtimeDir, "ccy", "container-names")
	if dirErr := privateDir(dir); dirErr != nil {
		return "", dirErr
	}
	unlock, err := lock(dir + ".lock")
	if err != nil {
		return "", err
	}
	name, claimErr := c.claimLocked(dir, base, me, existing)
	return name, errors.Join(claimErr, unlock())
}

func (c *Claim) claimLocked(dir, base string, me owner, taken map[string]bool) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("could not read the container name claims in %s: %w", dir, err)
	}
	for _, e := range entries {
		held, err := c.held(filepath.Join(dir, e.Name()), me)
		if err != nil {
			return "", err
		}
		if held {
			taken[e.Name()] = true
		}
	}
	name := base
	for n := 1; taken[name]; n++ {
		name = base + "_" + strconv.Itoa(n)
	}
	if err := fsx.WriteAtomic(filepath.Join(dir, name), "container name claim", []byte(me.line()), fileMode); err != nil {
		return "", err
	}
	return name, nil
}

// held reports whether the claim at path belongs to another running process. The claimant's
// own claim and one whose process has gone are removed.
func (c *Claim) held(path string, me owner) (bool, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return false, fmt.Errorf("could not read the container name claim %s: %w", path, err)
	}
	var o owner
	if _, err := fmt.Sscanf(string(data), "%d %d\n", &o.pid, &o.start); err != nil || o.line() != string(data) {
		return false, fmt.Errorf("the container name claim %s is not '<pid> <start>' (%q); remove it and launch again", path, string(data))
	}
	if o != me {
		now, alive, err := c.process(o.pid)
		if err != nil {
			return false, err
		}
		if alive && now == o {
			return true, nil
		}
	}
	if err := fsx.RemoveIfPresent(path); err != nil {
		return false, fmt.Errorf("could not remove the container name claim %s: %w", path, err)
	}
	return false, nil
}

// process is pid's start time from /proc/<pid>/stat (field 22, counted after the command
// name's closing bracket), or not alive.
func (c *Claim) process(pid int) (owner, bool, error) {
	path := filepath.Join(c.ProcRoot, strconv.Itoa(pid), "stat")
	raw, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return owner{}, false, nil
	}
	if err != nil {
		return owner{}, false, fmt.Errorf("could not read process %d: %w", pid, err)
	}
	line := string(raw)
	const starttime = 19
	cut := strings.LastIndex(line, ") ")
	if cut < 0 {
		return owner{}, false, fmt.Errorf("process %d: unreadable %s", pid, path)
	}
	fields := strings.Fields(line[cut+2:])
	if len(fields) <= starttime {
		return owner{}, false, fmt.Errorf("process %d: unreadable %s", pid, path)
	}
	start, err := strconv.ParseInt(fields[starttime], 10, 64)
	if err != nil {
		return owner{}, false, fmt.Errorf("process %d: unreadable start time in %s", pid, path)
	}
	return owner{pid: pid, start: start}, true, nil
}

// containers is every container name the launcher's engine has, running or not.
func (c *Claim) containers(ctx context.Context) (map[string]bool, error) {
	engine := c.Host.Getenv("CONTAINER_ENGINE")
	if engine == "" {
		engine = defaultEngine
	}
	names, err := sessprobe.AllContainerNames(ctx, c.Host.Runner, engine)
	if err != nil {
		return nil, fmt.Errorf("could not list the containers: %w", err)
	}
	return names, nil
}

// privateDir makes dir and its parent the owner's alone, refusing a symlink or file in the way.
func privateDir(dir string) error {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("could not make %s: %w", dir, err)
	}
	for _, d := range []string{filepath.Dir(dir), dir} {
		info, err := os.Lstat(d)
		if err != nil {
			return fmt.Errorf("could not read %s: %w", d, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory (a symlink or a file is in the way); remove it and launch again", d)
		}
		if err := os.Chmod(d, dirMode); err != nil {
			return fmt.Errorf("could not make %s private: %w", d, err)
		}
	}
	return nil
}

// lock takes the flock on path, waiting for another claim to finish (it holds the lock only to
// read the claims and write one), and returns its release.
func lock(path string) (func() error, error) {
	f, err := os.OpenFile(filepath.Clean(path), os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, fileMode)
	if err != nil {
		return nil, fmt.Errorf("could not open the lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return nil, errors.Join(fmt.Errorf("could not lock %s: %w", path, err), f.Close())
	}
	return func() error {
		unlockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		if unlockErr != nil {
			unlockErr = fmt.Errorf("could not unlock %s: %w", path, unlockErr)
		}
		return errors.Join(unlockErr, f.Close())
	}, nil
}

func (c *Claim) say(msg string) { c.Host.Print(c.Host.Stderr, msg+"\n") }
