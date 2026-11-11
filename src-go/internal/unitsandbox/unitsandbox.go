// Package unitsandbox holds the rule tmpfs-file-bind (CLAUDE/QA.md): no systemd sandbox binds a
// file, or a source nobody can say is a directory, onto a path inside a TemporaryFileSystem=.
// systemd must create that mount point inside the tmpfs it has made, and a file mount point
// cannot be created there when the tmpfs is read only (EACCES, the unit exits 226/NAMESPACE).
// A directory holding the files, bound over the whole path, needs no mount point made inside.
//
// The rule reads unit text: a unit file, a drop-in, a transient unit's property lines, or a YAML
// block holding one. Drop-ins are judged alone; a tmpfs in one file and a bind in another are not
// joined.
package unitsandbox

import (
	"fmt"
	"strconv"
	"strings"
)

// ID is the rule's identifier, the heading of its entry in CLAUDE/QA.md.
const ID = "tmpfs-file-bind"

// SourceKind is what a bind's source is on the host.
type SourceKind int

// The kinds: Unknown is the zero value, so a classifier that does not know says so.
const (
	Unknown SourceKind = iota
	File
	Directory
)

// Bind is one entry of a BindPaths= or BindReadOnlyPaths= line.
type Bind struct{ Source, Dest string }

// Finding is one bind the rule refuses: its line (1-based), the tmpfs path it lies inside, and
// what its source is.
type Finding struct {
	Line  int
	Bind  Bind
	Tmpfs string
	Kind  SourceKind
}

func (f Finding) String() string {
	what := "binds a file"
	if f.Kind == Unknown {
		what = "binds a source it cannot tell is a directory"
	}
	return fmt.Sprintf("%s: line %d %s (%s) onto %s inside TemporaryFileSystem=%s; bind a directory over %s instead",
		ID, f.Line, what, f.Bind.Source, f.Bind.Dest, f.Tmpfs, f.Tmpfs)
}

// located is a bind and the line it is on.
type located struct {
	line int
	bind Bind
}

// sandbox is a unit text's tmpfs paths and binds, each list as systemd ends up with it (an empty
// assignment resets the list).
type sandbox struct {
	tmpfs []string
	binds []located
}

// parse reads the tmpfs and bind lines of text.
func parse(text string) sandbox {
	var s sandbox
	for i, raw := range strings.Split(text, "\n") {
		key, value, ok := keyValue(raw)
		if !ok {
			continue
		}
		entries := fields(value)
		switch strings.TrimSpace(key) {
		case "TemporaryFileSystem":
			if len(entries) == 0 {
				s.tmpfs = nil
			}
			for _, entry := range entries {
				s.tmpfs = append(s.tmpfs, strings.TrimSuffix(firstPart(entry), "/"))
			}
		case "BindPaths", "BindReadOnlyPaths":
			if len(entries) == 0 {
				s.binds = nil
			}
			for _, entry := range entries {
				s.binds = append(s.binds, located{i + 1, parseBind(entry)})
			}
		}
	}
	return s
}

// Check is every finding in text. kind says what a bind's source is; anything but Directory is
// a finding, so an unknown source fails closed.
func Check(text string, kind func(src string) SourceKind) []Finding {
	s := parse(text)
	return judge(s.tmpfs, s.binds, kind)
}

// judge is the binds that lie inside a tmpfs path and whose source is not a directory.
func judge(tmpfs []string, binds []located, kind func(src string) SourceKind) []Finding {
	var out []Finding
	for _, b := range binds {
		for _, t := range tmpfs {
			if !strings.HasPrefix(b.bind.Dest, t+"/") {
				continue
			}
			if k := kind(b.bind.Source); k != Directory {
				out = append(out, Finding{Line: b.line, Bind: b.bind, Tmpfs: t, Kind: k})
			}
			break
		}
	}
	return out
}

// keyValue is a line's directive and value, a systemd-run --property= argument's included.
func keyValue(raw string) (string, string, bool) {
	key, value, ok := strings.Cut(unwrap(raw), "=")
	if ok && strings.TrimSpace(key) == "--property" {
		key, value, ok = strings.Cut(value, "=")
	}
	return strings.TrimSpace(key), value, ok
}

// unwrap is a line with its code wrapping taken off, so a property written as a string literal
// in a list (Python's f"Key=value", or a quoted shell argument) reads as the unit line it is: the
// indentation, a line continuation, a trailing comma, the closing quote, and the opening quote
// with any string prefix letters before it.
func unwrap(line string) string {
	s := strings.TrimSpace(line)
	s = strings.TrimSpace(strings.TrimSuffix(s, `\`))
	s = strings.TrimSuffix(s, ",")
	trimmed := strings.TrimLeft(s, "rbfuRBFU")
	if trimmed == "" || (trimmed[0] != '"' && trimmed[0] != '\'') {
		return s
	}
	return strings.TrimSuffix(trimmed[1:], trimmed[:1])
}

// fields splits a list value into its entries, honouring double quotes.
func fields(value string) []string {
	out := make([]string, 0, strings.Count(value, " ")+1)
	for f := range strings.FieldsSeq(value) {
		if unq, err := strconv.Unquote(f); err == nil {
			f = unq
		}
		out = append(out, f)
	}
	return out
}

// firstPart is s up to its first colon.
func firstPart(s string) string { return strings.SplitN(s, ":", 2)[0] }

// parseBind reads `[-][+]source[:dest[:options]]`; with no dest the source is bound onto itself.
func parseBind(entry string) Bind {
	entry = strings.TrimLeft(entry, "-+")
	src := firstPart(entry)
	dest := src
	if len(entry) > len(src) {
		dest = firstPart(entry[len(src)+1:])
	}
	return Bind{Source: src, Dest: strings.TrimSuffix(dest, "/")}
}
