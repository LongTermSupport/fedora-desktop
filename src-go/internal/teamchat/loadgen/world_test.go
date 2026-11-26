package loadgen

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
)

// labName is the test team's server name.
const labName = "loadgen.test"

// startMS is the step clock's start, 2026-11-25T13:00:00Z.
const startMS = 1_795_611_600_000

// stepClock is a clock whose sleep moves it on at once, so a run of hours takes none: the server
// stamps its events by it.
type stepClock struct {
	mu    sync.Mutex
	ms    int64
	slept time.Duration
	// onSleep, when set, runs at the start of each sleep (an interrupt arriving while it waits).
	onSleep func()
}

func newStepClock() *stepClock { return &stepClock{ms: startMS} }

func (c *stepClock) WallMS() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ms
}

func (c *stepClock) Sleep(ctx context.Context, d time.Duration) error {
	if c.onSleep != nil {
		c.onSleep()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ms += d.Milliseconds()
	c.slept += d
	return nil
}

func (c *stepClock) now() time.Time { return time.UnixMilli(c.WallMS()) }

// lab is a fakehs named name on c's time, over HTTP on loopback, stopped when the test ends.
func lab(t *testing.T, name string, c *stepClock) *fakehs.Server {
	t.Helper()
	srv := fakehs.New(fakehs.Options{ServerName: name, Now: c.now, Quirks: fakehs.TuwunelQuirks()})
	if err := srv.Start(); err != nil {
		t.Fatalf("fakehs: %v", err)
	}
	t.Cleanup(func() {
		if err := srv.CheckModelled(); err != nil {
			t.Errorf("fakehs: %v", err)
		}
		if err := srv.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return srv
}

// labTeam is a team of agents set up on a fresh lab server on c.
func labTeam(t *testing.T, c *stepClock, agents int) Team {
	t.Helper()
	return setUp(t, lab(t, labName, c), c, agents)
}

// setUp is a team of agents set up on srv.
func setUp(t *testing.T, srv *fakehs.Server, c *stepClock, agents int) Team {
	t.Helper()
	team, err := Setup(t.Context(), SetupOptions{Base: srv.URL(), ServerName: labName, RegKey: srv.RegistrationKey(), Agents: agents, Clock: c})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	return team
}

// member is a client of the account with token on the team's server.
func member(t *testing.T, team Team, token string) *matrix.Client {
	t.Helper()
	cl, err := matrix.NewMember(matrix.Config{BaseURL: team.Homeserver, Token: token, Clock: newStepClock()})
	if err != nil {
		t.Fatalf("NewMember: %v", err)
	}
	t.Cleanup(cl.Close)
	return cl
}
