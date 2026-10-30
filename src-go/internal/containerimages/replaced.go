package containerimages

// replaced.go is ccy's self-prune (Plan 00235 Phase 3): the image build lock
// (files/var/local/claude-yolo/lib/image-build-lock.bash) runs `before <repository>` once it
// holds the lock and `replaced --lock <its lock> <repository> <ids-file>` after a build that
// succeeded, so what the build replaced goes the moment it is replaced.
//
//   - R1. `before` prints each tag of the repository with the id it names, `<name> <id>` a
//     line; a repository with no image prints nothing.
//   - R2. `replaced` reads that record (`/dev/stdin` will do) and removes each recorded id no
//     tag now names and no container, running or stopped, uses, with the engine's own unforced
//     `image rm`. Each removal and each kept id is named on stderr with why, and each removal is
//     appended to the prune's log (P6). An id still named by every name it was recorded under
//     changed nothing and is passed over in silence.
//   - R3. A removal the engine refuses (an old :latest a project image is built on) is
//     reported and is not a failure: the daily prune removes it once nothing holds it.
//   - R4. A record line that is not `<name> <64-hex id>`, a list the engine cannot give or a
//     log that cannot be opened stops the run, exit 1, before any removal. The containers are
//     asked twice before that: one that goes between the listing and the inspect is a race.
//   - R5. The engine is --engine's, else CCY_CONTAINER_ENGINE's, else podman (engine.Select).
//   - R6. The build locks are per image and the record is the whole repository, so while a
//     ccy image build lock other than the caller's own (--lock) is held, nothing is removed:
//     each id that would go is named as kept, and the daily prune removes it later (P3a).
//   - R7. Without play-podman.yml's store-clean marker (P9) nothing is removed: the run says so
//     once on stderr and exits 0, since a launch never fails for its self-prune.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

var imageID = regexp.MustCompile(`^[0-9a-f]{64}$`)

// recorded is one id of the record and the names it had.
type recorded struct {
	id    string
	names []string
}

// selfPruneArgs reads `[--engine E] before <repository>` and `[--engine E] replaced [--lock
// <file>] <repository> <ids-file>`: the engine request, the words after the flag (for
// `replaced`, always the verb, repository, ids-file and lock, "" for none), and whether they are
// one of the two.
func selfPruneArgs(envEngine string, args []string) (engine.Request, []string, bool) {
	req := engine.Request{Env: envEngine}
	rest := args
	if len(rest) > 0 && rest[0] == engine.FlagName {
		flag, err := engine.FlagValue(rest, 0)
		if err != nil {
			return req, nil, false
		}
		req.Flag, req.FlagSet, rest = flag, true, rest[2:]
	}
	switch {
	case len(rest) == 2 && rest[0] == "before" && rest[1] != "":
		return req, rest, true
	case len(rest) > 0 && rest[0] == "replaced":
		words, ok := replacedArgs(rest[1:])
		return req, words, ok
	case len(rest) > 0 && rest[0] == verbStale:
		words, ok := staleArgs(rest[1:])
		return req, words, ok
	default:
		return req, nil, false
	}
}

// replacedArgs reads `[--lock <file>] <repository> <ids-file>` as the verb, repository,
// ids-file and lock.
func replacedArgs(args []string) ([]string, bool) {
	lock := ""
	if len(args) == 4 && args[0] == "--lock" {
		lock, args = args[1], args[2:]
		if lock == "" {
			return nil, false
		}
	}
	if len(args) != 2 || args[0] == "" || args[1] == "" {
		return nil, false
	}
	return []string{"replaced", args[0], args[1], lock}, true
}

// selfPrune selects the engine and runs `before` or `replaced`.
func (a App) selfPrune(ctx context.Context, req engine.Request, args []string) term.ExitCode {
	choice, err := engine.Select(req, a.Env.LookPath)
	if err != nil {
		if code := a.fail(err); code != term.ExitFailure {
			return code
		}
		return engine.Exit(err)
	}
	eng, err := engine.New(choice.Kind, a.Env.Runner)
	if err != nil {
		return a.fail(err)
	}
	switch args[0] {
	case "before":
		return a.before(ctx, eng, args[1])
	case verbStale:
		if choice.Kind != engine.Podman {
			return a.print(a.Env.Stderr, "container-images: the stale-image rule reads podman's store only; nothing removed\n")
		}
		return a.stale(ctx, eng, args[1], args[2], args[3] != "")
	}
	return a.replaced(ctx, eng, args[1], args[2], args[3])
}

func (a App) before(ctx context.Context, eng engine.Engine, repo string) term.ExitCode {
	tags, err := eng.RepositoryTags(ctx, repo)
	if err != nil {
		return a.fail(err)
	}
	var b strings.Builder
	for _, t := range tags {
		b.WriteString(t.Name + " " + t.ID + "\n")
	}
	return a.print(a.Env.Stdout, b.String())
}

func (a App) replaced(ctx context.Context, eng engine.Engine, repo, path, ownLock string) term.ExitCode {
	recs, err := readRecord(path)
	if err != nil {
		return a.fail(err)
	}
	tags, err := eng.RepositoryTags(ctx, repo)
	if err != nil {
		return a.fail(err)
	}
	named := map[string][]string{}
	for _, t := range tags {
		named[t.ID] = append(named[t.ID], t.Name)
	}
	r := &run{App: a, eng: eng, yes: true}
	var gone []recorded
	for _, rec := range recs {
		now := named[rec.id]
		switch {
		case len(now) == 0:
			gone = append(gone, rec)
		case !allIn(rec.names, now):
			r.say("kept", rec, "still named by "+strings.Join(now, ", "))
		}
	}
	if len(gone) == 0 {
		return r.finish(term.ExitOK)
	}
	clean, err := a.storeClean()
	if err != nil {
		return r.finish(r.fail(err))
	}
	if !clean {
		return a.sayUnconfirmed()
	}
	other, err := heldBuildLock(a.buildLocks(), ownLock)
	if err != nil {
		return r.finish(r.fail(err))
	}
	if other != "" {
		for _, rec := range gone {
			r.say("kept", rec, "another ccy image build is running ("+other+" is held); the daily prune (container-images-prune.timer) removes it once no build runs")
		}
		return r.finish(term.ExitOK)
	}
	if err := r.removeReplaced(ctx, repo, gone); err != nil {
		return r.finish(r.fail(err))
	}
	return r.finish(term.ExitOK)
}

// removeReplaced is R2 and R3 for the ids no tag names any more.
func (r *run) removeReplaced(ctx context.Context, repo string, gone []recorded) error {
	users, err := r.eng.ContainerImageIDs(ctx)
	if err != nil {
		if users, err = r.eng.ContainerImageIDs(ctx); err != nil {
			return err
		}
	}
	var unused []recorded
	for _, rec := range gone {
		if slices.Contains(users, rec.id) {
			r.say("kept", rec, "a container uses it")
			continue
		}
		unused = append(unused, rec)
	}
	if len(unused) == 0 {
		return nil
	}
	if r.log, err = r.openLog(); err != nil {
		return err
	}
	defer func() {
		if cerr := r.log.Close(); cerr != nil {
			r.failed = append(r.failed, "the log: "+cerr.Error())
		}
	}()
	for _, rec := range unused {
		earlier, err := r.removeItem(ctx, kindImage, rec.id, false)
		if err != nil {
			r.say("could not remove", rec, err.Error()+"; the daily prune (container-images-prune.timer) removes it once nothing holds it")
			continue
		}
		verb := "removed"
		if earlier {
			verb = "removed with an earlier item"
		}
		r.say(verb, rec, "replaced by this build; no tag names it and no container uses it")
		if err := r.record(action{kind: kindImage, name: short(rec.id), id: rec.id, why: "replaced by a build of " + repo}); err != nil {
			return err
		}
	}
	return nil
}

func (r *run) say(verb string, rec recorded, why string) {
	r.p.Print(r.Env.Stderr, "container-images: "+verb+" image "+short(rec.id)+" (was "+strings.Join(rec.names, ", ")+"): "+why+"\n")
}

// allIn is every name still among the names now.
func allIn(names, now []string) bool {
	for _, n := range names {
		if !slices.Contains(now, n) {
			return false
		}
	}
	return true
}

// readRecord is the ids `before` printed, each once, in order, with the names each had (R4).
func readRecord(path string) ([]recorded, error) {
	text, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("cannot read the record of the images before the build: %w", err)
	}
	var recs []recorded
	at := map[string]int{}
	for i, line := range strings.Split(string(text), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		var id string
		if len(fields) == 2 {
			id = strings.TrimPrefix(fields[1], "sha256:")
		}
		if !imageID.MatchString(id) {
			return nil, fmt.Errorf("line %s of the record %s is not `<name> <image id>`: %q", strconv.Itoa(i+1), path, line)
		}
		if j, ok := at[id]; ok {
			recs[j].names = append(recs[j].names, fields[0])
			continue
		}
		at[id] = len(recs)
		recs = append(recs, recorded{id: id, names: []string{fields[0]}})
	}
	return recs, nil
}
