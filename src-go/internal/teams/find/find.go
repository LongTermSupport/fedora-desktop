// Package find is `ccy-teams for-checkout`: which teams work on the GitHub repository of the
// checkout it is run in. The ccy launcher calls it for a bare `ccy --teams`, so the person
// need not type the team's name. The matching is declare.py's (a team whose trust covers
// OWNER/REPO, an organisation team by its owner); this package reads the checkout, asks the
// person when several match, and prints the team on stdout and everything else on stderr.
package find

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// FlagChoose asks the person which team, when several cover the checkout.
const FlagChoose = "--choose"

// Find is the for-checkout flow. Every dependency is a field so a test stands in for each.
type Find struct {
	Host    *host.Host
	Git     *gitx.Git
	Declare *declare.Client
	Gum     *ui.Gum
}

// New is the Find flow over a host.
func New(h *host.Host) *Find {
	return &Find{Host: h, Git: h.Git, Declare: h.Declare, Gum: h.Gum}
}

// Run prints the teams covering this checkout, one per line, or with --choose exactly one: the
// only match, or the one the person picks of several.
func (f *Find) Run(ctx context.Context, args []string) term.ExitCode {
	choose := slices.Equal(args, []string{FlagChoose})
	if len(args) > 0 && !choose {
		f.say("ccy-teams: 'for-checkout' takes only " + FlagChoose + ". See ccy-teams --help.")
		return term.ExitUsage
	}
	repo, ok := f.repo(ctx)
	if !ok {
		return term.ExitFailure
	}
	teams, err := f.Declare.TeamsFor(ctx, repo)
	if err != nil {
		f.Host.ReportErr(err)
		return term.ExitFailure
	}
	switch {
	case len(teams) == 0:
		f.say("No team on this machine works on " + repo + " (this checkout). Create one with ccy-teams " +
			"(it offers to when run here), or name a team: ccy --teams <team>.")
		return term.ExitFailure
	case len(teams) == 1:
		f.say("Team " + teams[0] + " works on " + repo + " (this checkout).")
	case choose:
		return f.pick(ctx, repo, teams)
	}
	f.Host.Print(f.Host.Stdout, strings.Join(teams, "\n")+"\n")
	return term.ExitOK
}

// pick asks which of several teams, or fails naming them when nobody is there to ask.
func (f *Find) pick(ctx context.Context, repo string, teams []string) term.ExitCode {
	list := strings.Join(teams, ", ")
	if !f.Host.IsTerminal(f.Host.Stdin) || !f.Host.IsTerminal(f.Host.Stderr) {
		f.say("Several teams work on " + repo + ": " + list + ". Nobody is here to choose; name one: ccy --teams <team>.")
		return term.ExitFailure
	}
	if !hasGum(f.Host) {
		f.say("Several teams work on " + repo + ": " + list + ". gum is not installed to choose one " +
			"(playbooks/imports/play-claude-yolo.yml installs it); name one: ccy --teams <team>.")
		return term.ExitFailure
	}
	choice, err := f.Gum.Choose(ctx, "Several teams work on "+repo+": which one?", teams[0], teams)
	if errors.Is(err, ui.ErrCancelled) {
		f.say("No team chosen; nothing started.")
		return term.ExitCancelled
	}
	if err != nil {
		f.Host.ReportErr(err)
		return term.ExitFailure
	}
	f.Host.Print(f.Host.Stdout, choice+"\n")
	return term.ExitOK
}

// repo is the lower-case OWNER/REPO of this checkout's origin; when there is none it has said why.
func (f *Find) repo(ctx context.Context) (string, bool) {
	inTree, err := f.Git.ProjectHere(ctx, "")
	if err != nil {
		f.Host.ReportErr(err)
		return "", false
	}
	if !inTree {
		f.say("ccy-teams: " + f.Host.Here() + " is not a git checkout, so no team can be found for it. " +
			"Name the team: ccy --teams <team>.")
		return "", false
	}
	url, err := f.Git.RemoteURL(ctx, "")
	if err != nil {
		f.Host.ReportErr(err)
		return "", false
	}
	repo := ""
	if url != "" {
		if repo, err = f.Declare.GithubRemote(ctx, url); err != nil {
			f.Host.ReportErr(err)
			return "", false
		}
	}
	if repo == "" {
		f.say("ccy-teams: this checkout's origin is not a GitHub repository, so no team can be found for it. " +
			"Name the team: ccy --teams <team>.")
	}
	return repo, repo != ""
}

func (f *Find) say(msg string) { f.Host.Print(f.Host.Stderr, msg+"\n") }

func hasGum(h *host.Host) bool {
	_, err := h.LookPath("gum")
	return err == nil
}
