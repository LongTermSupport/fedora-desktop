package teamtest

import (
	"io"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestInteractiveMakesTheHostATerminalWithScriptedFzfAndGum(t *testing.T) {
	w := Populated(t)
	c := Interactive(t, w)
	if !w.Host.Term.IsTerminal(w.Host.Stdin) || w.Env["NO_COLOR"] != "1" {
		t.Fatal("the host is not on a plain terminal")
	}
	c.Script.Gum = []string{"YES", "NO", "CANCEL"}
	for _, want := range []bool{true, false} {
		yes, err := w.Host.Gum.Confirm(t.Context(), "Sure?", true)
		if err != nil || yes != want {
			t.Fatalf("%v %v", yes, err)
		}
	}
	if _, err := w.Host.Gum.Confirm(t.Context(), "Sure?", true); err == nil {
		t.Fatal("CANCEL is Ctrl-C")
	}
	if len(c.Script.GumLog) != 3 {
		t.Fatalf("gum log %v", c.Script.GumLog)
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
