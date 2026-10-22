package lxc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestfacts"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host/lxc/guestroot"
	memberstate "github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// The host kit's walk bounds (n3-D27) and the guest listings' caps.
const (
	kitDepth    = 6
	kitBytes    = 64 << 20
	maxKitNames = 256
	maxRuns     = 1024
	binDir      = "bin"
	tempPrefix  = ".new-"
	currentName = "current"
)

// hostKit is the host kit's tree, read once per phase: directories and regular files in walk
// order, parents first.
type hostKit struct {
	entries []kitEntry
}

type kitEntry struct {
	rel  string
	dir  bool
	data []byte
}

// hostFaultError is an error of the host's own, which fails the phase rather than a member.
type hostFaultError struct{ err error }

func (e *hostFaultError) Error() string { return e.err.Error() }
func (e *hostFaultError) Unwrap() error { return e.err }

// readHostKit walks the host kit at dir by an os.Root (no link followed), each file read by the
// host's trust read; a link, a device or anything else, a tree over kitDepth or over kitBytes,
// is refused.
func readHostKit(dir string) (*hostKit, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	kit := &hostKit{}
	total := int64(0)
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == "." {
			return err
		}
		if strings.Count(p, "/")+1 > kitDepth {
			return fmt.Errorf("the host kit %s is deeper than %d", dir, kitDepth)
		}
		switch {
		case d.IsDir():
			kit.entries = append(kit.entries, kitEntry{rel: p, dir: true})
			return nil
		case !d.Type().IsRegular():
			return fmt.Errorf("the host kit's %s is not a directory or a regular file", p)
		}
		data, found, err := state.ReadFile(filepath.Join(dir, p), kitBytes-total)
		if err == nil && !found {
			err = fmt.Errorf("the host kit's %s went during the walk", p)
		}
		if err != nil {
			return err
		}
		total += int64(len(data))
		kit.entries = append(kit.entries, kitEntry{rel: p, data: data})
		return nil
	})
	return kit, errors.Join(err, root.Close())
}

// fill writes the kit into a fresh directory (KT1): directories 0755, binaries under bin/ 0755,
// every other file 0644.
func (k *hostKit) fill(d *guestroot.Dir) error {
	dirs := map[string]*guestroot.Dir{".": d}
	for _, e := range k.entries {
		parent := dirs[path.Dir(e.rel)]
		name := path.Base(e.rel)
		if e.dir {
			nd, err := parent.Mkdir(name)
			if err != nil {
				return err
			}
			dirs[e.rel] = nd
			continue
		}
		mode := fs.FileMode(0o644)
		if strings.HasPrefix(e.rel, binDir+"/") {
			mode = 0o755
		}
		if err := parent.WriteFile(name, e.data, mode); err != nil {
			return err
		}
	}
	return nil
}

// placeKit is step 04 for one container (LX5): the digest's kit placed when the guest lacks
// it, current and the binary link swapped to it, and every old digest no live session names
// removed. False is a host fault, which fails the phase.
func (r *run) placeKit(ctx context.Context, g *guestBox) bool {
	err := r.guestKit(g)
	if err == nil {
		err = r.sweepKits(g)
	}
	if err == nil {
		return true
	}
	if hf, ok := errors.AsType[*hostFaultError](err); ok {
		r.errs = append(r.errs, &host.Error{Verb: verb, Step: "kit", Err: hf.err})
		return false
	}
	for _, m := range g.live() {
		r.step(ctx, g, m, "kit", err)
	}
	return true
}

func (r *run) guestKit(g *guestBox) error {
	digest := r.d.Kit.String()
	w := g.root.Writer()
	info, err := g.root.Stat(paths.HostKitRoot + "/" + digest)
	switch {
	case err == nil:
		if !info.Mode.IsDir() || info.UID != 0 || info.Mode.Perm()&0o022 != 0 {
			return fmt.Errorf("%w: kit %s is not a root-owned directory only root writes", guestfacts.ErrRefused, digest)
		}
	case errors.Is(err, fs.ErrNotExist):
		if r.kit == nil {
			kit, readErr := readHostKit(r.d.HostKit)
			if readErr != nil {
				return &hostFaultError{readErr}
			}
			r.kit = kit
		}
		if _, err = w.MkdirAll(paths.HostKitRoot); err != nil {
			return fmt.Errorf("kit: %w", err)
		}
		if _, err = w.PlaceDir(paths.HostKitRoot, digest, r.kit.fill); err != nil {
			return fmt.Errorf("kit: %w", err)
		}
	default:
		return err
	}
	if _, err := w.Symlink(paths.HostKitRoot, currentName, digest); err != nil {
		return fmt.Errorf("kit: %w", err)
	}
	bin := path.Dir(paths.MemberBinaryPath)
	if _, err := w.MkdirAll(bin); err != nil {
		return fmt.Errorf("kit: %w", err)
	}
	if _, err := w.Symlink(bin, path.Base(paths.MemberBinaryPath), paths.HostKitCurrent().Binary()); err != nil {
		return fmt.Errorf("kit: %w", err)
	}
	return nil
}

func validKitName(n string) bool {
	if n == currentName || strings.HasPrefix(n, tempPrefix) {
		return true
	}
	_, err := paths.ParseDigest(n)
	return err == nil
}

// sweepKits removes each old digest that is not current and that no live session names
// (n3-D8). A write's leftover temporary is not a digest and is left alone.
func (r *run) sweepKits(g *guestBox) error {
	names, found, err := g.root.ReadDir(paths.HostKitRoot, validKitName, maxKitNames)
	if err != nil || !found {
		return err
	}
	var old []string
	for _, n := range names {
		if n != currentName && n != r.d.Kit.String() && !strings.HasPrefix(n, tempPrefix) {
			old = append(old, n)
		}
	}
	if len(old) == 0 {
		return nil
	}
	live, all, err := liveKits(g)
	if err != nil || all {
		return err
	}
	for _, n := range old {
		if live[n] {
			continue
		}
		if _, err := g.root.Writer().RemoveTree(paths.HostKitRoot, n); err != nil {
			return fmt.Errorf("kit: %w", err)
		}
	}
	return nil
}

// liveKits is the digests the box's live sessions name: each session.pid under the two runtime
// parents of each of its users whose keeper's start time is the recorded one. all is a record
// that cannot be read for sure, which keeps every digest.
func liveKits(g *guestBox) (map[string]bool, bool, error) {
	live := map[string]bool{}
	for _, m := range g.members {
		if m.acct.UID <= 0 {
			continue
		}
		parents, err := paths.GuestRuntimeParents(m.decl.User, m.acct.UID)
		if err != nil {
			return nil, false, err
		}
		for _, parent := range parents {
			all, err := runsOf(g.root, parent, live)
			if err != nil || all {
				return nil, all, err
			}
		}
	}
	return live, false, nil
}

func isRunID(s string) bool {
	_, err := paths.ParseRunID(s)
	return err == nil
}

// errKeepAll is a run record that cannot be read for sure: every digest is kept.
var errKeepAll = errors.New("a run record cannot be read for sure")

func runsOf(root Root, parent string, live map[string]bool) (bool, error) {
	runs, found, err := root.ReadDir(parent, isRunID, maxRuns)
	if refused(err) {
		return true, nil
	}
	if err != nil || !found {
		return false, err
	}
	for _, run := range runs {
		err = sessionRun(root, parent+"/"+run, live)
		if errors.Is(err, errKeepAll) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
	}
	return false, nil
}

// sessionRun marks the digest one run's session.pid names as live when its keeper is still the
// recorded process. A record that is refused or does not decode is errKeepAll.
func sessionRun(root Root, dir string, live map[string]bool) error {
	data, _, found, err := root.ReadFile(dir+"/session.pid", memberstate.MaxSessionPID)
	if refused(err) {
		return errKeepAll
	}
	if err != nil || !found {
		return err
	}
	sp, err := memberstate.DecodeSessionPID(data)
	if err != nil {
		return fmt.Errorf("%w: %w", errKeepAll, err)
	}
	stat, found, err := root.ReadProcStat(sp.Keeper.PID)
	if refused(err) {
		return errKeepAll
	}
	if err != nil || !found {
		return err
	}
	ticks, err := guestfacts.StartTicks(stat)
	if err != nil {
		return fmt.Errorf("%w: %w", errKeepAll, err)
	}
	if ticks == sp.Keeper.StartTicks {
		live[sp.Kit.String()] = true
	}
	return nil
}
