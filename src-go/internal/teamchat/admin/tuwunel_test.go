//go:build tuwunel

package admin

// RT2-RT4 against a real Tuwunel (through `matrix/tuwunelhs`): `go test -tags tuwunel` with
// FAKEHS_TUWUNEL_BIN naming the binary. The world runs the real verbs as root over host fakes,
// with alpha's homeserver a real server L on the team's database and backup paths; backup N is
// Tuwunel's own, taken by backup.Take through a SIGUSR2. The staged server S is the same binary
// started with `--restore-backup N` on db.restore-N, and the child secures it in process.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/backup"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/hostnet"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/tuwunelhs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/restore"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	tuwunelTestLimit = 4 * time.Minute
	floorType        = "teamchat.floor"
	floorAtN         = `{"since_ms":1}`
	floorAfterN      = `{"since_ms":2}`
	testSelfBinary   = "/usr/local/bin/teamchat-admin"
	whomMain         = "main"
)

// liveUnits is the homeserver unit as systemd would report the running server L, and KillUnit's
// main-process signal sent to it.
type liveUnits struct{ hs tuwunelhs.Server }

func (u liveUnits) State(context.Context, string) (backup.UnitState, error) {
	return backup.UnitState{ActiveState: "active", InvocationID: "0123456789abcdef0123456789abcdef", Result: "success"}, nil
}

func (u liveUnits) Kill(_ context.Context, _, whom string, signal int32) error {
	if whom != whomMain {
		return fmt.Errorf("signal sent to %q, not the main process", whom)
	}
	return u.hs.Signal(syscall.Signal(signal))
}

// liveReady probes L on its own port: team.json names the team's port, which a test server
// cannot take.
type liveReady struct{ port int }

func (r liveReady) ServerVersion(ctx context.Context, _ int) error {
	return hostnet.Readiness{}.ServerVersion(ctx, r.port)
}

// options are a server's on alpha's database path db, with the team's backup path.
func options(root string, team paths.TeamID, db string, args ...string) tuwunelhs.Options {
	return tuwunelhs.Options{
		DatabasePath: db,
		Extra: fmt.Sprintf("database_backup_path = %q\ndatabase_backups_to_keep = 7\nadmin_signal_execute = [\"server backup-database\"]\n",
			root+paths.HomeserverPrivateBackups(team)),
		Args: args,
	}
}

// realWorld is a world whose alpha homeserver is the real server L: its admin registered by the
// key, the two members' accounts made, and the admin token where the bootstrap keeps it.
type realWorld struct {
	*world

	live  tuwunelhs.Server
	token string
}

func newRealWorld(ctx context.Context, t *testing.T) *realWorld {
	t.Helper()
	w, team := bareWorld(t), alpha(t)
	if err := os.MkdirAll(w.root+paths.HomeserverPrivateState(team), 0o700); err != nil {
		t.Fatal(err)
	}
	hs := tuwunelhs.StartWith(ctx, t, server, options(w.root, team, w.root+paths.HomeserverPrivateDatabase(team)))
	w.base = hs.Base
	// L runs, so the unit is active: apply's backup step finds the database served.
	w.units[paths.ServerUnit(team)] = &host.UnitState{Active: "active"}
	anon, err := matrix.NewAdmin(matrix.Config{BaseURL: hs.Base, Clock: tuwunelhs.Clock{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(anon.Close)
	sess, err := anon.Register(ctx, hs.RegKey, matrix.Registration{Username: proto.AdminLocalpart, Password: tuwunelhs.RandomKey(t), Admin: true})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	token := sess.AccessToken.Reveal()
	if w.admin, err = anon.WithToken(token); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{coord, other} {
		if _, err := w.admin.CreateAccount(ctx, proto.UserID(h, server), tuwunelhs.RandomKey(t), ""); err != nil {
			t.Fatalf("CreateAccount %s: %v", h, err)
		}
	}
	return &realWorld{world: w, live: hs, token: token}
}

// ok runs teamchat-admin as root over the world under ctx, and wants exit 0.
func (w *realWorld) ok(ctx context.Context, t *testing.T, stdin string, args ...string) outcome {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(ctx, Env{
		Args: args, Stdin: strings.NewReader(stdin), Stdout: &stdout, Stderr: &stderr,
		Proc: rootProc(), Digest: digest, Sys: w.sys(),
	})
	if code != term.ExitOK {
		t.Fatalf("%v = %d\nstdout %s\nstderr %s", args, code, stdout.String(), stderr.String())
	}
	return outcome{code, stdout.String(), stderr.String()}
}

// writeToken is the admin token where the bootstrap writes it on a host: the backup's source.
func (w *realWorld) writeToken(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(w.root+paths.CtlState(alpha(t))+"/admin.token", []byte(w.token), 0o600); err != nil {
		t.Fatal(err)
	}
}

// takeBackup is backup.Take against L: Tuwunel's next backup, and its tar.
func (w *realWorld) takeBackup(ctx context.Context, t *testing.T) backup.Contents {
	t.Helper()
	team, clock, uid := alpha(t), tuwunelhs.Clock{}, selfUID(t)
	d := backup.Deps{Root: w.root, Units: liveUnits{w.live}, Ready: liveReady{w.live.Port}, Clock: clock, CtlUID: uid, StateOwnerUID: uid}
	lock, err := teamlock.Take(ctx, t.TempDir()+"/run", team, "backup", 0, clock, teamlock.Unguarded)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if rerr := lock.Release(); rerr != nil {
			t.Error(rerr)
		}
	}()
	res, err := backup.Take(ctx, d, lock, nil)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	c, err := backup.ReadTar(d, team, res.Backup)
	if err != nil {
		t.Fatalf("tar %d: %v", res.Backup, err)
	}
	return c
}

// stage starts S with --restore-backup n on a fresh db.restore-<n>, with a file planted there
// first, which the restore must wipe (RS2's hazard).
func (w *realWorld) stage(ctx context.Context, t *testing.T, n int64) tuwunelhs.Server {
	t.Helper()
	team := alpha(t)
	path := w.root + paths.HomeserverPrivateRestoreDatabase(team, n)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+"/SENTINEL", []byte("planted"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := tuwunelhs.StartWith(ctx, t, server, options(w.root, team, path, "--restore-backup", strconv.FormatInt(n, 10)))
	if _, err := os.Stat(path + "/SENTINEL"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the planted file outlived the restore: %v", err)
	}
	return s
}

// secure merges the current roster with tar's and runs restore.secure through the child
// against the server s; the merged roster comes back.
func (w *realWorld) secure(ctx context.Context, t *testing.T, s tuwunelhs.Server, tar backup.Contents, offline bool) restore.Merged {
	t.Helper()
	team := alpha(t)
	m, err := restore.Merge(w.roster(t), tar.Roster, restore.MergeOptions{Team: team, Offline: offline})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	argv, err := restore.SecureArgv(team, testSelfBinary)
	if err != nil {
		t.Fatal(err)
	}
	c := RestoreChild{Live: inProcessChild{w.world}, Unit: func([]string) Child { return inProcessChild{w.world} }}
	if _, err = c.Secure(ctx, team, argv, restore.SecureArgs{
		TeamFile: w.teamFile(t), Port: s.Port, Token: tar.Token, Rooms: tar.Rooms, Roster: m.Roster,
		Retire: m.Lists.Retire, Revoke: m.Lists.Revoke, RevokeHumans: m.Lists.RevokeHumans, Lock: m.Lists.Lock,
	}); err != nil {
		t.Fatalf("secure: %v", err)
	}
	return m
}

// adminOn is the admin client of server s with token.
func adminOn(t *testing.T, s tuwunelhs.Server, token string) *matrix.Admin {
	t.Helper()
	a, err := matrix.NewAdmin(matrix.Config{BaseURL: s.Base, Token: token, Clock: tuwunelhs.Clock{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a
}

// memberOn is a member client of server s with token.
func memberOn(t *testing.T, s tuwunelhs.Server, token string) *matrix.Client {
	t.Helper()
	c, err := matrix.NewMember(matrix.Config{BaseURL: s.Base, PlainHTTPHosts: []string{tuwunelhs.Loopback}, Token: token, Clock: tuwunelhs.Clock{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

// refused is a login or a token the server said no to: an answer, not a server lost or a rate
// limit, which would pass for a refusal.
func refused(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: accepted", what)
		return
	}
	if errors.Is(err, matrix.ErrUnreachable) || errors.Is(err, matrix.ErrRateLimited) {
		t.Errorf("%s: %v, not a refusal", what, err)
	}
}

// rosterOn is the roster event main serves on s, read with token.
func rosterOn(ctx context.Context, t *testing.T, s tuwunelhs.Server, token string, rooms proto.RoomIDs) proto.Roster {
	t.Helper()
	raw, err := adminOn(t, s, token).GetState(ctx, rooms.Main, proto.RosterType, "")
	if err != nil {
		t.Fatalf("the roster event: %v", err)
	}
	r, err := proto.DecodeRoster(parsed(t, raw), server, "alpha", rooms.Main)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// scene is RT2's history around backup N, on L.
type scene struct {
	tar                    backup.Contents
	seat3Old, seat3New     string
	seatToken, tokenMember string
	memberToken            string
	aliceAtN               string
}

// beforeAndAfterN applies alpha, adds the third seat by password and a member by token, gives
// alice a known password, signs the seat in as a pool seat, and puts the token member's floor;
// takes N; then reissues the seat, revokes alice after N with a new password, and moves the
// floor.
func (w *realWorld) beforeAndAfterN(ctx context.Context, t *testing.T) scene {
	t.Helper()
	team := alpha(t)
	var sc scene
	in := encodeDoc(t, alphaDoc())
	w.ok(ctx, t, in, "team", "approve", "--team", "alpha")
	w.ok(ctx, t, in, "team", "apply")
	w.writeToken(t)
	sc.seat3Old = decodeDoc(t, w.ok(ctx, t, "", addArgs("3", "password")...)).Bundle.Secret
	tm := decodeDoc(t, w.ok(ctx, t, "", addArgs("next", "token")...)).Bundle
	sc.tokenMember, sc.memberToken = tm.Handle, tm.Secret
	sc.aliceAtN = tuwunelhs.RandomKey(t)
	if err := w.admin.ResetPassword(ctx, proto.UserID("alice", server), sc.aliceAtN); err != nil {
		t.Fatal(err)
	}
	seat, err := w.admin.LoginDevice(ctx, sampleHandle, sc.seat3Old, "TCSEAT")
	if err != nil {
		t.Fatalf("the pool seat's login: %v", err)
	}
	sc.seatToken = seat.AccessToken.Reveal()
	main := w.rooms(t).Main
	floor := func(content string) {
		if err := memberOn(t, w.live, sc.memberToken).PutRoomAccountData(ctx, proto.UserID(sc.tokenMember, server), main, floorType, json.RawMessage(content)); err != nil {
			t.Fatalf("floor: %v", err)
		}
	}
	floor(floorAtN)

	sc.tar = w.takeBackup(ctx, t)

	sc.seat3New = decodeDoc(t, w.ok(ctx, t, "", on("reissue", "--credential", "password")...)).Bundle.Secret
	f := w.roster(t)
	h, ok := f.Humans["alice"]
	if !ok {
		t.Fatalf("no human alice in %v", f.Humans)
	}
	h.RevokedSeq = f.Seq
	f.Humans["alice"] = h
	if err := os.WriteFile(w.root+paths.RosterFile(team), f.Encode(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := w.admin.ResetPassword(ctx, proto.UserID("alice", server), tuwunelhs.RandomKey(t)); err != nil {
		t.Fatal(err)
	}
	floor(floorAfterN)
	if f.Seq <= sc.tar.Roster.Seq || f.Members[sampleHandle].CredSeq <= sc.tar.Roster.Seq {
		t.Fatalf("fixture: seq %d, the seat's cred seq %d, not after N's %d", f.Seq, f.Members[sampleHandle].CredSeq, sc.tar.Roster.Seq)
	}
	return sc
}

// wantSecured is RT2's list on the secured server s: the seat's passwords, its pool token from
// N and alice's N-era password refused, the floor as N holds it, the raised epoch served.
func (w *realWorld) wantSecured(ctx context.Context, t *testing.T, s tuwunelhs.Server, sc scene, m restore.Merged) {
	t.Helper()
	a := adminOn(t, s, sc.tar.Token)
	_, err := a.LoginDevice(ctx, sampleHandle, sc.seat3Old, "TCTEST")
	refused(t, "the reissued seat's N-era password", err)
	_, err = a.LoginDevice(ctx, sampleHandle, sc.seat3New, "TCTEST")
	refused(t, "the reissued seat's new password", err)
	_, err = a.LoginDevice(ctx, "alice", sc.aliceAtN, "TCTEST")
	refused(t, "alice's N-era password", err)
	w.wantServed(ctx, t, s, sc, m)
	content, found, err := memberOn(t, s, sc.memberToken).RoomAccountData(ctx, proto.UserID(sc.tokenMember, server), sc.tar.Rooms.Main, floorType)
	if err != nil || !found || string(content) != floorAtN {
		t.Errorf("the floor %s %v %v, want N's %s", content, found, err, floorAtN)
	}
}

// wantServed is what a restarted server must still give (RT3): the merged epoch and rev in the
// first state read, and the pool seat's token from N refused.
func (w *realWorld) wantServed(ctx context.Context, t *testing.T, s tuwunelhs.Server, sc scene, m restore.Merged) {
	t.Helper()
	got := rosterOn(ctx, t, s, sc.tar.Token, sc.tar.Rooms)
	if got.RestoreEpoch != m.Roster.RestoreEpoch || got.Rev != m.Roster.Rev || got.RestoreEpoch <= sc.tar.Roster.RestoreEpoch {
		t.Errorf("served epoch %d rev %d, want the merged %d at rev %d", got.RestoreEpoch, got.Rev, m.Roster.RestoreEpoch, m.Roster.Rev)
	}
	_, err := memberOn(t, s, sc.seatToken).Whoami(ctx)
	refused(t, "the pool seat's token from N", err)
}

// RT2: --restore-backup N serves the restored database in a fresh path, wiping a plant, while
// L is untouched; restore.secure leaves RT2's list.
func TestTuwunelRestoreSecureRT2(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), tuwunelTestLimit)
	defer cancel()
	w := newRealWorld(ctx, t)
	sc := w.beforeAndAfterN(ctx, t)
	s := w.stage(ctx, t, sc.tar.Manifest.Backup)
	m := w.secure(ctx, t, s, sc.tar, false)
	if !slices.Contains(m.Lists.RevokeHumans, "alice") {
		t.Errorf("the merge does not revoke alice: %+v", m.Lists)
	}
	w.wantSecured(ctx, t, s, sc, m)
	if _, err := w.admin.LoginDevice(ctx, sampleHandle, sc.seat3New, "TCTEST"); err != nil {
		t.Errorf("L no longer takes the seat's new password: %v", err)
	}
	if got := rosterOn(ctx, t, w.live, w.token, sc.tar.Rooms); got.RestoreEpoch != sc.tar.Roster.RestoreEpoch {
		t.Errorf("L serves epoch %d after the secure of S", got.RestoreEpoch)
	}
}

// RT3: the secured epoch survives a stop by SIGTERM and, in a run of its own, one by SIGKILL,
// each sent to the server the secure has just written to; the plain start on the same path
// serves it first, and the revoked token stays refused.
func TestTuwunelEpochSurvivesTheStopRT3(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		t.Run(sig.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), tuwunelTestLimit)
			defer cancel()
			w := newRealWorld(ctx, t)
			sc := w.beforeAndAfterN(ctx, t)
			n := sc.tar.Manifest.Backup
			s := w.stage(ctx, t, n)
			m := w.secure(ctx, t, s, sc.tar, false)
			if err := s.Stop(sig); err != nil {
				t.Fatalf("stop by %v: %v", sig, err)
			}
			s = tuwunelhs.StartWith(ctx, t, server, options(w.root, alpha(t), w.root+paths.HomeserverPrivateRestoreDatabase(alpha(t), n)))
			w.wantServed(ctx, t, s, sc, m)
		})
	}
}

// RT4: L's database cannot open (CURRENT truncated); N is restored into a fresh path, secured
// offline, renamed into place by RS16's renames, and the server on the team's path serves the
// restored database with the raised epoch.
func TestTuwunelOfflineRestoreRT4(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), tuwunelTestLimit)
	defer cancel()
	w := newRealWorld(ctx, t)
	sc := w.beforeAndAfterN(ctx, t)
	team, n := alpha(t), sc.tar.Manifest.Backup
	if err := w.live.Stop(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	db := w.root + paths.HomeserverPrivateDatabase(team)
	if err := os.Truncate(db+"/CURRENT", 0); err != nil {
		t.Fatal(err)
	}
	s := w.stage(ctx, t, n)
	m := w.secure(ctx, t, s, sc.tar, true)
	if err := s.Stop(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := restore.Renames(restore.OS{Root: w.root, StateOwnerUID: selfUID(t)}, team, n, restore.Swapped); err != nil {
		t.Fatalf("renames: %v", err)
	}
	if _, err := os.Stat(w.root + paths.HomeserverPrivateRestoreDatabase(team, n)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("db.restore-%d is still there: %v", n, err)
	}
	served := tuwunelhs.StartWith(ctx, t, server, options(w.root, team, db))
	w.wantSecured(ctx, t, served, sc, m)
}
