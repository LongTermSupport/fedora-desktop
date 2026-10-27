package reclaim

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// action is one numbered menu line (E11).
type action struct {
	key  string
	name string
	do   func(*run, context.Context) bool
}

// actions are the menu's numbered choices, in order.
var actions = []action{
	{"1", "Biggest directories", (*run).bigDirs},
	{"2", "Biggest files", (*run).bigFiles},
	{"3", namePackages, (*run).packages},
	{"4", "Remove old kernels", (*run).oldKernels},
	{"5", nameJournal, (*run).journal},
	{"6", nameContainers, (*run).containers},
	{"7", nameFlatpak, (*run).flatpak},
	{"8", "Clear ~/.cache", (*run).cache},
	{"9", nameTrash, (*run).trash},
	{"10", "Safe sweep", (*run).sweep},
}

// The names of the actions the safe sweep runs too.
const (
	namePackages   = "dnf autoremove + clean all"
	nameJournal    = "Vacuum systemd journal"
	nameContainers = "Prune containers"
	nameFlatpak    = "Flatpak unused runtimes"
	nameTrash      = "Empty trash"
)

const needTerminal = "needs an interactive terminal; run 'reclaim report' for a non-interactive summary."

// menu is E9-E13.
func (r *run) menu(ctx context.Context) term.ExitCode {
	if !r.env.IsTerminal(r.env.Stdin) {
		r.failf(needTerminal)
		return term.ExitFailure
	}
	tty, err := r.env.OpenTTY()
	if err != nil {
		r.failf(needTerminal)
		return term.ExitFailure
	}
	r.tty = newLineReader(tty)
	r.header(ctx)
	code, done := r.loop(ctx)
	r.tty.stop()
	if cerr := tty.Close(); cerr != nil {
		r.failf("close the terminal: %v", cerr)
		return term.ExitFailure
	}
	if done {
		return code
	}
	if r.failed > 0 {
		r.failf("%d action(s) failed in this run; see the messages above.", r.failed)
		return term.ExitFailure
	}
	return term.ExitOK
}

// loop reads choices until the person quits (done false: the run's verdict follows) or the
// run must end now with code (done true).
func (r *run) loop(ctx context.Context) (term.ExitCode, bool) {
	invalid := 0
	for {
		r.printMenu()
		r.prompt("Choose", "[1-10, r, q]:")
		choice, err := r.readLine(ctx)
		if errors.Is(err, errCancelled) {
			return term.ExitCancelled, true
		}
		if errors.Is(err, io.EOF) {
			r.say("")
			r.info("No more input — exiting.")
			return term.ExitOK, false
		}
		if err != nil {
			r.failf("%v", err)
			return term.ExitFailure, true
		}
		switch choice {
		case "":
			continue
		case "q", "Q", "quit", "exit":
			r.okLine("Bye.")
			return term.ExitOK, false
		case "r", "R":
			invalid = 0
			r.header(ctx)
			continue
		}
		if a, ok := findAction(choice); ok {
			invalid = 0
			r.act(ctx, a)
			if ctx.Err() != nil {
				return term.ExitCancelled, true
			}
			continue
		}
		invalid++
		r.errLine(fmt.Sprintf("  Not a valid choice: '%s'. Pick 1-10, r, or q.", choice))
		if invalid >= maxTries {
			r.errLine("Too many invalid choices — exiting.")
			return term.ExitFailure, true
		}
	}
}

func findAction(key string) (action, bool) {
	for _, a := range actions {
		if a.key == key {
			return a, true
		}
	}
	return action{}, false
}

// act runs one action under its heading (E14), counting a failure (E13).
func (r *run) act(ctx context.Context, a action) {
	r.heading(ui.Here("reclaim", a.name))
	if !a.do(r, ctx) && ctx.Err() == nil {
		r.failed++
		r.warn("  This action did not complete.")
	}
}

// header is E10.
func (r *run) header(ctx context.Context) {
	r.heading(ui.Here("reclaim").About(r.hostname() + " · v" + Version))
	if text, ok := r.filesystems(ctx); ok {
		r.show(text)
	}
	journal, ok := r.journalLine(ctx)
	if ok {
		r.say("  " + r.paint(styleDim, journal))
	} else {
		r.say("  " + journal)
	}
	r.say("")
}

// printMenu is E11.
func (r *run) printMenu() {
	section := func(title string) { r.say(r.paint(styleBold, title)) }
	item := func(text, note string) {
		if note == "" {
			r.say(text)
			return
		}
		r.say(text + " " + r.paint(styleDim, note))
	}
	deep := "du summary"
	if r.have("ncdu") {
		deep = "ncdu"
	}
	section("Analyse")
	item("   1) Biggest directories       ", deep)
	item("   2) Biggest files", "")
	r.say(r.paint(styleBold, "Reclaim") + "  " + r.paint(styleDim, "(destructive — each confirms first)"))
	item("   3) dnf autoremove + clean all", "(sudo)")
	item("   4) Remove old kernels        ", fmt.Sprintf("(sudo, keep newest %d)", kernelsKept))
	item("   5) Vacuum systemd journal    ", "(sudo)")
	item("   6) Prune containers          ", r.enginesNote())
	flatpak := "(flatpak absent)"
	if r.have(progFlatpak) {
		flatpak = progFlatpak
	}
	item("   7) Flatpak unused runtimes   ", flatpak)
	item("   8) Clear ~/.cache", "")
	item("   9) Empty trash", "")
	item("  10) Safe sweep (run 3,5,6,7,9)", "")
	section("Other")
	r.say("   r) Refresh report     q) Quit")
	r.say("")
}

func (r *run) enginesNote() string {
	podman, docker := r.have(progPodman), r.have(progDocker)
	switch {
	case podman && docker:
		return "podman + docker"
	case podman:
		return progPodman
	case docker:
		return progDocker
	}
	return "-"
}
