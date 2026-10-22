package hostnet

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/bits"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// The kernel route flags that decide whether a route is used, and firewalld's exit code for an
// interface that is in no zone (SPEC-n2f n2f-D5).
const (
	routeUp       = 0x1
	routeReject   = 0x200
	exitNoZone    = 2
	exitAbsent    = 1
	firewallCmd   = "firewall-cmd"
	maxIPv6Length = 128
)

var zoneName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

var (
	errZoneName   = errors.New("is not a zone name")
	errRouteLine  = errors.New("not a route table line")
	errFirewallRC = errors.New("firewall-cmd refused")
)

// Firewall implements host.Firewall over firewall-cmd (FW1, FW2). Runner is the one place the
// program runs; Proc is /proc, whose route tables say which interface routes a source.
type Firewall struct {
	Runner execx.Runner
	Proc   string
}

var _ host.Firewall = Firewall{}

// Zone is the zone of the interface that routes source, or the default zone when no route
// covers it or the interface is in no zone (FW1).
func (f Firewall) Zone(ctx context.Context, source netip.Prefix) (string, error) {
	dev, found, err := f.route(source.Addr().Unmap())
	if err != nil {
		return "", err
	}
	if found {
		res, code, runErr := f.run(ctx, "--get-zone-of-interface="+dev)
		if runErr != nil {
			return "", runErr
		}
		if code != exitNoZone {
			return zoneFrom(res, code, "--get-zone-of-interface")
		}
	}
	res, code, err := f.run(ctx, "--get-default-zone")
	if err != nil {
		return "", err
	}
	return zoneFrom(res, code, "--get-default-zone")
}

// zoneFrom is the one-word zone a successful query printed.
func zoneFrom(res execx.Result, code int, flag string) (string, error) {
	if code != 0 {
		return "", fmt.Errorf("%w: %s %s exited %d: %s", errFirewallRC, firewallCmd, flag, code, strings.TrimSpace(string(res.Stderr)))
	}
	zone := strings.TrimSpace(string(res.Stdout))
	if !zoneName.MatchString(zone) {
		return "", fmt.Errorf("%s %s answered %q, which %w", firewallCmd, flag, zone, errZoneName)
	}
	return zone, nil
}

// Query is whether the rule is in the runtime or the permanent configuration: exit 0 is
// present, 1 absent, anything else an error naming the code (FW2).
func (f Firewall) Query(ctx context.Context, permanent bool, r host.Rule) (bool, error) {
	args, err := ruleArgs(permanent, r, "--query-rich-rule=")
	if err != nil {
		return false, err
	}
	res, code, err := f.run(ctx, args...)
	switch {
	case err != nil:
		return false, err
	case code == 0:
		return true, nil
	case code == exitAbsent:
		return false, nil
	}
	return false, fmt.Errorf("%w: %s --query-rich-rule of %s exited %d: %s", errFirewallRC, firewallCmd, r, code, strings.TrimSpace(string(res.Stderr)))
}

// Add adds the rule to the runtime or the permanent configuration.
func (f Firewall) Add(ctx context.Context, permanent bool, r host.Rule) error {
	return f.change(ctx, permanent, r, "--add-rich-rule=")
}

// Remove removes the rule from the runtime or the permanent configuration.
func (f Firewall) Remove(ctx context.Context, permanent bool, r host.Rule) error {
	return f.change(ctx, permanent, r, "--remove-rich-rule=")
}

func (f Firewall) change(ctx context.Context, permanent bool, r host.Rule, flag string) error {
	args, err := ruleArgs(permanent, r, flag)
	if err != nil {
		return err
	}
	if _, err = f.Runner.Run(ctx, execx.Cmd{Name: firewallCmd, Args: args}); err != nil {
		return fmt.Errorf("hostnet: %s %s of %s: %w", firewallCmd, flag, r, err)
	}
	return nil
}

// run runs firewall-cmd; a command that ran and exited non-zero is its code, not an error, and
// one that could not start, or was killed, is.
func (f Firewall) run(ctx context.Context, args ...string) (execx.Result, int, error) {
	res, err := f.Runner.Run(ctx, execx.Cmd{Name: firewallCmd, Args: args})
	var exit *execx.ExitError
	if errors.As(err, &exit) && exit.Signal == 0 {
		return res, exit.Code, nil
	}
	if err != nil {
		return res, 0, fmt.Errorf("hostnet: %w", err)
	}
	return res, 0, nil
}

// ruleArgs is `[--permanent] --zone=<z> <flag><rule>`, the rule one argument.
func ruleArgs(permanent bool, r host.Rule, flag string) ([]string, error) {
	if !zoneName.MatchString(r.Zone) {
		return nil, fmt.Errorf("hostnet: the zone %q %w", r.Zone, errZoneName)
	}
	args := make([]string, 0, 3)
	if permanent {
		args = append(args, "--permanent")
	}
	return append(args, "--zone="+r.Zone, flag+r.Rich()), nil
}

// route is one usable kernel route.
type route struct {
	dev    string
	prefix netip.Prefix
	metric uint64
}

// route is the interface of the best route covering addr: the longest prefix, then the lowest
// metric; the first of equals wins.
func (f Firewall) route(addr netip.Addr) (string, bool, error) {
	var routes []route
	var err error
	if addr.Is4() {
		routes, err = readRoutes(f.Proc+"/net/route", true, parseRoute4)
	} else {
		routes, err = readRoutes(f.Proc+"/net/ipv6_route", false, parseRoute6)
	}
	if err != nil {
		return "", false, err
	}
	var best *route
	for i := range routes {
		r := &routes[i]
		if !r.prefix.Contains(addr) {
			continue
		}
		if best == nil || r.prefix.Bits() > best.prefix.Bits() ||
			(r.prefix.Bits() == best.prefix.Bits() && r.metric < best.metric) {
			best = r
		}
	}
	if best == nil {
		return "", false, nil
	}
	return best.dev, true, nil
}

// readRoutes is a route table's usable routes; a host without IPv6 has no ipv6_route, which is
// no routes. A line that does not parse fails the read.
func readRoutes(path string, required bool, parse func(string) (route, bool, error)) ([]route, error) {
	raw, found, err := state.ReadFile(path, tableCap)
	if err != nil {
		return nil, err
	}
	if !found {
		if required {
			return nil, fmt.Errorf("hostnet: %s does not exist", path)
		}
		return nil, nil
	}
	var out []route
	for i, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" || (required && i == 0) {
			continue // blank, or the IPv4 table's header
		}
		r, usable, parseErr := parse(line)
		if parseErr != nil {
			return nil, fmt.Errorf("hostnet: %s line %d: %w", path, i+1, parseErr)
		}
		if usable {
			out = append(out, r)
		}
	}
	return out, nil
}

// parseRoute4 reads a /proc/net/route line: Iface Destination Gateway Flags RefCnt Use Metric
// Mask, the addresses as little-endian hex words.
func parseRoute4(line string) (route, bool, error) {
	const minColumns = 8
	f := strings.Fields(line)
	if len(f) < minColumns {
		return route{}, false, fmt.Errorf("%w: %d columns", errRouteLine, len(f))
	}
	dest, err := strconv.ParseUint(f[1], 16, 32)
	if err != nil {
		return route{}, false, fmt.Errorf("the destination %q: %w", f[1], err)
	}
	flags, err := strconv.ParseUint(f[3], 16, 32)
	if err != nil {
		return route{}, false, fmt.Errorf("the flags %q: %w", f[3], err)
	}
	metric, err := strconv.ParseUint(f[6], 10, 32)
	if err != nil {
		return route{}, false, fmt.Errorf("the metric %q: %w", f[6], err)
	}
	mask, err := strconv.ParseUint(f[7], 16, 32)
	if err != nil {
		return route{}, false, fmt.Errorf("the mask %q: %w", f[7], err)
	}
	network := bits.ReverseBytes32(uint32(mask))
	ones := bits.OnesCount32(network)
	if bits.LeadingZeros32(^network) != ones {
		return route{}, false, fmt.Errorf("%w: the mask %q is not a prefix", errRouteLine, f[7])
	}
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(dest))
	return route{dev: f[0], prefix: netip.PrefixFrom(netip.AddrFrom4(b), ones).Masked(), metric: metric}, usableRoute(flags), nil
}

// parseRoute6 reads a /proc/net/ipv6_route line: destination, its length, source, its length,
// next hop, metric, refcount, use, flags, device; all numbers hex.
func parseRoute6(line string) (route, bool, error) {
	const minColumns = 10
	f := strings.Fields(line)
	if len(f) < minColumns {
		return route{}, false, fmt.Errorf("%w: %d columns", errRouteLine, len(f))
	}
	raw, err := hex.DecodeString(f[0])
	if err != nil || len(raw) != 16 {
		return route{}, false, fmt.Errorf("%w: the destination %q is not 16 bytes of hex", errRouteLine, f[0])
	}
	length, err := strconv.ParseUint(f[1], 16, 8)
	if err != nil || length > maxIPv6Length {
		return route{}, false, fmt.Errorf("%w: the prefix length %q", errRouteLine, f[1])
	}
	metric, err := strconv.ParseUint(f[5], 16, 32)
	if err != nil {
		return route{}, false, fmt.Errorf("the metric %q: %w", f[5], err)
	}
	flags, err := strconv.ParseUint(f[8], 16, 32)
	if err != nil {
		return route{}, false, fmt.Errorf("the flags %q: %w", f[8], err)
	}
	addr := netip.AddrFrom16([16]byte(raw))
	return route{dev: f[9], prefix: netip.PrefixFrom(addr, int(length)).Masked(), metric: metric}, usableRoute(flags), nil
}

// usableRoute is a route that is up and does not reject.
func usableRoute(flags uint64) bool {
	return flags&routeUp != 0 && flags&routeReject == 0
}
