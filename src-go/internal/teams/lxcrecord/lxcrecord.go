// Package lxcrecord reads the LXC member record play-agent-bus.yml writes for each team
// (helpers/agent_bus_lxc/members.py; Plan 00203 SPEC-lxc-stopped-members.md L8): what the last
// run found for each agent_bus_lxc_members entry, so ccy-teams can say which members were held
// back because their containers were not running. It sits beside the applied stamps, readable
// by the same group, and holds no secret.
package lxcrecord

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Dir is the record's directory under the applied stamps' directory.
const Dir = "lxc"

// version is the record version this reader knows.
const version = 1

// An item's status: joined by the last run; a member whose container was not running (skipped);
// held back to a later run (deferred); taken off the team.
const (
	StatusJoined   = "joined"
	StatusStopped  = "stopped"
	StatusDeferred = "deferred"
	StatusLeft     = "left"
)

// Item is one entry's line of the record.
type Item struct {
	Container string `json:"container"`
	User      string `json:"user"`
	State     string `json:"state"`
	Status    string `json:"status"`
	Handle    string `json:"handle"`
	Checkout  string `json:"checkout"`
	Why       string `json:"why"`
	Since     int64  `json:"since"`
}

// Label is the member as the play names it: user@container.
func (i Item) Label() string { return i.User + "@" + i.Container }

// Words is the item's status as a person reads it, with why.
func (i Item) Words() string {
	switch {
	case i.Status == StatusStopped:
		return "skipped (already a member): " + i.Why
	case i.Status == StatusDeferred && i.State == "absent":
		return "deferred (leaving): " + i.Why
	}
	return i.Status + ": " + i.Why
}

// Record is a team's record.
type Record struct {
	Version int    `json:"version"`
	Team    string `json:"team"`
	Members []Item `json:"members"`
}

// HeldBack is the items the last run skipped or deferred, in entry order.
func (r Record) HeldBack() []Item {
	var held []Item
	for _, item := range r.Members {
		if item.Status == StatusStopped || item.Status == StatusDeferred {
			held = append(held, item)
		}
	}
	return held
}

// Find is the item of the container's user, if the record has one.
func (r Record) Find(container, user string) (Item, bool) {
	for _, item := range r.Members {
		if item.Container == container && item.User == user {
			return item, true
		}
	}
	return Item{}, false
}

// Read is the team's record under appliedDir; found is false when there is none (a team with no
// LXC member, or not applied since the record began).
func Read(appliedDir, team string) (Record, bool, error) {
	var rec Record
	path := filepath.Join(appliedDir, Dir, team+".json")
	data, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("the LXC member record %s: %w", path, err)
	}
	if err = json.Unmarshal(data, &rec); err != nil {
		return Record{}, false, fmt.Errorf("the LXC member record %s: %w", path, err)
	}
	if rec.Version != version {
		return Record{}, false, fmt.Errorf("the LXC member record %s is version %d, not %d: ccy-teams apply %s writes it again",
			path, rec.Version, version, team)
	}
	return rec, true, nil
}
