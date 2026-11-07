// Package tlsverb is `teamchat tls --team T --port P --listen A...`, the process of the unit
// teamchat-tls@T (Plan 00246 SPEC-cross-network X18): it reads the team's leaf certificate and
// its key from the unit's credentials, then runs the terminator (package tlsterm) on each listen
// address at the team's port, relaying to the homeserver on loopback at that port, until the
// unit is stopped. The unit's drop-in names the arguments; nothing secret is in argv.
package tlsverb

import (
	"context"
	"net/netip"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/tlsterm"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// credentialLimit caps a credential file read: a certificate or a key is a few kilobytes.
const credentialLimit = 64 << 10

const (
	flagPort   = "port"
	flagListen = "listen"
)

const (
	msgNoDirectory exits.Template = "the unit has no directory for its files: {} is not set; tell a human"
	msgNoFile      exits.Template = "the unit's file {} for team {} is missing or empty; tell a human"
	msgFlag        exits.Template = "--{} is not a value the terminator can serve; tell a human"
)

// loopbackHomeserver is the address the homeserver binds, and the terminator's upstream.
var loopbackHomeserver = netip.MustParseAddr("127.0.0.1")

// Main is `teamchat tls`.
func Main(e cli.Env, inv cli.Invocation) term.ExitCode {
	return Serve(e, inv, Deps{Run: tlsterm.Run, Clock: clock.System(), Notify: stopOnSignal})
}

// Deps are the verb's seams: the terminator, the clock its bounds are armed on, and the context
// that ends when the unit is stopped. Main gives the real ones; a test that runs the verb as the
// unit would (X29) gives its own.
type Deps struct {
	Run    func(context.Context, tlsterm.Config) error
	Clock  tlsterm.Clock
	Notify func() (context.Context, context.CancelFunc)
}

// stopOnSignal is a context that ends at the unit's stop (SIGTERM) or an interrupt.
func stopOnSignal() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
}

// Serve is the verb over d: the configuration read from inv and the unit's credentials, then the
// terminator until the context d gives ends.
func Serve(e cli.Env, inv cli.Invocation, d Deps) term.ExitCode {
	cfg, err := config(e, inv, d.Clock)
	if err != nil {
		return exits.Write(e.Stderr, err)
	}
	ctx, stop := d.Notify()
	defer stop()
	return exits.Write(e.Stderr, d.Run(ctx, cfg))
}

// config is what the terminator serves: the credentials, every listen address at the port, and
// the loopback homeserver at the port.
func config(e cli.Env, inv cli.Invocation, clk tlsterm.Clock) (tlsterm.Config, error) {
	team := *inv.Team
	dir := e.Getenv(paths.EnvCredentials)
	if dir == "" {
		return tlsterm.Config{}, exits.New(exits.CodeRuntime, &team, msgNoDirectory, exits.Name(paths.EnvCredentials))
	}
	cert, err := credential(dir, paths.LeafCertCredential, team)
	if err != nil {
		return tlsterm.Config{}, err
	}
	key, err := credential(dir, paths.LeafKeyCredential, team)
	if err != nil {
		return tlsterm.Config{}, err
	}
	port, err := portOf(inv, team)
	if err != nil {
		return tlsterm.Config{}, err
	}
	listen, err := listenOf(inv, team, port)
	if err != nil {
		return tlsterm.Config{}, err
	}
	return tlsterm.Config{
		Listen: listen, Upstream: netip.AddrPortFrom(loopbackHomeserver, port), CertPEM: cert, KeyPEM: key,
		Clock: clk, Log: e.Stderr,
	}, nil
}

// credential is one of the unit's credential files, which must hold something.
func credential(dir, name string, team paths.TeamID) ([]byte, error) {
	b, found, err := state.ReadFile(filepath.Join(dir, name), credentialLimit)
	if err != nil || !found || len(b) == 0 {
		return nil, exits.New(exits.CodeRuntime, &team, msgNoFile, exits.Name(name), exits.Team(team))
	}
	return b, nil
}

// portOf is the team's port, a flag the parser has already held to a port.
func portOf(inv cli.Invocation, team paths.TeamID) (uint16, error) {
	text, given := inv.Flag(flagPort)
	n, err := strconv.ParseUint(text, 10, 16)
	if !given || err != nil || n == 0 {
		return 0, exits.New(exits.CodeRuntime, &team, msgFlag, exits.Name(flagPort))
	}
	return uint16(n), nil
}

// listenOf is every --listen address at port.
func listenOf(inv cli.Invocation, team paths.TeamID, port uint16) ([]netip.AddrPort, error) {
	var out []netip.AddrPort
	for _, text := range inv.Values(flagListen) {
		addr, err := netip.ParseAddr(text)
		if err != nil {
			return nil, exits.New(exits.CodeRuntime, &team, msgFlag, exits.Name(flagListen))
		}
		out = append(out, netip.AddrPortFrom(addr, port))
	}
	return out, nil
}
