// Package nvidiastatus is the nvidia-status command (Plan 00192 SPEC-nvidia-status.md, E1-E25):
// whether the NVIDIA driver play-nvidia.yml installs is built, loaded and working, with Secure
// Boot and the MOK key, the packages, Vulkan, OpenGL, VA-API and the kernel's NVIDIA errors of
// this boot. It only reads: every program it asks runs as the user, through execx, once, under
// a fixed bound. The facts are gathered first, then rendered, and the exit status is the
// verdict over them.
package nvidiastatus

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

//go:embed usage.txt
var usageText string

const (
	tool      = "nvidia-status"
	akmodsKey = "/etc/pki/akmods/certs/public_key.der"
	// commandBound is how long any one program may take (E6): nvidia-smi hangs on a wedged GPU.
	commandBound = 30 * time.Second
)

// Env is everything a run touches outside its own memory.
type Env struct {
	Runner           execx.Runner
	Getenv           func(string) string
	LookPath         func(string) (string, error)
	Stat             func(string) (fs.FileInfo, error)
	Proc             fs.FS
	Stdout           io.Writer
	Stderr           io.Writer
	StdoutIsTerminal bool
}

// NewEnv is the production environment.
func NewEnv() Env {
	return Env{
		Runner: execx.OS{}, Getenv: os.Getenv, LookPath: exec.LookPath, Stat: os.Stat,
		Proc: os.DirFS("/proc"), Stdout: os.Stdout, Stderr: os.Stderr,
		StdoutIsTerminal: term.FileIsTerminal(os.Stdout),
	}
}

// App is one nvidia-status invocation.
type App struct {
	Env Env
}

// Run is the whole command; it returns the exit code.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	env := a.Env
	var diag term.Printer
	opts, err := parseArgs(args)
	if err != nil {
		diag.Print(env.Stderr, fmt.Sprintf("%s: %s — see '%s --help'.\n", tool, err, tool))
		return term.ExitUsage
	}
	if opts.help {
		var p term.Printer
		p.Print(env.Stdout, usageText)
		return finish(env, &p, term.ExitOK)
	}
	f := gather(ctx, env)
	code := term.ExitFailure
	v := judge(f)
	if v == complete {
		code = term.ExitOK
	}
	if opts.check {
		return code
	}
	r := &report{w: env.Stdout, colour: env.StdoutIsTerminal && env.Getenv("NO_COLOR") == ""}
	r.render(f, v)
	return finish(env, &r.p, code)
}

// finish turns a report that could not be written into a failure, said on stderr (E3).
func finish(env Env, p *term.Printer, code term.ExitCode) term.ExitCode {
	if err := p.Err(); err != nil {
		var diag term.Printer
		diag.Print(env.Stderr, fmt.Sprintf("%s: cannot write the report: %v\n", tool, err))
		return term.ExitFailure
	}
	return code
}

type options struct{ check, help bool }

// parseArgs is E1 and E2: --check, -h/--help, nothing else.
func parseArgs(args []string) (options, error) {
	var o options
	for _, arg := range args {
		switch {
		case arg == "-h" || arg == "--help":
			o.help = true
		case arg == "--check" && !o.check:
			o.check = true
		case arg == "--check":
			return options{}, errors.New("--check given twice")
		case strings.HasPrefix(arg, "-"):
			return options{}, fmt.Errorf("unknown option %s", arg)
		default:
			return options{}, fmt.Errorf("unexpected argument %q", arg)
		}
	}
	return o, nil
}

// outcome is what one program said: its streams and exit code, or why it never reached an exit.
type outcome struct {
	name, out, errOut string
	code              int
	problem           string // set when it did not run, did not answer in time, or was killed
}

func (o outcome) ok() bool { return o.problem == "" && o.code == 0 }

func (o outcome) both() string { return o.out + o.errOut }

// why is the failure in one line: the problem, or the exit code and the first line it said.
func (o outcome) why() string {
	if o.problem != "" {
		return o.problem
	}
	msg := fmt.Sprintf("%s exited %d", o.name, o.code)
	if first := firstLine(o.both()); first != "" {
		msg += ": " + first
	}
	return msg
}

// probe runs one reading command under commandBound (E6).
func probe(ctx context.Context, runner execx.Runner, c execx.Cmd) outcome {
	bounded, cancel := context.WithTimeout(ctx, commandBound)
	defer cancel()
	res, err := runner.Run(bounded, c)
	o := outcome{name: c.Name, out: string(res.Stdout), errOut: string(res.Stderr), code: res.ExitCode}
	if err == nil {
		return o
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(bounded.Err(), context.DeadlineExceeded) {
		o.problem = fmt.Sprintf("%s did not answer within %s", c.Name, commandBound)
		return o
	}
	if exitErr, ok := errors.AsType[*execx.ExitError](err); ok {
		o.code = exitErr.Code
		if exitErr.Signal != 0 {
			o.problem = exitErr.Error()
		}
		return o
	}
	o.problem = fmt.Sprintf("%s did not run: %v", c.Name, err)
	return o
}

func lines(s string) []string {
	var out []string
	for l := range strings.SplitSeq(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func firstLine(s string) string {
	if ls := lines(s); len(ls) > 0 {
		return ls[0]
	}
	return ""
}

func lastLine(s string) string {
	if ls := lines(s); len(ls) > 0 {
		return ls[len(ls)-1]
	}
	return ""
}

func oneLine(s string) string { return strings.Join(lines(s), " ") }

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
