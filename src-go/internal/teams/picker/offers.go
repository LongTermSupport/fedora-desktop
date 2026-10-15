package picker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/ops"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

const nothingCreated = "Nothing created. ccy-teams create starts one whenever you like."

// offerCreate is the answer to a machine with no teams: offer to make one, and leave with the
// outcome.
func (p *Picker) offerCreate(ctx context.Context) error {
	if err := p.Kit.NeedGum(); err != nil {
		return err
	}
	p.Kit.Term.Say("No agent teams on this machine yet.")
	yes, err := p.Kit.AskYes(ctx, "Create one now?", false)
	if err != nil {
		return err
	}
	if !yes {
		p.Kit.Tell(nothingCreated)
		return flow.Leave(term.ExitOK)
	}
	return flow.Leave(p.Create.Run(ctx, nil))
}

// pathLimitMarker is the file whose presence says the person declined to lift team's limit.
func (p *Picker) pathLimitMarker(team string) string {
	return filepath.Join(p.StateDir, "declined", "path-limit-"+team)
}

// offerPathLimits asks, once per team and whichever checkout this is, whether to lift a limit
// to some folders that a team was made with.
func (p *Picker) offerPathLimits(ctx context.Context) error {
	limits, err := p.Declare.Limited(ctx)
	if err != nil {
		p.Kit.ReportErr(err)
		return flow.Leave(term.ExitFailure)
	}
	for _, limit := range limits {
		marker := p.pathLimitMarker(limit.Team)
		_, statErr := os.Stat(marker)
		if statErr == nil {
			continue
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			p.Kit.Term.PrintError("the marker " + marker + " could not be looked for: " + statErr.Error())
			return flow.Leave(term.ExitFailure)
		}
		if err := p.askPathLimit(ctx, limit.Team, limit.Prefixes, limit.Trusts, marker); err != nil {
			return err
		}
	}
	return nil
}

func (p *Picker) askPathLimit(ctx context.Context, team, prefixes, trusts, marker string) error {
	if err := p.Kit.NeedGum(); err != nil {
		return err
	}
	p.Kit.Term.Say("One question about a team already on this machine, asked once, whichever checkout you are in.")
	p.Kit.Term.Say("Team " + team + " (for " + trusts + ") was made when teams were limited to some folders: " +
		"its agents may only point each other at files under " + prefixes + ".")
	yes, err := p.Kit.AskYes(ctx, "Remove the limit, so any path in its repositories can be named?", true)
	if err != nil {
		return err
	}
	if !yes {
		return p.keepLimit(team, marker)
	}
	if _, err := p.Declare.DropLimit(ctx, team); err != nil {
		p.Kit.ReportErr(err)
		return flow.Leave(term.ExitFailure)
	}
	if err := p.Ops.ApplyTeams(ctx, []string{team}); err != nil {
		p.Kit.Tell("(ccy-teams apply " + team + " failed: see above.)")
	}
	return nil
}

// keepLimit remembers that the person kept team's limit, so it is not asked about again.
func (p *Picker) keepLimit(team, marker string) error {
	return p.keepAnswer(marker, "Kept. ccy-teams will not ask about "+team+"'s limit again.")
}

// keepAnswer records a declined offer in marker, so it is not made again, and says so.
func (p *Picker) keepAnswer(marker, said string) error {
	if err := os.MkdirAll(filepath.Dir(marker), 0o750); err != nil {
		p.Kit.Term.PrintError("the answer could not be kept: " + err.Error())
		return flow.Leave(term.ExitFailure)
	}
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		p.Kit.Term.PrintError("the answer could not be kept: " + err.Error())
		return flow.Leave(term.ExitFailure)
	}
	p.Kit.Tell(said)
	return nil
}

// descriptionMarker is the file whose presence says the person declined to describe team.
func (p *Picker) descriptionMarker(team string) string {
	return filepath.Join(p.StateDir, "declined", "description-"+team)
}

// maxDescriptionTries is how often a refused description is asked again.
const maxDescriptionTries = 3

// offerDescriptions asks, once per team and whichever checkout this is, to write the
// description of a team that has none: shown wherever teams are listed or chosen.
func (p *Picker) offerDescriptions(ctx context.Context) error {
	missing, err := p.Declare.Undescribed(ctx)
	if err != nil {
		p.Kit.ReportErr(err)
		return flow.Leave(term.ExitFailure)
	}
	for _, team := range missing {
		marker := p.descriptionMarker(team.Team)
		_, statErr := os.Stat(marker)
		if statErr == nil {
			continue
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			p.Kit.Term.PrintError("the marker " + marker + " could not be looked for: " + statErr.Error())
			return flow.Leave(term.ExitFailure)
		}
		if err := p.askDescription(ctx, team, marker); err != nil {
			return err
		}
	}
	return nil
}

func (p *Picker) askDescription(ctx context.Context, team declare.Undescribed, marker string) error {
	if err := p.Kit.NeedGum(); err != nil {
		return err
	}
	yes, err := p.Kit.AskYes(ctx, "Team "+team.Team+" (for "+team.Trusts+") has no description. Write one now?", true)
	if err != nil {
		return err
	}
	if !yes {
		return p.keepAnswer(marker, "Kept. ccy-teams will not ask about "+team.Team+"'s description again.")
	}
	text, err := p.inputDescription(ctx)
	if err != nil || text == "" {
		return err
	}
	if _, err := p.Declare.Describe(ctx, team.Team, text); err != nil {
		p.Kit.ReportErr(err)
		return flow.Leave(term.ExitFailure)
	}
	if err := p.Ops.ApplyTeams(ctx, []string{team.Team}); err != nil {
		p.Kit.Tell("(ccy-teams apply " + team.Team + " failed: see above.)")
	}
	return nil
}

// inputDescription asks for one line until declare.py accepts it, maxDescriptionTries at most; ""
// with no error is a person who gave up, and nothing is written.
func (p *Picker) inputDescription(ctx context.Context) (string, error) {
	value := ""
	for range maxDescriptionTries {
		answer, err := p.Kit.Gum.Input(ctx, "What is this team for?", "e.g. Builds and reviews the example site", value)
		if errors.Is(err, ui.ErrCancelled) {
			return "", p.Kit.Cancelled()
		}
		if err != nil {
			p.Kit.ReportErr(err)
			return "", flow.Leave(term.ExitFailure)
		}
		checkErr := p.Declare.Check(ctx, declare.Entry{Description: answer})
		if checkErr == nil {
			return answer, nil
		}
		if !errors.Is(checkErr, declare.ErrUsage) {
			p.Kit.ReportErr(checkErr)
			return "", flow.Leave(term.ExitFailure)
		}
		p.Kit.ReportErr(checkErr)
		value = answer
	}
	p.Kit.Tell("Giving up after " + strconv.Itoa(maxDescriptionTries) + " tries; nothing was changed.")
	return "", nil
}

// offerUpdate offers to bring the teams that are out of date, or declared and not built, up to
// what is declared.
func (p *Picker) offerUpdate(ctx context.Context) error {
	s, err := p.Board.Refresh(ctx)
	if err != nil {
		return flow.Leave(term.ExitFailure)
	}
	var behind []string
	for _, team := range s.Teams {
		if state := s.Status[team].State; state == declare.ApplyOutdated || state == declare.ApplyUnbuilt {
			behind = append(behind, team+" ("+s.StatusWord(team)+")")
		}
	}
	if len(behind) == 0 {
		return nil
	}
	slices.Sort(behind)
	if err = p.Kit.NeedGum(); err != nil {
		return err
	}
	p.Kit.Term.Say("Not as declared: " + strings.Join(behind, " ") + ".")
	yes, err := p.Kit.AskYes(ctx, "Update them now (play-agent-bus.yml)?", true)
	if err != nil || !yes {
		return err
	}
	if err = p.Kit.InPicker("ccy-teams apply --all", p.Ops.Apply(ctx, []string{ops.FlagAll})); err != nil {
		return err
	}
	p.Kit.BackToList(ctx)
	return nil
}

// checkoutRepo is the lower-case OWNER/REPO of this checkout's origin, or "" when it has none
// on GitHub.
func (p *Picker) checkoutRepo(ctx context.Context) (string, error) {
	url, err := p.Git.RemoteURL(ctx, "")
	if err != nil || url == "" {
		return "", err
	}
	return p.Declare.GithubRemote(ctx, url)
}

// offerJoinHere, on the first pass, offers a session of the team that covers this checkout, or
// the making of one when none does.
func (r *run) offerJoinHere(ctx context.Context) error {
	p := r.p
	here, err := p.Git.ProjectHere(ctx, "")
	if err != nil {
		p.Kit.ReportErr(err)
		return flow.Leave(term.ExitFailure)
	}
	if !here {
		return nil
	}
	repo, err := p.checkoutRepo(ctx)
	if err != nil {
		p.Kit.ReportErr(err)
		return nil
	}
	if repo == "" {
		return nil
	}
	teams, err := p.Declare.TeamsFor(ctx, repo)
	if err != nil {
		p.Kit.ReportErr(err)
		return nil
	}
	if len(teams) == 0 {
		return p.offerCreateHere(ctx, repo)
	}
	top, err := p.Git.TopLevel(ctx, "")
	if err != nil {
		p.Kit.ReportErr(err)
		return flow.Leave(term.ExitFailure)
	}
	for _, team := range teams {
		if !r.snap.Loaded[team] || r.snap.StateOf(team) != systemd.HomeserverUp || r.snap.HoldsLiveSeat(top, team) {
			continue
		}
		if err := p.offerJoin(ctx, top, team); err != nil {
			return err
		}
	}
	return nil
}

// offerCreateHere says that no team works on this checkout's repository, and offers one.
func (p *Picker) offerCreateHere(ctx context.Context, repo string) error {
	if err := p.Kit.NeedGum(); err != nil {
		return err
	}
	p.Kit.Term.Say("No team on this machine works on " + repo + " (this checkout). " +
		"The teams listed next are for other repositories.")
	yes, err := p.Kit.AskYes(ctx, "Create a team for "+repo+" now?", true)
	if err != nil {
		return err
	}
	if !yes {
		p.Kit.Tell(nothingCreated)
		return nil
	}
	return flow.Leave(p.Create.Run(ctx, nil))
}

// offerJoin offers to start a session in top as a member of team.
func (p *Picker) offerJoin(ctx context.Context, top, team string) error {
	if err := p.Kit.NeedGum(); err != nil {
		return err
	}
	yes, err := p.Kit.AskYes(ctx, "Start a ccy session in "+p.Kit.Abbrev(top)+" as a member of "+team+"?", true)
	if err != nil || !yes {
		return err
	}
	return p.Seats.StartCcy(top, team)
}
