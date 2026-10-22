package restore

import (
	"bytes"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// The handles and humans the merge tests use.
const (
	h1 = "web.1+desk.podman"
	h2 = "web.2+desk.podman"
	h3 = "web.3+desk.podman"
	h4 = "web.4+desk.podman"
	h7 = "web.7+desk.podman"
	hp = "web+desk.podman"
)

// roster is a roster of ops at seq and rev with the given members and humans.
func roster(t *testing.T, seq, rev, epoch int64, members map[string]apply.Member, humans map[string]apply.Human) apply.RosterFile {
	t.Helper()
	f := apply.NewRosterFile(opsTeam(t))
	f.Seq, f.Rev, f.RestoreEpoch = seq, rev, epoch
	maps.Copy(f.Members, members)
	maps.Copy(f.Humans, humans)
	return f
}

func token(state string, credSeq int64) apply.Member {
	return apply.Member{State: state, Kind: "podman", Cred: "token", CredSeq: credSeq}
}

func password(state string, credSeq int64) apply.Member {
	return apply.Member{State: state, Kind: "podman", Cred: "password", CredSeq: credSeq}
}

func human(state string, revoked int64) apply.Human {
	return apply.Human{State: state, RevokedSeq: revoked}
}

func merge(t *testing.T, current, backup apply.RosterFile, high int64) Merged {
	t.Helper()
	m, err := Merge(current, backup, MergeOptions{Team: opsTeam(t), High: high})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// RM1: each prefix's counter is the larger of the two.
func TestMergeCountersAreTheLarger(t *testing.T) {
	current := roster(t, 20, 5, 0, nil, nil)
	current.Counters[hp] = 3
	current.Counters["api+desk.podman"] = 1
	backup := roster(t, 10, 4, 0, nil, nil)
	backup.Counters[hp] = 7
	backup.Counters["old+desk.podman"] = 2
	got := merge(t, current, backup, 0).Roster.Counters
	want := map[string]int64{hp: 7, "api+desk.podman": 1, "old+desk.podman": 2}
	if len(got) != len(want) {
		t.Fatalf("counters = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("counters[%s] = %d, want %d", k, got[k], v)
		}
	}
}

// RM2-RM6: each member rule, B = 10.
func TestMergeMembers(t *testing.T) {
	for _, c := range []struct {
		name           string
		current        *apply.Member
		backup         *apply.Member
		state, needs   string
		retire, revoke bool
		reissue, ret   bool
	}{
		{name: "retired in the current", current: new(token(apply.MemberRetired, 3)), backup: new(token(apply.MemberActive, 3)), state: apply.MemberRetired, retire: true},
		{name: "retired in the backup", current: new(token(apply.MemberActive, 3)), backup: new(token(apply.MemberRetired, 3)), state: apply.MemberRetired, retire: true},
		{name: "only in the backup", backup: new(token(apply.MemberActive, 3)), state: apply.MemberRetired, retire: true},
		{name: "only in the current", current: new(token(apply.MemberActive, 12)), state: apply.MemberLost, ret: true},
		{name: "reissued after N, active in the backup", current: new(token(apply.MemberActive, 12)), backup: new(token(apply.MemberActive, 3)), state: apply.MemberActive, needs: "reissue", revoke: true, reissue: true},
		{name: "reissued after N, parked in the backup", current: new(token(apply.MemberActive, 12)), backup: new(token(apply.MemberParked, 3)), state: apply.MemberActive, needs: "reissue", revoke: true, reissue: true},
		{name: "reissued after N, lost in the backup", current: new(token(apply.MemberActive, 12)), backup: new(token(apply.MemberLost, 3)), state: apply.MemberActive, needs: "reissue", revoke: true, reissue: true},
		{name: "a password, issued before N", current: new(password(apply.MemberActive, 3)), backup: new(password(apply.MemberActive, 3)), state: apply.MemberActive, needs: "reissue", revoke: true, reissue: true},
		{name: "parked now, present in the backup", current: new(token(apply.MemberParked, 3)), backup: new(token(apply.MemberActive, 3)), state: apply.MemberParked, revoke: true},
		{name: "lost now, present in the backup", current: new(token(apply.MemberLost, 3)), backup: new(token(apply.MemberActive, 3)), state: apply.MemberLost, revoke: true},
		{name: "unchanged since N", current: new(token(apply.MemberActive, 10)), backup: new(token(apply.MemberActive, 10)), state: apply.MemberActive},
	} {
		t.Run(c.name, func(t *testing.T) {
			current, backup := roster(t, 20, 5, 0, nil, nil), roster(t, 10, 4, 0, nil, nil)
			if c.current != nil {
				current.Members[h1] = *c.current
			}
			if c.backup != nil {
				backup.Members[h1] = *c.backup
			}
			m := merge(t, current, backup, 0)
			got := m.Roster.Members[h1]
			if got.State != c.state || got.Needs != c.needs {
				t.Errorf("member = %+v, want state %s needs %q", got, c.state, c.needs)
			}
			for list, want := range map[string]bool{"retire": c.retire, "revoke": c.revoke, "reissue": c.reissue, "return": c.ret} {
				in := slices.Contains(m.Lists.byName(list), h1)
				if in != want {
					t.Errorf("in %s = %v, want %v", list, in, want)
				}
			}
		})
	}
}

// RM7: the human rules, B = 10, and the fixed line about Element passwords.
func TestMergeHumans(t *testing.T) {
	current := roster(t, 20, 5, 0, nil, map[string]apply.Human{
		"ann": human(apply.HumanActive, 12), "bob": human(apply.HumanActive, 4), "cat": human(apply.HumanActive, 0),
		"dan": human(apply.HumanRemoved, 2), "eve": human(apply.HumanActive, 15),
	})
	backup := roster(t, 10, 4, 0, nil, map[string]apply.Human{
		"ann": human(apply.HumanActive, 1), "bob": human(apply.HumanActive, 4), "dan": human(apply.HumanActive, 2),
		"fay": human(apply.HumanActive, 0),
	})
	m := merge(t, current, backup, 0)
	for list, want := range map[string][]string{
		"password": {"ann", "cat", "eve"}, "revoke_humans": {"ann"}, "lock": {"dan", "fay"},
	} {
		if got := m.Lists.byName(list); !slices.Equal(got, want) {
			t.Errorf("%s = %v, want %v", list, got, want)
		}
	}
	for name, needs := range map[string]string{"ann": "password", "bob": "", "cat": "password", "eve": "password", "dan": ""} {
		if got := m.Roster.Humans[name].Needs; got != needs {
			t.Errorf("%s needs %q, want %q", name, got, needs)
		}
	}
	if _, kept := m.Roster.Humans["fay"]; kept {
		t.Error("a human only the backup holds is in the merged roster")
	}
	if !slices.Contains(m.Lists.Notes, NoteElementPasswords) {
		t.Errorf("notes %v lack the Element line", m.Lists.Notes)
	}
}

// RM8, RM10: the current's other fields; seq, rev and the epoch raised.
func TestMergeKeepsTheCurrentAndRaises(t *testing.T) {
	current := roster(t, 20, 5, 3, map[string]apply.Member{h1: {State: apply.MemberActive, Kind: "lxc", HumanText: true, Cred: "token", CredSeq: 2, CredMS: 9, SinceMS: 8}}, nil)
	current.Coordinator, current.CoordinatorWanted = h1, h1
	backup := roster(t, 10, 4, 1, map[string]apply.Member{h1: token(apply.MemberActive, 2)}, nil)
	for _, c := range []struct {
		name                 string
		current, backup      apply.RosterFile
		high, seq, rev, epch int64
	}{
		{"current above", current, backup, 0, 21, 6, 4},
		{"the high-water above", current, backup, 9, 21, 6, 10},
		{"backup seq above", withSeq(current, 7), backup, 0, 11, 6, 4},
	} {
		m := merge(t, c.current, c.backup, c.high)
		r := m.Roster
		if r.Seq != c.seq || r.Rev != c.rev || r.RestoreEpoch != c.epch {
			t.Errorf("%s: seq %d rev %d epoch %d, want %d %d %d", c.name, r.Seq, r.Rev, r.RestoreEpoch, c.seq, c.rev, c.epch)
		}
		if r.Coordinator != h1 || r.CoordinatorWanted != h1 || r.Pending != nil {
			t.Errorf("%s: coordinator %q/%q pending %v", c.name, r.Coordinator, r.CoordinatorWanted, r.Pending)
		}
		if got := r.Members[h1]; got != c.current.Members[h1] {
			t.Errorf("%s: member %+v, want the current's", c.name, got)
		}
	}
}

func withSeq(f apply.RosterFile, seq int64) apply.RosterFile {
	f.Seq = seq
	return f
}

// RM9, RM10: a claimed handle, another team and the epoch's maximum are refused.
func TestMergeRefuses(t *testing.T) {
	other := apply.NewRosterFile(team(t, "dev"))
	claimed := roster(t, 20, 5, 0, map[string]apply.Member{h2: token(apply.MemberClaimed, 0)}, nil)
	ok := roster(t, 10, 4, 0, nil, nil)
	for _, c := range []struct {
		name            string
		current, backup apply.RosterFile
		high            int64
		names           string
	}{
		{"claimed in the current", claimed, ok, 0, h2},
		{"claimed in the backup", ok, withSeq(claimed, 5), 0, h2},
		{"the current of another team", other, ok, 0, "dev"},
		{"the backup of another team", ok, other, 0, "dev"},
		{"the epoch at its maximum", ok, ok, MaxEpoch, "epoch"},
	} {
		_, err := Merge(c.current, c.backup, MergeOptions{Team: opsTeam(t), High: c.high})
		if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), c.names) {
			t.Errorf("%s: Merge = %v, want a refusal naming %q", c.name, err, c.names)
		}
	}
}

// RM12: a pending operation under --offline, each resolved in the safe direction and named;
// without --offline it is refused.
func TestMergeResolvesAPendingOperationOffline(t *testing.T) {
	base := func() (apply.RosterFile, apply.RosterFile) {
		current := roster(t, 20, 5, 0, map[string]apply.Member{h1: token(apply.MemberActive, 3), h2: token(apply.MemberParked, 3)}, nil)
		backup := roster(t, 10, 4, 0, map[string]apply.Member{h1: token(apply.MemberActive, 3), h2: token(apply.MemberActive, 3)}, nil)
		return current, backup
	}
	for _, c := range []struct {
		name, op, handle string
		state, needs     string
		list             string
	}{
		{"team apply", apply.OpTeamApply, "", "", "", ""},
		{"a retire", apply.OpMemberRetire, h1, apply.MemberRetired, "", "retire"},
		{"a park of an active handle", apply.OpMemberPark, h1, apply.MemberActive, "reissue", "revoke"},
		{"a reissue of a parked handle", apply.OpMemberReissue, h2, apply.MemberLost, "", "revoke"},
	} {
		t.Run(c.name, func(t *testing.T) {
			current, backup := base()
			current.Pending = &apply.Pending{Op: c.op, Step: "x", Handle: c.handle}
			if _, err := Merge(current, backup, MergeOptions{Team: opsTeam(t)}); !errors.Is(err, ErrRefused) {
				t.Fatalf("online Merge = %v, want a refusal", err)
			}
			m, err := Merge(current, backup, MergeOptions{Team: opsTeam(t), Offline: true})
			if err != nil {
				t.Fatal(err)
			}
			if m.Roster.Pending != nil || m.Lists.Pending == "" || !strings.Contains(m.Lists.Pending, c.op) {
				t.Errorf("pending %v, resolution %q", m.Roster.Pending, m.Lists.Pending)
			}
			checkResolved(t, m, c.handle, c.state, c.needs, c.list)
		})
	}
}

func checkResolved(t *testing.T, m Merged, handle, state, needs, list string) {
	t.Helper()
	if handle == "" {
		if !slices.ContainsFunc(m.Lists.Notes, func(n string) bool { return strings.Contains(n, "team apply") }) {
			t.Errorf("notes %v do not say the declaration waits for team apply", m.Lists.Notes)
		}
		return
	}
	got := m.Roster.Members[handle]
	if got.State != state || got.Needs != needs || !slices.Contains(m.Lists.byName(list), handle) {
		t.Errorf("member %+v in %s %v, want state %s needs %q", got, list, m.Lists.byName(list), state, needs)
	}
}

// RV5: the backup roster's pending is refused, --offline or not.
func TestMergeRefusesTheBackupsPending(t *testing.T) {
	backup := roster(t, 10, 4, 0, nil, nil)
	backup.Pending = &apply.Pending{Op: apply.OpTeamApply, Step: "x"}
	for _, offline := range []bool{false, true} {
		if _, err := Merge(roster(t, 20, 5, 0, nil, nil), backup, MergeOptions{Team: opsTeam(t), Offline: offline}); !errors.Is(err, ErrRefused) {
			t.Errorf("offline %v: Merge = %v, want a refusal", offline, err)
		}
	}
}

// RM11: --roster-from-backup.
func TestMergeFromBackup(t *testing.T) {
	backup := roster(t, 10, 4, 3, map[string]apply.Member{
		h1: token(apply.MemberActive, 3), h2: token(apply.MemberParked, 3), h3: token(apply.MemberRetired, 3),
	}, map[string]apply.Human{"ann": human(apply.HumanActive, 0)})
	backup.Counters[hp] = 3
	backup.Coordinator = h1
	members := []status.Member{{Handle: h1, Kind: "podman"}, {Handle: h3, Kind: "podman"}, {Handle: h7, Kind: "lxc"}, {Handle: h4, Kind: "podman"}}
	m, err := MergeFromBackup(backup, members, MergeOptions{Team: opsTeam(t), High: 40}, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	r := m.Roster
	if r.Seq != 11 || r.Rev != 5 || r.Counters[hp] != 7 || r.Coordinator != h1 {
		t.Errorf("seq %d rev %d counter %d coordinator %q", r.Seq, r.Rev, r.Counters[hp], r.Coordinator)
	}
	if r.RestoreEpoch <= 40 || r.RestoreEpoch >= 40+fromBackupWindow {
		t.Errorf("epoch %d is not in (40, 40+window)", r.RestoreEpoch)
	}
	for handle, want := range map[string]apply.Member{
		h1: {State: apply.MemberActive, Kind: "podman", Cred: "token", CredSeq: 3, Needs: "reissue"},
		h2: token(apply.MemberParked, 3), h3: token(apply.MemberRetired, 3),
		h7: {State: apply.MemberLost, Kind: "lxc"}, h4: {State: apply.MemberLost, Kind: "podman"},
	} {
		if got := r.Members[handle]; got != want {
			t.Errorf("%s = %+v, want %+v", handle, got, want)
		}
	}
	for list, want := range map[string][]string{
		"revoke": {h1, h2}, "reissue": {h1}, "revoke_humans": {"ann"}, "password": {"ann"}, "return": {h4, h7},
		"retire": nil, "lock": nil,
	} {
		if got := m.Lists.byName(list); !slices.Equal(got, want) {
			t.Errorf("%s = %v, want %v", list, got, want)
		}
	}
	if r.Humans["ann"].Needs != "password" || len(m.Lists.Unknown) == 0 {
		t.Errorf("ann needs %q; unknown %v", r.Humans["ann"].Needs, m.Lists.Unknown)
	}
	if !slices.ContainsFunc(m.Lists.Notes, func(n string) bool { return strings.Contains(n, "pre-restore backup") }) {
		t.Errorf("notes %v do not say why no pre-restore backup is taken", m.Lists.Notes)
	}
}

// RM11: the random epoch's window: above max(H, the backup's), at most a million wide and below
// the ceiling; an empty window refuses; a random source that fails is a failure.
func TestMergeFromBackupEpochWindow(t *testing.T) {
	ff := bytes.Repeat([]byte{0xff}, 64)
	for _, c := range []struct {
		name           string
		high, backupEp int64
		random         []byte
		lo, hi         int64
		refused        bool
	}{
		{"the high-water above", 40, 3, make([]byte, 64), 41, 41, false},
		{"the backup's above", 3, 50, make([]byte, 64), 51, 51, false},
		{"near the ceiling", fromBackupCeiling - 3, 0, ff, fromBackupCeiling - 2, fromBackupCeiling - 1, false},
		{"at the ceiling", fromBackupCeiling - 1, 0, ff, 0, 0, true},
	} {
		backup := roster(t, 10, 4, c.backupEp, nil, nil)
		m, err := MergeFromBackup(backup, nil, MergeOptions{Team: opsTeam(t), High: c.high}, bytes.NewReader(c.random))
		if c.refused {
			if !errors.Is(err, ErrRefused) {
				t.Errorf("%s: %v, want a refusal", c.name, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if e := m.Roster.RestoreEpoch; e < c.lo || e > c.hi {
			t.Errorf("%s: epoch %d, want [%d, %d]", c.name, e, c.lo, c.hi)
		}
	}
	if _, err := MergeFromBackup(roster(t, 10, 4, 0, nil, nil), nil, MergeOptions{Team: opsTeam(t)}, bytes.NewReader(nil)); err == nil || errors.Is(err, ErrRefused) {
		t.Errorf("an empty random source = %v, want a failure", err)
	}
}

// RM11 with RM9: the base's claimed handle, its pending, another team and a bad status handle
// are refused.
func TestMergeFromBackupRefuses(t *testing.T) {
	claimed := roster(t, 10, 4, 0, map[string]apply.Member{h2: token(apply.MemberClaimed, 0)}, nil)
	pending := roster(t, 10, 4, 0, nil, nil)
	pending.Pending = &apply.Pending{Op: apply.OpTeamApply, Step: "x"}
	for name, c := range map[string]struct {
		backup  apply.RosterFile
		members []status.Member
	}{
		"claimed":          {claimed, nil},
		"pending":          {pending, nil},
		"another team":     {apply.NewRosterFile(team(t, "dev")), nil},
		"a handle unknown": {roster(t, 10, 4, 0, nil, nil), []status.Member{{Handle: "not a handle"}}},
	} {
		if _, err := MergeFromBackup(c.backup, c.members, MergeOptions{Team: opsTeam(t)}, bytes.NewReader(make([]byte, 64))); !errors.Is(err, ErrRefused) {
			t.Errorf("%s: %v, want a refusal", name, err)
		}
	}
}
