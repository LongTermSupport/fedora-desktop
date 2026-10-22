package restore

import (
	"crypto/rand"
	"fmt"
	"io"
	"maps"
	"math/big"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// RM11's random epoch: drawn above m in a window a million wide, below the ceiling.
const (
	fromBackupWindow  = 1_000_000
	fromBackupCeiling = 500_000_000
)

// The roster's needs values.
const (
	needsReissue  = "reissue"
	needsPassword = "password"
)

// NoteElementPasswords is RM7's fixed line (n2g-D19).
const NoteElementPasswords = "a password a human changed from Element after the backup is in no roster, so any such human " +
	"has the backup's old password back; nobody was revoked on a guess"

// The fixed lines of a --roster-from-backup merge (RM11, n2g-D21).
const (
	unknownRoster = "the current roster could not be read: every credential in the backup is revoked, and every " +
		"active handle and every human needs a new one"
	unknownHandles = "a handle added after the backup that the status record does not list is unknown"
	noteNoPre      = "no pre-restore backup is taken: its tar needs the current roster, which could not be read"
	noteApplyWaits = "a team apply was under way: the applied team.json is bootstrapped, and the declaration it was " +
		"writing waits for the next team apply"
)

var kindRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// Lists are what the merge decided beyond the roster: who the secure op retires, revokes and
// locks (RC1), who the answer names (RS9), RM12's resolution and the notes.
type Lists struct {
	Retire, Revoke, RevokeHumans, Lock []string
	Reissue, Return, Password, Unknown []string
	// Pending is RM12's resolution of a pending operation, "" for none.
	Pending string
	Notes   []string
}

// Merged is the merged roster and its lists (RM1-RM12).
type Merged struct {
	Roster apply.RosterFile
	Lists  Lists
}

// MergeOptions are the restore's team, the high-water epoch of restore-epoch.json (RM10), and
// whether the restore is offline (RM12).
type MergeOptions struct {
	Team    paths.TeamID
	High    int64
	Offline bool
}

// Merge is the merged roster of the current roster and backup N's (RM1-RM10, RM12). B is the
// backup's seq; "after N" is a seq above B.
func Merge(current, backup apply.RosterFile, o MergeOptions) (Merged, error) {
	if err := checkRosters(o.Team, backup, current); err != nil {
		return Merged{}, err
	}
	if p := current.Pending; p != nil && !o.Offline {
		return Merged{}, pendingRefusal(o.Team, p)
	}
	b := backup.Seq
	m := Merged{Roster: apply.NewRosterFile(o.Team)}
	r, l := &m.Roster, &m.Lists
	r.Counters = maps.Clone(backup.Counters)
	for k, v := range current.Counters {
		r.Counters[k] = max(r.Counters[k], v)
	}
	for _, h := range union(current.Members, backup.Members) {
		r.Members[h] = mergeMember(h, current.Members, backup.Members, b, l)
	}
	for _, name := range union(current.Humans, backup.Humans) {
		if h, kept := mergeHuman(name, current.Humans, backup.Humans, b, l); kept {
			r.Humans[name] = h
		}
	}
	if p := current.Pending; p != nil {
		resolve(p, current, backup, &m)
	}
	r.Coordinator, r.CoordinatorWanted = current.Coordinator, current.CoordinatorWanted
	r.Seq, r.Rev = max(current.Seq, b)+1, current.Rev+1
	r.RestoreEpoch = max(current.RestoreEpoch, o.High) + 1
	if r.RestoreEpoch > MaxEpoch {
		return Merged{}, refusedf("the restore epoch of team %s is at its maximum %d", o.Team, MaxEpoch)
	}
	l.Notes = append(l.Notes, NoteElementPasswords)
	l.tidy()
	return m, nil
}

// mergeMember is one handle's merged entry, with the lists it goes in (RM2-RM6).
func mergeMember(h string, current, backup map[string]apply.Member, b int64, l *Lists) apply.Member {
	cur, inCur := current[h]
	bak, inBak := backup[h]
	switch {
	case !inCur:
		bak.State, bak.Needs = apply.MemberRetired, ""
		l.Retire = append(l.Retire, h)
		return bak
	case cur.State == apply.MemberRetired || bak.State == apply.MemberRetired && inBak:
		cur.State, cur.Needs = apply.MemberRetired, ""
		l.Retire = append(l.Retire, h)
	case !inBak:
		cur.State = apply.MemberLost
		l.Return = append(l.Return, h)
	case cur.State == apply.MemberActive && (cur.CredSeq > b || cur.Cred == apply.CredPassword):
		cur.Needs = needsReissue
		l.Revoke, l.Reissue = append(l.Revoke, h), append(l.Reissue, h)
	case cur.State == apply.MemberParked || cur.State == apply.MemberLost:
		l.Revoke = append(l.Revoke, h)
	}
	return cur
}

// mergeHuman is one human's merged entry and whether the merged roster keeps it (RM7). A human
// only the backup holds is not in the roster, and its restored account is locked.
func mergeHuman(name string, current, backup map[string]apply.Human, b int64, l *Lists) (apply.Human, bool) {
	cur, inCur := current[name]
	_, inBak := backup[name]
	switch {
	case !inCur:
		l.Lock = append(l.Lock, name)
		return apply.Human{}, false
	case cur.State == apply.HumanRemoved:
		l.Lock = append(l.Lock, name)
	case !inBak:
		cur.Needs = needsPassword
		l.Password = append(l.Password, name)
	case cur.RevokedSeq > b:
		cur.Needs = needsPassword
		l.Password, l.RevokeHumans = append(l.Password, name), append(l.RevokeHumans, name)
	}
	return cur, true
}

// resolve is RM12: the current roster's pending operation under --offline, in the safe
// direction, named in the lists.
func resolve(p *apply.Pending, current, backup apply.RosterFile, m *Merged) {
	l := &m.Lists
	if !strings.HasPrefix(p.Op, "member.") {
		l.Pending = p.Op + " was under way: cleared"
		l.Notes = append(l.Notes, noteApplyWaits)
		return
	}
	h := p.Handle
	merged, inMerged := m.Roster.Members[h]
	_, inBak := backup.Members[h]
	switch {
	case !inMerged || !inBak || merged.State == apply.MemberRetired:
		l.Pending = p.Op + " of " + h + ": left as the merge has it"
	case p.Op == apply.OpMemberRetire:
		merged.State, merged.Needs = apply.MemberRetired, ""
		l.drop(h)
		l.Retire = append(l.Retire, h)
		l.Pending = p.Op + " of " + h + ": retired"
	case current.Members[h].State == apply.MemberActive && backup.Members[h].State == apply.MemberActive:
		merged.Needs = needsReissue
		l.drop(h)
		l.Revoke, l.Reissue = append(l.Revoke, h), append(l.Reissue, h)
		l.Pending = p.Op + " of " + h + ": revoked, needs a reissue"
	default:
		merged.State, merged.Needs = apply.MemberLost, ""
		l.drop(h)
		l.Revoke, l.Return = append(l.Revoke, h), append(l.Return, h)
		l.Pending = p.Op + " of " + h + ": revoked, lost"
	}
	if inMerged {
		m.Roster.Members[h] = merged
	}
}

// MergeFromBackup is RM11, --roster-from-backup: the backup roster is the base, the status
// record's members raise the counters and are lost when the base lacks them, every credential
// counts as changed after N, and the epoch is drawn from random above max(H, the backup's).
func MergeFromBackup(backup apply.RosterFile, members []status.Member, o MergeOptions, random io.Reader) (Merged, error) {
	if err := checkRosters(o.Team, backup); err != nil {
		return Merged{}, err
	}
	m := Merged{Roster: apply.NewRosterFile(o.Team)}
	r, l := &m.Roster, &m.Lists
	r.Counters, r.Members, r.Humans = maps.Clone(backup.Counters), maps.Clone(backup.Members), maps.Clone(backup.Humans)
	added := map[string]bool{}
	for _, sm := range members {
		h, ok := proto.ParseHandle(sm.Handle)
		if !ok || h.String() != sm.Handle || !kindRe.MatchString(sm.Kind) {
			return Merged{}, refusedf("the status record of team %s lists a member that is not a valid handle and kind", o.Team)
		}
		if n, numbered := h.SeatNumber(); numbered {
			r.Counters[h.Prefix().String()] = max(r.Counters[h.Prefix().String()], int64(n))
		}
		if _, ok := r.Members[sm.Handle]; !ok {
			r.Members[sm.Handle] = apply.Member{State: apply.MemberLost, Kind: sm.Kind}
			added[sm.Handle] = true
			l.Return = append(l.Return, sm.Handle)
		}
	}
	for _, h := range slices.Sorted(maps.Keys(r.Members)) {
		mem := r.Members[h]
		if added[h] || mem.State == apply.MemberRetired {
			continue
		}
		l.Revoke = append(l.Revoke, h)
		if mem.State == apply.MemberActive {
			mem.Needs = needsReissue
			r.Members[h] = mem
			l.Reissue = append(l.Reissue, h)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(r.Humans)) {
		hu := r.Humans[name]
		hu.Needs = needsPassword
		r.Humans[name] = hu
		l.RevokeHumans, l.Password = append(l.RevokeHumans, name), append(l.Password, name)
	}
	r.Coordinator, r.CoordinatorWanted = backup.Coordinator, backup.CoordinatorWanted
	r.Seq, r.Rev = backup.Seq+1, backup.Rev+1
	epoch, err := drawEpoch(o, max(o.High, backup.RestoreEpoch), random)
	if err != nil {
		return Merged{}, err
	}
	r.RestoreEpoch = epoch
	l.Unknown = []string{unknownRoster, unknownHandles}
	l.Notes = []string{NoteElementPasswords, noteNoPre}
	l.tidy()
	return m, nil
}

// drawEpoch is uniform in the open interval (m, min(m + window, ceiling)); empty refuses.
func drawEpoch(o MergeOptions, m int64, random io.Reader) (int64, error) {
	width := min(m+fromBackupWindow, fromBackupCeiling) - m - 1
	if width < 1 {
		return 0, refusedf("the restore epoch of team %s has no room left below %d for a --roster-from-backup restore", o.Team, fromBackupCeiling)
	}
	n, err := rand.Int(random, big.NewInt(width))
	if err != nil {
		return 0, fmt.Errorf("cannot draw the restore epoch: %w", err)
	}
	return m + 1 + n.Int64(), nil
}

// checkRosters refuses a roster of another team, a claimed handle (RM9) and a pending
// operation in the backup's (RV5); the first roster given is the backup's.
func checkRosters(team paths.TeamID, backup apply.RosterFile, others ...apply.RosterFile) error {
	for _, f := range append([]apply.RosterFile{backup}, others...) {
		if f.Team != team {
			return refusedf("a roster of team %s is not team %s's", f.Team, team)
		}
		for _, h := range slices.Sorted(maps.Keys(f.Members)) {
			if f.Members[h].State == apply.MemberClaimed {
				return refusedf("handle %s is claimed by a member add that did not finish: run member add --team %s again first", h, team)
			}
		}
	}
	if p := backup.Pending; p != nil {
		return refusedf("the backup's roster holds a pending %s, which no restore can complete: choose another backup", p.Op)
	}
	return nil
}

// pendingRefusal is RV5's refusal of the current roster's pending operation, naming the verb
// that completes it.
func pendingRefusal(team paths.TeamID, p *apply.Pending) error {
	verb := "team apply"
	if word, member := strings.CutPrefix(p.Op, "member."); member {
		verb = "member " + word + " --team " + team.String() + " --handle " + p.Handle
	}
	return refusedf("team %s's roster holds a pending %s: run %s first, or restore --offline", team, p.Op, verb)
}

func union[V any](a, b map[string]V) []string {
	keys := slices.Collect(maps.Keys(a))
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}

// drop takes h out of every handle list.
func (l *Lists) drop(h string) {
	for _, k := range []string{listRetire, listRevoke, listReissue, listReturn} {
		f := l.field(k)
		*f = slices.DeleteFunc(*f, func(s string) bool { return s == h })
	}
}

// tidy sorts every list but the notes and drops repeats.
func (l *Lists) tidy() {
	for _, k := range listKeys {
		if k == listNotes {
			continue
		}
		f := l.field(k)
		slices.Sort(*f)
		*f = slices.Compact(*f)
	}
}
