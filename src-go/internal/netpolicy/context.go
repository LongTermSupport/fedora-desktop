package netpolicy

import (
	"fmt"
	"net/netip"
	"strings"
)

// Marker heads the context block every session is given (N8); the acceptance gate looks for it.
const Marker = "CCY NETWORK POLICY"

// ContextPath is where the same text is mounted read-only inside the session.
const ContextPath = "/run/ccy/network-policy"

// State is whether a session's network is limited.
type State string

// The three states a session can start in (N7).
const (
	Enforced    State = "enforced"
	Off         State = "off"
	Unavailable State = "unavailable"
)

// Network is a project network a session joined, and its subnets.
type Network struct {
	Name    string         `json:"name"`
	Subnets []netip.Prefix `json:"subnets"`
}

// Context is what a session is told about its network.
type Context struct {
	State    State        `json:"state"`
	Reason   string       `json:"reason"` // why enforcement is unavailable
	Entries  []string     `json:"entries"`
	HostFile string       `json:"hostFile"`
	Bus      []netip.Addr `json:"bus"`
	Networks []Network    `json:"networks"`
	LXC      int          `json:"lxc"` // how many addresses the lxc keyword opened
}

// Text is the block for the system prompt and ContextPath.
func (c Context) Text() string {
	var b strings.Builder
	b.WriteString(Marker + "\n\n")
	widen := fmt.Sprintf("The owner widens or changes it on the host, in %s (lines of %s=<entries>: addresses, prefixes, host names, any with :port, `lxc` for the LXC dev sites, or `off`), then relaunches this session; `ccy --connect NETWORK` on the host opens a project network while the session runs.\n", c.HostFile, AllowVar)
	switch c.State {
	case Off:
		b.WriteString("The network policy is off for this checkout (" + AllowVar + "=off): this session reaches everything the host can, the LAN and the host itself included. Be careful with what is on them.\n")
		b.WriteString(widen)
		return b.String()
	case Unavailable:
		b.WriteString("This session runs with NO network policy: " + c.Reason + ". It reaches everything the host can, the LAN and the host itself included.\n")
		return b.String()
	case Enforced:
	}
	ranges := make([]string, 0, 7)
	for _, p := range DefaultBlock() {
		ranges = append(ranges, p.String())
	}
	b.WriteString("This session's network is limited by a policy the host holds outside this container; nothing inside can change it.\n\n")
	b.WriteString("Refused: the private and LAN ranges (" + strings.Join(ranges, ", ") + "), this host's own addresses, host.containers.internal, and other ccy sessions on the shared podman network.\n\n")
	b.WriteString("Open: the public internet; DNS through this session's own nameservers")
	for _, a := range c.Bus {
		b.WriteString("; the agent bus at " + a.String() + " (every port)")
	}
	for _, n := range c.Networks {
		subnets := make([]string, 0, len(n.Subnets))
		for _, s := range n.Subnets {
			subnets = append(subnets, s.String())
		}
		b.WriteString("; the project network " + n.Name + " (" + strings.Join(subnets, ", ") + ")")
	}
	b.WriteString(".\n")
	entries := "none"
	if len(c.Entries) > 0 {
		entries = strings.ReplaceAll(strings.Join(c.Entries, ", "), "lxc", fmt.Sprintf("lxc (%d LXC dev-site addresses)", c.LXC))
	}
	b.WriteString("The owner's allow-list: " + entries + ".\n\n")
	b.WriteString("A refused connection fails at once: nc and ssh say \"No route to host\", curl exit 7 \"Couldn't connect to server\". To an address in a refused range that is this policy, not a service that is down: do not retry it or look for a way around it; say what you needed and why, so the owner can decide.\n")
	b.WriteString(widen)
	b.WriteString("This text is also at " + ContextPath + ", to read again later.\n")
	return b.String()
}
