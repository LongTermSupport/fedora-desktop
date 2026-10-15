package versionpins

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// manifestPath is the manifest, from the repository root.
const manifestPath = "vars/version-pins.yml"

// tagNull is the short tag of a null.
const tagNull = "!!null"

// topKey is the one key of the manifest.
const topKey = "version_pins"

// The kinds an installed block may name. The first three are resolved on the host; the last is a
// recorded decision not to compare a pin with the host.
const (
	kindDKMS      = "dkms"
	kindRPM       = "rpm"
	kindCommand   = "command"
	kindUntracked = "untracked"
)

// rowKeys are the keys of a pin; installedKeys the keys of its installed block.
var (
	rowKeys       = []string{"playbook", "var", "github", "tag_prefix", "note", "installed"}
	installedKeys = []string{"kind", "name", "why"}
	kinds         = []string{kindCommand, kindDKMS, kindRPM, kindUntracked}
)

// githubShape is owner/repo and nothing else.
var githubShape = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// rowDelimiters are the characters check-pinned-versions.bash splits a row on.
const rowDelimiters = "|\n\r"

// finding is one thing wrong with the manifest or a pin, at a line of the manifest.
type finding struct {
	line int
	text string
}

// pin is one well-formed row.
type pin struct {
	index    int
	line     int
	playbook string
	name     string
	tracked  bool
}

// report is everything a check found.
type report struct {
	problems []string
	findings []finding

	// pins is the number of rows declared; wellFormed those with no shape finding; reached those
	// that went through the on-disk check; tracked those whose install state is compared.
	pins       int
	wellFormed int
	reached    int
	tracked    int
}

func (r *report) problemf(format string, args ...any) {
	r.problems = append(r.problems, fmt.Sprintf(format, args...))
}

func (r *report) findf(line int, format string, args ...any) {
	r.findings = append(r.findings, finding{line: line, text: fmt.Sprintf(format, args...)})
}

// check reads the manifest, then every file its pins name.
func check(fsys fs.FS) report {
	var r report
	pins, ok := r.readManifest(fsys)
	if !ok {
		return r
	}
	if len(r.findings) == 0 && r.tracked == 0 {
		r.findf(1, "no pin tracks the installed version: %d pin(s) declared, 0 tracked, so the installed-against-pinned "+
			"check compares nothing and cannot fail; give at least one pin an installed: block that is not untracked", r.pins)
	}
	r.resolve(fsys, pins)
	slices.SortStableFunc(r.findings, func(a, b finding) int {
		if a.line != b.line {
			return a.line - b.line
		}
		return strings.Compare(a.text, b.text)
	})
	return r
}

// readManifest parses the manifest and judges its shape. It returns the well-formed pins and
// false when nothing can be judged further.
func (r *report) readManifest(fsys fs.FS) ([]pin, bool) {
	data, ok := r.readRegular(fsys, manifestPath)
	if !ok {
		return nil, false
	}
	root, ok := r.parse(manifestPath, data)
	if !ok {
		return nil, false
	}
	top := mappingOrFinding(r, root, "the top level")
	if top == nil {
		return nil, false
	}
	var rows *yaml.Node
	for _, e := range r.entries(top) {
		if e.key == topKey {
			rows = e.val
		}
	}
	if rows == nil || kindTag(rows) == tagNull || (rows.Kind == yaml.SequenceNode && len(rows.Content) == 0) {
		r.problemf("%s declares no pins; nothing was checked", manifestPath)
		return nil, false
	}
	for _, e := range r.entries(top) {
		if e.key != topKey {
			r.findf(e.keyNode.Line, "unknown top-level key '%s'", e.key)
		}
	}
	if rows.Kind != yaml.SequenceNode {
		r.findf(rows.Line, "%s must be a list, got %s", topKey, kindTag(rows))
		return nil, true
	}
	return r.readRows(rows), true
}

// readRows judges every row.
func (r *report) readRows(rows *yaml.Node) []pin {
	var pins []pin
	first := map[string]int{}
	r.pins = len(rows.Content)
	for i, raw := range rows.Content {
		row := deref(raw)
		where := fmt.Sprintf("%s[%d]", topKey, i)
		if row.Kind != yaml.MappingNode {
			r.findf(row.Line, "%s: each pin must be a mapping, got %s", where, kindTag(row))
			continue
		}
		before := len(r.findings)
		p := r.readRow(row, where, i)
		key := p.playbook + "\x00" + p.name
		if prior, dup := first[key]; dup && p.playbook != "" && p.name != "" {
			r.findf(row.Line, "%s: %s %s is declared twice (first at %s[%d])", where, p.playbook, p.name, topKey, prior)
		} else {
			first[key] = i
		}
		if len(r.findings) != before {
			continue
		}
		r.wellFormed++
		if p.tracked {
			r.tracked++
		}
		pins = append(pins, p)
	}
	return pins
}

// readRow judges one row's keys and fields.
func (r *report) readRow(row *yaml.Node, where string, index int) pin {
	p := pin{index: index, line: row.Line}
	fields := map[string]string{}
	wrongType := map[string]bool{}
	var installed *yaml.Node
	for _, e := range r.entries(row) {
		switch {
		case !slices.Contains(rowKeys, e.key):
			r.findf(e.keyNode.Line, "%s: unknown key '%s'; known keys are %s", where, e.key, strings.Join(rowKeys, ", "))
		case e.key == "installed":
			installed = e.val
		default:
			value, ok := r.stringField(e, where)
			fields[e.key], wrongType[e.key] = value, !ok
		}
	}
	p.playbook, p.name = fields["playbook"], fields["var"]
	for _, key := range []string{"playbook", "var"} {
		if fields[key] == "" && !wrongType[key] {
			r.findf(row.Line, "%s: %s is required and must not be empty", where, key)
		}
	}
	if gh := fields["github"]; gh != "" && !githubShape.MatchString(gh) {
		r.findf(row.Line, "%s: github must be owner/repo, got '%s'", where, gh)
	}
	if fields["github"] == "" && fields["note"] == "" {
		r.findf(row.Line, "%s: a pin with no github: needs a note: saying how to check it by hand", where)
	}
	p.tracked = r.readInstalled(installed, row, where)
	return p
}

// stringField reads a field that must be a string; a null reads as empty. It is false, after
// reporting why, for a value that is not a usable string.
func (r *report) stringField(e entry, where string) (string, bool) {
	n := deref(e.val)
	tag := kindTag(n)
	switch {
	case tag == tagNull:
		return "", true
	case n.Kind != yaml.ScalarNode || tag != "!!str":
		r.findf(e.keyNode.Line, "%s: %s must be a string, got %s", where, e.key, tag)
		return "", false
	}
	if i := strings.IndexAny(n.Value, rowDelimiters); i >= 0 {
		r.findf(e.keyNode.Line, "%s: %s contains a row delimiter (%q), which check-pinned-versions.bash would read as a "+
			"different row than this one", where, e.key, n.Value[i])
		return "", false
	}
	return strings.TrimSpace(n.Value), true
}

// readInstalled judges the installed block and reports whether it tracks the host.
func (r *report) readInstalled(block, row *yaml.Node, where string) bool {
	if block == nil {
		r.findf(row.Line, "%s: no installed: block. Declare a resolver, or `installed: {kind: untracked, why: ...}` — "+
			"silence is not a decision", where)
		return false
	}
	n := deref(block)
	if n.Kind != yaml.MappingNode {
		r.findf(n.Line, "%s: installed must be a mapping, got %s", where, kindTag(n))
		return false
	}
	fields := map[string]string{}
	for _, e := range r.entries(n) {
		if !slices.Contains(installedKeys, e.key) {
			r.findf(e.keyNode.Line, "%s: unknown installed key '%s'", where, e.key)
			continue
		}
		value, ok := r.stringField(e, where+".installed")
		if !ok {
			return false
		}
		fields[e.key] = value
	}
	kind := fields["kind"]
	if !slices.Contains(kinds, kind) {
		r.findf(n.Line, "%s: installed.kind must be one of %v, got '%s'", where, kinds, kind)
		return false
	}
	switch {
	case kind == kindUntracked && fields["why"] == "":
		r.findf(n.Line, "%s: installed.kind=untracked needs a why: recording the decision", where)
	case kind != kindUntracked && fields["name"] == "":
		r.findf(n.Line, "%s: installed.kind=%s needs a name: to ask for", where, kind)
	}
	return kind != kindUntracked
}

// entry is one key of a mapping.
type entry struct {
	key     string
	keyNode *yaml.Node
	val     *yaml.Node
}

// entries is the mapping's keys in order. A repeated key is a finding and only its first
// occurrence is kept; a key that is not a plain scalar is a finding and is left out.
func (r *report) entries(m *yaml.Node) []entry {
	var out []entry
	seen := map[string]bool{}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := deref(m.Content[i]), m.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			r.findf(k.Line, "a mapping key must be a plain string, got %s", kindTag(k))
			continue
		}
		if seen[k.Value] {
			r.findf(k.Line, "key '%s' is repeated", k.Value)
			continue
		}
		seen[k.Value] = true
		out = append(out, entry{key: k.Value, keyNode: k, val: v})
	}
	return out
}

// mappingOrFinding is the node as a mapping, or nil after reporting what it was.
func mappingOrFinding(r *report, n *yaml.Node, what string) *yaml.Node {
	n = deref(n)
	if n.Kind == yaml.MappingNode {
		return n
	}
	if kindTag(n) == tagNull {
		r.problemf("%s declares no pins; nothing was checked", manifestPath)
		return nil
	}
	r.findf(n.Line, "%s must be a mapping, got %s", what, kindTag(n))
	return nil
}

// deref follows an alias to the node it names.
func deref(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

// kindTag names what a node is in YAML's short tags.
func kindTag(n *yaml.Node) string {
	n = deref(n)
	switch n.Kind {
	case yaml.MappingNode:
		return "!!map"
	case yaml.SequenceNode:
		return "!!seq"
	case yaml.ScalarNode:
		return n.ShortTag()
	case yaml.DocumentNode, yaml.AliasNode:
		return "a document"
	}
	return tagNull
}

// readRegular reads a regular file; a file that is absent, linked, not regular or unreadable is a
// problem of the run.
func (r *report) readRegular(fsys fs.FS, name string) ([]byte, bool) {
	info, err := fs.Lstat(fsys, name)
	if err != nil {
		r.problemf("cannot read %s: %v", name, err)
		return nil, false
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		r.problemf("%s is a symlink; a link is not followed, so what it names would go unjudged", name)
		return nil, false
	case !info.Mode().IsRegular():
		r.problemf("%s is not a regular file", name)
		return nil, false
	}
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		r.problemf("cannot read %s: %v", name, err)
		return nil, false
	}
	return data, true
}

// parse decodes a manifest with exactly one document and returns its root node. A file with no
// document is the empty manifest.
func (r *report) parse(name string, data []byte) (*yaml.Node, bool) {
	docs, err := decodeAll(data)
	switch {
	case err != nil:
		r.problemf("%s is not valid YAML: %v", name, err)
		return nil, false
	case len(docs) == 0:
		r.problemf("%s declares no pins; nothing was checked", name)
		return nil, false
	case len(docs) > 1:
		r.problemf("%s is not valid YAML for a manifest: it holds %d documents", name, len(docs))
		return nil, false
	}
	return docs[0], true
}

// decodeAll is the root node of every document in the data.
func decodeAll(data []byte) ([]*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []*yaml.Node
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			return nil, err
		}
		if len(doc.Content) == 0 {
			continue
		}
		docs = append(docs, doc.Content[0])
	}
}
