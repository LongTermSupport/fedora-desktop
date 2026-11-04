// Package histclean is the histclean command: it removes every shell-history entry that
// contains a pattern, from both files the history setup keeps (files/etc/profile.d/
// zz_lts-fedora-desktop.bash and files/home/bashrc-includes/history-search.bash):
//
//   - the history file, where an entry is its "#<epoch>" line and every command line after it
//     (lithist stores a multi-line command literally); lines before the first timestamp are one
//     entry each;
//   - the Ctrl+R context file, where an entry is one NUL-terminated record,
//     "epoch TAB status TAB dir TAB command".
//
// Only the command is searched, never the directory a record was filed under.
//
// Expectations, each pinned by a test in histclean_test.go:
//
//   - H1 the pattern is a fixed substring (-e: a Go regexp); the matches of both files are
//     removed together, after one confirmation.
//   - H2 a multi-line entry goes whole, with its timestamp line.
//   - H3 the entries are shown numbered, on stderr, before "Remove N entries? [y/N]".
//   - H4 no, Enter and the end of input change nothing.
//   - H5 a typo is asked again; three bad answers give up with nothing removed.
//   - H6 no match: said, exit 0, no question.
//   - H7 without a terminal and without --yes nothing is removed: exit 1.
//   - H8 --yes removes without asking.
//   - H9 the regexp is compiled before anything is read; a bad one is a usage error.
//   - H10 usage errors are exit 64; --help prints to stdout.
//   - H11 a file that does not exist is skipped; both missing is an error.
//   - H12 each file is replaced whole by rename of a 0600 temporary beside it, fsynced.
//   - H13 text before the first timestamp and a missing final newline are kept as they were.
//   - H14 the files are read again just before the rewrite and only the entries that were
//     shown are removed from the fresh content, so a command another shell appended while
//     the question was open is kept.
package histclean

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// Help is the text of --help.
const Help = `histclean - remove matching entries from the shell history

Usage: histclean [--yes] [-e] [--] PATTERN

The pattern is a fixed substring, not a regular expression: histclean 'a.b' matches the text
a.b only. With -e it is a Go (RE2) regular expression instead.

Every entry whose command contains PATTERN is looked up in the history file and in the
Ctrl+R context file, shown numbered, and removed after one confirmation. A history entry
is its timestamp line and all its command lines; the directory a command ran in is not
searched.

  -e         PATTERN is a Go regular expression
  -y, --yes  remove without asking (required when stdin is not a terminal)
  -h, --help this text

A terminal that is already open still holds the removed lines in memory: its up-arrow
shows them until it is closed, or until history -d <number> removes them there.
`

const (
	historyTimestampMin = 11 // "#" and at least ten digits, as bash-history-rank reads one
	maxShown            = 1000
)

// App is one histclean run. Stdin is only read for the confirmation; Interactive says it is a
// terminal.
type App struct {
	HistoryPath, ContextPath string
	Stdin                    io.Reader
	Interactive              bool
	Stdout, Stderr           io.Writer
}

type options struct {
	match func(string) bool
	label string
	yes   bool
}

// chunk is one entry: the exact bytes it occupies in its file and the command it holds.
type chunk struct {
	raw     string
	command string
}

type fileScan struct {
	name, path string
	exists     bool
	hits       []chunk
	parse      func(string) []chunk
}

// Run executes histclean with the arguments after the program name.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	var p term.Printer
	opts, code, done := a.parseArgs(args, &p)
	if done {
		return finish(&p, code)
	}
	files := []*fileScan{
		{name: "history", path: a.HistoryPath, parse: parseHistory},
		{name: "context", path: a.ContextPath, parse: parseContext},
	}
	total, code := a.scan(files, opts, &p)
	if code != term.ExitOK || total == 0 {
		return finish(&p, code)
	}
	if proceed, stop := a.confirm(ctx, total, opts, &p); !proceed {
		return finish(&p, stop)
	}
	removed := 0
	for _, f := range files {
		n, err := rewrite(f, opts)
		if err != nil {
			p.Print(a.Stderr, fmt.Sprintf("histclean: %v\n", err))
			return finish(&p, term.ExitFailure)
		}
		removed += n
		if f.exists {
			p.Print(a.Stderr, fmt.Sprintf("Removed %d from the %s file.\n", n, f.name))
		}
	}
	p.Print(a.Stderr, fmt.Sprintf("Removed %d entries in all. A terminal that is already open still holds them in memory:\n"+
		"its up-arrow shows them until it is closed, or until `history -d <number>` removes them there.\n", removed))
	return finish(&p, term.ExitOK)
}

func finish(p *term.Printer, code term.ExitCode) term.ExitCode {
	if code == term.ExitOK && p.Err() != nil {
		return term.ExitFailure
	}
	return code
}

func (a App) usage(p *term.Printer, msg string) {
	p.Print(a.Stderr, fmt.Sprintf("histclean: %s\nUsage: histclean [--yes] [-e] [--] PATTERN   (histclean --help)\n", msg))
}

// parseArgs reads the command line. done is true when the run ends here, with code.
func (a App) parseArgs(args []string, p *term.Printer) (options, term.ExitCode, bool) {
	var opts options
	var pattern []string
	regex := false
	rest := false
	for _, arg := range args {
		switch {
		case rest:
			pattern = append(pattern, arg)
		case arg == "--":
			rest = true
		case arg == "-h" || arg == "--help":
			p.Print(a.Stdout, Help)
			return opts, term.ExitOK, true
		case arg == "-e":
			regex = true
		case arg == "-y" || arg == "--yes":
			opts.yes = true
		case strings.HasPrefix(arg, "-") && arg != "-":
			a.usage(p, "unknown option "+arg)
			return opts, term.ExitUsage, true
		default:
			pattern = append(pattern, arg)
		}
	}
	if len(pattern) != 1 || pattern[0] == "" {
		a.usage(p, "exactly one non-empty PATTERN is required")
		return opts, term.ExitUsage, true
	}
	opts.label = pattern[0]
	match, err := matcher(pattern[0], regex)
	if err != nil {
		a.usage(p, err.Error())
		return opts, term.ExitUsage, true
	}
	opts.match = match
	return opts, term.ExitOK, false
}

// matcher is the test an entry's command must pass: a fixed substring, or a Go regexp.
func matcher(pattern string, regex bool) (func(string) bool, error) {
	if !regex {
		return func(s string) bool { return strings.Contains(s, pattern) }, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("bad regular expression: %w", err)
	}
	return re.MatchString, nil
}

func readIfPresent(path string) (string, bool, error) {
	b, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("cannot read %s: %w", path, err)
	}
	return string(b), true, nil
}

// scan finds the matches of every file and shows them numbered. It returns how many.
func (a App) scan(files []*fileScan, opts options, p *term.Printer) (int, term.ExitCode) {
	total, present := 0, 0
	for _, f := range files {
		text, exists, err := readIfPresent(f.path)
		if err != nil {
			p.Print(a.Stderr, fmt.Sprintf("histclean: %v\n", err))
			return 0, term.ExitFailure
		}
		f.exists = exists
		if !exists {
			continue
		}
		present++
		for _, c := range f.parse(text) {
			if opts.match(c.command) {
				f.hits = append(f.hits, c)
			}
		}
		total += len(f.hits)
	}
	if present == 0 {
		p.Print(a.Stderr, "histclean: neither the history file nor the context file exists\n")
		return 0, term.ExitFailure
	}
	if total == 0 {
		p.Print(a.Stderr, fmt.Sprintf("No entries contain %q.\n", opts.label))
		return 0, term.ExitOK
	}
	n := 0
	for _, f := range files {
		for _, c := range f.hits {
			n++
			if n > maxShown {
				break
			}
			p.Print(a.Stderr, fmt.Sprintf("%4d. [%s] %s\n", n, f.name, strings.ReplaceAll(c.command, "\n", "\n          ")))
		}
	}
	if total > maxShown {
		p.Print(a.Stderr, fmt.Sprintf("      ... and %d more\n", total-maxShown))
	}
	return total, term.ExitOK
}

// confirm asks once. --yes skips the question; a stdin that is not a terminal without it is
// refused, so nothing is ever removed unconfirmed.
func (a App) confirm(ctx context.Context, total int, opts options, p *term.Printer) (bool, term.ExitCode) {
	if opts.yes {
		return true, term.ExitOK
	}
	if !a.Interactive {
		p.Print(a.Stderr, "histclean: stdin is not a terminal, so the removal cannot be confirmed; nothing was removed (use --yes to remove without asking)\n")
		return false, term.ExitFailure
	}
	lines := &ui.Lines{In: a.Stdin, Out: a.Stderr}
	ok, err := lines.YesNo(ctx, ui.Here("histclean"), fmt.Sprintf("Remove %d entries?", total), false)
	var gaveUp *ui.GaveUpError
	switch {
	case errors.Is(err, ui.ErrCancelled):
		p.Print(a.Stderr, "\nhistclean: cancelled; nothing was removed\n")
		return false, term.ExitCancelled
	case errors.As(err, &gaveUp):
		p.Print(a.Stderr, "histclean: no valid answer; nothing was removed\n")
		return false, term.ExitFailure
	case err != nil:
		p.Print(a.Stderr, fmt.Sprintf("histclean: %v\n", err))
		return false, term.ExitFailure
	case !ok:
		p.Print(a.Stderr, "Nothing was removed.\n")
		return false, term.ExitOK
	}
	return true, term.ExitOK
}

// rewrite reads the file again and replaces it without the entries that were shown, so what
// another shell appended meanwhile is kept.
func rewrite(f *fileScan, opts options) (int, error) {
	if !f.exists || len(f.hits) == 0 {
		return 0, nil
	}
	shown := map[string]bool{}
	for _, c := range f.hits {
		shown[c.raw] = true
	}
	text, exists, err := readIfPresent(f.path)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, fmt.Errorf("%s disappeared while histclean was waiting", f.path)
	}
	var out strings.Builder
	removed := 0
	for _, c := range f.parse(text) {
		if shown[c.raw] && opts.match(c.command) {
			removed++
			continue
		}
		out.WriteString(c.raw)
	}
	if removed == 0 {
		return 0, nil
	}
	return removed, replaceFile(f.path, []byte(out.String()))
}

// replaceFile writes data to a 0600 temporary in path's directory, syncs it, renames it over
// path and syncs the directory, so a reader sees the old file or the new one, never a part.
func replaceFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".histclean.")
	if err != nil {
		return fmt.Errorf("cannot create a temporary file beside %s: %w", path, err)
	}
	fail := func(step string, err error) error {
		return errors.Join(fmt.Errorf("%s %s: %w", step, path, err), tmp.Close(), os.Remove(tmp.Name()))
	}
	if cerr := tmp.Chmod(0o600); cerr != nil {
		return fail("cannot set the mode of the replacement for", cerr)
	}
	if _, werr := tmp.Write(data); werr != nil {
		return fail("cannot write the replacement for", werr)
	}
	if serr := tmp.Sync(); serr != nil {
		return fail("cannot sync the replacement for", serr)
	}
	if clerr := tmp.Close(); clerr != nil {
		return errors.Join(fmt.Errorf("cannot close the replacement for %s: %w", path, clerr), os.Remove(tmp.Name()))
	}
	if rerr := os.Rename(tmp.Name(), path); rerr != nil {
		return errors.Join(fmt.Errorf("cannot move the replacement into place at %s: %w", path, rerr), os.Remove(tmp.Name()))
	}
	d, oerr := os.Open(filepath.Clean(dir))
	if oerr != nil {
		return fmt.Errorf("cannot open %s to sync it: %w", dir, oerr)
	}
	return errors.Join(d.Sync(), d.Close())
}

func isTimestamp(line string) bool {
	if len(line) < historyTimestampMin || line[0] != '#' {
		return false
	}
	for _, r := range line[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseHistory splits a history file into entries whose raw texts concatenate to the file.
func parseHistory(text string) []chunk {
	var out []chunk
	stamped := false
	var raw, cmd strings.Builder
	flush := func() {
		if raw.Len() > 0 {
			out = append(out, chunk{raw: raw.String(), command: cmd.String()})
		}
		raw.Reset()
		cmd.Reset()
	}
	for len(text) > 0 {
		line, rest, hasNL := strings.Cut(text, "\n")
		whole := line
		if hasNL {
			whole += "\n"
		}
		text = rest
		switch {
		case isTimestamp(line):
			flush()
			stamped = true
			raw.WriteString(whole)
		case stamped:
			if cmd.Len() > 0 {
				cmd.WriteString("\n")
			}
			cmd.WriteString(line)
			raw.WriteString(whole)
		default:
			out = append(out, chunk{raw: whole, command: line})
		}
	}
	flush()
	return out
}

// parseContext splits the recorder's file into NUL-terminated records; the command is the
// fourth tab-separated field and everything after it.
func parseContext(text string) []chunk {
	var out []chunk
	for len(text) > 0 {
		rec, rest, found := strings.Cut(text, "\x00")
		raw := rec
		if found {
			raw += "\x00"
		}
		text = rest
		fields := bytes.SplitN([]byte(rec), []byte("\t"), 4)
		command := rec
		if len(fields) == 4 {
			command = string(fields[3])
		}
		out = append(out, chunk{raw: raw, command: command})
	}
	return out
}
