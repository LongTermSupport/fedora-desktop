package js

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/discovery"
)

// extensionsDir is the project that owns the ESLint config and the node_modules holding ESLint.
const extensionsDir = "extensions"

// requireTools proves node runs, and that the project's ESLint (version 8, its config present)
// is installed. A missing tool is never a skip: the run could not complete.
func (g *gate) requireTools(ctx context.Context) error {
	res, err := g.runTool(ctx, probeTimeout, "node", "--version")
	if err != nil {
		return fmt.Errorf("node not installed (install via play-nvm-install.yml): %w", err)
	}
	if res.Code != 0 {
		return fmt.Errorf("node --version exited %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	return g.requireEslint(ctx)
}

// discover asks the shared discovery (SPEC-qa-discovery.md, D16) for the JavaScript files git
// would publish, outside the excluded trees, as paths relative to the root, sorted.
func (g *gate) discover() ([]string, error) {
	var stdout, stderr bytes.Buffer
	env := discovery.Env{Stdout: &stdout, Stderr: &stderr, Cwd: g.root, Runner: g.env.Runner}
	args := []string{flagRoot, g.root, flagScripts, g.scripts, "js"}
	if code := discovery.Main(args, env); code != discovery.ExitOK {
		return nil, fmt.Errorf("discovery of js failed: %s", strings.TrimSpace(strings.TrimPrefix(stderr.String(), "ERROR: ")))
	}
	var rels []string
	for abs := range strings.SplitSeq(stdout.String(), "\x00") {
		if abs != "" {
			rels = append(rels, strings.TrimPrefix(abs, g.root+"/"))
		}
	}
	return rels, nil
}

// lintable is the listed files under extensions/, the project the ESLint config belongs to.
func (g *gate) lintable(files []string) []string {
	var out []string
	for _, rel := range files {
		if strings.HasPrefix(rel, extensionsDir+"/") {
			out = append(out, rel)
		}
	}
	return out
}

// parseCheck runs node --check on every file, one process each, and returns the message of each
// file that does not parse (keyed by its relative path). A node that cannot run is an error.
func (g *gate) parseCheck(ctx context.Context, files []string) (map[string]string, error) {
	failed := map[string]string{}
	for _, rel := range files {
		res, err := g.runTool(ctx, nodeTimeout, "node", "--check", filepath.Join(g.root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		if res.Code != 0 {
			failed[rel] = g.nodeMessage(res.Stderr)
		}
	}
	return failed, nil
}

// nodeMessage is node's own text for a file that does not parse, without the checkout's path and
// without the version trailer node ends it with.
func (g *gate) nodeMessage(stderr string) string {
	var kept []string
	for line := range strings.SplitSeq(strings.ReplaceAll(stderr, g.root+"/", ""), "\n") {
		if !strings.HasPrefix(line, "Node.js v") {
			kept = append(kept, line)
		}
	}
	if msg := strings.TrimSpace(strings.Join(kept, "\n")); msg != "" {
		return msg
	}
	return "node --check failed with no message"
}
