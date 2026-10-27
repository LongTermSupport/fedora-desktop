package vmtest

import (
	"context"
	"fmt"
	"strings"
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

// consumerPrompts are the questions an attended consumer install can ask in the lab. The
// lab's user has passwordless sudo, so a password prompt is a defect, as is any request for a
// GitHub credential the consumer path promises never to make.
var consumerPrompts = []prompt{
	{text: "Numbers to install (e.g. 1 3), or Enter for none: ", reply: "\n"},
	{text: "[sudo] password for ", refuse: true},
	{text: "BECOME password: ", refuse: true},
	{text: "Your login password (nothing shows as you type): ", refuse: true},
	{text: "Username for 'https://", refuse: true},
	{text: "Password for 'https://", refuse: true},
	{text: "Enter passphrase", refuse: true},
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
	runs := []struct{ what, remote string }{
		{"~/run.bash --consumer", "~/run.bash --consumer"},
		{consumerCheckout + "/run.bash, the update a consumer runs", "set -o pipefail && " + consumerCheckout + "/run.bash 2>&1 | tee " + rerunLog},
	}
	for i, run := range runs {
		which := fmt.Sprintf("attended run %d of %d", i+1, len(runs))
		r.note("# harness: " + which + ": " + run.what + "\n")
		rc, h, err := r.g.attended(ctx, inSession(run.remote), r.tee(), consumerPrompts)
		if err != nil {
			return 0, err
		}
		if h.refused != "" {
			r.note(fmt.Sprintf("\nERROR: %s asked %q, which a consumer install must never ask here; the harness ended it\n", which, h.refused))
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
