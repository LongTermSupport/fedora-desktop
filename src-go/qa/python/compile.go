package python

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// checkerScript compiles every file named on its command line and prints one JSON line for each:
// the path as given and the compile error, empty when the file compiles. The source goes in as
// bytes so an encoding declaration is honoured, and nothing is written to disk.
const checkerScript = `
import json, sys, traceback
for path in sys.argv[1:]:
    try:
        with open(path, "rb") as fh:
            source = fh.read()
        compile(source, path, "exec", dont_inherit=True)
        error = ""
    except (SyntaxError, ValueError, OSError, RecursionError, MemoryError) as exc:
        error = "".join(traceback.format_exception_only(type(exc), exc)).strip()
    print(json.dumps({"file": path, "error": error}), flush=True)
`

// expression is a Jinja expression: shortest match, across lines.
var expression = regexp.MustCompile(`(?s)\{\{.*?\}\}`)

// renderedTemplate is a template with its expressions substituted, written under a temporary
// directory so the compiler can read it.
type renderedTemplate struct {
	rel  string
	path string
}

// render substitutes every {{ … }} in each template with None and writes the result out. A
// template with a statement ({% … %}) cannot be reconstructed this way and stops the run. The
// returned cleanup removes what was written and is always safe to call.
func (g *gate) render(templates []string) (func() error, []renderedTemplate, error) {
	cleanup := func() error { return nil }
	if len(templates) == 0 {
		return cleanup, nil, nil
	}
	texts := make([]string, len(templates))
	for i, rel := range templates {
		data, readErr := os.ReadFile(filepath.Join(g.root, filepath.FromSlash(rel)))
		if readErr != nil {
			return cleanup, nil, fmt.Errorf("cannot read %s: %w", rel, readErr)
		}
		if strings.Contains(string(data), "{%") {
			return cleanup, nil, fmt.Errorf("%s contains Jinja statements ({%% … %%}), so its rendered Python cannot be reconstructed by substitution and this gate cannot check it. Extract the logic into a real .py module the gate can read, or render it in a fixture; do not let it pass unexamined", rel)
		}
		texts[i] = expression.ReplaceAllString(string(data), "None")
	}
	dir, err := os.MkdirTemp(g.env.TempDir, "qa-python-")
	if err != nil {
		return cleanup, nil, fmt.Errorf("cannot make a directory for the rendered templates: %w", err)
	}
	cleanup = func() error {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			return fmt.Errorf("cannot remove %s: %w", dir, rmErr)
		}
		return nil
	}
	rendered := make([]renderedTemplate, 0, len(templates))
	for i, rel := range templates {
		out := filepath.Join(dir, fmt.Sprintf("%03d.py", i))
		if err := os.WriteFile(out, []byte(texts[i]), 0o600); err != nil {
			return cleanup, nil, fmt.Errorf("cannot write the rendered %s: %w", rel, err)
		}
		rendered = append(rendered, renderedTemplate{rel: rel, path: out})
	}
	return cleanup, rendered, nil
}

// compile compiles the files and the rendered templates in one python3 process. The result maps
// each file's relative path (a template's by its template path) to its compile error, "" when it
// compiles. python3 must answer for exactly the paths it was given.
func (g *gate) compile(ctx context.Context, files []string, rendered []renderedTemplate) (map[string]string, error) {
	argv := make([]string, 0, 4+len(files)+len(rendered))
	argv = append(argv, "python3", "-I", "-c", checkerScript)
	display := map[string]string{}
	for _, rel := range files {
		abs := filepath.Join(g.root, filepath.FromSlash(rel))
		argv = append(argv, abs)
		display[abs] = rel
	}
	for _, t := range rendered {
		argv = append(argv, t.path)
		display[t.path] = t.rel
	}
	res, err := g.runTool(ctx, compileTimeout, g.root, argv...)
	if err == nil && res.Code != 0 {
		err = fmt.Errorf("python3 exited %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	if err != nil {
		return nil, fmt.Errorf("the compile step failed: %w", err)
	}
	answers, err := parseAnswers(res.Stdout)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(display))
	for path, msg := range answers {
		rel, known := display[path]
		if !known {
			return nil, fmt.Errorf("python3 answered for %s, which it was not given", path)
		}
		out[rel] = g.scrub(msg, rendered)
	}
	if missing := missingAnswers(display, answers); len(missing) > 0 {
		return nil, fmt.Errorf("python3 did not answer for %d file(s) it was given:\n    %s", len(missing), strings.Join(missing, "\n    "))
	}
	return out, nil
}

// parseAnswers reads the checker's JSON lines; a path answered twice or a line in any other form
// is an error.
func parseAnswers(stdout string) (map[string]string, error) {
	answers := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	scanner.Buffer(nil, 1<<24)
	for scanner.Scan() {
		var a struct {
			File  string `json:"file"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &a); err != nil || a.File == "" {
			return nil, fmt.Errorf("python3 answered in an unexpected form: %q", scanner.Text())
		}
		if _, twice := answers[a.File]; twice {
			return nil, fmt.Errorf("python3 answered twice for %s", a.File)
		}
		answers[a.File] = a.Error
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("cannot read python3's answers: %w", err)
	}
	return answers, nil
}

func missingAnswers(display map[string]string, answers map[string]string) []string {
	var missing []string
	for path, rel := range display {
		if _, ok := answers[path]; !ok {
			missing = append(missing, rel)
		}
	}
	slices.Sort(missing)
	return missing
}

// scrub removes this machine's paths from a compile message: a rendered template is named by its
// template, a file by its path from the root.
func (g *gate) scrub(msg string, rendered []renderedTemplate) string {
	for _, t := range rendered {
		msg = strings.ReplaceAll(msg, t.path, t.rel)
	}
	return strings.ReplaceAll(msg, g.root+string(filepath.Separator), "")
}
