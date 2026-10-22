package hostnet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/health"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// The probes' bounds (RD1, RD2) and the cap of a team.json read.
const (
	probeTimeout = 5 * time.Second
	bodyCap      = 64 << 10
	teamFileCap  = 64 << 10
	maxPort      = 65535
)

// The two paths probed.
const (
	serverVersionPath  = "/_tuwunel/server_version"
	clientVersionsPath = "/_matrix/client/versions"
)

var (
	errStatus = errors.New("did not answer 200")
	errPort   = errors.New("is not a port")
)

// Readiness implements host.Readiness: the homeserver answers /_tuwunel/server_version (RD1).
type Readiness struct{}

var _ host.Readiness = Readiness{}

// ServerVersion is nil when the homeserver on loopback answers 200.
func (Readiness) ServerVersion(ctx context.Context, port int) error {
	return get(ctx, port, serverVersionPath)
}

// Prober implements health.Prober: the homeserver answers /_matrix/client/versions on the
// team's declared port (RD2).
type Prober struct {
	Port func(paths.TeamID) (int, error)
}

var _ health.Prober = Prober{}

// Probe is nil when the team's homeserver on loopback answers 200.
func (p Prober) Probe(ctx context.Context, team paths.TeamID) error {
	port, err := p.Port(team)
	if err != nil {
		return err
	}
	return get(ctx, port, clientVersionsPath)
}

// newClient is the probes' client: no proxy, no redirect followed, five seconds.
func newClient() *http.Client {
	return &http.Client{
		Timeout:       probeTimeout,
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// get is a GET of http://127.0.0.1:<port><path> that answers 200; at most bodyCap of the body
// is read.
func get(ctx context.Context, port int, path string) (err error) {
	if port < 1 || port > maxPort {
		return fmt.Errorf("hostnet: %d %w", port, errPort)
	}
	url := "http://127.0.0.1:" + strconv.Itoa(port) + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("hostnet: %w", err)
	}
	client := newClient()
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("hostnet: %w", err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()
	if _, err = io.Copy(io.Discard, io.LimitReader(resp.Body, bodyCap)); err != nil {
		return fmt.Errorf("hostnet: reading %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hostnet: %s %w: %s", url, errStatus, resp.Status)
	}
	return nil
}

// TeamPort is the port a team's applied `team.json` under root declares.
func TeamPort(root string) func(paths.TeamID) (int, error) {
	return func(team paths.TeamID) (int, error) {
		path := root + paths.TeamDeclaration(team)
		raw, found, err := state.ReadFile(path, teamFileCap)
		if err != nil {
			return 0, err
		}
		if !found {
			return 0, fmt.Errorf("hostnet: %s does not exist", path)
		}
		declared, err := declare.DecodeTeamFile(raw)
		if err != nil {
			return 0, fmt.Errorf("hostnet: %s: %w", path, err)
		}
		if declared.ID != team {
			return 0, fmt.Errorf("hostnet: %s declares team %s, not %s", path, declared.ID, team)
		}
		if declared.Port < 1 || declared.Port > maxPort {
			return 0, fmt.Errorf("hostnet: %s: %d %w", path, declared.Port, errPort)
		}
		return declared.Port, nil
	}
}
