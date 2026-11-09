package containerimages

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// capabilityXattr is where a file's capabilities live; a chown removes it.
const capabilityXattr = "security.capability"

// walkFunc walks a tree as walkStore does.
type walkFunc func(root string, fn func(scanEntry) error) error

// fileOps is what the re-own does to a file (the seam a test fakes where only root could act):
// the zero value is the real one.
type fileOps struct {
	lchown    func(path string, uid, gid int) error
	chmod     func(path string, mode fs.FileMode) error
	getcap    func(path string) ([]byte, error) // nil, nil for a file with none
	setcap    func(path string, value []byte) error
	removeAll func(path string) error
}

func realFileOps() fileOps {
	return fileOps{
		lchown: os.Lchown,
		chmod:  os.Chmod,
		getcap: func(path string) ([]byte, error) {
			buf := make([]byte, 256)
			n, err := unix.Lgetxattr(path, capabilityXattr, buf)
			if errors.Is(err, unix.ENODATA) {
				return nil, nil
			}
			if err != nil {
				return nil, fmt.Errorf("read the capabilities of %s: %w", path, err)
			}
			return buf[:n], nil
		},
		setcap: func(path string, value []byte) error {
			if err := unix.Lsetxattr(path, capabilityXattr, value, 0); err != nil {
				return fmt.Errorf("put back the capabilities of %s: %w", path, err)
			}
			return nil
		},
		removeAll: os.RemoveAll,
	}
}

func (a App) walk(root string, fn func(scanEntry) error) error {
	if a.Env.Walk != nil {
		return a.Env.Walk(root, fn)
	}
	return walkStore(root, fn)
}

func (a App) files() fileOps {
	if a.Env.Files.lchown != nil {
		return a.Env.Files
	}
	return realFileOps()
}

// reownPath is O10 for one path: the chown, then the setuid and setgid bits and the file
// capability the kernel cleared with it put back.
func reownPath(ops fileOps, path string, e scanEntry, uid, gid int) error {
	var caps []byte
	if e.regular {
		var err error
		if caps, err = ops.getcap(path); err != nil {
			return err
		}
	}
	if err := ops.lchown(path, uid, gid); err != nil {
		return fmt.Errorf("chown %s to %d:%d: %w", path, uid, gid, err)
	}
	if e.regular && e.mode&(fs.ModeSetuid|fs.ModeSetgid) != 0 {
		if err := ops.chmod(path, e.mode&(fs.ModePerm|fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky)); err != nil {
			return fmt.Errorf("put back the mode of %s: %w", path, err)
		}
	}
	if caps != nil {
		return ops.setcap(path, caps)
	}
	return nil
}

// readPlan reads a plan file (or /dev/stdin).
func readPlan(path string) (Plan, error) {
	var p Plan
	text, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return p, fmt.Errorf("cannot read the plan %s: %w", path, err)
	}
	if err := json.Unmarshal(text, &p); err != nil {
		return p, fmt.Errorf("the plan %s is not a plan: %w", path, err)
	}
	return p, nil
}

// storeDirKey is O13: a store directory, two parts, inside the store.
func storeDirKey(key string) bool {
	return filepath.IsLocal(key) && filepath.Clean(key) == key && strings.Count(key, "/") == 1 && dirKey(key) == key
}

// checkRootPlan is O13: what root is handed is checked before root acts on it.
func checkRootPlan(p Plan) error {
	if !filepath.IsAbs(p.Store) || filepath.Clean(p.Store) != p.Store || !strings.HasSuffix(p.Store, string(filepath.Separator)+storeRel) {
		return fmt.Errorf("the plan's store %q is not a rootless store path", p.Store)
	}
	if err := p.Owners.checkOld(p.Old); err != nil {
		return err
	}
	for _, s := range p.Reown {
		if !storeDirKey(s.Key) {
			return fmt.Errorf("the plan would re-own %q, which is not a store directory", s.Key)
		}
	}
	for _, s := range p.RootRemove {
		if !storeDirKey(s.Key) || !rootRemovable(s.Key) {
			return fmt.Errorf("the plan would remove %q, which root may not remove", s.Key)
		}
	}
	return nil
}

// reown is `old-range reown --plan FILE [--yes]` (O10, O13): as root, each planned directory's
// old-range paths shifted, and each planned stray directory removed; a dry run without --yes.
func (a App) reown(planPath string, yes bool) error {
	if a.Env.EUID != 0 {
		return errors.New("old-range reown changes files only root can; run it with sudo")
	}
	p, err := readPlan(planPath)
	if err != nil {
		return err
	}
	if err := checkRootPlan(p); err != nil {
		return err
	}
	out := a.Env.Stdout
	var lines []string
	if !yes {
		lines = append(lines, "old-range reown: a dry run, nothing is changed. --yes changes what is listed.")
	}
	ops := a.files()
	for _, s := range p.Reown {
		n, err := a.reownDir(ops, p, s.Key, yes)
		if err != nil {
			return errors.Join(writeLines(out, lines), err)
		}
		lines = append(lines, verb(yes, "re-owned", "would re-own")+" "+s.Key+": "+strconv.Itoa(n)+" paths ("+s.Why+")")
	}
	for _, s := range p.RootRemove {
		if yes {
			if err := ops.removeAll(filepath.Join(p.Store, s.Key)); err != nil {
				return errors.Join(writeLines(out, lines), fmt.Errorf("remove %s: %w", s.Key, err))
			}
		}
		lines = append(lines, verb(yes, "removed", "would remove")+" "+s.Key+" as root ("+s.Why+")")
	}
	for _, s := range p.Held {
		lines = append(lines, "held, left: "+s.Key+" ("+s.Why+")")
	}
	return writeLines(out, lines)
}

func verb(yes bool, done, would string) string {
	if yes {
		return done
	}
	return would
}

func writeLines(w io.Writer, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	if _, err := w.Write([]byte(strings.Join(lines, "\n") + "\n")); err != nil {
		return fmt.Errorf("cannot write to stdout: %w", err)
	}
	return nil
}

// reownDir shifts (or counts, in a dry run) every old-range path of one store directory; the
// first path it cannot place stops it, by name.
func (a App) reownDir(ops fileOps, p Plan, key string, yes bool) (int, error) {
	root := filepath.Join(p.Store, key)
	n := 0
	err := a.walk(root, func(e scanEntry) error {
		if !p.Owners.foreignUID(e.uid) {
			return nil
		}
		path := filepath.Join(root, e.rel)
		uid, gid, err := p.Owners.move(e.uid, e.gid, p.Old)
		if err != nil {
			return fmt.Errorf("%s cannot be re-owned: %w", path, err)
		}
		n++
		if !yes {
			return nil
		}
		return reownPath(ops, path, e, int(uid), int(gid))
	})
	if err != nil {
		return n, fmt.Errorf("re-own %s: %w", key, err)
	}
	return n, nil
}
