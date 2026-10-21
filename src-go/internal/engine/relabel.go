package engine

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/shellword"
)

// SharedFileLabel is the label podman's shared relabel (":z") gives every entry under a bind.
const SharedFileLabel = "system_u:object_r:container_file_t:s0"

// ErrUnreadableUIDMap is a uid map with no range, or a line that is not three numbers: it must
// never read as "every owner is foreign" (or as "none is").
var ErrUnreadableUIDMap = errors.New("not a uid map")

// Range is one line of /proc/self/uid_map: Count uids from Inside in the namespace are Outside
// onwards on the host.
type Range struct {
	Inside, Outside, Count uint64
}

// ParseUIDMap reads a uid map as `podman unshare cat /proc/self/uid_map` prints it. Blank
// lines are skipped; every other line must be three numbers, the last not zero.
func ParseUIDMap(text string) ([]Range, error) {
	var ranges []Range
	for line := range strings.Lines(text) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		r, err := parseRange(fields)
		if err != nil {
			return nil, fmt.Errorf("%w: line %q: %w", ErrUnreadableUIDMap, strings.TrimSpace(line), err)
		}
		ranges = append(ranges, r)
	}
	if len(ranges) == 0 {
		return nil, fmt.Errorf("%w: no range in %q", ErrUnreadableUIDMap, text)
	}
	return ranges, nil
}

func parseRange(fields []string) (Range, error) {
	if len(fields) != 3 {
		return Range{}, fmt.Errorf("%d fields, want 3", len(fields))
	}
	var n [3]uint64
	for i, f := range fields {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return Range{}, fmt.Errorf("field %d: %w", i+1, err)
		}
		n[i] = v
	}
	if n[2] == 0 {
		return Range{}, errors.New("a range of no uids")
	}
	return Range{Inside: n[0], Outside: n[1], Count: n[2]}, nil
}

// Maps reports whether the host uid is inside one of the ranges: an owner rootless podman may
// relabel.
func Maps(ranges []Range, uid uint64) bool {
	for _, r := range ranges {
		if uid >= r.Outside && uid-r.Outside < r.Count {
			return true
		}
	}
	return false
}

// ForeignOwnerArgs are the find(1) predicates that select an entry whose owner is outside
// every range: `! ( <range> -o <range> ... )`, a range being `-uid N` or
// `( -uid +LO -uid -HI )`.
func ForeignOwnerArgs(ranges []Range) []string {
	preds := []string{"!", "("}
	for i, r := range ranges {
		if i > 0 {
			preds = append(preds, "-o")
		}
		switch {
		case r.Count == 1:
			preds = append(preds, "-uid", strconv.FormatUint(r.Outside, 10))
		case r.Outside == 0:
			preds = append(preds, "-uid", "-"+strconv.FormatUint(r.Count, 10))
		default:
			preds = append(preds, "(", "-uid", "+"+strconv.FormatUint(r.Outside-1, 10),
				"-uid", "-"+strconv.FormatUint(r.Outside+r.Count, 10), ")")
		}
	}
	return append(preds, ")")
}

// NeedsRelabelCheck reports whether the workspace must be checked for entries podman cannot
// relabel: only when the binds carry a relabel, and only under podman, the one engine with a
// user-namespace uid map to read (`podman unshare`).
func NeedsRelabelCheck(relabel string, kind Kind) bool {
	return relabel != "" && kind == Podman
}

// RelabelBlocks reports whether the foreign entries, given by their labels, stop podman's
// relabel. It walks as SELinux's own relabel does: when the top of the bind already carries
// the shared label, an entry that does too is skipped; otherwise every entry is set. So an
// entry blocks when it, or the top, lacks the label.
func RelabelBlocks(topLabel string, entryLabels []string) bool {
	for _, label := range entryLabels {
		if topLabel != SharedFileLabel || label != SharedFileLabel {
			return true
		}
	}
	return false
}

// LookCommand is the command that lists the foreign entries with their owners, for a person
// to run.
func LookCommand(dir string, preds []string) string {
	return quoteAll(append(findArgv(dir, preds), "-printf", `%U:%G  %p\n`))
}

// FixArgv is the find that gives every foreign entry to uid:gid, run as root, complete with
// the `+` that ends its -exec.
func FixArgv(dir string, preds []string, uid, gid int) []string {
	return append(findArgv(dir, preds), "-exec", "chown", "-h", strconv.Itoa(uid)+":"+strconv.Itoa(gid), "{}", "+")
}

// FixCommand is FixArgv as a person would type it, under sudo: the same words, quoted.
func FixCommand(dir string, preds []string, uid, gid int) string {
	return "sudo " + quoteAll(FixArgv(dir, preds, uid, gid))
}

func findArgv(dir string, preds []string) []string {
	return append([]string{"find", dir}, preds...)
}

// quoteAll is argv as a shell line, with find's closing `+` left bare.
func quoteAll(argv []string) string {
	if n := len(argv); n > 0 && argv[n-1] == "+" {
		return shellword.Join(argv[:n-1]) + " +"
	}
	return shellword.Join(argv)
}
