package health

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// GuestProbe is an LXC member's seat lock as the guest probe found it (C 3.9.2): the container
// stopped, or the lock held, or free; Record is the lock file's content, nil when there is none.
type GuestProbe struct {
	Stopped bool
	Held    bool
	Record  []byte
}

// Guests probes an LXC member's seat lock through its guest's root (C 3.9.2, the L3 probe).
type Guests interface {
	Probe(ctx context.Context, team paths.TeamID, handle string) (GuestProbe, error)
}

// Notice is a disconnected notice judged: posted (or a goodbye found), or not possible for
// good, and why.
type Notice struct {
	Posted bool
	Why    string
}

// Announcer is the child's `member.disconnected` (C 3.11.2 step 6): posted as the member with
// the guest token copy, transaction id `tc-disc-<run>`. An error is a transient failure.
type Announcer interface {
	Disconnected(ctx context.Context, team paths.TeamID, handle string, run paths.RunID, sinceMS int64) (Notice, error)
}

// guestSeen is one LXC member's probe at step 3.
type guestSeen struct {
	probe GuestProbe
	err   error
}

// probeGuests is step 3's probe of every LXC member's seat lock.
func (t *Tick) probeGuests(ctx context.Context, team paths.TeamID, f *facts) map[string]guestSeen {
	seen := map[string]guestSeen{}
	for _, m := range f.view.Members {
		if m.Kind == status.KindLXC {
			probe, err := t.cfg.Guests.Probe(ctx, team, m.Handle)
			seen[m.Handle] = guestSeen{probe: probe, err: err}
		}
	}
	return seen
}

// maxOnline caps lxc-online.json.
const maxOnline = 1 << 20

// onlineFile is lxc-online.json (n2e-D6): per LXC handle, whether its lock was held at the last
// tick, the run it named, since when, and whether that run's notice is settled.
type onlineFile struct {
	V       int                    `json:"v"`
	Handles map[string]onlineEntry `json:"handles"`
}

type onlineEntry struct {
	Held    bool   `json:"held"`
	Run     string `json:"run"`
	SinceMS int64  `json:"since_ms"`
	Posted  bool   `json:"posted"`
}

// readOnline is lxc-online.json, empty when there is none; a broken one is an error.
func readOnline(path string) (onlineFile, error) {
	data, found, err := state.ReadFile(path, maxOnline)
	if err != nil || !found {
		return onlineFile{V: 1, Handles: map[string]onlineEntry{}}, err
	}
	var rec onlineFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	err = dec.Decode(&rec)
	if err == nil {
		if _, tokErr := dec.Token(); !errors.Is(tokErr, io.EOF) {
			err = errors.New("content after the value")
		}
	}
	if err == nil {
		err = rec.check(data)
	}
	if err != nil {
		return onlineFile{}, fmt.Errorf("health: %s: %w", path, err)
	}
	return rec, nil
}

func (r onlineFile) check(data []byte) error {
	if r.V != 1 || r.Handles == nil {
		return errors.New("not version 1 with handles")
	}
	if _, err := proto.Parse(data); err != nil {
		return err
	}
	for handle, e := range r.Handles {
		if _, ok := proto.ParseHandle(handle); !ok || e.SinceMS < 0 {
			return errors.New("an entry is not valid")
		}
		if _, err := paths.ParseRunID(e.Run); e.Run != "" && err != nil {
			return err
		}
	}
	return nil
}

// online is step 6, under the lock (HT6): each LXC member's entry moved on from its probe, a
// run that ended asked of the announcer by the id this record held, and lxc-online.json written
// on change. It answers the members with online set, and step 6's problems.
func (t *Tick) online(ctx context.Context, team paths.TeamID, f *facts) ([]status.Member, []status.Problem, error) {
	path := t.cfg.LXCOnlinePath(team)
	was, err := readOnline(path)
	if err != nil {
		return nil, nil, err
	}
	next := onlineFile{V: 1, Handles: map[string]onlineEntry{}}
	members := slices.Clone(f.view.Members)
	var problems []status.Problem
	for i, m := range members {
		if m.Kind != status.KindLXC {
			continue
		}
		seen := f.guests[m.Handle]
		entry, found := t.moveOn(ctx, team, m, was.Handles[m.Handle], seen, &problems)
		if found {
			next.Handles[m.Handle] = entry
		}
		online := seen.err == nil && seen.probe.Held && !seen.probe.Stopped
		members[i].Online = &online
	}
	if !maps.Equal(next.Handles, was.Handles) {
		data, err := json.Marshal(next)
		if err != nil {
			return nil, nil, err
		}
		if err := state.WriteFile(path, data, 0o600, -1); err != nil {
			return nil, nil, err
		}
	}
	return members, problems, nil
}

// memberActive is the roster state of a member whose end is announced.
const memberActive = "active"

// held is a held lock's entry: the run its record names (none for a record that does not decode
// by B's grammar), since now when the run is new.
func (t *Tick) held(entry onlineEntry, lock seat.LockRecord, lockErr error) onlineEntry {
	run := ""
	if lockErr == nil {
		run = lock.Run.String()
	}
	if run != entry.Run || run == "" {
		entry = onlineEntry{Run: run, SinceMS: t.cfg.Clock.Now().UnixMilli()}
	}
	entry.Held = true
	return entry
}

// moveOn is one LXC member's entry after this tick's probe; found is false for no entry.
func (t *Tick) moveOn(ctx context.Context, team paths.TeamID, m status.Member, entry onlineEntry, seen guestSeen, problems *[]status.Problem) (onlineEntry, bool) {
	found := entry != (onlineEntry{})
	if seen.err != nil {
		*problems = append(*problems, status.Problem{What: "the seat of " + m.Handle + " could not be probed", Fix: "journalctl -u teamchat-health.service"})
		return entry, found
	}
	lock, lockErr := seat.DecodeLockRecord(seen.probe.Record)
	if seen.probe.Held && !seen.probe.Stopped {
		return t.held(entry, lock, lockErr), true
	}
	entry.Held = false
	if !seen.probe.Stopped && lockErr == nil && lock.LaunchFailed {
		*problems = append(*problems, status.Problem{What: m.Handle + " launch-failed", Fix: "ccy-teams restart " + m.Handle})
		if lock.Run.String() == entry.Run {
			entry.Posted = true
		}
	}
	if entry.Run == "" || entry.Posted {
		return entry, found
	}
	if m.State != memberActive {
		entry.Posted = true
		return entry, true
	}
	run, err := paths.ParseRunID(entry.Run)
	if err != nil {
		return entry, true
	}
	if _, err := t.cfg.Announcer.Disconnected(ctx, team, m.Handle, run, entry.SinceMS); err != nil {
		*problems = append(*problems, status.Problem{What: "the disconnected notice for " + m.Handle + " is not posted yet", Fix: "the next health tick retries it"})
		return entry, true
	}
	entry.Posted = true
	return entry, true
}
