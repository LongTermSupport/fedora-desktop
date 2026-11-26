package setuprclone

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/hostvars"
)

const (
	configKey = "rclone_config"
	mountsKey = "rclone_mounts"
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
	top, err := hostvars.TopMapping(data)
	if err != nil {
		return hostVars{}, err
	}
	value := hostvars.TopValue(top, mountsKey)
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

func readMount(entry *yaml.Node) (mount, error) {
	if entry.Kind != yaml.MappingNode {
		return mount{}, errors.New("not a mapping of name, remote and mountpoint")
	}
	if err := hostvars.UniqueKeys(entry); err != nil {
		return mount{}, err
	}
	m := mount{node: entry}
	for _, field := range []struct {
		key string
		to  *string
	}{{"name", &m.name}, {"remote", &m.remote}, {"mountpoint", &m.mountpoint}} {
		v := hostvars.TopValue(entry, field.key)
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

// editHostVars is the file's text with rclone_config replaced by vault (when it is not empty)
// and rclone_mounts by mounts, proven by reading it back (E17, E18).
func editHostVars(text []byte, vault string, mounts []mount) ([]byte, error) {
	before, err := hostvars.TopMapping(text)
	if err != nil {
		return nil, err
	}
	lines := hostvars.SplitLines(string(text))
	if vault != "" {
		lines = hostvars.ReplaceBlock(lines, configKey, hostvars.SplitLines(vault))
	}
	block, intended, err := encodeMounts(mounts)
	if err != nil {
		return nil, err
	}
	lines = hostvars.ReplaceBlock(lines, mountsKey, block)
	out := []byte(strings.Join(lines, ""))
	if err := verify(before, out, vault, intended); err != nil {
		return nil, fmt.Errorf("the edit would not leave host_vars as intended (%w); nothing was written", err)
	}
	return out, nil
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
	return hostvars.SplitLines(buf.String()), seq, nil
}

// verify reads the edited text back: every other key as it was and in order, the two keys as
// intended (E18).
func verify(before *yaml.Node, out []byte, vault string, mounts *yaml.Node) error {
	after, err := hostvars.TopMapping(out)
	if err != nil {
		return err
	}
	if err = hostvars.SameOthers(before, after, configKey, mountsKey); err != nil {
		return err
	}
	if !hostvars.SameNode(hostvars.TopValue(after, mountsKey), mounts) {
		return errors.New(mountsKey + " does not read back as the mounts chosen")
	}
	want := hostvars.TopValue(before, configKey)
	if vault != "" {
		if want, err = hostvars.ParseVault([]byte(vault), configKey); err != nil {
			return err
		}
	}
	if !hostvars.SameNode(hostvars.TopValue(after, configKey), want) {
		return errors.New(configKey + " does not read back as intended")
	}
	return nil
}
