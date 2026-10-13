package sessions

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

var wholeMinutes = regexp.MustCompile(`^[1-9][0-9]*$`)

// minutesOrUsage is value as a number of minutes, or the usage sentence naming the flag.
func minutesOrUsage(value, flag string) (int, string) {
	n, err := strconv.Atoi(value)
	if !wholeMinutes.MatchString(value) || err != nil {
		return 0, fmt.Sprintf("%s needs a whole number of minutes, at least 1; got '%s'.", flag, value)
	}
	return n, ""
}

// goingDownKind is the warning kind that tells this user's sessions the truth about what
// follows: reboot-warning when their restore unit is enabled (the wants-symlink the play
// manages), shutdown-warning when it is not. A link that points at nothing pulls in nothing at
// boot, so it counts as off.
func (a *App) goingDownKind() string {
	wants := a.Getenv("HOME") + "/.config/systemd/user/default.target.wants/ccy-sessions-restore.service"
	if _, err := os.Stat(wants); err == nil {
		return "reboot-warning"
	}
	return "shutdown-warning"
}

// notifyArgs is a parsed `notify`.
type notifyArgs struct {
	kind    string
	minutes int
	dryRun  bool
}

// parseNotify reads the arguments after `notify`. A mistake is returned as its usage sentence.
func parseNotify(args []string) (notifyArgs, string) {
	kind := ""
	if len(args) > 0 {
		kind = args[0]
	}
	switch kind {
	case "going-down", "reboot-warning", "shutdown-warning":
		return parseWarning(kind, args)
	case kindCancelled:
		if len(args) > 1 {
			return notifyArgs{}, "'notify reboot-cancelled' takes no further arguments."
		}
		return notifyArgs{kind: kind}, ""
	case "":
		return notifyArgs{}, "'notify' needs a signal kind: going-down, reboot-warning, shutdown-warning or reboot-cancelled."
	}
	return notifyArgs{}, fmt.Sprintf("unknown signal kind '%s'.", kind)
}

// parseWarning reads `<kind> --minutes N [--dry-run]`.
func parseWarning(kind string, args []string) (notifyArgs, string) {
	if len(args) < 3 || args[1] != "--minutes" || args[2] == "" {
		return notifyArgs{}, fmt.Sprintf("'notify %s' needs --minutes N.", kind)
	}
	minutes, problem := minutesOrUsage(args[2], "--minutes")
	if problem != "" {
		return notifyArgs{}, problem
	}
	parsed := notifyArgs{kind: kind, minutes: minutes}
	switch {
	case len(args) > 3 && args[3] == flagDryRun:
		parsed.dryRun = true
	case len(args) > 3:
		return notifyArgs{}, fmt.Sprintf("unexpected argument '%s'.", args[3])
	}
	if len(args) > 4 {
		return notifyArgs{}, fmt.Sprintf("unexpected argument '%s'.", args[4])
	}
	return parsed, ""
}

// notify tells every live session's project the machine is going down, or that it is not
// after all. Nothing goes to stdout.
func (a *App) notify(ctx context.Context, args []string) int {
	n, problem := parseNotify(args)
	if problem != "" {
		return a.usageError(problem)
	}
	if n.kind == "going-down" {
		n.kind = a.goingDownKind()
	}
	if _, err := a.signalProjects(ctx, n.kind, n.minutes, n.dryRun); err != nil {
		return term.ExitFailure
	}
	return term.ExitOK
}
