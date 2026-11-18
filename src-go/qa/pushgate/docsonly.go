package pushgate

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/prepush"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// docsCheckScript is the local QA that judges a docs-only range in CI's stead, run with
// --base at the protected line's old tip.
const docsCheckScript = "scripts/qa-changed.bash"

// errFailClosed is a reason the gate cannot take a range as docs-only; CI's run is then required.
var errFailClosed = errors.New("CI's QA run is required")

func failClosed(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{errFailClosed}, args...)...)
}

// localDocsVerdict judges a push whose range qa.yml's own push filter leaves out entirely: the
// local docs check runs in CI's stead, and its verdict line is returned with true. A range qa.yml
// runs for answers false, as does one the gate cannot know is docs-only (saying why on stderr).
func (g *gate) localDocsVerdict(ctx context.Context, ref prepush.Ref) (string, bool, error) {
	files, docs, err := g.docsOnlyRange(ctx, ref)
	if err == nil && docs {
		err = g.checkoutIs(ctx, ref.LocalID)
	}
	if errors.Is(err, errFailClosed) {
		g.note(ref.RemoteRef + ": " + err.Error())
		return "", false, nil
	}
	if err != nil || !docs {
		return "", false, err
	}
	argv := []string{filepath.Join(g.env.Cwd, docsCheckScript), "--base", ref.RemoteID}
	shown := "./" + docsCheckScript + " --base " + ref.RemoteID
	g.note(ref.RemoteRef + ": the range is docs-only (" + QAWorkflow + "'s push filter leaves out its " + strconv.Itoa(len(files)) + " files); running " + shown)
	res, err := g.env.Runner.Run(ctx, proc.Cmd{Argv: argv, Dir: g.env.Cwd, StdoutTo: g.env.Stderr, StderrTo: g.env.Stderr})
	switch {
	case err != nil:
		return "", false, fmt.Errorf("the local docs check %s: %w", shown, err)
	case res.Code != 0 || res.TimedOut || res.Leftover:
		return "", false, refuse("the range is docs-only, and its local docs check (%s) failed with exit %d: the failed check is named in its output above", shown, res.Code)
	}
	return "docs-only (" + strconv.Itoa(len(files)) + " files " + QAWorkflow + " does not run for): the local docs check (" + shown + ") passed and stood in for CI's QA run", true, nil
}

func (g *gate) note(line string) {
	g.errOut.Line("qa-push-gate: " + line)
}

// docsOnlyRange is the files the push changes, and whether qa.yml at the pushed commit leaves
// out every one of them. An error wrapping errFailClosed is a reason it cannot be known.
func (g *gate) docsOnlyRange(ctx context.Context, ref prepush.Ref) ([]string, bool, error) {
	if ref.RemoteID == prepush.ZeroID {
		return nil, false, failClosed("a new branch has no tip on the remote to take its changed files from")
	}
	workflow, err := g.git(ctx, "cat-file", "blob", ref.LocalID+":"+QAWorkflow)
	if err != nil {
		return nil, false, failClosed("cannot read %s at %s: %v", QAWorkflow, ref.LocalID[:short], err)
	}
	filter, err := parsePushFilter([]byte(workflow))
	if err != nil {
		return nil, false, failClosed("cannot read the push filter of %s at %s: %v", QAWorkflow, ref.LocalID[:short], err)
	}
	listed, err := g.git(ctx, "diff", "--name-only", "-z", "--no-renames", ref.RemoteID, ref.LocalID, "--")
	if err != nil {
		return nil, false, failClosed("cannot list the files changed from %s to %s: %v", ref.RemoteID[:short], ref.LocalID[:short], err)
	}
	files := strings.FieldsFunc(listed, func(r rune) bool { return r == 0 })
	for _, f := range files {
		if filter.runsFor(f) {
			return files, false, nil
		}
	}
	return files, true, nil
}

// checkoutIs holds that the checkout the local docs check judges is the pushed commit: HEAD is
// sha, and nothing is changed or untracked.
func (g *gate) checkoutIs(ctx context.Context, sha string) error {
	const why = "the range is docs-only, but the local docs check judges this checkout, "
	head, err := g.git(ctx, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return failClosed(why+"whose HEAD cannot be read: %v", err)
	}
	if strings.TrimSpace(head) != sha {
		return failClosed(why+"whose HEAD is not %s: check it out and push again", sha[:short])
	}
	status, err := g.git(ctx, "status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return failClosed(why+"whose status cannot be read: %v", err)
	}
	if status != "" {
		return failClosed(why + "which has uncommitted or untracked files: commit or remove them and push again")
	}
	return nil
}

// git runs git in the checkout; anything but a clean exit is an error.
func (g *gate) git(ctx context.Context, args ...string) (string, error) {
	res, err := g.env.Runner.Run(ctx, proc.Cmd{Argv: append([]string{"git"}, args...), Dir: g.env.Cwd})
	switch {
	case err != nil:
		return "", err
	case res.Code != 0 || res.TimedOut || res.Leftover:
		return "", fmt.Errorf("git %s: exit %d: %s", args[0], res.Code, strings.TrimSpace(res.Stderr))
	}
	return res.Stdout, nil
}
