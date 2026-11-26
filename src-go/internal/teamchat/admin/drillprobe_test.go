package admin

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
)

// probeRig is the rig on the drill port with a staged copy brought up by a recorded fake: the
// probe's effects are recorded with whether they ran while the copy was up.
type probeRig struct {
	*rigWorld

	up      bool
	effects []string
	epoch   int64
	answers map[netip.AddrPort]bool
	dialErr error
}

func newProbeRig(t *testing.T) *probeRig {
	t.Helper()
	p := &probeRig{rigWorld: newRigWorld(t), epoch: 9, answers: map[netip.AddrPort]bool{}}
	p.rig.port = drillPort
	p.rig.StagedUp = func(ctx context.Context, _ restore.Deps, team paths.TeamID, fn func(context.Context, func(context.Context) (restore.Epoch, error)) error) error {
		p.effects = append(p.effects, "up "+team.String())
		p.up = true
		err := fn(ctx, func(context.Context) (restore.Epoch, error) {
			p.effects = append(p.effects, p.during("check"))
			return restore.Epoch{RestoreEpoch: p.epoch, Rev: 1}, nil
		})
		p.up = false
		p.effects = append(p.effects, "down")
		return err
	}
	p.rig.Dial = func(_ context.Context, addr netip.AddrPort) (bool, error) {
		p.effects = append(p.effects, p.during("dial "+addr.String()))
		return p.answers[addr], p.dialErr
	}
	return p
}

// during is effect marked with whether the staged copy was up when it ran.
func (p *probeRig) during(effect string) string {
	if p.up {
		return effect
	}
	return effect + " (copy not up)"
}

// Owners is the rig's walk of the team's state directory.
func TestDrillProbeOwnersIsTheStateWalk(t *testing.T) {
	p := newProbeRig(t)
	p.rig.OwnersOf = func(team paths.TeamID) (map[string]uint32, error) {
		return map[string]uint32{team.String() + "/db": 61000}, nil
	}
	got, err := p.rig.Owners(t.Context(), scratch(t))
	if err != nil || got["rd-0123456789ab/db"] != 61000 || len(got) != 1 {
		t.Fatalf("Owners = %v, %v", got, err)
	}
}

// JoinedEpoch is restore.check through the joined unit, run while the staged copy is up.
func TestDrillProbeJoinedEpochChecksInsideTheStagedCopy(t *testing.T) {
	p := newProbeRig(t)
	got, err := p.rig.JoinedEpoch(t.Context(), scratch(t))
	if err != nil || got != "9" {
		t.Fatalf("JoinedEpoch = %q, %v", got, err)
	}
	if want := []string{"up rd-0123456789ab", "check", "down"}; !slices.Equal(p.effects, want) {
		t.Errorf("effects = %q, want %q", p.effects, want)
	}
}

// A staged copy that cannot be brought up is an error, never an epoch.
func TestDrillProbeJoinedEpochFailsWithTheStagedCopy(t *testing.T) {
	p := newProbeRig(t)
	p.rig.StagedUp = func(context.Context, restore.Deps, paths.TeamID, func(context.Context, func(context.Context) (restore.Epoch, error)) error) error {
		return errors.New("nothing is staged")
	}
	got, err := p.rig.JoinedEpoch(t.Context(), scratch(t))
	if err == nil || got != "" {
		t.Fatalf("JoinedEpoch = %q, %v", got, err)
	}
}

// MemberReach dials the team's port on every address of the host but loopback, while the
// staged copy is up; nothing answering is false.
func TestDrillProbeMemberReachDialsEveryMemberAddressWithTheCopyUp(t *testing.T) {
	p := newProbeRig(t)
	reached, err := p.rig.MemberReach(t.Context(), scratch(t))
	if err != nil || reached {
		t.Fatalf("MemberReach = %v, %v", reached, err)
	}
	if want := []string{"up rd-0123456789ab", "dial 192.0.2.10:8470", "down"}; !slices.Equal(p.effects, want) {
		t.Errorf("effects = %q, want %q", p.effects, want)
	}
}

// An address that answers is a reach.
func TestDrillProbeMemberReachAnAnswerIsAReach(t *testing.T) {
	p := newProbeRig(t)
	p.answers[netip.MustParseAddrPort("192.0.2.10:8470")] = true
	reached, err := p.rig.MemberReach(t.Context(), scratch(t))
	if err != nil || !reached {
		t.Fatalf("MemberReach = %v, %v", reached, err)
	}
}

// A dial that could not judge is an error, never "not reached".
func TestDrillProbeMemberReachADialFailureIsAnError(t *testing.T) {
	p := newProbeRig(t)
	p.dialErr = errors.New("the dial could not judge")
	reached, err := p.rig.MemberReach(t.Context(), scratch(t))
	if err == nil || reached || !strings.Contains(err.Error(), "could not judge") {
		t.Fatalf("MemberReach = %v, %v", reached, err)
	}
}

// loopbackOnly is the scratch network with no address but loopback.
type loopbackOnly struct{ scratchNetwork }

func (loopbackOnly) Interfaces() ([]host.Interface, error) {
	return []host.Interface{{Name: "lo", Addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("::1")}}}, nil
}

// A host with no address but loopback cannot show a member's reach: refused before the copy
// is brought up, so the check cannot pass by probing nothing.
func TestDrillProbeMemberReachRefusesALoopbackOnlyHost(t *testing.T) {
	p := newProbeRig(t)
	p.rig.Sys.Host.Network = loopbackOnly{scratchNetwork{p.world}}
	reached, err := p.rig.MemberReach(t.Context(), scratch(t))
	if err == nil || reached || !strings.Contains(err.Error(), "loopback") || len(p.effects) != 0 {
		t.Fatalf("MemberReach = %v, %v, effects %q", reached, err, p.effects)
	}
}

// kinds is restore.FS answering Kind from a table, absent otherwise.
type kinds struct {
	restore.FS

	of map[string]restore.Kind
}

func (k kinds) Kind(_ paths.TeamID, name string) (restore.Kind, error) { return k.of[name], nil }

// recorded writes team's restore record at step for backup n.
func recorded(t *testing.T, root string, team paths.TeamID, step restore.Step, n int64) {
	t.Helper()
	path := root + paths.RestoreRecord(team)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	rec := restore.Record{Team: team, Backup: n, Step: step, Roster: apply.NewRosterFile(team)}
	if err := os.WriteFile(path, restore.EncodeRecord(rec), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Staged is the record at staged with backup N's copy kept as a directory; any other step, no
// record, or the copy gone is not staged.
func TestDrillProbeStagedIsTheRecordAndTheKeptCopy(t *testing.T) {
	id := scratch(t)
	copyName := filepath.Base(paths.HomeserverPrivateRestoreDatabase(id, 3))
	for _, tc := range []struct {
		name string
		step restore.Step
		copy restore.Kind
		want bool
	}{
		{"staged and kept", restore.StepStaged, restore.KindDirectory, true},
		{"staged, the copy gone", restore.StepStaged, restore.KindAbsent, false},
		{"staged, the copy not a directory", restore.StepStaged, restore.KindFile, false},
		{"at another step", restore.StepSwap, restore.KindDirectory, false},
		{"no record", "", restore.KindDirectory, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newProbeRig(t)
			p.rig.Engine.FS = kinds{of: map[string]restore.Kind{copyName: tc.copy}}
			if tc.step != "" {
				recorded(t, p.root, id, tc.step, 3)
			}
			got, err := p.rig.Staged(t.Context(), id)
			if err != nil || got != tc.want {
				t.Fatalf("Staged = %v, %v, want %v", got, err, tc.want)
			}
		})
	}
}

// A record that cannot be decoded is an error, never "not staged".
func TestDrillProbeStagedAnUndecodableRecordIsAnError(t *testing.T) {
	p := newProbeRig(t)
	id := scratch(t)
	recorded(t, p.root, id, restore.StepStaged, 3)
	if err := os.WriteFile(p.root+paths.RestoreRecord(id), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := p.rig.Staged(t.Context(), id)
	if err == nil || got {
		t.Fatalf("Staged = %v, %v", got, err)
	}
}
