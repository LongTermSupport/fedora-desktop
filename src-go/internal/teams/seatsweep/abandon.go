package seatsweep

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/system"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// abandonUsage is the grammar of `seats abandon`.
const abandonUsage = `seats abandon <team> [<member>...] --reason "<text>" [--allow-known-team <team>]`

// Flags of `seats abandon`.
const (
	flagReason    = "--reason"
	flagAllowKnow = "--allow-known-team"
)

// abandonArgs is one `seats abandon` command line.
type abandonArgs struct {
	team    paths.TeamID
	members []string
	reason  string
	allow   bool
}

// parseAbandon reads the arguments after `abandon`: the team, the members, a reason that is one
// line of text and not blank, and an override that repeats the team.
func parseAbandon(args []string) (abandonArgs, error) {
	var a abandonArgs
	positional, reason, allow, err := scanAbandon(args)
	if err != nil {
		return a, err
	}
	if len(positional) == 0 {
		return a, fmt.Errorf("%w: no team; takes %s", errUsage, abandonUsage)
	}
	team, err := paths.ParseTeamID(positional[0])
	if err != nil {
		return a, fmt.Errorf("%w: team %s: %w", errUsage, strconv.Quote(positional[0]), err)
	}
	a.team, a.members = team, positional[1:]
	if reason == nil || strings.TrimSpace(*reason) == "" {
		return a, fmt.Errorf("%w: %s \"<text>\" is required and must not be empty: it is kept in each seat's released.json", errUsage, flagReason)
	}
	if (seat.Abandonment{Reason: *reason, By: "-"}).Check() != nil {
		return a, fmt.Errorf("%w: %s must be one line of text", errUsage, flagReason)
	}
	a.reason = *reason
	if allow != nil {
		if *allow != team.String() {
			return a, fmt.Errorf("%w: %s must repeat the team being abandoned (%s), not %s", errUsage, flagAllowKnow, team, strconv.Quote(*allow))
		}
		a.allow = true
	}
	return a, nil
}

// scanAbandon splits the arguments into the positional ones and the two flags' values (nil
// when not given), each flag as `--flag value` or `--flag=value`, given at most once.
func scanAbandon(args []string) (positional []string, reason, allow *string, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, inline := strings.Cut(arg, "=")
		var target **string
		switch name {
		case flagReason:
			target = &reason
		case flagAllowKnow:
			target = &allow
		default:
			if strings.HasPrefix(arg, "-") {
				return nil, nil, nil, fmt.Errorf("%w: unknown option %s; takes %s", errUsage, strconv.Quote(arg), abandonUsage)
			}
			positional = append(positional, arg)
			continue
		}
		if *target != nil {
			return nil, nil, nil, fmt.Errorf("%w: %s given twice", errUsage, name)
		}
		if !inline {
			if i+1 >= len(args) {
				return nil, nil, nil, fmt.Errorf("%w: %s takes a value; takes %s", errUsage, name, abandonUsage)
			}
			i++
			value = args[i]
		}
		*target = &value
	}
	return positional, reason, allow, nil
}

// abandon is `seats abandon`: each held seat of the team (or of the members named) in the
// working directory's checkout released without its logout, on the caller's word that the
// team's server will never answer that logout again (issue 386). It contacts no server. One
// line per seat abandoned on stdout; a seat that is not held is said on stderr and is no error,
// but a run that abandons nothing (no seat of the team in this checkout's pool, or none held) is
// refused, since it is most likely run from the wrong directory or for a mistyped team.
// Exit 64 for a usage error, 78 for nothing to abandon here or a team a local source still
// knows (or a source that cannot be read), 75 when a seat's link is running for that team or
// its seat lock cannot be looked at, 1 for any other failure.
func (c *Cmd) abandon(ctx context.Context, args []string) term.ExitCode {
	a, err := parseAbandon(args)
	if err != nil {
		return c.fail(err, term.ExitUsage)
	}
	store, key, home, err := system.Here(c.Getenv, c.Getwd)
	if err != nil {
		return c.fail(err, term.ExitFailure)
	}
	seats, err := c.chosen(store, key, a)
	switch {
	case errors.Is(err, errUsage):
		return c.fail(err, term.ExitUsage)
	case errors.Is(err, errNothing):
		return c.fail(err, term.ExitConfig)
	case err != nil:
		return c.fail(err, term.ExitFailure)
	}
	if !a.allow {
		if why, knownErr := c.knownBy(store, a.team); knownErr != nil || why != "" {
			return c.fail(refusal(a.team, why, knownErr), term.ExitConfig)
		}
	}
	by, err := c.Who()
	if err != nil {
		return c.fail(fmt.Errorf("who is calling: %w", err), term.ExitFailure)
	}
	cfg := c.Parts
	cfg.StateHome, cfg.RuntimeDir, cfg.Checkout = home, c.Getenv("XDG_RUNTIME_DIR"), key
	s, err := c.Open(cfg)
	if err != nil {
		return c.fail(err, term.ExitFailure)
	}
	code, abandoned := c.abandonEach(ctx, s, seats, a, by)
	if code == term.ExitOK && abandoned == 0 {
		code = c.fail(nothingHere(a.team, store.PoolTeam(key, a.team), "has no seat held in this checkout's pool"), term.ExitConfig)
	}
	if err := s.Close(); err != nil {
		return c.fail(err, term.ExitFailure)
	}
	return code
}

// errNothing is a run of `seats abandon` that has nothing to abandon in this checkout.
var errNothing = errors.New("nothing to abandon")

// nothingHere is the refusal of a run that would abandon nothing: the team, what was found and
// the pool it looked in, and where the verb must be run.
func nothingHere(team paths.TeamID, pool, what string) error {
	return fmt.Errorf("%w: team %s %s (%s); run it from the checkout the seats were taken in", errNothing, team, what, pool)
}

// abandonEach abandons each seat on its own: the worst outcome is the exit, 1 over 75 over 0,
// and the number of seats abandoned.
func (c *Cmd) abandonEach(ctx context.Context, s Store, seats []paths.SeatName, a abandonArgs, by string) (term.ExitCode, int) {
	failed, busy, abandoned := false, false, 0
	for _, name := range seats {
		one := seat.Seat{Team: a.team, Name: name}
		got, err := s.Abandon(ctx, one, a.reason, by)
		if got.Held {
			abandoned++
			if werr := c.print(fmt.Sprintf("abandoned seat %s of team %s (session %s)\n", name, a.team, got.Session)); werr != nil {
				err = errors.Join(err, fmt.Errorf("seat %s of team %s was abandoned, but stdout could not say so: %w", name, a.team, werr))
			}
		} else if err == nil {
			err = c.sayAbandon(fmt.Errorf("seat %s of team %s is not held: nothing to do", name, a.team))
		}
		if err == nil {
			continue
		}
		if errors.Is(err, seat.ErrRunning) && !got.Held {
			busy = true
		} else {
			failed = true
		}
		if werr := c.sayAbandon(fmt.Errorf("seat %s of team %s: %w", name, a.team, err)); werr != nil {
			return term.ExitFailure, abandoned
		}
	}
	switch {
	case failed:
		return term.ExitFailure, abandoned
	case busy:
		return term.ExitBusy, abandoned
	}
	return term.ExitOK, abandoned
}

// chosen is the pool seats of the team in the checkout: all of them, or the members named, each
// by its whole name or its handle's seat part. No pool seat of the team at all is errNothing; a
// name that is no pool seat of the team is a usage error, so a typo never abandons nothing
// quietly; a pool that cannot be read is any other error.
func (c *Cmd) chosen(store paths.Store, key paths.CheckoutKey, a abandonArgs) ([]paths.SeatName, error) {
	all, err := system.PoolSeats(store, key, a.team)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, nothingHere(a.team, store.PoolTeam(key, a.team), "has no seat in this checkout's pool")
	}
	if len(a.members) == 0 {
		return all, nil
	}
	var out []paths.SeatName
	for _, m := range a.members {
		i := slices.IndexFunc(all, func(n paths.SeatName) bool {
			h, ok := proto.ParseHandle(n.String())
			return n.String() == m || ok && h.Seat == m
		})
		if i < 0 {
			return nil, fmt.Errorf("%w: member %s is no pool seat of team %s in this checkout", errUsage, strconv.Quote(m), a.team)
		}
		if !slices.Contains(out, all[i]) {
			out = append(out, all[i])
		}
	}
	return out, nil
}

// knownBy names a local source that still knows team as live, "" when none does; a source that
// cannot be read is an error, which refuses as a known team does (fail safe). The sources, read
// on every call, are the ones a later design for ad-hoc teams created at runtime reuses:
//
//   - the team's status record on this machine (paths.StatusFile, read by Cmd.Status): a team
//     installed here has one;
//   - every session's desired.json under the seat store's control/ (paths.Store.DesiredFile): a
//     team any session wants is known, whatever declared it. Nothing removes these files, so a
//     session once launched with the team lists it until it is relaunched without it; the
//     refusal names the file, for the owner to judge and override. A control/<session> with no
//     desired.json lists nothing: a launch that cannot write it fails, and holds no seat.
func (c *Cmd) knownBy(store paths.Store, team paths.TeamID) (string, error) {
	_, installed, err := c.Status(team)
	if err != nil {
		return "", fmt.Errorf("the status record of team %s (%s): %w", team, paths.StatusFile(team), err)
	}
	if installed {
		return "team " + team.String() + " has a status record at " + paths.StatusFile(team), nil
	}
	control := store.Root() + "/control"
	entries, err := os.ReadDir(control)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	d := Desired{Store: store}
	for _, e := range entries {
		session, err := paths.ParseSessionName(e.Name())
		if err != nil {
			return "", fmt.Errorf("%s/%s: %w", control, e.Name(), err)
		}
		file := store.DesiredFile(session)
		info, err := os.Lstat(file)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("%s is not a regular file", file)
		}
		got, _, err := d.read(session)
		if err != nil {
			return "", fmt.Errorf("%s: %w", file, err)
		}
		if slices.ContainsFunc(got.Seats, func(s state.DesiredSeat) bool { return s.Team == team }) {
			return "team " + team.String() + " is listed in " + file, nil
		}
	}
	return "", nil
}

// refusal is the known-team refusal: the source that knows it, or the one that could not be
// read, and the override.
func refusal(team paths.TeamID, why string, err error) error {
	if err != nil {
		why = "a source that says whether team " + team.String() + " is still known could not be read: " + err.Error()
	}
	return fmt.Errorf("refusing to abandon the seats of team %s: %s. If its server will never answer again, repeat with %s %s", team, why, flagAllowKnow, team)
}

// fail says err for `seats abandon` and answers code.
func (c *Cmd) fail(err error, code term.ExitCode) term.ExitCode {
	if werr := c.sayAbandon(err); werr != nil {
		return term.ExitFailure
	}
	return code
}

func (c *Cmd) sayAbandon(err error) error {
	_, werr := fmt.Fprintf(c.Stderr, "ccy-teams: seats abandon: %v.\n", err)
	return werr
}

// CurrentUser is the caller `seats abandon` records: the user name, or "uid N" when the
// account database has no name for the uid.
func CurrentUser() (string, error) {
	return callerName(user.Current, os.Getuid())
}

// callerName is CurrentUser over the account lookup and the process's uid.
func callerName(lookup func() (*user.User, error), uid int) (string, error) {
	u, err := lookup()
	var unknown user.UnknownUserIdError
	if errors.As(err, &unknown) {
		return "uid " + strconv.Itoa(uid), nil
	}
	if err != nil {
		return "", err
	}
	if u.Username == "" {
		return "uid " + u.Uid, nil
	}
	return u.Username, nil
}
