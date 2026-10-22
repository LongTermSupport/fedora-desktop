package client

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strconv"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/bundle"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/notice"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// seatBound is how long seat disconnected may talk to the homeserver: the host's pass runs it
// once a minute, so a hung call must end before the next pass.
const seatBound = 30 * time.Second

const (
	msgSeed        exits.Template = "seat disconnected --seed is a run id: 16 lowercase hex digits"
	msgSince       exits.Template = "seat disconnected --since is a time in milliseconds"
	msgBundleDir   exits.Template = "the directory above {} is missing, a link, not this user's or writable by others"
	msgNoPair      exits.Template = "{} holds no seat.json and token pair"
	msgTornPair    exits.Template = "the bundle in {} changed while it was read; the next pass reads it again"
	msgUserID      exits.Template = "seat.json in {} names a user that is not on its homeserver"
	msgHomeserver  exits.Template = "seat.json in {} names a homeserver the member client refuses"
	msgUnreachable exits.Template = "the homeserver of team {} did not answer; the next pass tries again"
)

// hsDeps are what a verb that talks to the homeserver itself waits on: clock bounds the call,
// wait is the matrix client's retry wait (nil is its own), and armed, a test's seam, runs once
// the bound is armed.
type hsDeps struct {
	clock clock.Clock
	wait  matrix.Clock
	armed func()
}

func systemDeps() hsDeps { return hsDeps{clock: clock.System()} }

// SeatDisconnected is `seat disconnected --bundle DIR --since MS --seed ID` (EN5): the host's
// pass posts the disconnected notice of the run ID whose seat it knew held since MS, from the
// host-held bundle in DIR and nothing else. It exits 0 when the notice is posted or already
// there, and 0 with one stderr line when it cannot be posted for good (n3c-D17), so the pass
// never retries it; a transient failure is 20, which the pass retries.
func SeatDisconnected(e cli.Env, inv cli.Invocation) term.ExitCode {
	return seatDisconnected(e, inv, systemDeps())
}

// seatDisconnected holds stderr until the notice is settled, so a terminal gone while the call
// ran cannot change the outcome; a failed write keeps the exit, as exits.Write does.
func seatDisconnected(e cli.Env, inv cli.Invocation, d hsDeps) term.ExitCode {
	var held bytes.Buffer
	code := disconnect(&held, inv, d)
	if held.Len() == 0 {
		return code
	}
	if _, err := e.Stderr.Write(held.Bytes()); err != nil {
		return code
	}
	return code
}

func disconnect(stderr *bytes.Buffer, inv cli.Invocation, d hsDeps) term.ExitCode {
	n, client, err := seatNotice(inv, d.wait)
	if err != nil {
		return exits.Write(stderr, err)
	}
	defer client.Close()
	ctx, stop := bounded(d, seatBound)
	defer stop()
	out, err := notice.Disconnected(ctx, client, n)
	if err != nil {
		team, parseErr := paths.ParseTeamID(n.Team)
		if parseErr != nil {
			return exits.Write(stderr, parseErr)
		}
		return exits.Write(stderr, exits.New(exits.CodeUnreachable, &team, msgUnreachable, exits.Team(team)))
	}
	if out.Why != "" {
		stderr.WriteString("teamchat: seat disconnected: no notice for run " + n.Run.String() + ": " + out.Why + "\n")
	}
	return term.ExitOK
}

// flagValue is the flag's value; the verb's grammar requires every flag read here.
func flagValue(inv cli.Invocation, name string) string {
	v, given := inv.Flag(name)
	if !given {
		return ""
	}
	return v
}

// seatNotice is the notice the flags and the bundle in --bundle describe, and the subject's
// client to post it with.
func seatNotice(inv cli.Invocation, wait matrix.Clock) (notice.Notice, *matrix.Client, error) {
	dir := flagValue(inv, "bundle")
	since, err := strconv.ParseInt(flagValue(inv, "since"), 10, 64)
	if err != nil {
		return notice.Notice{}, nil, exits.New(exits.CodeUsage, nil, msgSince)
	}
	seed, err := paths.ParseRunID(flagValue(inv, "seed"))
	if err != nil {
		return notice.Notice{}, nil, exits.New(exits.CodeUsage, nil, msgSeed)
	}
	pair, err := readHeld(dir)
	if err != nil {
		return notice.Notice{}, nil, err
	}
	b := pair.Bundle.Bundle
	handle, ok := proto.Localpart(b.UserID, b.ServerName)
	if !ok {
		return notice.Notice{}, nil, exits.New(exits.CodeBundle, &pair.Team, msgUserID, exits.Name(dir))
	}
	token := pair.Token.Reveal()
	client, err := matrix.NewMember(matrix.Config{BaseURL: b.BaseURL, PlainHTTPHosts: b.PlainHTTPHosts, Token: token, Clock: wait})
	if err != nil {
		return notice.Notice{}, nil, exits.New(exits.CodeBundle, &pair.Team, msgHomeserver, exits.Name(dir))
	}
	return notice.Notice{
		Server: b.ServerName, Team: pair.Team.String(), MainRoom: b.MainRoom,
		Handle: handle, Token: token, Run: seed, SinceMS: since,
	}, client, nil
}

// readHeld reads the pair in dir, opened from the directory above it, which the host keeps its
// own; seat.json's team is the pair's.
func readHeld(dir string) (bundle.Pair, error) {
	root, err := homefs.Open(filepath.Dir(dir), homefs.Owned)
	if errors.Is(err, homefs.ErrConfig) || errors.Is(err, fs.ErrNotExist) {
		return bundle.Pair{}, exits.New(exits.CodeBundle, nil, msgBundleDir, exits.Name(dir))
	}
	if err != nil {
		return bundle.Pair{}, err
	}
	pair, err := bundle.ReadPairAt(root, dir, nil)
	if closeErr := root.Close(); closeErr != nil && err == nil {
		return bundle.Pair{}, closeErr
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return bundle.Pair{}, exits.New(exits.CodeBundle, nil, msgNoPair, exits.Name(dir))
	case errors.Is(err, bundle.ErrTorn):
		return bundle.Pair{}, exits.New(exits.CodeUnreachable, nil, msgTornPair, exits.Name(dir))
	}
	return pair, err
}

// bounded is a context cancelled once limit has passed on d's clock.
func bounded(d hsDeps, limit time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	stop := d.clock.AfterFunc(limit, cancel)
	if d.armed != nil {
		d.armed()
	}
	return ctx, func() {
		stop()
		cancel()
	}
}
