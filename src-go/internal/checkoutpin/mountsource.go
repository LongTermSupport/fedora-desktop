package checkoutpin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
)

// resolveSource is the real path a mounts source names (K8): every link on the way followed.
// A source not there yet resolves through its nearest existing directory, with the rest of
// its path as written, so a source made later as that directory is the same source and one
// made as a link is not. there is whether the source itself exists.
func resolveSource(src string) (resolved string, there bool, err error) {
	resolved, err = filepath.EvalSymlinks(src)
	if err == nil {
		return resolved, true, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", false, err
	}
	rest := filepath.Base(src)
	for dir := filepath.Dir(src); ; dir = filepath.Dir(dir) {
		realDir, err := filepath.EvalSymlinks(dir)
		if err == nil {
			return filepath.Join(realDir, rest), false, nil
		}
		if !errors.Is(err, fs.ErrNotExist) || dir == "/" {
			return "", false, err
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}

// writableSource is a host path a container of this launch may write, and what lets it.
type writableSource struct {
	path, by string
	line     int
}

// extraWritable are CCY_EXTRA_MOUNTS' read-write sources (`-v src:dst[:opts]`, space
// separated), each resolved; a source that does not resolve binds nothing to write.
func extraWritable(extra string) ([]writableSource, error) {
	var out []writableSource
	fields := strings.Fields(extra)
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] != "-v" && fields[i] != "--volume" {
			continue
		}
		parts := strings.Split(fields[i+1], ":")
		if len(parts) < 2 || (len(parts) > 2 && strings.Contains(","+parts[2]+",", ",ro,")) {
			continue
		}
		realPath, err := filepath.EvalSymlinks(parts[0])
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("CCY_EXTRA_MOUNTS source %s cannot be resolved: %w", parts[0], err)
		}
		out = append(out, writableSource{path: realPath, by: "CCY_EXTRA_MOUNTS"})
	}
	return out, nil
}

// writableProblem is why a resolved source lies where a container of this launch could write
// a part of it, and so make that part a link to any host path; "" when it does not. A source
// is judged against every other writable source, never against its own line.
func writableProblem(resolved string, line int, writable []writableSource) string {
	for _, w := range writable {
		if w.line == line && w.by == "" {
			continue
		}
		if resolved == w.path || under(resolved, w.path) {
			by := w.by
			if by == "" {
				by = Mounts + " line " + strconv.Itoa(w.line)
			}
			return "host path " + resolved + " is inside " + w.path + ", which a container of this launch writes (" + by +
				", read-write), so it could make a part of the path a link to any host path"
		}
	}
	return ""
}

// mountsApproval is the digest a mounts approval records (K8): the file's bytes and, for each
// mount, where its source resolves, so a link on the way retargeted after the approval is a
// change like any other.
func mountsApproval(content []byte, mounts []Mount, resolved []string) string {
	text := string(content)
	for i, m := range mounts {
		text += "\x00" + strconv.Itoa(m.Line) + "\x00" + resolved[i]
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
