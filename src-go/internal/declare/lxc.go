package declare

import (
	"context"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/marker"
)

// LxcEntry is the values of an agent_bus_lxc_members entry that lxc-add and lxc-check take;
// empty fields are not passed. It is one agent user of an existing LXC container joining a team.
type LxcEntry struct {
	Team      string
	Container string
	User      string
	Host      string // the handle's host word, a role name and never a machine's hostname
	Checkout  string // an absolute path inside the container
	Role      string // worker (the default) or coordinator
	Seat      string // the handle's seat; by default the container's name gives it
}

func (e LxcEntry) args() []string {
	var args []string
	for _, f := range []struct{ flag, value string }{
		{"--team", e.Team},
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
		found = append(found, LxcMember{Team: f[1], Container: f[2], User: f[3], Host: f[4], Role: f[5], State: f[6]})
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

// LxcRemove declares the member absent, which the play applies by parking it and deleting its
// bundle; Changed is false when it already was.
func (c *Client) LxcRemove(ctx context.Context, team, container, user string) (Change, error) {
	return c.edit(ctx, "lxc-remove", "--team", team, "--container", container, "--user", user)
}

// LxcSuggestHost is the container's name as a handle host word, or "" when it will not do.
func (c *Client) LxcSuggestHost(ctx context.Context, container string) (string, error) {
	out, err := c.runFile(ctx, "lxc-suggest-host", "--container", container)
	return plain(out), err
}
