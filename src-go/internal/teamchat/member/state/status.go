package state

import (
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/colour"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/duty"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// MaxStatus bounds status.json (B 2.9: the status line reads at most 64 KiB).
const MaxStatus = 64 << 10

const statusFile = "status.json"

// The permanent failures that stop one team (B17): a 401, a 403, lost trust, a broken bundle.
const (
	FailedUnauthorized = "unauthorized"
	FailedForbidden    = "forbidden"
	FailedTrust        = "trust"
	FailedBundle       = "bundle"
)

var failures = []string{FailedUnauthorized, FailedForbidden, FailedTrust, FailedBundle}

// Status is B 2.9's status.json, the team segment's cache, written by the link's state
// goroutine alone.
type Status struct {
	V                 int         `json:"v"`
	Run               paths.RunID `json:"run"`
	WrittenMS         int64       `json:"written_ms"`
	Desired           string      `json:"desired"`
	UnusableHookStdin int64       `json:"unusable_hook_stdin"`
	// UnusableHookLast is the last hook input that was not usable, null exactly when the
	// count is 0.
	UnusableHookLast *UnusableHook `json:"unusable_hook_last"`
	Teams            []StatusTeam  `json:"teams"`
}

// StatusTeam is one held team's row: its role (null until the roster names one), the seat's
// duty (PROPOSAL-seat-kinds.md; null where the desired membership gave none), whether it is down, the permanent failure that stopped it, and its delivery, owed and missing counts;
// and what the status line draws it with (B27, n3e-D2): this session's short member name
// there (null until the link knows its handle), the team's colour (the admin's override, else
// colour.Auto, always stored hex), and whether delivery is stalled (B 2.7.3's 120 s).
type StatusTeam struct {
	Team     paths.TeamID `json:"team"`
	Role     *string      `json:"role"`
	Duty     *duty.Duty   `json:"duty"`
	Failed   *string      `json:"failed"`
	Delivery string       `json:"delivery"`
	Pending  int64        `json:"pending"`
	Missing  int64        `json:"missing"`
	Member   *string      `json:"member"`
	// Handle is this session's full handle there, null until the link knows it; its short
	// name is Member.
	Handle  *string `json:"handle"`
	Colour  string  `json:"colour"`
	Down    bool    `json:"down"`
	Stalled bool    `json:"stalled"`
}

// validHandleOf is a null handle, or an agent handle in its canonical form whose short name is
// member where member is known.
func validHandleOf(handle, member *string) bool {
	if handle == nil {
		return true
	}
	h, ok := proto.ParseHandle(*handle)
	return ok && h.String() == *handle && (member == nil || h.Short() == *member)
}

// Encode is the file's bytes.
func (s Status) Encode() ([]byte, error) {
	s.V = Version
	return encode(statusFile, s, DecodeStatus)
}

// DecodeStatus reads status.json strictly.
func DecodeStatus(data []byte) (Status, error) {
	var s Status
	if err := decodeStrict(statusFile, data, MaxStatus, &s, func(c *rules) { s.check(c) }); err != nil {
		return Status{}, err
	}
	return s, nil
}

func (s Status) check(c *rules) {
	c.need(validMS(s.WrittenMS), "written_ms is not a time")
	c.need(slices.Contains(desiredStates, s.Desired), "desired is not applied or refused")
	c.need(s.UnusableHookStdin >= 0, "unusable_hook_stdin is negative")
	c.need(validLast(s.UnusableHookStdin, s.UnusableHookLast),
		"unusable_hook_last is not null exactly when unusable_hook_stdin is 0, or not a hook event, reason and size")
	c.need(s.Teams != nil && len(s.Teams) <= maxTeams, "teams is not a list of at most 64 teams")
	seen := map[paths.TeamID]bool{}
	for _, t := range s.Teams {
		c.need(!seen[t.Team], "teams holds a team twice")
		seen[t.Team] = true
		c.need(optionalOf(t.Role, roles), "a team's role is not coordinator, worker, probe or null")
		c.need(t.Duty == nil || t.Duty.Valid(), "a team's duty is not coordinator, worker, host or null")
		c.need(optionalOf(t.Failed, failures), "a team's failed is not unauthorized, forbidden, trust, bundle or null")
		c.need(slices.Contains(deliveries, t.Delivery), "a team's delivery is not socket, wait or none")
		c.need(t.Pending >= 0 && t.Pending <= capPending, "a team's pending is not a count its store holds")
		c.need(t.Missing >= 0 && t.Missing <= capMissed, "a team's missing is not a count its store holds")
		c.need(t.Member == nil || proto.ValidShortName(*t.Member), "a team's member is not <repo>.<seat> or null")
		c.need(validHandleOf(t.Handle, t.Member), "a team's handle is not an agent handle whose short name is its member, or null")
		c.need(colour.Stored(t.Colour), "a team's colour is not a lower-case #rrggbb")
	}
}
