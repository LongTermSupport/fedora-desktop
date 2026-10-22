package admin

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/backup"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const backupInvocation = "0123456789abcdef0123456789abcdef"

// backupUnits is the homeserver units as a backup sees them: each active with one invocation
// unless inactive names it, and a Kill that makes Tuwunel's next backup (its meta file), or
// that loses the server when lose is set.
type backupUnits struct {
	w        *world
	inactive map[string]bool
	lose     bool
	lost     bool
	kills    []string
}

func (u *backupUnits) State(_ context.Context, unit string) (backup.UnitState, error) {
	switch {
	case u.inactive[unit]:
		return backup.UnitState{ActiveState: "inactive", Result: "success"}, nil
	case u.lost:
		return backup.UnitState{ActiveState: "active", InvocationID: strings.Repeat("f", 32), NRestarts: 1, Result: "success"}, nil
	}
	return backup.UnitState{ActiveState: "active", InvocationID: backupInvocation, Result: "success"}, nil
}

func (u *backupUnits) Kill(_ context.Context, unit, whom string, signal int32) error {
	u.kills = append(u.kills, unit+" "+whom+" "+strconv.Itoa(int(signal)))
	if u.lose {
		u.lost = true
		return nil
	}
	team, err := paths.ParseTeamID(strings.TrimSuffix(strings.TrimPrefix(unit, paths.Product+"-server@"), ".service"))
	if err != nil {
		return err
	}
	dir := u.w.root + paths.HomeserverPrivateBackupMeta(team)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	n := strconv.Itoa(len(u.kills))
	return os.WriteFile(dir+"/"+n, []byte("1792700000\n"), 0o600)
}

// selfUID is the test's own uid, the owner of every file the world makes.
func selfUID(t *testing.T) uint32 {
	t.Helper()
	info, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("no stat")
	}
	return st.Uid
}

// backupWorld is alpha applied with the admin token its bootstrap writes on a host (the
// in-process child keeps it in memory), and the backup seams over it.
func backupWorld(t *testing.T) (*world, *backupUnits) {
	t.Helper()
	w := newWorld(t)
	w.applied(t)
	if err := os.WriteFile(w.root+paths.CtlState(alpha(t))+"/admin.token", []byte("syt_YWRtaW4_abcdefghijklmnop_0123"), 0o600); err != nil {
		t.Fatal(err)
	}
	return w, &backupUnits{w: w, inactive: map[string]bool{}}
}

func (w *world) backupDeps(u *backupUnits) *backup.Deps {
	uid := selfUID(w.t)
	return &backup.Deps{Root: w.root, Units: u, Ready: w, Clock: w, CtlUID: uid, StateOwnerUID: uid}
}

// backupVerb runs teamchat-admin as root over the world with deps as its backups.
func (w *world) backupVerb(deps *backup.Deps, args ...string) outcome {
	w.t.Helper()
	sys := w.sys()
	sys.Backup = deps
	var stdout, stderr bytes.Buffer
	code := Run(w.t.Context(), Env{Args: args, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Proc: rootProc(), Digest: digest, Sys: sys})
	return outcome{code, stdout.String(), stderr.String()}
}

// BV1: the lock, Take, the status record refreshed, and the one document on stdout.
func TestBackupOneTeam(t *testing.T) {
	w, u := backupWorld(t)
	refreshed := len(w.status)
	out := w.backupVerb(w.backupDeps(u), "backup", "--team", "alpha")
	if out.code != term.ExitOK || out.stderr != "" {
		t.Fatalf("backup = %+v", out)
	}
	want := `{"v":1,"team":"alpha","backup":1,"time_ms":1792700000000,"pruned":[]}` + "\n"
	if out.stdout != want {
		t.Fatalf("stdout %q, want %q", out.stdout, want)
	}
	if len(u.kills) != 1 || len(w.status) != refreshed+1 || w.status[len(w.status)-1] {
		t.Fatalf("kills %v, status %v", u.kills, w.status)
	}
	w.lockIsFree(t)
	listed := w.backupVerb(w.backupDeps(u), "backups", "--team", "alpha")
	if listed.code != term.ExitOK || !strings.Contains(listed.stdout, `"backups":[{"id":1,"time_ms":1792700000000,"usable":true}]`) || !strings.Contains(listed.stdout, `"result":"ok"`) {
		t.Fatalf("backups = %+v", listed)
	}
}

// BV1, RS15: not running is 70, a server lost during the wait 69, and each still refreshes the
// status record and frees the lock.
func TestBackupFailuresByTheTable(t *testing.T) {
	cases := map[string]struct {
		set  func(*backupUnits)
		want term.ExitCode
	}{
		"not running": {func(u *backupUnits) { u.inactive[paths.ServerUnit(alpha(t))] = true }, ExitRefused},
		"lost":        {func(u *backupUnits) { u.lose = true }, ExitUnreachable},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w, u := backupWorld(t)
			c.set(u)
			refreshed := len(w.status)
			out := w.backupVerb(w.backupDeps(u), "backup", "--team", "alpha")
			if out.code != c.want || out.stdout != "" || !strings.Contains(out.stderr, "the backup of team alpha failed") {
				t.Fatalf("%+v, want %d", out, c.want)
			}
			if len(w.status) != refreshed+1 {
				t.Error("the status record was not refreshed")
			}
			w.lockIsFree(t)
		})
	}
}

// BV1: a backup taken whose status record cannot be refreshed is printed, then failed.
func TestBackupWithAFailedStatusRefresh(t *testing.T) {
	w, u := backupWorld(t)
	w.statErr = errors.New("disk full")
	out := w.backupVerb(w.backupDeps(u), "backup", "--team", "alpha")
	if out.code != term.ExitFailure || !strings.Contains(out.stdout, `"backup":1`) || !strings.Contains(out.stderr, "the status record: disk full") {
		t.Fatalf("%+v", out)
	}
}

// jumpClock passes a minute at each reading, so a lock wait ends after a few.
type jumpClock struct{ at time.Time }

func (c *jumpClock) Now() time.Time {
	c.at = c.at.Add(time.Minute)
	return c.at
}

func (c *jumpClock) Sleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

// BV1: a lock held past BackupWait is 75 naming its holder, and a recorded restore 70 (RG2);
// neither signals the server.
func TestBackupBusyAndRestorePending(t *testing.T) {
	w, u := backupWorld(t)
	lock, err := teamlock.Take(t.Context(), w.root+paths.AdminRunDir, alpha(t), "team.apply", 0, w, teamlock.Unguarded)
	if err != nil {
		t.Fatal(err)
	}
	sys := w.sys()
	sys.Backup = w.backupDeps(u)
	clock := &jumpClock{at: w.at}
	sys.Host.Clock = clock
	var stdout, stderr bytes.Buffer
	code := Run(t.Context(), Env{Args: []string{"backup", "--team", "alpha"}, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr, Proc: rootProc(), Digest: digest, Sys: sys})
	if code != ExitBusy || stdout.Len() != 0 || len(u.kills) != 0 || !strings.Contains(stderr.String(), "team.apply alpha pid=") {
		t.Fatalf("busy = %d %q %q", code, stdout.String(), stderr.String())
	}
	if waited := clock.at.Sub(w.at); waited < BackupWait || waited > BackupWait+2*time.Minute {
		t.Fatalf("waited %v for the lock, want %v", waited, BackupWait)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.root+paths.RestoreRecord(alpha(t)), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := w.backupVerb(w.backupDeps(u), "backup", "--team", "alpha")
	if out.code != ExitRefused || out.stdout != "" || len(u.kills) != 0 {
		t.Fatalf("restore pending = %+v", out)
	}
}

// stampBeta stamps a second team, beta, with only its team.json: its backup fails at its
// sources.
func (w *world) stampBeta(t *testing.T) {
	t.Helper()
	beta := teamID(t, "beta")
	for _, dir := range []string{paths.AdminState(beta), paths.StatusTeamDir(beta)} {
		if err := os.MkdirAll(w.root+dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(w.root+paths.TeamDeclaration(beta), []byte(betaTeamJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	stamp := `{"v":1,"team":"beta","config":"c","software":"s","tuwunel":"1.9.4","applied_ms":1}`
	if err := os.WriteFile(w.root+paths.StampFile(beta), []byte(stamp), 0o600); err != nil {
		t.Fatal(err)
	}
}

const betaTeamJSON = `{"v":1,"team":"beta","name":"Beta","server_name":"beta.teamchat.internal","port":8449,` +
	`"description":"The other team.","listen":["192.0.2.10"],"allow_from":["192.0.2.0/24"],` +
	`"url":"http://192.0.2.10:8449","humans":["alice"],"coordinator":"myrepo.1+desk.podman",` +
	`"trust":{"kind":"github","org":"example-org","repos":[{"repo":"example-org/thing","branches":["main"]}]}}`

// BV2, n2g-D5: every stamped team in turn, one stderr line each, nothing on stdout, 1 when one
// failed.
func TestBackupAll(t *testing.T) {
	w, u := backupWorld(t)
	out := w.backupVerb(w.backupDeps(u), "backup", "--all")
	if out.code != term.ExitOK || out.stdout != "" || out.stderr != "alpha backup 1\n" {
		t.Fatalf("one team = %+v", out)
	}
	w.stampBeta(t)
	out = w.backupVerb(w.backupDeps(u), "backup", "--all")
	lines := strings.Split(strings.TrimSuffix(out.stderr, "\n"), "\n")
	if out.code != term.ExitFailure || out.stdout != "" || len(lines) != 2 || lines[0] != "alpha backup 2" || !strings.HasPrefix(lines[1], "beta failed: the backup of team beta failed at its sources step") {
		t.Fatalf("two teams = %+v", out)
	}
}

func TestBackupAllWithNothingStampedAndAnUnreadableStamp(t *testing.T) {
	w := newWorld(t)
	u := &backupUnits{w: w, inactive: map[string]bool{}}
	if out := w.backupVerb(w.backupDeps(u), "backup", "--all"); out.code != term.ExitOK || out.stdout != "" || out.stderr != "" {
		t.Fatalf("no teams = %+v", out)
	}
	dir := w.root + paths.StatusTeamDir(alpha(t))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.root+paths.StampFile(alpha(t)), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := w.backupVerb(w.backupDeps(u), "backup", "--all"); out.code != term.ExitFailure || out.stdout != "" || out.stderr == "" {
		t.Fatalf("a bad stamp = %+v", out)
	}
}

// BV3: no lock, only reads; nothing taken is an empty listing with no last run.
func TestBackupsListsWithoutTheLock(t *testing.T) {
	w, u := backupWorld(t)
	lock, err := teamlock.Take(t.Context(), w.root+paths.AdminRunDir, alpha(t), "team.apply", 0, w, teamlock.Unguarded)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lock.Release(); err != nil {
			t.Error(err)
		}
	})
	out := w.backupVerb(w.backupDeps(u), "backups", "--team", "alpha")
	if want := `{"v":1,"team":"alpha","backups":[],"last_run":null}` + "\n"; out.code != term.ExitOK || out.stdout != want {
		t.Fatalf("%+v, want %q", out, want)
	}
	if err := os.MkdirAll(w.root+paths.BackupDir(alpha(t)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.root+paths.BackupLastRun(alpha(t)), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := w.backupVerb(w.backupDeps(u), "backups", "--team", "alpha"); out.code != term.ExitFailure || out.stdout != "" {
		t.Fatalf("a bad last run = %+v", out)
	}
}

// BV4: the flags are strict (64, nothing done), and a build with no backup seam says so.
func TestBackupFlags(t *testing.T) {
	w, u := backupWorld(t)
	bad := map[string][]string{
		"both":       {"backup", "--all", "--team", "alpha"},
		"neither":    {"backup"},
		"positional": {"backup", "alpha"},
		"unknown":    {"backup", "--team", "alpha", "--keep", "1"},
		"bad team":   {"backup", "--team", "Alpha!"},
		"list all":   {"backups", "--all"},
		"list none":  {"backups"},
		"list extra": {"backups", "--team", "alpha", "x"},
	}
	for name, args := range bad {
		if out := w.backupVerb(w.backupDeps(u), args...); out.code != term.ExitUsage || out.stdout != "" {
			t.Errorf("%s: %+v", name, out)
		}
	}
	if len(u.kills) != 0 {
		t.Fatalf("a refused verb signalled: %v", u.kills)
	}
	for _, args := range [][]string{{"backup", "--team", "alpha"}, {"backup", "--all"}, {"backups", "--team", "alpha"}} {
		if out := w.backupVerb(nil, args...); out.code != term.ExitFailure || out.stderr != "teamchat-admin: backups are not wired yet\n" {
			t.Errorf("%v unwired: %+v", args, out)
		}
	}
}

// BV4, DC4: backup, backups and restore (with --lost too) asked by the ctl uid or another uid
// are 77, and nothing is read: the System is never built.
func TestBackupVerbsAreRootsOnly(t *testing.T) {
	verbs := [][]string{
		{"backup", "--team", "alpha"},
		{"backup", "--all"},
		{"backups", "--team", "alpha"},
		{"restore", "--team", "alpha", "--backup", "1"},
		{"restore", "--team", "alpha", "--lost", "--backup", "1"},
	}
	for _, proc := range []*fakeProc{childProc(), ownerProc()} {
		for _, args := range verbs {
			wired := 0
			wire := func() (*System, error) {
				wired++
				return nil, errors.New("never")
			}
			out := runEnv(t, Env{Proc: proc, Wire: wire}, "", args...)
			if out.code != ExitClass || out.stdout != "" || wired != 0 {
				t.Errorf("%v by uid %d: %+v, wired %d", args, proc.uid, out, wired)
			}
		}
	}
}
