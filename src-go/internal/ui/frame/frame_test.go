package frame

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTitleJoinsTheCrumbsInOrder(t *testing.T) {
	got := ansi.Strip(Title([]string{"Teams", "alpha", "Add an LXC member"}))
	if got != "Teams › alpha › Add an LXC member" {
		t.Fatalf("title = %q", got)
	}
}

// The last crumb is where the person is, so it alone wears the accent: bold, terminal colour 3.
func TestTitleMarksOnlyTheLastCrumb(t *testing.T) {
	got := Title([]string{"Teams", "alpha"})
	if !strings.HasPrefix(got, "Teams › ") {
		t.Fatalf("the crumbs before the last are plain, got %q", got)
	}
	last := strings.TrimPrefix(got, "Teams › ")
	if last == "alpha" || ansi.Strip(last) != "alpha" {
		t.Fatalf("the last crumb is styled, got %q", last)
	}
	if !strings.Contains(last, "1") || !strings.Contains(last, "33") {
		t.Fatalf("the last crumb is bold (1) in colour 3 (33), got %q", last)
	}
}

func TestTitleOfOneCrumbIsThatCrumb(t *testing.T) {
	if got := ansi.Strip(Title([]string{"Sessions"})); got != "Sessions" {
		t.Fatalf("title = %q", got)
	}
}

func TestTitleOfNoCrumbsIsEmpty(t *testing.T) {
	if got := Title(nil); got != "" {
		t.Fatalf("title = %q", got)
	}
}

func TestModelStartsWithNoCommand(t *testing.T) {
	if cmd := New("Sessions").Init(); cmd != nil {
		t.Fatal("Init returns no command")
	}
}

// The view is the title and, below it, the one action drawn with its key: a key is never the
// only control surface (InteractiveScripts.md rule 14).
func TestViewDrawsTheTitleAndTheQuitAction(t *testing.T) {
	view := ansi.Strip(New("Teams", "alpha").View().Content)
	lines := strings.Split(view, "\n")
	if lines[0] != "Teams › alpha" {
		t.Fatalf("first line = %q, want the title", lines[0])
	}
	if !strings.Contains(lines[len(lines)-1], "q quit") {
		t.Fatalf("last line = %q, want the drawn quit action", lines[len(lines)-1])
	}
}

func TestQuitKeyEndsTheProgram(t *testing.T) {
	_, cmd := New("Sessions").Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("q returns a command")
	}
	if _, isQuit := cmd().(tea.QuitMsg); !isQuit {
		t.Fatal("q returns tea.Quit")
	}
}

func TestOtherMessagesChangeNothing(t *testing.T) {
	for _, msg := range []tea.Msg{tea.KeyPressMsg{Code: 'x', Text: "x"}, tea.WindowSizeMsg{Width: 80, Height: 24}} {
		next, cmd := New("Sessions").Update(msg)
		if cmd != nil {
			t.Errorf("%T returns a command", msg)
		}
		if ansi.Strip(next.View().Content) != ansi.Strip(New("Sessions").View().Content) {
			t.Errorf("%T changed the view", msg)
		}
	}
}
