// Package chooser is `ccy-teams choose-teams --type=podman|host [LIST]`: every question between
// "the owner typed --teams" and "the seats are known", so the launcher keeps none of it. Given a
// LIST it only resolves each team named alone to a seat; with no list it shows every team on
// this machine with what it is for and how it stands to this checkout, offers creating one, and
// when the team chosen does not cover the checkout offers to add the repository to its scope
// (declare.py add-repo, then one play run) before the seat is chosen. The payload on stdout is
// the canonical SEAT@TEAM[,...] list and nothing else; every other line goes to stderr, and
// leaving any screen prints nothing and succeeds, as the launcher expects.
package chooser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/systemd"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// maxRounds is how often the list is shown again after a team could not be taken.
const maxRounds = 3

// Flags and member types.
const (
	flagType   = "--type"
	typePodman = "podman"
	typeHost   = "host"
)

// SeatFunc chooses the seat for a team and offers its role: the SEAT@TEAM ref, or "" with ExitOK
// when the person left a question.
type SeatFunc func(ctx context.Context, team, memberType string) (string, term.ExitCode)

// CreateFunc runs the create wizard for this checkout: the team it made, or "" and the code.
type CreateFunc func(ctx context.Context) (string, term.ExitCode)

// ApplyFunc applies the declared teams (the play), saying why it failed when it does.
type ApplyFunc func(ctx context.Context, teams []string) error

// Chooser is the choose-teams flow. Every dependency is a field so a test stands in for each.
type Chooser struct {
	Host    *host.Host
	Declare *declare.Client
	Systemd *systemd.Systemd
	Gum     *ui.Gum
	Picker  *ui.Picker
	// Payload receives the one value the launcher captures.
	Payload io.Writer

	Seat   SeatFunc
	Create CreateFunc
	Apply  ApplyFunc
	// Now stamps the owner's acceptance.
	Now func() time.Time
}

// New is the chooser over a host and the flows it hands the work to.
func New(h *host.Host, seat SeatFunc, create CreateFunc, apply ApplyFunc) *Chooser {
	return &Chooser{
		Host: h, Declare: h.Declare, Systemd: h.Systemd, Gum: h.Gum, Picker: h.Picker, Payload: h.Payload,
		Seat: seat, Create: create, Apply: apply, Now: time.Now,
	}
}

// Run is choose-teams with the arguments after its name.
func (c *Chooser) Run(ctx context.Context, args []string) term.ExitCode {
	memberType, list, ok := c.parse(args)
	if !ok {
		return term.ExitUsage
	}
	if !c.Host.IsTerminal(c.Host.Stdin) || !c.Host.IsTerminal(c.Host.Stderr) {
		c.say("ccy-teams: 'choose-teams' asks its questions on a terminal; with none, name the seats: --teams SEAT@TEAM.")
		return term.ExitUsage
	}
	if list != "" {
		return c.resolve(ctx, memberType, list)
	}
	return c.bare(ctx, memberType)
}

// parse reads --type=T (or --type T) and the one optional list.
func (c *Chooser) parse(args []string) (string, string, bool) {
	var memberType string
	var rest []string
	for i := 0; i < len(args); i++ {
		switch value, isValue := strings.CutPrefix(args[i], flagType+"="); {
		case isValue:
			memberType = value
		case args[i] == flagType && i+1 < len(args):
			i++
			memberType = args[i]
		case strings.HasPrefix(args[i], "-"):
			c.usage("unknown option '" + args[i] + "'.")
			return "", "", false
		default:
			rest = append(rest, args[i])
		}
	}
	if memberType != typePodman && memberType != typeHost {
		c.usage("'choose-teams' needs " + flagType + "=podman or " + flagType + "=host.")
		return "", "", false
	}
	if len(rest) > 1 {
		c.usage("'choose-teams' takes at most one list.")
		return "", "", false
	}
	list := ""
	if len(rest) == 1 {
		list = rest[0]
	}
	return memberType, list, true
}

func (c *Chooser) usage(msg string) {
	c.say("ccy-teams: " + msg + " See ccy-teams --help.")
}

func (c *Chooser) say(msg string) { c.Host.Print(c.Host.Stderr, msg+"\n") }

// printList writes the payload; finish (consent.go) is the way to it.
func (c *Chooser) printList(refs []string) term.ExitCode {
	c.Host.Print(c.Payload, strings.Join(refs, ",")+"\n")
	return term.ExitOK
}

// resolve is a given list: each seat kept, each team named alone replaced by the seat the
// person chooses for it, in order.
func (c *Chooser) resolve(ctx context.Context, memberType, list string) term.ExitCode {
	var refs []string
	for item := range strings.SplitSeq(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			c.usage("the list '" + list + "' has an empty item.")
			return term.ExitUsage
		}
		if !strings.Contains(item, "@") {
			ref, code := c.Seat(ctx, item, memberType)
			if code != term.ExitOK {
				return code
			}
			if ref == "" {
				return term.ExitOK
			}
			item = ref
		}
		refs = append(refs, item)
	}
	return c.finish(ctx, memberType, refs)
}

// entry is a present team as the list shows it.
type entry struct {
	declare.Choice

	down bool
}

// bare is no list: show the teams and settle the one chosen.
func (c *Chooser) bare(ctx context.Context, memberType string) term.ExitCode {
	if _, err := c.Host.LookPath("fzf"); err != nil {
		c.say("ccy-teams: choosing a team needs fzf, and it is not installed; playbooks/imports/play-claude-yolo.yml installs it.")
		return term.ExitFailure
	}
	repo, err := c.checkoutRepo(ctx)
	if err != nil {
		return c.failed(err)
	}
	entries, err := c.entries(ctx, repo)
	if err != nil {
		return c.failed(err)
	}
	if repo != "" && !slices.ContainsFunc(entries, func(e entry) bool { return e.Standing == declare.StandingCovers }) {
		c.say("No team on this machine works on " + repo + " (this checkout). Choose a team to add it to, or create one.")
	}
	var held []entry
	for round := range maxRounds {
		taken, code, again := c.take(ctx, repo, entries, round == 0, held)
		if again {
			held = taken
			continue
		}
		if len(taken) == 0 {
			return code
		}
		final, code, again := c.settle(ctx, repo, taken)
		if again {
			held = nil // every team was left out: that answer stands, nothing is held
			continue
		}
		if len(final) == 0 {
			return code
		}
		return c.seats(ctx, memberType, final)
	}
	c.say("ccy-teams: no team could be taken in " + strconv.Itoa(maxRounds) + " tries; nothing started.")
	return term.ExitFailure
}

// failed says why something could not be read and exits with declare's own code when it gave one.
func (c *Chooser) failed(err error) term.ExitCode {
	c.Host.ReportErr(err)
	if de, ok := errors.AsType[*declare.Error](err); ok && de.Code != 0 {
		return term.ExitCode(de.Code)
	}
	return term.ExitFailure
}

// checkoutRepo is the lower-case OWNER/REPO of this checkout's origin, or "" when this is not a
// GitHub checkout (which the list then says: nothing is widened).
func (c *Chooser) checkoutRepo(ctx context.Context) (string, error) {
	inTree, err := c.Host.Git.ProjectHere(ctx, "")
	if err != nil || !inTree {
		return "", err
	}
	url, err := c.Host.Git.RemoteURL(ctx, "")
	if err != nil || url == "" {
		return "", err
	}
	return c.Declare.GithubRemote(ctx, url)
}

// entries are the present teams, covering ones first, each in declared order, with whether its
// homeserver is up.
func (c *Chooser) entries(ctx context.Context, repo string) ([]entry, error) {
	choices, err := c.choices(ctx, repo)
	if err != nil {
		return nil, err
	}
	entries := make([]entry, 0, len(choices))
	for _, choice := range choices {
		state, err := c.Systemd.HomeserverState(ctx, choice.Team)
		if err != nil {
			return nil, fmt.Errorf("is %s's homeserver up: %w", choice.Team, err)
		}
		entries = append(entries, entry{Choice: choice, down: state != systemd.HomeserverUp})
	}
	slices.SortStableFunc(entries, func(a, b entry) int {
		return coverRank(a) - coverRank(b)
	})
	return entries, nil
}

// choices are the present teams with their standing to the repository; with no repository
// (not a GitHub checkout) they have none.
func (c *Chooser) choices(ctx context.Context, repo string) ([]declare.Choice, error) {
	if repo != "" {
		return c.Declare.Choices(ctx, repo)
	}
	teams, err := c.Declare.Declared(ctx)
	if err != nil {
		return nil, err
	}
	var choices []declare.Choice
	for _, t := range teams {
		if t.Present {
			choices = append(choices, declare.Choice{Team: t.Name, Description: t.Description, DescriptionState: t.DescriptionState})
		}
	}
	return choices, nil
}

func coverRank(e entry) int {
	if e.Standing == declare.StandingCovers {
		return 0
	}
	return 1
}
