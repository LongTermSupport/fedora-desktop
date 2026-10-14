package declare

import (
	"context"
	"fmt"
	"strings"

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

// SeatAdvice says whether the container's name gives its seat. When it does not (no letter
// first, or another container of the team holds it) the seat is to be asked for, with a
// suggestion the play accepts, or none.
type SeatAdvice struct {
	Needed     bool
	Suggestion string
}

// LxcSuggestSeat is the seat advice for the container in the team.
func (c *Client) LxcSuggestSeat(ctx context.Context, team, container string) (SeatAdvice, error) {
	out, err := c.runFile(ctx, "lxc-suggest-seat", "--team", team, "--container", container)
	if err != nil {
		return SeatAdvice{}, err
	}
	fields := strings.Split(strings.TrimRight(out, "\r\n"), "\t")
	switch {
	case len(fields) == 2 && fields[0] == "SEAT" && fields[1] == "default":
		return SeatAdvice{}, nil
	case len(fields) == 3 && fields[0] == "SEAT" && fields[1] == "needed":
		return SeatAdvice{Needed: true, Suggestion: fields[2]}, nil
	}
	return SeatAdvice{}, fmt.Errorf("declare lxc-suggest-seat: unreadable answer %q", out)
}

// LxcCheckSeat refuses (ErrUsage) a seat the play would not take for the container in the team.
func (c *Client) LxcCheckSeat(ctx context.Context, team, container, seat string) error {
	_, err := c.runFile(ctx, "lxc-suggest-seat", "--team", team, "--container", container, "--seat", seat)
	return err
}
