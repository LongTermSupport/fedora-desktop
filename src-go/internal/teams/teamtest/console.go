package teamtest

import (
	"context"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

const (
	gumProgram = "gum"
	fzfProgram = "fzf"
	// defaultColumns is the width the fake terminal reports when a test names none.
	defaultColumns = 80
)

var colourCodes = regexp.MustCompile("\x1b\\[[0-9;]*m")

// Script stands in for fzf and gum: each fzf call takes the next answer of Fzf, "<key>|<choice>"
// where choice is text the chosen row contains, YES, EXIT or ABORT; each gum call takes the
// next of Gum, "YES", "NO" or "CANCEL".
type Script struct {
	T       *testing.T
	Fzf     []string
	Gum     []string
	Rows    [][]string // the rows each fzf call was shown, colour stripped
	FzfArgs [][]string
	GumLog  []string
	// SkipStyle makes "gum style" calls (a box or wrapped text) take no answer; Styled keeps them.
	SkipStyle bool
	Styled    []string
	// Rewrite, when set, changes fzf's answer before it is returned.
	Rewrite func(string) string
	// Headings is the heading of every screen asked (fzf, and gum but its style), in order: the
	// lines above the heading's rule, colour stripped.
	Headings []string
	// Columns is the terminal's width, as tput cols answers it; 0 is defaultColumns.
	Columns int
}

// HeadingOf is the heading ui draws on top of a screen's header or prompt: its lines above the
// rule, colour stripped ("" when it has none).
func HeadingOf(text string) string {
	lines := strings.Split(text, "\n")
	if at := ruleAt(lines); at >= 0 {
		return colourCodes.ReplaceAllString(strings.Join(lines[:at], "\n"), "")
	}
	return ""
}

// AfterHeading is a screen's header or prompt without the heading on top of it: the text after
// the heading's rule, or all of it when it has none.
func AfterHeading(text string) string {
	lines := strings.Split(text, "\n")
	if at := ruleAt(lines); at >= 0 {
		return strings.Join(lines[at+1:], "\n")
	}
	return text
}

// ruleAt is the line that is the heading's rule, or -1.
func ruleAt(lines []string) int {
	for i, l := range lines {
		if l != "" && strings.Trim(l, "─") == "" {
			return i
		}
	}
	return -1
}

// Unheaded is c with the heading cut out of each argument that carries one, so a test about
// what a screen asks reads it without the heading (asserted on its own through Headings).
func Unheaded(c execx.Cmd) execx.Cmd {
	args := make([]string, len(c.Args))
	for i, a := range c.Args {
		if v, ok := strings.CutPrefix(a, "--header="); ok {
			args[i] = "--header=" + AfterHeading(v)
			continue
		}
		args[i] = AfterHeading(a)
	}
	c.Args = args
	return c
}

// screenText is the header or prompt a heading is drawn on: fzf's --header, gum's --header, or
// gum confirm's prompt (its last argument).
func screenText(c execx.Cmd) string {
	for i, a := range c.Args {
		if v, ok := strings.CutPrefix(a, "--header="); ok {
			return v
		}
		if a == "--header" && i+1 < len(c.Args) {
			return c.Args[i+1]
		}
	}
	if len(c.Args) > 0 {
		return c.Args[len(c.Args)-1]
	}
	return ""
}

// HeadingAt fails the test unless screen n (counting fzf and gum questions together) has the
// heading want: the trail, a newline, and the context line when there is one.
func (s *Script) HeadingAt(n int, want string) {
	s.T.Helper()
	if n >= len(s.Headings) {
		s.T.Fatalf("screen %d was never asked; the headings were %q", n, s.Headings)
	}
	if s.Headings[n] != want {
		s.T.Fatalf("screen %d heading:\n got %q\nwant %q\n(all: %q)", n, s.Headings[n], want, s.Headings)
	}
}

// Run answers fzf and gum and fails the test for anything else.
func (s *Script) Run(_ context.Context, c execx.Cmd) (execx.Result, error) {
	switch c.Name {
	case fzfProgram:
		return s.runFzf(c)
	case gumProgram:
		return s.runGum(c)
	case "tput":
		cols := s.Columns
		if cols == 0 {
			cols = defaultColumns
		}
		return execx.Result{Stdout: []byte(strconv.Itoa(cols) + "\n")}, nil
	}
	s.T.Fatalf("scripted runner asked for %s", c.Line())
	return execx.Result{}, nil
}

func (s *Script) runFzf(c execx.Cmd) (execx.Result, error) {
	in, err := io.ReadAll(c.Stdin)
	if err != nil {
		s.T.Fatal(err)
	}
	rows := strings.Split(strings.TrimSuffix(colourCodes.ReplaceAllString(string(in), ""), "\n"), "\n")
	s.Rows = append(s.Rows, rows)
	s.FzfArgs = append(s.FzfArgs, c.Args)
	s.Headings = append(s.Headings, HeadingOf(screenText(c)))
	if len(s.Fzf) == 0 {
		s.T.Fatalf("no fzf answer queued for call %d (rows %q)", len(s.Rows), rows)
	}
	answer := s.Fzf[0]
	s.Fzf = s.Fzf[1:]
	key, choice, found := strings.Cut(answer, "|")
	if !found {
		s.T.Fatalf("fzf answer %q has no |", answer)
	}
	expects := slices.ContainsFunc(c.Args, func(a string) bool { return strings.HasPrefix(a, "--expect") })
	if choice == "ABORT" {
		return execx.Result{ExitCode: 130}, &execx.ExitError{Name: fzfProgram, Code: 130}
	}
	row := s.chosenRow(choice, rows)
	if row == "" {
		s.T.Fatalf("no row contains %q in %q", choice, rows)
	}
	out := row + "\n"
	if s.Rewrite != nil {
		out = s.Rewrite(out)
	}
	if expects {
		out = key + "\n" + out
	}
	return execx.Result{Stdout: []byte(out)}, nil
}

// chosenRow is the row an answer names: the last (EXIT), the first "Yes, " row (YES), or those
// containing each choice; several marks (fzf --multi) are choices joined with "&&", in order.
func (s *Script) chosenRow(choice string, rows []string) string {
	switch choice {
	case "EXIT":
		return rows[len(rows)-1]
	case "YES":
		for _, r := range rows {
			if strings.HasPrefix(r, "Yes, ") {
				return r
			}
		}
		return ""
	}
	var marked []string
	for want := range strings.SplitSeq(choice, "&&") {
		at := slices.IndexFunc(rows, func(r string) bool { return strings.Contains(r, want) })
		if at < 0 {
			s.T.Fatalf("no row contains %q in %q", want, rows)
		}
		marked = append(marked, rows[at])
	}
	return strings.Join(marked, "\n")
}

func (s *Script) runGum(c execx.Cmd) (execx.Result, error) {
	// With SkipStyle, "gum style" only prints (a box, wrapped text): it asks nothing, so it takes
	// no answer. Without it, as before, every gum call takes the next one.
	if s.SkipStyle && len(c.Args) > 0 && c.Args[0] == "style" {
		s.Styled = append(s.Styled, c.Line())
		return execx.Result{}, nil
	}
	s.GumLog = append(s.GumLog, Unheaded(c).Line())
	if len(c.Args) > 0 && c.Args[0] != "style" && c.Args[0] != "spin" {
		s.Headings = append(s.Headings, HeadingOf(screenText(c)))
	}
	if len(s.Gum) == 0 {
		s.T.Fatalf("no gum answer queued for %s", c.Line())
	}
	answer := s.Gum[0]
	s.Gum = s.Gum[1:]
	if choice, ok := strings.CutPrefix(answer, "CHOOSE:"); ok {
		return execx.Result{Stdout: []byte(choice + "\n")}, nil
	}
	switch answer {
	case "YES":
		return execx.Result{}, nil
	case "NO":
		return execx.Result{ExitCode: 1}, &execx.ExitError{Name: gumProgram, Code: 1}
	}
	return execx.Result{ExitCode: 130}, &execx.ExitError{Name: gumProgram, Code: 130}
}

// ExecCall is one replacement of the process by the launcher.
type ExecCall struct {
	Path string
	Argv []string
	Dir  string
}

// Console is the terminal side of a World: scripted fzf and gum, a terminal that answers
// TTY, and a launcher that is recorded, not run.
type Console struct {
	Script *Script
	Execs  []ExecCall
	// TTY is what the terminal gives when it is read for a keypress.
	TTY string
}

// Interactive makes w a plain terminal (no colour) with scripted fzf and gum, every tool on
// PATH, and the launcher recorded in Execs. The working folder is the checkout's root. A world
// with no host_vars file gets an empty one: no team is limited or undescribed.
func Interactive(t *testing.T, w *World) *Console {
	t.Helper()
	c := &Console{Script: &Script{T: t}, TTY: "\n"}
	w.Env["NO_COLOR"] = "1"
	w.Env["PWD"] = w.Root
	w.WriteHostVarsIfAbsent(t, "")
	h := w.Host
	h.IsTerminal = func(any) bool { return true }
	h.Term.IsTerminal = h.IsTerminal
	h.Gum = &ui.Gum{Runner: c.Script, Stdin: h.Stdin, Stderr: w.ErrOut, Plain: true}
	h.Picker = &ui.Picker{Runner: c.Script, Stderr: w.ErrOut, Plain: true}
	h.LookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	h.OpenTTY = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(c.TTY)), nil }
	h.Exec = func(path string, argv []string, dir string) error {
		c.Execs = append(c.Execs, ExecCall{Path: path, Argv: argv, Dir: dir})
		return nil
	}
	h.Launcher = "/x/launcher"
	return c
}

// HeaderOf is the --header fzf call n was given, without the heading on top of it (Headings
// has that).
func (c *Console) HeaderOf(n int) string {
	for _, a := range c.Script.FzfArgs[n] {
		if v, ok := strings.CutPrefix(a, "--header="); ok {
			return AfterHeading(v)
		}
	}
	return ""
}
