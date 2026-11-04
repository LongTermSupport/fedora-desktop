package pathinput

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// tree is a fake directory tree: the entries of each directory, by its path ending in a slash.
type tree struct {
	dirs   map[string][]Entry
	notes  map[string]string
	fail   map[string]error
	listed []string
}

func (t *tree) list(_ context.Context, dir string) (Listing, error) {
	t.listed = append(t.listed, dir)
	if err := t.fail[dir]; err != nil {
		return Listing{}, err
	}
	return Listing{Entries: t.dirs[dir], Note: t.notes[dir]}, nil
}

func sample() *tree {
	return &tree{dirs: map[string][]Entry{
		"/":         {{Name: "home"}, {Name: "srv"}, {Name: "var"}},
		"/var/":     {{Name: "www"}, {Name: "lib"}},
		"/var/www/": {{Name: "site-a", Note: "a git checkout"}, {Name: "site-b"}, {Name: "other"}, {Name: ".cache"}},
	}}
}

// head is the heading the tests' line is asked under.
var head = ui.Here("Teams", "alpha", "Add member").AtStep(3, 4, "Checkout").About("into team alpha")

func spec(t *tree) Spec {
	return Spec{Heading: head, Plain: true, Prompt: "Checkout: ", List: t.list}
}

// longPath is a suggestion wider than the narrow windows the wrapping tests use.
const longPath = "/var/www/vhosts/example.test/httpdocs/current"

// typed is the key presses that type the text.
func typed(text string) []tea.KeyPressMsg {
	keys := make([]tea.KeyPressMsg, 0, len(text))
	for _, r := range text {
		keys = append(keys, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return keys
}

var (
	tab       = tea.KeyPressMsg{Code: tea.KeyTab}
	enter     = tea.KeyPressMsg{Code: tea.KeyEnter}
	esc       = tea.KeyPressMsg{Code: tea.KeyEscape}
	up        = tea.KeyPressMsg{Code: tea.KeyUp}
	down      = tea.KeyPressMsg{Code: tea.KeyDown}
	left      = tea.KeyPressMsg{Code: tea.KeyLeft}
	backspace = tea.KeyPressMsg{Code: tea.KeyBackspace}
	ctrlC     = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	ctrlU     = tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
)

// drive presses the keys in order, running each command the model returns as Bubble Tea would
// and handing its message back, until the model quits or the keys run out.
func drive(t *testing.T, m Model, keys ...tea.KeyPressMsg) Model {
	t.Helper()
	for _, k := range keys {
		m = settle(t, m, k)
	}
	return m
}

func settle(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, cmd := m.Update(msg)
	m = asModel(t, next)
	for cmd != nil {
		out := cmd()
		if _, quit := out.(tea.QuitMsg); quit {
			return m
		}
		next, cmd = m.Update(out)
		m = asModel(t, next)
	}
	return m
}

func asModel(t *testing.T, next tea.Model) Model {
	t.Helper()
	m, isModel := next.(Model)
	if !isModel {
		t.Fatalf("Update returned a %T", next)
	}
	return m
}

func view(m Model) string { return m.View().Content }

func TestTypingComesFirstIntoTheLineWithThePrefilledValueEditable(t *testing.T) {
	s := spec(sample())
	s.Value = "/home/"
	m := drive(t, New(context.Background(), s), append([]tea.KeyPressMsg{ctrlU}, typed("/srv/x")...)...)
	if m.Line() != "/srv/x" || !strings.Contains(view(m), "Checkout: /srv/x") {
		t.Fatalf("line %q, view\n%s", m.Line(), view(m))
	}
}

func TestTheHeadingAndAHintLineSayingTabCompletesAreShown(t *testing.T) {
	v := view(New(context.Background(), spec(sample())))
	if !strings.HasPrefix(v, "Teams › alpha › Add member · step 3/4 Checkout\ninto team alpha\n") ||
		!strings.Contains(v, "Tab completes a directory name") || !strings.Contains(v, "Esc cancels") {
		t.Fatalf("view\n%s", v)
	}
}

func TestEveryLineIsWrappedToTheWindowNeverCut(t *testing.T) {
	s := spec(sample())
	s.Suggestions = []Suggestion{{Path: longPath, Note: "example-org/site"}}
	s.Notes = []string{"A note that is longer than the narrow window it is drawn in."}
	m := settle(t, New(context.Background(), s), tea.WindowSizeMsg{Width: 20, Height: 40})
	m = drive(t, m, ctrlU)
	m = drive(t, m, typed(longPath)...)
	v := view(m)
	var joined strings.Builder
	for l := range strings.SplitSeq(v, "\n") {
		if w := lipgloss.Width(l); w > 20 {
			t.Fatalf("a line %d wide at width 20: %q\n%s", w, l, v)
		}
		// A suggestion's rows are under its two-column mark.
		joined.WriteString(strings.TrimPrefix(l, "  "))
	}
	for _, want := range []string{
		"Checkout: " + longPath, longPath + "   (example-org/site)",
		"A note that is longer than the narrow window it is drawn in.", "Tab completes a directory name",
	} {
		if !strings.Contains(joined.String(), want) {
			t.Fatalf("%q was cut:\n%s", want, v)
		}
	}
	// The cursor is at the end of the typed line, on its last wrapped row.
	lines := strings.Split(v, "\n")
	c := m.View().Cursor
	if c == nil || c.Y >= len(lines) || !strings.HasSuffix(lines[c.Y], "current") || c.X != lipgloss.Width(lines[c.Y]) {
		t.Fatalf("cursor %+v over\n%s", c, v)
	}
}

// A ZWJ family (five runes, two cells) and a flag (two regional indicators, two cells): each
// is one grapheme cluster, which a terminal draws as one character.
var (
	family = string([]rune{0x1F468, 0x200D, 0x1F469, 0x200D, 0x1F467})
	flag   = string([]rune{0x1F1EC, 0x1F1E7})
)

func TestAGraphemeClusterIsWrappedWholeAndTheCursorCountsItsCells(t *testing.T) {
	m := settle(t, New(context.Background(), spec(sample())), tea.WindowSizeMsg{Width: 14, Height: 40})
	m = drive(t, m, typed("/a")...)
	// "Checkout: /a" is 12 cells: the family fills the row to 14, the flag starts the next.
	m = drive(t, m, tea.KeyPressMsg{Text: family}, tea.KeyPressMsg{Text: flag})
	lines := strings.Split(view(m), "\n")
	row := slices.Index(lines, "Checkout: /a"+family)
	if row < 0 || lines[row+1] != flag {
		t.Fatalf("the clusters were cut:\n%q", lines)
	}
	if c := m.View().Cursor; c == nil || c.Y != row+1 || c.X != 2 {
		t.Fatalf("cursor %+v, want row %d column 2 (after the flag's two cells)", c, row+1)
	}
	m = drive(t, m, left)
	if c := m.View().Cursor; c.Y != row+1 || c.X != 0 {
		t.Fatalf("after Left the cursor is %+v, want before the flag", c)
	}
	m = drive(t, m, backspace)
	if m.Line() != "/a"+flag {
		t.Fatalf("Backspace took %q, want the whole family", m.Line())
	}
}

func TestTheCursorStaysOnItsWrappedRowWhenMovedBack(t *testing.T) {
	m := settle(t, New(context.Background(), spec(sample())), tea.WindowSizeMsg{Width: 20, Height: 40})
	m = drive(t, m, typed("/var/www/vhosts/abcdefgh")...)
	m = drive(t, m, tea.KeyPressMsg{Code: tea.KeyHome})
	v := view(m)
	lines := strings.Split(v, "\n")
	c := m.View().Cursor
	if c == nil || !strings.HasPrefix(lines[c.Y], "Checkout: ") || c.X != len("Checkout: ") {
		t.Fatalf("cursor %+v over\n%s", c, v)
	}
}

// visible is what a terminal shows of Bubble Tea's output, roughly: the escape sequences and
// line breaks removed, so text a narrow window wrapped reads as one run again.
var escapes = regexp.MustCompile(`\x1b\[[0-9;?<>=$ ]*[A-Za-z~]|\x1b\][^\x07]*\x07|\x1b[=>]|[\r\n]`)

func TestTheRenderedScreenAtANarrowWidthLosesNothing(t *testing.T) {
	var out bytes.Buffer
	s := spec(sample())
	s.Value = longPath
	a := &Asker{In: strings.NewReader("\r"), Out: &out, Width: 24, Height: 40}
	if path, err := a.Ask(context.Background(), s); err != nil || path != longPath {
		t.Fatalf("ask %q %v", path, err)
	}
	shown := escapes.ReplaceAllString(out.String(), "")
	for _, want := range []string{"Checkout: " + longPath, "completes a directory name", "Esc cancels"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("%q is not on the screen:\n%q", want, shown)
		}
	}
}

func TestALineWithNoHeadingIsRefusedBeforeItRuns(t *testing.T) {
	s := spec(sample())
	s.Heading = ui.Heading{}
	a := &Asker{In: strings.NewReader("\r"), Out: &bytes.Buffer{}}
	if _, err := a.Ask(context.Background(), s); !errors.Is(err, ui.ErrNoHeading) {
		t.Fatalf("ask: %v", err)
	}
}

func TestTabCompletesTheOneDirectoryTheTextStarts(t *testing.T) {
	tr := sample()
	m := drive(t, New(context.Background(), spec(tr)), append(typed("/va"), tab)...)
	if m.Line() != "/var/" || strings.Join(tr.listed, ",") != "/" {
		t.Fatalf("line %q, listed %v", m.Line(), tr.listed)
	}
}

func TestTabOnAnAmbiguousNameCompletesWhatTheyShareAndListsThem(t *testing.T) {
	m := drive(t, New(context.Background(), spec(sample())), append(typed("/var/www/s"), tab)...)
	v := view(m)
	if m.Line() != "/var/www/site-" || !strings.Contains(v, "site-a/  (a git checkout)") ||
		!strings.Contains(v, "site-b/") || strings.Contains(v, "other/") {
		t.Fatalf("line %q, view\n%s", m.Line(), v)
	}
}

func TestTabAfterASlashListsTheDirectoryLeavingHiddenOnesUntilADotIsTyped(t *testing.T) {
	m := drive(t, New(context.Background(), spec(sample())), append(typed("/var/www/"), tab)...)
	if v := view(m); !strings.Contains(v, "other/") || strings.Contains(v, ".cache/") {
		t.Fatalf("view\n%s", v)
	}
	m = drive(t, m, append(typed("."), tab)...)
	if m.Line() != "/var/www/.cache/" {
		t.Fatalf("line %q", m.Line())
	}
}

func TestAListingsNoteIsShownWithWhatTabDid(t *testing.T) {
	tr := sample()
	tr.notes = map[string]string{"/var/www/": "Only the first 500 directories are listed."}
	m := drive(t, New(context.Background(), spec(tr)), append(typed("/var/www/"), tab)...)
	if v := view(m); !strings.Contains(v, "Only the first 500 directories are listed.") || !strings.Contains(v, "site-b/") {
		t.Fatalf("view\n%s", v)
	}
}

func TestAStartingValueThatIsASuggestionIsMarkedAsIt(t *testing.T) {
	s := spec(sample())
	s.Suggestions = []Suggestion{{Path: "/srv/one"}, {Path: "/srv/two"}}
	s.Value = "/srv/one"
	m := drive(t, New(context.Background(), s), down)
	if m.Line() != "/srv/two" {
		t.Fatalf("line %q", m.Line())
	}
}

func TestNothingToCompleteIsSaidAndTheLineKept(t *testing.T) {
	m := drive(t, New(context.Background(), spec(sample())), append(typed("/var/zz"), tab)...)
	if m.Line() != "/var/zz" || !strings.Contains(view(m), "Nothing in /var/ starts with zz.") {
		t.Fatalf("line %q, view\n%s", m.Line(), view(m))
	}
}

func TestADirectoryThatCannotBeListedIsSaidAndTheLineKept(t *testing.T) {
	tr := sample()
	tr.fail = map[string]error{"/srv/": errors.New("listing /srv/ took longer than 20s")}
	m := drive(t, New(context.Background(), spec(tr)), append(typed("/srv/a"), tab)...)
	if m.Line() != "/srv/a" || !strings.Contains(view(m), "listing /srv/ took longer than 20s") {
		t.Fatalf("line %q, view\n%s", m.Line(), view(m))
	}
}

func TestKeysPressedWhileAListingIsReadAreAppliedAfterIt(t *testing.T) {
	m := New(context.Background(), spec(sample()))
	m = drive(t, m, typed("/va")...)
	next, cmd := m.Update(tab)
	m = asModel(t, next)
	for _, k := range append(typed("www"), enter) {
		next, more := m.Update(k)
		m = asModel(t, next)
		if more != nil {
			t.Fatalf("a key pressed while listing ran %v", more)
		}
	}
	if !strings.Contains(view(m), "Listing /…") {
		t.Fatalf("view while listing\n%s", view(m))
	}
	m = settle(t, m, cmd())
	if path, err := m.Result(); err != nil || path != "/var/www" {
		t.Fatalf("result %q %v", path, err)
	}
}

func TestEnterTakesTheLineAsTypedWithoutItsTrailingSlash(t *testing.T) {
	m := drive(t, New(context.Background(), spec(sample())), append(typed("/var/www/site-a/"), enter)...)
	if path, err := m.Result(); err != nil || path != "/var/www/site-a" {
		t.Fatalf("result %q %v", path, err)
	}
}

func TestEnterOnAPathThatIsNotAbsoluteIsSaidAndNotTaken(t *testing.T) {
	m := drive(t, New(context.Background(), spec(sample())), append(typed("var"), enter)...)
	if _, err := m.Result(); !errors.Is(err, errNotDone) || !strings.Contains(view(m), "starts with /") {
		t.Fatalf("result %v, view\n%s", err, view(m))
	}
}

func TestTheSuggestionsAreListedAndTheArrowsPutOneInTheLine(t *testing.T) {
	s := spec(sample())
	s.Suggestions = []Suggestion{{Path: "/srv/one", Note: "example-org/one"}, {Path: "/var/www/two", Note: "example-org/two"}}
	m := New(context.Background(), s)
	if v := view(m); !strings.Contains(v, "/srv/one   (example-org/one)") || !strings.Contains(v, "/var/www/two") {
		t.Fatalf("view\n%s", v)
	}
	m = drive(t, m, down, down)
	if m.Line() != "/var/www/two" {
		t.Fatalf("line %q", m.Line())
	}
	m = drive(t, m, up, enter)
	if path, err := m.Result(); err != nil || path != "/srv/one" {
		t.Fatalf("result %q %v", path, err)
	}
}

func TestEditingKeysMoveAndDelete(t *testing.T) {
	m := drive(t, New(context.Background(), spec(sample())), append(typed("/srv"), left, backspace)...)
	if m.Line() != "/sv" {
		t.Fatalf("line %q", m.Line())
	}
	m = drive(t, m, typed("r")...)
	if m.Line() != "/srv" {
		t.Fatalf("line %q", m.Line())
	}
}

func TestTheReadlineEditsWork(t *testing.T) {
	ctrl := func(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }
	home := tea.KeyPressMsg{Code: tea.KeyHome}
	end := tea.KeyPressMsg{Code: tea.KeyEnd}
	del := tea.KeyPressMsg{Code: tea.KeyDelete}
	right := tea.KeyPressMsg{Code: tea.KeyRight}
	m := drive(t, New(context.Background(), spec(sample())), append(typed("/var/www/site"), ctrl('w'))...)
	if m.Line() != "/var/www/" {
		t.Fatalf("ctrl+w: %q", m.Line())
	}
	m = drive(t, m, home, right, del, end)
	if m.Line() != "/ar/www/" {
		t.Fatalf("delete: %q", m.Line())
	}
	m = drive(t, m, ctrl('a'), right, ctrl('k'))
	if m.Line() != "/" {
		t.Fatalf("ctrl+k: %q", m.Line())
	}
}

func TestTabOnAPathThatIsNotAbsoluteSaysSoAndListsNothing(t *testing.T) {
	tr := sample()
	m := drive(t, New(context.Background(), spec(tr)), append(typed("var"), tab)...)
	if len(tr.listed) != 0 || !strings.Contains(view(m), "A path starts with /") {
		t.Fatalf("listed %v, view\n%s", tr.listed, view(m))
	}
}

func TestAnEmptyDirectoryAndALongListingAreSaid(t *testing.T) {
	tr := sample()
	tr.dirs["/srv/"] = nil
	for i := range maxShown + 3 {
		tr.dirs["/home/"] = append(tr.dirs["/home/"], Entry{Name: fmt.Sprintf("user%02d", i)})
	}
	m := drive(t, New(context.Background(), spec(tr)), append(typed("/srv/"), tab)...)
	if !strings.Contains(view(m), "/srv/ has no directories to complete.") {
		t.Fatalf("view\n%s", view(m))
	}
	m = drive(t, New(context.Background(), spec(tr)), append(typed("/home/"), tab)...)
	if v := view(m); m.Line() != "/home/user" || !strings.Contains(v, "… and 3 more") {
		t.Fatalf("line %q, view\n%s", m.Line(), v)
	}
}

func TestTheArrowsDoNothingWithoutSuggestionsAndTheHintLineLeavesThemOut(t *testing.T) {
	m := drive(t, New(context.Background(), spec(sample())), append(typed("/x"), up)...)
	if m.Line() != "/x" || strings.Contains(view(m), "↑/↓") {
		t.Fatalf("line %q, view\n%s", m.Line(), view(m))
	}
}

func TestEscAndCtrlCCancel(t *testing.T) {
	m := drive(t, New(context.Background(), spec(sample())), esc)
	if _, err := m.Result(); !errors.Is(err, ui.ErrCancelled) {
		t.Fatalf("esc: %v", err)
	}
	m = drive(t, New(context.Background(), spec(sample())), ctrlC)
	if _, err := m.Result(); !errors.Is(err, ui.ErrCancelled) {
		t.Fatalf("ctrl+c: %v", err)
	}
}

func TestAskRunsTheProgramOverItsInputAndOutput(t *testing.T) {
	var out bytes.Buffer
	a := &Asker{In: strings.NewReader("/va\twww\r"), Out: &out}
	path, err := a.Ask(context.Background(), spec(sample()))
	if err != nil || path != "/var/www" {
		t.Fatalf("ask %q %v", path, err)
	}
	if out.Len() == 0 {
		t.Fatal("nothing was drawn on the output")
	}
}

func TestAskEndsWithTheContextAsAFailureNotAnAnswer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := &Asker{In: strings.NewReader(""), Out: &bytes.Buffer{}}
	if path, err := a.Ask(ctx, spec(sample())); err == nil || path != "" {
		t.Fatalf("ask %q %v", path, err)
	}
}
