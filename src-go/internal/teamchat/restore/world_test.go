package restore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// The world's backup: number 7, made at backupMS, and the pre-restore backup M the fake takes.
const (
	backupN  = 7
	backupMS = 1_700_000_000_000
	preM     = 9
)

// world is a host for the engine: a temp tree under an OS FS, and a fake for every other
// seam. Every effect (each FS write, rename, removal and sync, each unit stop, program run,
// child op, start, bootstrap, backup and status refresh) is one call in calls; the kill-th
// ends the engine's goroutine as a kill would (SM5), before it takes effect.
type world struct {
	t     *testing.T
	team  paths.TeamID
	os    OS
	state string
	decl  declare.Team

	mu    sync.Mutex
	calls []string
	n     int
	kill  int

	units   map[string]*fakeUnit
	nextInv int
	// unloadAfter is how many State reads a stopped staging unit stays loaded for.
	unloadAfter int
	// crashLoop makes the live unit alternate activating and failed until it is stopped, and
	// fail every start of the old database; stopped is whether it was stopped since.
	crashLoop, stopped bool
	// answers is how many ServerVersion probes fail before one answers; -1 is never.
	answers int
	probes  int

	current, backup apply.RosterFile
	statusRec       *status.Record
	tarErr          error
	tarServer       string

	// fail names the effect that fails, each time it is called; hooks run after an effect.
	fail  map[string]error
	hooks map[string]func()
	// stopFails makes a stopped live unit failed, as systemd keeps a crash-looping one.
	stopFails bool
	// stagedEpoch is what a staged check answers; secured is the epoch the secure op wrote.
	stagedEpoch *Epoch
	secured     Epoch
	// bootRev is a rev a bootstrap writes beyond the merged one.
	bootRev  int64
	events   [][]string
	sleeps   int
	exported int
}

type fakeUnit struct {
	st     UnitState
	unload int
}

func newWorld(t *testing.T) *world {
	t.Helper()
	o, state := osTree(t)
	w := &world{
		t: t, team: opsTeam(t), os: o, state: state, units: map[string]*fakeUnit{}, answers: 0,
		fail: map[string]error{}, hooks: map[string]func(){}, unloadAfter: 2,
	}
	in, err := declare.DecodeInput([]byte(`{"team":{"team":"ops","state":"present","port":8448,"description":"d",` +
		`"listen":["192.0.2.10"],"allow_from":["192.0.2.0/24"],"humans":["alice"],"trust":{"kind":"github",` +
		`"org":"example-org","repos":[{"repo":"example-org/thing","branches":["main"]}]}},"lxc_members":[],"address":"192.0.2.10"}`))
	if err != nil {
		t.Fatal(err)
	}
	w.decl = in.Team
	w.units[paths.ServerUnit(w.team)] = &fakeUnit{st: UnitState{Load: "loaded", Active: "active", InvocationID: "live1"}}
	w.current = roster(t, 20, 5, 2, map[string]apply.Member{h1: token(apply.MemberActive, 5), h2: token(apply.MemberActive, 15)},
		map[string]apply.Human{"alice": human(apply.HumanActive, 0)})
	w.backup = roster(t, 10, 4, 2, map[string]apply.Member{h1: token(apply.MemberActive, 5), h2: token(apply.MemberActive, 5)},
		map[string]apply.Human{"alice": human(apply.HumanActive, 0)})
	w.statusRec = &status.Record{Origin: "apply", Members: []status.Member{{Handle: h1, Kind: "podman"}}}
	layout(t, state, map[string]string{"db": "d"})
	w.mark("db", "live")
	writeFile(t, state+"/backups/meta/"+strconv.Itoa(backupN), "1700000000\n")
	w.put(paths.TeamDeclaration(w.team), string(w.decl.Canonical()))
	w.put(paths.ServerNameFile(w.team), w.decl.ServerName)
	w.put(paths.ServerConfig(w.team), w.config())
	w.put(paths.StampFile(w.team), `{"v":1,"team":"ops","config":"c","software":"s","tuwunel":"1.9.4","applied_ms":1}`)
	w.put(paths.RosterFile(w.team), string(w.current.Encode()))
	return w
}

// config is the live config's lines the restore rewrites, among others.
func (w *world) config() string {
	return "server_name = \"" + w.decl.ServerName + "\"\naddress = [\"127.0.0.1\", \"192.0.2.10\"]\nport = 8448\n" +
		"database_path = \"" + paths.HomeserverDatabase(w.team) + "\"\ndatabase_backup_path = \"" +
		paths.HomeserverBackups(w.team) + "\"\nregistration_shared_secret = \"k\"\n"
}

func (w *world) put(path, content string) {
	w.t.Helper()
	writeFile(w.t, w.os.Root+path, content)
}

func (w *world) has(path string) bool {
	w.t.Helper()
	return exists(w.t, w.os.Root+path)
}

func (w *world) read(path string) string {
	w.t.Helper()
	raw, err := os.ReadFile(filepath.Clean(w.os.Root + path))
	if err != nil {
		w.t.Fatal(err)
	}
	return string(raw)
}

// mark sets what the database directory name holds.
func (w *world) mark(name, what string) {
	w.t.Helper()
	writeFile(w.t, w.state+"/"+name+"/marker", what)
}

// db is what the state directory's db holds: "live", "restored", or "" when absent.
func (w *world) db() string { return marker(w.t, w.state, "db") }

// effect records one effect and dies at the kill-th; a configured failure is returned.
func (w *world) effect(name string) error {
	w.mu.Lock()
	w.n++
	n := w.n
	w.calls = append(w.calls, name)
	err := w.fail[name]
	hook := w.hooks[name]
	w.mu.Unlock()
	if n == w.kill {
		runtime.Goexit()
	}
	if hook != nil {
		hook()
	}
	return err
}

func (w *world) record() Record {
	w.t.Helper()
	r, err := DecodeRecord([]byte(w.read(paths.RestoreRecord(w.team))), w.team)
	if err != nil {
		w.t.Fatal(err)
	}
	return r
}

func (w *world) floor() EpochFloor {
	w.t.Helper()
	if !w.has(paths.RestoreEpochFile(w.team)) {
		return EpochFloor{}
	}
	f, err := DecodeEpochFloor([]byte(w.read(paths.RestoreEpochFile(w.team))))
	if err != nil {
		w.t.Fatal(err)
	}
	return f
}

// worldFS is the OS FS with every mutation an effect.
type worldFS struct {
	OS

	w *world
}

func (f worldFS) Rename(team paths.TeamID, from, to string) error {
	if err := f.w.effect("rename " + from + " " + to); err != nil {
		return err
	}
	return f.OS.Rename(team, from, to)
}

func (f worldFS) RemoveTree(team paths.TeamID, name string) error {
	if err := f.w.effect("removetree " + name); err != nil {
		return err
	}
	return f.OS.RemoveTree(team, name)
}

func (f worldFS) Fsync(team paths.TeamID) error {
	if err := f.w.effect("fsync"); err != nil {
		return err
	}
	return f.OS.Fsync(team)
}

func (f worldFS) Syncfs(team paths.TeamID) error {
	if err := f.w.effect("syncfs"); err != nil {
		return err
	}
	return f.OS.Syncfs(team)
}

// short is a root path's last name, with the record's step when it is the record.
func short(path string, data []byte) string {
	name := path[strings.LastIndex(path, "/")+1:]
	if name == "restore.json" {
		var r struct {
			Step string `json:"step"`
		}
		if json.Unmarshal(data, &r) == nil {
			name += "=" + r.Step
		}
	}
	return name
}

func (f worldFS) WriteFile(path string, data []byte) error {
	if err := f.w.effect("write " + short(path, data)); err != nil {
		return err
	}
	return f.OS.WriteFile(path, data)
}

func (f worldFS) Remove(path string) error {
	if err := f.w.effect("remove " + short(path, nil)); err != nil {
		return err
	}
	return f.OS.Remove(path)
}

func (w *world) unit(name string) *fakeUnit {
	u, ok := w.units[name]
	if !ok {
		u = &fakeUnit{st: UnitState{Load: "not-found", Active: "inactive"}}
		w.units[name] = u
	}
	return u
}

type worldUnits struct{ w *world }

func (u worldUnits) State(_ context.Context, unit string) (UnitState, error) {
	w := u.w
	f := w.unit(unit)
	if next, looping := map[string]string{"activating": "failed", "failed": "activating"}[f.st.Active]; looping &&
		unit == paths.ServerUnit(w.team) && w.crashLoop && !w.stopped {
		f.st.Active = next
	}
	if f.unload > 0 {
		f.unload--
		if f.unload == 0 {
			f.st = UnitState{Load: "not-found", Active: "inactive"}
		}
	}
	return f.st, nil
}

func (u worldUnits) Stop(_ context.Context, unit string) error {
	w := u.w
	if err := w.effect("stop " + unit); err != nil {
		return err
	}
	f := w.unit(unit)
	switch {
	case unit == paths.RestoreUnit(w.team):
		f.st.Active, f.unload = "inactive", w.unloadAfter
		if w.unloadAfter == 0 {
			f.st = UnitState{Load: "not-found", Active: "inactive"}
		}
	case w.crashLoop || w.stopFails || f.st.Active == "failed":
		f.st.Active, w.stopped = "failed", true
	default:
		f.st.Active, w.stopped = "inactive", true
	}
	return nil
}

type worldReady struct{ w *world }

func (r worldReady) ServerVersion(context.Context, int) error {
	w := r.w
	w.probes++
	if w.answers < 0 || w.probes <= w.answers || w.unit(paths.ServerUnit(w.team)).st.Active != "active" {
		return errors.New("connection refused")
	}
	return nil
}

type worldClock struct{ w *world }

func (c worldClock) Now() time.Time { return time.UnixMilli(backupMS + 86_400_000) }

func (c worldClock) Sleep(context.Context, time.Duration) error {
	c.w.sleeps++
	return nil
}

// run is systemd-run: a staging start restores into db.restore-7 (a plant included, which
// Tuwunel wipes) or opens it as it is.
func (w *world) run(_ context.Context, c execx.Cmd) (execx.Result, error) {
	argv := append([]string{c.Name}, c.Args...)
	restoring := slices.Contains(argv, "--restore-backup")
	if !c.CleanEnv || !slices.Contains(argv, "--unit="+paths.RestoreUnit(w.team)) {
		return execx.Result{}, fmt.Errorf("unexpected run %v", argv)
	}
	if err := w.effect(fmt.Sprintf("run staging restoring=%v", restoring)); err != nil {
		return execx.Result{}, err
	}
	name := restoreName(w.team, backupN)
	if restoring {
		if err := os.RemoveAll(w.state + "/" + name); err != nil {
			return execx.Result{}, err
		}
		w.mark(name, "restored")
	} else if marker(w.t, w.state, name) != "restored" {
		return execx.Result{}, errors.New("nothing to open")
	}
	w.nextInv++
	w.unit(paths.RestoreUnit(w.team)).st = UnitState{Load: "loaded", Active: "active", InvocationID: "stage" + strconv.Itoa(w.nextInv)}
	return execx.Result{}, nil
}

type worldRun struct{ w *world }

func (r worldRun) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) { return r.w.run(ctx, c) }

// worldChild is the teamchat-ctl child: the staged server's epoch is what secure wrote, the
// live server's is the restored copy's when db holds it.
type worldChild struct{ w *world }

func (c worldChild) Secure(_ context.Context, _ paths.TeamID, unit []string, a SecureArgs) ([]string, error) {
	w := c.w
	if err := w.effect("child secure"); err != nil {
		return nil, err
	}
	if len(unit) == 0 || a.Token != "tok" || a.Port != 8448 {
		return nil, errors.New("secure without the unit, the tar's token or the port")
	}
	w.secured = Epoch{RestoreEpoch: a.Roster.RestoreEpoch, Rev: a.Roster.Rev}
	return []string{"CHANGED secured"}, nil
}

func (c worldChild) Check(_ context.Context, _ paths.TeamID, unit []string, _ CheckArgs) (Epoch, error) {
	w := c.w
	if err := w.effect(fmt.Sprintf("child check staged=%v", unit != nil)); err != nil {
		return Epoch{}, err
	}
	switch {
	case unit != nil && w.stagedEpoch != nil:
		return *w.stagedEpoch, nil
	case unit != nil:
		return w.secured, nil
	case w.db() == "restored":
		return Epoch{RestoreEpoch: w.secured.RestoreEpoch, Rev: w.secured.Rev + w.bootRev}, nil
	}
	return Epoch{RestoreEpoch: 2, Rev: 5}, nil
}

func (c worldChild) Swap(_ context.Context, _ paths.TeamID, a SwapArgs) ([]string, error) {
	if a.Token != "tok" || a.Rooms.Main == "" {
		return nil, errors.New("swap without the tar's token and rooms")
	}
	return []string{"CHANGED swapped"}, c.w.effect("child swap")
}

func (c worldChild) Export(_ context.Context, _ paths.TeamID, a ExportArgs) (Export, error) {
	if a.Backup != backupN || a.SinceMS != backupMS {
		return Export{}, errors.New("export of another backup")
	}
	c.w.exported++
	return Export{Count: 3, FirstMS: backupMS + 10, EndMS: backupMS + 50}, c.w.effect("child export")
}

func (c worldChild) Finish(context.Context, paths.TeamID, []byte) ([]string, error) {
	return nil, c.w.effect("child finish")
}

func (c worldChild) Revert(context.Context, paths.TeamID, []byte) ([]string, error) {
	return nil, c.w.effect("child revert")
}

type worldApprovals struct{ w *world }

func (a worldApprovals) Load(paths.TeamID) (*declare.Gated, error) {
	g := declare.Input{Team: a.w.decl}.Gated()
	return &g, nil
}

type worldStatus struct{ w *world }

func (s worldStatus) Refresh(_ context.Context, _ paths.TeamID, first bool, events []string) error {
	if first {
		return errors.New("a restore's refresh is never a first apply")
	}
	if err := s.w.effect("status"); err != nil {
		return err
	}
	s.w.events = append(s.w.events, events)
	return nil
}

func (w *world) tar(team paths.TeamID, n int64) (Tar, error) {
	if w.tarErr != nil || team != w.team || n != backupN {
		return Tar{}, errors.Join(w.tarErr, errors.New("no such tar"))
	}
	server := w.decl.ServerName
	if w.tarServer != "" {
		server = w.tarServer
	}
	return Tar{
		TimeMS: backupMS, ServerName: server, TeamFile: w.decl.Canonical(), Roster: w.backup,
		Rooms: proto.RoomIDs{Main: "!m:x", Reports: "!r:x", Questions: "!q:x"}, Token: "tok",
	}, nil
}

func (w *world) deps() Deps {
	return Deps{
		Root: w.os.Root, CtlUID: w.os.StateOwnerUID, StateOwnerUID: w.os.StateOwnerUID,
		Units: worldUnits{w}, Ready: worldReady{w}, Run: worldRun{w}, Child: worldChild{w}, FS: worldFS{OS: w.os, w: w},
		Tar: w.tar,
		Backup: func(_ context.Context, _ *teamlock.Lock, keep []int64) (int64, error) {
			if !slices.Equal(keep, []int64{backupN}) {
				return 0, errors.New("the pre-restore backup does not keep N")
			}
			return preM, w.effect("backup")
		},
		Serve: func(context.Context, paths.TeamID) error {
			if err := w.effect("serve"); err != nil {
				return err
			}
			live := w.unit(paths.ServerUnit(w.team))
			if w.crashLoop && w.db() != "restored" || w.db() == "" {
				live.st.Active, w.stopped = "activating", false
				return errors.New("the homeserver did not start")
			}
			live.st, w.stopped = UnitState{Load: "loaded", Active: "active", InvocationID: "live2"}, false
			return nil
		},
		Bootstrap: func(_ context.Context, b host.Bootstrap) ([]string, error) {
			if b.RegKey != "k" || !bytes.Equal(b.TeamFile, w.decl.Canonical()) || b.Input.ID() != w.team {
				return nil, errors.New("a bootstrap of another input")
			}
			return []string{"CHANGED bootstrapped"}, w.effect("bootstrap")
		},
		RegKey: func(config []byte) (string, error) {
			if !strings.Contains(string(config), `registration_shared_secret = "k"`) {
				return "", errors.New("no key")
			}
			return "k", nil
		},
		Approvals:    worldApprovals{w},
		StatusRecord: func(paths.TeamID) (*status.Record, error) { return w.statusRec, nil },
		Status:       worldStatus{w},
		Rand:         bytes.NewReader(bytes.Repeat([]byte{7}, 64)),
		Clock:        worldClock{w},
		Sandbox: func(team paths.TeamID) []string {
			return []string{"DynamicUser=yes", "User=" + paths.HomeserverUser(team), "StateDirectory=teamchat/" + team.String()}
		},
		Version: func(paths.TeamID) (paths.ServerVersion, error) { return paths.ParseServerVersion("1.9.4") },
		Self:    "/usr/local/sbin/teamchat-admin",
	}
}

// request is a fresh restore of backup 7.
func (w *world) request() Request { return Request{Team: w.team, Backup: backupN} }

func (w *world) callsSince(i int) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.calls[i:])
}

// index is where name first is in calls, -1 when not.
func (w *world) index(name string) int { return slices.Index(w.calls, name) }
