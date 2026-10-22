//go:build tuwunel

package backup

// RT1 against a real Tuwunel (through `matrix/tuwunelhs`): `go test -tags tuwunel` with
// FAKEHS_TUWUNEL_BIN naming the binary. The server runs with the team's database and backup
// paths under a host tree and `admin_signal_execute`; Take's Kill is a SIGUSR2 to its process,
// the wait reads the meta number Tuwunel writes, and the tar pair validates.

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/hostnet"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/tuwunelhs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

const tuwunelTestLimit = 2 * time.Minute

// realUnits is the homeserver unit as systemd would report the running test server, and
// KillUnit's main-process signal sent to it.
type realUnits struct {
	hs    tuwunelhs.Server
	kills int
}

func (u *realUnits) State(context.Context, string) (UnitState, error) {
	return active(invocationA), nil
}

func (u *realUnits) Kill(_ context.Context, _, whom string, signal int32) error {
	if whom != whomMain {
		return fmt.Errorf("signal sent to %q, not the main process", whom)
	}
	u.kills++
	return u.hs.Signal(syscall.Signal(signal))
}

// realTree is a host tree whose team alpha has its sources and a real server on its database
// and backup paths: the Deps over it, the held lock, the units, and the team.json written.
func realTree(ctx context.Context, t *testing.T) (Deps, *teamlock.Lock, *realUnits, string) {
	t.Helper()
	root, team := treeRoot(t), alpha(t)
	if err := os.MkdirAll(root+paths.HomeserverPrivateState(team), 0o700); err != nil {
		t.Fatal(err)
	}
	hs := tuwunelhs.StartWith(ctx, t, paths.ServerName(team), tuwunelhs.Options{
		DatabasePath: root + paths.HomeserverPrivateDatabase(team),
		Extra: fmt.Sprintf("database_backup_path = %q\ndatabase_backups_to_keep = 7\nadmin_signal_execute = [\"server backup-database\"]\n",
			root+paths.HomeserverPrivateBackups(team)),
	})
	w := &world{t: t, root: root, team: team}
	src := sampleSources(t)
	teamJSON := strings.ReplaceAll(alphaTeamJSON, strconv.Itoa(alphaPort), strconv.Itoa(hs.Port))
	w.file(paths.AdminState(team), map[string][]byte{"team.json": []byte(teamJSON), "config.toml": src.Config})
	w.file(paths.CtlState(team), map[string][]byte{"roster.json": src.Roster, "rooms.json": src.Rooms, "admin.token": src.Token})
	units := &realUnits{hs: hs}
	clock := tuwunelhs.Clock{}
	d := Deps{Root: root, Units: units, Ready: hostnet.Readiness{}, Clock: clock, CtlUID: selfUID(t), StateOwnerUID: selfUID(t)}
	lock, err := teamlock.Take(ctx, t.TempDir()+"/run", team, "backup", 0, clock, teamlock.Unguarded)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if rerr := lock.Release(); rerr != nil {
			t.Error(rerr)
		}
	})
	return d, lock, units, teamJSON
}

// takeReal takes one backup and checks it is Tuwunel's backup want, and that its tar holds the
// sources.
func takeReal(ctx context.Context, t *testing.T, d Deps, lock *teamlock.Lock, units *realUnits, want int64, teamJSON string) {
	t.Helper()
	res, err := Take(ctx, d, lock, nil)
	if err != nil {
		t.Fatalf("backup %d: %v", want, err)
	}
	if res.Backup != want || res.TimeMS <= 0 || len(res.Pruned) != 0 || units.kills != int(want) {
		t.Fatalf("backup %d: %+v after %d signals", want, res, units.kills)
	}
	c, err := readTar(d, lock.Team(), res.Backup)
	if err != nil {
		t.Fatalf("tar %d: %v", want, err)
	}
	if c.Manifest.Backup != want || c.Manifest.TimeMS != res.TimeMS || string(c.TeamJSON) != teamJSON || c.Team.Port != units.hs.Port {
		t.Fatalf("tar %d manifest %+v", want, c.Manifest)
	}
}

// RT1: two backups, each Tuwunel's next number read by the wait, each tar valid by BK3 and
// holding the sources, and the listing showing both usable.
func TestTuwunelBackupRT1(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), tuwunelTestLimit)
	defer cancel()
	d, lock, units, teamJSON := realTree(ctx, t)
	team := lock.Team()
	for want := int64(1); want <= 2; want++ {
		takeReal(ctx, t, d, lock, units, want, teamJSON)
	}
	l, err := List(d, team)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Backups) != 2 || l.Backups[0].ID != 2 || !l.Backups[0].Usable || !l.Backups[1].Usable || l.LastRun == nil || l.LastRun.Backup != 2 {
		t.Fatalf("listing %+v", l)
	}
}
