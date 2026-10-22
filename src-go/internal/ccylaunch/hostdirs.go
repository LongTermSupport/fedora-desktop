package ccylaunch

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// hostDir is one ccy_host_dir call of lib/host-paths.bash: an override variable, its default,
// and whether the directory may be absent (its parent must then exist).
type hostDir struct {
	env      string
	def      string
	absentOK bool
}

// hostDirs are the overrides the bash launcher checks as soon as it starts, before it answers
// --version or --help; TestHostDirsAreTheLibrarysCalls holds this table to the library.
var hostDirs = []hostDir{
	{"CCY_DOCKERFILE_DIR", "/opt/claude-yolo", false},
	{"CCY_GPU_DEVICE_DIR", "/dev/dri", true},
	{"CCY_ZONEINFO_DIR", "/usr/share/zoneinfo", false},
}

// hostDirOK reports whether ccy_host_dir accepts value as the override of a directory whose
// default is def. It only ever says yes where the bash would: on a no, or on anything it cannot
// resolve, the front hands over and the bash prints its own refusal.
func hostDirOK(value, def string, absentOK bool) bool {
	if value == "" {
		return true
	}
	if !strings.HasPrefix(value, "/") {
		return false
	}
	resolved, err := realpathM(value)
	if err != nil {
		return false
	}
	if resolved != def && inSystemTree(resolved) {
		return false
	}
	checked := resolved
	if absentOK {
		checked = filepath.Dir(resolved)
	}
	info, err := os.Stat(checked)
	return err == nil && info.IsDir()
}

// inSystemTree is /, /proc, /sys or /dev, or under one of the last three.
func inSystemTree(p string) bool {
	if p == "/" {
		return true
	}
	for _, tree := range []string{"/proc", "/sys", "/dev"} {
		if p == tree || strings.HasPrefix(p, tree+"/") {
			return true
		}
	}
	return false
}

// maxLinks bounds the links one resolution follows, as the kernel's own limit does.
const maxLinks = 40

// errLinkLoop is a path whose links do not end.
var errLinkLoop = errors.New("too many levels of symbolic links")

// realpathM is `realpath -m` of an absolute path: each symbolic link in the part of the path
// that exists is followed, and what does not exist is taken as written, ".." included.
func realpathM(p string) (string, error) {
	resolved := "/"
	rest := strings.Split(p, "/")
	links := 0
	for len(rest) > 0 {
		part := rest[0]
		rest = rest[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			resolved = filepath.Dir(resolved)
			continue
		}
		next := filepath.Join(resolved, part)
		info, err := os.Lstat(next)
		switch {
		case errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR):
			resolved = next
			continue
		case err != nil:
			return "", fmt.Errorf("resolve %s: %w", p, err)
		case info.Mode()&fs.ModeSymlink == 0:
			resolved = next
			continue
		}
		if links++; links > maxLinks {
			return "", fmt.Errorf("resolve %s: %w", p, errLinkLoop)
		}
		target, err := os.Readlink(next)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", p, err)
		}
		if strings.HasPrefix(target, "/") {
			resolved = "/"
		}
		rest = append(strings.Split(target, "/"), rest...)
	}
	return resolved, nil
}
