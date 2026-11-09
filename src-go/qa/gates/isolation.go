package gates

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// SuiteLocale is the locale every row of the table runs in: byte order for sort and comm, UTF-8
// for the characters the suites print. A clean container runs in it already; a desktop's
// en_GB.UTF-8 sorts "-oc" before "--opencode".
const SuiteLocale = "C.UTF-8"

// SuiteUmask is the file mode creation mask every row of the table runs under: owner-only (077),
// the umask of the CCY container and of a host `cc` session, where every suite was written and
// its recordings made. A CI runner and a plain login run under 022, so a suite comparing the
// modes of what a program created would otherwise judge the machine.
const SuiteUmask = 0o077

// osBinDirs are the distribution's own program directories. This repository installs none of
// its commands there, so a same-named program in one (ImageMagick's convert) is not a copy of
// one of them and stays reachable.
var osBinDirs = []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin"}

// Isolation is the environment every row of the table runs in, under qa-all and qa-changed
// alike, so a verdict is the code's and not the machine's (suite-env-isolated in CLAUDE/QA.md):
//
//   - the locale is SuiteLocale, whatever LANG and LC_* the machine has;
//   - no exported bash function (Fedora exports `which`), BASH_ENV or ENV reaches a suite;
//   - a PATH directory holding a command this repository ships (ShippedCommands) is replaced by
//     a stand-in holding everything else in it, so a suite finds the checkout's copy it put on
//     PATH itself, or nothing: never one a play installed on this machine;
//   - the umask is SuiteUmask. A umask belongs to the process, not to a command's environment,
//     so the runner sets it once for itself before its first command (its Env.Umask), and
//     every command inherits it.
//
// HOME is left alone: the suites give the program under test a scratch HOME themselves, and the
// tools they run (go, semgrep) keep their caches in the real one.
type Isolation struct {
	env      []string
	dropPath bool
}

// NewIsolation builds the isolation for a run of the tree at root with its scripts directory,
// whose PATH is path, writing any PATH stand-ins under scratch.
func NewIsolation(root, scripts, path, scratch string) (Isolation, error) {
	iso := Isolation{env: []string{"LC_ALL=" + SuiteLocale}}
	if path == "" {
		return iso, nil
	}
	names, err := ShippedCommands(root, scripts)
	if err != nil {
		return Isolation{}, err
	}
	masked, err := maskPath(path, names, scratch)
	if err != nil {
		return Isolation{}, err
	}
	iso.env = append(iso.env, "PATH="+masked)
	iso.dropPath = true
	return iso, nil
}

// Apply is the command run under the isolation: the inherited entries it replaces are dropped,
// its own added, then the command's own Env.
func (i Isolation) Apply(c proc.Cmd) proc.Cmd {
	c.Env = slices.Concat(i.env, c.Env)
	c.Drop = func(key string) bool {
		switch key {
		case "LANG", "LANGUAGE", "BASH_ENV", "ENV":
			return true
		case "PATH":
			return i.dropPath
		}
		return strings.HasPrefix(key, "LC_") || strings.HasPrefix(key, "BASH_FUNC_")
	}
	return c
}

// ShippedCommands are the names of the commands this repository installs on a machine: every
// command of go-binary-suites.tsv in the scripts directory (each deployed Go command has a row)
// and every file of the root's files/home/.local/bin. A table with no command, or either
// missing, is an error: an empty list would hide nothing and pass.
func ShippedCommands(root, scripts string) ([]string, error) {
	tablePath := filepath.Join(scripts, "go-binary-suites.tsv")
	f, err := os.Open(tablePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read the shipped commands: %w", err)
	}
	var names []string
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		names = append(names, strings.Fields(line)[0])
	}
	if readErr := errors.Join(scan.Err(), f.Close()); readErr != nil {
		return nil, fmt.Errorf("cannot read %s: %w", tablePath, readErr)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("%s names no command", tablePath)
	}
	entries, err := os.ReadDir(filepath.Join(root, "files", "home", ".local", "bin"))
	if err != nil {
		return nil, fmt.Errorf("cannot read the shipped commands: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// maskPath is path with each directory that holds a shipped command replaced, in place, by a
// stand-in under scratch linking to everything else in it.
func maskPath(path string, names []string, scratch string) (string, error) {
	dirs := strings.Split(path, ":")
	for i, dir := range dirs {
		if dir == "" || !filepath.IsAbs(dir) || slices.Contains(osBinDirs, filepath.Clean(dir)) {
			continue
		}
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue // a PATH entry that does not exist holds no command
		}
		if err != nil {
			return "", fmt.Errorf("cannot read PATH directory %s: %w", dir, err)
		}
		if !slices.ContainsFunc(entries, func(e os.DirEntry) bool { return slices.Contains(names, e.Name()) }) {
			continue
		}
		standIn := filepath.Join(scratch, fmt.Sprintf("path-%d", i))
		if err := os.Mkdir(standIn, 0o700); err != nil {
			return "", fmt.Errorf("cannot make the PATH stand-in for %s: %w", dir, err)
		}
		for _, e := range entries {
			if slices.Contains(names, e.Name()) {
				continue
			}
			if err := os.Symlink(filepath.Join(dir, e.Name()), filepath.Join(standIn, e.Name())); err != nil {
				return "", fmt.Errorf("cannot make the PATH stand-in for %s: %w", dir, err)
			}
		}
		dirs[i] = standIn
	}
	return strings.Join(dirs, ":"), nil
}
