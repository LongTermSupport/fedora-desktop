package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// ExitRow is the last row of every picker, so leaving is a visible choice, not only a key.
const ExitRow = "Exit"

// pickerColours: the terminal's own colours (16 base, -1 its default) with the accent for
// what is active; the same accent as the gum questions.
const pickerColours = "16,fg:-1,bg:-1,fg+:-1:bold,bg+:-1,hl:3,hl+:3:bold,gutter:-1,pointer:3," +
	"marker:3,prompt:3,spinner:3,info:-1,header:-1,border:-1,label:3:bold"

// fzf's exit statuses that mean the person left: 1 is Enter with nothing matching, 130 is Esc,
// Ctrl-C or q. Any other status (2 is an error) is a failure, never a leave.
const (
	fzfNoMatch     = 1
	fzfInterrupted = 130
)

// fzfMissingError is fzf not being installed, with the bash library's sentence verbatim.
type fzfMissingError struct{}

func (fzfMissingError) Error() string {
	return "fzf is not installed; playbooks/imports/play-claude-yolo.yml installs it."
}

// ErrFzfMissing is fzf not being installed. It is a failure, not the person leaving.
var ErrFzfMissing error = fzfMissingError{}

// errNoRow is fzf printing a key line with no row under it.
var errNoRow = errors.New("fzf: no row after the key line")

// errNotInList is fzf handing back a row the picker did not show.
var errNotInList = errors.New("fzf: a row that is not in the list")

// PickSpec is one picker: a bordered box titled on its frame, the header (key legend and
// context) above the rows, and the actions it offers. There is no bare key: a key exists only
// as the Key of an Action, listed in words on this screen or in the menu of the row it acts on.
type PickSpec struct {
	Title      string
	Header     string
	Rows       []string
	CursorLast bool // start on the Exit row: the safe default for a yes/no
	// Tags, when set, is one hidden tag per row: fzf shows only the row, and hands the tag back
	// with the choice, so rows that read the same are still told apart.
	Tags []string
	// RowHeaders, when set, is one header per row (and, optionally, one more per action row):
	// the header shown follows the row under the cursor, and Header is the one shown on the
	// Exit row. It needs an fzf that has transform-header (0.45 or later).
	RowHeaders []string
	// ExitLabel names the last row; it is ExitRow when empty.
	ExitLabel string
	// Actions are the screen's own choices, listed as rows after Rows, above the Exit row,
	// each with its key beside it; choosing one, or pressing its key anywhere in the list, is
	// Picked.Action.
	Actions []Action
	// Menus, when set, is one menu per row: Enter on a row lists its menu, and a key of the
	// menu pressed on the row chooses that action there; a key the row's menu does not have is
	// a NotOfferedError.
	Menus []Menu
}

// errHeaderCount is RowHeaders that do not pair up with Rows.
var errHeaderCount = errors.New("picker: row headers and rows differ in number")

// maxBindingBytes is how large the header binding may be: Linux refuses one argument over
// 128 KiB, and fzf is given the binding as one.
const maxBindingBytes = 100_000

// errHeadersTooLarge is row headers that do not fit in one fzf argument.
var errHeadersTooLarge = errors.New("picker: the row headers are too large for fzf")

// Picked is what was chosen: the row (as plain text, without the colour it was shown in) and
// its hidden Tag, and the ID of the Action chosen, "" when Enter chose a row with no menu.
type Picked struct {
	Row    string
	Tag    string
	Action string
}

// errTagCount is Tags that do not pair up with Rows.
var errTagCount = errors.New("picker: tags and rows differ in number")

// tagDelimiter separates a row from its hidden place in the list.
const tagDelimiter = "\t"

// Picker runs fzf in the house style shared by ccy, ccy-sessions and ccy-teams.
type Picker struct {
	Runner execx.Runner
	Stderr io.Writer
}

// Pick shows the rows and the actions and returns what was chosen. Esc, q and the Exit row are
// ErrLeave; leaving a row's menu is ErrBack.
func (p *Picker) Pick(ctx context.Context, s PickSpec) (Picked, error) {
	if err := s.checkActions(); err != nil {
		return Picked{}, err
	}
	if len(s.Tags) > 0 && len(s.Tags) != len(s.Rows) {
		return Picked{}, errTagCount
	}
	args, err := s.fzfArgs()
	if err != nil {
		return Picked{}, err
	}
	lines := s.lines()
	exitRow := ExitRow
	if s.ExitLabel != "" {
		exitRow = s.ExitLabel
	}
	stdin := strings.NewReader(strings.Join(append(lines, exitRow), "\n") + "\n")
	res, err := p.Runner.Run(ctx, execx.Cmd{Name: "fzf", Args: args, Env: s.env(), Stdin: stdin, Stderr: p.Stderr})
	out := strings.TrimSuffix(string(res.Stdout), "\n")
	if err != nil {
		switch code := exitCode(err); {
		case code == fzfNoMatch, code == fzfInterrupted:
			return Picked{}, ErrLeave
		case execx.IsNotInstalled(err):
			return Picked{}, ErrFzfMissing
		}
		return Picked{}, fmt.Errorf("fzf: %w", err)
	}
	return p.pickedFrom(ctx, s, out, len(lines), exitRow)
}

// placeOf splits an indexed line into its text and its place among n lines.
func placeOf(line string, n int) (string, int, error) {
	text, place, tagged := strings.Cut(line, tagDelimiter)
	at, err := strconv.Atoi(place)
	if !tagged || err != nil || at < 0 || at >= n {
		return "", 0, fmt.Errorf("%w: %q", errNotInList, line)
	}
	return text, at, nil
}

// fzfArgs is the house style, the header, and the keys the screen declares.
func (s PickSpec) fzfArgs() ([]string, error) {
	headerArgs, err := s.headerArgs()
	if err != nil {
		return nil, err
	}
	args := append([]string{
		"--ansi", "--height=~70%", "--layout=reverse", "--no-multi", "--no-sort", "--no-info",
		"--color=" + pickerColours,
		"--border=rounded", "--border-label= " + s.Title + " ", "--border-label-pos=3",
		"--margin=1,2", "--padding=1,2", "--header-first", "--pointer=▶",
		"--prompt=filter > ", "--bind=q:abort",
	}, headerArgs...)
	// fzf refuses an empty --expect, so it is passed only when there are keys.
	if keys := s.keys(); len(keys) > 0 {
		args = append(args, expectArg(keys))
	}
	if s.CursorLast {
		args = append(args, "--bind=start:last")
	}
	if s.indexed() || len(s.Tags) > 0 {
		args = append(args, "--delimiter="+tagDelimiter, "--with-nth=1")
	}
	return args, nil
}

// indexed is whether each line carries its hidden place in the list: when there are actions,
// so a row and an action row are told apart and a key is judged against the right row's menu.
// Rows with tags only carry the tag itself.
func (s PickSpec) indexed() bool {
	return len(s.Actions) > 0 || len(s.Menus) > 0
}

// lines is the rows and then the action rows, each with its place when indexed, else with its
// tag when tagged.
func (s PickSpec) lines() []string {
	lines := append([]string(nil), s.Rows...)
	for _, a := range s.Actions {
		lines = append(lines, a.Line())
	}
	if !s.indexed() {
		for i, tag := range s.Tags {
			lines[i] += tagDelimiter + tag
		}
		return lines
	}
	for i := range lines {
		lines[i] += tagDelimiter + strconv.Itoa(i)
	}
	return lines
}

// headerArgs is the fzf arguments that set the header: the one given, or the first row's when
// each row has its own, which then follows the cursor.
func (s PickSpec) headerArgs() ([]string, error) {
	if len(s.RowHeaders) == 0 {
		return []string{"--header=" + s.Header + "\n"}, nil
	}
	n := len(s.RowHeaders)
	if n != len(s.Rows) && (len(s.Actions) == 0 || n != len(s.Rows)+len(s.Actions)) {
		return nil, errHeaderCount
	}
	first := s.RowHeaders[0]
	if s.CursorLast {
		first = s.Header
	}
	follow := headerFollowsCursor(s.RowHeaders, s.Header)
	if len(follow) > maxBindingBytes {
		return nil, fmt.Errorf("%w: %d bytes, the limit is %d", errHeadersTooLarge, len(follow), maxBindingBytes)
	}
	return []string{"--header=" + first + "\n", follow}, nil
}

// env is what fzf is run with: the header binding is sh, whatever the login shell is.
func (s PickSpec) env() []string {
	if len(s.RowHeaders) == 0 {
		return nil
	}
	return []string{"SHELL=/bin/sh"}
}

// headerFollowsCursor is the fzf binding that swaps the header for the one of the row the
// cursor is on, by the row's place in the input; the Exit row, past the last, gets fallback.
// The texts are single-quoted for sh, never expanded.
func headerFollowsCursor(headers []string, fallback string) string {
	var arms strings.Builder
	for i, h := range headers {
		fmt.Fprintf(&arms, "%d) %s;; ", i, printLine(h))
	}
	return "--bind=focus:transform-header^case {n} in " + arms.String() + "*) " + printLine(fallback) + ";; esac^"
}

// printLine is the sh command that prints text and a newline, with text quoted. The caret
// delimits the binding, so a caret in text is shown as a hyphen.
func printLine(text string) string {
	text = strings.ReplaceAll(text, "^", "-")
	return "printf '%s\\n' '" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}

// parsePicked reads fzf's output, the key and the row: "<key>\n<row>" when --expect was given,
// else the row.
func parsePicked(out string, withKey bool) (string, string, error) {
	if !withKey {
		return "", out, nil
	}
	key, row, found := strings.Cut(out, "\n")
	if !found {
		return "", "", errNoRow
	}
	return key, row, nil
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

// pickedFrom is what fzf's answer chose: the key line when keys were expected, then the row.
func (p *Picker) pickedFrom(ctx context.Context, s PickSpec, out string, n int, exitRow string) (Picked, error) {
	key, line, err := parsePicked(out, len(s.keys()) > 0)
	if err != nil {
		return Picked{}, err
	}
	if line == exitRow {
		return p.chosen(ctx, s, key, "", -1)
	}
	if !s.indexed() {
		// Rows with tags only carry the tag itself; a row fzf hands back without one keeps "".
		if row, tag, tagged := strings.Cut(line, tagDelimiter); tagged && len(s.Tags) > 0 {
			return Picked{Row: row, Tag: tag}, nil
		}
		return Picked{Row: line}, nil
	}
	text, at, err := placeOf(line, n)
	if err != nil {
		return Picked{}, err
	}
	return p.chosen(ctx, s, key, text, at)
}

// chosen is what the key, or Enter, chose on the row at place at (-1 for the Exit row).
func (p *Picker) chosen(ctx context.Context, s PickSpec, key, row string, at int) (Picked, error) {
	var picked Picked
	onRow := at >= 0 && at < len(s.Rows)
	if onRow {
		picked.Row = row
		if len(s.Tags) > 0 {
			picked.Tag = s.Tags[at]
		}
	}
	if key != "" {
		return s.byKeyAt(key, at, picked)
	}
	switch {
	case at < 0:
		return Picked{}, ErrLeave
	case !onRow:
		return Picked{Action: s.Actions[at-len(s.Rows)].ID}, nil
	case len(s.Menus) > 0:
		return p.menu(ctx, s.Menus[at], picked)
	}
	return picked, nil
}

// menu is Enter on a row: its actions, each with its key; leaving it is ErrBack.
func (p *Picker) menu(ctx context.Context, m Menu, picked Picked) (Picked, error) {
	got, err := p.Pick(ctx, PickSpec{Title: m.Title, Header: m.Header, Actions: m.Actions, ExitLabel: menuBack})
	if errors.Is(err, ErrLeave) {
		return Picked{}, ErrBack
	}
	if err != nil {
		return Picked{}, err
	}
	picked.Action = got.Action
	return picked, nil
}

// byKeyAt is the action key chooses on the row at place at: the screen's, else the row's menu's.
func (s PickSpec) byKeyAt(key string, at int, picked Picked) (Picked, error) {
	if a, ok := byKey(s.Actions, key); ok {
		// The row the cursor was on stays: a screen action may act on it (Tab completing a
		// typed path opens the highlighted directory when nothing is typed).
		picked.Action = a.ID
		return picked, nil
	}
	if at >= 0 && at < len(s.Menus) {
		if a, ok := byKey(s.Menus[at].Actions, key); ok {
			picked.Action = a.ID
			return picked, nil
		}
	}
	return picked, &NotOfferedError{Key: key}
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
