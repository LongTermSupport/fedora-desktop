package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore/drill"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// The scratch team's names: its one human, the prefix of its two token members, the lock the
// whole drill holds (an rd- id, which declare refuses, so no real team shares it) and the verb
// each of its team locks records.
const (
	drillHuman      = "drill"
	drillPrefix     = "drill+scratch.podman"
	drillKind       = "podman"
	drillLockID     = "rd-drill"
	drillVerb       = "restore-drill"
	drillDeviceName = "restore drill"
)

// DrillRig is drill.Rig over the host (issue #324): the scratch team declared, approved and
// applied in process, its members and human made through the child, messages and floor markers
// posted on its own loopback server, backup N, the engine's restores with the drill's hooks,
// and the probes of each check. One DrillRig is one drill: Apply and Seed keep what the later
// steps need.
type DrillRig struct {
	Sys *System
	// Engine is the restore engine's Deps, its hooks unset. RunRestore, RunDiscard and
	// StagedUp are restore.Run, restore.Discard and restore.Staged over it.
	Engine     restore.Deps
	RunRestore func(ctx context.Context, d restore.Deps, held *teamlock.Lock, req restore.Request) (restore.Answer, error)
	RunDiscard func(ctx context.Context, d restore.Deps, team paths.TeamID) (restore.Answer, error)
	StagedUp   func(ctx context.Context, d restore.Deps, team paths.TeamID, fn func(context.Context, func(context.Context) (restore.Epoch, error)) error) error
	// TakeBackup is backup.Take under the held lock, answering the backup's number.
	TakeBackup func(ctx context.Context, held *teamlock.Lock) (int64, error)
	// OwnersOf is restore.OS.Owners.
	OwnersOf func(team paths.TeamID) (map[string]uint32, error)
	// Base is the team's homeserver as the host reaches it, http://127.0.0.1:<port>.
	Base func(port int) string
	// Dial is whether a TCP connect to addr is answered.
	Dial func(ctx context.Context, addr netip.AddrPort) (bool, error)
	// Clock is how the Matrix clients wait.
	Clock matrix.Clock
	// Clean is the clean-up of a marked scratch team (HostDrill's).
	Clean drill.Cleaner

	port     int
	main     string
	kept     string
	tokens   map[string]string
	password string
}

// DrillInput is the scratch team's declaration: present on the drill port, loopback only (no
// network, no LXC member, no address, so the terminator listens nowhere and the url's host is
// one no member resolves), its one human the drill's, and a trust section naming nothing real.
func DrillInput(team paths.TeamID, port int) (declare.Input, error) {
	raw, err := json.Marshal(map[string]any{
		"team": map[string]any{
			"team": team.String(), "state": "present", "port": port, "description": "The restore drill's scratch team.",
			"networks": []any{}, "url": "https://" + team.String() + ".invalid:" + strconv.Itoa(port), "humans": []any{drillHuman},
			"trust": map[string]any{
				"kind": "github", "org": "restore-drill",
				"repos": []any{map[string]any{"repo": "restore-drill/scratch", "branches": []any{"main"}}},
			},
		},
		"lxc_members": []any{},
	})
	if err != nil {
		return declare.Input{}, err
	}
	return declare.DecodeInput(raw)
}

// Cleaner implements drill.Machine.
func (r *DrillRig) Cleaner() drill.Cleaner { return r.Clean }

// Lock implements drill.Machine: the one drill-wide lock, one try.
func (r *DrillRig) Lock(ctx context.Context) (func() error, error) {
	id, err := paths.ParseTeamID(drillLockID)
	if err != nil {
		return nil, err
	}
	d := r.Sys.Host
	l, err := teamlock.Take(ctx, d.LockDir, id, drillVerb, 0, d.Clock, teamlock.Unguarded)
	if err != nil {
		return nil, err
	}
	return l.Release, nil
}

// Apply implements drill.Machine: the scratch input approved with the owner group's gid, then
// team apply's host.Apply with the child's bootstrap.
func (r *DrillRig) Apply(ctx context.Context, team paths.TeamID, port int) error {
	in, err := DrillInput(team, port)
	if err != nil {
		return err
	}
	sys := r.Sys
	gid, err := sys.OwnerGroup()
	if err != nil {
		return fmt.Errorf("the owner group: %w", err)
	}
	fa := declare.FileApprovals{Path: func(id paths.TeamID) string { return sys.Host.Root + paths.ApprovedFile(id) }, GID: gid}
	if _, err = approve(sys.Host.Root, fa, in, nil); err != nil {
		return fmt.Errorf("the scratch team's approval: %w", err)
	}
	d := sys.Host
	if d.Pin, err = sys.Pin(); err != nil {
		return fmt.Errorf("the server pin: %w", err)
	}
	ctl, err := sys.CtlIDs()
	if err != nil {
		return err
	}
	d.CtlUID, d.CtlGID, d.LockWait, d.Bootstrap = ctl.UID, ctl.GID, host.PlayWait, Bootstrapper(sys)
	if _, err = host.Apply(ctx, d, in); err != nil {
		return err
	}
	r.port = port
	return nil
}

// Seed implements drill.Machine: two token members and the human's password through the child,
// then two messages and each token member's floor marker on the team's server.
func (r *DrillRig) Seed(ctx context.Context, team paths.TeamID) (drill.Seed, error) {
	var s drill.Seed
	r.tokens = map[string]string{}
	err := r.locked(ctx, team, r.Sys.Host.Guard, func(*teamlock.Lock) error {
		for seat := 1; seat <= 2; seat++ {
			c, err := r.memberOp(ctx, team, apply.OpMemberAdd, proto.M(keyPrefix, proto.Str(drillPrefix)),
				proto.M(keySeat, proto.Int(int64(seat))), proto.M(keyKind, proto.Str(drillKind)),
				proto.M(keyHumanText, proto.Bool(false)), proto.M(keyCredential, proto.Str(apply.CredToken)))
			if err != nil {
				return err
			}
			if seat == 1 {
				s.Kept, r.kept = c, c.Handle
			} else {
				s.Reissued = c
			}
		}
		pw, err := r.humanPassword(ctx, team)
		s.Human = drill.Credential{Handle: drillHuman, Secret: pw}
		return err
	})
	if err != nil {
		return drill.Seed{}, err
	}
	for _, h := range r.handles() {
		if err = r.asMember(ctx, h, func(c *matrix.Client, _ string) error {
			_, jerr := c.Join(ctx, r.main)
			return jerr
		}); err != nil {
			return drill.Seed{}, fmt.Errorf("%s joining the main room, as its seat would: %w", h, err)
		}
	}
	if s.Events, err = r.post(ctx); err != nil {
		return drill.Seed{}, err
	}
	return s, r.markFloors(ctx, team)
}

// Change implements drill.Machine: s.Reissued reissued and the human's password reset.
func (r *DrillRig) Change(ctx context.Context, team paths.TeamID, s drill.Seed) error {
	return r.locked(ctx, team, r.Sys.Host.Guard, func(*teamlock.Lock) error {
		if _, err := r.memberOp(ctx, team, apply.OpMemberReissue, proto.M(keyHandle, proto.Str(s.Reissued.Handle)),
			proto.M(keyCredential, proto.Str(apply.CredToken))); err != nil {
			return err
		}
		_, err := r.humanPassword(ctx, team)
		return err
	})
}

// Backup implements drill.Machine.
func (r *DrillRig) Backup(ctx context.Context, team paths.TeamID) (int64, error) {
	var n int64
	err := r.locked(ctx, team, r.Sys.Host.Guard, func(l *teamlock.Lock) error {
		var err error
		n, err = r.TakeBackup(ctx, l)
		return err
	})
	return n, err
}

// Restore implements drill.Machine: the engine's fresh restore of backup n under the team lock,
// h as its OnStaged and AfterSwap.
func (r *DrillRig) Restore(ctx context.Context, team paths.TeamID, n int64, h drill.Hooks) error {
	d := r.Engine
	if h.OnStaged != nil {
		d.OnStaged = func(ctx context.Context, _ paths.TeamID) error { return h.OnStaged(ctx) }
	}
	if h.AfterSwap != nil {
		d.AfterSwap = func(ctx context.Context, _ paths.TeamID) error { return h.AfterSwap(ctx) }
	}
	return r.locked(ctx, team, restore.Guard(r.Sys.Host.Root), func(l *teamlock.Lock) error {
		_, err := r.RunRestore(ctx, d, l, restore.Request{Team: team, Backup: n})
		return err
	})
}

// Discard implements drill.Machine.
func (r *DrillRig) Discard(ctx context.Context, team paths.TeamID) error {
	return r.locked(ctx, team, restore.Recorded(r.Sys.Host.Root), func(*teamlock.Lock) error {
		_, err := r.RunDiscard(ctx, r.Engine, team)
		return err
	})
}

// locked runs fn under the scratch team's lock, guarded as a member verb is.
func (r *DrillRig) locked(ctx context.Context, team paths.TeamID, guard teamlock.Guard, fn func(*teamlock.Lock) error) error {
	d := r.Sys.Host
	l, err := teamlock.Take(ctx, d.LockDir, team, drillVerb, SeatWait, d.Clock, guard)
	if err != nil {
		return err
	}
	return errors.Join(fn(l), l.Release())
}

// memberOp is a member verb's work on the scratch team; its issued token becomes the member's
// current one.
func (r *DrillRig) memberOp(ctx context.Context, team paths.TeamID, op string, args ...proto.Member) (drill.Credential, error) {
	w, err := memberWork(ctx, r.Sys, op, team, args, ReachURL)
	if err != nil {
		return drill.Credential{}, err
	}
	a := w.answer
	if a == nil || a.Credential == nil || a.MainRoom == "" {
		return drill.Credential{}, childLie("no credential", "the drill cannot go on")
	}
	r.main, r.tokens[a.Handle] = a.MainRoom, a.Credential.Secret
	return drill.Credential{Handle: a.Handle, Secret: a.Credential.Secret}, nil
}

// humanPassword is human.password on the drill's human: its new password, kept as the current
// one.
func (r *DrillRig) humanPassword(ctx context.Context, team paths.TeamID) (string, error) {
	_, tf, err := readTeamFile(r.Sys.Host.Root, team)
	if err != nil {
		return "", err
	}
	a, err := ask(ctx, r.Sys, apply.OpHumanPassword, team, proto.Obj(proto.M(keyTeamFile, tf), proto.M(keyName, proto.Str(drillHuman))))
	if err != nil {
		return "", err
	}
	if lie := humanAsked(apply.OpHumanPassword, *a); lie != nil {
		return "", lie
	}
	r.password = a.Human.Password
	return r.password, nil
}

// post is two messages in the main room as the kept member: their event ids.
func (r *DrillRig) post(ctx context.Context) ([]string, error) {
	c, err := r.member(r.tokens[r.kept])
	if err != nil {
		return nil, err
	}
	defer c.Close()
	var events []string
	for i := 1; i <= 2; i++ {
		txn, err := matrix.NewTxnID()
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(map[string]string{"msgtype": "m.text", "body": "restore drill message " + strconv.Itoa(i)})
		if err != nil {
			return nil, err
		}
		id, err := c.Send(ctx, r.main, "m.room.message", txn, body)
		if err != nil {
			return nil, fmt.Errorf("the drill's message %d: %w", i, err)
		}
		events = append(events, id)
	}
	return events, nil
}

// markFloors puts each token member's floor marker in the main room, as a seat does (SY10),
// with the live restore epoch.
func (r *DrillRig) markFloors(ctx context.Context, team paths.TeamID) error {
	epoch, err := r.LiveEpoch(ctx, team)
	if err != nil {
		return err
	}
	for i, h := range r.handles() {
		marker := fmt.Sprintf(`{"v":1,"floor_ms":%d,"restore_epoch":%s}`, r.Sys.Host.Clock.Now().UnixMilli()+int64(i), epoch)
		if err := r.asMember(ctx, h, func(c *matrix.Client, user string) error {
			return c.PutRoomAccountData(ctx, user, r.main, paths.FloorAccountData, json.RawMessage(marker))
		}); err != nil {
			return fmt.Errorf("the floor marker of %s: %w", h, err)
		}
	}
	return nil
}

// member is a member client of the team's server with token.
func (r *DrillRig) member(token string) (*matrix.Client, error) {
	return matrix.NewMember(matrix.Config{BaseURL: r.Base(r.port), Token: token, Clock: r.Clock})
}

// asMember runs fn as handle with its current token, given its user id.
func (r *DrillRig) asMember(ctx context.Context, handle string, fn func(c *matrix.Client, user string) error) error {
	c, err := r.member(r.tokens[handle])
	if err != nil {
		return err
	}
	defer c.Close()
	me, err := c.Whoami(ctx)
	if err != nil {
		return err
	}
	return fn(c, me.UserID)
}

// handles are the token members, the kept one first.
func (r *DrillRig) handles() []string {
	out := []string{r.kept}
	for _, h := range slices.Sorted(maps.Keys(r.tokens)) {
		if h != r.kept {
			out = append(out, h)
		}
	}
	return out
}

// teamFile is the applied team.json, which the child's checks are given.
func (r *DrillRig) teamFile(team paths.TeamID) ([]byte, error) {
	raw, found, err := state.ReadFile(r.Sys.Host.Root+paths.TeamDeclaration(team), maxTeamFile)
	if err == nil && !found {
		err = fmt.Errorf("team %s is not applied", team)
	}
	return raw, err
}
