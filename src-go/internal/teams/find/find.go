// Package find is `ccy-teams for-checkout`: which teams work on the GitHub repository of the
// checkout it is run in. The matching is declare.py's (a team whose trust covers OWNER/REPO, an
// organisation team by its owner, honouring its exclude); this package reads the checkout and
// prints the teams on stdout and everything else on stderr. Choosing among them, and the teams
// that do not cover the checkout, is choose-teams.
package find

import (
	"context"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Find is the for-checkout flow. Every dependency is a field so a test stands in for each.
type Find struct {
	Host    *host.Host
	Git     *gitx.Git
	Declare *declare.Client
}

// New is the Find flow over a host.
func New(h *host.Host) *Find {
	return &Find{Host: h, Git: h.Git, Declare: h.Declare}
}

// Run prints the teams covering this checkout, one per line, for scripts.
func (f *Find) Run(ctx context.Context, args []string) term.ExitCode {
	if len(args) > 0 {
		f.say("ccy-teams: 'for-checkout' takes no arguments. See ccy-teams --help.")
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
	if len(teams) == 0 {
		f.say("No team on this machine works on " + repo + " (this checkout). Create one with ccy-teams " +
			"(it offers to when run here), or name a team: ccy --teams <team>.")
		return term.ExitFailure
	}
	f.Host.Print(f.Host.Stdout, strings.Join(teams, "\n")+"\n")
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
