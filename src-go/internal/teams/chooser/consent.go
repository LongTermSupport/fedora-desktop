package chooser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/agentbus"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/matrixclients"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// exitConfig is the launcher's code for a state that stops the launch (agent-bus's own 78).
const exitConfig = term.ExitConfig

// Where the owner's acceptance lives, under their passwd home (never HOME, PINGBUS_HOME or
// XDG_CONFIG_HOME): the path pingbus reads (helpers/pingbus/config.py ELEMENT_ACCEPTED_DIR and
// ELEMENT_ACCEPTED_FILE under _home_of), kept equal by scripts/test-cc-teams.bash.
const (
	acceptedDir  = ".config/pingbus"
	acceptedFile = "element-accepted"
)

// finish is the end of every path: the seats are known, so a host launch gets the owner's
// acceptance, and then the list is the payload.
func (c *Chooser) finish(ctx context.Context, memberType string, refs []string) term.ExitCode {
	if memberType == typeHost {
		if code := c.consent(ctx, memberType, refs); code != term.ExitOK {
			return code
		}
	}
	return c.printList(refs)
}

// consent is the owner's once-per-user acceptance that a host seat runs beside the Matrix
// clients signed in under their home (the one list, matrixclients), asked after the seats are
// chosen and before any is made. A record that names every client found answers it; a record
// that does not (one written before Nheko or Lightning was there, or before records named their
// clients) is asked again, for the new clients. A decline, or Ctrl-C at the question, records
// nothing and ends the run (1).
func (c *Chooser) consent(ctx context.Context, memberType string, refs []string) term.ExitCode {
	home, err := c.Host.UserHome(c.UID())
	if err != nil {
		c.say("ccy-teams: your home is not in the passwd database (" + err.Error() +
			"), so the Matrix consent cannot be found or recorded.")
		return exitConfig
	}
	list, err := matrixclients.Load()
	if err != nil {
		c.say("ccy-teams: " + err.Error())
		return exitConfig
	}
	found, err := list.Present(home)
	if err != nil {
		c.say("ccy-teams: " + err.Error())
		return exitConfig
	}
	dir := filepath.Join(home, acceptedDir)
	record := filepath.Join(dir, acceptedFile)
	accepted, again, err := readRecord(list, dir, record, c.UID())
	if err != nil {
		c.say("ccy-teams: " + err.Error())
		return exitConfig
	}
	newClients := matrixclients.Uncovered(found, accepted)
	if again && len(newClients) == 0 {
		return term.ExitOK
	}
	if !c.attended() {
		c.say("ccy-teams: the Matrix consent needs a terminal to be asked on, and there is none: " +
			"start cc --teams once from a terminal, and no seat has been made.")
		return exitConfig
	}
	if _, lookErr := c.Host.LookPath("gum"); lookErr != nil {
		c.say("ccy-teams: gum is needed to ask you this, and it is not installed: run play-claude-yolo.yml.")
		return exitConfig
	}
	handles, code := c.handles(ctx, memberType, refs)
	if code != term.ExitOK {
		return code
	}
	c.say(consentWords(handles, list, accepted, found, newClients, again))
	who := strings.Join(handles, ", ")
	if len(handles) == 0 {
		who = "this session on the bus"
	}
	head := ui.Here(flow.LaunchCrumb, "Matrix consent").About("host seats: " + who)
	yes, err := c.Gum.Confirm(ctx, head, "Accept, and start "+who+"?", false)
	if errors.Is(err, ui.ErrCancelled) || (err == nil && !yes) {
		c.say("Not started. Nothing was recorded; cc --teams asks again next time.")
		return term.ExitFailure
	}
	if err != nil {
		c.Host.ReportErr(err)
		return term.ExitFailure
	}
	ids := accepted
	for _, id := range matrixclients.AppIDs(newClients) {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if err := c.record(dir, record, ids); err != nil {
		c.say("ccy-teams: could not record your answer in " + record + ": " + err.Error())
		return exitConfig
	}
	return term.ExitOK
}

// consentWords are what the owner reads before answering: every handle that starts (with none,
// cc --bus, the teams the session is added to later), the Matrix clients found under their home,
// and, asked again, the clients that are new since they last answered.
func consentWords(handles []string, list matrixclients.List, accepted []string, found []matrixclients.Found,
	newClients []matrixclients.Client, again bool,
) string {
	var opening string
	switch len(handles) {
	case 0:
		opening = "This session joins the teams you add it to later (ccy-teams session add),\n" +
			"  each as its own member, and runs as you."
	case 1:
		opening = handles[0] + " joins its team as its own member, and runs as you."
	default:
		opening = strings.Join(handles, ", ") + " join their teams as its own member, and runs as you."
	}
	if again {
		before := list.Names(accepted)
		if before == "" {
			before = "no Matrix client"
		}
		opening = "You accepted cc --teams seats beside " + before + ". " +
			list.Names(matrixclients.AppIDs(newClients)) + " is now under your home too,\n" +
			"  so you are asked again, for it.\n\n  " + opening
	}
	allNames := list.Names(matrixclients.AppIDs(list.Clients))
	signedIn := "  No Matrix client is signed in under your home now; if one is\n" +
		"  later, you are asked again.\n\n"
	if len(found) > 0 {
		var ids []string
		for _, f := range found {
			ids = append(ids, f.Client.AppID)
		}
		signedIn = "  (found under your home: " + list.Names(ids) + ")\n\n"
	}
	return "\n  " + opening + "\n\n" +
		"  That means it can reach what you can, including the Matrix\n" +
		"  session you are signed in to in " + strings.Replace(allNames, " and ", " or ", 1) + ".\n" +
		"  A misbehaving agent could use it to post as you, and every\n" +
		"  agent on the team would obey that.\n" + signedIn +
		"  It is told to post only through pingbus, as itself, and its\n" +
		"  tools are denied those clients' folders, the keyring and the\n" +
		"  plain commands that reach them. That is guidance, not a wall: an\n" +
		"  agent beside them can still reach your session by other routes.\n" +
		"  ccy --teams is the safe choice: its agent runs in a container\n" +
		"  that cannot reach your Matrix sign-in.\n\n" +
		"  Any human listed in a team can direct its agents as you can.\n" +
		"  You are asked once, for every cc --teams seat, and again only\n" +
		"  when a Matrix client you have not accepted appears.\n"
}

// readRecord is the app ids the owner's record accepts, and whether there is one: none is not an
// error, and a record whose place or content pingbus would not count is (recordState,
// matrixclients.List.ParseConsent), with how to put it right.
func readRecord(list matrixclients.List, dir, record string, uid int) ([]string, bool, error) {
	exists, err := recordState(dir, record, uid)
	if err != nil || !exists {
		return nil, false, err
	}
	f, err := os.OpenFile(filepath.Clean(record), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, false, err
	}
	data, readErr := io.ReadAll(io.LimitReader(f, matrixclients.ConsentMaxBytes+1))
	if err := errors.Join(readErr, f.Close()); err != nil {
		return nil, false, err
	}
	accepted, err := list.ParseConsent(data)
	if err != nil {
		return nil, false, fmt.Errorf("%s is not a consent record pingbus can read (%w); remove it and answer again", record, err)
	}
	return accepted, true, nil
}

// recordState is whether the acceptance is already recorded, by the rule pingbus applies
// (config.accepted_clients): a real folder and a plain file, both the user's, the file with no
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

// handles are the member handles the refs will have, as agent-bus offers them for a seat of
// memberType (the launch's own: host, as only a host launch is asked) in this checkout. A seat it does not offer cannot be named, and the run stops there.
func (c *Chooser) handles(ctx context.Context, memberType string, refs []string) ([]string, term.ExitCode) {
	var handles []string
	for _, ref := range refs {
		seat, team, ok := strings.Cut(ref, "@")
		if !ok {
			c.say("ccy-teams: '" + ref + "' is not SEAT@TEAM.")
			return nil, term.ExitUsage
		}
		choices, err := c.Host.Bus.SeatChooseAs(ctx, team, memberType)
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

// record writes the acceptance of ids: the folder the owner's alone (0700), the record
// owner-only, replaced whole (a second yes rewrites it with the clients added).
func (c *Chooser) record(dir, record string, ids []string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return fsx.WriteAtomic(record, "Matrix consent record", matrixclients.FormatConsent(c.Now(), ids), 0o600)
}
