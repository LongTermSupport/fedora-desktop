package chooser

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// exitConfig is the launcher's code for a state that stops the launch (agent-bus's own 78).
const exitConfig term.ExitCode = 78

// Where the owner's acceptance lives, under their home: the path pingbus reads
// (helpers/pingbus/config.py ELEMENT_ACCEPTED_DIR and ELEMENT_ACCEPTED_FILE), kept equal by
// scripts/test-cc-teams.bash.
const (
	acceptedDir  = ".config/pingbus"
	acceptedFile = "element-accepted"
)

// finish is the end of every path: the seats are known, so a host launch gets the owner's
// acceptance, and then the list is the payload.
func (c *Chooser) finish(ctx context.Context, memberType string, refs []string) term.ExitCode {
	if memberType == typeHost {
		if code := c.consent(ctx, refs); code != term.ExitOK {
			return code
		}
	}
	return c.printList(refs)
}

// consent is the owner's once-per-user acceptance that a host seat runs beside their Element
// session, asked after the seats are chosen and before any is made. A record that is already
// there answers it. A decline, or Ctrl-C at the question, records nothing and ends the run (1).
func (c *Chooser) consent(ctx context.Context, refs []string) term.ExitCode {
	home := c.Host.Getenv("HOME")
	if home == "" {
		c.say("ccy-teams: HOME is not set, so the Element acceptance cannot be found or recorded.")
		return exitConfig
	}
	dir := filepath.Join(home, acceptedDir)
	record := filepath.Join(dir, acceptedFile)
	accepted, err := recordState(dir, record, c.UID())
	if err != nil {
		c.say("ccy-teams: " + err.Error())
		return exitConfig
	}
	if accepted {
		return term.ExitOK
	}
	if !c.attended() {
		c.say("ccy-teams: the Element consent needs a terminal to be asked on, and there is none: " +
			"start cc --teams once from a terminal, and no seat has been made.")
		return exitConfig
	}
	if _, lookErr := c.Host.LookPath("gum"); lookErr != nil {
		c.say("ccy-teams: gum is needed to ask you this, and it is not installed: run play-claude-yolo.yml.")
		return exitConfig
	}
	handles, code := c.handles(ctx, refs)
	if code != term.ExitOK {
		return code
	}
	c.say(consentWords(handles))
	yes, err := c.Gum.Confirm(ctx, "Accept, and start "+strings.Join(handles, ", ")+"?", false)
	if errors.Is(err, ui.ErrCancelled) || (err == nil && !yes) {
		c.say("Not started. Nothing was recorded; cc --teams asks again next time.")
		return term.ExitFailure
	}
	if err != nil {
		c.Host.ReportErr(err)
		return term.ExitFailure
	}
	if err := c.record(dir, record); err != nil {
		c.say("ccy-teams: could not record your answer in " + record + ": " + err.Error())
		return exitConfig
	}
	return term.ExitOK
}

// consentWords are what the owner reads before answering, naming every handle that starts.
func consentWords(handles []string) string {
	verb, who := "joins its team", handles[0]
	if len(handles) > 1 {
		verb, who = "join their teams", strings.Join(handles, ", ")
	}
	return "\n  " + who + " " + verb + " as its own member, and runs as you.\n\n" +
		"  That means it can reach what you can, including the Element\n" +
		"  session you are signed in to. A misbehaving agent could use it\n" +
		"  to post as you, and every agent on the team would obey that.\n\n" +
		"  It is told to post only through pingbus, as itself, and its\n" +
		"  tools are denied Element's folders, the keyring and the plain\n" +
		"  commands that reach them. That is guidance, not a wall: an agent\n" +
		"  beside Element can still reach your session by other routes.\n" +
		"  ccy --teams is the safe choice: its agent runs in a container\n" +
		"  that cannot reach Element at all.\n\n" +
		"  Any human listed in a team can direct its agents as you can.\n" +
		"  You are asked once, for every cc --teams seat.\n"
}

// recordState is whether the acceptance is already recorded, by the rule pingbus applies
// (config.element_accepted): a real folder and a plain file, both the user's, the file with no
// group or other access. Anything else that exists is refused with how to put it right, so no
// seat is made for a record pingbus would then not count; nothing is written through a symlink.
func recordState(dir, record string, uid int) (bool, error) {
	info, err := os.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, err
	case !info.IsDir():
		return false, fmt.Errorf("%s is not a real directory; remove it and start again", dir)
	case !ownedBy(info, uid):
		return false, fmt.Errorf("%s is not yours (the owner must be you); remove it and start again", dir)
	case info.Mode().Perm()&0o077 != 0:
		return false, fmt.Errorf("%s is open to other users; chmod 700 it and start again", dir)
	}
	info, err = os.Lstat(record)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, err
	case !info.Mode().IsRegular():
		return false, fmt.Errorf("%s is not a plain file; remove it and start again", record)
	case !ownedBy(info, uid):
		return false, fmt.Errorf("%s is not yours (the owner must be you); remove it and answer again", record)
	case info.Mode().Perm()&0o077 != 0:
		return false, fmt.Errorf("%s is open to other users, so pingbus would not count it; chmod 600 it, "+
			"or remove it and answer again", record)
	}
	return true, nil
}

// ownedBy is whether the file belongs to uid.
func ownedBy(info fs.FileInfo, uid int) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == uid
}

// handles are the member handles the refs will have, as agent-bus offers them for a host seat of
// this checkout. A seat it does not offer cannot be named, and the run stops there.
func (c *Chooser) handles(ctx context.Context, refs []string) ([]string, term.ExitCode) {
	var handles []string
	for _, ref := range refs {
		seat, team, ok := strings.Cut(ref, "@")
		if !ok {
			c.say("ccy-teams: '" + ref + "' is not SEAT@TEAM.")
			return nil, term.ExitUsage
		}
		choices, err := c.Host.Bus.SeatChooseAs(ctx, team, typeHost)
		if err != nil {
			c.Host.ReportErr(err)
			return nil, exitConfig
		}
		at := slices.IndexFunc(choices.Rows, func(row agentbus.Choice) bool { return row.Seat == seat })
		if at < 0 {
			c.say("ccy-teams: " + ref + " is not one of the seats agent-bus offers for this checkout in " + team +
				"; choose one of them, or leave the seat out to be asked.")
			return nil, exitConfig
		}
		handles = append(handles, choices.Rows[at].Handle)
	}
	return handles, term.ExitOK
}

// record writes the acceptance: the folder the owner's alone (0700), the record owner-only, a
// symlink never followed and an existing record never overwritten.
func (c *Chooser) record(dir, record string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Clean(record), os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	_, werr := fmt.Fprintf(f, "accepted %s\n", c.Now().UTC().Format("2006-01-02T15:04:05Z"))
	return errors.Join(werr, f.Close())
}
