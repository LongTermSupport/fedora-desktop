// Package seatadd is `ccy-teams seats add <team> [--host] [--no-human-text]` (Plan 00265 U5,
// issue #416): one teamchat seat for the working directory's checkout, issued and placed in one
// run on the machine that serves the team, its name derived and never typed.
//
// The handle is `<repo>.<seat>+<site>.<type>` (A 2.3): repo by guestfacts.RepoName (origin's last
// component, else the checkout's base name, the rule an LXC member's prefix uses), site the
// machine's declared teamchat_site or local (SPEC-cross-network X25; no hostname is read), type
// podman, or host with --host. The seat number is the lowest one that no pool seat of this
// checkout uses as its seat part (a launch chooses by seat part alone) and that the team's roster
// keeps for no handle of the same prefix, in any state (C 3.7.4).
//
// The issue is `teamchat-admin member add ... --credential password` through password sudo (C10):
// the owner's password is asked by sudo itself on the terminal, never in argv. Its answer, which
// holds the seat's password, is captured from the pipe and handed in memory to placeissued.Place
// for this checkout's pool, so it never reaches a screen, a file outside the pool, or argv
// (C13). A team with no status record on this machine is served elsewhere and is refused (78),
// naming the cross-machine pipe. A seat issued but not placed names the reissue pipe for its
// handle. The allocation lock is taken by the placement, not across the sudo call as C 3.7.3
// draws it: a second checkout racing for the same number is refused by member add (enrolled),
// which this verb reports, and nothing is placed twice.
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
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Verb is the word after `ccy-teams seats`.
const Verb = "add"

// Sudo is the part of internal/sudo the verb uses: the credential made ready on the terminal,
// then one root command whose streams come back unprinted.
type Sudo interface {
	Ready(ctx context.Context, why string) error
	Capture(ctx context.Context, argv ...string) (execx.Result, error)
}

// Cmd is the verb over its seams.
type Cmd struct {
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	Getwd          func() (string, error)
	// Status is the team's status record on this machine; false when it has none.
	Status func(paths.TeamID) (status.Record, bool, error)
	// Origin is the checkout's origin URL, "" when it has none (gitx.Git.RemoteURL).
	Origin func(ctx context.Context, dir string) (string, error)
	// Site is the machine's declared teamchat_site, "" when undeclared.
	Site  func() (string, error)
	Sudo  Sudo
	Place func(ctx context.Context, d placeissued.Deps, doc io.Reader) (placeissued.Placed, error)

	noteErr error
}

const usageLine = "ccy-teams seats add <team> [--host] [--no-human-text]"

const prefix = "ccy-teams seats add: "

// errUsage is an argument the verb cannot use (64).
var errUsage = errors.New("usage")

// errNotHere is a team this machine does not serve (78).
var errNotHere = errors.New("not served here")

// options are the parsed arguments.
type options struct {
	team      paths.TeamID
	host      bool
	humanText bool
}

// Run is the verb over its arguments (after `seats add`).
func (c *Cmd) Run(ctx context.Context, args []string) term.ExitCode {
	o, err := parse(args)
	if err == nil {
		err = c.add(ctx, o)
	}
	switch {
	case err == nil:
		return term.ExitOK
	case errors.Is(err, errUsage):
		return c.say(term.ExitUsage, prefix+err.Error()+". Usage: "+usageLine+". See ccy-teams --help.")
	case errors.Is(err, errNotHere):
		return c.say(term.ExitConfig, prefix+err.Error())
	}
	return c.say(term.ExitFailure, prefix+err.Error())
}

func parse(args []string) (options, error) {
	o := options{humanText: true}
	var teams []string
	for _, a := range args {
		switch {
		case a == "--host":
			o.host = true
		case a == "--no-human-text":
			o.humanText = false
		case strings.HasPrefix(a, "-"):
			return o, fmt.Errorf("%w: unknown option %s", errUsage, a)
		default:
			teams = append(teams, a)
		}
	}
	if len(teams) != 1 {
		return o, fmt.Errorf("%w: it takes one team", errUsage)
	}
	team, err := paths.ParseTeamID(teams[0])
	if err != nil {
		return o, fmt.Errorf("%w: the team: %w", errUsage, err)
	}
	o.team = team
	return o, nil
}

// add derives the handle, issues it and places it.
func (c *Cmd) add(ctx context.Context, o options) error {
	wd, err := c.Getwd()
	if err != nil {
		return fmt.Errorf("the working directory: %w", err)
	}
	store, key, state, err := system.Here(c.Getenv, func() (string, error) { return wd, nil })
	if err != nil {
		return err
	}
	rec, found, err := c.Status(o.team)
	if err != nil {
		return fmt.Errorf("the status record of team %s: %w", o.team, err)
	}
	if !found {
		return fmt.Errorf("%w: team %s has no status record on this machine, so its homeserver runs on another one. "+
			"Issue the seat there and place it here: teamchat-admin member add ... --credential password | teamchat seat place-issued --checkout %s "+
			"(docs/agent-bus-cross-network.md, A member on another machine)", errNotHere, o.team, shellword.Quote(wd))
	}
	pre, err := c.prefix(ctx, wd, o.host)
	if err != nil {
		return err
	}
	pool, err := system.PoolSeats(store, key, o.team)
	if err != nil {
		return err
	}
	handle, err := freeHandle(pre, pool, rec)
	if err != nil {
		return err
	}
	c.note("Seat " + handle.Seat + " of team " + o.team.String() + " for this checkout: " + handle.String() +
		" (repo " + pre.Repo + ", site " + pre.Site + ", type " + pre.Type + ").")
	if err = c.Sudo.Ready(ctx, "Issuing the seat runs teamchat-admin member add as root."); err != nil {
		return fmt.Errorf("nothing was issued: sudo is not ready (%w)", err)
	}
	argv := []string{
		paths.AdminBinaryPath, "member", "add", "--team", o.team.String(), "--prefix", pre.String(), "--seat", handle.Seat,
		"--kind", pre.Type, "--human-text", strconv.FormatBool(o.humanText), "--credential", "password",
	}
	res, err := c.Sudo.Capture(ctx, argv...)
	defer clear(res.Stdout)
	if err != nil {
		return fmt.Errorf("nothing was placed: sudo %s failed (%w): %s", strings.Join(argv, " "), err, strings.TrimSpace(string(res.Stderr)))
	}
	placed, err := c.Place(ctx, placeissued.OS(state, c.Getenv("XDG_RUNTIME_DIR"), key), bytes.NewReader(res.Stdout))
	if err == nil && placed.Seat.String() != handle.String() {
		err = fmt.Errorf("member add answered seat %s, not %s", placed.Seat, handle)
	}
	if err != nil {
		return fmt.Errorf("seat %s was issued but not placed: %w. Place it again with: sudo %s member reissue --team %s --handle %s --credential password | teamchat seat place-issued --checkout %s",
			handle, err, paths.AdminBinaryPath, o.team, handle, shellword.Quote(wd))
	}
	launcher := "ccy"
	if o.host {
		launcher = "cc"
	}
	list := handle.Seat + "@" + o.team.String()
	c.note("Seat " + list + " is in this checkout's pool. Launch it here: " + launcher + " --teams " + list)
	if _, err = io.WriteString(c.Stdout, list+"\n"); err != nil {
		return fmt.Errorf("could not print the seat: %w", err)
	}
	if c.noteErr != nil {
		return fmt.Errorf("the seat is placed, but stderr could not be written: %w", c.noteErr)
	}
	return nil
}

// prefix is this checkout's handle prefix for the seat type.
func (c *Cmd) prefix(ctx context.Context, wd string, host bool) (proto.Prefix, error) {
	origin, err := c.Origin(ctx, wd)
	if err != nil {
		return proto.Prefix{}, fmt.Errorf("the checkout's origin: %w", err)
	}
	repo, err := guestfacts.RepoName(origin, origin != "", wd)
	if err != nil {
		return proto.Prefix{}, err
	}
	site, err := c.Site()
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

// freeHandle is the lowest-numbered handle under pre whose seat part no pool seat uses and whose
// number the roster keeps for no handle of pre.
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
