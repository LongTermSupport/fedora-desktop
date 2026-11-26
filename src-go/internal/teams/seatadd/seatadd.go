// Package seatadd adds one teamchat seat for a checkout: its handle derived, never typed, issued
// and placed in one run (Plan 00265 U5, issue #416). `ccy-teams seats add <team> [--host]
// [--human-text]` is Cmd; a caller in process (the launcher's chooser) runs Adder.Add.
//
// The handle is `<repo>.<seat>+<site>.<type>` (A 2.3): repo by guestfacts.RepoName (origin's last
// component, else the checkout's base name, the rule an LXC member's prefix uses), site the
// machine's declared teamchat_site or local (SPEC-cross-network X25; no hostname is read), type
// podman, or host for a host cc seat. The seat is the one freeHandle picks: the lowest number
// that no pool seat of the checkout uses as its seat part (a launch chooses by seat part alone)
// and that the team's roster keeps for no handle of the same prefix, in any state (C 3.7.4).
// A new seat takes no human text unless asked (the owner's word for worker seats).
//
// The issue is an Issuer. SudoIssuer, the one built, runs `teamchat-admin member add ...
// --credential password` on this machine through password sudo (C10): sudo asks the owner's
// password on the terminal, never in argv. The answer, which holds the seat's password, is kept
// in memory and handed to placeissued.Place for the checkout's pool, so it never reaches a
// screen, a file outside the pool, or argv (C13). A team with no status record on this machine
// is served elsewhere and is refused (78), naming the cross-machine pipe; one the host_vars
// declare is refused (78) naming its removal or its missing install instead (register R59). A seat issued but not
// placed names the reissue pipe for its handle. The allocation lock is taken by the placement,
// not across the issue as C 3.7.3 draws it: a second checkout racing for the same number is
// refused by member add (enrolled), which is reported, and nothing is placed twice.
package seatadd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/shellword"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestfacts"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/placeissued"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/membership"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Verb is the word after `ccy-teams seats`.
const Verb = "add"

// ErrNotHere is a team this machine does not serve: no status record of it here.
var ErrNotHere = errors.New("the team is not served here")

// Request is one seat to add.
type Request struct {
	Team paths.TeamID
	// Checkout is the checkout the seat is for, as the launch will see it (its working directory).
	Checkout string
	// Host is a host cc seat (type host); otherwise a ccy (podman) seat.
	Host bool
	// HumanText is whether the seat takes the humans' text.
	HumanText bool
}

// Added is a seat added: its handle, the `<seat>@<team>` a launch names, and the launcher
// (ccy or cc).
type Added struct {
	Handle   proto.Handle
	List     string
	Launcher string
}

// Issue is one seat to issue on the team's homeserver.
type Issue struct {
	Team      paths.TeamID
	Handle    proto.Handle
	HumanText bool
}

// Issuer issues a seat and answers member add's document, which holds the seat's password and
// must reach no stream.
type Issuer interface {
	Issue(ctx context.Context, is Issue) ([]byte, error)
}

// Sudo is the part of internal/sudo SudoIssuer uses: the credential made ready on the terminal,
// then one root command whose streams come back unprinted.
type Sudo interface {
	Ready(ctx context.Context, why string) error
	Capture(ctx context.Context, argv ...string) (execx.Result, error)
}

// SudoIssuer issues on this machine: `teamchat-admin member add` through password sudo.
type SudoIssuer struct{ Sudo Sudo }

// Issue runs member add for is and answers its stdout; a failure carries its stderr only.
func (s SudoIssuer) Issue(ctx context.Context, is Issue) ([]byte, error) {
	if err := s.Sudo.Ready(ctx, "Issuing the seat runs teamchat-admin member add as root."); err != nil {
		return nil, fmt.Errorf("nothing was issued: sudo is not ready (%w)", err)
	}
	pre := is.Handle.Prefix()
	argv := []string{
		paths.AdminBinaryPath, "member", "add", "--team", is.Team.String(), "--prefix", pre.String(), "--seat", is.Handle.Seat,
		"--kind", pre.Type, "--human-text", strconv.FormatBool(is.HumanText), "--credential", "password",
	}
	res, err := s.Sudo.Capture(ctx, argv...)
	if err != nil {
		clear(res.Stdout)
		return nil, fmt.Errorf("nothing was issued: sudo %s failed (%w): %s", strings.Join(argv, " "), err, strings.TrimSpace(string(res.Stderr)))
	}
	return res.Stdout, nil
}

// Adder adds a seat over its seams.
type Adder struct {
	Getenv func(string) string
	// Status is the team's status record on this machine; false when it has none.
	Status func(paths.TeamID) (status.Record, bool, error)
	// Life tells a team with no status record that was removed from this machine (or declared
	// and not installed) from one served on another (system.Life.Check, register R59).
	Life func(paths.TeamID) error
	// Origin is the checkout's origin URL, "" when it has none (gitx.Git.RemoteURL).
	Origin func(ctx context.Context, dir string) (string, error)
	// Site is the machine's declared teamchat_site, "" when undeclared.
	Site   func() (string, error)
	Issuer Issuer
	Place  func(ctx context.Context, d placeissued.Deps, doc io.Reader) (placeissued.Placed, error)
	// Note says progress to a person; nil says nothing.
	Note func(string)
}

// Add derives the seat's handle, issues it and places it in the checkout's pool. ErrNotHere is a
// team with no status record on this machine.
func (a *Adder) Add(ctx context.Context, r Request) (Added, error) {
	store, key, state, err := system.Here(a.Getenv, func() (string, error) { return r.Checkout, nil })
	if err != nil {
		return Added{}, err
	}
	rec, found, err := a.Status(r.Team)
	if err != nil {
		return Added{}, fmt.Errorf("the status record of team %s: %w", r.Team, err)
	}
	if !found {
		if a.Life == nil {
			return Added{}, fmt.Errorf("team %s has no status record here, and whether it was removed cannot be judged: no lifecycle is wired", r.Team)
		}
		if err = a.Life(r.Team); err != nil {
			return Added{}, err
		}
		return Added{}, fmt.Errorf("%w: team %s has no status record on this machine, so its homeserver runs on another one. "+
			"Issue the seat there and place it here: teamchat-admin member add ... --credential password | teamchat seat place-issued --checkout %s "+
			"(docs/agent-bus-cross-network.md, A member on another machine)", ErrNotHere, r.Team, shellword.Quote(r.Checkout))
	}
	pre, err := a.prefix(ctx, r.Checkout, r.Host)
	if err != nil {
		return Added{}, err
	}
	pool, err := system.PoolSeats(store, key, r.Team)
	if err != nil {
		return Added{}, err
	}
	handle, err := freeHandle(pre, pool, rec)
	if err != nil {
		return Added{}, err
	}
	a.note("Seat " + handle.Seat + " of team " + r.Team.String() + " for this checkout: " + handle.String() +
		" (repo " + pre.Repo + ", site " + pre.Site + ", type " + pre.Type + ").")
	doc, err := a.Issuer.Issue(ctx, Issue{Team: r.Team, Handle: handle, HumanText: r.HumanText})
	if err != nil {
		return Added{}, err
	}
	defer clear(doc)
	placed, err := a.Place(ctx, placeissued.OS(state, a.Getenv("XDG_RUNTIME_DIR"), key), bytes.NewReader(doc))
	if err == nil && placed.Seat.String() != handle.String() {
		err = fmt.Errorf("member add answered seat %s, not %s", placed.Seat, handle)
	}
	if err != nil {
		return Added{}, fmt.Errorf("seat %s was issued but not placed: %w. Place it again with: sudo %s member reissue --team %s --handle %s --credential password | teamchat seat place-issued --checkout %s",
			handle, err, paths.AdminBinaryPath, r.Team, handle, shellword.Quote(r.Checkout))
	}
	added := Added{Handle: handle, List: handle.Seat + "@" + r.Team.String(), Launcher: "ccy"}
	if r.Host {
		added.Launcher = "cc"
	}
	a.note("Seat " + added.List + " is in this checkout's pool.")
	return added, nil
}

// NewSeat is the launcher's add of a seat (register R5), the chooser's Teamchat.NewSeat: when a
// team named alone has no free seat of the launcher's kind (memberType podman for ccy, host for
// cc), a seat of that kind is added to the working directory's checkout, as `ccy-teams seats
// add` would, and its <seat>@<team> is the launch's. Progress and refusals go to say (stderr): a
// team served on another machine is 78, naming the route there, anything else 1.
func (a *Adder) NewSeat(getwd func() (string, error), say func(string)) func(ctx context.Context, team, memberType string) (string, term.ExitCode) {
	return func(ctx context.Context, team, memberType string) (string, term.ExitCode) {
		id, err := paths.ParseTeamID(team)
		if err != nil {
			say("ccy-teams: no new seat of " + team + " could be added: " + err.Error() + ".")
			return "", term.ExitUsage
		}
		checkout, err := getwd()
		if err != nil {
			say("ccy-teams: no new seat of " + team + " could be added: the working directory: " + err.Error() + ".")
			return "", term.ExitFailure
		}
		adder := *a
		adder.Note = say
		added, err := adder.Add(ctx, Request{Team: id, Checkout: checkout, Host: memberType == "host"})
		switch {
		case errors.Is(err, ErrNotHere), errors.Is(err, system.ErrTeamGone):
			say("ccy-teams: no new seat of " + team + " could be added here: " + err.Error() + ".")
			return "", term.ExitConfig
		case err != nil:
			say("ccy-teams: no new seat of " + team + " could be added: " + err.Error() + ".")
			return "", term.ExitFailure
		}
		say("This " + added.Launcher + " launch takes seat " + added.List + ".")
		return added.List, term.ExitOK
	}
}

func (a *Adder) note(msg string) {
	if a.Note != nil {
		a.Note(msg)
	}
}

// prefix is the checkout's handle prefix for the seat type.
func (a *Adder) prefix(ctx context.Context, checkout string, host bool) (proto.Prefix, error) {
	origin, err := a.Origin(ctx, checkout)
	if err != nil {
		return proto.Prefix{}, fmt.Errorf("the checkout's origin: %w", err)
	}
	repo, err := guestfacts.RepoName(origin, origin != "", checkout)
	if err != nil {
		return proto.Prefix{}, err
	}
	site, err := a.Site()
	if err != nil {
		return proto.Prefix{}, fmt.Errorf("the machine's teamchat_site: %w", err)
	}
	if site == "" {
		site = guestfacts.DefaultSite
	}
	typ := "podman"
	if host {
		typ = "host"
	}
	p, ok := proto.ParsePrefix(repo + "+" + site + "." + typ)
	if !ok {
		return proto.Prefix{}, fmt.Errorf("repo %q and site %q do not make a handle prefix", repo, site)
	}
	return p, nil
}

// freeHandle is the one choice of the seat under pre: the lowest number whose seat part no pool
// seat uses and that the roster keeps for no handle of pre. A named seat would be chosen here.
func freeHandle(pre proto.Prefix, pool []paths.SeatName, rec status.Record) (proto.Handle, error) {
	used := map[string]bool{}
	for _, n := range pool {
		if h, ok := proto.ParseHandle(n.String()); ok {
			used[h.Seat] = true
		}
	}
	for _, m := range rec.Members {
		if h, ok := proto.ParseHandle(m.Handle); ok && h.Prefix() == pre {
			used[h.Seat] = true
		}
	}
	for n := 1; ; n++ {
		h, ok := pre.Numbered(n)
		if !ok {
			return proto.Handle{}, fmt.Errorf("no seat number is free under %s", pre)
		}
		if !used[h.Seat] {
			return h, nil
		}
	}
}

// Cmd is `ccy-teams seats add` over an Adder: the working directory's checkout, notes on
// stderr, the seat to launch on stdout.
type Cmd struct {
	Stdout, Stderr io.Writer
	Getwd          func() (string, error)
	Adder          *Adder

	noteErr error
}

const usageLine = "ccy-teams seats add <team> [--host] [--human-text]"

const prefix = "ccy-teams seats add: "

// errUsage is an argument the verb cannot use (64).
var errUsage = errors.New("usage")

// Run is the verb over its arguments (after `seats add`).
func (c *Cmd) Run(ctx context.Context, args []string) term.ExitCode {
	err := c.run(ctx, args)
	switch {
	case err == nil:
		return term.ExitOK
	case errors.Is(err, errUsage):
		return c.say(term.ExitUsage, prefix+err.Error()+". Usage: "+usageLine+". See ccy-teams --help.")
	case errors.Is(err, ErrNotHere), errors.Is(err, system.ErrTeamGone):
		return c.say(term.ExitConfig, prefix+err.Error())
	}
	return c.say(term.ExitFailure, prefix+err.Error())
}

func (c *Cmd) run(ctx context.Context, args []string) error {
	r, err := parse(args)
	if err != nil {
		return err
	}
	if r.Checkout, err = c.Getwd(); err != nil {
		return fmt.Errorf("the working directory: %w", err)
	}
	adder := *c.Adder
	adder.Note = c.note
	added, err := adder.Add(ctx, r)
	if err != nil {
		return err
	}
	c.note("Launch it here: " + membership.Launch(added.Launcher, added.List))
	if _, err = io.WriteString(c.Stdout, added.List+"\n"); err != nil {
		return fmt.Errorf("could not print the seat: %w", err)
	}
	if c.noteErr != nil {
		return fmt.Errorf("the seat is placed, but stderr could not be written: %w", c.noteErr)
	}
	return nil
}

func parse(args []string) (Request, error) {
	var r Request
	var teams []string
	for _, a := range args {
		switch {
		case a == "--host":
			r.Host = true
		case a == "--human-text":
			r.HumanText = true
		case strings.HasPrefix(a, "-"):
			return r, fmt.Errorf("%w: unknown option %s", errUsage, a)
		default:
			teams = append(teams, a)
		}
	}
	if len(teams) != 1 {
		return r, fmt.Errorf("%w: it takes one team", errUsage)
	}
	team, err := paths.ParseTeamID(teams[0])
	if err != nil {
		return r, fmt.Errorf("%w: the team: %w", errUsage, err)
	}
	r.Team = team
	return r, nil
}

// note says msg on stderr; the first write that fails is kept, and fails the run at its end.
func (c *Cmd) note(msg string) {
	if _, err := io.WriteString(c.Stderr, msg+"\n"); err != nil && c.noteErr == nil {
		c.noteErr = err
	}
}

func (c *Cmd) say(code term.ExitCode, msg string) term.ExitCode {
	if _, err := io.WriteString(c.Stderr, msg+"\n"); err != nil {
		return term.ExitFailure
	}
	return code
}
