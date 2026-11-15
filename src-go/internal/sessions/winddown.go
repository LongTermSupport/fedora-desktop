package sessions

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sessclose"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tmuxccy"
)

// cmdWindDown names the subcommand that compacts every live session before a planned reboot.
const cmdWindDown = "wind-down"

// defaultWindDownWait is the wind-down's deadline when --wait is not given: as long as a close
// gives one compaction (sessclose.DefaultWaits.Compact).
const defaultWindDownWait = 600 * time.Second

// maxWindDownWait is the longest --wait taken, a day.
const maxWindDownWait = 86400

// parseWindDown reads `wind-down [--wait SECONDS]`; a mistake is returned as its sentence.
func parseWindDown(args []string) (time.Duration, string) {
	wait := defaultWindDownWait
	for i := 0; i < len(args); i++ {
		if args[i] != "--wait" {
			return 0, fmt.Sprintf("unknown wind-down option '%s'.", args[i])
		}
		value := ""
		if i+1 < len(args) {
			value = args[i+1]
		}
		secs, err := strconv.Atoi(value)
		if !wholeSeconds.MatchString(value) || err != nil || secs < 1 {
			return 0, fmt.Sprintf("--wait needs a whole number of seconds, at least 1; got '%s'.", value)
		}
		if secs > maxWindDownWait {
			return 0, fmt.Sprintf("--wait takes at most %d seconds; got '%s'.", maxWindDownWait, value)
		}
		wait = time.Duration(secs) * time.Second
		i++
	}
	return wait, ""
}

// windDownSessions compacts every live recorded session before a planned reboot (fedora-desktop#290):
// each lets a running turn finish, is sent /compact and has its end waited for, all against one
// deadline, and is left running with its record kept, so the restore brings it back on a small
// context. One line each on stdout: "<name> COMPACTED", "<name> COMPACTED-AFTER-TURN",
// "<name> NOT-FINISHED <why>", "<name> SKIPPED <why>" or "<name> FAILED <why>"; exit 0 unless a
// session is NOT-FINISHED or FAILED.
func (a *App) windDownSessions(ctx context.Context, args []string) term.ExitCode {
	wait, problem := parseWindDown(args)
	if problem != "" {
		return a.usageError(problem)
	}
	live, err := a.Tmux.List(ctx)
	if err != nil {
		return a.failed(err)
	}
	if len(live) == 0 {
		a.sayf("No live session to wind down.\n")
		return term.ExitOK
	}
	store, err := registry.Locate(a.Getenv)
	if err != nil {
		return a.failed(err)
	}
	lines := make(map[string]string, len(live))
	var names []string
	for _, s := range live {
		_, getErr := store.Get(s.Name)
		switch {
		case errors.Is(getErr, registry.ErrNoRecord):
			lines[s.Name] = "SKIPPED no session record, so no restore brings it back"
		case getErr != nil:
			lines[s.Name] = "FAILED its session record could not be read: " + getErr.Error()
		default:
			names = append(names, s.Name)
		}
	}
	if len(names) > 0 {
		a.sayf("Winding down %d session(s): each finishes its turn, then compacts, within %s.\n", len(names), sessclose.Seconds(wait))
		for _, r := range a.WindDown.CompactAll(ctx, names, a.Now().Add(wait)) {
			lines[r.Name] = windDownLine(r)
		}
	}
	return a.reportWindDown(live, lines)
}

// reportWindDown prints one line per live session, in the server's order, and is the exit.
func (a *App) reportWindDown(live []tmuxccy.Session, lines map[string]string) term.ExitCode {
	short := 0
	for _, s := range live {
		line := lines[s.Name]
		a.out(s.Name + " " + line + "\n")
		if windDownShort(line) {
			short++
		}
	}
	if short > 0 {
		a.sayf("%d session(s) were not compacted; the reasons are above.\n", short)
		return term.ExitFailure
	}
	return term.ExitOK
}

// windDownShort is whether a line is a session the wind-down fell short on.
func windDownShort(line string) bool {
	return strings.HasPrefix(line, "NOT-FINISHED") || strings.HasPrefix(line, "FAILED")
}

// windDownLine is a session's result as its report line, without the name.
func windDownLine(r sessclose.Result) string {
	word := "FAILED"
	switch r.Outcome {
	case sessclose.Compacted:
		word = "COMPACTED"
		if r.Busy {
			word = "COMPACTED-AFTER-TURN"
		}
	case sessclose.Unfinished:
		word = "NOT-FINISHED"
	case sessclose.Skipped:
		word = "SKIPPED"
	case sessclose.Failed:
	case sessclose.Pending, sessclose.Exited, sessclose.Stopped:
		return word + " the wind-down ended it as a close would, which it never should"
	}
	if r.Note == "" {
		return word
	}
	return word + " " + r.Note
}
