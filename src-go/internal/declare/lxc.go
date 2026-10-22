package declare

import (
	"context"
	"fmt"
	"strconv"

	busdeclare "github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/declare"
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

// LxcMembers is the entries of agent_bus_lxc_members, in file order, as the play reads them.
func (c *Client) LxcMembers(context.Context) ([]LxcMember, error) {
	return read(c, busdeclare.HostVars.LxcMembers)
}

// Identity is the repo and host of the handles a checkout gives: <repo>.<seat>+<host>.lxc.
type Identity struct {
	Repo, Host string
}

// LxcIdentity is the identity of the checkout the root-side reader printed (probe), by
// derive.py's rule: ccy-teams finds which entry a live LXC handle is with it.
func (c *Client) LxcIdentity(ctx context.Context, probe string) (Identity, error) {
	out, err := c.runFile(ctx, "lxc-identity", "--probe", probe)
	if err != nil {
		return Identity{}, err
	}
	rows, err := marker.Fields(out, "IDENTITY", 3)
	if err != nil || len(rows) != 1 {
		return Identity{}, fmt.Errorf("declare lxc-identity: unreadable answer %q", out)
	}
	return Identity{Repo: rows[0][1], Host: rows[0][2]}, nil
}

// LxcCheck refuses the first value of e that the play would not accept (ErrUsage: the value is
// wrong, so asking again can help); with team, container and user all given, e is also judged
// among the entries already declared, in place of the member's own entry when it has one
// (ErrRefused: the file's entries refuse it, so the same answer never passes).
func (c *Client) LxcCheck(ctx context.Context, e LxcEntry) error {
	_, err := c.runFile(ctx, "lxc-check", e.args()...)
	return err
}

// LxcAdd adds the entry to the host_vars (a backup of the file is kept beside it). A member the
// file declares already as e is not Changed; declared otherwise, it is refused (ErrRefused)
// naming the keys that differ (LxcCompare reads them, LxcUpdate replaces the entry).
func (c *Client) LxcAdd(ctx context.Context, e LxcEntry) (Change, error) {
	return c.edit(ctx, "lxc-add", e.args()...)
}

// LxcUpdate makes the entry already declaring e's member e, in place (a backup is kept); not
// Changed when it is e already.
func (c *Client) LxcUpdate(ctx context.Context, e LxcEntry) (Change, error) {
	return c.edit(ctx, "lxc-add", append(e.args(), "--update")...)
}

// Difference is a key on which a declared entry and a given one differ, each value as JSON, "-"
// for a key not set.
type Difference struct {
	Key, File, Given string
}

// Declared is the entry already declaring a member: its number in agent_bus_lxc_members (0: the
// file declares no such member) and how it differs from the entry given.
type Declared struct {
	Entry   int
	Differs []Difference
}

// LxcCompare is the entry already declaring e's member (team, container, user) and the keys
// on which it differs from e, as the play reads them: declare.py's one rule.
func (c *Client) LxcCompare(ctx context.Context, e LxcEntry) (Declared, error) {
	out, err := c.runFile(ctx, "lxc-compare", e.args()...)
	if err != nil {
		return Declared{}, err
	}
	entry, err := marker.Fields(out, "DECLARED", 2)
	if err != nil || len(entry) > 1 {
		return Declared{}, fmt.Errorf("declare lxc-compare: unreadable answer %q", out)
	}
	differs, err := marker.Fields(out, "DIFFERS", 4)
	if err != nil || (len(entry) == 0 && len(differs) > 0) {
		return Declared{}, fmt.Errorf("declare lxc-compare: unreadable answer %q", out)
	}
	if len(entry) == 0 {
		return Declared{}, nil
	}
	n, err := strconv.Atoi(entry[0][1])
	if err != nil || n < 1 {
		return Declared{}, fmt.Errorf("declare lxc-compare: unreadable answer %q", out)
	}
	found := Declared{Entry: n}
	for _, f := range differs {
		found.Differs = append(found.Differs, Difference{Key: f[1], File: f[2], Given: f[3]})
	}
	return found, nil
}

// LxcSetRole sets a declared member's role in its entry (a backup of the file is kept beside
// it): lxc-add's takeover makes the declared coordinator a worker with it first.
func (c *Client) LxcSetRole(ctx context.Context, m LxcMember, role string) (Change, error) {
	return c.edit(ctx, "lxc-role", "--team", m.Team, "--container", m.Container, "--user", m.User, "--role", role)
}

// LxcAddDryRun is the entry LxcAdd (LxcUpdate, when update) would write, as its lines; nothing
// is written.
func (c *Client) LxcAddDryRun(ctx context.Context, e LxcEntry, update bool) (string, error) {
	args := e.args()
	if update {
		args = append(args, "--update")
	}
	return c.runFile(ctx, "lxc-add", append(args, "--dry-run")...)
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

// Checkout is a checkout in a container whose origin is on GitHub.
type Checkout struct {
	Path string // inside the container
	Repo string // OWNER/REPO, lower case
	Ccy  bool   // it has a .claude/ccy directory: a ccy project
}

// Checkouts is what the search of a container (the user's home, /var/www, /srv, /opt) found,
// ccy projects first, whether it is partial (it stopped at its time or count limit), and the
// search roots it did not search because a symbolic link is on their way.
type Checkouts struct {
	Found   []Checkout
	Partial bool
	Skipped []string
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
	cut, err := marker.Fields(out, "PARTIAL", 1)
	if err != nil {
		return Checkouts{}, fmt.Errorf("declare lxc-checkouts: %w", err)
	}
	skipped, err := marker.Fields(out, "SKIPPED", 2)
	if err != nil {
		return Checkouts{}, fmt.Errorf("declare lxc-checkouts: %w", err)
	}
	found := Checkouts{Found: make([]Checkout, 0, len(rows)), Partial: len(cut) > 0}
	for _, f := range skipped {
		found.Skipped = append(found.Skipped, f[1])
	}
	for _, f := range rows {
		found.Found = append(found.Found, Checkout{Path: f[1], Repo: f[2], Ccy: f[3] == "ccy"})
	}
	return found, nil
}
