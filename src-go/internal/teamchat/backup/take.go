package backup

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/teamlock"
)

// The steps a backup fails at (BT6).
const (
	StepSources = "sources"
	StepReady   = "ready"
	StepSignal  = "signal"
	StepWait    = "wait"
	StepWrite   = "write"
	StepVerify  = "verify"
	StepPrune   = "prune"
)

var (
	// ErrNotRunning is a homeserver not active or not answering before the signal (BT1); the
	// verb refuses with 70 on it (RS15).
	ErrNotRunning = errors.New("the homeserver is not running")
	// ErrLost is a homeserver that restarted or stopped once it had answered (BT1, BT2); the verb
	// exits 69 on it (RS15).
	ErrLost = errors.New("the homeserver restarted or stopped during the backup")
)

const (
	waitFor = 60 * time.Second
	poll    = time.Second
	// whomMain is KillUnit's process selector for the unit's main process (DC17).
	whomMain = "main"
	stateOK  = "active"
)

var tarRe = regexp.MustCompile(`^[1-9][0-9]{0,17}\.tar$`)

// Error is a backup that failed, naming the team and the step (BT6).
type Error struct {
	Team paths.TeamID
	Step string
	Err  error
}

func (e *Error) Error() string {
	return "the backup of team " + e.Team.String() + " failed at its " + e.Step + " step: " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// Result is a backup taken: Tuwunel's number, the backup's time, and the tars pruned.
type Result struct {
	Backup, TimeMS int64
	Pruned         []int64
}

// Take is the internal backup (BT1-BT5) of the team whose lock its caller holds: `team apply`
// step 6, the `backup` verb and the restore's step 6. It never takes a lock and never starts
// the homeserver. keep is the tars prune must leave (the restore's N, n2g-D10). Every Take
// writes the last run's record, and a record it could not write is joined to its answer.
func Take(ctx context.Context, d Deps, held *teamlock.Lock, keep []int64) (Result, error) {
	if held == nil {
		return Result{}, errors.New("a backup must be given the team lock its caller holds")
	}
	team := held.Team()
	res, err := take(ctx, d, team, keep)
	run := Run{Result: status.RunOK, AtMS: d.Clock.Now().UnixMilli(), Backup: res.Backup}
	if err != nil {
		res, run = Result{}, Run{Result: status.RunFailed, AtMS: run.AtMS, Error: oneLine(err)}
	}
	if werr := writeLastRun(d, team, run); werr != nil {
		err = errors.Join(err, fmt.Errorf("the record of the run: %w", werr))
	}
	return res, err
}

func take(ctx context.Context, d Deps, team paths.TeamID, keep []int64) (Result, error) {
	fail := func(step string, err error) (Result, error) {
		return Result{}, &Error{Team: team, Step: step, Err: err}
	}
	src, decl, err := readSources(d, team)
	if err != nil {
		return fail(StepSources, err)
	}
	unit := paths.ServerUnit(team)
	invocation, err := ready(ctx, d, unit, decl.Port)
	if err != nil {
		return fail(StepReady, err)
	}
	before, err := scanMeta(d, team)
	if err == nil {
		err = sameInvocation(ctx, d, unit, invocation)
	}
	if err == nil {
		err = d.Units.Kill(ctx, unit, whomMain, int32(unix.SIGUSR2))
	}
	if err != nil {
		return fail(StepSignal, err)
	}
	n, sec, err := wait(ctx, d, team, unit, invocation, highest(before))
	if err != nil {
		return fail(StepWait, err)
	}
	m := Manifest{Team: team, Backup: n, TimeMS: sec * 1000, ServerName: decl.ServerName}
	if step, werr := writePair(d, m, src); werr != nil {
		return fail(step, werr)
	}
	pruned, err := prune(d, team, keep)
	if err != nil {
		return fail(StepPrune, err)
	}
	return Result{Backup: n, TimeMS: m.TimeMS, Pruned: pruned}, nil
}

// readSources reads the five files of BK4 by the trust rule, and decodes `team.json` for the
// port and the server name.
func readSources(d Deps, team paths.TeamID) (Sources, declare.Team, error) {
	root, err := euid()
	if err != nil {
		return Sources{}, declare.Team{}, err
	}
	var s Sources
	for _, f := range []struct {
		path  string
		limit int64
		owner uint32
		into  *[]byte
	}{
		{paths.TeamDeclaration(team), capRoot, root, &s.TeamJSON},
		{paths.ServerConfig(team), capRoot, root, &s.Config},
		{paths.RosterFile(team), capCtl, d.CtlUID, &s.Roster},
		{paths.RoomsFile(team), capCtl, d.CtlUID, &s.Rooms},
		{paths.CtlState(team) + "/" + nameToken, capSmall, d.CtlUID, &s.Token},
	} {
		data, found, rerr := state.ReadOwnedFile(d.Root+f.path, f.limit, f.owner)
		if rerr != nil {
			return Sources{}, declare.Team{}, rerr
		}
		if !found {
			return Sources{}, declare.Team{}, fmt.Errorf("%s is missing", d.Root+f.path)
		}
		*f.into = data
	}
	decl, err := declare.DecodeTeamFile(s.TeamJSON)
	if err == nil && decl.ID != team {
		err = errors.New("team.json names another team")
	}
	return s, decl, err
}

// ready is BT1's first read: the unit active in one invocation, and the server answering.
func ready(ctx context.Context, d Deps, unit string, port int) (string, error) {
	st, err := d.Units.State(ctx, unit)
	if err != nil {
		return "", err
	}
	if st.ActiveState != stateOK || st.InvocationID == "" {
		return "", fmt.Errorf("%w: %s is %s", ErrNotRunning, unit, st.ActiveState)
	}
	if err := d.Ready.ServerVersion(ctx, port); err != nil {
		return "", fmt.Errorf("%w: it does not answer on port %d: %w", ErrNotRunning, port, err)
	}
	return st.InvocationID, nil
}

// sameInvocation refuses a unit that is not active in the invocation that answered (BT1).
func sameInvocation(ctx context.Context, d Deps, unit, invocation string) error {
	st, err := d.Units.State(ctx, unit)
	if err != nil {
		return err
	}
	if st.ActiveState != stateOK || st.InvocationID != invocation {
		return fmt.Errorf("%w: %s is %s in another invocation or none", ErrLost, unit, st.ActiveState)
	}
	return nil
}

// wait polls once a second, up to 60 s, for the highest number above before whose meta file
// has a whole first line (BT2); at the deadline it says whether the server was lost.
func wait(ctx context.Context, d Deps, team paths.TeamID, unit, invocation string, before int64) (int64, int64, error) {
	deadline := d.Clock.Now().Add(waitFor)
	for {
		metas, err := scanMeta(d, team)
		if err != nil {
			return 0, 0, err
		}
		var n, sec int64
		for m, s := range metas {
			if m > before && s > 0 && m > n {
				n, sec = m, s
			}
		}
		if n > 0 {
			return n, sec, nil
		}
		if !d.Clock.Now().Before(deadline) {
			timeout := errors.New("no backup appeared in 60 s (a signal before Tuwunel's handler is dropped)")
			return 0, 0, errors.Join(timeout, sameInvocation(ctx, d, unit, invocation))
		}
		if err := d.Clock.Sleep(ctx, poll); err != nil {
			return 0, 0, err
		}
	}
}

func highest(metas map[int64]int64) int64 {
	var n int64
	for m := range metas {
		n = max(n, m)
	}
	return n
}

// writePair writes the tar of m and reads it back by BK3, removing a tar that does not
// validate so no half pair looks whole (BT3).
func writePair(d Deps, m Manifest, src Sources) (string, error) {
	raw, err := Encode(m, src)
	if err == nil {
		err = backupDir(d, m.Team)
	}
	path := d.Root + paths.BackupTar(m.Team, m.Backup)
	if err == nil {
		err = state.WriteFile(path, raw, 0o600, -1)
	}
	if err != nil {
		return StepWrite, err
	}
	if _, err := readTar(d, m.Team, m.Backup); err != nil {
		return StepVerify, errors.Join(err, os.Remove(path))
	}
	return "", nil
}

// readTar reads tar n back and validates it; found is false when there is none.
func readTar(d Deps, team paths.TeamID, n int64) (Contents, error) {
	root, err := euid()
	if err != nil {
		return Contents{}, err
	}
	raw, found, err := state.ReadOwnedFile(d.Root+paths.BackupTar(team, n), maxArchive, root)
	switch {
	case err != nil:
		return Contents{}, err
	case !found:
		return Contents{}, fmt.Errorf("there is no tar of backup %d", n)
	}
	return Decode(raw, team, n)
}

// backupDir makes `root/<team>/backups/` or converges the one there to 0700, refusing a link or
// one owned by another than root (BT3).
func backupDir(d Deps, team paths.TeamID) error {
	path := d.Root + paths.BackupDir(team)
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("cannot make %s: %w", path, err)
	}
	root, err := euid()
	if err != nil {
		return err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("%s must be a directory, not a link: %w", path, err)
	}
	var st unix.Stat_t
	err = unix.Fstat(fd, &st)
	switch {
	case err != nil:
	case st.Uid != root:
		err = fmt.Errorf("%s is owned by uid %d, not %d", path, st.Uid, root)
	default:
		err = unix.Fchmod(fd, 0o700)
	}
	return errors.Join(err, unix.Close(fd))
}

// prune removes every `<m>.tar` whose database backup Tuwunel has pruned, but those in keep
// (BT4); any other name is left.
func prune(d Deps, team paths.TeamID, keep []int64) ([]int64, error) {
	metas, err := scanMeta(d, team)
	if err != nil {
		return nil, err
	}
	dir := d.Root + paths.BackupDir(team)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var pruned []int64
	for _, e := range entries {
		if !tarRe.MatchString(e.Name()) {
			continue
		}
		m, err := strconv.ParseInt(strings.TrimSuffix(e.Name(), ".tar"), 10, 64)
		if err != nil {
			return pruned, err
		}
		if _, held := metas[m]; held || slices.Contains(keep, m) {
			continue
		}
		if err := os.Remove(dir + "/" + e.Name()); err != nil {
			return pruned, err
		}
		pruned = append(pruned, m)
	}
	slices.Sort(pruned)
	return pruned, nil
}

// euid is the running euid as a file owner: root's own files must be its (0 on a host).
func euid() (uint32, error) {
	id := os.Geteuid()
	if id < 0 || id > math.MaxUint32 {
		return 0, fmt.Errorf("the euid %d is not a file owner", id)
	}
	return uint32(id), nil
}

// Run is the record of a team's last backup run, `backups/last-run.json` (BT5, n2g-D3): the
// result, when, the backup on success (0 for none) and the one-line error on failure.
type Run struct {
	Result string
	AtMS   int64
	Backup int64
	Error  string
}

const runVersion = 1

// Value is the record as JSON.
func (r Run) Value() proto.Value {
	backup, line := proto.Null(), proto.Null()
	if r.Backup > 0 {
		backup = proto.Int(r.Backup)
	}
	if r.Error != "" {
		line = proto.Str(r.Error)
	}
	return proto.Obj(
		proto.M("v", proto.Int(runVersion)), proto.M("result", proto.Str(r.Result)),
		proto.M("at_ms", proto.Int(r.AtMS)), proto.M("backup", backup), proto.M("error", line),
	)
}

var runKeys = []string{"v", "result", "at_ms", "backup", "error"}

func decodeRun(raw []byte) (Run, error) {
	v, err := proto.Parse(raw)
	if err != nil || v.Kind() != proto.KindObject || len(v.Members()) != len(runKeys) {
		return Run{}, errors.New("is not an object of exactly v, result, at_ms, backup and error")
	}
	got := map[string]proto.Value{}
	for _, m := range v.Members() {
		got[m.Key] = m.Val
	}
	ver, result, at, backup, line := got["v"], got["result"], got["at_ms"], got["backup"], got["error"]
	r := Run{Result: result.Str(), AtMS: at.Int(), Backup: backup.Int(), Error: line.Str()}
	head := ver.Kind() == proto.KindInt && ver.Int() == runVersion && result.Kind() == proto.KindString &&
		at.Kind() == proto.KindInt && at.Int() >= 0
	if !head || !runBody(r, backup, line) {
		return Run{}, errors.New("is not a record of an ok or a failed run")
	}
	return r, nil
}

// runBody is an ok run with its backup and no error, or a failed one with its error and none.
func runBody(r Run, backup, line proto.Value) bool {
	switch r.Result {
	case status.RunOK:
		return backup.Kind() == proto.KindInt && r.Backup > 0 && line.Kind() == proto.KindNull
	case status.RunFailed:
		return backup.Kind() == proto.KindNull && line.Kind() == proto.KindString && status.OneLine(r.Error)
	}
	return false
}

// LastRun is the team's last run record; found is false when no run has written one.
func LastRun(d Deps, team paths.TeamID) (Run, bool, error) {
	root, err := euid()
	if err != nil {
		return Run{}, false, err
	}
	path := d.Root + paths.BackupLastRun(team)
	raw, found, err := state.ReadOwnedFile(path, capSmall, root)
	if err != nil || !found {
		return Run{}, false, err
	}
	r, err := decodeRun(raw)
	if err != nil {
		return Run{}, false, fmt.Errorf("%s %w", path, err)
	}
	return r, true, nil
}

func writeLastRun(d Deps, team paths.TeamID, r Run) error {
	if err := backupDir(d, team); err != nil {
		return err
	}
	return state.WriteFile(d.Root+paths.BackupLastRun(team), r.Value().Compact(), 0o600, -1)
}

// oneLine is an error as the record's one printable line: its lines joined, each character
// that is not printable replaced, cut to status.OneLine's 512 bytes.
func oneLine(err error) string {
	var b strings.Builder
	for _, r := range strings.ReplaceAll(err.Error(), "\n", "; ") {
		if r == utf8.RuneError || unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) || r == ' ' || r == ' ' {
			r = '?'
		}
		if b.Len()+utf8.RuneLen(r) > 512 {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}
