// Package hostnet is the installer's view of the host's network tools: ip, ss and nmcli for
// addresses, routes and listeners, and curl for the release download and the readiness poll.
// It runs the programs and returns their text; netprobe parses it.
package hostnet

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/netprobe"
)

// The subcommands the ip and nmcli calls share.
const (
	nmcliBin      = "nmcli"
	subShow       = "show"
	subConnection = "connection"
)

// Net runs the network tools through a Runner. Chatter receives what nmcli prints when it
// changes something (nil keeps it for the error text).
type Net struct {
	Runner  execx.Runner
	Chatter io.Writer
}

// AddrJSON is `ip -j addr show`.
func (n *Net) AddrJSON(ctx context.Context) ([]byte, error) {
	out, err := n.output(ctx, execx.Cmd{Name: "ip", Args: []string{"-j", "addr", subShow}})
	return []byte(out), err
}

// RouteDev is the device the kernel routes addr through; netprobe.ErrNoRoute when the
// answer names none.
func (n *Net) RouteDev(ctx context.Context, addr string) (string, error) {
	out, err := n.output(ctx, execx.Cmd{Name: "ip", Args: []string{"-j", "route", "get", addr}})
	if err != nil {
		return "", err
	}
	return netprobe.RouteDev([]byte(out))
}

// Listening is `ss -ltnH` for the TCP listeners on one port.
func (n *Net) Listening(ctx context.Context, port int) (string, error) {
	return n.output(ctx, execx.Cmd{Name: "ss", Args: []string{"-ltnH", "sport = :" + strconv.Itoa(port)}})
}

// ConnectionExists is whether NetworkManager has a connection of that name.
func (n *Net) ConnectionExists(ctx context.Context, name string) (bool, error) {
	out, err := n.output(ctx, execx.Cmd{Name: nmcliBin, Args: []string{"-t", "-f", "NAME", subConnection, subShow}})
	if err != nil {
		return false, err
	}
	return netprobe.HasLine(out, name), nil
}

// AddDummy adds an autoconnecting dummy-interface connection with the given settings.
func (n *Net) AddDummy(ctx context.Context, name string, settings []string) error {
	args := make([]string, 0, 10+len(settings))
	args = append(args, subConnection, "add", "type", "dummy", "ifname", name, "con-name", name, "connection.autoconnect", "yes")
	return n.change(ctx, append(args, settings...)...)
}

// Modify applies the settings to an existing connection.
func (n *Net) Modify(ctx context.Context, name string, settings []string) error {
	return n.change(ctx, append([]string{subConnection, "modify", name}, settings...)...)
}

// Up activates the connection.
func (n *Net) Up(ctx context.Context, name string) error {
	return n.change(ctx, subConnection, "up", name)
}

// Addresses is nmcli's terse listing of the connection's IPv4 and IPv6 addresses.
func (n *Net) Addresses(ctx context.Context, name string) (string, error) {
	return n.output(ctx, execx.Cmd{Name: nmcliBin, Args: []string{"-g", "ipv4.addresses,ipv6.addresses", subConnection, subShow, name}})
}

// State is the connection's GENERAL.STATE ("activated" when it is up).
func (n *Net) State(ctx context.Context, name string) (string, error) {
	out, err := n.output(ctx, execx.Cmd{Name: nmcliBin, Args: []string{"-g", "GENERAL.STATE", subConnection, subShow, name}})
	return strings.TrimSpace(out), err
}

// Download fetches an https URL into the file out, following redirects, within ten minutes.
func (n *Net) Download(ctx context.Context, url, out string) error {
	_, err := n.Runner.Run(ctx, execx.Cmd{Name: "curl", Args: []string{
		"-q", "--fail", "--silent", "--show-error", "--location", "--proto", "=https", "--tlsv1.2",
		"--max-time", "600", "-o", out, url,
	}})
	if err != nil {
		return fmt.Errorf("curl %s: %w", url, err)
	}
	return nil
}

// Probe asks a local URL once, within two seconds and past any proxy. The output of both
// streams comes back with the error, as it explains a failed poll.
func (n *Net) Probe(ctx context.Context, url string) (string, error) {
	out, err := execx.Combined(ctx, n.Runner, execx.Cmd{Name: "curl", Args: []string{
		"-q", "--fail", "--silent", "--show-error", "--noproxy", "*", "--max-time", "2", url,
	}})
	if err != nil {
		return out, fmt.Errorf("curl %s: %w", url, err)
	}
	return out, nil
}

func (n *Net) output(ctx context.Context, cmd execx.Cmd) (string, error) {
	res, err := n.Runner.Run(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", cmd.Name, strings.Join(cmd.Args, " "), err)
	}
	return string(res.Stdout), nil
}

func (n *Net) change(ctx context.Context, args ...string) error {
	_, err := n.Runner.Run(ctx, execx.Cmd{Name: nmcliBin, Args: args, Stdout: n.Chatter, Stderr: n.Chatter})
	if err != nil {
		return fmt.Errorf("nmcli %s: %w", strings.Join(args, " "), err)
	}
	return nil
}
