package containerimages

// stale.go is the stale-image rule (Plan 00235 Q7, the owner's answer): ccy keeps its images up
// to date itself, so an image an older ccy built is stale the moment the installed ccy moves
// on. Every image ccy builds or updates carries the label ccy-version=<the CCY_VERSION that made
// it>; `stale [--lock <file>] [--yes] <installed CCY_VERSION>` is run by ccy's build lock after
// each build (lib/image-build-prune.bash), which play-claude-yolo.yml's build of :latest runs
// too, right after it installs a new ccy.
//
//   - S1. A ccy image is one with a ccy-version or claude-yolo-version label (the Dockerfile's,
//     which every project image built on ccy's inherits) or a name in the claude-yolo repository.
//   - S2. It is stale when its ccy-version is an older version than the installed one, or it
//     has none (built before ccy labelled its images). One that is not a version, or is newer,
//     is not.
//   - S3. A stale image's claude-yolo:<project> tags are removed, whatever its age, when no
//     container (running, stopped or another tool's) uses it, podman counts none, and no image
//     is built on it; the project's next launch rebuilds it. claude-yolo:base and :latest are
//     never removed: ccy rebuilds them itself. A stale dangling image no container uses is the
//     daily prune's (P1) and is passed over in silence.
//   - S4. A stale image a container uses is kept, and so is the container: ccy never removes
//     one. Each container is named on stderr; a stopped one (an old session keeping a stale
//     image alive) is also appended to the removal log as a `held` line, with --yes.
//   - S5. Removals wait for play-podman.yml's store-clean marker (P9; said once, exit 0) and
//     for no ccy image build lock but --lock's to be held (R6). Without --yes nothing is removed
//     and nothing is logged: each removal is named as one that would be.
//   - S6. Everything is said on stderr. A list the engine cannot give stops the run before a
//     removal, exit 1; a refused removal is named, the others still run, and the run exits 1.
//     Each removal is logged as P6. The rule reads podman's store only: under docker it says so
//     and exits 0.

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	verbStale = "stale"
	// labelCCYVersion is the label every image ccy builds carries: the CCY_VERSION that made it.
	labelCCYVersion = "ccy-version"
	// labelContainerVersion is the Dockerfile's own label, the ccy container's version.
	labelContainerVersion = "claude-yolo-version"
	kindHeld              = "held"
)

var version = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)

// staleArgs reads `[--lock <file>] [--yes] <version>` as the verb, version, lock ("" for none)
// and "yes" ("" without it).
func staleArgs(args []string) ([]string, bool) {
	lock, yes := "", ""
	for len(args) > 1 {
		switch {
		case args[0] == "--lock" && lock == "" && args[1] != "":
			lock, args = args[1], args[2:]
		case args[0] == flagYes && yes == "":
			yes, args = "yes", args[1:]
		default:
			return nil, false
		}
	}
	if len(args) != 1 || !version.MatchString(args[0]) {
		return nil, false
	}
	return []string{verbStale, args[0], lock, yes}, true
}

// olderVersion is a before b, part by part as numbers; a missing part is 0.
func olderVersion(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(pa), len(pb)) {
		x, y := part(pa, i), part(pb, i)
		if x != y {
			return x < y
		}
	}
	return false
}

func part(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, err := strconv.Atoi(parts[i])
	if err != nil {
		return 0
	}
	return n
}

// staleBecause is why an image is stale (S1, S2), "" when it is not.
func staleBecause(img engine.StoreImage, installed string) string {
	built, labelled := img.Labels[labelCCYVersion]
	ccys := labelled || img.Labels[labelContainerVersion] != "" || slices.ContainsFunc(img.Names, inRepository)
	switch {
	case !ccys:
		return ""
	case !labelled:
		return "built before ccy labelled its images (" + labelCCYVersion + ")"
	case version.MatchString(built) && olderVersion(built, installed):
		return "built by ccy " + built + ", older than the installed " + installed
	default:
		return ""
	}
}

func inRepository(name string) bool {
	repo, _, ok := cutLast(name, ":")
	return ok && strings.TrimPrefix(repo, "localhost/") == repository
}

// staleRun is one stale run's judgement: what goes, what stays and why, the held lines.
type staleRun struct {
	*run

	removals []action
	held     []action
}

func (a App) stale(ctx context.Context, eng engine.Engine, installed, ownLock string, yes bool) term.ExitCode {
	r := &staleRun{run: &run{App: a, eng: eng, yes: yes}}
	containers, err := eng.StoreContainers(ctx)
	if err != nil {
		return r.finish(r.fail(err))
	}
	images, err := eng.StoreImages(ctx, false)
	if err != nil {
		return r.finish(r.fail(err))
	}
	layers, err := imageLayers(ctx, eng, images, nil)
	if err != nil {
		return r.finish(r.fail(err))
	}
	s := store{images: images, layers: layers}
	for _, img := range images {
		if why := staleBecause(img, installed); why != "" {
			r.judge(s, img, containers, why)
		}
	}
	return r.act(ctx, ownLock)
}

// judge is S3 and S4 for one stale image.
func (r *staleRun) judge(s store, img engine.StoreImage, containers []engine.StoreContainer, why string) {
	what, names := kindImage+" "+short(img.ID), []string(nil)
	if !slices.ContainsFunc(img.Names, baseOrLatest) {
		names = slices.DeleteFunc(slices.Clone(img.Names), func(n string) bool { return !projectTag(n) })
	}
	if len(names) > 0 {
		what = kindTag + " " + strings.Join(names, ", ")
	}
	what += sizeNote(img.Size)
	used := r.holders(img, what, containers, why)
	child, known := builtOn(s, img.ID)
	switch {
	case used || len(names) == 0:
	case img.Containers > 0:
		r.say("keep " + what + ": " + why + ", but podman counts " + strconv.Itoa(img.Containers) + " container using it")
	case !known:
		r.say("keep " + what + ": " + why + ", but podman did not report its layers")
	case child != "":
		r.say("keep " + what + ": " + why + ", but image " + short(child) + " is built on it")
	default:
		for i, name := range names {
			size := int64(0)
			if i == len(names)-1 && len(names) == len(img.Names) {
				size = img.Size
			}
			r.removals = append(r.removals, action{
				kind: kindTag, ref: name, name: name, id: img.ID, size: size, labels: img.Labels,
				why: why + ", and no container uses it",
			})
		}
	}
}

// holders names each container using a stale image (S4) and is whether there is one.
func (r *staleRun) holders(img engine.StoreImage, what string, containers []engine.StoreContainer, why string) bool {
	used := false
	for _, c := range containers {
		if c.ImageID != img.ID {
			continue
		}
		used = true
		name := c.Name
		if name == "" {
			name = c.ID
		}
		if c.State == "running" {
			r.say("keep " + what + ": " + why + ", but the running container " + name + " uses it")
			continue
		}
		line := why + ", but the stopped container " + name + " (" + c.State + ", created " +
			c.Created.UTC().Format(time.DateOnly) + ") uses it; ccy never removes a container: remove the container to let the image go"
		r.say("keep " + what + ": " + line)
		r.held = append(r.held, action{kind: kindHeld, name: name, id: img.ID, size: img.Size, labels: img.Labels, why: line})
	}
	return used
}

// act logs the held lines and makes the removals (S5, S6).
func (r *staleRun) act(ctx context.Context, ownLock string) term.ExitCode {
	if !r.yes {
		for _, act := range r.removals {
			r.say("would remove " + act.kind + " " + act.name + sizeNote(act.size) + ": " + act.why)
		}
		return r.finish(term.ExitOK)
	}
	if len(r.held) == 0 && len(r.removals) == 0 {
		return r.finish(term.ExitOK)
	}
	var err error
	if r.log, err = r.openLog(); err != nil {
		return r.finish(r.fail(err))
	}
	code := r.logAndRemove(ctx, ownLock)
	if err := r.log.Close(); err != nil {
		return r.finish(r.fail(err))
	}
	return code
}

func (r *staleRun) logAndRemove(ctx context.Context, ownLock string) term.ExitCode {
	for _, h := range r.held {
		if err := r.record(h); err != nil {
			return r.finish(r.fail(err))
		}
	}
	if len(r.removals) == 0 {
		return r.finish(term.ExitOK)
	}
	clean, err := r.storeClean()
	if err != nil {
		return r.finish(r.fail(err))
	}
	if !clean {
		if code := r.sayUnconfirmed(); code != term.ExitOK {
			return code
		}
		return r.finish(term.ExitOK)
	}
	other, err := heldBuildLock(r.buildLocks(), ownLock)
	if err != nil {
		return r.finish(r.fail(err))
	}
	for _, act := range r.removals {
		if other != "" {
			r.say("keep " + act.kind + " " + act.name + sizeNote(act.size) + ": another ccy image build is running (" + other + " is held)")
			continue
		}
		if err := r.eng.RemoveImage(ctx, act.ref); err != nil {
			r.failed = append(r.failed, "could not remove "+act.kind+" "+act.name+": "+err.Error())
			continue
		}
		r.say("removed " + act.kind + " " + act.name + sizeNote(act.size) + ": " + act.why)
		if err := r.record(act); err != nil {
			return r.finish(r.fail(err))
		}
	}
	return r.finish(term.ExitOK)
}

func (r *staleRun) say(line string) { r.p.Print(r.Env.Stderr, "container-images: "+line+"\n") }

func sizeNote(size int64) string {
	if size <= 0 {
		return ""
	}
	return " (" + humanBytes(size) + ")"
}
