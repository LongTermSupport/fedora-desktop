package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// ExitRow is the last row of every picker, so leaving is a visible choice, not only a key.
const ExitRow = "Exit"

// pickerColours: the terminal's own colours (16 base, -1 its default) with the accent for
// what is active; the same accent as the gum questions.
const pickerColours = "16,fg:-1,bg:-1,fg+:-1:bold,bg+:-1,hl:3,hl+:3:bold,gutter:-1,pointer:3," +
	"marker:3,prompt:3,spinner:3,info:-1,header:-1,border:-1,label:3:bold"

// errNoRow is fzf printing a key line with no row under it.
var errNoRow = errors.New("fzf: no row after the key line")

// PickSpec is one picker: a bordered box titled on its frame, the header (key legend and
// context) above the rows, and the keys besides Enter that choose a row.
type PickSpec struct {
	Title      string
	Header     string
	Keys       []string
	Rows       []string
	CursorLast bool // start on the Exit row: the safe default for a yes/no
}

// Picked is the chosen row and the key that chose it ("" for Enter). A row comes back as
// plain text, without the colour it was shown in.
type Picked struct {
	Key string
	Row string
}

// Picker runs fzf in the house style shared by ccy, ccy-sessions and ccy-teams.
type Picker struct {
	Runner execx.Runner
	Stderr io.Writer
}

// Pick shows the rows and returns the one chosen. Esc, q and the Exit row are ErrLeave.
func (p *Picker) Pick(ctx context.Context, s PickSpec) (Picked, error) {
	args := []string{
		"--ansi", "--height=~70%", "--layout=reverse", "--no-multi", "--no-sort", "--no-info",
		"--color=" + pickerColours,
		"--border=rounded", "--border-label= " + s.Title + " ", "--border-label-pos=3",
		"--margin=1,2", "--padding=1,2", "--header-first", "--pointer=▶",
		"--prompt=filter > ", "--header=" + s.Header + "\n", "--bind=q:abort",
	}
	// fzf refuses an empty --expect, so it is passed only when there are keys.
	if len(s.Keys) > 0 {
		args = append(args, "--expect="+strings.Join(s.Keys, ","))
	}
	if s.CursorLast {
		args = append(args, "--bind=start:last")
	}
	rows := strings.Join(append(append([]string(nil), s.Rows...), ExitRow), "\n") + "\n"
	res, err := p.Runner.Run(ctx, execx.Cmd{
		Name: "fzf", Args: args, Stdin: strings.NewReader(rows), Stderr: p.Stderr,
	})
	if err != nil {
		if exitCode(err) > 0 {
			return Picked{}, ErrLeave
		}
		return Picked{}, fmt.Errorf("fzf: %w", err)
	}
	picked, err := parsePicked(strings.TrimSuffix(string(res.Stdout), "\n"), len(s.Keys) > 0)
	if err != nil {
		return Picked{}, err
	}
	if picked.Row == ExitRow {
		return Picked{}, ErrLeave
	}
	return picked, nil
}

// parsePicked reads fzf's output: "<key>\n<row>" when --expect was given, else the row.
func parsePicked(out string, withKey bool) (Picked, error) {
	if !withKey {
		return Picked{Row: out}, nil
	}
	key, row, found := strings.Cut(out, "\n")
	if !found {
		return Picked{}, errNoRow
	}
	return Picked{Key: key, Row: row}, nil
}

// Confirm is a yes/no in the picker, so no prompt is a bare read: one "Yes, <action>" row
// above Exit. The cursor starts on Exit unless defaultYes (a harmless step the person came
// to take). Leaving is a no.
func (p *Picker) Confirm(ctx context.Context, title, question, action string, defaultYes bool) (bool, error) {
	picked, err := p.Pick(ctx, PickSpec{
		Title: title, Header: Header("Enter choose   Esc or q exit", question),
		Rows: []string{"Yes, " + action}, CursorLast: !defaultYes,
	})
	if errors.Is(err, ErrLeave) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(picked.Row, "Yes, "), nil
}

// Header is a picker header: the key legend first, then the context lines that are not
// empty.
func Header(keys string, lines ...string) string {
	out := []string{"↑↓ choose   " + keys}
	for _, l := range lines {
		if l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
