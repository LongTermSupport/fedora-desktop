// Package gocache is `ccy-teams internal go-cache <project> [z]`: the Go caches of a ccy
// project's sessions, kept on the host instead of in the container's writable layer.
//
// `podman run --rm` deletes the writable layer file by file when the container exits, and a
// session that runs Go QA fills it with tens of thousands of cache files (the build cache, the
// golangci-lint cache, the module cache, go-qa's pinned tools), so ending a long session waited
// up to a minute after the container had died. The caches now live in
// ~/.cache/ccy/<project>/go on the host, bind-mounted at /var/cache/ccy-go, so they survive the
// container, warm the next session, and cost the exit nothing.
//
//   - G1. Under podman or docker (CONTAINER_ENGINE, as the launcher exports it; empty is
//     podman) the command makes <HOME>/.cache/ccy/<project>/go and its four cache directories
//     (0700, each a real directory: a symlink or a file in the way is exit 1), then prints the
//     run arguments
//     the launcher appends to its `run`, one per line: the bind (`-v`, with the shared
//     SELinux relabel `:z` when the launcher passes `z`, so a second seat of the project shares
//     it) and an `-e NAME=path` for each of GOCACHE, GOLANGCI_LINT_CACHE, GOMODCACHE and
//     GOQA_TOOLS_DIR. A project's ccy.env can still override them: the entrypoint sources it
//     after this environment.
//   - G2. Both engines are treated alike: the launcher refuses a rootful engine before it asks
//     for the caches, so under either the container's root is the user and the bind fills
//     with files the user can prune. Any other engine is exit 1.
//   - G3. Every seat of one project shares the caches. go's build and module caches and
//     golangci-lint's (derived from go's) are written for concurrent use, and go-qa installs a
//     tool in a scratch directory and renames it into place.
//   - G4. The bind's caches are bounded at launch, under an flock on
//     ~/.cache/ccy/<project>/go.lock (beside the bind, out of the container's reach), so two
//     launches of the project never prune at once and one never takes another's .prune-* for a
//     leftover. go already trims build-cache entries unused for five days; on top of that, when
//     the regular files under the root hold more than Limit (DefaultLimit) on disk (allocated
//     blocks), the build caches (go-build, golangci-lint) are cleared, and if the total is still
//     over, the module cache and the tools too; still over after that is exit 1, naming the
//     root, since what remains is nothing ccy keeps. Each is renamed into a .prune-* directory
//     first, so the cache path is at once either the whole old cache or a fresh one, then
//     removed (the module cache's read-only directories made writable) and made again. A seat
//     building at that moment loses only cache hits, except that clearing the module cache can
//     fail a module download it has in flight, and clearing the tools makes its next go-qa run
//     install them again. A .prune-* left by an interrupted launch is removed before
//     measuring. A file another seat removes during the walk counts as nothing. What was cleared is said on stderr.
//     The container writes inside the root, so every operation there goes through one os.Root:
//     a link it plants (as a leftover, a cache or inside one) is moved or unlinked, never
//     followed out of the cache.
//   - G5. Wrong arguments are exit 64; a HOME that is not absolute or holds a character the
//     bind or the one-per-line output cannot carry (`:`, `,`, a newline) is exit 1, as is any
//     other file system error.
package gocache

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	sub = "go-cache"
	// ContainerDir is where the caches are mounted in the container.
	ContainerDir = "/var/cache/ccy-go"
	// DefaultLimit bounds what one project's caches hold on disk: 8 GiB.
	DefaultLimit int64 = 8 << 30
	dirMode            = 0o700
	prunePrefix        = ".prune-"
	lockName           = "go.lock"
	lockMode           = 0o600
	// blockSize is the unit of Stat_t.Blocks.
	blockSize = 512
)

// cache is one Go cache: its directory under the root, the variable that points go at it, and
// the stage of the bound that clears it (1 regrows from the sources, 2 is fetched again).
type cache struct {
	dir   string
	env   string
	stage int
}

var caches = []cache{
	{"go-build", "GOCACHE", 1},
	{"golangci-lint", "GOLANGCI_LINT_CACHE", 1},
	{"mod", "GOMODCACHE", 2},
	{"go-qa-tools", "GOQA_TOOLS_DIR", 2},
}

// projectName is what the launcher's get_project_name can make, as claim-container-name takes it.
var projectName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]{0,199}$`)

// Cache is the go-cache command.
type Cache struct {
	Host *host.Host
	// Limit is the bound of G4, in bytes on disk.
	Limit int64
	// lock takes the exclusive lock at path and returns its release (flock outside tests).
	lock func(path string) (func() error, error)
	// wrap, when set, stands between the command and the cache's os.Root: the tests' fault
	// injection.
	wrap func(fsRoot) fsRoot
}

// fsRoot is what the command does inside the cache root; *os.Root is the one implementation.
type fsRoot interface {
	Mkdir(name string, perm fs.FileMode) error
	Lstat(name string) (fs.FileInfo, error)
	Rename(oldname, newname string) error
	Chmod(name string, mode fs.FileMode) error
	Remove(name string) error
	RemoveAll(name string) error
	FS() fs.FS
}

// New is the command over a host, with the default bound.
func New(h *host.Host) *Cache { return &Cache{Host: h, Limit: DefaultLimit, lock: flock} }

// Run is `ccy-teams internal go-cache <project> [z]`.
func (c *Cache) Run(_ context.Context, args []string) term.ExitCode {
	project, relabel, err := parse(args)
	if err != nil {
		c.say(err.Error() + ". See ccy-teams --help.")
		return term.ExitUsage
	}
	switch engine := c.Host.Getenv("CONTAINER_ENGINE"); engine {
	case "", "podman", "docker":
	default:
		c.say(fmt.Sprintf("CONTAINER_ENGINE is %q: the Go caches know podman and docker.", engine))
		return term.ExitFailure
	}
	home := c.Host.Getenv("HOME")
	if !filepath.IsAbs(home) || strings.ContainsAny(home, ":,\n") {
		c.say(fmt.Sprintf("HOME is %q: the Go caches need an absolute HOME holding no ':', ',' or newline, "+
			"which the container's bind mount can name.", home))
		return term.ExitFailure
	}
	root := filepath.Join(home, ".cache", "ccy", project, "go")
	if err := c.prepare(root); err != nil {
		c.say(err.Error())
		return term.ExitFailure
	}
	c.Host.Print(c.Host.Payload, strings.Join(runArgs(root, relabel), "\n")+"\n")
	return term.ExitOK
}

func parse(args []string) (string, string, error) {
	if len(args) < 1 || len(args) > 2 {
		return "", "", errors.New("takes <project> [z]")
	}
	if !projectName.MatchString(args[0]) {
		return "", "", fmt.Errorf("%q is not a project name", args[0])
	}
	if len(args) == 2 && args[1] != "z" {
		return "", "", fmt.Errorf("%q is not a relabel the shared caches can take (only z)", args[1])
	}
	if len(args) == 2 {
		return args[0], ":z", nil
	}
	return args[0], "", nil
}

// runArgs are G1's arguments for the engine's `run`.
func runArgs(root, relabel string) []string {
	out := make([]string, 0, 2+2*len(caches))
	out = append(out, "-v", root+":"+ContainerDir+relabel)
	for _, c := range caches {
		out = append(out, "-e", c.env+"="+ContainerDir+"/"+c.dir)
	}
	return out
}

// prepare makes the directories and, under the project's lock, finishes an interrupted prune
// and applies the bound. Everything inside root is done through one os.Root: the container
// writes there, so a link it plants never leads the host outside the cache (G4).
func (c *Cache) prepare(root string) (err error) {
	if err = makeRoot(root); err != nil {
		return err
	}
	opened, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("open the Go cache directory %s: %w", root, err)
	}
	defer func() { err = errors.Join(err, opened.Close()) }()
	var r fsRoot = opened
	if c.wrap != nil {
		r = c.wrap(r)
	}
	// The lock sits beside the bind, where the container cannot hold, remove or replace it.
	unlock, err := c.lock(filepath.Join(filepath.Dir(root), lockName))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unlock()) }()
	if err := removeLeftovers(r); err != nil {
		return fmt.Errorf("clear an interrupted prune in %s: %w", root, err)
	}
	for stage := 1; ; stage++ {
		size, err := diskUsage(r)
		if err != nil {
			return fmt.Errorf("measure the Go caches in %s: %w", root, err)
		}
		if size <= c.Limit {
			break
		}
		if stage > 2 {
			return fmt.Errorf("%s still holds %s after every Go cache in it was cleared, over the %s bound: "+
				"what remains is not a cache ccy keeps; remove it from %s", root, gib(size), gib(c.Limit), root)
		}
		names := stageDirs(stage)
		c.say(fmt.Sprintf("the Go caches in %s hold %s, over the %s bound: clearing %s.",
			root, gib(size), gib(c.Limit), strings.Join(names, ", ")))
		for _, name := range names {
			if err := prune(r, name); err != nil {
				return fmt.Errorf("clear the Go cache %s: %w", filepath.Join(root, name), err)
			}
		}
	}
	for _, cc := range caches {
		if err := makeCache(r, cc.dir); err != nil {
			return fmt.Errorf("%s: %w", filepath.Join(root, cc.dir), err)
		}
	}
	return nil
}

func stageDirs(stage int) []string {
	var names []string
	for _, c := range caches {
		if c.stage == stage {
			names = append(names, c.dir)
		}
	}
	return names
}

// makeRoot makes the cache root and its parents (0700) and refuses anything but a real
// directory there. The container cannot reach this path: it sees only what is inside it.
func makeRoot(dir string) error {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("make the Go cache directory %s: %w", dir, err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("read the Go cache directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory (%v): move it away so ccy can keep the Go caches there", dir, info.Mode().Type())
	}
	return nil
}

// makeCache makes one cache directory in r and refuses anything but a real directory there.
func makeCache(r fsRoot, name string) error {
	if err := r.Mkdir(name, dirMode); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	info, err := r.Lstat(name)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory (%v): move it away so ccy can keep the Go cache there", info.Mode().Type())
	}
	return nil
}

// removeLeftovers removes the .prune-* entries an interrupted launch left.
func removeLeftovers(r fsRoot) error {
	entries, err := fs.ReadDir(r.FS(), ".")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prunePrefix) {
			if err := removeTree(r, e.Name()); err != nil {
				return err
			}
		}
	}
	return nil
}

// prune moves the cache name into a fresh .prune-* directory, then removes that.
func prune(r fsRoot, name string) error {
	trash, err := trashDir(r)
	if err != nil {
		return err
	}
	if err := r.Rename(name, trash+"/"+name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return removeTree(r, trash)
}

// trashDir makes a .prune-<random> directory in r.
func trashDir(r fsRoot) (string, error) {
	name := prunePrefix + rand.Text()
	return name, r.Mkdir(name, dirMode)
}

// removeTree removes name in r: an entry that is not a directory (a planted link) is only
// unlinked; a directory has each of its directories made writable first (go leaves the module
// cache read-only), then is removed. r keeps both inside the cache.
func removeTree(r fsRoot, name string) error {
	info, err := r.Lstat(name)
	if err != nil {
		return skipGone(err)
	}
	if !info.IsDir() {
		return skipGone(r.Remove(name))
	}
	err = fs.WalkDir(r.FS(), name, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return skipGone(err)
		}
		if d.IsDir() {
			return skipGone(r.Chmod(path, dirMode))
		}
		return nil
	})
	if err != nil {
		return err
	}
	return r.RemoveAll(name)
}

// diskUsage is what the regular files in r hold on disk: allocated blocks, so a sparse file
// counts only what it really takes.
func diskUsage(r fsRoot) (int64, error) {
	var total int64
	err := fs.WalkDir(r.FS(), ".", func(_ string, d fs.DirEntry, err error) error {
		n, err := blocks(d, err)
		total += n
		return err
	})
	return total, err
}

// blocks is what one walked entry holds on disk: a regular file's allocated blocks, else
// nothing. An entry another seat removed after it was listed holds nothing (G4).
func blocks(d fs.DirEntry, walkErr error) (int64, error) {
	if walkErr != nil {
		return 0, skipGone(walkErr)
	}
	if !d.Type().IsRegular() {
		return 0, nil
	}
	info, err := d.Info()
	if err != nil {
		return 0, skipGone(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("no block count for %s", d.Name())
	}
	return st.Blocks * blockSize, nil
}

// skipGone is err, unless it says the file no longer exists.
func skipGone(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// flock takes an exclusive flock on path, made 0600 if missing, and returns its release.
func flock(path string) (func() error, error) {
	f, err := os.OpenFile(filepath.Clean(path), os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, lockMode)
	if err != nil {
		return nil, fmt.Errorf("open the lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return nil, errors.Join(fmt.Errorf("lock %s: %w", path, err), f.Close())
	}
	return func() error {
		unlockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		if unlockErr != nil {
			unlockErr = fmt.Errorf("unlock %s: %w", path, unlockErr)
		}
		return errors.Join(unlockErr, f.Close())
	}, nil
}

func gib(n int64) string { return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30)) }

func (c *Cache) say(msg string) {
	c.Host.Print(c.Host.Stderr, "ccy-teams: internal "+sub+": "+msg+"\n")
}
