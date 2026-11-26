package restore

import (
	"context"
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Staged brings a staged restore's secured copy up as it is, for the restore drill's probes
// (issue #324): the staging unit reopens the copy without restoring it (RU1), fn runs while it
// serves, and check is restore.check through the secure unit, which joins the staging unit's
// namespace (RC8). The unit is then stopped and unloaded, as the staging left it, so the
// restore can go on from staged.
func Staged(ctx context.Context, d Deps, team paths.TeamID, fn func(ctx context.Context, check func(context.Context) (Epoch, error)) error) error {
	rec, err := readRecord(d, team)
	if err != nil {
		return err
	}
	if rec.Step != StepStaged {
		return refusedf("the restore of team %s is at %s, not staged: there is no secured copy to bring up", team, rec.Step)
	}
	tar, err := readTar(d, team, rec.Backup)
	if err != nil {
		return err
	}
	v, err := ReadApplied(d, team)
	if err != nil {
		return err
	}
	e := &engine{d: d, rec: rec, tar: tar, teamFile: v.TeamFile, config: v.Config, decl: v.Team}
	version, err := d.Version(team)
	if err != nil {
		return err
	}
	reopen, err := StagingArgv(Staging{Team: team, Backup: rec.Backup, Version: version, Port: e.decl.Port, Sandbox: d.Sandbox(team)}, false)
	if err != nil {
		return err
	}
	secure, err := SecureArgv(team, d.Self)
	if err != nil {
		return err
	}
	if err = e.waitUnloaded(ctx); err != nil {
		return err
	}
	inv, err := e.startStaging(ctx, reopen)
	if err == nil {
		err = fn(ctx, func(ctx context.Context) (Epoch, error) {
			return d.Child.Check(ctx, team, secure, CheckArgs{TeamFile: e.teamFile, Port: e.decl.Port, Token: tar.Token})
		})
	}
	if err == nil {
		err = e.sameRun(ctx, inv)
	}
	if down := e.stopUnit(ctx, paths.RestoreUnit(team)); down != nil {
		return errors.Join(err, down)
	}
	return errors.Join(err, e.waitUnloaded(ctx))
}

// Owners is the owner of every entry beneath the team's state directory, by its name relative
// to it, each read by lstat beneath a descriptor of its directory, so a link is its own entry
// and never followed (the drill's H-C5 check that a staged copy changes no owner).
func (o OS) Owners(team paths.TeamID) (map[string]uint32, error) {
	out := map[string]uint32{}
	err := o.inTeam(team, func(dir *os.File) error { return ownersAt(dir, "", out) })
	return out, err
}

func ownersAt(dir *os.File, prefix string, out map[string]uint32) error {
	names, err := dir.Readdirnames(-1)
	if err != nil {
		return fmt.Errorf("cannot list %s: %w", dir.Name(), err)
	}
	for _, name := range names {
		var st unix.Stat_t
		if err := unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return fmt.Errorf("cannot stat %s/%s: %w", dir.Name(), name, err)
		}
		out[prefix+name] = st.Uid
		if kindOf(st.Mode) != KindDirectory {
			continue
		}
		sub, err := openBeneath(dir, name, unix.O_RDONLY|unix.O_DIRECTORY)
		if err != nil {
			return err
		}
		if err := errors.Join(ownersAt(sub, prefix+name+"/", out), sub.Close()); err != nil {
			return err
		}
	}
	return nil
}
