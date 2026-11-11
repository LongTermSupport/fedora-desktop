package release

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/releasever"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// changelogFile is the changelog at the top of the checkout.
const changelogFile = "CHANGELOG.md"

const usage = `usage: release-plan decide  --branch F<major>
       release-plan prepare --branch F<major> --version <major>.<minor>.<patch>
       release-plan notes   --version <major>.<minor>.<patch> --commit <full sha>
`

// AppGit is the questions the app asks git: Decide's, and a file as committed at a ref.
type AppGit interface {
	Git
	FileAtRef(ctx context.Context, dir, ref, path string) (string, bool, error)
}

// Files is the changelog of the working tree.
type Files interface {
	// Read is the file's text and whether it is there at all.
	Read(path string) (text string, present bool, err error)
	// Write replaces the file whole.
	Write(path, data string) error
}

// App is the release-plan command, the workflow's one caller of this package:
//
//	decide   what the push calls for, as key=value lines for $GITHUB_OUTPUT (nothing is changed)
//	prepare  decide again, and write the changelog entry of the release that was decided
//	notes    the changelog entry of a version, as committed at its release commit
//
// A refusal is exit 1 with words on stderr and nothing on stdout; a usage error is exit 64.
type App struct {
	Out, Err io.Writer
	Git      AppGit
	Pulls    func(branch string) Pulls // the pull requests merged into a branch
	Dir      string                    // the checkout
	Remote   string
	Now      func() time.Time
	Files    Files
}

// Run runs one command.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	var p term.Printer
	code := a.dispatch(ctx, &p, args)
	if p.Err() != nil {
		return term.ExitFailure
	}
	return code
}

func (a App) dispatch(ctx context.Context, p *term.Printer, args []string) term.ExitCode {
	if len(args) == 0 {
		return a.usageError(p, "a command is needed")
	}
	names, run := a.command(args[0])
	if run == nil {
		return a.usageError(p, fmt.Sprintf("unknown command %q", args[0]))
	}
	flags, err := parseFlags(args[1:], names)
	if err != nil {
		return a.usageError(p, err.Error())
	}
	if err := run(ctx, p, flags); err != nil {
		p.Print(a.Err, "release-plan: "+err.Error()+"\n")
		return term.ExitFailure
	}
	return term.ExitOK
}

// command is a command's flags, all required, and what it does.
func (a App) command(name string) ([]string, func(context.Context, *term.Printer, map[string]string) error) {
	switch name {
	case "decide":
		return []string{"branch"}, a.decide
	case "prepare":
		return []string{"branch", "version"}, a.prepare
	case "notes":
		return []string{"version", "commit"}, a.notes
	default:
		return nil, nil
	}
}

func (a App) usageError(p *term.Printer, msg string) term.ExitCode {
	p.Print(a.Err, "release-plan: "+msg+"\n"+usage)
	return term.ExitUsage
}

// parseFlags reads --name value pairs: exactly the names given, each once, each with a value.
func parseFlags(args, names []string) (map[string]string, error) {
	flags := map[string]string{}
	for i := 0; i < len(args); i += 2 {
		name, ok := strings.CutPrefix(args[i], "--")
		if !ok || !slices.Contains(names, name) {
			return nil, fmt.Errorf("unexpected argument %q", args[i])
		}
		if i+1 >= len(args) {
			return nil, fmt.Errorf("--%s needs a value", name)
		}
		if _, dup := flags[name]; dup {
			return nil, fmt.Errorf("--%s is given twice", name)
		}
		flags[name] = args[i+1]
	}
	for _, name := range names {
		if _, ok := flags[name]; !ok {
			return nil, fmt.Errorf("--%s is required", name)
		}
	}
	return flags, nil
}

func (a App) decider(branch string) Decider {
	return Decider{Git: a.Git, Pulls: a.Pulls(branch), Dir: a.Dir, Branch: branch, Remote: a.Remote}
}

// decide prints the plan, one key=value per line. Each value is checked to be what it should be
// before it is printed, so no line can carry more than its own value into $GITHUB_OUTPUT.
func (a App) decide(ctx context.Context, p *term.Printer, flags map[string]string) error {
	plan, err := a.decider(flags["branch"]).Decide(ctx)
	if err != nil {
		return err
	}
	if plan.Version != "" {
		if _, err := releasever.ParseVersion(plan.Version); err != nil {
			return err
		}
	}
	if plan.Commit != "" && !commitSHA.MatchString(plan.Commit) {
		return fmt.Errorf("%q is not a full commit sha", plan.Commit)
	}
	p.Print(a.Out, "action="+plan.Action+"\n")
	p.Print(a.Out, "version="+plan.Version+"\n")
	p.Print(a.Out, "commit="+plan.Commit+"\n")
	p.Print(a.Out, "reason="+oneLine(plan.Reason)+"\n")
	return nil
}

// prepare writes the entry of the release decide named, but only if the branch still calls for
// exactly that release: what the version step saw and what this step sees must agree.
func (a App) prepare(ctx context.Context, _ *term.Printer, flags map[string]string) error {
	want, err := releasever.ParseVersion(flags["version"])
	if err != nil {
		return err
	}
	plan, err := a.decider(flags["branch"]).Decide(ctx)
	if err != nil {
		return err
	}
	if plan.Action != ActionPrepare || plan.Version != want.String() {
		return fmt.Errorf("the branch no longer calls for preparing %s (it calls for %q %s)", want, plan.Action, plan.Version)
	}
	path := filepath.Join(a.Dir, changelogFile)
	existing, _, err := a.Files.Read(path)
	if err != nil {
		return err
	}
	text, err := Prepend(existing, plan.Version, a.Now().UTC().Format(time.DateOnly), plan.Releasable)
	if err != nil {
		return err
	}
	return a.Files.Write(path, text)
}

// notes prints the entry of the version in the changelog as committed at the release commit.
func (a App) notes(ctx context.Context, p *term.Printer, flags map[string]string) error {
	if _, err := releasever.ParseVersion(flags["version"]); err != nil {
		return err
	}
	commit := flags["commit"]
	if !commitSHA.MatchString(commit) {
		return fmt.Errorf("%q is not a full commit sha", commit)
	}
	text, found, err := a.Git.FileAtRef(ctx, a.Dir, commit, changelogFile)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%s has no %s", commit, changelogFile)
	}
	notes, err := Notes(text, flags["version"])
	if err != nil {
		return err
	}
	p.Print(a.Out, notes+"\n")
	return nil
}

// OSFiles is Files over a directory on disk. Reads and writes stay under it.
type OSFiles struct{ Root string }

// Read implements Files; a file that is not there is present=false, any other failure an error.
func (f OSFiles) Read(path string) (string, bool, error) {
	rel, err := filepath.Rel(f.Root, path)
	if err != nil {
		return "", false, err
	}
	root, err := os.OpenRoot(f.Root)
	if err != nil {
		return "", false, err
	}
	data, readErr := root.ReadFile(rel)
	closeErr := root.Close()
	if errors.Is(readErr, fs.ErrNotExist) {
		return "", false, closeErr
	}
	if err := errors.Join(readErr, closeErr); err != nil {
		return "", false, err
	}
	return string(data), true, nil
}

// Write implements Files.
func (f OSFiles) Write(path, data string) error {
	return fsx.WriteAtomic(path, "changelog", []byte(data), 0o644)
}
