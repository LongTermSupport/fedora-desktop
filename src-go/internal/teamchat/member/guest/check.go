package guest

import (
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
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// OpCheck is `guest check`, which changes nothing and so prints nothing.
var OpCheck = register(Op{Verb: "guest check"})

// SystemDirs are the guest session unit's PATH after the home's own ~/.local/bin (GU2).
var SystemDirs = []string{"/usr/local/bin", "/usr/bin", "/bin"}

// UnitPath is the session unit's PATH for home: the only PATH guest check looks a program up
// on, never the attach's own.
func UnitPath(home string, system []string) string {
	return strings.Join(append([]string{filepath.Join(home, ".local", "bin")}, system...), ":")
}

// Kernel is the kernel features a member session needs, a seam so a test can take one away.
type Kernel interface {
	// Pidfd opens and closes a pidfd for the calling process.
	Pidfd() error
	// Inotify makes an inotify instance and watches dir with it.
	Inotify(dir string) error
}

// Checker is `guest check`'s adapters. NewChecker is the real one.
type Checker struct {
	Sys        System
	Run        execx.Runner
	Kernel     Kernel
	SystemDirs []string
}

// NewChecker is the checker of the real guest.
func NewChecker() Checker {
	return Checker{Sys: OS{}, Run: execx.OS{}, Kernel: LinuxKernel{}, SystemDirs: SystemDirs}
}

const (
	// stdinCap bounds the reach facts on stdin; they are a URL and a few IP literals.
	stdinCap = 16 << 10
	// reachTimeout is B's 3 s for the homeserver's versions answer.
	reachTimeout = 3 * time.Second
	// ghTimeout bounds gh's own call to GitHub, so a guest with no route fails rather than
	// hangs the host's apply.
	ghTimeout = 30 * time.Second
	// reachToken stands in for the token the member client needs: versions is asked without
	// one.
	reachToken = "unused"

	keyV              = "v"
	keyTeam           = "team"
	keyBaseURL        = "base_url"
	keyPlainHTTPHosts = "plain_http_hosts"

	msgStdin     exits.Template = "guest check reads one object on stdin: v 1, team, base_url and plain_http_hosts, nothing else"
	msgOtherTeam exits.Template = "guest check's stdin names a team that is not --team {}"
	msgProgram   exits.Template = "{} is not an executable file on the session's PATH (~/.local/bin, /usr/local/bin, /usr/bin, /bin) for guest user {}"
	msgGh        exits.Template = "gh auth status --hostname github.com failed as guest user {}: log gh in, and check the guest reaches GitHub"
	msgKernel    exits.Template = "the guest's kernel gives guest user {} no usable {}; a member session needs pidfd and inotify"
	msgURL       exits.Template = "team {}'s base_url is not a member URL its plain_http_hosts allow"
	msgReach     exits.Template = "the homeserver of team {} did not answer /_matrix/client/versions within 3 s from the guest"
)

// facts are guest check's stdin (n3-D6): how the team's homeserver is reached.
type facts struct {
	baseURL string
	hosts   []string
}

// Check is `guest check --user U --team T` (GU2): the caller is U (GU1); the reach facts on
// stdin are T's; gh, tmux and claude are on the unit's PATH; gh is logged in to github.com;
// pidfd and inotify work; the homeserver answers. The first miss is 78 with the check's code.
func (c Checker) Check(ctx context.Context, u paths.GuestUser, team paths.TeamID, stdin io.Reader) error {
	id, err := Establish(c.Sys, u)
	if err != nil {
		return err
	}
	return errors.Join(c.check(ctx, id, team, stdin), id.Close())
}

func (c Checker) check(ctx context.Context, id *Identity, team paths.TeamID, stdin io.Reader) error {
	f, err := readFacts(stdin, team)
	if err != nil {
		return err
	}
	name := exits.Name(id.User.String())
	unitPath := UnitPath(id.Home.Path(), c.SystemDirs)
	found := map[string]string{}
	for _, p := range []struct{ name, code string }{{"gh", exits.CodeGh}, {"tmux", exits.CodeTmux}, {"claude", exits.CodeClaude}} {
		path, ok := lookPath(unitPath, p.name)
		if !ok {
			return exits.New(p.code, &team, msgProgram, exits.Name(p.name), name)
		}
		found[p.name] = path
	}
	if err := c.ghLoggedIn(ctx, found["gh"], unitPath, id.Home.Path()); err != nil {
		return exits.New(exits.CodeGh, &team, msgGh, name)
	}
	if err := c.Kernel.Pidfd(); err != nil {
		return exits.New(exits.CodeKernel, &team, msgKernel, name, exits.Words("pidfd"))
	}
	if err := c.Kernel.Inotify(id.Home.Path()); err != nil {
		return exits.New(exits.CodeKernel, &team, msgKernel, name, exits.Words("inotify"))
	}
	return reach(ctx, f, team)
}

// lookPath is the first executable regular file called name on unitPath, as exec would find
// it: a link is followed (Claude Code's installer links ~/.local/bin/claude), and a match that
// is not executable by the caller is passed over.
func lookPath(unitPath, name string) (string, bool) {
	for dir := range strings.SplitSeq(unitPath, ":") {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() && unix.Access(path, unix.X_OK) == nil {
			return path, true
		}
	}
	return "", false
}

// ghLoggedIn runs gh auth status as the session would: at the path found, with the unit's
// PATH and the home, its output kept off the op's own streams (it names the account).
func (c Checker) ghLoggedIn(ctx context.Context, gh, unitPath, home string) error {
	ctx, cancel := context.WithTimeout(ctx, ghTimeout)
	defer cancel()
	_, err := c.Run.Run(ctx, execx.Cmd{
		Name: gh,
		Args: []string{"auth", "status", "--hostname", "github.com"},
		Env:  []string{"PATH=" + unitPath, "HOME=" + home},
	})
	if err != nil {
		return fmt.Errorf("gh auth status: %w", err)
	}
	return nil
}

func reach(ctx context.Context, f facts, team paths.TeamID) error {
	client, err := matrix.NewMember(matrix.Config{BaseURL: f.baseURL, PlainHTTPHosts: f.hosts, Token: reachToken})
	if err != nil {
		return exits.New(exits.CodeReach, &team, msgURL, exits.Team(team))
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(ctx, reachTimeout)
	defer cancel()
	if _, err := client.Versions(ctx); err != nil {
		return exits.New(exits.CodeReach, &team, msgReach, exits.Team(team))
	}
	return nil
}

// readFacts decodes stdin strictly: exactly the four keys, v 1, team equal to --team, a string
// base_url and a list of strings; a refusal quotes nothing of it.
func readFacts(stdin io.Reader, team paths.TeamID) (facts, error) {
	refused := exits.New(exits.CodeUsage, &team, msgStdin)
	data, err := io.ReadAll(io.LimitReader(stdin, stdinCap+1))
	if err != nil || len(data) > stdinCap {
		return facts{}, refused
	}
	v, err := proto.Parse(data)
	if err != nil || v.Kind() != proto.KindObject || len(v.Members()) != 4 {
		return facts{}, refused
	}
	ver, okV := member(v, keyV, proto.KindInt)
	t, okTeam := member(v, keyTeam, proto.KindString)
	base, okBase := member(v, keyBaseURL, proto.KindString)
	hosts, okHosts := member(v, keyPlainHTTPHosts, proto.KindArray)
	if !okV || ver.Int() != 1 || !okTeam || !okBase || !okHosts {
		return facts{}, refused
	}
	f := facts{baseURL: base.Str(), hosts: []string{}}
	for _, h := range hosts.Items() {
		if h.Kind() != proto.KindString {
			return facts{}, refused
		}
		f.hosts = append(f.hosts, h.Str())
	}
	if named, err := paths.ParseTeamID(t.Str()); err != nil || named != team {
		return facts{}, exits.New(exits.CodeUsage, &team, msgOtherTeam, exits.Team(team))
	}
	return f, nil
}

func member(v proto.Value, key string, kind proto.Kind) (proto.Value, bool) {
	m, ok := v.Get(key)
	return m, ok && m.Kind() == kind
}

// LinuxKernel is the running kernel.
type LinuxKernel struct{}

// Pidfd opens and closes a pidfd for this process.
func (LinuxKernel) Pidfd() error {
	fd, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		return fmt.Errorf("pidfd_open: %w", err)
	}
	if err := unix.Close(fd); err != nil {
		return fmt.Errorf("close pidfd: %w", err)
	}
	return nil
}

// Inotify makes an inotify instance, watches dir with it, and closes it.
func (LinuxKernel) Inotify(dir string) error {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC)
	if err != nil {
		return fmt.Errorf("inotify_init1: %w", err)
	}
	_, werr := unix.InotifyAddWatch(fd, dir, unix.IN_CREATE|unix.IN_ONLYDIR)
	if werr != nil {
		werr = fmt.Errorf("inotify_add_watch: %w", werr)
	}
	return errors.Join(werr, unix.Close(fd))
}
