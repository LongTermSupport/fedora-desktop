package state

import (
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
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
	V                 int          `json:"v"`
	Run               paths.RunID  `json:"run"`
	WrittenMS         int64        `json:"written_ms"`
	Desired           string       `json:"desired"`
	UnusableHookStdin int64        `json:"unusable_hook_stdin"`
	Teams             []StatusTeam `json:"teams"`
}

// StatusTeam is one held team's row: its role (null until the roster names one), whether it
// is down, the permanent failure that stopped it, and its delivery, owed and missing counts.
type StatusTeam struct {
	Team     paths.TeamID `json:"team"`
	Role     *string      `json:"role"`
	Down     bool         `json:"down"`
	Failed   *string      `json:"failed"`
	Delivery string       `json:"delivery"`
	Pending  int64        `json:"pending"`
	Missing  int64        `json:"missing"`
}

// Encode is the file's bytes.
func (s Status) Encode() ([]byte, error) {
	s.V = Version
	return encode(statusFile, s, DecodeStatus)
}

// DecodeStatus reads status.json strictly.
func DecodeStatus(data []byte) (Status, error) {
	var s Status
	if _, err := decodeStrict(statusFile, data, MaxStatus, &s, func(c *rules) { s.check(c) }); err != nil {
		return Status{}, err
	}
	return s, nil
}

func (s Status) check(c *rules) {
	c.need(validMS(s.WrittenMS), "written_ms is not a time")
	c.need(slices.Contains(desiredStates, s.Desired), "desired is not applied or refused")
	c.need(s.UnusableHookStdin >= 0, "unusable_hook_stdin is negative")
	c.need(s.Teams != nil && len(s.Teams) <= maxTeams, "teams is not a list of at most 64 teams")
	seen := map[paths.TeamID]bool{}
	for _, t := range s.Teams {
		c.need(!seen[t.Team], "teams holds a team twice")
		seen[t.Team] = true
		c.need(optionalOf(t.Role, roles), "a team's role is not coordinator, worker, probe or null")
		c.need(optionalOf(t.Failed, failures), "a team's failed is not unauthorized, forbidden, trust, bundle or null")
		c.need(slices.Contains(deliveries, t.Delivery), "a team's delivery is not socket, wait or none")
		c.need(t.Pending >= 0 && t.Pending <= capPending, "a team's pending is not a count its store holds")
		c.need(t.Missing >= 0 && t.Missing <= capMissed, "a team's missing is not a count its store holds")
	}
}
