package teams

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

const (
	whyBackup   = "A backup is taken as root, by agent-bus-install."
	whyBackups  = "The backups are listed and restored as root, by agent-bus-install."
	whyUndecl   = "A team that is not declared is taken down as root, by agent-bus-install."
	choiceKeep  = "Take it down, keep its data"
	choicePurge = "Take it down and delete its data"
	choiceStay  = "Leave it running"

	removeExplanation = "Its homeserver and its backup timer stop: its agents and people can no longer reach it. " +
		"Its data (the room's whole history, the accounts, the backups) stays on this machine unless you choose to delete it, " +
		"which cannot be undone. There may be history in it worth keeping."
)

// backupNumber is how a backup is named on the command line.
var backupNumber = regexp.MustCompile(`^[1-9][0-9]*$`)

// explain prints text wrapped to the terminal, indented, before a question.
func (o *teamOps) explain(ctx context.Context, text string, indent int) error {
	cols := 80
	if o.Width != nil {
		cols = o.Width(ctx)
	}
	if cols < 44 {
		cols = 80 // a width is only layout: with none to read, a common one serves
	}
	return o.Gum.Style(ctx, text, cols-4, indent)
}

// questionFailed is the exit for a gum question that did not answer.
func (o *teamOps) questionFailed(err error) term.ExitCode {
	if errors.Is(err, ui.ErrCancelled) {
		return o.cancelled()
	}
	o.Term.PrintError(err.Error())
	return term.ExitFailure
}

// remove is "ccy-teams remove <team>": take one team down, on a terminal, after saying what
// is lost: a backup first (yes by default), then down with its data kept, unless the human
// chooses to delete it and types the team's name. Never with --yes, never in bulk.
func (o *teamOps) remove(ctx context.Context, args []string) term.ExitCode {
	defer o.changed()
	if err := o.NeedCheckout(); err != nil {
		return term.ExitFailure
	}
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return o.usageError("'remove' takes one team (and no --yes: it always asks).")
	}
	team := args[0]
	if !o.interactive() {
		return o.usageError("'remove' asks on a terminal, always: taking a team down is a human's decision.")
	}
	if _, err := o.LookPath("gum"); err != nil {
		o.Term.PrintError("gum is not installed, and ccy-teams needs it here. playbooks/imports/play-claude-yolo.yml installs it; run that play and try again.")
		return term.ExitFailure
	}
	status, err := o.Refresh(ctx)
	if err != nil {
		return term.ExitFailure
	}
	s := status[team]
	switch s.State {
	case declare.ApplyAbsent, declare.ApplyRemoved, declare.ApplyUnbuilt:
		return o.usageError("team " + team + " is not built here; there is nothing to take down.")
	case declare.ApplyUnreadable:
		return o.usageError("team " + team + "'s entry cannot be read: " + s.Detail + ".")
	case declare.ApplyCurrent, declare.ApplyOutdated, declare.ApplyDecide:
		// Built here: it can be taken down.
	}
	if code, ok := o.heading(ctx, team); !ok {
		return code
	}
	if code, ok := o.backupFirst(ctx, team); !ok {
		return code
	}
	purge, code, ok := o.chooseFate(ctx, team)
	if !ok {
		return code
	}
	if s.Detail == "undeclared" {
		code, ok = o.takeDownUndeclared(ctx, team, purge)
	} else {
		code, ok = o.takeDownDeclared(ctx, team, purge)
	}
	if !ok {
		return code
	}
	return o.confirmDown(ctx, team, purge)
}

// heading is the framed title and what taking the team down means.
func (o *teamOps) heading(ctx context.Context, team string) (term.ExitCode, bool) {
	if err := o.Gum.Box(ctx, true, "Take team "+team+" down"); err != nil {
		o.Term.PrintError(err.Error())
		return term.ExitFailure, false
	}
	if err := o.explain(ctx, removeExplanation, 0); err != nil {
		o.Term.PrintError(err.Error())
		return term.ExitFailure, false
	}
	return term.ExitOK, true
}

// backupFirst offers a backup of the team before it goes, and takes it.
func (o *teamOps) backupFirst(ctx context.Context, team string) (term.ExitCode, bool) {
	yes, err := o.Gum.Confirm(ctx, "Back "+team+" up first?", true)
	if err != nil {
		return o.questionFailed(err), false
	}
	if !yes {
		return term.ExitOK, true
	}
	if o.Sudo.Ready(ctx, whyBackup) != nil {
		return term.ExitFailure, false
	}
	if err := o.Bus.BackupNow(ctx, team); err != nil {
		o.childExit(err)
		o.Term.PrintError("the backup failed (above), so " + team + " was not taken down.")
		return term.ExitFailure, false
	}
	return term.ExitOK, true
}

// chooseFate asks what happens to the team and, for deletion, makes the person type its name.
func (o *teamOps) chooseFate(ctx context.Context, team string) (bool, term.ExitCode, bool) {
	choice, err := o.Gum.Choose(ctx, "What happens to "+team+"?", choiceKeep, []string{choiceKeep, choicePurge, choiceStay})
	if err != nil {
		return false, o.questionFailed(err), false
	}
	switch choice {
	case choiceKeep:
		return false, 0, true
	case choicePurge:
		typed, err := o.Gum.Input(ctx, "Type "+team+" to delete its data for good", "the team's name", "")
		if err != nil {
			return false, o.questionFailed(err), false
		}
		if typed != team {
			o.Term.Printf("That is not the team's name; nothing was changed.\n")
			return false, term.ExitFailure, false
		}
		return true, 0, true
	default:
		o.Term.Printf("Nothing was changed.\n")
		return false, term.ExitFailure, false
	}
}

// takeDownUndeclared stops a team the host_vars no longer declare, which the play cannot
// take: the installer does, as the play would.
func (o *teamOps) takeDownUndeclared(ctx context.Context, team string, purge bool) (term.ExitCode, bool) {
	if o.Sudo.Ready(ctx, whyUndecl) != nil {
		return term.ExitFailure, false
	}
	if err := o.Bus.RemoveTeam(ctx, team, purge); err != nil {
		o.childExit(err)
		o.Term.PrintError(team + " was not taken down (above).")
		return term.ExitFailure, false
	}
	return term.ExitOK, true
}

// takeDownDeclared declares the team absent (purge marks its data for deletion), then runs
// the play that stops it. declare.py's own exit status is the status when it refuses.
func (o *teamOps) takeDownDeclared(ctx context.Context, team string, purge bool) (term.ExitCode, bool) {
	change, err := o.Declare.Remove(ctx, team, purge)
	if err != nil {
		return o.childExit(err), false
	}
	marker := "UNCHANGED"
	if change.Changed {
		marker = "CHANGED"
	}
	if _, err := fmt.Fprintf(o.Stdout, "%s\t%s\n", marker, change.Text); err != nil {
		o.Term.PrintError(err.Error())
		return term.ExitFailure, false
	}
	if o.runPlay(ctx, []string{team}) != nil {
		o.Term.PrintError(team + " is declared absent in " + o.home(o.Cfg.HostVars) +
			" but is still running; once the cause above is fixed, ccy-teams remove " + team + " runs the play again.")
		return term.ExitFailure, false
	}
	return term.ExitOK, true
}

// confirmDown reads the stamps back: the team must now be removed, or gone altogether.
func (o *teamOps) confirmDown(ctx context.Context, team string, purge bool) term.ExitCode {
	status, err := o.Refresh(ctx)
	if err != nil {
		return term.ExitFailure
	}
	if s := status[team]; s.State != declare.ApplyAbsent && s.State != declare.ApplyRemoved {
		o.Term.PrintError(team + " is still " + applyWord(s) + " after the play.")
		return term.ExitFailure
	}
	if purge {
		o.Term.Say("Team " + team + " is down and its data deleted.")
	} else {
		o.Term.Say("Team " + team + " is down; its data is kept in /var/lib/agent-bus/" + team + ".")
	}
	return term.ExitOK
}

// backup is "ccy-teams backup <team>": a backup taken now, as root. The installer's own exit
// status is the status.
func (o *teamOps) backup(ctx context.Context, args []string) term.ExitCode {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return o.usageError("'backup' takes one team.")
	}
	if o.Sudo.Ready(ctx, whyBackup) != nil {
		return term.ExitFailure
	}
	if err := o.Bus.BackupNow(ctx, args[0]); err != nil {
		return o.childExit(err)
	}
	return term.ExitOK
}

// restore is "ccy-teams restore <team> [<n>] [--yes]": put a backup back. Without a number
// the backups are listed (on stdout when there is no terminal to choose on, else a picker);
// without --yes a terminal confirms.
func (o *teamOps) restore(ctx context.Context, args []string) term.ExitCode {
	defer o.changed()
	req, code, ok := o.restoreArgs(args)
	if !ok {
		return code
	}
	if o.Sudo.Ready(ctx, whyBackups) != nil {
		return term.ExitFailure
	}
	if req.id == "" {
		if req.id, code, ok = o.chooseBackup(ctx, req.team); !ok {
			return code
		}
	}
	if !req.yes {
		if !o.interactive() {
			return o.usageError("a restore replaces the team's database and state: confirm it with --yes when there is no terminal.")
		}
		sure, err := o.Picker.Confirm(ctx, "Restore "+req.team,
			"Restore backup "+req.id+" of "+req.team+"? Its homeserver stops, and every message and change since that backup is lost.",
			"restore backup "+req.id, false)
		if err != nil {
			return o.childExit(err)
		}
		if !sure {
			o.Term.Printf("Nothing restored.\n")
			return term.ExitOK
		}
	}
	if err := o.Bus.Restore(ctx, req.team, req.id); err != nil {
		return o.childExit(err)
	}
	return term.ExitOK
}

// restoreReq is what restore was asked for: the team, the backup number if named, and
// whether --yes confirmed it.
type restoreReq struct {
	team, id string
	yes      bool
}

// restoreArgs reads the arguments of restore: --yes anywhere, then the team and the number.
func (o *teamOps) restoreArgs(args []string) (restoreReq, term.ExitCode, bool) {
	var req restoreReq
	for _, a := range args {
		switch {
		case a == "--yes":
			req.yes = true
		case strings.HasPrefix(a, "-"):
			return req, o.usageError("unknown restore option '" + a + "'."), false
		case req.team == "":
			req.team = a
		case req.id == "":
			req.id = a
		default:
			return req, o.usageError("unexpected argument '" + a + "'."), false
		}
	}
	if req.team == "" {
		return req, o.usageError("'restore' needs a team."), false
	}
	if req.id != "" && !backupNumber.MatchString(req.id) {
		return req, o.usageError("a backup is named by its number; got '" + req.id + "'."), false
	}
	return req, 0, true
}

// chooseBackup lists the team's backups and has the person choose one. With no terminal the
// listing goes to stdout and the person is told to name one.
func (o *teamOps) chooseBackup(ctx context.Context, team string) (string, term.ExitCode, bool) {
	backups, err := o.Bus.Backups(ctx, team)
	if err != nil {
		return "", o.childExit(err), false
	}
	if len(backups) == 0 {
		o.Term.PrintError("team " + team + " has no backup to restore.")
		return "", term.ExitFailure, false
	}
	if !o.interactive() {
		for _, b := range backups {
			if _, werr := fmt.Fprintf(o.Stdout, "BACKUP\t%s\t%s\n", b.ID, b.Time); werr != nil {
				o.Term.PrintError(werr.Error())
				return "", term.ExitFailure, false
			}
		}
		return "", o.usageError("name the backup to restore: ccy-teams restore " + team + " <n> --yes."), false
	}
	rows, tags := make([]string, 0, len(backups)), make([]string, 0, len(backups))
	for _, b := range backups {
		rows, tags = append(rows, fmt.Sprintf("Backup %-6s %s", b.ID, b.Time)), append(tags, b.ID)
	}
	picked, err := o.Picker.Pick(ctx, ui.PickSpec{
		Title: "Restore " + team,
		Header: ui.Header("Enter choose   Esc or q exit",
			"The homeserver is stopped, the backup's database and state put back, and it is started again."),
		Rows: rows, Tags: tags,
	})
	if errors.Is(err, ui.ErrLeave) {
		o.Term.Printf("Nothing restored.\n")
		return "", term.ExitOK, false
	}
	if err != nil {
		o.Term.PrintError(err.Error())
		return "", term.ExitFailure, false
	}
	if picked.Tag == "" {
		o.Term.PrintError("the choice '" + picked.Row + "' names no backup.")
		return "", term.ExitFailure, false
	}
	return picked.Tag, term.ExitOK, true
}
