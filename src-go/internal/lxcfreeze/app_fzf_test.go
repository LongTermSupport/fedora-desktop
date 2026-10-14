package lxcfreeze

// The menus through fzf (D2: the legacy flags, kept for parity). The Fake stands in for fzf: it
// records the argv and the rows on stdin, and answers by choosing rows or by exiting.

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
)

// fzfAnswer is one fzf run: the zero-based rows chosen, or a non-zero exit (1 is no match, 130
// is Esc or Ctrl-C, anything else is a crash).
type fzfAnswer struct {
	picks []int
	exit  int
}

type fzfRun struct {
	args []string
	rows []string
}

func scriptFzf(t *testing.T, h *harness, answers ...fzfAnswer) *[]fzfRun {
	t.Helper()
	var runs []fzfRun
	h.fake.OnFunc([]string{"fzf"}, func(c execx.Cmd) (execx.Result, error) {
		raw, err := io.ReadAll(c.Stdin)
		if err != nil {
			return execx.Result{}, err
		}
		rows := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
		runs = append(runs, fzfRun{args: c.Args, rows: rows})
		if len(runs) > len(answers) {
			t.Errorf("fzf run %d was not scripted (rows %q)", len(runs), rows)
			return execx.Result{ExitCode: 130}, nil
		}
		answer := answers[len(runs)-1]
		if answer.exit != 0 {
			return execx.Result{ExitCode: answer.exit}, nil
		}
		var chosen []string
		for _, i := range answer.picks {
			chosen = append(chosen, rows[i])
		}
		return execx.Result{Stdout: []byte(strings.Join(chosen, "\n") + "\n")}, nil
	})
	return &runs
}

func TestFzfGroupAndDrillDownUseTheLegacyArgvAndTheChoiceIsActedOn(t *testing.T) {
	h := newHarness(t)
	h.withTTY("")
	// group menu: row 2 (bridge: lxcbr0); drill-down: the all row; then Esc at the menu.
	runs := scriptFzf(t, h, fzfAnswer{picks: []int{1}}, fzfAnswer{picks: []int{0}}, fzfAnswer{exit: 130})
	mustExit(t, h, 0)
	eqStrings(t, "acts", h.acts(), []string{"freeze alpha"})
	got := *runs
	eqStrings(t, "group argv", got[0].args, []string{
		"--height=50%", "--reverse", "--prompt=lxcfreeze: ",
		"--header=ENTER acts on the chosen group — ESC to quit",
	})
	eqStrings(t, "drill-down argv", got[1].args, []string{
		"--multi", "--height=60%", "--reverse", "--prompt=In this group: ",
		"--header=ENTER on the first row acts on all — TAB picks specific ones, ESC goes back",
	})
	if len(got[0].rows) != 3 || got[0].rows[0] != menuLabel("all LXC containers", "FREEZE 2") {
		t.Fatalf("the labels, one per line, go on stdin: %q", got[0].rows)
	}
	if got[1].rows[0] != fmt.Sprintf("%-34s %s", freeze.AllRowKey, "FREEZE 1") {
		t.Fatalf("the first drill-down row acts on all: %q", got[1].rows[0])
	}
}

func TestFzfTabPickedRowsActOnlyOnThoseContainers(t *testing.T) {
	h := newHarness(t)
	h.withTTY("")
	// "all LXC containers", then TAB-pick alpha and bravo (rows 4 and 6 of the drill-down).
	scriptFzf(t, h, fzfAnswer{picks: []int{0}}, fzfAnswer{picks: []int{3, 5}}, fzfAnswer{exit: 130})
	mustExit(t, h, 0)
	eqStrings(t, "acts", h.acts(), []string{"freeze alpha", "freeze bravo"})
}

func TestFzfEscInTheDrillDownGoesBackToTheMenu(t *testing.T) {
	h := newHarness(t)
	h.withTTY("")
	runs := scriptFzf(t, h, fzfAnswer{picks: []int{1}}, fzfAnswer{exit: 130}, fzfAnswer{exit: 1})
	mustExit(t, h, 0)
	if len(h.acts()) != 0 || len(*runs) != 3 {
		t.Fatalf("back then quit, nothing acted on: acts %v, runs %d", h.acts(), len(*runs))
	}
}

// Bug 2: fzf crashing (exit 2, no tty) was treated as a quiet quit with exit 0.
func TestAFzfThatCrashedIsAnErrorNotAQuietQuit(t *testing.T) {
	h := newHarness(t)
	h.withTTY("")
	scriptFzf(t, h, fzfAnswer{exit: 2})
	mustExit(t, h, 1)
	if !strings.Contains(h.errb.String(), "lxcfreeze: fzf: fzf exited 2") {
		t.Fatalf("got %q", h.errb)
	}
}
