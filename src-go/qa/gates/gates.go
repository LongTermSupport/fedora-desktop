// Package gates is the list of QA gates qa-all runs, as data, and the readers that turn a gate's
// output into its stage line. scripts/qa-gates.tsv is the one place that says what runs: the
// qa-all runner executes its rows, qa-changed picks suites from them, and the QA.md inventory
// gate checks the documentation against them, so none of the three scans another script's text
// for a fact.
package gates

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Kind is how a gate is run and reported.
type Kind string

// The four kinds. A gate is run, captured, and read for a summary; a stage writes a JSON report
// that is merged; the drift gate's own output is its stage line; the helpers gate hands its
// counts over in a file.
const (
	KindGate    Kind = "gate"
	KindStage   Kind = "stage"
	KindDrift   Kind = "drift"
	KindHelpers Kind = "helpers"
)

// Reader reads a gate's captured output for the summary on its stage line: the case count when
// Detail is nil, else the text the pattern matches.
type Reader struct {
	Detail *regexp.Regexp
}

// Gate is one row of the table.
type Gate struct {
	Name string
	Kind Kind
	// Argv is the command as written: space-separated words relative to the repository root.
	Argv []string
	// Text is the reason on a failed gate's stage line; for a stage, the message of its exit 2.
	Text    string
	Readers []Reader
	Line    int
}

// Command is the argv to run with the repository at root: a scripts/*.bash script runs through
// bash, so a lost executable bit is not a 126, and the words after it are its arguments;
// anything else runs as written.
func (g Gate) Command(root string) []string {
	if _, ok := g.Script(); ok {
		return append([]string{"bash", filepath.Join(root, g.Argv[0])}, g.Argv[1:]...)
	}
	return slices.Clone(g.Argv)
}

// Script is the file name of the gate's script (test-x.bash) when its command is one under
// scripts/, with no leading arguments before it.
func (g Gate) Script() (string, bool) {
	name, ok := strings.CutPrefix(g.Argv[0], "scripts/")
	if !ok || !strings.HasSuffix(name, ".bash") || strings.Contains(name, "/") {
		return "", false
	}
	return name, true
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

const columns = 4

// Load reads and strictly validates the table. Every refusal names the line. The stage line
// verdicts.py parses is "<name>: ...", so a name outside [a-z0-9-] would be invisible to it.
func Load(path string) ([]Gate, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read the gate table: %w", err)
	}
	var out []Gate
	seen := map[string]bool{}
	for n, line := range strings.Split(string(body), "\n") {
		n++
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		g, err := parseRow(line, n)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		if seen[g.Name] {
			return nil, fmt.Errorf("%s:%d: duplicate gate %q", path, n, g.Name)
		}
		seen[g.Name] = true
		out = append(out, g)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s lists no gates", path)
	}
	return out, nil
}

func parseRow(line string, n int) (Gate, error) {
	fields := strings.Split(line, "\t")
	if len(fields) < columns {
		return Gate{}, errors.New("want <kind><TAB><name><TAB><command><TAB><text>[<TAB><reader>...]")
	}
	g := Gate{Kind: Kind(fields[0]), Name: fields[1], Argv: strings.Fields(fields[2]), Text: strings.TrimSpace(fields[3]), Line: n}
	switch g.Kind {
	case KindGate, KindStage, KindDrift, KindHelpers:
	default:
		return Gate{}, fmt.Errorf("unknown kind %q", fields[0])
	}
	if !nameRe.MatchString(g.Name) {
		return Gate{}, fmt.Errorf("name %q must match %s", g.Name, nameRe)
	}
	if len(g.Argv) == 0 {
		return Gate{}, errors.New("command is empty")
	}
	if g.Text == "" {
		return Gate{}, errors.New("text is empty")
	}
	readers, err := parseReaders(g, fields[columns:])
	if err != nil {
		return Gate{}, err
	}
	g.Readers = readers
	return g, nil
}

// parseReaders validates the reader columns for the gate's kind: a gate needs at least one,
// every other kind none; the kinds that run a script must name one.
func parseReaders(g Gate, fields []string) ([]Reader, error) {
	if _, ok := g.Script(); !ok && g.Kind != KindGate {
		return nil, fmt.Errorf("a %s gate must run a script under scripts/, not %q", g.Kind, strings.Join(g.Argv, " "))
	}
	if g.Kind != KindGate {
		if len(fields) > 0 {
			return nil, fmt.Errorf("a %s gate takes no reader", g.Kind)
		}
		return nil, nil
	}
	if len(fields) == 0 {
		return nil, errors.New("a gate needs a reader: count or detail:<ERE>")
	}
	var readers []Reader
	for _, field := range fields {
		r, err := parseReader(field)
		if err != nil {
			return nil, err
		}
		readers = append(readers, r)
	}
	return readers, nil
}

func parseReader(field string) (Reader, error) {
	if field == "count" {
		return Reader{}, nil
	}
	pattern, ok := strings.CutPrefix(field, "detail:")
	if !ok {
		return Reader{}, fmt.Errorf("unknown reader %q: want count or detail:<ERE>", field)
	}
	if pattern == "" {
		return Reader{}, errors.New("the detail pattern is empty")
	}
	re, err := regexp.CompilePOSIX(pattern)
	if err != nil {
		return Reader{}, fmt.Errorf("the detail pattern %q does not compile: %w", pattern, err)
	}
	return Reader{Detail: re}, nil
}

// Population is the scripts that may be picked as suites: the script of every row, in file
// order, once each, minus the per-file gates (their names without .bash).
func Population(gs []Gate, perFileGates []string) []string {
	var out []string
	for _, g := range gs {
		name, ok := g.Script()
		if !ok || slices.Contains(out, name) || slices.Contains(perFileGates, strings.TrimSuffix(name, ".bash")) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// CheckScripts is an error naming the first gate whose script is not a regular file under root.
// A gate that is gone would otherwise be a command that fails at run time with the shell's
// words, or, for qa-changed, a suite that is never picked.
func CheckScripts(root string, gs []Gate) error {
	for _, g := range gs {
		if _, ok := g.Script(); !ok {
			continue
		}
		info, err := os.Stat(filepath.Join(root, g.Argv[0]))
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("gate %q (line %d) runs %s, which is not a file under %s", g.Name, g.Line, g.Argv[0], root)
		}
	}
	return nil
}
