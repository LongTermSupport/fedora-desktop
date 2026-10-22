package keeper

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// keeper is one session's keeper; every field is the loop goroutine's.
type keeper struct {
	cfg            Config
	stdout, stderr io.Writer
	events         chan func()
	done           chan struct{}

	early  []string
	log    *os.File
	logErr error

	runRoot   *homefs.Root
	runID     paths.RunID
	run       paths.Run
	madeRun   bool
	klock     *lock.KeeperLock
	kit       paths.Kit
	digest    paths.Digest
	stateRoot *homefs.Root
	linkLog   *os.File
	watch     <-chan WatchEvent
	argv      []string

	desired      *state.Desired
	desiredState string
	desiredCode  *string
	inbound      string

	seats      []*seat
	removedIDs map[paths.TeamID][]string

	phase        phase
	link         *link
	linkStatus   string
	why          *string
	restarts     int64
	nextMS       *int64
	restartTimer *timer
	endTimer     *timer
	timers       map[*timer]struct{}

	cmd        execx.Started
	cmdProc    state.Process
	cmdDone    bool
	cmdStatus  execx.ExitStatus
	cmdSent    map[syscall.Signal]bool
	claude     Pidfd
	claudeProc *state.Process
	claudeDone bool
	started    bool
	token      *wire.Token
	stopped    bool
	stopSig    syscall.Signal
	afterTerm  *timer
	afterKill  *timer
	failedCmd  bool

	status   term.ExitCode
	exitErr  error
	finished bool
}

// phase is where the session is: the link coming up, CMD running, or the end.
type phase int

const (
	launching phase = iota
	running
	ending
)

func newKeeper(cfg Config, stdout, stderr io.Writer) *keeper {
	return &keeper{
		cfg: cfg, stdout: stdout, stderr: stderr,
		events: make(chan func()), done: make(chan struct{}),
		desiredState: state.DesiredApplied, inbound: state.InboundRefuse,
		removedIDs: map[paths.TeamID][]string{}, linkStatus: state.LinkRestarting,
		timers: map[*timer]struct{}{}, cmdSent: map[syscall.Signal]bool{},
	}
}

// prepare is B 2.4.1 steps 1-6: everything before the link starts.
// Its lock tries and the kit's --version are bounded by their own clocks; a stop arrives
// by Signals once the loop runs.
func (k *keeper) prepare() error {
	ctx := context.Background()
	if err := k.openRunRoot(); err != nil {
		return err
	}
	if err := k.resolveKit(ctx); err != nil {
		return err
	}
	if err := k.readLaunchDesired(); err != nil {
		return err
	}
	if err := k.sweep(ctx); err != nil {
		return err
	}
	if err := k.makeRun(ctx); err != nil {
		return err
	}
	if err := k.claudeArgv(); err != nil {
		return err
	}
	return k.takeLaunchSeats(ctx)
}

// resolveKit is step 2: the kit, checked by its --version, re-executed when it is another
// version than this binary.
func (k *keeper) resolveKit(ctx context.Context) error {
	l := k.cfg.Layout
	named := ""
	switch {
	case k.cfg.Launch.Kit != "":
		kit, err := paths.KitAt(k.cfg.Launch.Kit)
		if err != nil {
			return fault(exits.CodeRuntime, msgKit, exits.Name(k.cfg.Launch.Kit))
		}
		k.kit = kit
		if _, derr := paths.ParseDigest(filepath.Base(kit.String())); derr == nil {
			named = filepath.Base(kit.String())
		}
	case l.CurrentKit().String() == l.KitRoot():
		k.kit = l.CurrentKit()
	default:
		kit, name, err := k.currentKit()
		if err != nil {
			return err
		}
		k.kit, named = kit, name
	}
	printed, err := k.kitVersion(ctx)
	if err != nil {
		return err
	}
	if named != "" && printed != named {
		return fault(exits.CodeRuntime, msgKitVersion, exits.Name(k.kit.String()))
	}
	if printed == k.cfg.Digest {
		k.digest, err = paths.ParseDigest(printed)
		return err
	}
	if k.cfg.Launch.Kit != "" {
		return fault(exits.CodeRuntime, msgKitMismatch, exits.Name(k.kit.String()))
	}
	args := append([]string{"run", "--kit", k.kit.String()}, k.cfg.Args...)
	if err := k.cfg.Execer.Exec(execx.Cmd{Name: k.kit.Binary(), Args: args}); err != nil {
		k.logf("re-exec %s: %v", k.kit.Binary(), err)
		return fault(exits.CodeRuntime, msgReexec, exits.Name(k.kit.Binary()))
	}
	return errExeced
}

// currentKit is the kit the current link names: a digest directory directly under the kit
// root.
func (k *keeper) currentKit() (paths.Kit, string, error) {
	root, current := k.cfg.Layout.KitRoot(), k.cfg.Layout.CurrentKit().String()
	target, err := os.Readlink(current)
	if err != nil {
		k.logf("read %s: %v", current, err)
		return paths.Kit{}, "", fault(exits.CodeRuntime, msgKit, exits.Name(current))
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	name := filepath.Base(target)
	if _, derr := paths.ParseDigest(name); derr != nil || filepath.Dir(target) != root {
		return paths.Kit{}, "", fault(exits.CodeRuntime, msgKit, exits.Name(current))
	}
	kit, err := paths.KitAt(target)
	if err != nil {
		return paths.Kit{}, "", fault(exits.CodeRuntime, msgKit, exits.Name(current))
	}
	return kit, name, nil
}

// kitVersion is the digest the kit's binary prints: one line, a digest.
func (k *keeper) kitVersion(ctx context.Context) (string, error) {
	bin := k.kit.Binary()
	bound, cancel := context.WithCancel(ctx)
	stop := k.cfg.Clock.AfterFunc(versionWait, cancel)
	res, err := k.cfg.Runner.Run(bound, execx.Cmd{Name: bin, Args: []string{"--version"}})
	stop()
	cancel()
	if err != nil || res.ExitCode != 0 {
		k.logf("%s --version: exit %d, %v", bin, res.ExitCode, err)
		return "", fault(exits.CodeRuntime, msgKitVersion, exits.Name(k.kit.String()))
	}
	line, ok := strings.CutSuffix(string(res.Stdout), "\n")
	if _, derr := paths.ParseDigest(line); !ok || derr != nil {
		return "", fault(exits.CodeRuntime, msgKitVersion, exits.Name(k.kit.String()))
	}
	return line, nil
}

// readLaunchDesired is step 5's inbound decision: the desired membership, watched before it is
// read so no change is missed.
func (k *keeper) readLaunchDesired() error {
	if k.cfg.Launch.Desired == "" {
		if kind := k.cfg.Launch.Kind; kind == cli.KindCCY || kind == cli.KindLXC {
			return fault(exits.CodeMembership, msgNoDesired, exits.Name(string(kind)))
		}
		return nil
	}
	dir := k.cfg.Layout.Desired()
	ch, err := k.cfg.Watcher.Watch(dir)
	if err != nil {
		k.logf("watch %s: %v", dir, err)
		return fault(exits.CodeRuntime, msgWatch, exits.Name(dir))
	}
	k.watch = ch
	d, err := k.readDesired()
	if err != nil {
		k.logf("read %s: %v", k.cfg.Layout.DesiredFile(), err)
		return fault(exits.CodeMembership, msgDesired, exits.Name(k.cfg.Layout.DesiredFile()))
	}
	k.desired, k.inbound = &d, d.Inbound
	return nil
}

func (k *keeper) readDesired() (state.Desired, error) {
	data, err := trustedRead(k.cfg.Layout.DesiredFile(), state.MaxDesired)
	if err != nil {
		return state.Desired{}, err
	}
	return state.DecodeDesired(data)
}

// errUntrusted is a file a trusted read refuses.
var errUntrusted = errors.New("keeper: not a regular file of this user or root, free of group and other write, within its cap")

// trustedRead reads a file another account may have placed (desired.json, the kit's
// settings): reached through no link, opened without blocking, a regular file of this user or
// root that no group or other can write, at most limit bytes.
func trustedRead(path string, limit int) ([]byte, error) {
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_NONBLOCK | unix.O_CLOEXEC | unix.O_NOCTTY,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	f := os.NewFile(uintptr(fd), path)
	data, err := readTrusted(f, limit)
	return data, errors.Join(err, f.Close())
}

func readTrusted(f *os.File, limit int) ([]byte, error) {
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return nil, err
	}
	owner := int64(st.Uid) == int64(os.Geteuid()) || st.Uid == 0
	if st.Mode&unix.S_IFMT != unix.S_IFREG || !owner || st.Mode&0o022 != 0 {
		return nil, errUntrusted
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errUntrusted
	}
	return data, nil
}

// sweep is step 3 (T8): each runtime left behind whose keeper.lock is free and whose keeper
// and session are gone, or which never wrote session.pid, is removed under that lock.
func (k *keeper) sweep(ctx context.Context) error {
	rr := k.cfg.Layout.RunRoot()
	entries, err := k.runRoot.Entries(rr)
	if err != nil {
		k.logf("list %s: %v", rr, err)
		return fault(exits.CodeRuntime, msgRunRoot, exits.Name(rr))
	}
	for _, e := range entries {
		id, perr := paths.ParseRunID(e.Name)
		if perr != nil || e.Type != fs.ModeDir {
			continue
		}
		k.sweepOne(ctx, k.cfg.Layout.Run(id))
	}
	return nil
}

func (k *keeper) sweepOne(ctx context.Context, run paths.Run) {
	kl, err := lock.TakeKeeper(ctx, k.cfg.Locks, k.runRoot, run)
	if err != nil {
		if !errors.Is(err, lock.ErrBusy) {
			k.logf("leftover %s: keeper.lock: %v", run.Dir(), err)
		}
		return
	}
	defer func() {
		if rerr := kl.Release(); rerr != nil {
			k.logf("leftover %s: release keeper.lock: %v", run.Dir(), rerr)
		}
	}()
	pid, err := state.Read(k.runRoot, run.PID(), state.MaxSessionPID, state.DecodeSessionPID)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		k.logf("leftover %s left: session.pid: %v", run.Dir(), err)
		return
	case !k.gone(pid.Keeper) || (pid.Session != nil && !k.gone(*pid.Session)):
		return
	}
	if err := k.runRoot.RemoveTree(run.Dir()); err != nil {
		k.logf("leftover %s: remove: %v", run.Dir(), err)
		return
	}
	k.logf("leftover %s removed", run.Dir())
}

// gone is whether p has ended: no such pid, or another process's start time.
func (k *keeper) gone(p state.Process) bool {
	ticks, err := k.cfg.Proc.StartTicks(p.PID)
	return err != nil || ticks != p.StartTicks
}

// makeRun is step 4: the run id, its directory, keeper.lock, keeper.log and session.pid.
func (k *keeper) makeRun(ctx context.Context) error {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fault(exits.CodeInternal, msgInternal, exits.Words("draw a run id"))
	}
	id, err := paths.ParseRunID(hex.EncodeToString(b[:]))
	if err != nil {
		return err
	}
	k.runID, k.run = id, k.cfg.Layout.Run(id)
	if merr := k.runRoot.Mkdir(k.run.Dir()); merr != nil {
		k.logf("make %s: %v", k.run.Dir(), merr)
		return fault(exits.CodeRuntime, msgRunDir, exits.Name(k.run.Dir()))
	}
	k.madeRun = true
	if k.klock, err = lock.TakeKeeper(ctx, k.cfg.Locks, k.runRoot, k.run); err != nil {
		k.logf("take %s: %v", k.run.KeeperLock(), err)
		return fault(exits.CodeRuntime, msgRunDir, exits.Name(k.run.Dir()))
	}
	if k.log, err = openAppend(k.run.KeeperLog()); err != nil {
		return fault(exits.CodeRuntime, msgRunDir, exits.Name(k.run.Dir()))
	}
	early := k.early
	k.early = nil
	for _, line := range early {
		k.logf("%s", strings.TrimSuffix(line, "\n"))
	}
	return k.writeSessionPID(nil)
}

// openAppend opens a fresh log file of the run: made here, never through a link.
func openAppend(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_APPEND|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}

// writeSessionPID writes session.pid, with the session from step 7 on.
func (k *keeper) writeSessionPID(session *state.Process) error {
	ticks, err := k.cfg.Proc.StartTicks(os.Getpid())
	if err != nil {
		k.logf("read the keeper's start time: %v", err)
		return fault(exits.CodeInternal, msgInternal, exits.Words("read its own start time"))
	}
	launch := state.LaunchPlain
	if k.cfg.Launch.Mode == cli.LaunchClaude {
		launch = state.LaunchClaude
	}
	pid := state.SessionPID{
		V: state.Version, Run: k.runID, Keeper: state.Process{PID: os.Getpid(), StartTicks: ticks},
		Session: session, Kit: k.digest, Launch: launch, Inbound: k.inbound, Kind: string(k.cfg.Launch.Kind),
	}
	if err := state.Write(k.runRoot, k.run.PID(), pid); err != nil {
		k.logf("write %s: %v", k.run.PID(), err)
		return fault(exits.CodeRuntime, msgRunDir, exits.Name(k.run.Dir()))
	}
	return nil
}

// claudeArgv is step 5: CMD, and for Claude Code the resume, the plugin and the per-launch
// settings appended.
func (k *keeper) claudeArgv() error {
	k.argv = k.cfg.Launch.Cmd
	if k.cfg.Launch.Mode != cli.LaunchClaude {
		return nil
	}
	settings := k.kit.Settings()
	data, err := trustedRead(settings, maxSettings)
	var obj map[string]json.RawMessage
	if err == nil {
		err = json.Unmarshal(data, &obj)
	}
	if _, has := obj[inboundKey]; err != nil || obj == nil || has {
		k.logf("read %s: %v", settings, err)
		return fault(exits.CodeRuntime, msgSettings, exits.Name(settings))
	}
	obj[inboundKey], err = json.Marshal(k.inbound)
	if err != nil {
		return err
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	if err := k.runRoot.Create(k.run.Settings(), out, 0o600); err != nil {
		k.logf("write %s: %v", k.run.Settings(), err)
		return fault(exits.CodeRuntime, msgRunDir, exits.Name(k.run.Dir()))
	}
	argv := append([]string{}, k.cfg.Launch.Cmd...)
	if k.cfg.Launch.ResumeRecorded {
		argv = append(argv, k.resume()...)
	}
	argv = append(argv, "--plugin-dir", k.kit.PluginDir(), "--settings", k.run.Settings())
	k.argv = argv
	return nil
}

// resume is --resume <id> when the conversation record names a session whose transcript is in
// the session's project directory; else nothing, and keeper.log says why.
func (k *keeper) resume() []string {
	root, err := k.state()
	if err != nil {
		k.logf("no resume: the state root: %v", err)
		return nil
	}
	conv, err := state.Read(root, k.cfg.Layout.ConversationFile(), state.MaxConversation, state.DecodeConversation)
	if err != nil {
		k.logf("no resume: the conversation record: %v", err)
		return nil
	}
	id := conv.SessionID
	if id == "" || filepath.Base(id) != id || strings.HasPrefix(id, ".") {
		k.logf("no resume: the conversation record names no session id")
		return nil
	}
	path := filepath.Join(k.claudeDir(), "projects", projectDir(k.cfg.Cwd), id+".jsonl")
	var st unix.Stat_t
	if err := unix.Fstatat(unix.AT_FDCWD, path, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil || st.Mode&unix.S_IFMT != unix.S_IFREG {
		k.logf("no resume: the transcript %s is not a regular file (%v)", path, err)
		return nil
	}
	return []string{"--resume", id}
}

// claudeDir is Claude Code's configuration directory: CLAUDE_CONFIG_DIR when absolute and
// clean, else ~/.claude.
func (k *keeper) claudeDir() string {
	if d := k.claudeConfigDir(); d != nil {
		return *d
	}
	return filepath.Join(k.cfg.Env(envHome), ".claude")
}

func (k *keeper) claudeConfigDir() *string {
	d := k.cfg.Env(envConfig)
	if d == "" || !filepath.IsAbs(d) || filepath.Clean(d) != d {
		return nil
	}
	return &d
}

// projectDir is the project directory Claude Code derives from a working directory: every byte
// that is not a letter or a digit as '-'.
func projectDir(cwd string) string {
	b := []byte(cwd)
	for i, c := range b {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			b[i] = '-'
		}
	}
	return string(b)
}

// state is the state root, opened at first use.
func (k *keeper) state() (*homefs.Root, error) {
	if k.stateRoot != nil {
		return k.stateRoot, nil
	}
	root, err := homefs.Open(k.cfg.Layout.State(), homefs.Private)
	if err != nil {
		return nil, err
	}
	k.stateRoot = root
	return root, nil
}

// want is a seat the session should hold.
type want struct {
	team paths.TeamID
	name string
}

// wanted are the seats the launch names: the desired membership's, or the --seat refs.
func (k *keeper) wanted() []want {
	var out []want
	if k.desired != nil {
		for _, s := range k.desired.Seats {
			out = append(out, want{s.Team, s.Seat})
		}
		return out
	}
	for _, s := range k.cfg.Launch.Seats {
		out = append(out, want{s.Team, s.Seat.String()})
	}
	return out
}

// takeLaunchSeats is step 6's locks: a seat another session holds fails the launch (75); any
// other failure is logged and the launch goes on without that seat (the owner's rule).
func (k *keeper) takeLaunchSeats(ctx context.Context) error {
	for _, w := range k.wanted() {
		s, err := k.takeSeat(ctx, w)
		if errors.Is(err, lock.ErrBusy) {
			return exits.New(exits.CodeSeatHeld, &w.team, msgSeatHeld, exits.Team(w.team))
		}
		if err == nil {
			k.seats = append(k.seats, s)
		}
	}
	return nil
}

// takeSeat takes w's seat lock and writes its record; a failure other than busy is logged.
func (k *keeper) takeSeat(ctx context.Context, w want) (*seat, error) {
	root, err := k.state()
	if err != nil {
		k.logf("seat %s not held: the state root: %v", w.team, err)
		return nil, err
	}
	ps := k.cfg.Layout.Seat(w.team)
	l, err := k.klock.TakeSeat(ctx, root, ps)
	if err != nil {
		if !errors.Is(err, lock.ErrBusy) {
			k.logf("seat %s not held: %v", w.team, err)
		}
		return nil, err
	}
	s := &seat{team: w.team, name: w.name, path: ps, lock: l, startedMS: k.cfg.Clock.WallMS()}
	if err := state.WriteLockRecord(l, state.LockRecord{V: state.Version, Run: k.runID, StartedMS: s.startedMS}); err != nil {
		k.logf("seat %s: write its lock record: %v", w.team, err)
	}
	k.logf("seat %s/%s held", w.team, w.name)
	return s, nil
}

// close releases what the keeper holds; with keep, the run directory is left for its logs.
func (k *keeper) close(keep bool) {
	for _, s := range k.seats {
		k.closeSeatLock(s)
	}
	k.seats = nil
	if k.madeRun && !keep {
		if err := k.runRoot.RemoveTree(k.run.Dir()); err != nil {
			k.logf("remove %s: %v", k.run.Dir(), err)
		}
	}
	if k.klock != nil {
		if err := k.klock.Release(); err != nil {
			k.logf("release keeper.lock: %v", err)
		}
	}
	if k.claude != nil {
		k.closeQuietly(k.claude)
	}
	if k.cfg.Watcher != nil {
		k.closeQuietly(k.cfg.Watcher)
	}
	if k.stateRoot != nil {
		k.closeQuietly(k.stateRoot)
	}
	if k.runRoot != nil {
		k.closeQuietly(k.runRoot)
	}
	if k.linkLog != nil {
		k.closeQuietly(k.linkLog)
	}
	if k.log != nil {
		if err := k.log.Close(); err != nil {
			k.logErr = err
		}
		k.log = nil
	}
}

// closeSeatLock closes s's lock, if it still holds one.
func (k *keeper) closeSeatLock(s *seat) {
	if s.lock == nil {
		return
	}
	if err := s.lock.Close(); err != nil {
		k.logf("seat %s: close its lock: %v", s.team, err)
	}
	s.lock = nil
}
