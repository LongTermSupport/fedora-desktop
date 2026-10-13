package sessions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// rebootParams is a parsed `reboot`.
type rebootParams struct {
	minutes int
	dryRun  bool
}

// parseReboot reads the arguments after `reboot`. A mistake is returned as its usage sentence.
func parseReboot(args []string) (rebootParams, string) {
	var p rebootParams
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--in":
			if i+1 >= len(args) || args[i+1] == "" {
				return p, "--in needs a number of minutes."
			}
			minutes, problem := minutesOrUsage(args[i+1], "--in")
			if problem != "" {
				return p, problem
			}
			p.minutes = minutes
			i++
		case flagDryRun:
			p.dryRun = true
		default:
			return p, fmt.Sprintf("unknown reboot option '%s'.", args[i])
		}
	}
	if p.minutes == 0 {
		return p, "'reboot' needs --in N."
	}
	return p, ""
}

// rebootPermitted says whether logind lets this account reboot with nobody to authenticate.
// From a login with no terminal polkit refuses `systemctl reboot` ("interactive authentication
// required"), and asked only at the end that refusal would come after every session had been
// warned twice. So it is asked first. "yes" is the only answer that reboots unattended;
// anything else is a refusal that names the way round it.
func (a *App) rebootPermitted(ctx context.Context) bool {
	out, err := execx.Combined(ctx, a.Runner, execx.Cmd{Name: "busctl", Args: []string{
		"--system", "call", "org.freedesktop.login1", "/org/freedesktop/login1",
		"org.freedesktop.login1.Manager", "CanReboot",
	}})
	out = strings.TrimRight(out, "\n")
	if err != nil {
		if out == "" {
			out = err.Error()
		}
		a.fail("could not ask logind whether this account may reboot: " + out)
		return false
	}
	// busctl prints a string reply as its signature and the quoted value: s "yes".
	answer := strings.TrimSuffix(strings.TrimPrefix(out, `s "`), `"`)
	switch answer {
	case "yes":
		return true
	case "challenge":
		a.fail("this account may reboot only after authenticating to polkit (logind CanReboot: challenge). The reboot comes after the countdown, when nobody may be there to answer, and from a login with no terminal polkit refuses it outright.")
	default:
		a.fail("logind does not let this account reboot (logind CanReboot: " + answer + "), so the reboot would be refused after every session had been warned.")
	}
	a.sayf("Reboot as root instead, which warns the sessions the same way: sudo /usr/local/bin/reboot-with-update --in N (it applies updates first). Or run ccy-sessions reboot from the machine's own desktop session.\n")
	return false
}

// rebootRun is one reboot in progress: what to tell the sessions, the signals that may cancel
// it, and what has happened so far.
type rebootRun struct {
	rebootParams

	kind     string
	sig      <-chan os.Signal
	stop     func()
	warned   bool
	failed   bool
	projects []projectInfo
}

// reboot warns every live project, counts down, warns again at one minute, then reboots. All of
// what would stop it is found before anyone is told anything. Once a warning may have gone
// out, any exit other than the reboot itself (an interrupt, a project refusing a warning,
// systemctl refusing) tells the sessions the reboot is off.
func (a *App) reboot(ctx context.Context, args []string) int {
	params, problem := parseReboot(args)
	if problem != "" {
		return a.usageError(problem)
	}
	run := &rebootRun{rebootParams: params, kind: a.goingDownKind(), stop: func() {}}
	// Everything that would stop the reboot is found before anyone is warned, and all of it
	// at once: whether this account may reboot unattended, and every session that cannot be
	// warned. A dry run checks the same, so it never says it would reboot when it would not.
	refused := !a.rebootPermitted(ctx)
	projects, err := a.checkProjects(ctx, run.kind)
	if err != nil {
		refused = true
	}
	if refused {
		a.sayf("Nothing was signalled and nothing rebooted.\n")
		return term.ExitFailure
	}
	run.projects = projects
	if !params.dryRun {
		// Armed BEFORE the first warning goes out, so an interrupt that lands mid-loop, with
		// some projects warned and some not, still cancels. A cancellation reaching a project
		// that was never warned is harmless; a warned project left uncancelled is not.
		run.sig, run.stop = a.Signals()
		defer run.stop()
	}
	code := a.rebootSteps(ctx, run)
	if code != term.ExitOK && run.warned {
		a.withdraw(ctx, run)
	}
	return code
}

// interrupted reports whether an INT or TERM is waiting.
func interrupted(sig <-chan os.Signal) bool {
	select {
	case <-sig:
		return true
	default:
		return false
	}
}

// cancelled is the person (or the system) cancelling the countdown.
func (a *App) cancelled(run *rebootRun) int {
	run.stop()
	a.sayf("\nReboot cancelled.\n")
	return term.ExitCancelled
}

// rebootSteps is the warnings, the countdown and the reboot itself.
func (a *App) rebootSteps(ctx context.Context, run *rebootRun) int {
	run.warned = !run.dryRun
	reached, err := a.deliverSignal(ctx, run.projects, run.kind, run.minutes, run.dryRun)
	if err != nil {
		return term.ExitFailure
	}
	if interrupted(run.sig) {
		return a.cancelled(run)
	}
	a.sayf("Rebooting in %d minute(s); %d project(s) warned.\n", run.minutes, reached)
	if run.minutes > 1 {
		if code, stop := a.secondWarning(ctx, run); stop {
			return code
		}
	}
	if run.dryRun {
		a.sayf("would reboot (systemctl reboot)\n")
		return term.ExitOK
	}
	if a.Pause(a.cfg.Minute, run.sig) {
		return a.cancelled(run)
	}
	return a.systemctlReboot(ctx, run)
}

// secondWarning waits until one minute is left and warns again, through a fresh check: a
// session started meanwhile is warned too, and a project that broke meanwhile stops the
// reboot. It says whether the reboot ends here, with the code.
func (a *App) secondWarning(ctx context.Context, run *rebootRun) (int, bool) {
	if run.dryRun {
		a.sayf("would wait %d minute(s), then warn again at one minute\n", run.minutes-1)
		return term.ExitOK, false
	}
	wait := time.Duration(run.minutes-1) * a.cfg.Minute
	if a.Pause(wait, run.sig) {
		return a.cancelled(run), true
	}
	if _, err := a.signalProjects(ctx, run.kind, 1, false); err != nil {
		return term.ExitFailure, true
	}
	if interrupted(run.sig) {
		return a.cancelled(run), true
	}
	a.sayf("Rebooting in 1 minute.\n")
	return term.ExitOK, false
}

// systemctlReboot asks systemd to reboot. From here an INT or TERM is the shutdown reaching
// this process, not a cancellation, so the channel is simply no longer read. A refused reboot
// is a failure, reported as one: nobody cancelled it.
func (a *App) systemctlReboot(ctx context.Context, run *rebootRun) int {
	_, err := a.Runner.Run(ctx, execx.Cmd{Name: "systemctl", Args: []string{cmdReboot}, Stdout: a.Stderr, Stderr: a.Stderr})
	if err == nil {
		return term.ExitOK
	}
	run.failed = true
	exit, ok := errors.AsType[*execx.ExitError](err)
	if !ok {
		a.fail("the reboot failed: " + err.Error() + ". The machine is staying up.")
		return term.ExitFailure
	}
	a.fail(fmt.Sprintf("the reboot failed: systemctl reboot exited %d (its reason is above). The machine is staying up.", exit.Code))
	return exit.Code
}

// withdraw tells the sessions the reboot they were warned of is not happening. It carries on
// past a project that refuses.
func (a *App) withdraw(ctx context.Context, run *rebootRun) {
	if run.failed {
		a.sayf("Telling the sessions the warned reboot is not happening.\n")
	} else {
		a.sayf("The machine is staying up; telling the sessions the reboot is off.\n")
	}
	if _, err := a.signalProjects(ctx, kindCancelled, 0, false); err != nil {
		a.fail("the sessions could not all be told; some still expect the reboot.")
	}
}
