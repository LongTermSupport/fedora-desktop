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
//     its image and the image is over 30 days old (podman records no last use, so the age is the
//     image's; ccy's daily in-place update makes a new one on each day a project is used). The
//     project's next launch rebuilds it. A tag of an image with other names only untags it.
//   - P3. A container another tool made (buildah's working container, left by an interrupted
//     build) is removed when it is over a day old and no ccy image build holds its lock
//     ($XDG_RUNTIME_DIR/ccy/image-builds/*.lock, lib/image-build-lock.bash).
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
	kindImage  = "image"
	kindTag    = "tag"
	kindCtr    = "container"
	usage      = `usage: container-images prune [--yes]

  prune          list what the daily prune would remove from the podman store (a dry run)
  prune --yes    remove it: dangling images no container uses, over a day old; claude-yolo
                 project images no container uses, over 30 days old; buildah working
                 containers over a day old while no ccy image build runs. Dangling volumes
                 are listed, never removed. Each removal is logged to
                 ~/.local/state/fedora-desktop/container-images.log.
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
	switch {
	case len(args) == 1 && (args[0] == "--help" || args[0] == "-h"):
		return a.print(a.Env.Stdout, usage)
	case len(args) >= 1 && args[0] == "prune" && (len(args) == 1 || len(args) == 2 && args[1] == "--yes"):
		return a.prune(ctx, len(args) == 2)
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
		case s.building != "":
			keeps = append(keeps, keep{what, "a ccy image build is running (" + s.building + " is held)"})
		case age < danglingAge:
			keeps = append(keeps, keep{what, "created " + age.String() + " ago, under a day: a build may still be using it"})
		default:
			acts = append(acts, action{
				kind: kindCtr, ref: c.ID, name: cmp.Or(c.Name, c.ID), id: c.ID,
				why: "a " + cmp.Or(c.Command, "storage") + " working container over a day old, left by a build that did not finish",
			})
		}
	}
	for _, img := range s.images {
		acts, keeps = planTags(now, img, users[img.ID], acts, keeps)
	}
	for _, img := range s.dangling {
		what := kindImage + " " + short(img.ID)
		switch age := now.Sub(img.Created); {
		case len(users[img.ID]) > 0:
			keeps = append(keeps, keep{what, "used by container " + strings.Join(users[img.ID], ", ")})
		case img.Containers > 0:
			keeps = append(keeps, keep{what, "podman counts " + strconv.Itoa(img.Containers) + " container using it"})
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
func planTags(now time.Time, img engine.StoreImage, users []string, acts []action, keeps []keep) ([]action, []keep) {
	for _, name := range img.Names {
		if !projectTag(name) {
			continue
		}
		what := kindTag + " " + name
		days := int(now.Sub(img.Created) / (24 * time.Hour))
		switch {
		case len(users) > 0:
			keeps = append(keeps, keep{what, "used by container " + strings.Join(users, ", ")})
		case img.Containers > 0:
			keeps = append(keeps, keep{what, "podman counts " + strconv.Itoa(img.Containers) + " container using it"})
		case now.Sub(img.Created) < staleTagAge:
			keeps = append(keeps, keep{what, "created " + strconv.Itoa(days) + " days ago, under 30"})
		default:
			size := img.Size
			if len(img.Names) > 1 {
				size = 0
			}
			acts = append(acts, action{
				kind: kindTag, ref: name, name: name, id: img.ID, size: size, labels: img.Labels,
				why: "a " + repository + " project image used by no container, created " + strconv.Itoa(days) + " days ago",
			})
		}
	}
	return acts, keeps
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
}

func (a App) prune(ctx context.Context, yes bool) term.ExitCode {
	eng, err := engine.New(engine.Podman, a.Env.Runner)
	if err != nil {
		return a.fail(err)
	}
	r := &run{App: a, eng: eng, yes: yes, seen: map[keep]bool{}}
	if yes {
		if r.log, err = a.openLog(); err != nil {
			return a.fail(err)
		}
		defer func() {
			if cerr := r.log.Close(); cerr != nil {
				r.failed = append(r.failed, "the log: "+cerr.Error())
			}
		}()
	} else {
		r.p.Print(a.Env.Stdout, "container-images prune: a dry run, nothing is removed. prune --yes removes what is listed.\n")
	}
	volumes, err := eng.DanglingVolumes(ctx)
	if err != nil {
		return a.fail(err)
	}
	code := r.passes(ctx)
	if code != term.ExitOK && len(r.failed) == 0 {
		return code
	}
	if len(volumes) > 0 {
		r.p.Print(a.Env.Stdout, fmt.Sprintf("dangling volumes: %d, never removed by this command: %s\n", len(volumes), strings.Join(volumes, " ")))
	}
	verb := "would free"
	if yes {
		verb = "freed"
	}
	r.p.Print(a.Env.Stdout, fmt.Sprintf("%s about %s (image sizes summed; layers they share are counted again)\n", verb, humanBytes(r.freed)))
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
		for _, k := range keeps {
			if !r.seen[k] {
				r.seen[k] = true
				r.p.Print(r.Env.Stdout, "keep "+k.what+": "+k.why+"\n")
			}
		}
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
	s.building, err = heldBuildLock(filepath.Join(playlock.RuntimeDir(r.Env.Getenv, r.Env.UID), "ccy", "image-builds"))
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

func (a App) openLog() (*os.File, error) {
	dir := filepath.Join(a.Env.Home, ".local", "state", "fedora-desktop")
	path := filepath.Join(dir, logName)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("cannot open the removal log %s: %w", path, err)
	}
	f, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("cannot open the removal log %s: %w", path, err)
	}
	return f, nil
}

// heldBuildLock is the name of a ccy image build lock some process holds, "" when none is: each
// *.lock is probed with a shared lock that never waits and is let go at once.
func heldBuildLock(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("cannot read the ccy image build locks in %s: %w", dir, err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".lock") {
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
