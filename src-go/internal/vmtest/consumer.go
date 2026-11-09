package vmtest

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/consumer"
)

// A consumer-install scenario (Plan 00228): `./run.bash --consumer`, which refuses headless,
// driven attended at a terminal (attend.go), then run again as the update a consumer runs, so
// the checker can judge that a second run changes nothing.

const (
	consumerCheckout = "~/Projects/fedora-desktop"
	// rerunLog is the second run's output, kept in the guest for the checker to judge.
	rerunLog = guestRunState + "/rerun.log"
	// endedStatus is the provisioning status of a run the harness ended for asking what it
	// must never ask, or what it has no answer for: run.bash failed, so the verdict is fail,
	// never error.
	endedStatus = 1
)

// consumerPrompts are the questions an attended consumer install can ask in the lab: the
// optional-app menu, answered with the numbers of picks (Enter for none), and the questions
// refused. The lab's user has passwordless sudo, so a password prompt is a defect, as is any
// request for a GitHub credential the consumer path promises never to make.
func consumerPrompts(picks []string) []prompt {
	menu := prompt{text: "Numbers to install (e.g. 1 3), or Enter for none: ", reply: "\n"}
	if len(picks) > 0 {
		menu.answer = menuAnswer(picks)
	}
	return []prompt{
		menu,
		{text: "[sudo] password for ", refuse: true},
		{text: "BECOME password: ", refuse: true},
		{text: "Your login password (nothing shows as you type): ", refuse: true},
		{text: "Username for 'https://", refuse: true},
		{text: "Password for 'https://", refuse: true},
		{text: "Enter passphrase", refuse: true},
	}
}

// menuLine is one line of the optional-app menu: "  <n>) <label>".
var menuLine = regexp.MustCompile(`^  ([0-9]+)\) (.+)$`)

// menuAnswer reads the optional-app menu, the last block of numbered lines printed before its
// question, and answers with each pick's number, in the picks' order. A pick is found by the
// label the menu gives its file (consumer.MenuLabel), as a common or a hardware play. A pick
// the menu does not offer is an error: answering without it would pass a run that never
// installed it.
func menuAnswer(picks []string) func(before []byte) (string, error) {
	return func(before []byte) (string, error) {
		var labels []string
		misnumbered := "" // the line that broke the block's numbering, if one did
		for line := range strings.SplitSeq(string(before), "\n") {
			m := menuLine.FindStringSubmatch(line)
			switch {
			case m == nil:
				// The question's heading follows the menu; only a new "1)" starts a new block.
			case m[1] == "1":
				labels, misnumbered = append(labels[:0], m[2]), ""
			case m[1] == strconv.Itoa(len(labels)+1) && misnumbered == "":
				labels = append(labels, m[2])
			case misnumbered == "":
				misnumbered = fmt.Sprintf("the optional-app menu is not numbered 1 to %d: %s) %s", len(labels)+1, m[1], m[2])
			}
		}
		if misnumbered != "" {
			return "", errors.New(misnumbered)
		}
		if len(labels) == 0 {
			return "", errors.New("no numbered menu was printed before the optional-app question")
		}
		numbers := make([]string, 0, len(picks))
		for _, pick := range picks {
			i := slices.IndexFunc(labels, func(l string) bool {
				return l == consumer.MenuLabel(pick, false) || l == consumer.MenuLabel(pick, true)
			})
			if i < 0 {
				return "", fmt.Errorf("the optional-app menu offers no %s (%s); it offers %s",
					consumer.MenuLabel(pick, false), pick, strings.Join(labels, ", "))
			}
			numbers = append(numbers, strconv.Itoa(i+1))
		}
		return strings.Join(numbers, " ") + "\n", nil
	}
}

// sessionVars are the autologin session's display and desktop variables, read from the
// session.env the session runner records, so the attended run sees the desktop a terminal in
// that session would; the bus is the session's own, as the checker's is.
var sessionVars = []string{"WAYLAND_DISPLAY", "DISPLAY", "XDG_CURRENT_DESKTOP", "XDG_SESSION_DESKTOP", "XDG_SESSION_TYPE", "DESKTOP_SESSION"}

// inSession is remote run with the session's environment, from the home directory.
func inSession(remote string) string {
	return "export XDG_RUNTIME_DIR=/run/user/$(id -u) DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$(id -u)/bus TERM=dumb" +
		" && while IFS='=' read -r k v; do case \"$k\" in " + strings.Join(sessionVars, "|") +
		") export \"$k=$v\" ;; esac; done <" + guestRunState + "/session.env && cd && " + remote
}

// pinnedCheckout clones the public repository over HTTPS, as run.bash --consumer does, but at
// the pinned commit: run.bash would clone the default branch and then `git pull --ff-only`, so
// the checkout's branch tracks a local ref holding the commit, and that pull changes nothing.
func (r *running) pinnedCheckout() string {
	return "sudo dnf -y -q install git-core && git clone -q '" + r.l.repoURL + "' " + consumerCheckout +
		" && git -C " + consumerCheckout + " branch vmtest-commit '" + r.commit + "'" +
		" && git -C " + consumerCheckout + " checkout -q --track -b vmtest-pinned vmtest-commit"
}

// consumerProvision is the install and its re-run. It returns the first non-zero status, or
// endedStatus for a run the attendant ended.
func (r *running) consumerProvision(ctx context.Context) (int, error) {
	// The person types in the session, so it must be there, with its environment recorded.
	if err := r.waitSession(ctx, "test -r "+guestRunState+"/session.env", " with a session.env recorded"); err != nil {
		return 0, err
	}
	if err := r.g.do(ctx, r.pinnedCheckout()); err != nil {
		return 0, err
	}
	r.note("# harness: the checkout cloned over HTTPS and pinned at " + r.commit[:12] + ", where run.bash --consumer pulls it\n")
	// The install picks the scenario's optional plays; the re-run, an update that adds nothing,
	// answers the menu with Enter, so its recaps are the main install's alone.
	runs := []struct {
		what, remote string
		picks        []string
	}{
		{"~/run.bash --consumer", "~/run.bash --consumer", r.s.OptionalPlays},
		{consumerCheckout + "/run.bash, the update a consumer runs", "set -o pipefail && " + consumerCheckout + "/run.bash 2>&1 | tee " + rerunLog, nil},
	}
	for i, run := range runs {
		which := fmt.Sprintf("attended run %d of %d", i+1, len(runs))
		r.note("# harness: " + which + ": " + run.what + "\n")
		if len(run.picks) > 0 {
			r.note("# harness: the optional-app menu is answered with " + strings.Join(run.picks, ", ") + "\n")
		}
		rc, h, err := r.g.attended(ctx, inSession(run.remote), r.tee(), consumerPrompts(run.picks))
		if err != nil {
			return 0, err
		}
		if h.refused != "" {
			r.note(fmt.Sprintf("\nERROR: %s asked %q, which a consumer install must never ask here; the harness ended it\n", which, h.refused))
			return endedStatus, nil
		}
		if h.unanswerable != "" {
			r.note(fmt.Sprintf("\nERROR: %s asked a question the harness could not answer: %s; the harness ended it\n", which, h.unanswerable))
			return endedStatus, nil
		}
		if h.unanswered != "" {
			r.note(fmt.Sprintf("\nERROR: %s stopped at a question the harness has no answer for, and printed nothing for %ds; the harness ended it. Its last lines: %q\n",
				which, int(promptIdle.Seconds()), h.unanswered))
			return endedStatus, nil
		}
		if rc != 0 {
			return rc, nil
		}
	}
	return 0, nil
}
