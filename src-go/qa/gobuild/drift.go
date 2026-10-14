package gobuild

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// TablePath is the table of deployed commands, relative to the checkout: `command<TAB>installed
// path<TAB>play` per row (`~/` is the login user's home). A new deployed command takes a row
// there and in scripts/go-binary-suites.tsv; the tests of this package fail while either is
// missing.
const TablePath = "scripts/go-binary-installs.tsv"

// Exit codes of the commands: ExitFailure is a digest that could not be computed, ExitDrift a
// deployed command older than the checkout, ExitCannotRun a check that did not run at all (a
// bad table, a command the source does not have, a bad command line), which is not drift.
const (
	ExitOK        = 0
	ExitFailure   = 1
	ExitDrift     = 1
	ExitCannotRun = 2
)

// answerTimeout bounds one installed command's `--version`.
const answerTimeout = 30 * time.Second

const tableColumns = 3

// Row is one deployed command of the table.
type Row struct {
	Installed string
	Play      string
}

// ReadTable reads the table of deployed commands.
func ReadTable(path string) (map[string]Row, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	rows := map[string]Row{}
	number := 0
	for line := range strings.Lines(string(data)) {
		number++
		line = strings.TrimRight(line, "\n")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != tableColumns || fields[0] == "" || fields[1] == "" || fields[2] == "" {
			return nil, fmt.Errorf("%s line %d: want command<TAB>installed path<TAB>play, got %q", filepath.Base(path), number, line)
		}
		rows[fields[0]] = Row{Installed: fields[1], Play: fields[2]}
	}
	return rows, nil
}

// DriftOptions say what to check.
type DriftOptions struct {
	// Source is the fedora-desktop checkout.
	Source string
	// Home is what `~/` means in the table.
	Home string
}

// installedDigest is what the installed command prints for --version; ok is false when it
// cannot answer (it would not start, failed, or took too long).
func installedDigest(ctx context.Context, runner proc.Runner, program string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, answerTimeout)
	defer cancel()
	res, err := runner.Run(ctx, proc.Cmd{Argv: []string{program, "--version"}})
	if err != nil || res.Code != 0 || res.TimedOut {
		return "", false
	}
	return strings.TrimSpace(res.Stdout), true
}

func expand(installed, home string) string {
	if head, rest, found := strings.Cut(installed, "/"); found && head == "~" {
		return strings.TrimRight(home, "/") + "/" + rest
	}
	return installed
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func short(digest string) string {
	if len(digest) > 12 {
		return digest[:12]
	}
	return digest
}

// Drift runs the installed commands of the table and compares each answer with the digest of
// the checkout. A command is checked only when it is installed, so a host that never ran a play
// is not nagged about it. Findings go to stderr, the summary of a clean run to stdout.
func Drift(ctx context.Context, runner proc.Runner, opts DriftOptions, stdout, stderr io.Writer) int {
	out, errs := &sink{w: stdout}, &sink{w: stderr}
	return settle(drift(ctx, runner, opts, out, errs), out, errs)
}

// expectedDigests are the checkout's digests of the commands of the table.
func expectedDigests(source string, rows map[string]Row) (map[string]string, error) {
	expected := make(map[string]string, len(rows))
	for command := range rows {
		digest, err := SourceDigest(source, command)
		if err != nil {
			return nil, err
		}
		expected[command] = digest
	}
	return expected, nil
}

// finding is the report of one installed command that differs from the checkout.
func finding(command string, row Row, program, answer, want string, answered bool) string {
	why := "does not report a source digest (built before it did, or not runnable)"
	if answered {
		why = fmt.Sprintf("reports %s, the checkout is %s", short(answer), short(want))
	}
	return fmt.Sprintf("  %s: %s\n    installed: %s\n    rebuild it with:\n    ansible-playbook %s\n", command, why, program, row.Play)
}

func drift(ctx context.Context, runner proc.Runner, opts DriftOptions, out, errs *sink) int {
	rows, err := ReadTable(filepath.Join(opts.Source, TablePath))
	if err != nil {
		errs.printf("gobuild: %v\n", err)
		return ExitCannotRun
	}
	expected, err := expectedDigests(opts.Source, rows)
	if err != nil {
		errs.printf("gobuild: %v\n", err)
		return ExitCannotRun
	}
	var drifted []string
	checked, missing := 0, 0
	for _, command := range slices.Sorted(maps.Keys(rows)) {
		program := expand(rows[command].Installed, opts.Home)
		if !isFile(program) {
			missing++
			continue
		}
		checked++
		answer, answered := installedDigest(ctx, runner, program)
		if !answered || answer != expected[command] {
			drifted = append(drifted, finding(command, rows[command], program, answer, expected[command], answered))
		}
	}
	if len(drifted) > 0 {
		errs.printf("✗ deployed-drift: Go commands built from older source than the checkout\n\n%s\n", strings.Join(drifted, "\n"))
		errs.printf("✗ deployed-drift: %d of %d deployed Go command(s) differ from the checkout\n", len(drifted), checked)
		return ExitDrift
	}
	out.printf("✓ deployed-drift: %d deployed Go command(s) match the checkout; %d not installed on this host\n", checked, missing)
	return ExitOK
}
