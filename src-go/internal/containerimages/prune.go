// Package containerimages is the container-images command (Plan 00235): what nothing uses is
// removed from the rootless podman store. `prune` is the daily catch-all a user timer runs;
// without --yes it is a dry run that lists what would go.
//
// Intended behaviour, each held by a test:
//
//   - P1. A dangling image (no tag, no image built on it) is removed when no container, running,
//     stopped or another tool's, uses it, podman counts none, and it is over a day old, so a
//     build in flight is never touched. Removal is podman's own unforced `image rm`.
//   - P2. A claude-yolo:<project> tag (any but base and latest) is removed when no container uses
//     its image, the image was created over 30 days ago, and the project was last launched over
//     30 days ago: ccy touches ~/.cache/claude-yolo-update-checks/claude-yolo_<project> on every
//     launch day, and with no such stamp the creation alone decides. The project's next launch
//     rebuilds it. A tag of an image with other names only untags it.
//   - P3. buildah's working container (podman lists it in the "storage" state with the command
//     "buildah"), left by an interrupted build, is removed when it is over a day old. Any other
//     "storage" container, one whose record podman's database lost (a toolbox, say), is named
//     and kept.
//   - P3a. While a ccy image build holds its lock ($XDG_RUNTIME_DIR/ccy/image-builds/*.lock,
//     lib/image-build-lock.bash), no image and no buildah container is removed, and the run says
//     so on stderr: a `--layers` cache hit can reuse an old dangling image.
//   - P3b. One `prune --yes` at a time: a second refuses while the first holds
//     ~/.local/state/fedora-desktop/container-images.lock. Only a `prune --yes` that succeeded
//     writes container-images.succeeded beside it, the play's sign that the first run is done.
//   - P4. Dangling volumes are listed (count and names) and never removed: a volume is data.
//   - P5. `podman system df` is never asked: it fails on a store holding a volume owned outside
//     the user's subuid range.
//   - P6. Each removal is appended to ~/.local/state/fedora-desktop/container-images.log (time,
//     kind, what, id, size, labels, why; tab-separated) only after it succeeded. A failed one is
//     named on stderr and not logged, and the run exits 1. A log that cannot be opened stops the
//     run before anything is removed.
//   - P7. With --yes the run looks again after each pass, since a removed image can leave its
//     parent dangling, until a pass removes nothing; the passes are bounded, and a run that
//     still finds work at the bound fails.
//   - P8. Any list the engine cannot give stops the run before a removal.
package containerimages

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/playlock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	danglingAge = 24 * time.Hour
	staleTagAge = 30 * 24 * time.Hour
	// maxPasses bounds the passes of one run (P7): far more than the depth of any image chain.
	maxPasses  = 20
	repository = "claude-yolo"
	shortID    = 12
	logName    = "container-images.log"
	lockName   = "container-images.lock"
	stampName  = "container-images.succeeded"
	// launchStamps is the ccy launcher's per-image update-check stamps (VERSION_CHECK_CACHE).
	launchStamps = ".cache/claude-yolo-update-checks"
	kindImage    = "image"
	kindTag      = "tag"
	kindCtr      = "container"
	usage        = `usage: container-images prune [--yes]
       container-images [--engine podman|docker] before <repository>
       container-images [--engine podman|docker] replaced [--lock <file>] <repository> <ids-file>

  prune          list what the daily prune would remove from the podman store (a dry run)
  prune --yes    remove it: dangling images no container uses, over a day old; claude-yolo
                 project images no container uses, built and last launched over 30
                 days ago; buildah working
                 containers over a day old while no ccy image build runs. Dangling volumes
                 are listed, never removed. Each removal is logged to
                 ~/.local/state/fedora-desktop/container-images.log.
  before         print each tag of the repository with its image id (ccy's build lock
                 records this before a build)
  replaced       remove each image of that record no tag now names and no container uses
                 (ccy's build lock runs it after a build); ids-file may be /dev/stdin.
                 While a ccy image build lock other than --lock's is held, it removes
                 nothing. The engine is --engine's, else CCY_CONTAINER_ENGINE's, else podman.
`
)

// Env is what a run reaches outside itself.
type Env struct {
	Runner   execx.Runner
	Getenv   func(string) string
	LookPath func(string) (string, error)
	Now      func() time.Time
	Home     string
	UID      int
	Stdout   io.Writer
	Stderr   io.Writer
}

// App is the command.
type App struct{ Env Env }

// Run runs the command line.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	req, rest, selfPrune := selfPruneArgs(a.Env.Getenv(engine.EnvName), args)
	switch {
	case len(args) == 1 && (args[0] == "--help" || args[0] == "-h"):
		return a.print(a.Env.Stdout, usage)
	case len(args) >= 1 && args[0] == "prune" && (len(args) == 1 || len(args) == 2 && args[1] == "--yes"):
		return a.prune(ctx, len(args) == 2)
	case selfPrune:
		return a.selfPrune(ctx, req, rest)
	default:
		if code := a.print(a.Env.Stderr, usage); code != term.ExitOK {
			return code
		}
		return term.ExitUsage
	}
}

func (a App) print(w io.Writer, text string) term.ExitCode {
	if _, err := io.WriteString(w, text); err != nil {
		return term.ExitFailure
	}
	return term.ExitOK
}

// action is one removal: what goes, and why.
type action struct {
	kind   string
	ref    string // what the engine is told to remove: an id, a tag, a container id
	name   string // what a person reads: a short id, the tag, the container's name
	id     string
	size   int64 // what the removal frees at most; 0 for a tag whose image keeps other names
	labels map[string]string
	why    string
}

// keep is one thing a pass looked at and leaves, with why.
type keep struct{ what, why string }

// store is one pass's view of the engine's store.
type store struct {
	dangling   []engine.StoreImage
	images     []engine.StoreImage
	containers []engine.StoreContainer
	// building is the build lock held, "" when no ccy image build runs.
	building string
	// launched is each project tag's last ccy launch, absent when none is recorded.
	launched map[string]time.Time
}

// buildRunning is why nothing is removed while a ccy image build runs (P3a).
func (s store) buildRunning() string {
	return "a ccy image build is running (" + s.building + " is held)"
}

// plan is what a pass removes and keeps (P1-P3), containers first: a buildah working container
// holds the image it was made from.
func plan(now time.Time, s store) ([]action, []keep) {
	users := map[string][]string{}
	for _, c := range s.containers {
		users[c.ImageID] = append(users[c.ImageID], cmp.Or(c.Name, c.ID))
	}
	var acts []action
	var keeps []keep
	for _, c := range s.containers {
		if !c.External {
			continue
		}
		what := kindCtr + " " + cmp.Or(c.Name, c.ID)
		switch age := now.Sub(c.Created); {
		case c.Command != "buildah":
			keeps = append(keeps, keep{what, "not buildah's working container (its command is " + cmp.Or(c.Command, "unknown") +
				"): podman's record of it is gone; left for a person"})
		case s.building != "":
			keeps = append(keeps, keep{what, s.buildRunning()})
		case age < danglingAge:
			keeps = append(keeps, keep{what, "created " + age.String() + " ago, under a day: a build may still be using it"})
		default:
			acts = append(acts, action{
				kind: kindCtr, ref: c.ID, name: cmp.Or(c.Name, c.ID), id: c.ID,
				why: "a buildah working container over a day old, left by a build that did not finish",
			})
		}
	}
	for _, img := range s.images {
		acts, keeps = planTags(now, s, img, users[img.ID], acts, keeps)
	}
	for _, img := range s.dangling {
		what := kindImage + " " + short(img.ID)
		switch age := now.Sub(img.Created); {
		case len(users[img.ID]) > 0:
			keeps = append(keeps, keep{what, "used by container " + strings.Join(users[img.ID], ", ")})
		case img.Containers > 0:
			keeps = append(keeps, keep{what, "podman counts " + strconv.Itoa(img.Containers) + " container using it"})
		case s.building != "":
			keeps = append(keeps, keep{what, s.buildRunning()})
		case age < danglingAge:
			keeps = append(keeps, keep{what, "created " + age.String() + " ago, under a day: a build may still be using it"})
		default:
			acts = append(acts, action{
				kind: kindImage, ref: img.ID, name: short(img.ID), id: img.ID, size: img.Size,
				labels: img.Labels, why: "dangling (no tag), used by no container, created " + img.Created.UTC().Format(time.DateOnly),
			})
		}
	}
	return acts, keeps
}

// planTags is P2 for one image's names.
func planTags(now time.Time, s store, img engine.StoreImage, users []string, acts []action, keeps []keep) ([]action, []keep) {
	for _, name := range img.Names {
		if !projectTag(name) {
			continue
		}
		what := kindTag + " " + name
		days := wholeDays(now.Sub(img.Created))
		launched, wasLaunched := s.launched[name]
		lastUse := "no launch recorded"
		if wasLaunched {
			lastUse = "last launched " + wholeDays(now.Sub(launched)) + " days ago"
		}
		switch {
		case len(users) > 0:
			keeps = append(keeps, keep{what, "used by container " + strings.Join(users, ", ")})
		case img.Containers > 0:
			keeps = append(keeps, keep{what, "podman counts " + strconv.Itoa(img.Containers) + " container using it"})
		case s.building != "":
			keeps = append(keeps, keep{what, s.buildRunning()})
		case now.Sub(img.Created) < staleTagAge:
			keeps = append(keeps, keep{what, "created " + days + " days ago, under 30"})
		case wasLaunched && now.Sub(launched) < staleTagAge:
			keeps = append(keeps, keep{what, lastUse + ", under 30"})
		default:
			size := img.Size
			if len(img.Names) > 1 {
				size = 0
			}
			acts = append(acts, action{
				kind: kindTag, ref: name, name: name, id: img.ID, size: size, labels: img.Labels,
				why: "a " + repository + " project image used by no container, created " + days + " days ago, " + lastUse,
			})
		}
	}
	return acts, keeps
}

func wholeDays(d time.Duration) string { return strconv.Itoa(int(d / (24 * time.Hour))) }

// launchStamp is the file the ccy launcher touches on each launch day of a project image: its
// image name with ':' and '/' made '_' (auto_update_claude_code).
func launchStamp(home, tag string) string {
	name := strings.NewReplacer(":", "_", "/", "_").Replace(strings.TrimPrefix(tag, "localhost/"))
	return filepath.Join(home, launchStamps, name)
}

// launches reads the last launch of each project tag (P2); a stamp that cannot be read stops
// the run, since a missing reading could remove an image in use.
func launches(home string, images []engine.StoreImage) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	for _, img := range images {
		for _, name := range img.Names {
			if !projectTag(name) {
				continue
			}
			path := launchStamp(home, name)
			info, err := os.Stat(path)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("cannot read the ccy launch stamp %s: %w", path, err)
			}
			out[name] = info.ModTime()
		}
	}
	return out, nil
}

// projectTag is a claude-yolo tag other than base and latest.
func projectTag(name string) bool {
	repo, tag, ok := cutLast(name, ":")
	repo = strings.TrimPrefix(repo, "localhost/")
	return ok && repo == repository && tag != "base" && tag != "latest" && tag != ""
}

func cutLast(s, sep string) (string, string, bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}

func short(id string) string { return id[:min(shortID, len(id))] }

// run is one prune.
type run struct {
	App

	eng     engine.Engine
	yes     bool
	log     *os.File
	p       term.Printer
	failed  []string
	freed   int64
	removed int
	seen    map[keep]bool
	// toldBuilding is a run that has said on stderr that a ccy image build is running.
	toldBuilding bool
}

func (a App) prune(ctx context.Context, yes bool) term.ExitCode {
	eng, err := engine.New(engine.Podman, a.Env.Runner)
	if err != nil {
		return a.fail(err)
	}
	r := &run{App: a, eng: eng, yes: yes, seen: map[keep]bool{}}
	if !yes {
		r.p.Print(a.Env.Stdout, "container-images prune: a dry run, nothing is removed. prune --yes removes what is listed.\n")
		return r.body(ctx)
	}
	release, err := a.takeRunLock()
	if err != nil {
		return a.fail(err)
	}
	defer release()
	if r.log, err = a.openLog(); err != nil {
		return a.fail(err)
	}
	code := r.body(ctx)
	if err := r.log.Close(); err != nil {
		return a.fail(fmt.Errorf("close the removal log %s: %w", r.log.Name(), err))
	}
	if code != term.ExitOK {
		return code
	}
	return a.stampSuccess()
}

// body is one run after its setup: the passes, then the report.
func (r *run) body(ctx context.Context) term.ExitCode {
	volumes, err := r.eng.DanglingVolumes(ctx)
	if err != nil {
		return r.App.fail(err)
	}
	code := r.passes(ctx)
	if len(volumes) > 0 {
		r.p.Print(r.Env.Stdout, fmt.Sprintf("dangling volumes: %d, never removed by this command: %s\n", len(volumes), strings.Join(volumes, " ")))
	}
	verb := "would free"
	if r.yes {
		verb = "freed"
	}
	r.p.Print(r.Env.Stdout, fmt.Sprintf("%s about %s (image sizes summed; layers they share are counted again)\n", verb, humanBytes(r.freed)))
	return r.finish(code)
}

// passes runs passes until one removes nothing (P7); a dry run makes one.
func (r *run) passes(ctx context.Context) term.ExitCode {
	for range maxPasses {
		s, err := r.look(ctx)
		if err != nil {
			return r.fail(err)
		}
		acts, keeps := plan(r.Env.Now(), s)
		r.tell(s, keeps)
		if len(acts) == 0 {
			return term.ExitOK
		}
		for _, act := range acts {
			if err := r.act(ctx, act); err != nil {
				return r.fail(err)
			}
		}
		if !r.yes || len(r.failed) > 0 {
			return term.ExitOK
		}
	}
	r.failed = append(r.failed, fmt.Sprintf("after %d passes the prune still finds images to remove; run it again, or look at what podman keeps", maxPasses))
	return term.ExitFailure
}

// tell names a running ccy image build once, on stderr (P3a), and each kept thing once.
func (r *run) tell(s store, keeps []keep) {
	if s.building != "" && !r.toldBuilding {
		r.toldBuilding = true
		r.p.Print(r.Env.Stderr, "container-images: "+s.buildRunning()+": this run removes no image and no buildah container\n")
	}
	for _, k := range keeps {
		if !r.seen[k] {
			r.seen[k] = true
			r.p.Print(r.Env.Stdout, "keep "+k.what+": "+k.why+"\n")
		}
	}
}

// look reads the store for one pass (P8).
func (r *run) look(ctx context.Context) (store, error) {
	var s store
	var err error
	if s.containers, err = r.eng.StoreContainers(ctx); err != nil {
		return s, err
	}
	if s.images, err = r.eng.StoreImages(ctx, false); err != nil {
		return s, err
	}
	if s.dangling, err = r.eng.StoreImages(ctx, true); err != nil {
		return s, err
	}
	if s.launched, err = launches(r.Env.Home, s.images); err != nil {
		return s, err
	}
	s.building, err = heldBuildLock(r.buildLocks(), "")
	return s, err
}

// act removes one thing, or in a dry run says it would; only a log write that fails is an
// error, since a removal that is not recorded cannot be reported afterwards (P6).
func (r *run) act(ctx context.Context, act action) error {
	size := ""
	if act.size > 0 {
		size = " (" + humanBytes(act.size) + ")"
	}
	if !r.yes {
		r.p.Print(r.Env.Stdout, "would remove "+act.kind+" "+act.name+size+": "+act.why+"\n")
		r.freed += act.size
		return nil
	}
	if !r.remove(ctx, act) {
		return nil
	}
	r.removed++
	r.freed += act.size
	verb := "removed " + act.kind + " " + act.name + size
	if act.kind == kindTag && act.size == 0 {
		verb = "untagged " + act.name + ": it keeps its other names"
	}
	r.p.Print(r.Env.Stdout, verb+": "+act.why+"\n")
	return r.record(act)
}

// remove is one removal; a failed one is noted for the end of the run, by name.
func (r *run) remove(ctx context.Context, act action) bool {
	var err error
	if act.kind == kindCtr {
		err = r.eng.RemoveExternalContainer(ctx, act.ref)
	} else {
		err = r.eng.RemoveImage(ctx, act.ref)
	}
	if err != nil {
		r.failed = append(r.failed, "could not remove "+act.kind+" "+act.name+": "+err.Error())
	}
	return err == nil
}

func (r *run) record(act action) error {
	labels := slices.Sorted(maps.Keys(act.labels))
	pairs := make([]string, 0, len(labels))
	for _, k := range labels {
		pairs = append(pairs, k+"="+act.labels[k])
	}
	fields := []string{
		r.Env.Now().UTC().Format(time.RFC3339), act.kind, act.name, act.id,
		strconv.FormatInt(act.size, 10), strings.Join(pairs, ","), act.why,
	}
	for i, f := range fields {
		fields[i] = strings.NewReplacer("\t", " ", "\n", " ").Replace(f)
	}
	if _, err := io.WriteString(r.log, strings.Join(fields, "\t")+"\n"); err != nil {
		return fmt.Errorf("could not write %s: %w; the removal of %s %s is not recorded", r.log.Name(), err, act.kind, act.name)
	}
	return nil
}

func (r *run) finish(code term.ExitCode) term.ExitCode {
	for _, f := range r.failed {
		r.p.Print(r.Env.Stderr, "container-images: "+f+"\n")
	}
	if r.p.Err() != nil {
		return term.ExitFailure
	}
	if len(r.failed) > 0 {
		return term.ExitFailure
	}
	return code
}

func (r *run) fail(err error) term.ExitCode {
	r.failed = append(r.failed, err.Error())
	return term.ExitFailure
}

func (a App) fail(err error) term.ExitCode {
	if _, werr := fmt.Fprintf(a.Env.Stderr, "container-images: %v\n", err); werr != nil {
		return term.ExitFailure
	}
	return term.ExitFailure
}

func (a App) stateDir() string { return filepath.Join(a.Env.Home, ".local", "state", "fedora-desktop") }

// stateFile is the path of name in the state directory, made first: a fresh home has no
// ~/.local/state. what names the file in the error.
func (a App) stateFile(name, what string) (string, error) {
	if err := os.MkdirAll(a.stateDir(), 0o750); err != nil {
		return "", fmt.Errorf("cannot make %s for the %s: %w", a.stateDir(), what, err)
	}
	return filepath.Join(a.stateDir(), name), nil
}

// takeRunLock is P3b: one `prune --yes` at a time. The lock is held until release.
func (a App) takeRunLock() (func(), error) {
	path, err := a.stateFile(lockName, "run lock")
	if err != nil {
		return nil, err
	}
	fh, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("cannot open the run lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(fh.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		msg := fmt.Errorf("cannot take the run lock %s: %w", path, err)
		if errors.Is(err, syscall.EWOULDBLOCK) {
			msg = fmt.Errorf("another container-images prune --yes is running (%s is held); this run removes nothing", path)
		}
		return nil, errors.Join(msg, fh.Close())
	}
	// Closing the descriptor lets the lock go; a close that fails after the run has nothing
	// left to protect.
	return func() {
		if err := fh.Close(); err != nil {
			a.fail(fmt.Errorf("close the run lock %s: %w", path, err))
		}
	}, nil
}

// stampSuccess records a `prune --yes` that succeeded (P3b): the time, in the stamp file.
func (a App) stampSuccess() term.ExitCode {
	path := filepath.Join(a.stateDir(), stampName)
	if err := os.WriteFile(path, []byte(a.Env.Now().UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		return a.fail(fmt.Errorf("the prune succeeded but its stamp %s could not be written: %w", path, err))
	}
	return term.ExitOK
}

// openLog is the removal log both prune and replaced append to (P6, R2).
func (a App) openLog() (*os.File, error) {
	path, err := a.stateFile(logName, "removal log")
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("cannot open the removal log %s: %w", path, err)
	}
	return f, nil
}

// buildLocks is the directory of the ccy image build locks (lib/image-build-lock.bash).
func (a App) buildLocks() string {
	return filepath.Join(playlock.RuntimeDir(a.Env.Getenv, a.Env.UID), "ccy", "image-builds")
}

// heldBuildLock is the name of a ccy image build lock some process holds, "" when none is: each
// *.lock but the path except (the caller's own, "" for none) is probed with a shared lock that
// never waits and is let go at once.
func heldBuildLock(dir, except string) (string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("cannot read the ccy image build locks in %s: %w", dir, err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".lock") || except != "" && filepath.Join(dir, e.Name()) == filepath.Clean(except) {
			continue
		}
		held, err := lockHeld(filepath.Join(dir, e.Name()))
		if err != nil {
			return "", err
		}
		if held {
			return e.Name(), nil
		}
	}
	return "", nil
}

func lockHeld(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, fmt.Errorf("cannot read the ccy image build lock %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("the ccy image build lock %s is not a regular file", path)
	}
	fh, err := os.OpenFile(filepath.Clean(path), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return false, fmt.Errorf("cannot open the ccy image build lock %s: %w", path, err)
	}
	flockErr := syscall.Flock(int(fh.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
	held := errors.Is(flockErr, syscall.EWOULDBLOCK)
	if flockErr != nil && !held {
		return false, errors.Join(fmt.Errorf("cannot probe the ccy image build lock %s: %w", path, flockErr), fh.Close())
	}
	if err := fh.Close(); err != nil {
		return false, fmt.Errorf("close the ccy image build lock %s: %w", path, err)
	}
	return held, nil
}

// humanBytes is n in binary units, one decimal.
func humanBytes(n int64) string {
	if n < 1024 {
		return strconv.FormatInt(n, 10) + " B"
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	v, i := float64(n)/1024, 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}
