package python

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// requireTools proves ruff and python3 run and returns the ruff version.
func (g *gate) requireTools(ctx context.Context) (string, error) {
	res, err := g.runTool(ctx, probeTimeout, g.root, toolRuff, "--version")
	if err != nil {
		return "", fmt.Errorf("ruff not installed (%w): run ./playbooks/imports/play-python.yml\n  (NOT 'dnf install ruff': Fedora's package is unpinned and is removed\n  by that play precisely so it cannot shadow the pinned pipx one.)", err)
	}
	fields := strings.Fields(res.Stdout)
	if res.Code != 0 || len(fields) != 2 || fields[0] != toolRuff {
		return "", fmt.Errorf("ruff --version did not answer as ruff does (exit %d): %q %s", res.Code, strings.TrimSpace(res.Stdout), strings.TrimSpace(res.Stderr))
	}
	res, err = g.runTool(ctx, probeTimeout, g.root, "python3", "--version")
	if err != nil {
		return "", fmt.Errorf("python3 not installed: %w", err)
	}
	if res.Code != 0 {
		return "", fmt.Errorf("python3 --version exited %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	return fields[1], nil
}

// readPin is the ruff version .qa-versions pins: exactly one RUFF= line with a value.
func (g *gate) readPin() (string, error) {
	file := filepath.Join(g.root, pinFile)
	data, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("cannot read %s, the single source of truth for every QA tool version: %w", pinFile, err)
	}
	var pins []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "RUFF="); ok {
			pins = append(pins, strings.TrimSpace(value))
		}
	}
	if len(pins) != 1 || pins[0] == "" {
		return "", fmt.Errorf("%s must hold exactly one RUFF=<version> line with a value (found %d): with no pin the gate cannot say which gate it is", pinFile, len(pins))
	}
	return pins[0], nil
}

// versionMismatch fails the gate, without aborting the run: ruff is not asked to lint, and the
// report is still written, one document in the position qa-all expects it.
func (g *gate) versionMismatch(pin, actual string) (ExitCode, error) {
	g.stderr.WriteString("ruff version mismatch: this gate's verdict is version-dependent.\n")
	g.stderr.WriteString("    expected : " + pin + "  (" + pinFile + ")\n")
	g.stderr.WriteString("    found    : " + actual + "\n\n")
	g.stderr.WriteString("  Match the pin, or bump " + pinFile + " deliberately and triage what the new\n")
	g.stderr.WriteString("  version adds (then rebuild the ccy image).\n")
	msg := fmt.Sprintf("ruff version mismatch: expected %s, found %s — ruff was not run, so no python file was linted this run", pin, actual)
	failure := entry{File: pinFile, Type: typePython, Status: statusFail, Error: msg}
	rep := machineReport{
		Type: typePython, Status: statusFail, Summary: summary{Failed: 1},
		Results: []entry{}, Failures: []entry{failure}, RuffDiagnostics: []map[string]json.RawMessage{},
	}
	if err := g.writeReport(rep); err != nil {
		return ExitCannotRun, err
	}
	g.stdout.WriteString("✗ python: ruff version mismatch: expected " + pin + " (" + pinFile + "), found " + actual + " → " + g.report + " (no files linted)\n")
	return ExitFailed, nil
}

// diagnostic is one ruff finding: the fields the gate reads, beside every field ruff gave.
type diagnostic struct {
	file string
	code string
	row  int
	text string
	raw  map[string]json.RawMessage
}

// ruff proves ruff selected every file it was given, then lints them and returns the
// diagnostics with their paths relative to the root.
func (g *gate) ruff(ctx context.Context, files []string) ([]diagnostic, error) {
	abs := make([]string, 0, len(files))
	rel := make(map[string]string, len(files))
	for _, f := range files {
		p := filepath.Join(g.root, filepath.FromSlash(f))
		abs = append(abs, p)
		rel[p] = f
	}
	if err := g.proveSelected(ctx, abs, rel); err != nil {
		return nil, err
	}
	argv := append(g.ruffBase("--no-cache", "--ignore-noqa", "--output-format", "json"), abs...)
	res, err := g.runRuff(ctx, argv)
	if err != nil {
		return nil, err
	}
	var raws []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(res.Stdout), &raws); err != nil {
		return nil, fmt.Errorf("ruff did not print a JSON array of diagnostics: %w", err)
	}
	out := make([]diagnostic, 0, len(raws))
	for _, raw := range raws {
		d, err := readDiagnostic(raw, rel)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// ruffBase is the start of every ruff command: the root's ruff.toml named explicitly, which turns
// off ruff's discovery of a closer pyproject.toml or ruff.toml that would swap the ruleset for a
// subtree while --show-files still listed its files.
func (g *gate) ruffBase(flags ...string) []string {
	return append([]string{toolRuff, "check", "--config", filepath.Join(g.root, "ruff.toml")}, flags...)
}

// runRuff runs ruff; status 0 and 1 are data, anything else is a failure to run.
func (g *gate) runRuff(ctx context.Context, argv []string) (proc.Result, error) {
	res, err := g.runTool(ctx, ruffTimeout, g.root, argv...)
	if err == nil && res.Code != 0 && res.Code != 1 {
		err = fmt.Errorf("ruff invocation failed (status %d):\n%s", res.Code, strings.TrimSpace(res.Stderr))
	}
	return res, err
}

// proveSelected is the independent proof that ruff read every Python file: it lists the files it
// would check among those given, and each must be listed.
func (g *gate) proveSelected(ctx context.Context, abs []string, rel map[string]string) error {
	argv := append(g.ruffBase("--show-files", "--no-cache"), abs...)
	res, err := g.runRuff(ctx, argv)
	if err != nil {
		return err
	}
	listed := map[string]bool{}
	for line := range strings.SplitSeq(res.Stdout, "\n") {
		listed[strings.TrimSpace(line)] = true
	}
	var missed []string
	for _, p := range abs {
		if !listed[p] {
			missed = append(missed, rel[p])
		}
	}
	if len(missed) > 0 {
		slices.Sort(missed)
		return fmt.Errorf("ruff did not select %d of the Python file(s) it was given:\n    %s\n  A pass here would vouch for code ruff never read; check ruff.toml's exclude rules", len(missed), strings.Join(missed, "\n    "))
	}
	return nil
}

// readDiagnostic reads one diagnostic and makes its filename relative to the root; a file ruff
// reports on that it was not given means ruff and the gate disagree on what was checked.
func readDiagnostic(raw map[string]json.RawMessage, rel map[string]string) (diagnostic, error) {
	var d diagnostic
	var filename string
	if err := json.Unmarshal(raw["filename"], &filename); err != nil {
		return d, errors.New("a ruff diagnostic has no filename")
	}
	relPath, given := rel[filename]
	if !given {
		return d, fmt.Errorf("ruff reported on %s, which it was not given", filename)
	}
	var location struct {
		Row int `json:"row"`
	}
	if err := json.Unmarshal(raw["location"], &location); err != nil {
		return d, fmt.Errorf("a ruff diagnostic on %s has no location: %w", relPath, err)
	}
	var code *string
	if err := json.Unmarshal(raw["code"], &code); err != nil {
		return d, fmt.Errorf("a ruff diagnostic on %s has a code that is not a string: %w", relPath, err)
	}
	if err := json.Unmarshal(raw["message"], &d.text); err != nil {
		return d, fmt.Errorf("a ruff diagnostic on %s has no message: %w", relPath, err)
	}
	d.code = "syntax-error"
	if code != nil {
		d.code = *code
	}
	name, err := json.Marshal(relPath)
	if err != nil {
		return d, fmt.Errorf("cannot encode %s: %w", relPath, err)
	}
	raw["filename"] = name
	d.file, d.row, d.raw = relPath, location.Row, raw
	return d, nil
}
