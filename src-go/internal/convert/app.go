// Package convert is the convert command: one file turned into the other formats its type
// supports, by a proven converter (an engine) the conversion table names. It owns the table and
// every check around a conversion; it never converts anything itself. The specification is
// CLAUDE/Plan/00210-convert-cli-markdown-to-docx/SPEC-convert.md; the expectation numbers
// (E1...) the comments cite are its.
package convert

import (
	"context"
	_ "embed" // usage.txt is embedded verbatim
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

//go:embed usage.txt
var usageText string

// Env is everything a run touches outside its own memory and the files it is given.
type Env struct {
	Runner   execx.Runner
	Stdout   io.Writer
	Stderr   io.Writer
	LookPath func(string) (string, error)
	// Pid names the temporary each engine writes to (E11).
	Pid   int
	Table []InputType
}

// NewEnv is the production environment.
func NewEnv() Env {
	return Env{
		Runner: execx.OS{}, Stdout: os.Stdout, Stderr: os.Stderr,
		LookPath: exec.LookPath, Pid: os.Getpid(), Table: Table,
	}
}

// App is one convert invocation.
type App struct {
	Env Env
}

// failure ends a run with code after printing "convert: <msg>".
type failure struct {
	code term.ExitCode
	msg  string
}

func (f *failure) Error() string { return f.msg }

func fail(format string, args ...any) error {
	return &failure{code: term.ExitFailure, msg: fmt.Sprintf(format, args...)}
}

var errCancelled = &failure{code: term.ExitCancelled, msg: "cancelled."}

// job is one requested output: its row, where it lands and the temporary the engine writes.
type job struct {
	out       Output
	path, tmp string
}

// Run is one invocation; its exit code is SPEC-convert.md section 5's.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	var out, errOut term.Printer
	err := a.main(ctx, args, &out)
	code := term.ExitOK
	if err != nil {
		code = term.ExitFailure
		if f, ok := errors.AsType[*failure](err); ok {
			code = f.code
		}
		errOut.Print(a.Env.Stderr, "convert: "+err.Error()+"\n")
	}
	if code == term.ExitOK && out.Err() != nil { // E15
		errOut.Print(a.Env.Stderr, "convert: could not write the output: "+out.Err().Error()+"\n")
		return term.ExitFailure
	}
	return code
}

func (a App) main(ctx context.Context, args []string, out *term.Printer) error {
	opts, err := parseArgs(args)
	if opts.help {
		out.Print(a.Env.Stdout, usageText)
		return nil
	}
	if err != nil {
		return err
	}
	in, err := a.inputType(opts.file)
	if err != nil {
		return err
	}
	if len(opts.to) == 0 {
		for _, o := range in.Outputs { // E5
			out.Print(a.Env.Stdout, fmt.Sprintf("%s  %s (%s)\n", o.Format, o.Desc, o.Engine.Program()))
		}
		return nil
	}
	jobs, err := a.plan(opts, in)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(opts.file)
	if err != nil {
		return fail("cannot resolve '%s': %v", opts.file, err)
	}
	for _, j := range jobs {
		if err := a.convert(ctx, abs, opts.file, j); err != nil {
			return err
		}
		out.Print(a.Env.Stdout, j.path+"\n") // E13
	}
	return nil
}

// inputType is E3 and E4.
func (a App) inputType(file string) (InputType, error) {
	info, err := os.Stat(file)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return InputType{}, fail("'%s' does not exist.", file)
	case err != nil:
		return InputType{}, fail("cannot read '%s': %v", file, err)
	case !info.Mode().IsRegular():
		return InputType{}, fail("'%s' is not a regular file.", file)
	}
	in, ok := typeOf(file, a.Env.Table)
	if !ok {
		known := make([]string, 0, len(a.Env.Table))
		for _, t := range a.Env.Table {
			known = append(known, fmt.Sprintf("%s (%s)", t.Name, strings.Join(t.Exts, ", ")))
		}
		return InputType{}, fail("cannot convert '%s': known input types are %s.", file, strings.Join(known, "; "))
	}
	return in, nil
}

// plan is E7-E10: every format, output and engine is checked before anything runs.
func (a App) plan(opts options, in InputType) ([]job, error) {
	base := filepath.Base(opts.file)
	stem := filepath.Join(filepath.Dir(opts.file), strings.TrimSuffix(base, filepath.Ext(base)))
	jobs := make([]job, 0, len(opts.to))
	for _, format := range opts.to {
		o, ok := in.output(format)
		if !ok {
			return nil, &failure{code: term.ExitUsage, msg: fmt.Sprintf("'%s' (%s) cannot become '%s'; it can become: %s.",
				opts.file, in.Name, format, strings.Join(in.formats(), ", "))}
		}
		path := stem + "." + format
		jobs = append(jobs, job{out: o, path: path, tmp: fmt.Sprintf("%s.tmp.%d", path, a.Env.Pid)})
	}
	for _, j := range jobs {
		if _, err := os.Lstat(j.path); err == nil && !opts.force {
			return nil, fail("'%s' already exists; use --force to replace it.", j.path)
		}
	}
	for _, j := range jobs {
		if _, err := a.Env.LookPath(j.out.Engine.Program()); err != nil {
			return nil, notInstalled(j.out.Engine)
		}
	}
	return jobs, nil
}

func notInstalled(e Engine) error {
	return fail("%s is not installed; the play that installs it is %s (run it, do not install by hand).", e.Program(), e.Play())
}

// convert is E11, E12 and E14 for one output: the engine writes the temporary, which is
// renamed onto the output only when the engine succeeded.
func (a App) convert(ctx context.Context, abs, file string, j job) error {
	tmp, err := filepath.Abs(j.tmp)
	if err != nil {
		return fail("cannot resolve '%s': %v", j.tmp, err)
	}
	err = j.out.Engine.Convert(ctx, a.Env.Runner, abs, tmp)
	if err == nil {
		if rerr := os.Rename(tmp, j.path); rerr != nil {
			return errors.Join(fail("cannot move the output into place at '%s': %v", j.path, rerr), fsx.RemoveIfPresent(tmp))
		}
		return nil
	}
	if rerr := fsx.RemoveIfPresent(tmp); rerr != nil {
		return fail("%v; and %v", engineError(ctx, file, j, err), rerr)
	}
	return engineError(ctx, file, j, err)
}

func engineError(ctx context.Context, file string, j job, err error) error {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return errCancelled
	}
	if execx.IsNotInstalled(err) {
		return notInstalled(j.out.Engine)
	}
	reason := err.Error()
	if exitErr, ok := errors.AsType[*execx.ExitError](err); ok {
		reason = fmt.Sprintf("%s exited %d", exitErr.Name, exitErr.Code)
		if s := strings.TrimSpace(exitErr.Stderr); s != "" {
			reason = s
		}
	}
	return fail("%s could not convert '%s' to %s: %s", j.out.Engine.Program(), file, j.out.Format, reason)
}
