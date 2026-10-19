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
	yes, err := p.Kit.AskYes(ctx, flow.Here("Create a team").About("no agent teams on this machine yet"), "Create one now?", false)
	if err != nil {
		return err
	}
	if !yes {
		p.Kit.Tell(nothingCreated)
		return flow.Leave(term.ExitOK)
	}
	return flow.Leave(p.Create.Run(ctx, nil))
}

// pathLimitMarker is the file that says the person chose never to be asked about team's limit.
func (p *Picker) pathLimitMarker(team string) string {
	return filepath.Join(p.StateDir, "declined", "path-limit-"+team)
}

// neverRecord is what a never answer writes in its marker. The yes/no prompt these offers had
// before kept an empty file for a no, chosen with no "later" to choose instead; such a marker is
// taken as not now, once (oldNo), so the person is asked again (the owner's feedback, Plan 00201
// SPEC-many-teams.md S-A15).
const neverRecord = "never v2\n"

// notNowWords is the middle answer of every offer, and what Esc means.
const notNowWords = "Not now (ask again next time)"

// answer is how a person answered a three-way offer.
type answer int

const (
	answerNow answer = iota
	answerLater
	answerNever
)

// askedBefore says whether the person chose never for marker. A marker the old yes/no prompt
// kept is removed and said, and the offer is made again; anything unreadable stops the picker.
func (p *Picker) askedBefore(marker, about string) (bool, error) {
	body, err := os.ReadFile(filepath.Clean(marker))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	case err != nil:
		p.Kit.Term.PrintError("the marker " + marker + " could not be read: " + err.Error())
		return false, flow.Leave(term.ExitFailure)
	case string(body) == neverRecord:
		return true, nil
	}
	if err := os.Remove(marker); err != nil {
		p.Kit.Term.PrintError("the old marker " + marker + " could not be removed: " + err.Error())
		return false, flow.Leave(term.ExitFailure)
	}
	p.Kit.Tell("An earlier no to " + about + ", asked when there was no \"not now\", is taken as not now, so it is asked again.")
	return false, nil
}

// offer is one three-way question: now (the cursor starts there), not now, or never. Esc is
// not now: only the explicit never is kept.
func (p *Picker) offer(ctx context.Context, h ui.Heading, header, now, never string) (answer, error) {
	picked, err := p.Kit.Gum.Choose(ctx, h, header, now, []string{now, notNowWords, never})
	switch {
	case errors.Is(err, ui.ErrCancelled) || picked == notNowWords:
		p.Kit.Tell("Not now: ccy-teams asks again next time.")
		return answerLater, nil
	case err != nil:
		p.Kit.ReportErr(err)
		return answerLater, flow.Leave(term.ExitFailure)
	case picked == never:
		return answerNever, nil
	case picked == now:
		return answerNow, nil
	}
	p.Kit.Term.PrintError("gum returned a choice that was not offered: " + picked)
	return answerLater, flow.Leave(term.ExitFailure)
}

// offerPathLimits asks, per team and whichever checkout this is, whether to lift a limit to
// some folders that a team was made with, until it is lifted or the person says never.
func (p *Picker) offerPathLimits(ctx context.Context) error {
	limits, err := p.Declare.Limited(ctx)
	if err != nil {
		p.Kit.ReportErr(err)
		return flow.Leave(term.ExitFailure)
	}
	for _, limit := range limits {
		marker := p.pathLimitMarker(limit.Team)
		never, err := p.askedBefore(marker, "lifting "+limit.Team+"'s limit")
		if err != nil {
			return err
		}
		if never {
			continue
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
	p.Kit.Term.Say("One question about a team already on this machine, whichever checkout you are in.")
	p.Kit.Term.Say("Team " + team + " (for " + trusts + ") was made when teams were limited to some folders: " +
		"its agents may only point each other at files under " + prefixes + ".")
	h := flow.Here(team, "Folder limit").About("team " + team + " · for " + trusts + " · limited to " + prefixes)
	got, err := p.offer(ctx, h, "Remove "+team+"'s limit, so any path in its repositories can be named?",
		"Remove "+team+"'s limit now", "Keep "+team+"'s limit and never ask again")
	switch {
	case err != nil || got == answerLater:
		return err
	case got == answerNever:
		return p.keepAnswer(marker, "Kept. ccy-teams will not ask about "+team+"'s limit again; the menu of "+team+
			"'s row has "+askLimitLabel(team)+".")
	case got == answerNow:
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

// keepAnswer records a never answer in marker, so the offer is not made again, and says so.
func (p *Picker) keepAnswer(marker, said string) error {
	if err := os.MkdirAll(filepath.Dir(marker), 0o750); err != nil {
		p.Kit.Term.PrintError("the answer could not be kept: " + err.Error())
		return flow.Leave(term.ExitFailure)
	}
	if err := os.WriteFile(marker, []byte(neverRecord), 0o600); err != nil {
		p.Kit.Term.PrintError("the answer could not be kept: " + err.Error())
		return flow.Leave(term.ExitFailure)
	}
	p.Kit.Tell(said)
	return nil
}

// saidNever is whether marker records a never answer; a marker that cannot be read is not one.
func saidNever(marker string) bool {
	body, err := os.ReadFile(filepath.Clean(marker))
	return err == nil && string(body) == neverRecord
}

// askAgain is a team row's undo of a never answer: the marker goes, and the offer is made at the
// next start of ccy-teams.
func (r *run) askAgain(ctx context.Context, marker, what string) (flow.Result, error) {
	if err := os.Remove(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		r.p.Kit.Term.PrintError("the answer could not be forgotten: " + err.Error())
		return r.p.Kit.Note(ctx, "Nothing was changed.")
	}
	return r.p.Kit.Note(ctx, "ccy-teams asks "+what+" again the next time it starts.")
}

// askAgainOf is askAgain for the action ID of a team row's undo line.
func (r *run) askAgainOf(ctx context.Context, id, team string) (flow.Result, error) {
	if id == actAskLimit {
		return r.askAgain(ctx, r.p.pathLimitMarker(team), "about "+team+"'s limit")
	}
	return r.askAgain(ctx, r.p.descriptionMarker(team), "to describe "+team)
}

// askDescriptionLabel and askLimitLabel are the team row's undo lines.
func askDescriptionLabel(team string) string { return "Ask me to describe " + team + " again" }

func askLimitLabel(team string) string { return "Ask me about " + team + "'s limit again" }

// descriptionMarker is the file that says the person chose never to be asked to describe team.
func (p *Picker) descriptionMarker(team string) string {
	return filepath.Join(p.StateDir, "declined", "description-"+team)
}

// maxDescriptionTries is how often a refused description is asked again.
const maxDescriptionTries = 3

// offerDescriptions asks, per team and whichever checkout this is, to write the description of
// a team that has none (shown wherever teams are listed or chosen), until it has one or the
// person says never.
func (p *Picker) offerDescriptions(ctx context.Context) error {
	missing, err := p.Declare.Undescribed(ctx)
	if err != nil {
		p.Kit.ReportErr(err)
		return flow.Leave(term.ExitFailure)
	}
	for _, team := range missing {
		marker := p.descriptionMarker(team.Team)
		never, err := p.askedBefore(marker, "describing "+team.Team)
		if err != nil {
			return err
		}
		if never {
			continue
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
	got, err := p.offer(ctx, describeHeading(team), "Team "+team.Team+" (for "+team.Trusts+") has no description.",
		"Describe "+team.Team+" now", "Never ask about "+team.Team)
	switch {
	case err != nil || got == answerLater:
		return err
	case got == answerNever:
		return p.keepAnswer(marker, "Kept. ccy-teams will not ask about "+team.Team+"'s description again; the menu of "+
			team.Team+"'s row has "+askDescriptionLabel(team.Team)+".")
	case got == answerNow:
	}
	text, err := p.inputDescription(ctx, describeHeading(team), team.Team)
	if err != nil || text == "" {
		return err
	}
	if _, err := p.Declare.Describe(ctx, team.Team, text); err != nil {
		p.Kit.ReportErr(err)
		return flow.Leave(term.ExitFailure)
	}
	if err := p.Ops.ApplyTeams(ctx, []string{team.Team}); err != nil {
		p.Kit.Term.PrintError("team " + team.Team + " now has its description in the host_vars but is not applied as declared. " +
			"Once the cause above is fixed, ccy-teams apply " + team.Team + " applies it.")
		return flow.Leave(term.ExitFailure)
	}
	return nil
}

// describeHeading is the heading of the questions that describe team.
func describeHeading(team declare.Undescribed) ui.Heading {
	return flow.Here(team.Team, "Description").About("team " + team.Team + " · for " + team.Trusts + " · no description yet")
}

// inputDescription asks for team's one line, under h, until declare.py accepts it,
// maxDescriptionTries at most; "" with no error is a person who gave up, and nothing is written.
func (p *Picker) inputDescription(ctx context.Context, h ui.Heading, team string) (string, error) {
	value := ""
	for range maxDescriptionTries {
		answer, err := p.Kit.Gum.Input(ctx, h, "What is team "+team+" for?", "e.g. Builds and reviews the example site", value)
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
	yes, err := p.Kit.AskYes(ctx, flow.Here("Update").About("not as declared: "+strings.Join(behind, " ")), "Update them now (play-agent-bus.yml)?", true)
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
	here, err := p.Checkouts.Find(ctx, "")
	switch {
	case err != nil:
		p.Kit.ReportErr(err)
		return nil
	case here.Top == "":
		return nil
	case !here.Host():
		p.Kit.Tell(here.Refusal(p.Kit.Abbrev(here.Top)))
		return nil
	}
	top := here.Top
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
		return p.offerCreateHere(ctx, repo, top)
	}
	free := func(team string) bool {
		return r.snap.Loaded[team] && r.snap.StateOf(team) == systemd.HomeserverUp && !r.snap.HoldsLiveSeat(top, team)
	}
	if !slices.ContainsFunc(teams, free) {
		return nil
	}
	return p.offerStart(ctx, top, repo, "Start a ccy session in "+p.Kit.Abbrev(top)+"?")
}

// offerCreateHere says that no team works on this checkout's repository, and offers one.
func (p *Picker) offerCreateHere(ctx context.Context, repo, top string) error {
	if err := p.Kit.NeedGum(); err != nil {
		return err
	}
	p.Kit.Term.Say("No team on this machine works on " + repo + " (this checkout). " +
		"The teams listed next are for other repositories.")
	// No by default: creating a team is heavy and outward, and a stray Enter must not do it.
	yes, err := p.Kit.AskYes(ctx, flow.Here("Create a team").About("no team works on "+repo+" (this checkout)"), "Create a team for "+repo+" now?", false)
	if err != nil {
		return err
	}
	if !yes {
		p.Kit.Tell(nothingCreated)
		return p.offerStart(ctx, top, repo, "Start a ccy session in "+p.Kit.Abbrev(top)+" (choose its teams)?")
	}
	return flow.Leave(p.Create.Run(ctx, nil))
}

// offerStart offers a bare `ccy --teams` in top, which lists the teams and lets the person choose
// (adding this repository to a team's scope when the team does not cover it).
func (p *Picker) offerStart(ctx context.Context, top, repo, question string) error {
	if err := p.Kit.NeedGum(); err != nil {
		return err
	}
	yes, err := p.Kit.AskYes(ctx, flow.Here("New session").About("in "+p.Kit.Abbrev(top)+" · "+repo), question, true)
	if err != nil || !yes {
		return err
	}
	return p.Seats.StartCcy(top, "")
}
