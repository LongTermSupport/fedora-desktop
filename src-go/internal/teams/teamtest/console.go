package teamtest

import (
	"context"
	"io"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

const gumProgram = "gum"

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
	// Rewrite, when set, changes fzf's answer before it is returned.
	Rewrite func(string) string
}

// Run answers fzf and gum and fails the test for anything else.
func (s *Script) Run(_ context.Context, c execx.Cmd) (execx.Result, error) {
	switch c.Name {
	case "fzf":
		return s.runFzf(c)
	case gumProgram:
		return s.runGum(c)
	case "tput":
		return execx.Result{Stdout: []byte("80\n")}, nil
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
	if len(s.Fzf) == 0 {
		s.T.Fatalf("no fzf answer queued for call %d (rows %q)", len(s.Rows), rows)
	}
	answer := s.Fzf[0]
	s.Fzf = s.Fzf[1:]
	key, choice, found := strings.Cut(answer, "|")
	if !found {
		s.T.Fatalf("fzf answer %q has no |", answer)
	}
	row := ""
	switch choice {
	case "ABORT":
		return execx.Result{ExitCode: 130}, &execx.ExitError{Name: "fzf", Code: 130}
	case "EXIT":
		row = rows[len(rows)-1]
	case "YES":
		for _, r := range rows {
			if strings.HasPrefix(r, "Yes, ") {
				row = r
				break
			}
		}
	default:
		// Several marks (fzf --multi) are choices joined with "&&", answered in the order given.
		var marked []string
		for want := range strings.SplitSeq(choice, "&&") {
			at := slices.IndexFunc(rows, func(r string) bool { return strings.Contains(r, want) })
			if at < 0 {
				s.T.Fatalf("no row contains %q in %q", want, rows)
			}
			marked = append(marked, rows[at])
		}
		row = strings.Join(marked, "\n")
	}
	if row == "" {
		s.T.Fatalf("no row contains %q in %q", choice, rows)
	}
	out := row + "\n"
	if s.Rewrite != nil {
		out = s.Rewrite(out)
	}
	if slices.ContainsFunc(c.Args, func(a string) bool { return strings.HasPrefix(a, "--expect") }) {
		out = key + "\n" + out
	}
	return execx.Result{Stdout: []byte(out)}, nil
}

func (s *Script) runGum(c execx.Cmd) (execx.Result, error) {
	s.GumLog = append(s.GumLog, c.Line())
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
// PATH, and the launcher recorded in Execs. The working folder is the checkout's root.
func Interactive(t *testing.T, w *World) *Console {
	t.Helper()
	c := &Console{Script: &Script{T: t}, TTY: "\n"}
	w.Env["NO_COLOR"] = "1"
	w.Env["PWD"] = w.Root
	w.OnDeclare("limited", "")
	w.OnDeclare("undescribed", "")
	h := w.Host
	h.IsTerminal = func(any) bool { return true }
	h.Term.IsTerminal = h.IsTerminal
	h.Gum = &ui.Gum{Runner: c.Script, Stdin: h.Stdin, Stderr: w.ErrOut}
	h.Picker = &ui.Picker{Runner: c.Script, Stderr: w.ErrOut}
	h.LookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	h.OpenTTY = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(c.TTY)), nil }
	h.Exec = func(path string, argv []string, dir string) error {
		c.Execs = append(c.Execs, ExecCall{Path: path, Argv: argv, Dir: dir})
		return nil
	}
	h.Launcher = "/x/launcher"
	return c
}

// HeaderOf is the --header fzf call n was given.
func (c *Console) HeaderOf(n int) string {
	for _, a := range c.Script.FzfArgs[n] {
		if v, ok := strings.CutPrefix(a, "--header="); ok {
			return v
		}
	}
	return ""
}
