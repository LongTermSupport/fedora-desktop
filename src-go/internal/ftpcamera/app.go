// Package ftpcamera is ftp-camera, the camera FTP session tool: it runs vsftpd for a Sony
// camera's uploads, sorts them by EXIF date into the photo library layout (in one pass at the
// end, or as each one arrives), ships the sorted tree to an rclone remote, and prunes RAW files
// whose JPG was deleted. The specification is CLAUDE/Plan/00192-.../SPEC-ftp-camera.md; the
// expectation numbers (E1...) and departures from the bash (B1...) the comments cite are its.
package ftpcamera

import (
	"context"
	_ "embed" // usage.txt is embedded verbatim
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/rclonerc"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

//go:embed usage.txt
var usageText string

// The programs and words more than one file names.
const (
	nmcliBin    = "nmcli"
	findmntBin  = "findmnt"
	exiftoolBin = "exiftool"
	firewallCmd = "firewall-cmd"
	exifShort   = "-s3"
	nmShow      = "show"
	extARW      = "arw"
	extJPG      = "jpg"
	extJPEG     = "jpeg"
)

// exitUsage is a command line that cannot be run (E2-E6): the bash's 2, kept.
const exitUsage term.ExitCode = 2

// Env is everything a run touches outside its own memory; every field is a seam a test fills.
type Env struct {
	Runner execx.Runner
	Execer execx.Execer
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	// LookPath finds a program on PATH (exec.LookPath).
	LookPath func(string) (string, error)
	Now      func() time.Time
	// Sleep waits d or until ctx ends, whichever is first; it returns ctx's error then.
	Sleep func(ctx context.Context, d time.Duration) error
	// Self is this program's path, re-run under systemd-inhibit (E9).
	Self string
	// PID is this process's id: the log followers end with it (tail --pid).
	PID int
	// Proc is /proc, where the rclone mount processes are found (E25).
	Proc fs.FS
	// IsTerminal reports whether a stream is a terminal (colour, E10; the prune prompt, E39).
	IsTerminal func(any) bool
	// OpenTTY opens the controlling terminal, where sudo asks for a password.
	OpenTTY    func() (io.ReadCloser, error)
	ConfigPath string
}

// NewEnv is the production environment on the process's own streams.
func NewEnv() Env {
	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	return Env{
		Runner:     execx.OS{},
		Execer:     execx.OS{},
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Getenv:     os.Getenv,
		LookPath:   exec.LookPath,
		Now:        time.Now,
		Sleep:      sleepContext,
		Self:       self,
		PID:        os.Getpid(),
		Proc:       os.DirFS("/proc"),
		IsTerminal: term.FileIsTerminal,
		OpenTTY:    openTTY,
		ConfigPath: DefaultConfigPath,
	}
}

func openTTY() (io.ReadCloser, error) {
	f, err := os.Open("/dev/tty")
	if err != nil {
		return nil, fmt.Errorf("open /dev/tty: %w", err)
	}
	return f, nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("sleep: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

// errFailed is a failure whose message has already been printed: exit 1.
var errFailed = errors.New("failed")

// advice is a failure explained for the person who has to fix it, over several lines, printed
// after "ERROR: " as written.
type advice struct{ text string }

func (a *advice) Error() string { return a.text }

func advise(format string, args ...any) error { return &advice{text: fmt.Sprintf(format, args...)} }

// App is one ftp-camera invocation.
type App struct {
	Env Env
}

// cli is the state of one run.
type cli struct {
	env    Env
	out    stream // stdout, through the session's lock
	err    stream // stderr, through the same lock
	failed error  // the first write to either stream that failed
	t      *term.Term
	root   *sudo.Sudo
	sd     *systemd.Systemd
	rc     *rclonerc.Client
	opts   options
	tun    tunables
	upload string
	link   linkState
}

func newCLI(env Env) *cli {
	c := &cli{env: env}
	var mu sync.Mutex
	outW := &lockedWriter{mu: &mu, w: env.Stdout, failed: &c.failed}
	errW := &lockedWriter{mu: &mu, w: env.Stderr, failed: &c.failed}
	colourOn := func(w io.Writer) bool { return env.IsTerminal(w) && env.Getenv("NO_COLOR") == "" }
	c.out = stream{palette: colours(colourOn(env.Stdout)), w: outW}
	c.err = stream{palette: colours(colourOn(env.Stderr)), w: errW}
	c.t = &term.Term{Err: errW, IsTerminal: env.IsTerminal, Getenv: env.Getenv}
	c.root = &sudo.Sudo{Runner: env.Runner, Term: c.t, Stdin: env.Stdin, Stdout: outW, OpenTTY: env.OpenTTY}
	c.sd = &systemd.Systemd{Runner: env.Runner}
	c.rc = &rclonerc.Client{Runner: env.Runner, Getenv: env.Getenv, Proc: env.Proc}
	return c
}

// Run executes args (without the program name) and returns the exit status.
func (a *App) Run(ctx context.Context, args []string) term.ExitCode {
	c := newCLI(a.Env)
	code := c.run(ctx, args)
	if code == term.ExitOK && c.failed != nil {
		return term.ExitFailure
	}
	return code
}

func (c *cli) run(ctx context.Context, args []string) term.ExitCode {
	opts, help, problems := parse(args)
	if len(problems) > 0 {
		c.err.f("%s\n", strings.Join(problems, "\n"))
		return exitUsage
	}
	if help {
		c.out.f("%s", usageText)
		return term.ExitOK
	}
	tun, problems := readTunables(c.env.Getenv)
	if len(problems) > 0 {
		c.err.f("%s\n", strings.Join(problems, "\n"))
		return exitUsage
	}
	c.tun = tun
	if err := c.findUploadDir(ctx); err != nil {
		return term.ExitFailure
	}
	if len(args) == 0 {
		chosen, code, ok := c.launch(ctx)
		if !ok {
			return code
		}
		args = chosen
		opts, help, problems = parse(args)
		if len(problems) > 0 || help {
			return c.fail("the launcher chose flags that do not parse: %s", strings.Join(args, " "))
		}
	}
	c.opts = opts
	if len(args) == 0 {
		args = []string{"--no-tui"}
	}
	if code, replaced := c.inhibit(args); replaced {
		return code
	}
	return c.dispatch(ctx)
}

// fail prints ERROR: <message> and is exit 1.
func (c *cli) fail(format string, args ...any) term.ExitCode {
	c.err.f("ERROR: %s\n", fmt.Sprintf(format, args...))
	return term.ExitFailure
}

// failed prints ERROR: <message> and returns errFailed, for a step whose caller decides the exit.
func (c *cli) failErr(format string, args ...any) error {
	c.err.f("ERROR: %s\n", fmt.Sprintf(format, args...))
	return errFailed
}

// rootWhy is the reason sudo is asked for, when its credential is not cached (D5).
const rootWhy = "ftp-camera runs vsftpd, the firewall and the filing of uploads as root."

// ready makes sudo's credential available; sudo has explained a refusal.
func (c *cli) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.root.Ready(ctx, rootWhy); err != nil {
		return errFailed
	}
	return nil
}

// asRoot runs argv as root, its output on the terminal (D5). The error names the command.
func (c *cli) asRoot(ctx context.Context, argv ...string) error {
	if err := c.ready(ctx); err != nil {
		return err
	}
	if err := c.root.Run(ctx, argv...); err != nil {
		return fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
	}
	return nil
}

// rootCapture runs argv as root and hands both streams back unprinted.
func (c *cli) rootCapture(ctx context.Context, argv ...string) (execx.Result, error) {
	if err := c.ready(ctx); err != nil {
		return execx.Result{}, err
	}
	return c.root.Capture(ctx, argv...)
}

// rootWrite writes text to path as root through tee, appending when asked; tee's echo of the
// text is discarded.
func (c *cli) rootWrite(ctx context.Context, path, text string, appendTo bool) error {
	if err := c.ready(ctx); err != nil {
		return err
	}
	s := *c.root
	s.Stdin = strings.NewReader(text)
	s.Stdout = io.Discard
	var err error
	if appendTo {
		err = s.Run(ctx, "tee", "-a", path)
	} else {
		err = s.Run(ctx, "tee", path)
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func (c *cli) onPath(name string) bool {
	_, err := c.env.LookPath(name)
	return err == nil
}

// findUploadDir is the camera account's home (E7).
func (c *cli) findUploadDir(ctx context.Context) error {
	res, err := c.env.Runner.Run(ctx, execx.Cmd{Name: "getent", Args: []string{"passwd", "camera"}})
	if err == nil {
		fields := strings.Split(strings.TrimSpace(string(res.Stdout)), ":")
		if len(fields) >= 6 && fields[5] != "" {
			c.upload = fields[5]
			return nil
		}
	}
	c.err.f("ERROR: 'camera' user not found — run play-ftp-camera.yml first\n")
	return errFailed
}

// inhibitFree are the modes that end in seconds and need no suspend lock (E9).
func (o options) inhibitFree() bool { return o.pass || o.sort || o.prune || o.preflight }

// inhibit replaces the process with itself under systemd-inhibit (E9). replaced is true when
// the run ends here: the exec failed (code 1), or a test's exec returned.
func (c *cli) inhibit(args []string) (term.ExitCode, bool) {
	if c.opts.inhibitFree() || c.env.Getenv("_FTP_CAMERA_INHIBITED") != "" || !c.onPath("systemd-inhibit") {
		return term.ExitOK, false
	}
	err := c.env.Execer.Exec(execx.Cmd{
		Name: "systemd-inhibit",
		Args: append([]string{
			"--what=idle:sleep", "--who=ftp-camera", "--why=Camera FTP transfer in progress",
			"--mode=block", "--", c.env.Self,
		}, args...),
		Env: []string{"_FTP_CAMERA_INHIBITED=1"},
	})
	if err != nil {
		return c.fail("could not run systemd-inhibit: %v", err), true
	}
	return term.ExitOK, true
}

func (c *cli) dispatch(ctx context.Context) term.ExitCode {
	var err error
	switch o := c.opts; {
	case o.pass:
		return c.pass()
	case o.sort:
		err = c.sortPass(ctx)
	case o.push:
		err = c.push(ctx)
	case o.preflight:
		err = c.preflightMode(ctx)
	case o.copy:
		err = c.copyToMount(ctx)
	case o.prune:
		err = c.prune(ctx)
	default:
		return c.serve(ctx)
	}
	switch {
	case err == nil:
		return term.ExitOK
	case ctx.Err() != nil:
		return term.ExitCancelled
	default:
		return term.ExitFailure
	}
}

// loadConfig reads the config; a missing file is the named error (E11, E12).
func (c *cli) loadConfig() (map[string]string, error) {
	cfg, err := readConfig(c.env.ConfigPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, c.failErr("%s not found — run play-ftp-camera.yml first", c.env.ConfigPath)
	}
	if err != nil {
		return nil, c.failErr("%v", err)
	}
	return cfg, nil
}

// pass replaces the process with the vault helper, which prints the camera password (E12).
func (c *cli) pass() term.ExitCode {
	cfg, err := c.loadConfig()
	if err != nil {
		return term.ExitFailure
	}
	repo := cfg["REPO_PATH"]
	if repo == "" {
		return c.fail("REPO_PATH not set in %s", c.env.ConfigPath)
	}
	vault := repo + "/vault.bash"
	if info, err := os.Stat(vault); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		c.err.f("ERROR: %s not found or not executable\n", vault)
		c.err.f("  Repo may have moved — re-run play-ftp-camera.yml from current location\n")
		return term.ExitFailure
	}
	return c.execVault(vault)
}

// execVault runs the checkout's vault helper; its path comes from the config.
func (c *cli) execVault(vault string) term.ExitCode {
	if err := c.env.Execer.Exec(execx.Cmd{Name: vault, Args: []string{"get", "ftp_camera_password"}}); err != nil {
		return c.fail("could not run %s: %v", vault, err)
	}
	return term.ExitOK
}
