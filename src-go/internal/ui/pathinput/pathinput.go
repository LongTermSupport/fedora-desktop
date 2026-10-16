// Package pathinput is a line a path is typed in, with Tab completing a directory name as a
// shell does: the one directory the typed name starts, or what several share, with those
// listed. Typing comes first: the line has the focus from the start, and anything it is given
// (a starting value, a suggestion put in it with the arrows) is edited like typed text. The
// directories are read through the caller's List, so the tree can be anywhere (a container's,
// read from the host's side). A hint line says what the keys do, Tab included: Tab completing
// a line is standard UX, so it is a key with no row of its own, but never an unsaid one.
package pathinput

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// Entry is a directory in a listing, with a note shown beside it ("a git checkout").
type Entry struct{ Name, Note string }

// Listing is a directory's subdirectories, with a note shown when Tab used it ("only the
// first 500 are listed").
type Listing struct {
	Entries []Entry
	Note    string
}

// Suggestion is a path offered under the line; the arrows put it in the line.
type Suggestion struct{ Path, Note string }

// Spec is what the line asks.
type Spec struct {
	// Heading is the first line: where in its flow the question is.
	Heading string
	// Notes are lines under the heading.
	Notes []string
	// Prompt is drawn before the line.
	Prompt string
	// Value is what the line starts with.
	Value string
	// Suggestions are listed under the line.
	Suggestions []Suggestion
	// List is the directories in dir (ending in a slash), bounded in time by the caller.
	List func(ctx context.Context, dir string) (Listing, error)
}

// errNotDone is the line still being edited: no path given and nobody left.
var errNotDone = errors.New("pathinput: no path was given")

// maxShown is how many of an ambiguous Tab's directories are listed.
const maxShown = 20

// The keys the line takes as actions, each drawn on the hint line. The rest are line editing.
var (
	completeKey = key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "completes a directory name"))
	suggestKey  = key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "puts a suggestion in the line"))
	acceptKey   = key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "uses the path"))
	leaveKey    = key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "back"))
	cancelKey   = key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("Ctrl+C", "cancel"))
)

// picked marks the suggestion in the line.
var picked = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.Accent))

// listedMsg is a directory listing read for a Tab.
type listedMsg struct {
	dir, frag string
	listing   Listing
	err       error
}

// Model is the line and what it shows.
type Model struct {
	spec   Spec
	list   func(dir string) (Listing, error)
	line   []rune
	cursor int
	// pick is the suggestion in the line, -1 for none.
	pick int
	// note is what the last Tab or Enter said; shown, an ambiguous Tab's directories.
	note  string
	shown []string
	// listing is the directory being read for a Tab, "" when none; keys pressed meanwhile wait
	// in queue and are applied after it, as a shell applies typed-ahead keys.
	listing string
	queue   []tea.KeyPressMsg
	path    string
	err     error
	help    help.Model
}

// New is the line asking s; ctx bounds every listing.
func New(ctx context.Context, s Spec) Model {
	h := help.New()
	h.Styles = help.Styles{}
	value := []rune(s.Value)
	return Model{
		spec: s, list: func(dir string) (Listing, error) { return s.List(ctx, dir) },
		line: value, cursor: len(value), help: h,
		pick: slices.IndexFunc(s.Suggestions, func(g Suggestion) bool { return g.Path == s.Value }),
	}
}

// Line is the text in the line.
func (m Model) Line() string { return string(m.line) }

// Result is the path given, ui.ErrLeave for Esc, ui.ErrCancelled for Ctrl+C.
func (m Model) Result() (string, error) {
	if m.err != nil {
		return "", m.err
	}
	if m.path == "" {
		return "", errNotDone
	}
	return m.path, nil
}

// Init starts nothing.
func (m Model) Init() tea.Cmd { return nil }

// Update takes a key or a listing.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.press(msg)
	case listedMsg:
		m.listing = ""
		return m.completed(msg).replay()
	}
	return m, nil
}

// View is the heading, the notes, the line, what the last Tab or Enter said, the suggestions
// and the hint line.
func (m Model) View() tea.View {
	lines := append([]string{m.spec.Heading}, m.spec.Notes...)
	lines = append(lines, "")
	row := len(lines)
	lines = append(lines, m.spec.Prompt+string(m.line))
	lines = append(lines, m.said()...)
	if len(m.spec.Suggestions) > 0 {
		lines = append(lines, "", "Suggestions:")
		for i, s := range m.spec.Suggestions {
			lines = append(lines, m.suggestionRow(i, s))
		}
	}
	lines = append(lines, "", m.help.ShortHelpView(m.keys()))
	v := tea.NewView(strings.Join(lines, "\n"))
	v.Cursor = tea.NewCursor(lipgloss.Width(m.spec.Prompt+string(m.line[:m.cursor])), row)
	return v
}

func (m Model) press(k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(k, cancelKey):
		m.err = ui.ErrCancelled
		return m, tea.Quit
	case key.Matches(k, leaveKey):
		m.err = ui.ErrLeave
		return m, tea.Quit
	case m.listing != "":
		m.queue = append(m.queue, k)
		return m, nil
	case key.Matches(k, completeKey):
		return m.complete()
	case key.Matches(k, acceptKey):
		return m.accept()
	case key.Matches(k, suggestKey):
		return m.suggest(k.String() == "up"), nil
	}
	return m.edit(k), nil
}

// replay applies the keys pressed while a listing was read, until one starts another.
func (m Model) replay() (Model, tea.Cmd) {
	queue := m.queue
	m.queue = nil
	for i, k := range queue {
		var cmd tea.Cmd
		m, cmd = m.press(k)
		if cmd != nil {
			m.queue = append(m.queue, queue[i+1:]...)
			return m, cmd
		}
	}
	return m, nil
}

// complete starts reading the directory the line names, for Tab.
func (m Model) complete() (Model, tea.Cmd) {
	text := string(m.line)
	if !strings.HasPrefix(text, "/") {
		m.note = "A path starts with /: Tab completes from there."
		return m, nil
	}
	cut := strings.LastIndex(text, "/") + 1
	dir, frag := text[:cut], text[cut:]
	m.listing, m.note, m.shown = dir, "", nil
	list := m.list
	return m, func() tea.Msg {
		listing, err := list(dir)
		return listedMsg{dir: dir, frag: frag, listing: listing, err: err}
	}
}

// completed is a Tab's listing applied: the one directory completed, what several share with
// those listed, or nothing to complete said. Hidden directories match only a typed dot.
func (m Model) completed(l listedMsg) Model {
	if l.err != nil {
		m.note = l.err.Error()
		return m
	}
	var names, rows []string
	for _, e := range l.listing.Entries {
		if !strings.HasPrefix(e.Name, l.frag) || (strings.HasPrefix(e.Name, ".") && !strings.HasPrefix(l.frag, ".")) {
			continue
		}
		names = append(names, e.Name)
		rows = append(rows, entryRow(e))
	}
	switch len(names) {
	case 0:
		m.note = fmt.Sprintf("Nothing in %s starts with %s.", l.dir, l.frag)
		if l.frag == "" {
			m.note = l.dir + " has no directories to complete."
		}
	case 1:
		m = m.setLine(l.dir + names[0] + "/")
	default:
		m = m.setLine(l.dir + sharedPrefix(names))
		m.shown = rows
	}
	if l.listing.Note != "" {
		m.note = strings.TrimSpace(m.note + " " + l.listing.Note)
	}
	return m
}

// accept takes the line as the path, cleaned: no trailing slash.
func (m Model) accept() (Model, tea.Cmd) {
	text := string(m.line)
	if !strings.HasPrefix(text, "/") {
		m.note = "A path starts with /: type the whole path, or Tab from /."
		return m, nil
	}
	m.path = path.Clean(text)
	return m, tea.Quit
}

// suggest puts the suggestion above or below the one in the line into it.
func (m Model) suggest(up bool) Model {
	n := len(m.spec.Suggestions)
	if n == 0 {
		return m
	}
	switch {
	case m.pick < 0:
		m.pick = 0
	case up:
		m.pick = max(m.pick-1, 0)
	default:
		m.pick = min(m.pick+1, n-1)
	}
	return m.setLine(m.spec.Suggestions[m.pick].Path)
}

func (m Model) setLine(text string) Model {
	m.line = []rune(text)
	m.cursor = len(m.line)
	m.note, m.shown = "", nil
	return m
}

// edit is line editing: moving, deleting, and typing.
func (m Model) edit(k tea.KeyPressMsg) Model {
	if moved, isMove := m.move(k.String()); isMove {
		return moved
	}
	before := string(m.line)
	m = m.delete(k.String())
	if k.Text != "" && !strings.ContainsFunc(k.Text, func(r rune) bool { return !unicode.IsPrint(r) }) {
		typed := []rune(k.Text)
		m.line = slices.Insert(slices.Clone(m.line), m.cursor, typed...)
		m.cursor += len(typed)
	}
	if string(m.line) != before {
		m.pick, m.note, m.shown = -1, "", nil
	}
	return m
}

// move moves the cursor for a movement key, and is whether the key was one.
func (m Model) move(k string) (Model, bool) {
	switch k {
	case "left":
		m.cursor = max(m.cursor-1, 0)
	case "right":
		m.cursor = min(m.cursor+1, len(m.line))
	case "home", "ctrl+a":
		m.cursor = 0
	case "end", "ctrl+e":
		m.cursor = len(m.line)
	default:
		return m, false
	}
	return m, true
}

// delete deletes for a deletion key: a character either side, to either end, or a path
// component back.
func (m Model) delete(k string) Model {
	from, to := m.cursor, m.cursor
	switch k {
	case "backspace", "ctrl+h":
		from = max(m.cursor-1, 0)
	case "delete":
		to = min(m.cursor+1, len(m.line))
	case "ctrl+u":
		from = 0
	case "ctrl+k":
		to = len(m.line)
	case "ctrl+w":
		from = componentStart(m.line, m.cursor)
	}
	m.line = slices.Delete(slices.Clone(m.line), from, to)
	m.cursor = from
	return m
}

// said is the lines under the line: a listing being read, the last note, a Tab's directories.
func (m Model) said() []string {
	var lines []string
	if m.listing != "" {
		lines = append(lines, "  Listing "+m.listing+"…")
	}
	if m.note != "" {
		lines = append(lines, "  "+m.note)
	}
	for i, r := range m.shown {
		if i == maxShown {
			lines = append(lines, fmt.Sprintf("  … and %d more: type more of the name.", len(m.shown)-maxShown))
			break
		}
		lines = append(lines, "  "+r)
	}
	return lines
}

func (m Model) suggestionRow(i int, s Suggestion) string {
	row := s.Path
	if s.Note != "" {
		row += "   (" + s.Note + ")"
	}
	if i == m.pick {
		return "› " + picked.Render(row)
	}
	return "  " + row
}

// keys are the keys the hint line shows: the arrows only when there are suggestions.
func (m Model) keys() []key.Binding {
	keys := []key.Binding{completeKey}
	if len(m.spec.Suggestions) > 0 {
		keys = append(keys, suggestKey)
	}
	return append(keys, acceptKey, leaveKey, cancelKey)
}

func entryRow(e Entry) string {
	if e.Note == "" {
		return e.Name + "/"
	}
	return e.Name + "/  (" + e.Note + ")"
}

// componentStart is where the path component before the cursor starts, its slash kept.
func componentStart(line []rune, cursor int) int {
	at := cursor
	for at > 0 && line[at-1] == '/' {
		at--
	}
	for at > 0 && line[at-1] != '/' {
		at--
	}
	return at
}

// sharedPrefix is the longest text every name starts with, whole characters only.
func sharedPrefix(names []string) string {
	prefix := []rune(names[0])
	for _, n := range names[1:] {
		for !strings.HasPrefix(n, string(prefix)) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return string(prefix)
}

// Asker asks for a path on a terminal: In is read for the keys, Out drawn on.
type Asker struct {
	In  io.Reader
	Out io.Writer
}

// Ask runs the line until a path is given (ui.ErrLeave for Esc, ui.ErrCancelled for Ctrl+C or
// an interrupt).
func (a *Asker) Ask(ctx context.Context, s Spec) (string, error) {
	p := tea.NewProgram(New(ctx, s), tea.WithContext(ctx), tea.WithInput(a.In), tea.WithOutput(a.Out))
	final, err := p.Run()
	if errors.Is(err, tea.ErrInterrupted) {
		return "", ui.ErrCancelled
	}
	if err != nil {
		return "", fmt.Errorf("the path line: %w", err)
	}
	m, isModel := final.(Model)
	if !isModel {
		return "", errNotDone
	}
	return m.Result()
}
