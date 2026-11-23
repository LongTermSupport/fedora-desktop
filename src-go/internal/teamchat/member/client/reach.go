package client

import (
	"errors"
	"io/fs"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	// reachBound is how long reach waits for the versions answer (n3c-D16).
	reachBound = 3 * time.Second
	// reachToken stands in for the token the member client needs: versions is asked without
	// one, so reach reads no token.
	reachToken = "unused"
)

const (
	msgNoHome     exits.Template = "HOME and XDG_RUNTIME_DIR do not name a member's home; tell a human"
	msgStateRoot  exits.Template = "the seat directory root {} is missing, a link, not this user's or not mode 0700"
	msgSeatJSON   exits.Template = "{} is missing, a link, not this user's or over its cap"
	msgSeatShape  exits.Template = "{} is not a guest's or a session's bundle"
	msgSeatTeam   exits.Template = "{} is for team {}"
	msgReachURL   exits.Template = "the bundle of team {} names a homeserver the member client refuses"
	msgReachTeams exits.Template = "the homeserver of team {} did not answer its versions"
)

// Reach is `reach --team T` (n3c-D16): an unauthenticated GET of versions from T's homeserver,
// by T's seat.json alone, so a route is proven from the member side. It prints nothing and
// exits 0, 20 unreachable, or 78 bundle.
func Reach(e cli.Env, inv cli.Invocation) term.ExitCode {
	return reach(e, inv, systemDeps())
}

func reach(e cli.Env, inv cli.Invocation, d hsDeps) term.ExitCode {
	team := *inv.Team
	b, err := seatBundle(e.Getenv, team)
	if err != nil {
		return exits.Write(e.Stderr, err)
	}
	cfg := b.MemberConfig(reachToken)
	cfg.Clock = d.wait
	client, err := matrix.NewMember(cfg)
	if err != nil {
		return exits.Write(e.Stderr, exits.New(exits.CodeBundle, &team, msgReachURL, exits.Team(team)))
	}
	defer client.Close()
	ctx, stop := bounded(d, reachBound)
	defer stop()
	if _, err := client.Versions(ctx); err != nil {
		return exits.Write(e.Stderr, exits.New(exits.CodeUnreachable, &team, msgReachTeams, exits.Team(team)))
	}
	return term.ExitOK
}

// stateLayout is TEAMCHAT_RUNTIME's layout when it is set, with the state root its keeper
// recorded (run --state) in place of the layout's own; else the home member's.
func stateLayout(getenv func(string) string) (paths.Layout, error) {
	if dir := getenv(paths.EnvRuntime); dir != "" {
		r, l, err := sessionOf(dir, getenv)
		if err != nil {
			return l, err
		}
		return recordedState(r, l)
	}
	l, err := paths.HomeLayout(getenv("HOME"), getenv("XDG_RUNTIME_DIR"))
	if err != nil {
		return paths.Layout{}, exits.New(exits.CodeRuntime, nil, msgNoHome)
	}
	return l, nil
}

// maxStateRoot bounds the run's state-root record: one path and a newline.
const maxStateRoot = 4 << 10

// recordedState is l with the state root r's keeper recorded, or l unchanged when it recorded
// none. A record that is not one absolute, clean path is refused as the state root (78); the
// root's owner and mode are checked where it is opened.
func recordedState(r paths.Run, l paths.Layout) (paths.Layout, error) {
	root, err := homefs.Open(r.Dir(), homefs.Private)
	if err != nil {
		return paths.Layout{}, exits.New(exits.CodeNoSession, nil, msgNoSession)
	}
	f, err := root.Read(r.StateRoot(), maxStateRoot)
	if closeErr := root.Close(); closeErr != nil && err == nil {
		return paths.Layout{}, closeErr
	}
	if errors.Is(err, fs.ErrNotExist) {
		return l, nil
	}
	bad := exits.New(exits.CodeBundle, nil, msgStateRoot, exits.Name(r.StateRoot()))
	if err != nil {
		return paths.Layout{}, bad
	}
	dir, ok := strings.CutSuffix(string(f.Data), "\n")
	if !ok || strings.Contains(dir, "\n") {
		return paths.Layout{}, bad
	}
	moved, err := l.WithState(dir)
	if err != nil {
		return paths.Layout{}, bad
	}
	return moved, nil
}

// seatBundle is team's seat.json, read alone below the state root; its team must be team.
func seatBundle(getenv func(string) string, team paths.TeamID) (seat.Bundle, error) {
	l, err := stateLayout(getenv)
	if err != nil {
		return seat.Bundle{}, err
	}
	root, err := homefs.Open(l.State(), homefs.Private)
	if errors.Is(err, homefs.ErrConfig) || errors.Is(err, fs.ErrNotExist) {
		return seat.Bundle{}, exits.New(exits.CodeBundle, &team, msgStateRoot, exits.Name(l.State()))
	}
	if err != nil {
		return seat.Bundle{}, err
	}
	full := l.Seat(team).Bundle()
	f, err := root.Read(full, seat.MaxBundle)
	if closeErr := root.Close(); closeErr != nil && err == nil {
		return seat.Bundle{}, closeErr
	}
	if errors.Is(err, homefs.ErrConfig) || errors.Is(err, fs.ErrNotExist) {
		return seat.Bundle{}, exits.New(exits.CodeBundle, &team, msgSeatJSON, exits.Name(full))
	}
	if err != nil {
		return seat.Bundle{}, err
	}
	mb, err := seat.DecodeMemberBundle(f.Data)
	if err != nil {
		return seat.Bundle{}, exits.New(exits.CodeBundle, &team, msgSeatShape, exits.Name(full))
	}
	if mb.Bundle.Team != team {
		return seat.Bundle{}, exits.New(exits.CodeBundle, &team, msgSeatTeam, exits.Name(full), exits.Team(mb.Bundle.Team))
	}
	return mb.Bundle, nil
}
