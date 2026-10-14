package teams

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

const nothingCreated = "Nothing created. ccy-teams create starts one whenever you like."

// needGum refuses without gum, naming the play that installs it.
func (a *App) needGum() error {
	if !a.hasTool("gum") {
		a.Term.PrintError("gum is not installed, and ccy-teams needs it here. " +
			"playbooks/imports/play-claude-yolo.yml installs it; run that play and try again.")
		return leave(term.ExitFailure)
	}
	return nil
}

// askYes is a yes/no question in gum. Ctrl-C ends the picker.
func (a *App) askYes(ctx context.Context, question string, defaultYes bool) (bool, error) {
	yes, err := a.Gum.Confirm(ctx, question, defaultYes)
	if errors.Is(err, ui.ErrCancelled) {
		return false, a.cancelled()
	}
	if err != nil {
		a.reportErr(err)
		return false, leave(term.ExitFailure)
	}
	return yes, nil
}

// offerCreate is the answer to a machine with no teams: offer to make one, and leave with the
// outcome.
func (a *App) offerCreate(ctx context.Context) error {
	if err := a.needGum(); err != nil {
		return err
	}
	a.Term.Say("No agent teams on this machine yet.")
	yes, err := a.askYes(ctx, "Create one now?", false)
	if err != nil {
		return err
	}
	if !yes {
		a.tell(nothingCreated)
		return leave(term.ExitOK)
	}
	return leave(a.Ops.create(ctx, nil))
}

// pathLimitMarker is the file whose presence says the person declined to lift team's limit.
func (a *App) pathLimitMarker(team string) string {
	return filepath.Join(a.StateDir, "declined", "path-limit-"+team)
}

// offerPathLimits asks, once per team and whichever checkout this is, whether to lift a limit
// to some folders that a team was made with.
func (a *App) offerPathLimits(ctx context.Context) error {
	limits, err := a.Declare.Limited(ctx)
	if err != nil {
		a.reportErr(err)
		return leave(term.ExitFailure)
	}
	for _, limit := range limits {
		marker := a.pathLimitMarker(limit.Team)
		_, statErr := os.Stat(marker)
		if statErr == nil {
			continue
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			a.Term.PrintError("the marker " + marker + " could not be looked for: " + statErr.Error())
			return leave(term.ExitFailure)
		}
		if err := a.askPathLimit(ctx, limit.Team, limit.Prefixes, limit.Trusts, marker); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) askPathLimit(ctx context.Context, team, prefixes, trusts, marker string) error {
	if err := a.needGum(); err != nil {
		return err
	}
	a.Term.Say("One question about a team already on this machine, asked once, whichever checkout you are in.")
	a.Term.Say("Team " + team + " (for " + trusts + ") was made when teams were limited to some folders: " +
		"its agents may only point each other at files under " + prefixes + ".")
	yes, err := a.askYes(ctx, "Remove the limit, so any path in its repositories can be named?", true)
	if err != nil {
		return err
	}
	if !yes {
		return a.keepLimit(team, marker)
	}
	if _, err := a.Declare.DropLimit(ctx, team); err != nil {
		a.reportErr(err)
		return leave(term.ExitFailure)
	}
	if err := a.Ops.applyTeams(ctx, []string{team}); err != nil {
		a.tell("(ccy-teams apply " + team + " failed: see above.)")
	}
	return nil
}

// keepLimit remembers that the person kept team's limit, so it is not asked about again.
func (a *App) keepLimit(team, marker string) error {
	if err := os.MkdirAll(filepath.Dir(marker), 0o750); err != nil {
		a.Term.PrintError("the answer could not be kept: " + err.Error())
		return leave(term.ExitFailure)
	}
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		a.Term.PrintError("the answer could not be kept: " + err.Error())
		return leave(term.ExitFailure)
	}
	a.tell("Kept. ccy-teams will not ask about " + team + "'s limit again.")
	return nil
}

// offerUpdate offers to bring the teams that are out of date, or declared and not built, up to
// what is declared.
func (a *App) offerUpdate(ctx context.Context) error {
	s, err := a.refresh(ctx)
	if err != nil {
		return leave(term.ExitFailure)
	}
	var behind []string
	for _, team := range s.Teams {
		if state := s.Status[team].State; state == declare.ApplyOutdated || state == declare.ApplyUnbuilt {
			behind = append(behind, team+" ("+s.statusWord(team)+")")
		}
	}
	if len(behind) == 0 {
		return nil
	}
	slices.Sort(behind)
	if err = a.needGum(); err != nil {
		return err
	}
	a.Term.Say("Not as declared: " + strings.Join(behind, " ") + ".")
	yes, err := a.askYes(ctx, "Update them now (play-agent-bus.yml)?", true)
	if err != nil || !yes {
		return err
	}
	if err = a.inPicker("ccy-teams apply --all", a.Ops.apply(ctx, []string{flagAll})); err != nil {
		return err
	}
	a.backToList(ctx)
	return nil
}

// checkoutRepo is the lower-case OWNER/REPO of this checkout's origin, or "" when it has none
// on GitHub.
func (a *App) checkoutRepo(ctx context.Context) (string, error) {
	url, err := a.Git.RemoteURL(ctx, "")
	if err != nil || url == "" {
		return "", err
	}
	return a.Declare.GithubRemote(ctx, url)
}

// offerJoinHere, on the first pass, offers a session of the team that covers this checkout, or
// the making of one when none does.
func (r *pickerRun) offerJoinHere(ctx context.Context) error {
	a := r.a
	here, err := a.Git.ProjectHere(ctx, "")
	if err != nil {
		a.reportErr(err)
		return leave(term.ExitFailure)
	}
	if !here {
		return nil
	}
	repo, err := a.checkoutRepo(ctx)
	if err != nil {
		a.reportErr(err)
		return nil
	}
	if repo == "" {
		return nil
	}
	teams, err := a.Declare.TeamsFor(ctx, repo)
	if err != nil {
		a.reportErr(err)
		return nil
	}
	if len(teams) == 0 {
		return a.offerCreateHere(ctx, repo)
	}
	top, err := a.Git.TopLevel(ctx, "")
	if err != nil {
		a.reportErr(err)
		return leave(term.ExitFailure)
	}
	for _, team := range teams {
		if !r.snap.Loaded[team] || r.snap.stateOf(team) != systemd.HomeserverUp || r.snap.holdsLiveSeat(top, team) {
			continue
		}
		if err := a.offerJoin(ctx, top, team); err != nil {
			return err
		}
	}
	return nil
}

// offerCreateHere says that no team works on this checkout's repository, and offers one.
func (a *App) offerCreateHere(ctx context.Context, repo string) error {
	if err := a.needGum(); err != nil {
		return err
	}
	a.Term.Say("No team on this machine works on " + repo + " (this checkout). " +
		"The teams listed next are for other repositories.")
	yes, err := a.askYes(ctx, "Create a team for "+repo+" now?", true)
	if err != nil {
		return err
	}
	if !yes {
		a.tell(nothingCreated)
		return nil
	}
	return leave(a.Ops.create(ctx, nil))
}

// offerJoin offers to start a session in top as a member of team.
func (a *App) offerJoin(ctx context.Context, top, team string) error {
	if err := a.needGum(); err != nil {
		return err
	}
	yes, err := a.askYes(ctx, "Start a ccy session in "+a.abbrev(top)+" as a member of "+team+"?", true)
	if err != nil || !yes {
		return err
	}
	return a.startCcy(top, team)
}

// startCcy replaces this process with the launcher, run in top with the given seat refs.
func (a *App) startCcy(top, refs string) error {
	a.tell("Starting ccy --teams " + refs + " in " + a.abbrev(top) + ".")
	if err := a.Exec(a.Launcher, []string{a.Launcher, flagTeams, refs}, top); err != nil {
		a.Term.PrintError(err.Error())
		return leave(term.ExitFailure)
	}
	return leave(term.ExitOK)
}

// stateOf is the STATE of a team's own row.
func (s *Snapshot) stateOf(team string) string {
	for _, row := range s.Rows {
		if row.Team == team && row.Kind == kindTeam {
			return row.State
		}
	}
	return ""
}

// holdsLiveSeat is whether a session still running holds a seat of team in the checkout at top.
func (s *Snapshot) holdsLiveSeat(top, team string) bool {
	for key, session := range s.SeatSession {
		if !strings.HasPrefix(key, top+"|") || !strings.HasSuffix(key, "@"+team) {
			continue
		}
		if _, live := s.Live[session]; live {
			return true
		}
	}
	return false
}

// backToList holds what was just printed on the screen until the person has read it, because
// the list is drawn over it.
func (a *App) backToList(ctx context.Context) {
	a.tell("Press Enter to go back to the list.")
	a.waitForEnter(ctx)
}

// readLine waits for a line from r, one byte at a time so nothing past it is taken, and closes
// r. It gives up at the end of the input, an error, or when ctx is done. Closing r is what ends
// the reader goroutine of a cancelled wait, so readLine waits for that goroutine before it
// returns and r's Read must return once Close has been called (a terminal file does): nothing
// is left blocked on the terminal to swallow the next wait's Enter. The error is Close's.
func readLine(ctx context.Context, r io.ReadCloser) (bool, error) {
	got := make(chan bool, 1)
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := r.Read(buf)
			if n == 1 && buf[0] == '\n' {
				got <- true
				return
			}
			if err != nil {
				got <- false
				return
			}
		}
	}()
	select {
	case ok := <-got:
		return ok, r.Close()
	case <-ctx.Done():
		err := r.Close()
		<-got
		return false, err
	}
}
