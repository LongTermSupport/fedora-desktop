package statusline

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
)

// The bus segment's cache (S32 to S36): one file under the resolved home, keyed by what the
// segment depends on that is not on disk, holding the fingerprint of every path the render read.
const (
	cacheFile     = "statusline-cache.json"
	cacheTTL      = 60 * time.Second
	cacheEntries  = 16
	cacheVersion  = 1
	cacheMaxBytes = 1 << 20
)

// cacheKeyVars are the variables the segment reads (S32), in the order the key lists them.
var cacheKeyVars = []string{
	membershipVar, "PINGBUS_TEAMS", "PINGBUS_HOME", "XDG_CONFIG_HOME", "HOME", sessionsDirVar, "COLUMNS",
}

// cacheContent is the cache file.
type cacheContent struct {
	V       int     `json:"v"`
	Entries []entry `json:"entries"`
}

// entry is one cached segment: the text, the role diagnostic printed with it, when it was written
// and when it stops being used (unix ns), and the paths as the render saw them.
type entry struct {
	Key     string        `json:"key"`
	Written int64         `json:"written"`
	Expires int64         `json:"expires"`
	Segment string        `json:"segment"`
	Problem string        `json:"problem"`
	Prints  []fingerprint `json:"prints"`
}

// segment is one render of the bus segment and what the cache needs to store it.
type segment struct {
	text, problem string
	// expires is the earliest moment a mark it shows stops counting; zero for none.
	expires time.Time
	// teamsRead is whether the render got as far as the teams, so it is worth storing.
	teamsRead bool
}

// cacheKey is the key of a render: the session id, the uid, the working directory and each
// present key variable as NAME=value.
func cacheKey(env map[string]string, id string, uid int, cwd string) string {
	vars := []string{}
	for _, name := range cacheKeyVars {
		if value, ok := env[name]; ok {
			vars = append(vars, name+"="+value)
		}
	}
	raw, err := json.Marshal([]any{id, uid, cwd, vars})
	if err != nil {
		return ""
	}
	return string(raw)
}

// cachePath is the cache file, or "" when the home cannot be resolved (S32).
func cachePath(env map[string]string) string {
	home, err := resolveHome(env)
	if err != nil {
		return ""
	}
	return pyJoin(home, cacheFile)
}

// loadCache is the cache file's entries, or nil when it cannot be read, is not a regular file
// of this user's alone, or is not one this writes (S36). It is never read through a symlink.
func loadCache(path string, uid int) []entry {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil
	}
	file := os.NewFile(uintptr(fd), path)
	got, readErr := readOpened(file, cacheMaxBytes)
	if closeErr := file.Close(); closeErr != nil || readErr != nil {
		return nil
	}
	if int(got.info.Uid) != uid || got.info.Mode&0o077 != 0 {
		return nil
	}
	var content cacheContent
	if err := json.Unmarshal(got.raw, &content); err != nil || content.V != cacheVersion {
		return nil
	}
	return content.Entries
}

// lookup is the entry for key that is still good at now: written no later than now, not yet
// expired, and every path it recorded unchanged (S33, S34).
func lookup(entries []entry, key string, now time.Time) (entry, bool) {
	at := now.UnixNano()
	for _, e := range entries {
		if e.Key != key {
			continue
		}
		if e.Written > at || at >= e.Expires {
			return entry{}, false
		}
		for _, fp := range e.Prints {
			if !fp.unchanged() {
				return entry{}, false
			}
		}
		return e, true
	}
	return entry{}, false
}

// store writes seg into the cache under key, newest first, without the entries it replaces or
// that have expired, at most cacheEntries of them. A cache that cannot be written is left as it
// is: the render's output never depends on it (S36).
func store(path string, entries []entry, fresh entry, now time.Time) {
	kept := []entry{fresh}
	at := now.UnixNano()
	for _, e := range entries {
		if e.Key != fresh.Key && at < e.Expires && len(kept) < cacheEntries {
			kept = append(kept, e)
		}
	}
	raw, err := json.Marshal(cacheContent{V: cacheVersion, Entries: kept})
	if err != nil {
		return
	}
	if err := fsx.WriteAtomic(path, "status line cache", raw, 0o600); err != nil {
		return
	}
}

// cachedSegment is the bus segment from the cache when it holds a good entry for this render,
// else rendered by compute and stored when it is safe to: the teams were read, no path read was
// racy and the render was not cancelled.
func cachedSegment(ctx context.Context, w *world, env map[string]string, id, cwd string,
	compute func(*world) (segment, error),
) (segment, error) {
	path := cachePath(env)
	key := cacheKey(env, id, w.uid, cwd)
	var entries []entry
	if path != "" && key != "" {
		entries = loadCache(path, w.uid)
		if hit, ok := lookup(entries, key, w.now); ok {
			return segment{text: hit.Segment, problem: hit.Problem}, nil
		}
	}
	seg, err := compute(w)
	if err != nil {
		return segment{}, err
	}
	if path != "" && key != "" && storable(ctx, w, seg) {
		expires := w.now.Add(cacheTTL)
		if !seg.expires.IsZero() && seg.expires.Before(expires) {
			expires = seg.expires
		}
		store(path, entries, entry{
			Key: key, Written: w.now.UnixNano(), Expires: expires.UnixNano(),
			Segment: seg.text, Problem: seg.problem, Prints: slices.Clone(w.prints),
		}, w.now)
	}
	return seg, nil
}

// storable is whether a computed segment may be cached: the teams were read, no path read was
// racy, and the render was not cancelled.
func storable(ctx context.Context, w *world, seg segment) bool {
	return seg.teamsRead && !w.racy() && ctx.Err() == nil
}
