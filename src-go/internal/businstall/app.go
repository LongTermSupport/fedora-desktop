// Package businstall is agent-bus-install: it builds an agent team bus homeserver host on
// Fedora. `software` installs the packages, the service account, the pinned Tuwunel, the
// zipapps and the units; `team` installs or updates one team; `remove`, `backup-now`,
// `restore`, `backups` and `check` look after it.
//
// It moves files and drives dnf, systemd, firewalld and NetworkManager. Validation and
// rendering of the team file, and every account operation, stay the Python admin tool's.
// stdout carries only marker lines (CHANGED, CHECK, BACKUP) and the admin tool's own report;
// diagnostics go to stderr. No secret is ever printed or put in an argument.
package businstall

import (
	"context"
	_ "embed" // the usage text
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

//go:embed usage.txt
var usageText string

// optTeam is the --team option, which every command but `software` and `team` takes.
const optTeam = "team"

var teamPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,23}$`)

// backupIDPattern is a backup number: no sign, no leading zero, no whitespace.
var backupIDPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

// failure is a run that ends with a message and an exit status. An empty message is
// printed as nothing (check reports its failure in its own lines).
type failure struct {
	code term.ExitCode
	msg  string
}

func (f *failure) Error() string { return f.msg }

func fail(code term.ExitCode, format string, args ...any) error {
	return &failure{code: code, msg: fmt.Sprintf(format, args...)}
}

func failf(format string, args ...any) error {
	return fail(term.ExitFailure, format, args...)
}

// sink is a writer that remembers its first failure, so a full disk under stdout fails the
// run instead of passing for success.
type sink struct {
	w   io.Writer
	err error
}

func (s *sink) Write(p []byte) (int, error) {
	n, err := s.w.Write(p)
	if err != nil {
		if s.err == nil {
			s.err = err
		}
		return n, fmt.Errorf("write: %w", err)
	}
	return n, nil
}

// printf writes formatted text; a failure is kept in err.
func (s *sink) printf(format string, args ...any) {
	if _, err := fmt.Fprintf(s.w, format, args...); err != nil && s.err == nil {
		s.err = err
	}
}

// App is one installer run.
type App struct {
	cfg    Config
	stdout *sink
	stderr *sink

	dirs    map[string]*fsx.FS
	root    *fsx.Owner
	svc     *fsx.Owner
	workDir string
}

// NewApp is an installer over cfg.
func NewApp(cfg Config) *App {
	return &App{
		cfg:    cfg,
		stdout: &sink{w: cfg.Stdout},
		stderr: &sink{w: cfg.Stderr},
		dirs:   map[string]*fsx.FS{},
	}
}

// Run runs the installer with the command line (without the program name) and returns its
// exit status.
func (a *App) Run(args []string) term.ExitCode {
	a.stdout.err, a.stderr.err = nil, nil
	code := a.report(a.dispatch(args))
	if err := a.finish(); err != nil && code == term.ExitOK {
		a.sayf("%v", err)
		code = term.ExitFailure
	}
	if a.stdout.err != nil && code == term.ExitOK {
		a.sayf("writing to stdout: %v", a.stdout.err)
		code = term.ExitFailure
	}
	return code
}

// ctx is the context of every command the installer runs: a run has no deadline of its own
// and is ended by its caller (the play, systemd or a signal).
func (a *App) ctx() context.Context { return context.Background() }

func (a *App) report(err error) term.ExitCode {
	if err == nil {
		return term.ExitOK
	}
	if f, ok := errors.AsType[*failure](err); ok {
		if f.msg != "" {
			a.sayf("%s", f.msg)
		}
		return f.code
	}
	a.sayf("%v", err)
	return term.ExitFailure
}

// finish releases every directory the run opened.
func (a *App) finish() error {
	var errs []error
	for display, f := range a.dirs {
		if err := f.Close(); err != nil {
			errs = append(errs, err)
		}
		delete(a.dirs, display)
	}
	if a.workDir != "" {
		if err := os.RemoveAll(a.workDir); err != nil {
			errs = append(errs, fmt.Errorf("remove the work directory: %w", err))
		}
		a.workDir = ""
	}
	return errors.Join(errs...)
}

func (a *App) sayf(format string, args ...any) {
	a.stderr.printf("%s: %s\n", prog, fmt.Sprintf(format, args...))
}

func (a *App) changedf(format string, args ...any) {
	a.stdout.printf("CHANGED\t%s\n", fmt.Sprintf(format, args...))
}

func (a *App) dispatch(args []string) error {
	if len(args) == 0 {
		a.stderr.printf("%s", usageText)
		return &failure{code: term.ExitUsage}
	}
	command, rest := args[0], args[1:]
	switch command {
	case "-h", "--help", "help":
		a.stdout.printf("%s", usageText)
		return nil
	case "--version":
		a.stdout.printf("%s\n", a.cfg.SourceDigest)
		return nil
	case "software":
		return a.software(rest)
	case "team":
		return a.team(rest)
	case "remove":
		return a.remove(rest)
	case "backup-now":
		return a.backupNow(rest)
	case "restore":
		return a.restore(rest)
	case "backups":
		return a.backups(rest)
	case "check":
		return a.check(rest)
	}
	return fail(term.ExitUsage, "unknown command '%s' (see --help)", command)
}

// options are the parsed --name value pairs of a command; a flag is stored as "1".
type options map[string]string

// parseOptions reads --name VALUE and --name=VALUE for the names in spec (true: takes a
// value, false: a flag). A value may itself start with "--".
func parseOptions(args []string, spec map[string]bool) (options, error) {
	opts := options{}
	rest := args
	for len(rest) > 0 {
		arg := rest[0]
		rest = rest[1:]
		if !strings.HasPrefix(arg, "--") {
			return nil, fail(term.ExitUsage, "unexpected argument '%s' (see --help)", arg)
		}
		name, value, hasValue := strings.Cut(arg[2:], "=")
		takesValue, known := spec[name]
		if !known {
			return nil, fail(term.ExitUsage, "unknown option --%s (see --help)", name)
		}
		if _, given := opts[name]; given {
			return nil, fail(term.ExitUsage, "--%s given twice", name)
		}
		if !takesValue {
			if hasValue {
				return nil, fail(term.ExitUsage, "--%s takes no value", name)
			}
			opts[name] = "1"
			continue
		}
		if !hasValue {
			if len(rest) == 0 {
				return nil, fail(term.ExitUsage, "--%s needs a value", name)
			}
			value = rest[0]
			rest = rest[1:]
		}
		if value == "" {
			return nil, fail(term.ExitUsage, "--%s needs a non-empty value", name)
		}
		opts[name] = value
	}
	return opts, nil
}

// require is the usage error for an option the command cannot run without.
func (o options) require(name, command string) error {
	if o[name] == "" {
		return fail(term.ExitUsage, "%s needs --%s", command, name)
	}
	return nil
}

// team is the validated --team of a command that takes one.
func (o options) team(command string) (string, error) {
	if err := o.require(optTeam, command); err != nil {
		return "", err
	}
	if !teamPattern.MatchString(o[optTeam]) {
		return "", fail(term.ExitUsage, "--team '%s' is not a team name (%s)", o[optTeam], teamPattern)
	}
	return o[optTeam], nil
}

func (a *App) requireRoot() error {
	if a.cfg.Prefix == "" && a.cfg.Euid() != 0 {
		return fail(term.ExitNotRoot, "run it as root: sudo %s ...", prog)
	}
	return nil
}

// adminCodes are the exit statuses of the admin tool that are part of its contract with the
// callers of the installer: usage, unreachable, refused, busy, not root and configuration.
var adminCodes = []int{64, 69, 70, 75, 77, 78}

// adminFailure ends the run for a failed admin-tool command: with the tool's own status
// when that status is one of its contract, with 1 otherwise.
func adminFailure(err error, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if code, ok := execx.ExitOf(err); ok && slices.Contains(adminCodes, code) {
		return fail(term.ExitCode(code), "%s", msg)
	}
	return failf("%s: %v", msg, err)
}
