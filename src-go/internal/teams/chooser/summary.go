package chooser

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/flow"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// ProbeFunc is what the summary asks of a remembered seat: whether a running session holds it
// now, and its member's role ("" when that cannot be read). It is the one place that knows which
// bus a team is on, so the summary does not.
type ProbeFunc func(ctx context.Context, team, seat, memberType string) (taken bool, role string, err error)

// The words of the launch summary, `ccy-teams launch-summary`: one screen of every default the
// launcher would use, with Enter on launching and one line per thing that can be changed.
const (
	launchRow    = "Launch with these choices"
	problemMark  = " - NEEDS AN ANSWER: "
	summaryCrumb = "Launch summary"
	summaryHelp  = "Enter launches with these choices. Pick a line to change only that one; a line marked NEEDS AN ANSWER must be answered first."

	flagToken     = "--token"
	flagSSH       = "--ssh"
	flagNetwork   = "--network"
	flagTeams     = "--teams"
	flagRoles     = "--roles"
	flagSkipTeams = "--skip-teams"

	// The launcher asks the one question the payload's ASK line names, then shows the summary again.
	askNone    = "none"
	askToken   = "token"
	askSSH     = "ssh"
	askNetwork = "network"

	// exitNotOffered is the code for a row gum returned that the screen did not offer.
	exitNotOffered term.ExitCode = 78
)

// launchTeam is one team of the summary: the remembered seat, the role its member has now, and
// what, if anything, has to be answered about it.
type launchTeam struct {
	team, seat, role string
	problem          string
	kind             problemKind
}

type problemKind int

const (
	fine     problemKind = iota
	gone                 // the team cannot be taken here at all: left out
	scope                // the team does not cover this checkout: widen, join, or leave it out
	seatKind             // the seat is taken, or could not be checked, or its role changed: choose again
)

// summaryOpts are the defaults the launcher hands over.
type summaryOpts struct {
	memberType, token, ssh, network string
	skipTeams                       bool
	teams                           []launchTeam
	saved                           map[string]string
}

// summary is one run of the screen: the defaults, and which teams' scope question was answered.
type summary struct {
	c         *Chooser
	o         summaryOpts
	repo      string
	scopeDone map[string]bool
}

// Summary is `launch-summary --type=podman --token=T --ssh=K --network=N --teams=LIST
// --roles=R [--skip-teams]`: the screen the launcher shows in place of its questions. The payload
// is TEAMS, ROLES and ASK lines; ASK names the one question left to the launcher (token, ssh or
// network) or none when the person accepted.
func (c *Chooser) Summary(ctx context.Context, args []string) term.ExitCode {
	o, ok := c.parseSummary(args)
	if !ok {
		return term.ExitUsage
	}
	if !c.attended() {
		c.usage("'launch-summary' asks on a terminal.")
		return term.ExitUsage
	}
	if code, refused := c.refuseContainerCheckout(ctx); refused {
		return code
	}
	if _, err := c.Host.LookPath("gum"); err != nil {
		c.say("ccy-teams: the launch summary needs gum, and it is not installed; playbooks/imports/play-claude-yolo.yml installs it.")
		return term.ExitFailure
	}
	repo, err := c.checkoutRepo(ctx)
	if err != nil {
		return c.failed(err)
	}
	s := &summary{c: c, o: o, repo: repo, scopeDone: map[string]bool{}}
	return s.run(ctx)
}

func (c *Chooser) parseSummary(args []string) (summaryOpts, bool) {
	o := summaryOpts{saved: map[string]string{}}
	var list string
	for _, a := range args {
		name, value, hasValue := strings.Cut(a, "=")
		if a == flagSkipTeams {
			o.skipTeams = true
			continue
		}
		if !hasValue || !o.take(name, value, &list) {
			c.usage("'launch-summary': unknown argument '" + a + "'.")
			return o, false
		}
	}
	if o.memberType != typePodman && o.memberType != typeHost {
		c.usage("'launch-summary' needs " + flagType + "=podman or " + flagType + "=host.")
		return o, false
	}
	teams, bad := parseSeats(list)
	if bad != "" {
		c.usage("'launch-summary': '" + bad + "' is not SEAT@TEAM.")
		return o, false
	}
	o.teams = teams
	return o, true
}

// take reads one --name=value argument; false when the name is not one of the summary's.
func (o *summaryOpts) take(name, value string, list *string) bool {
	switch name {
	case flagType:
		o.memberType = value
	case flagToken:
		o.token = value
	case flagSSH:
		o.ssh = value
	case flagNetwork:
		o.network = value
	case flagTeams:
		*list = value
	case flagRoles:
		for pair := range strings.SplitSeq(value, ",") {
			if team, role, ok := strings.Cut(pair, "="); ok {
				o.saved[team] = role
			}
		}
	default:
		return false
	}
	return true
}

// parseSeats reads the canonical SEAT@TEAM list; bad is the first item that is not one.
func parseSeats(list string) ([]launchTeam, string) {
	var teams []launchTeam
	for item := range strings.SplitSeq(list, ",") {
		if item == "" {
			continue
		}
		seat, team, ok := strings.Cut(item, "@")
		if !ok || seat == "" || team == "" {
			return nil, item
		}
		teams = append(teams, launchTeam{team: team, seat: seat})
	}
	return teams, ""
}

func (s *summary) run(ctx context.Context) term.ExitCode {
	for {
		entries, err := s.c.entries(ctx, s.repo)
		if err != nil {
			return s.c.failed(err)
		}
		s.assess(ctx, entries)
		rows, def := s.rows()
		answer, err := s.c.Gum.Choose(ctx, ui.Here(flow.LaunchCrumb, summaryCrumb), summaryHelp, def, rows)
		if errors.Is(err, ui.ErrCancelled) {
			s.c.say("Left the launch summary. Nothing started.")
			return term.ExitOK
		}
		if err != nil {
			s.c.Host.ReportErr(err)
			return term.ExitFailure
		}
		if code, done := s.act(ctx, answer, entries); done {
			return code
		}
	}
}

// assess reads each team's standing and seat again and says what has to be answered.
func (s *summary) assess(ctx context.Context, entries []entry) {
	for i := range s.o.teams {
		t := &s.o.teams[i]
		t.problem, t.kind, t.role = "", fine, ""
		at := slices.IndexFunc(entries, func(e entry) bool { return e.Team == t.team })
		if at < 0 {
			t.problem, t.kind = t.team+" is no longer on this machine", gone
			continue
		}
		e := entries[at]
		if why := blocked(s.repo, e); why != "" {
			t.problem, t.kind = shortReason(why), gone
			continue
		}
		taken, role, err := s.c.Probe(ctx, t.team, t.seat, s.o.memberType)
		t.role = role
		switch {
		case e.Standing == declare.StandingWiden && !s.scopeDone[t.team]:
			t.problem, t.kind = t.team+" does not cover "+s.repo, scope
		case err != nil:
			t.problem, t.kind = fmt.Sprintf("seat %s could not be checked: %v", t.seat, err), seatKind
		case taken:
			t.problem, t.kind = "seat "+t.seat+" is held by a running session", seatKind
		case s.o.saved[t.team] != "" && role != "" && s.o.saved[t.team] != role:
			t.problem, t.kind = "its role is now "+role+" (was "+s.o.saved[t.team]+")", seatKind
		}
	}
}

// shortReason is the first clause of a blocked() message, for a line of the summary.
func shortReason(why string) string {
	if before, after, found := strings.Cut(why, ":"); found && after != "" {
		why = before
	}
	return strings.TrimSuffix(why, ".")
}

// seatOf is the seat of a SEAT@TEAM ref.
func seatOf(ref string) string {
	if before, after, found := strings.Cut(ref, "@"); found && after != "" {
		return before
	}
	return ref
}

func orNone(v string) string {
	if v == "" {
		return "none"
	}
	return v
}

func (t launchTeam) row() string {
	role := t.role
	if role == "" {
		role = "role unknown"
	}
	text := fmt.Sprintf("Change team %s (seat %s, %s)", t.team, t.seat, role)
	if t.problem != "" {
		text += problemMark + t.problem
	}
	return text
}

func (s *summary) tokenRow() string   { return "Change token (" + orNone(s.o.token) + ")" }
func (s *summary) sshRow() string     { return "Change SSH key (" + orNone(s.o.ssh) + ")" }
func (s *summary) networkRow() string { return "Change network (" + orNone(s.o.network) + ")" }

// rows are the launch row first, then one row per default; def is the row Enter lands on: the
// first line to be answered, else launching.
func (s *summary) rows() ([]string, string) {
	rows := []string{launchRow, s.tokenRow(), s.sshRow(), s.networkRow()}
	def := launchRow
	if !s.o.skipTeams {
		for _, t := range s.o.teams {
			rows = append(rows, t.row())
			if def == launchRow && t.problem != "" {
				def = t.row()
			}
		}
	}
	return rows, def
}

// act does what the chosen row says; done is true when the run ends with code.
func (s *summary) act(ctx context.Context, answer string, entries []entry) (term.ExitCode, bool) {
	switch answer {
	case launchRow:
		for _, t := range s.o.teams {
			if t.problem != "" && !s.o.skipTeams {
				s.c.say(t.team + " needs an answer before the launch: " + t.problem + ".")
				return term.ExitOK, false
			}
		}
		return s.finish(askNone), true
	case s.tokenRow():
		return s.finish(askToken), true
	case s.sshRow():
		return s.finish(askSSH), true
	case s.networkRow():
		return s.finish(askNetwork), true
	}
	i := slices.IndexFunc(s.o.teams, func(t launchTeam) bool { return t.row() == answer })
	if i < 0 {
		s.c.say("ccy-teams: gum returned a row that was not offered: " + answer)
		return exitNotOffered, true
	}
	return s.change(ctx, i, entries)
}

func (s *summary) finish(ask string) term.ExitCode {
	var refs, roles []string
	if !s.o.skipTeams {
		for _, t := range s.o.teams {
			refs = append(refs, t.seat+"@"+t.team)
			if t.role != "" {
				roles = append(roles, t.team+"="+t.role)
			}
		}
	}
	s.c.Host.Print(s.c.Payload, "TEAMS\t"+strings.Join(refs, ",")+"\nROLES\t"+strings.Join(roles, ",")+"\nASK\t"+ask+"\n")
	return term.ExitOK
}

// change answers the one line: a team that cannot be taken is left out, one that does not cover
// the checkout is asked its scope, and the seat and role are chosen again.
func (s *summary) change(ctx context.Context, i int, entries []entry) (term.ExitCode, bool) {
	t := s.o.teams[i]
	if t.kind == gone {
		s.c.say(t.problem + ". It is left out of this launch.")
		s.drop(i)
		return term.ExitOK, false
	}
	if at := slices.IndexFunc(entries, func(e entry) bool { return e.Team == t.team }); t.kind == scope && at >= 0 {
		kept, code, again := s.c.settle(ctx, s.repo, []entry{entries[at]})
		switch {
		case code != term.ExitOK:
			return code, true
		case again:
			s.drop(i)
			return term.ExitOK, false
		case len(kept) == 0:
			return term.ExitOK, false
		}
		s.scopeDone[t.team] = true
	}
	ref, code := s.c.Seat(ctx, flow.LaunchTeam(i+1, len(s.o.teams), t.team), t.team, s.o.memberType)
	if code != term.ExitOK {
		return code, true
	}
	if ref == "" {
		s.c.say(t.team + " is kept as it was.")
		return term.ExitOK, false
	}
	s.o.teams[i].seat = seatOf(ref)
	delete(s.o.saved, t.team)
	return term.ExitOK, false
}

func (s *summary) drop(i int) {
	delete(s.o.saved, s.o.teams[i].team)
	s.o.teams = slices.Delete(s.o.teams, i, i+1)
}
