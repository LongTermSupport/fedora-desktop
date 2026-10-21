package ui

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func lines(input string) (*Lines, *bytes.Buffer) {
	var out bytes.Buffer
	return &Lines{In: strings.NewReader(input), Out: &out, Width: 80}, &out
}

// asker is Lines over input whose output the test does not read.
func asker(input string) *Lines {
	return &Lines{In: strings.NewReader(input), Out: &bytes.Buffer{}, Width: 80}
}

var here = Here("ccy", "Relabel")

// The answer cases of the relabel preflight (scripts/test-ccy-relabel-preflight.bash): Enter is
// the default, three answers that are neither y nor n give up, a bad answer then y is a yes.
func TestYesNo(t *testing.T) {
	cases := []struct {
		name, input string
		def, want   bool
		asked       int
	}{
		{"y", "y\n", false, true, 1},
		{"Yes", "Yes\n", false, true, 1},
		{"n", "n\n", true, false, 1},
		{"NO", "NO\n", true, false, 1},
		{"Enter alone is the default no", "\n", false, false, 1},
		{"Enter alone is the default yes", "\n", true, true, 1},
		{"spaces around the answer", "  y \n", false, true, 1},
		{"a bad answer then y", "maybe\ny\n", false, true, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l, out := lines(c.input)
			got, err := l.YesNo(here, "Run the fix now?", c.def)
			if err != nil || got != c.want {
				t.Fatalf("YesNo = %v, %v; want %v", got, err, c.want)
			}
			if n := strings.Count(out.String(), "Run the fix now?"); n != c.asked {
				t.Fatalf("asked %d times, want %d:\n%s", n, c.asked, out.String())
			}
		})
	}
}

func TestYesNoShowsTheDefaultAndTheHeading(t *testing.T) {
	l, out := lines("maybe\nn\n")
	if _, err := l.YesNo(here, "Run the fix now?", false); err != nil {
		t.Fatal(err)
	}
	want := "ccy › Relabel\n" + strings.Repeat("─", 24) + "\nRun the fix now? [y/N] " + "   Answer y or n.\n" + "Run the fix now? [y/N] "
	if out.String() != want {
		t.Fatalf("YesNo wrote\n%q\nwant\n%q", out.String(), want)
	}
	l, out = lines("y\n")
	if _, err := l.YesNo(here, "Start it?", true); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.String(), "Start it? [Y/n] ") {
		t.Fatalf("a default yes is shown as [Y/n]: %q", out.String())
	}
}

func TestYesNoGivesUpAfterThreeAnswers(t *testing.T) {
	l, out := lines("maybe\nwhat\nhuh\ny\n")
	_, err := l.YesNo(here, "Run the fix now?", false)
	gaveUp, ok := errors.AsType[*GaveUpError](err)
	if !ok || gaveUp.Tries != 3 {
		t.Fatalf("YesNo error = %v, want a give-up after 3", err)
	}
	if err.Error() != "no valid answer after 3 tries" {
		t.Fatalf("the give-up says %q", err.Error())
	}
	if n := strings.Count(out.String(), "Answer y or n."); n != 3 {
		t.Fatalf("each bad answer is re-asked: %d", n)
	}
}

func TestEndOfInputCancels(t *testing.T) {
	for _, input := range []string{"", "partial line with no newline"} {
		l, out := lines(input)
		_, err := l.YesNo(here, "Run the fix now?", false)
		if !errors.Is(err, ErrInputEnded) || !errors.Is(err, ErrCancelled) {
			t.Fatalf("YesNo at the end of input = %v, want ErrInputEnded", err)
		}
		if !strings.HasSuffix(out.String(), "\n") {
			t.Fatalf("the end of input leaves the prompt line open: %q", out.String())
		}
	}
}

func TestATriesSettingIsKept(t *testing.T) {
	l := asker("x\nx\n")
	l.Tries = 2
	if _, err := l.YesNo(here, "Q?", false); !strings.Contains(fmt.Sprint(err), "after 2 tries") {
		t.Fatalf("Tries 2 = %v", err)
	}
}

func TestNoHeadingIsRefusedBeforeReading(t *testing.T) {
	l, out := lines("y\n")
	if _, err := l.YesNo(Heading{}, "Q?", false); !errors.Is(err, ErrNoHeading) {
		t.Fatalf("YesNo with no heading = %v", err)
	}
	if _, err := l.Ask(Heading{}, "Name", nil); !errors.Is(err, ErrNoHeading) {
		t.Fatalf("Ask with no heading = %v", err)
	}
	if _, err := l.Choose(Heading{}, "Pick", []Choice{{Key: "1", Label: "a"}}); !errors.Is(err, ErrNoHeading) {
		t.Fatalf("Choose with no heading = %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("a refused question drew: %q", out.String())
	}
}

func TestAskRepromptsUntilTheCheckPasses(t *testing.T) {
	l, out := lines("bad name\nwork\n")
	check := func(s string) error {
		if strings.Contains(s, " ") {
			return errors.New("a name has no spaces")
		}
		return nil
	}
	got, err := l.Ask(Here("ccy", "Token"), "Token name", check)
	if err != nil || got != "work" {
		t.Fatalf("Ask = %q, %v", got, err)
	}
	if !strings.Contains(out.String(), "Token name: ") || !strings.Contains(out.String(), "   a name has no spaces\n") {
		t.Fatalf("Ask wrote %q", out.String())
	}
	if got, err := asker("anything\n").Ask(Here("ccy"), "Text", nil); err != nil || got != "anything" {
		t.Fatalf("Ask with no check = %q, %v", got, err)
	}
}

var tokenMenu = []Choice{{Key: "1", Label: "fresh (expires: 2027-10-21)"}, {Key: "d", Label: "Use the Desktop account"}}

// The menu cases of scripts/test-ccy-token-mode.bash: a valid pick selects, the Desktop line is
// a visible choice, EOF cancels, runaway invalid input gives up.
func TestLinesChoose(t *testing.T) {
	l, out := lines("1\n")
	got, err := l.Choose(Here("cc", "Token"), "Choose a token", tokenMenu)
	if err != nil || got != "1" {
		t.Fatalf("Choose = %q, %v", got, err)
	}
	want := "cc › Token\n" + strings.Repeat("─", 24) + "\n  1) fresh (expires: 2027-10-21)\n  d) Use the Desktop account\nChoose a token: "
	if out.String() != want {
		t.Fatalf("Choose wrote\n%q\nwant\n%q", out.String(), want)
	}
	if got, err := asker("d\n").Choose(Here("cc", "Token"), "Choose a token", tokenMenu); err != nil || got != "d" {
		t.Fatalf("the Desktop line = %q, %v", got, err)
	}
	if _, err := asker("").Choose(Here("cc", "Token"), "Choose a token", tokenMenu); !errors.Is(err, ErrInputEnded) {
		t.Fatalf("EOF at the menu = %v", err)
	}
	l, out = lines("9\n9\n9\n9\n9\n9\n")
	if _, err := l.Choose(Here("cc", "Token"), "Choose a token", tokenMenu); !strings.Contains(fmt.Sprint(err), "after 3 tries") {
		t.Fatalf("runaway invalid input = %v", err)
	}
	if !strings.Contains(out.String(), "   Choose one of: 1, d.\n") {
		t.Fatalf("a bad pick names the choices: %q", out.String())
	}
}

func TestChooseRefusesAMenuThatCannotBeAnswered(t *testing.T) {
	bad := [][]Choice{nil, {{Key: "", Label: "x"}}, {{Key: "1", Label: ""}}, {{Key: "1", Label: "a"}, {Key: "1", Label: "b"}}}
	for _, menu := range bad {
		if _, err := asker("1\n").Choose(Here("cc"), "Pick", menu); err == nil {
			t.Errorf("Choose accepted the menu %+v", menu)
		}
	}
}

func TestAFailedWriteIsAnError(t *testing.T) {
	l := &Lines{In: strings.NewReader("y\n"), Out: brokenWriter{}}
	if _, err := l.YesNo(here, "Q?", false); err == nil {
		t.Fatal("a question nobody could see was answered")
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestAReadThatFailsIsAnError(t *testing.T) {
	broken := errors.New("read failed")
	l := &Lines{In: failingReader{broken}, Out: &bytes.Buffer{}}
	if _, err := l.YesNo(here, "Q?", false); !errors.Is(err, broken) || errors.Is(err, ErrInputEnded) {
		t.Fatalf("a failed read = %v", err)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }
