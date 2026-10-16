package declare

import (
	"context"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/marker"
)

const flagTeam = "--team"

// LxcEntry is the values of an agent_bus_lxc_members entry that lxc-add and lxc-check take;
// empty fields are not passed. It is one agent user of an existing LXC container joining a team.
type LxcEntry struct {
	Team      string
	Container string
	User      string
	Host      string // an override of the handle's host; otherwise read from the checkout, never asked for
	Checkout  string // an absolute path inside the container
	Role      string // worker (the default) or coordinator
	Seat      string // an override of the handle's seat; otherwise derived, never asked for
}

func (e LxcEntry) args() []string {
	var args []string
	for _, f := range []struct{ flag, value string }{
		{flagTeam, e.Team},
		{"--container", e.Container},
		{"--user", e.User},
		{"--host", e.Host},
		{"--checkout", e.Checkout},
		{"--role", e.Role},
		{"--seat", e.Seat},
	} {
		if f.value != "" {
			args = append(args, f.flag, f.value)
		}
	}
	return args
}

// LxcMember is an LXCMEMBER line of "declare lxc-members": an entry as the play reads it.
type LxcMember struct {
	Team      string
	Container string
	User      string
	Host      string
	Role      string // worker or coordinator (the legacy orchestrator is read as coordinator)
	State     string // present or absent
}

// LxcMembers is the entries of agent_bus_lxc_members, in file order.
func (c *Client) LxcMembers(ctx context.Context) ([]LxcMember, error) {
	out, err := c.runFile(ctx, "lxc-members")
	if err != nil {
		return nil, err
	}
	rows, err := marker.Fields(out, "LXCMEMBER", 7)
	if err != nil {
		return nil, fmt.Errorf("declare lxc-members: %w", err)
	}
	found := make([]LxcMember, 0, len(rows))
	for _, f := range rows {
		host := f[4]
		if host == "-" { // the entry leaves its host to be derived
			host = ""
		}
		found = append(found, LxcMember{Team: f[1], Container: f[2], User: f[3], Host: host, Role: f[5], State: f[6]})
	}
	return found, nil
}

// LxcCheck refuses (ErrUsage) the first value of e that the play would not accept; with team,
// container and user all given, e is also judged among the entries already declared.
func (c *Client) LxcCheck(ctx context.Context, e LxcEntry) error {
	_, err := c.runFile(ctx, "lxc-check", e.args()...)
	return err
}

// LxcAdd adds the entry to the host_vars (a backup of the file is kept beside it).
func (c *Client) LxcAdd(ctx context.Context, e LxcEntry) (Change, error) {
	return c.edit(ctx, "lxc-add", e.args()...)
}

// LxcAddDryRun is the entry LxcAdd would add, as the lines it would write; nothing is written.
func (c *Client) LxcAddDryRun(ctx context.Context, e LxcEntry) (string, error) {
	return c.runFile(ctx, "lxc-add", append(e.args(), "--dry-run")...)
}

// LxcScript is the script the play runs on the host, as root, to read a container's checkout
// (derive.py's, so there is one copy): run as `python3 -I -c SCRIPT MODE LXCPATH CONTAINER USER PATH`.
func (c *Client) LxcScript(ctx context.Context, name string) (string, error) {
	return c.run(ctx, "lxc-script", "--name", name)
}

// Derived is what the play would give an entry that leaves its host and seat out.
type Derived struct {
	Repo, Host, Seat, Handle string
}

// LxcDerive is the handle the play would give the entry, from the checkout as the root-side
// reader printed it (probe) and the team's members (roster: "HANDLE=active|parked,...", empty for
// none). Both come from the container and are judged by declare.py, never here.
func (c *Client) LxcDerive(ctx context.Context, e LxcEntry, probe, roster string) (Derived, error) {
	args := []string{flagTeam, e.Team, "--container", e.Container, "--user", e.User, "--probe", probe, "--roster", roster}
	if e.Host != "" {
		args = append(args, "--host", e.Host)
	}
	if e.Seat != "" {
		args = append(args, "--seat", e.Seat)
	}
	out, err := c.runFile(ctx, "lxc-derive", args...)
	if err != nil {
		return Derived{}, err
	}
	rows, err := marker.Fields(out, "DERIVED", 5)
	if err != nil || len(rows) != 1 {
		return Derived{}, fmt.Errorf("declare lxc-derive: unreadable answer %q", out)
	}
	f := rows[0]
	return Derived{Repo: f[1], Host: f[2], Seat: f[3], Handle: f[4]}, nil
}

// Checkout is a checkout in a container's user home whose origin is on GitHub.
type Checkout struct {
	Path string // inside the container
	Repo string // OWNER/REPO, lower case
	Ccy  bool   // it has a .claude/ccy directory: a ccy project
}

// Checkouts is what the search of a user's home found, ccy projects first, and whether it
// stopped at its time limit before it was done.
type Checkouts struct {
	Found    []Checkout
	TimedOut bool
}

// LxcCheckouts are the checkouts with a GitHub remote among those the root-side reader found
// (scan is what it printed, judged by declare.py).
func (c *Client) LxcCheckouts(ctx context.Context, scan string) (Checkouts, error) {
	out, err := c.runFile(ctx, "lxc-checkouts", "--scan", scan)
	if err != nil {
		return Checkouts{}, err
	}
	rows, err := marker.Fields(out, "CHECKOUT", 4)
	if err != nil {
		return Checkouts{}, fmt.Errorf("declare lxc-checkouts: %w", err)
	}
	cut, err := marker.Fields(out, "TIMEDOUT", 1)
	if err != nil {
		return Checkouts{}, fmt.Errorf("declare lxc-checkouts: %w", err)
	}
	found := Checkouts{Found: make([]Checkout, 0, len(rows)), TimedOut: len(cut) > 0}
	for _, f := range rows {
		found.Found = append(found.Found, Checkout{Path: f[1], Repo: f[2], Ccy: f[3] == "ccy"})
	}
	return found, nil
}
