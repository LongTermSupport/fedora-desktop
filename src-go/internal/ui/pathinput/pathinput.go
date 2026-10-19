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
	// Heading is drawn first: where in its flow the question is, as every ui screen says it.
	Heading ui.Heading
	// Plain draws without colour.
	Plain bool
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

// defaultWidth is the width drawn to until Bubble Tea says the window's.
const defaultWidth = 80

// The keys the line takes as actions, each drawn on the hint line. The rest are line editing.
// Esc and Ctrl+C both cancel: there is no step to go back to inside the line.
var (
	completeKey = key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "completes a directory name"))
	suggestKey  = key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "puts a suggestion in the line"))
	acceptKey   = key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "uses the path"))
	cancelKey   = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("Esc", "cancels"))
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
	spec Spec
	list func(dir string) (Listing, error)
	// line is the text typed, as grapheme clusters; cursor is the cluster it is before.
	line   []ui.Cluster
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
	// width is the window's, every drawn line wrapped to it (Bubble Tea cuts a wider one off).
	width int
}

// New is the line asking s; ctx bounds every listing.
func New(ctx context.Context, s Spec) Model {
	h := help.New()
	h.Styles = help.Styles{}
	value := ui.Clusters(s.Value)
	return Model{
		spec: s, list: func(dir string) (Listing, error) { return s.List(ctx, dir) },
		line: value, cursor: len(value), help: h, width: defaultWidth,
		pick: slices.IndexFunc(s.Suggestions, func(g Suggestion) bool { return g.Path == s.Value }),
	}
}

// Line is the text in the line.
func (m Model) Line() string {
	var b strings.Builder
	for _, c := range m.line {
		b.WriteString(c.Text)
	}
	return b.String()
}

// Result is the path given, or ui.ErrCancelled for Esc or Ctrl+C.
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
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
		}
	}
	return m, nil
}

// View is the heading, the notes, the line, what the last Tab or Enter said, the suggestions
// and the hint line, every line wrapped to the window, never cut; the cursor is on the line's
// wrapped row where it is.
func (m Model) View() tea.View {
	head, err := m.spec.Heading.Draw(!m.spec.Plain, m.width)
	if err != nil {
		head = err.Error()
	}
	lines := strings.Split(head, "\n")
	for _, n := range m.spec.Notes {
		lines = append(lines, wrapped(n, m.width)...)
	}
	lines = append(lines, "")
	input := wrapAt(m.spec.Prompt, m.line, m.cursor, m.width)
	row, col := len(lines)+input.row, input.col
	lines = append(lines, input.rows...)
	for _, s := range m.said() {
		lines = append(lines, wrapped(s, m.width)...)
	}
	if len(m.spec.Suggestions) > 0 {
		lines = append(lines, "", "Suggestions:")
		for i, s := range m.spec.Suggestions {
			lines = append(lines, m.suggestionRows(i, s)...)
		}
	}
	lines = append(lines, "")
	lines = append(lines, wrapped(m.help.ShortHelpView(m.keys()), m.width)...)
	v := tea.NewView(strings.Join(lines, "\n"))
	v.Cursor = tea.NewCursor(col, row)
	return v
}

func (m Model) press(k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(k, cancelKey):
		m.err = ui.ErrCancelled
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
	text := m.Line()
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
	text := m.Line()
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
	m.line = ui.Clusters(text)
	m.cursor = len(m.line)
	m.note, m.shown = "", nil
	return m
}

// edit is line editing: moving, deleting, and typing.
func (m Model) edit(k tea.KeyPressMsg) Model {
	if moved, isMove := m.move(k.String()); isMove {
		return moved
	}
	before := m.Line()
	m = m.delete(k.String())
	// A joiner or a variation selector is part of a cluster, not a control: only controls are
	// refused.
	if k.Text != "" && !strings.ContainsFunc(k.Text, unicode.IsControl) {
		typed := ui.Clusters(k.Text)
		m.line = slices.Insert(slices.Clone(m.line), m.cursor, typed...)
		m.cursor += len(typed)
	}
	if m.Line() != before {
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

// suggestionRows is a suggestion wrapped under its two-column mark, the one in the line marked
// and in the accent.
func (m Model) suggestionRows(i int, s Suggestion) []string {
	row := s.Path
	if s.Note != "" {
		row += "   (" + s.Note + ")"
	}
	rows := wrapped(row, max(m.width-2, 1))
	for j, r := range rows {
		lead := "  "
		if i == m.pick {
			if j == 0 {
				lead = "› "
			}
			if !m.spec.Plain {
				r = picked.Render(r)
			}
		}
		rows[j] = lead + r
	}
	return rows
}

// keys are the keys the hint line shows: the arrows only when there are suggestions.
func (m Model) keys() []key.Binding {
	keys := []key.Binding{completeKey}
	if len(m.spec.Suggestions) > 0 {
		keys = append(keys, suggestKey)
	}
	return append(keys, acceptKey, cancelKey)
}

func entryRow(e Entry) string {
	if e.Note == "" {
		return e.Name + "/"
	}
	return e.Name + "/  (" + e.Note + ")"
}

// wrapping is text laid out in rows, and where a cursor in it is.
type wrapping struct {
	rows     []string
	row, col int
}

// wrapped is text in rows of at most width cells, cut between grapheme clusters anywhere (a
// path has no spaces to break at), nothing left out. Empty text is one empty row.
func wrapped(text string, width int) []string { return wrapAt("", ui.Clusters(text), 0, width).rows }

// wrapAt is prompt then line in rows of at most width cells, cut between grapheme clusters
// and measured by their cells (ui.Clusters, the count ui.Width gives), and the row and column
// of a cursor before line's cluster at cursor, from the same count. A cursor at the very end
// of a full row is at the start of a row of its own, as a terminal puts it.
func wrapAt(prompt string, line []ui.Cluster, cursor, width int) wrapping {
	width = max(width, 1)
	var out wrapping
	var cur strings.Builder
	used := 0
	lead := ui.Clusters(prompt)
	units := slices.Concat(lead, line)
	at := len(lead) + cursor
	for i, u := range units {
		if used+u.Cells > width && used > 0 {
			out.rows = append(out.rows, cur.String())
			cur.Reset()
			used = 0
		}
		if i == at {
			out.row, out.col = len(out.rows), used
		}
		cur.WriteString(u.Text)
		used += u.Cells
	}
	if at >= len(units) {
		out.row, out.col = len(out.rows), used
		if used >= width {
			out.rows = append(out.rows, cur.String())
			cur.Reset()
			out.row, out.col = len(out.rows), 0
		}
	}
	out.rows = append(out.rows, cur.String())
	return out
}

// componentStart is where the path component before the cursor starts, its slash kept.
func componentStart(line []ui.Cluster, cursor int) int {
	at := cursor
	for at > 0 && line[at-1].Text == "/" {
		at--
	}
	for at > 0 && line[at-1].Text != "/" {
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

// Asker asks for a path on a terminal: In is read for the keys, Out drawn on. Width and Height,
// when set, stand in for the terminal's size.
type Asker struct {
	In            io.Reader
	Out           io.Writer
	Width, Height int
}

// Ask runs the line until a path is given (ui.ErrCancelled for Esc, Ctrl+C or an interrupt).
// A heading that names no place is ui.ErrNoHeading, and nothing is drawn.
func (a *Asker) Ask(ctx context.Context, s Spec) (string, error) {
	if _, err := s.Heading.Draw(false, defaultWidth); err != nil {
		return "", err
	}
	opts := []tea.ProgramOption{tea.WithContext(ctx), tea.WithInput(a.In), tea.WithOutput(a.Out)}
	if a.Width > 0 {
		opts = append(opts, tea.WithWindowSize(a.Width, a.Height))
	}
	p := tea.NewProgram(New(ctx, s), opts...)
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
