package setuprclone

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	configKey = "rclone_config"
	mountsKey = "rclone_mounts"
	vaultTag  = "!vault"
	tagStr    = "!!str"
	tagMap    = "!!map"
)

// mount is one rclone_mounts entry: its three values, and the entry's own node, so an edit
// keeps every other key of it (E12).
type mount struct {
	node                     *yaml.Node
	name, remote, mountpoint string
}

// newMount is an entry added here: remote and mountpoint double-quoted, as the play's
// documented example writes them (E17).
func newMount(remote, name, mountpoint string) mount {
	return mount{name: name, remote: remote, mountpoint: mountpoint}
}

// with is m with its three values replaced; the entry's other keys stay.
func (m mount) with(remote, name, mountpoint string) mount {
	return mount{node: m.node, name: name, remote: remote, mountpoint: mountpoint}
}

func (m mount) remoteName() string {
	if i := strings.IndexByte(m.remote, ':'); i >= 0 {
		return m.remote[:i]
	}
	return m.remote
}

func (m mount) folder() string {
	if i := strings.IndexByte(m.remote, ':'); i >= 0 {
		return m.remote[i+1:]
	}
	return ""
}

// encode is the entry's node with the three values written in; the node read is not changed.
func (m mount) encode() *yaml.Node {
	if m.node == nil {
		return &yaml.Node{Kind: yaml.MappingNode, Tag: tagMap, Content: []*yaml.Node{
			str("name", 0), str(m.name, 0),
			str("remote", 0), str(m.remote, yaml.DoubleQuotedStyle),
			str("mountpoint", 0), str(m.mountpoint, yaml.DoubleQuotedStyle),
		}}
	}
	node := *m.node
	node.Content = slices.Clone(m.node.Content)
	for i := 0; i+1 < len(node.Content); i += 2 {
		var value string
		switch node.Content[i].Value {
		case "name":
			value = m.name
		case "remote":
			value = m.remote
		case "mountpoint":
			value = m.mountpoint
		default:
			continue
		}
		v := *node.Content[i+1]
		v.Value, v.Tag = value, tagStr
		node.Content[i+1] = &v
	}
	return &node
}

func str(value string, style yaml.Style) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tagStr, Value: value, Style: style}
}

// hostVars is the file read (E14): its mounts.
type hostVars struct {
	mounts []mount
}

// parseHostVars reads host_vars as YAML; tags such as !vault are data and nothing is
// decrypted (E14).
func parseHostVars(data []byte) (hostVars, error) {
	top, err := topMapping(data)
	if err != nil {
		return hostVars{}, err
	}
	value := topValue(top, mountsKey)
	if value == nil || (value.Kind == yaml.ScalarNode && value.ShortTag() == "!!null") {
		return hostVars{}, nil
	}
	if value.Kind != yaml.SequenceNode {
		return hostVars{}, errors.New(mountsKey + " is not a list of mounts")
	}
	mounts := make([]mount, 0, len(value.Content))
	for i, entry := range value.Content {
		m, err := readMount(entry)
		if err != nil {
			return hostVars{}, fmt.Errorf("%s entry %d: %w", mountsKey, i+1, err)
		}
		mounts = append(mounts, m)
	}
	return hostVars{mounts: mounts}, nil
}

// topMapping is the document's top-level mapping, with no key given twice; an empty file is
// an empty mapping.
func topMapping(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("not YAML: %w", err)
	}
	if doc.Kind == 0 {
		return &yaml.Node{Kind: yaml.MappingNode, Tag: tagMap}, nil
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("not a YAML mapping")
	}
	top := doc.Content[0]
	if err := uniqueKeys(top); err != nil {
		return nil, err
	}
	return top, nil
}

func uniqueKeys(mapping *yaml.Node) error {
	seen := map[string]bool{}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		k := mapping.Content[i].Value
		if seen[k] {
			return fmt.Errorf("the key %s is given twice", k)
		}
		seen[k] = true
	}
	return nil
}

func topValue(top *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value == key {
			return top.Content[i+1]
		}
	}
	return nil
}

func readMount(entry *yaml.Node) (mount, error) {
	if entry.Kind != yaml.MappingNode {
		return mount{}, errors.New("not a mapping of name, remote and mountpoint")
	}
	if err := uniqueKeys(entry); err != nil {
		return mount{}, err
	}
	m := mount{node: entry}
	for _, field := range []struct {
		key string
		to  *string
	}{{"name", &m.name}, {"remote", &m.remote}, {"mountpoint", &m.mountpoint}} {
		v := topValue(entry, field.key)
		if v == nil || v.Kind != yaml.ScalarNode || v.ShortTag() != tagStr || v.Value == "" {
			return mount{}, fmt.Errorf("%s is not a plain string", field.key)
		}
		*field.to = v.Value
	}
	if !strings.Contains(m.remote, ":") {
		return mount{}, fmt.Errorf("remote %q is not <remote>:<path>", m.remote)
	}
	return m, nil
}

// mountsChanged reports whether after differs from the mounts read.
func mountsChanged(before, after []mount) bool {
	return !slices.EqualFunc(before, after, func(a, b mount) bool {
		return a.name == b.name && a.remote == b.remote && a.mountpoint == b.mountpoint && a.node == b.node
	})
}

// parseVault is ansible-vault's encrypt_string output: one key holding a !vault scalar (E5,
// E10).
func parseVault(out []byte, key string) (*yaml.Node, error) {
	top, err := topMapping(out)
	if err != nil {
		return nil, fmt.Errorf("ansible-vault printed something that is not YAML: %w", err)
	}
	if len(top.Content) != 2 || top.Content[0].Value != key {
		return nil, fmt.Errorf("ansible-vault did not print one %s value", key)
	}
	v := top.Content[1]
	if v.Kind != yaml.ScalarNode || v.Tag != vaultTag || v.Value == "" {
		return nil, fmt.Errorf("ansible-vault did not print a %s value for %s", vaultTag, key)
	}
	return v, nil
}

// editHostVars is the file's text with rclone_config replaced by vault (when it is not empty)
// and rclone_mounts by mounts, proven by reading it back (E17, E18).
func editHostVars(text []byte, vault string, mounts []mount) ([]byte, error) {
	before, err := topMapping(text)
	if err != nil {
		return nil, err
	}
	lines := splitLines(string(text))
	if vault != "" {
		lines = replaceBlock(lines, configKey, splitLines(vault))
	}
	block, intended, err := encodeMounts(mounts)
	if err != nil {
		return nil, err
	}
	lines = replaceBlock(lines, mountsKey, block)
	out := []byte(strings.Join(lines, ""))
	if err := verify(before, out, vault, intended); err != nil {
		return nil, fmt.Errorf("the edit would not leave host_vars as intended (%w); nothing was written", err)
	}
	return out, nil
}

// splitLines is text as lines, each ending in a newline.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if last := len(lines) - 1; !strings.HasSuffix(lines[last], "\n") {
		lines[last] += "\n"
	}
	return lines
}

// replaceBlock puts with in place of the top-level key's block, or after a blank line at the
// end when the key is absent; an empty with removes the block and one blank line beside it
// (E17).
func replaceBlock(lines []string, key string, with []string) []string {
	start := slices.IndexFunc(lines, func(l string) bool { return isKeyLine(l, key) })
	if start < 0 {
		if len(with) == 0 {
			return lines
		}
		if len(lines) > 0 {
			lines = append(lines, "\n")
		}
		return append(lines, with...)
	}
	end := start + 1
	for end < len(lines) && !startsTopLevel(lines[end]) {
		end++
	}
	for end-1 > start && blank(lines[end-1]) {
		end--
	}
	if len(with) == 0 && start > 0 && end < len(lines) && blank(lines[start-1]) && blank(lines[end]) {
		end++
	}
	return slices.Concat(lines[:start], with, lines[end:])
}

func blank(line string) bool { return strings.TrimSpace(line) == "" }

func isKeyLine(line, key string) bool {
	rest, ok := strings.CutPrefix(line, key+":")
	return ok && (rest == "" || strings.ContainsRune(" \t\r\n", rune(rest[0])))
}

// startsTopLevel is a line that ends a block: one whose first character is not a space or a
// tab, ignoring blank lines.
func startsTopLevel(line string) bool {
	return !blank(line) && line[0] != ' ' && line[0] != '\t'
}

// encodeMounts is the rclone_mounts block, and the node it must read back as.
func encodeMounts(mounts []mount) ([]string, *yaml.Node, error) {
	if len(mounts) == 0 {
		return nil, nil, nil
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, m := range mounts {
		seq.Content = append(seq.Content, m.encode())
	}
	doc := &yaml.Node{Kind: yaml.MappingNode, Tag: tagMap, Content: []*yaml.Node{str(mountsKey, 0), seq}}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, nil, fmt.Errorf("cannot write %s: %w", mountsKey, err)
	}
	if err := enc.Close(); err != nil {
		return nil, nil, fmt.Errorf("cannot write %s: %w", mountsKey, err)
	}
	return splitLines(buf.String()), seq, nil
}

// verify reads the edited text back: every other key as it was and in order, the two keys as
// intended (E18).
func verify(before *yaml.Node, out []byte, vault string, mounts *yaml.Node) error {
	after, err := topMapping(out)
	if err != nil {
		return err
	}
	if err = sameOthers(before, after); err != nil {
		return err
	}
	if !sameNode(topValue(after, mountsKey), mounts) {
		return errors.New(mountsKey + " does not read back as the mounts chosen")
	}
	want := topValue(before, configKey)
	if vault != "" {
		if want, err = parseVault([]byte(vault), configKey); err != nil {
			return err
		}
	}
	if !sameNode(topValue(after, configKey), want) {
		return errors.New(configKey + " does not read back as intended")
	}
	return nil
}

func sameOthers(before, after *yaml.Node) error {
	others := func(top *yaml.Node) []*yaml.Node {
		var out []*yaml.Node
		for i := 0; i+1 < len(top.Content); i += 2 {
			if k := top.Content[i].Value; k != configKey && k != mountsKey {
				out = append(out, top.Content[i], top.Content[i+1])
			}
		}
		return out
	}
	b, a := others(before), others(after)
	if len(b) != len(a) {
		return errors.New("the other top-level keys changed")
	}
	for i := 0; i < len(b); i += 2 {
		if !sameNode(b[i], a[i]) || !sameNode(b[i+1], a[i+1]) {
			return fmt.Errorf("the key %s changed", b[i].Value)
		}
	}
	return nil
}

// sameNode compares what two nodes hold: kind, tag, value and content, not style, position or
// comments.
func sameNode(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Kind != b.Kind || a.ShortTag() != b.ShortTag() || len(a.Content) != len(b.Content) {
		return false
	}
	if a.Kind == yaml.ScalarNode && strings.TrimRight(a.Value, "\n") != strings.TrimRight(b.Value, "\n") {
		return false
	}
	if a.Kind == yaml.AliasNode {
		return sameNode(a.Alias, b.Alias)
	}
	for i := range a.Content {
		if !sameNode(a.Content[i], b.Content[i]) {
			return false
		}
	}
	return true
}
