package bash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// diagnostic is one shellcheck finding: the fields the gate reads, beside every field shellcheck
// gave.
type diagnostic struct {
	file   string
	level  string
	code   int
	line   int
	column int
	text   string
	raw    map[string]json.RawMessage
}

// gating is whether the finding fails the gate: error and warning do, info and style are advisory.
func (d diagnostic) gating() bool { return d.level == "error" || d.level == "warning" }

// shellcheckArgs is the invocation. --norc and the empty SHELLCHECK_OPTS (see lint) leave the rules
// to shellcheck's defaults whatever a .shellcheckrc or the environment holds; --external-sources
// follows source directives, as qa-changed's shellcheck check does.
func shellcheckArgs(files []string) []string {
	return append([]string{toolShellcheck, "--norc", "--external-sources", "--format", "json", "--"}, files...)
}

// shellcheck lints every file in one process and returns its findings, paths relative to the root.
func (g *gate) shellcheck(ctx context.Context, files []string) ([]diagnostic, error) {
	res, err := g.runTool(ctx, lintTimeout, proc.Cmd{Argv: shellcheckArgs(files), Env: []string{"SHELLCHECK_OPTS="}})
	if err != nil {
		return nil, err
	}
	if res.Code != 0 && res.Code != 1 {
		return nil, fmt.Errorf("shellcheck invocation failed (status %d):\n%s", res.Code, strings.TrimSpace(res.Stderr))
	}
	if !strings.HasPrefix(strings.TrimSpace(res.Stdout), "[") {
		return nil, errors.New("shellcheck did not print a JSON array of findings")
	}
	var elems []json.RawMessage
	if err := json.Unmarshal([]byte(res.Stdout), &elems); err != nil {
		return nil, fmt.Errorf("shellcheck did not print a JSON array of findings: %w", err)
	}
	out := make([]diagnostic, 0, len(elems))
	for _, elem := range elems {
		d, err := g.readDiagnostic(elem, files)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// readDiagnostic reads one finding and makes its file relative to the root. A finding on a file that
// was not given means shellcheck and the gate disagree on what was checked.
func (g *gate) readDiagnostic(elem json.RawMessage, files []string) (diagnostic, error) {
	var d diagnostic
	var fields struct {
		File    *string `json:"file"`
		Level   *string `json:"level"`
		Code    *int    `json:"code"`
		Line    *int    `json:"line"`
		Column  *int    `json:"column"`
		Message *string `json:"message"`
	}
	if err := json.Unmarshal(elem, &fields); err != nil {
		return d, fmt.Errorf("a shellcheck finding is not readable: %w", err)
	}
	if fields.File == nil || fields.Level == nil || fields.Code == nil || fields.Line == nil || fields.Column == nil || fields.Message == nil {
		return d, fmt.Errorf("a shellcheck finding lacks a field the gate reads: %s", elem)
	}
	d.file = g.relative(*fields.File)
	if !slices.Contains(files, d.file) {
		return d, fmt.Errorf("shellcheck reported on %s, which it was not given", *fields.File)
	}
	if !slices.Contains([]string{"error", "warning", "info", "style"}, *fields.Level) {
		return d, fmt.Errorf("shellcheck reported level '%s' on %s:%d, which the gate does not know", *fields.Level, d.file, *fields.Line)
	}
	d.level, d.code, d.line, d.column, d.text = *fields.Level, *fields.Code, *fields.Line, *fields.Column, *fields.Message
	if err := json.Unmarshal(elem, &d.raw); err != nil {
		return d, fmt.Errorf("a shellcheck finding is not an object: %w", err)
	}
	name, err := json.Marshal(d.file)
	if err != nil {
		return d, fmt.Errorf("cannot encode %s: %w", d.file, err)
	}
	d.raw["file"] = name
	return d, nil
}

// relative is the path under the root when it is under it, cleaned.
func (g *gate) relative(path string) string {
	if filepath.IsAbs(path) {
		if rel, err := filepath.Rel(g.root, path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return filepath.Clean(path)
}
