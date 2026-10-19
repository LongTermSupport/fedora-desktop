package teamtest

import (
	"io"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

func TestInteractiveMakesTheHostATerminalWithScriptedFzfAndGum(t *testing.T) {
	w := Populated(t)
	c := Interactive(t, w)
	if !w.Host.Term.IsTerminal(w.Host.Stdin) || w.Env["NO_COLOR"] != "1" {
		t.Fatal("the host is not on a plain terminal")
	}
	c.Script.Gum = []string{"YES", "NO", "CANCEL"}
	for _, want := range []bool{true, false} {
		yes, err := w.Host.Gum.Confirm(t.Context(), ui.Here("Teams"), "Sure?", true)
		if err != nil || yes != want {
			t.Fatalf("%v %v", yes, err)
		}
	}
	if _, err := w.Host.Gum.Confirm(t.Context(), ui.Here("Teams"), "Sure?", true); err == nil {
		t.Fatal("CANCEL is Ctrl-C")
	}
	if len(c.Script.GumLog) != 3 {
		t.Fatalf("gum log %v", c.Script.GumLog)
	}
}

// The heading of each screen is kept apart from what the screen asks: Headings and HeadingAt
// read it, and GumLog and HeaderOf read the screen without it.
func TestEveryScreensHeadingIsRecordedApartFromWhatItAsks(t *testing.T) {
	w := Populated(t)
	c := Interactive(t, w)
	c.Script.Gum = []string{"YES"}
	c.Script.Fzf = []string{"|EXIT"}
	if _, err := w.Host.Gum.Confirm(t.Context(), ui.Here("Teams", "alpha").About("team alpha"), "Sure?", true); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Host.Picker.Pick(t.Context(), ui.Here("Teams").AtStep(2, 3, "Seat"), ui.PickSpec{Title: "t", Header: "legend", Rows: []string{"r"}}); err == nil {
		t.Fatal("the Exit row leaves")
	}
	c.Script.HeadingAt(0, "Teams › alpha\nteam alpha")
	c.Script.HeadingAt(1, "Teams · step 2/3 Seat")
	if c.Script.GumLog[0] != "gum confirm --default=true Sure?" {
		t.Fatalf("gum log %q", c.Script.GumLog)
	}
	if c.HeaderOf(0) != "legend\n" {
		t.Fatalf("header %q", c.HeaderOf(0))
	}
	if AfterHeading("no heading here") != "no heading here" || HeadingOf("no heading here") != "" {
		t.Fatal("text with no heading is left whole")
	}
}

func TestScriptedFzfAnswersWithTheChosenRowAndKey(t *testing.T) {
	w := Populated(t)
	c := Interactive(t, w)
	c.Script.Fzf = []string{"ctrl-p|beta", "|ABORT"}
	run := func() (execx.Result, error) {
		return c.Script.Run(t.Context(), execx.Cmd{
			Name: "fzf", Args: []string{"--expect=ctrl-p"}, Stdin: strings.NewReader("alpha one\nbeta two\n"),
		})
	}
	res, err := run()
	if err != nil || string(res.Stdout) != "ctrl-p\nbeta two\n" {
		t.Fatalf("%q %v", res.Stdout, err)
	}
	if _, err = run(); err == nil {
		t.Fatal("ABORT is Esc")
	}
	if len(c.Script.Rows) != 2 || len(c.Script.FzfArgs) != 2 {
		t.Fatalf("rows %v", c.Script.Rows)
	}
}

func TestScriptedTypedFzfHandsBackTheQueryFirstOrWhatWasTyped(t *testing.T) {
	w := Populated(t)
	c := Interactive(t, w)
	c.Script.Fzf = []string{"tab|beta", "tab|TYPED:/srv/wo"}
	run := func() (execx.Result, error) {
		return c.Script.Run(t.Context(), execx.Cmd{
			Name: "fzf", Args: []string{"--print-query", "--query=be", "--expect=tab"},
			Stdin: strings.NewReader("alpha one\nbeta two\n"),
		})
	}
	res, err := run()
	if err != nil || string(res.Stdout) != "be\ntab\nbeta two\n" {
		t.Fatalf("%q %v", res.Stdout, err)
	}
	res, err = run()
	if err == nil || res.ExitCode != 1 || string(res.Stdout) != "/srv/wo\ntab\n" {
		t.Fatalf("typed text that matches no row: %q %v", res.Stdout, err)
	}
}

func TestScriptedFzfMarksSeveralRowsWhenTheChoicesAreJoinedWithAmpersands(t *testing.T) {
	w := Populated(t)
	c := Interactive(t, w)
	c.Script.Fzf = []string{"|alpha&&gamma"}
	res, err := c.Script.Run(t.Context(), execx.Cmd{
		Name: "fzf", Args: []string{"--multi"}, Stdin: strings.NewReader("alpha one\nbeta two\ngamma three\n"),
	})
	if err != nil || string(res.Stdout) != "alpha one\ngamma three\n" {
		t.Fatalf("%q %v", res.Stdout, err)
	}
}

func TestInteractiveRecordsTheLauncherAndReadsTheTerminalFromTTY(t *testing.T) {
	w := Populated(t)
	c := Interactive(t, w)
	if err := w.Host.Exec("/x/launcher", []string{"ccy"}, "/top"); err != nil {
		t.Fatal(err)
	}
	if len(c.Execs) != 1 || c.Execs[0].Dir != "/top" || c.Execs[0].Path != "/x/launcher" {
		t.Fatalf("execs %+v", c.Execs)
	}
	c.TTY = "line\n"
	tty, err := w.Host.OpenTTY()
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(tty)
	if err != nil || string(got) != "line\n" {
		t.Fatalf("%q %v", got, err)
	}
	path, err := w.Host.LookPath("fzf")
	if err != nil || path != "/usr/bin/fzf" {
		t.Fatalf("%q %v", path, err)
	}
}

func TestScriptPrintsWithGumStyleWithoutTakingAnAnswer(t *testing.T) {
	w := Populated(t)
	c := Interactive(t, w)
	c.Script.Gum = []string{"YES"}
	c.Script.SkipStyle = true
	if err := w.Host.Gum.Box(t.Context(), true, "Title", "", "body"); err != nil {
		t.Fatal(err)
	}
	if err := w.Host.Gum.Explain(t.Context(), "words", 5); err != nil {
		t.Fatal(err)
	}
	if len(c.Script.Gum) != 1 || len(c.Script.GumLog) != 0 || len(c.Script.Styled) != 2 {
		t.Fatalf("answers left %v, questions %v, styled %v", c.Script.Gum, c.Script.GumLog, c.Script.Styled)
	}
	if yes, err := w.Host.Gum.Confirm(t.Context(), ui.Here("Teams"), "Sure?", true); err != nil || !yes {
		t.Fatalf("%v %v", yes, err)
	}
}

func TestScriptAnswersTheTerminalWidthWith80Columns(t *testing.T) {
	c := Interactive(t, Populated(t))
	res, err := c.Script.Run(t.Context(), execx.Cmd{Name: "tput", Args: []string{"cols"}})
	if err != nil || string(res.Stdout) != "80\n" {
		t.Fatalf("%q %v", res.Stdout, err)
	}
}

func TestHeaderOfIsTheHeaderFzfWasGiven(t *testing.T) {
	w := Populated(t)
	c := Interactive(t, w)
	c.Script.FzfArgs = [][]string{{"--border", "--header=keys\nrows"}, {"--border"}}
	if got := c.HeaderOf(0); got != "keys\nrows" {
		t.Fatalf("got %q", got)
	}
	if got := c.HeaderOf(1); got != "" {
		t.Fatalf("got %q", got)
	}
}
