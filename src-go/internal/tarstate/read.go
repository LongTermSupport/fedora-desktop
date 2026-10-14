// Package tarstate reads and writes the state tar of a team: team.json, registry.json,
// room_id and the secrets/ tree, nothing else.
//
// The tar lives in root's own directory and is restored as root, so it is treated as
// hostile input. There is exactly one parser: Read validates every member and keeps the
// validated bytes in a State, and the caller writes that State's members, never the tar
// again. The thing that was checked is the thing that is written, so a header two parsers
// would read differently (a pax path over a ustar name, a GNU long name) cannot pass one
// and reach the other.
package tarstate

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// ErrRefused is wrapped by every refusal: a member that is not a state file, a setuid bit,
// a missing required member, a limit exceeded, a corrupt archive.
var ErrRefused = errors.New("state tar refused")

// RefusedError carries every problem found, one line each.
type RefusedError struct {
	Problems []string
}

func (e *RefusedError) Error() string { return strings.Join(e.Problems, "\n") }

// Is makes errors.Is(err, ErrRefused) true.
func (e *RefusedError) Is(target error) bool { return target == ErrRefused }

// Limits bound what Read accepts. A zero field takes its default.
type Limits struct {
	MaxEntries  int   // members
	MaxFileSize int64 // bytes in one file
	MaxTotal    int64 // bytes in all files
	MaxNameLen  int   // bytes in a member name
	MaxDepth    int   // path components in a member name
}

// DefaultLimits are far above any real team state (a few small files and secrets) and far
// below anything that could exhaust memory.
func DefaultLimits() Limits {
	return Limits{MaxEntries: 4096, MaxFileSize: 16 << 20, MaxTotal: 64 << 20, MaxNameLen: 512, MaxDepth: 16}
}

func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	if l.MaxEntries <= 0 {
		l.MaxEntries = d.MaxEntries
	}
	if l.MaxFileSize <= 0 {
		l.MaxFileSize = d.MaxFileSize
	}
	if l.MaxTotal <= 0 {
		l.MaxTotal = d.MaxTotal
	}
	if l.MaxNameLen <= 0 {
		l.MaxNameLen = d.MaxNameLen
	}
	if l.MaxDepth <= 0 {
		l.MaxDepth = d.MaxDepth
	}
	return l
}

type entry struct {
	name string
	dir  bool
	data []byte
}

// State is a validated state tar. It can only be made by Read.
type State struct {
	entries []entry
	index   map[string]int
}

// Has reports whether the state holds a member (file or directory) of that name.
func (s *State) Has(name string) bool {
	_, ok := s.index[name]
	return ok
}

// File is the content of a file member.
func (s *State) File(name string) ([]byte, bool) {
	i, ok := s.index[name]
	if !ok || s.entries[i].dir {
		return nil, false
	}
	return s.entries[i].data, true
}

// Names lists every member in tar order.
func (s *State) Names() []string {
	names := make([]string, len(s.entries))
	for i, e := range s.entries {
		names[i] = e.name
	}
	return names
}

var stateFiles = map[string]bool{"team.json": true, "registry.json": true, "room_id": true}

// Read validates the tar in r and returns its content. Every problem is collected; the
// archive is read only up to its end-of-archive marker.
func Read(r io.Reader, limits Limits) (*State, error) {
	lim := limits.withDefaults()
	rd := &reader{lim: lim, state: &State{index: map[string]int{}}}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			rd.problemf("the archive is corrupt: %s", quote(err.Error()))
			break
		}
		if len(rd.state.entries)+rd.skipped >= lim.MaxEntries {
			rd.problemf("more than %d members", lim.MaxEntries)
			break
		}
		if err := rd.member(hdr, tr); err != nil {
			rd.problemf("the archive is corrupt: %s", quote(err.Error()))
			break
		}
	}
	rd.finish()
	if len(rd.problems) > 0 {
		return nil, &RefusedError{Problems: rd.problems}
	}
	return rd.state, nil
}

type reader struct {
	lim      Limits
	state    *State
	skipped  int // members seen and refused, counted against MaxEntries
	total    int64
	problems []string
}

func (rd *reader) problemf(format string, args ...any) {
	rd.problems = append(rd.problems, fmt.Sprintf(format, args...))
}

// member judges one header and, if it is a state file, reads its data. The returned
// error is an archive read failure; a refused member is a recorded problem.
func (rd *reader) member(hdr *tar.Header, tr *tar.Reader) error {
	isDir := hdr.Typeflag == tar.TypeDir
	name := hdr.Name
	if isDir {
		name = strings.TrimRight(name, "/")
	}
	label := quote(name)
	if !rd.admissible(hdr, name, isDir, label) {
		rd.skipped++
		return nil
	}
	if rd.state.Has(name) {
		rd.problemf("%s: duplicate member", label)
		rd.skipped++
		return nil
	}
	if isDir {
		rd.add(entry{name: name, dir: true})
		return nil
	}
	if hdr.Size > rd.lim.MaxFileSize {
		rd.problemf("%s: %d bytes is over the %d byte file limit", label, hdr.Size, rd.lim.MaxFileSize)
		rd.skipped++
		return nil
	}
	if rd.total+hdr.Size > rd.lim.MaxTotal {
		rd.problemf("%s: the total size of the files is over the %d byte limit", label, rd.lim.MaxTotal)
		rd.skipped++
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(tr, hdr.Size))
	if err != nil {
		return fmt.Errorf("reading %s: %w", label, err)
	}
	rd.total += int64(len(data))
	rd.add(entry{name: name, data: data})
	return nil
}

func (rd *reader) add(e entry) {
	rd.state.index[e.name] = len(rd.state.entries)
	rd.state.entries = append(rd.state.entries, e)
}

// admissible records a problem for each reason the member is not a state file.
func (rd *reader) admissible(hdr *tar.Header, name string, isDir bool, label string) bool {
	ok := true
	if !isStateMember(hdr.Typeflag, name, isDir) {
		rd.problemf("%s: not one of the state files", label)
		ok = false
	}
	if hasSparse(hdr) {
		rd.problemf("%s: a sparse file is not a state file", label)
		ok = false
	}
	if hdr.Mode&0o7000 != 0 {
		rd.problemf("%s: mode %o carries a setuid, setgid or sticky bit", label, hdr.Mode&0o7777)
		ok = false
	}
	if len(name) > rd.lim.MaxNameLen {
		rd.problemf("%s: name is longer than %d bytes", label, rd.lim.MaxNameLen)
		ok = false
	}
	if len(strings.Split(name, "/")) > rd.lim.MaxDepth {
		rd.problemf("%s: path is deeper than %d components", label, rd.lim.MaxDepth)
		ok = false
	}
	return ok
}

// isStateMember: a regular file named exactly team.json, registry.json or room_id, or a
// regular file below secrets/, or the directory secrets or a directory below it. Names
// are exact: no absolute path, no empty, "." or ".." component, no control character.
func isStateMember(typeflag byte, name string, isDir bool) bool {
	if typeflag != tar.TypeReg && typeflag != tar.TypeDir {
		return false
	}
	if hasControl(name) {
		return false
	}
	parts := strings.Split(name, "/")
	inSecrets := parts[0] == "secrets" && !slices.ContainsFunc(parts, func(p string) bool {
		return p == "" || p == "." || p == ".."
	})
	if isDir {
		return inSecrets
	}
	return stateFiles[name] || (inSecrets && len(parts) > 1)
}

func hasControl(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

func hasSparse(hdr *tar.Header) bool {
	for key := range hdr.PAXRecords {
		if strings.HasPrefix(key, "GNU.sparse") {
			return true
		}
	}
	return false
}

// finish checks what only the whole archive shows: the required members, and a file that
// is also the parent of another member.
func (rd *reader) finish() {
	for _, need := range []string{"team.json", "secrets"} {
		if !rd.state.Has(need) {
			rd.problemf("%s is missing", need)
		}
	}
	for _, e := range rd.state.entries {
		for parent := range parents(e.name) {
			if i, ok := rd.state.index[parent]; ok && !rd.state.entries[i].dir {
				rd.problemf("%s: conflicts with the file %s", quote(e.name), quote(parent))
			}
		}
	}
}

// parents yields each proper ancestor path of name.
func parents(name string) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for i := range len(name) {
			if name[i] == '/' && !yield(name[:i]) {
				return
			}
		}
	}
}

// quote renders text the way Python's repr does for the problem lines the bash printed:
// single quotes, and every control or non-printing byte escaped, so a hostile member name
// can neither forge another line nor drive the terminal.
func quote(text string) string {
	q := strconv.Quote(text)
	q = q[1 : len(q)-1]
	q = strings.ReplaceAll(q, `\"`, `"`)
	q = strings.ReplaceAll(q, `'`, `\'`)
	return "'" + q + "'"
}
