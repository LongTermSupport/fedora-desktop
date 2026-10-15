package js

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// ignoredPrefix starts the warning ESLint gives a file an ignore pattern dropped.
const ignoredPrefix = "File ignored"

// eslint8 matches the version ESLint 8 prints; the pins below are its flags.
var eslint8 = regexp.MustCompile(`^v8\.\d+\.\d+`)

// message is one finding ESLint made on a file.
type message struct {
	Rule     *string `json:"ruleId"`
	Text     string  `json:"message"`
	Line     int     `json:"line"`
	Column   int     `json:"column"`
	Severity int     `json:"severity"`
}

// eslintBin is where the project's ESLint lives.
func (g *gate) eslintBin() string {
	return filepath.Join(g.root, extensionsDir, "node_modules", ".bin", "eslint")
}

// eslintConfig is the one config the lint runs under.
func (g *gate) eslintConfig() string {
	return filepath.Join(g.root, extensionsDir, ".eslintrc.json")
}

// requireEslint proves the dev tooling is set up. It is never installed or skipped here: no
// playbook installs it, so a missing one is exit 2 with the one-time setup.
func (g *gate) requireEslint(ctx context.Context) error {
	info, err := os.Stat(g.eslintBin())
	if err != nil || info.IsDir() {
		return fmt.Errorf("eslint dev tooling for %s/ is not set up: %s is missing.\n"+
			"  It needs node deps no playbook installs (dev-only, needed to check the extensions' JavaScript).\n"+
			"  Set it up once, reproducibly from package-lock.json, then run this again:\n      cd %s && npm ci",
			extensionsDir, g.eslintBin(), extensionsDir)
	}
	if _, statErr := os.Stat(g.eslintConfig()); statErr != nil {
		return fmt.Errorf("the ESLint config %s cannot be read: %w", g.eslintConfig(), statErr)
	}
	res, err := g.runTool(ctx, probeTimeout, g.eslintBin(), "--version")
	if err != nil {
		return err
	}
	if version := strings.TrimSpace(res.Stdout); res.Code != 0 || !eslint8.MatchString(version) {
		return fmt.Errorf("eslint --version printed %q (exit %d): this gate's pins (--no-eslintrc, --no-inline-config) are ESLint 8's; install it with cd %s && npm ci", version, res.Code, extensionsDir)
	}
	return nil
}

// lint runs ESLint once over the files and returns the messages per relative path. The pins:
// --no-eslintrc switches off every config lookup, so a nested .eslintrc.* or a package.json
// eslintConfig cannot weaken the rules for its subtree; --config names the repository's; and
// --no-inline-config makes eslint-disable comments inert.
func (g *gate) lint(ctx context.Context, files []string) (map[string][]message, error) {
	abs := make([]string, 0, len(files))
	rel := make(map[string]string, len(files))
	for _, f := range files {
		p := filepath.Join(g.root, filepath.FromSlash(f))
		abs = append(abs, p)
		rel[p] = f
	}
	argv := append([]string{g.eslintBin()}, EslintFlags(g.eslintConfig())...)
	res, err := g.runTool(ctx, eslintLimit, append(argv, abs...)...)
	if err != nil {
		return nil, err
	}
	return readReport(g.root, rel, res.Code, res.Stdout, res.Stderr)
}

// EslintFlags are the pins every ESLint run of this repository carries, in the order they are
// given: nothing but the named config sets the rules, and no inline directive changes them. The
// files follow them. qa-changed shares this list so the two cannot drift.
func EslintFlags(config string) []string {
	return []string{"--no-eslintrc", "--config", config, "--no-inline-config", "--format", "json"}
}

// Finding is one message ESLint made on a file, the path relative to the root.
type Finding struct {
	message

	File string
}

// String is the finding as a line: path:line:column: text (rule).
func (f Finding) String() string {
	return fmt.Sprintf("%s:%d:%d: %s%s", f.File, f.Line, f.Column, f.Text, ruleSuffix(f.message))
}

// readReport is the judgement of one ESLint run. rel maps each absolute path the run was given to
// its path relative to root; status, stdout and stderr are what the run produced. It returns the
// messages per relative path, or an error when the run proves nothing: ESLint failed, did not
// print a JSON array, or did not read exactly the files it was given (see prove).
func readReport(root string, rel map[string]string, status int, stdout, stderr string) (map[string][]message, error) {
	if status != 0 && status != 1 {
		return nil, fmt.Errorf("eslint invocation failed (status %d):\n%s", status, strings.TrimSpace(stderr))
	}
	var results []fileResult
	if decodeErr := json.Unmarshal([]byte(stdout), &results); decodeErr != nil {
		return nil, fmt.Errorf("eslint did not print a JSON array of results: %w", decodeErr)
	}
	found, err := prove(root, rel, results)
	if err != nil {
		return nil, err
	}
	if status == 1 && len(found) == 0 {
		return nil, errors.New("eslint exited 1 but reported no finding: the exit status and the report disagree")
	}
	return found, nil
}

// Findings is the judgement of one ESLint run (see readReport) as a sorted list.
func Findings(root string, rel map[string]string, status int, stdout, stderr string) ([]Finding, error) {
	found, err := readReport(root, rel, status, stdout, stderr)
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, file := range slices.Sorted(maps.Keys(found)) {
		for _, m := range found[file] {
			out = append(out, Finding{File: file, message: m})
		}
	}
	return out, nil
}

// fileResult is ESLint's answer for one file.
type fileResult struct {
	FilePath string    `json:"filePath"`
	Messages []message `json:"messages"`
}

// prove is the independent proof that ESLint read every file it was given (rel maps each absolute
// path given to its relative one): each is answered once, nothing else is answered, and none was
// dropped by an ignore pattern. It returns the messages per relative path.
func prove(root string, rel map[string]string, results []fileResult) (map[string][]message, error) {
	answered := map[string]bool{}
	var ignored []string
	found := map[string][]message{}
	for _, r := range results {
		name, isGiven := rel[r.FilePath]
		switch {
		case !isGiven:
			return nil, fmt.Errorf("eslint reported on %s, which it was not given", strings.TrimPrefix(r.FilePath, root+"/"))
		case answered[name]:
			return nil, fmt.Errorf("eslint reported on %s twice", name)
		}
		answered[name] = true
		if slices.ContainsFunc(r.Messages, func(m message) bool { return strings.HasPrefix(m.Text, ignoredPrefix) }) {
			ignored = append(ignored, name)
		}
		if len(r.Messages) > 0 {
			found[name] = sortedMessages(r.Messages)
		}
	}
	if missed := len(rel) - len(answered); missed > 0 {
		return nil, fmt.Errorf("eslint did not report on %d of the %d file(s) it was given: a pass here would vouch for code ESLint never read", missed, len(rel))
	}
	if len(ignored) > 0 {
		slices.Sort(ignored)
		return nil, fmt.Errorf("eslint ignored %d file(s) it was given, so it did not read them:\n    %s\n  Check ignorePatterns in the config and any .eslintignore", len(ignored), strings.Join(ignored, "\n    "))
	}
	return found, nil
}

// sortedMessages orders findings by line, then column.
func sortedMessages(msgs []message) []message {
	out := slices.Clone(msgs)
	slices.SortStableFunc(out, func(a, b message) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return a.Column - b.Column
	})
	return out
}
