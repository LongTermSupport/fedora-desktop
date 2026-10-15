package bash

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/discovery"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const (
	toolShellcheck = "shellcheck"
	toolBash       = "bash"
)

// requireTools proves shellcheck and bash run.
func (g *gate) requireTools(ctx context.Context) error {
	res, err := g.runTool(ctx, probeTimeout, proc.Cmd{Argv: []string{toolShellcheck, "--version"}})
	if err != nil {
		return fmt.Errorf("shellcheck not installed (%w): it is installed by playbooks/imports/play-devtools.yml\n  Deploy it: ansible-playbook playbooks/imports/play-devtools.yml\n  Do NOT install it by hand", err)
	}
	if res.Code != 0 || !strings.Contains("\n"+res.Stdout, "\nversion: ") {
		return fmt.Errorf("shellcheck --version did not answer as shellcheck does (exit %d): %q %s", res.Code, strings.TrimSpace(res.Stdout), strings.TrimSpace(res.Stderr))
	}
	res, err = g.runTool(ctx, probeTimeout, proc.Cmd{Argv: []string{toolBash, "--version"}})
	if err != nil {
		return fmt.Errorf("bash not installed: %w", err)
	}
	if res.Code != 0 {
		return fmt.Errorf("bash --version exited %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	return nil
}

// discover asks the shared discovery (SPEC-qa-discovery.md) for the shell scripts git would
// publish, and proves none that git tracks was left out. Which files those are is defined there
// and nowhere else; an empty list is its error (D13).
func (g *gate) discover() ([]string, error) {
	files, err := g.list("shell")
	if err != nil {
		return nil, err
	}
	tracked, err := g.list("tracked-shell")
	if err != nil {
		return nil, err
	}
	var missed []string
	for _, rel := range tracked {
		if !slices.Contains(files, rel) {
			missed = append(missed, rel)
		}
	}
	if len(missed) > 0 {
		slices.Sort(missed)
		return nil, fmt.Errorf("discovery missed %d tracked shell script(s):\n    %s\n  These are shell scripts this gate would have reported a pass over without reading", len(missed), strings.Join(missed, "\n    "))
	}
	return files, nil
}

// list runs one discovery list and returns its paths relative to the root.
func (g *gate) list(name string) ([]string, error) {
	var stdout, stderr bytes.Buffer
	env := discovery.Env{Stdout: &stdout, Stderr: &stderr, Cwd: g.root, Runner: g.env.Runner}
	args := []string{flagRoot, g.root, flagScripts, g.script, name}
	if code := discovery.Main(args, env); code != discovery.ExitOK {
		return nil, fmt.Errorf("discovery of %s failed: %s", name, strings.TrimSpace(strings.TrimPrefix(stderr.String(), "ERROR: ")))
	}
	var rels []string
	for p := range strings.SplitSeq(stdout.String(), "\x00") {
		if p != "" {
			rels = append(rels, strings.TrimPrefix(p, g.root+"/"))
		}
	}
	return rels, nil
}
