package containerimages

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

// maxSamples is how many old-range paths of a directory the scan names.
const maxSamples = 5

// storeRel is the rootless store under a home directory: the standard path, the one the play's
// repair asserts no configuration moves.
var storeRel = filepath.Join(".local", "share", "containers", "storage")

// StoreDir is one store directory holding old-range paths (O2): overlay/<id>, volumes/<name>,
// or the first two parts of any other path.
type StoreDir struct {
	Key   string `json:"key"`
	Paths int    `json:"paths"`
	// UIDs and GIDs are the distinct owners of its old-range paths.
	UIDs    []int64  `json:"uids"`
	GIDs    []int64  `json:"gids"`
	Samples []string `json:"samples"`
	// Size is the apparent size of every regular file under it; Data is whether one is not empty.
	Size int64 `json:"size"`
	Data bool  `json:"data"`
}

// Scan is what `old-range scan` prints: the store, its user's owners, and each directory
// holding a path the rootless user's namespace does not map.
type Scan struct {
	Store  string     `json:"store"`
	User   string     `json:"user"`
	Owners Owners     `json:"owners"`
	Dirs   []StoreDir `json:"dirs"`
}

// scanEntry is one path the walk met, relative to the store.
type scanEntry struct {
	rel      string
	regular  bool
	uid, gid int64
	size     int64
	mode     fs.FileMode
}

// dirKey is the store directory a path belongs to (O2).
func dirKey(rel string) string {
	parts := strings.SplitN(rel, "/", 3)
	if len(parts) == 1 {
		return parts[0]
	}
	return parts[0] + "/" + parts[1]
}

type aggregator struct {
	owners Owners
	dirs   map[string]*StoreDir
	uids   map[string]map[int64]bool
	gids   map[string]map[int64]bool
}

func newAggregator(o Owners) *aggregator {
	return &aggregator{owners: o, dirs: map[string]*StoreDir{}, uids: map[string]map[int64]bool{}, gids: map[string]map[int64]bool{}}
}

// add counts one entry; the store's own directory is no store directory of its own.
func (a *aggregator) add(e scanEntry) {
	if e.rel == "." {
		return
	}
	key := dirKey(e.rel)
	d := a.dirs[key]
	if d == nil {
		d = &StoreDir{Key: key}
		a.dirs[key] = d
		a.uids[key], a.gids[key] = map[int64]bool{}, map[int64]bool{}
	}
	if e.regular {
		d.Size += e.size
		d.Data = d.Data || e.size > 0
	}
	if !a.owners.foreignUID(e.uid) {
		return
	}
	d.Paths++
	a.uids[key][e.uid], a.gids[key][e.gid] = true, true
	if len(d.Samples) < maxSamples {
		d.Samples = append(d.Samples, e.rel)
	}
}

// result is every directory with an old-range path, by key.
func (a *aggregator) result() []StoreDir {
	out := []StoreDir{}
	for _, key := range slices.Sorted(maps.Keys(a.dirs)) {
		d := a.dirs[key]
		if d.Paths == 0 {
			continue
		}
		d.UIDs, d.GIDs = sortedIDs(a.uids[key]), sortedIDs(a.gids[key])
		out = append(out, *d)
	}
	return out
}

func sortedIDs(set map[int64]bool) []int64 {
	out := make([]int64, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// walkStore is O3: root (as ".") and every path under it on its filesystem (a mount, such as a
// running container's merged root, is passed over). A path gone before it is read is not an
// error: running containers add and delete files while the walk runs.
func walkStore(root string, fn func(scanEntry) error) error {
	var rootDev uint64
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path != root {
				return nil
			}
			return err
		}
		info, err := d.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("cannot read the owner of %s", path)
		}
		if path == root {
			rootDev = st.Dev
		}
		if d.IsDir() && st.Dev != rootDev {
			return fs.SkipDir
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return fn(scanEntry{rel: rel, regular: info.Mode().IsRegular(), uid: int64(st.Uid), gid: int64(st.Gid), size: info.Size(), mode: info.Mode()})
	})
}

// storeMovers are the settings that would put the rootless store somewhere other than storeRel.
var storeMovers = regexp.MustCompile(`(?m)^\s*(graphroot|rootless_storage_path)\s*=`)

// userStore is the user's owners and store path (O4): the one subuid and subgid range each,
// and the standard store path, refused when a storage configuration could move it.
func (a App) userStore(login string) (Owners, string, error) {
	u, err := a.Env.LookupUser(login)
	if err != nil {
		return Owners{}, "", fmt.Errorf("cannot look up the user %s: %w", login, err)
	}
	uid, err1 := strconv.ParseInt(u.Uid, 10, 64)
	gid, err2 := strconv.ParseInt(u.Gid, 10, 64)
	if err := errors.Join(err1, err2); err != nil {
		return Owners{}, "", fmt.Errorf("cannot read the uid and gid of %s: %w", login, err)
	}
	o := Owners{UID: uid, GID: gid}
	for _, f := range []struct {
		name string
		into *Range
		id   int64
	}{{"subuid", &o.SubUID, uid}, {"subgid", &o.SubGID, gid}} {
		text, err := os.ReadFile(filepath.Join(a.Env.Etc, f.name))
		if err != nil {
			return Owners{}, "", fmt.Errorf("cannot read the %s ranges: %w", f.name, err)
		}
		if *f.into, err = parseSubIDs(string(text), login, f.id); err != nil {
			return Owners{}, "", fmt.Errorf("%s: %w", f.name, err)
		}
	}
	for _, conf := range []string{filepath.Join(u.HomeDir, ".config", "containers", "storage.conf"), filepath.Join(a.Env.Etc, "containers", "storage.conf")} {
		text, err := os.ReadFile(filepath.Clean(conf))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return Owners{}, "", fmt.Errorf("cannot read %s: %w", conf, err)
		}
		if storeMovers.Match(text) {
			return Owners{}, "", fmt.Errorf("%s may move the rootless store away from %s, where this looks", conf, storeRel)
		}
	}
	return o, filepath.Join(u.HomeDir, storeRel), nil
}

// scan is `old-range scan --user NAME`: root's one walk of the store, printed as JSON. It
// reads only.
func (a App) scan(login string) error {
	if a.Env.EUID != 0 {
		return errors.New("old-range scan reads every file of the store, which only root can; run it with sudo")
	}
	o, store, err := a.userStore(login)
	if err != nil {
		return err
	}
	agg := newAggregator(o)
	if err := a.walk(store, func(e scanEntry) error { agg.add(e); return nil }); err != nil {
		return fmt.Errorf("cannot walk the store %s: %w", store, err)
	}
	return a.printJSON(Scan{Store: store, User: login, Owners: o, Dirs: agg.result()})
}

func (a App) printJSON(v any) error {
	text, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return fmt.Errorf("cannot write JSON: %w", err)
	}
	if _, err := a.Env.Stdout.Write(append(text, '\n')); err != nil {
		return fmt.Errorf("cannot write to stdout: %w", err)
	}
	return nil
}
