package screen

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// captures is every real capture named for the Claude Code version it was taken from
// (testdata/<what>-cc-<version>.txt), built in so a deployed binary knows the newest layout
// its screen reading was tested against without the checkout.
//
//go:embed testdata/*-cc-*.txt
var captures embed.FS

// capturesDir is where the captures sit, in the package tree and in captures.
const capturesDir = "testdata"

// versionedCapture is a capture's name: what it shows, "-cc-", the version, ".txt".
var versionedCapture = regexp.MustCompile(`^(.+)-cc-(.+)\.txt$`)

// Version is a Claude Code version, its dot-separated numeric components in order.
type Version []int

// ParseVersion reads a version of two or more dot-separated decimal components ("2.1.331"),
// and nothing else: a prefix, a suffix or an empty component is an error.
func ParseVersion(s string) (Version, error) {
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("%q is not a version: it needs at least two dot-separated numbers", s)
	}
	v := make(Version, 0, len(parts))
	for _, p := range parts {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return nil, fmt.Errorf("%q is not a version: %q is not a number", s, p)
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("%q is not a version: %w", s, err)
		}
		v = append(v, n)
	}
	return v, nil
}

// Compare is -1, 0 or 1 as v is older than, the same as or newer than o, component by
// component as numbers (2.1.331 is newer than 2.1.40); a missing component is older than any.
func (v Version) Compare(o Version) int {
	for i := range min(len(v), len(o)) {
		if v[i] != o[i] {
			if v[i] < o[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(v) < len(o):
		return -1
	case len(v) > len(o):
		return 1
	}
	return 0
}

// String is the version as it is written.
func (v Version) String() string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}

// NewestCaptured is the newest version named by a capture in fsys's testdata. A tree with no
// such capture, or a capture whose name holds no version, is an error.
func NewestCaptured(fsys fs.FS) (Version, error) {
	entries, err := fs.ReadDir(fsys, capturesDir)
	if err != nil {
		return nil, fmt.Errorf("the screen captures could not be listed: %w", err)
	}
	var newest Version
	for _, e := range entries {
		m := versionedCapture.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		v, err := ParseVersion(m[2])
		if err != nil {
			return nil, fmt.Errorf("the screen capture %s is not named for a version: %w", path.Join(capturesDir, e.Name()), err)
		}
		if newest == nil || v.Compare(newest) > 0 {
			newest = v
		}
	}
	if newest == nil {
		return nil, errors.New("no screen capture in " + capturesDir + " is named for a Claude Code version (<what>-cc-<version>.txt)")
	}
	return newest, nil
}

// LayoutDrift is what to do when the installed Claude Code is newer than every built-in
// capture, or "" when it is not. An installed version that cannot be read is an error.
func LayoutDrift(installed string) (string, error) {
	return layoutDrift(captures, installed)
}

// layoutDrift is LayoutDrift against the captures in fsys.
func layoutDrift(fsys fs.FS, installed string) (string, error) {
	have, err := ParseVersion(installed)
	if err != nil {
		return "", fmt.Errorf("the installed Claude Code version: %w", err)
	}
	newest, err := NewestCaptured(fsys)
	if err != nil {
		return "", err
	}
	if have.Compare(newest) <= 0 {
		return "", nil
	}
	return fmt.Sprintf("Claude Code %[1]s is newer than the newest screen capture the idle checks were tested on (%[2]s), "+
		"so a change in its screen layout would go unseen (fedora-desktop#391).\n"+
		"  To cover it: take two captures of a Claude Code pane with tmux capture-pane -p -e, one mid-turn (a turn running) "+
		"and one idle (an empty input box); redact them to placeholders, keeping the layout and every escape; and add them "+
		"to fedora-desktop as src-go/internal/screen/testdata/mid-turn-cc-%[1]s.txt and idle-cc-%[1]s.txt, with tests "+
		"that read them.\n", have, newest), nil
}
