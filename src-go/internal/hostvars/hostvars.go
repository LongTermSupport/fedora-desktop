// Package hostvars is the host_vars file as the tools that edit it see it: YAML read as nodes,
// so a !vault value is data and nothing is decrypted; a top-level key's block replaced in the
// text, so every other line stays as written; the edit read back and compared before it is
// written; and the file replaced whole with its mode kept. setup-rclone and vault-list share it.
package hostvars

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
)

// VaultTag is the tag of a vault-encrypted value.
const VaultTag = "!vault"

const tagMap = "!!map"

// TopMapping is the document's top-level mapping, with no key given twice; an empty file is
// an empty mapping.
func TopMapping(data []byte) (*yaml.Node, error) {
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
	if err := UniqueKeys(top); err != nil {
		return nil, err
	}
	return top, nil
}

// UniqueKeys refuses a mapping that gives a key twice.
func UniqueKeys(mapping *yaml.Node) error {
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

// TopValue is the value of key in a mapping, or nil when it is absent.
func TopValue(top *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value == key {
			return top.Content[i+1]
		}
	}
	return nil
}

// ParseVault is ansible-vault's encrypt_string output: one key holding a !vault scalar.
func ParseVault(out []byte, key string) (*yaml.Node, error) {
	top, err := TopMapping(out)
	if err != nil {
		return nil, fmt.Errorf("ansible-vault printed something that is not YAML: %w", err)
	}
	if len(top.Content) != 2 || top.Content[0].Value != key {
		return nil, fmt.Errorf("ansible-vault did not print one %s value", key)
	}
	v := top.Content[1]
	if v.Kind != yaml.ScalarNode || v.Tag != VaultTag || v.Value == "" {
		return nil, fmt.Errorf("ansible-vault did not print a %s value for %s", VaultTag, key)
	}
	return v, nil
}

// SplitLines is text as lines, each ending in a newline.
func SplitLines(text string) []string {
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

// ReplaceBlock puts with in place of the top-level key's block, or after a blank line at the
// end when the key is absent; an empty with removes the block and one blank line beside it.
func ReplaceBlock(lines []string, key string, with []string) []string {
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
	for end < len(lines) && !endsBlock(lines, end) {
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

// startsTopLevel is a line whose first character is not a space or a tab, ignoring blank lines.
func startsTopLevel(line string) bool {
	return !blank(line) && line[0] != ' ' && line[0] != '\t'
}

// endsBlock is whether lines[i] ends the block it follows: a line that starts at the top
// level, unless it is a column-0 comment with more of the block after it (the next line that
// is neither blank nor such a comment is indented), which YAML allows inside a block.
func endsBlock(lines []string, i int) bool {
	if !startsTopLevel(lines[i]) {
		return false
	}
	for _, line := range lines[i:] {
		if !startsTopLevel(line) {
			if !blank(line) {
				return false
			}
			continue
		}
		if line[0] != '#' {
			return true
		}
	}
	return true
}

// SameOthers is nil when every top-level key but the excepted ones reads the same, in the
// same order, in before and after.
func SameOthers(before, after *yaml.Node, except ...string) error {
	others := func(top *yaml.Node) []*yaml.Node {
		var out []*yaml.Node
		for i := 0; i+1 < len(top.Content); i += 2 {
			if !slices.Contains(except, top.Content[i].Value) {
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
		if !SameNode(b[i], a[i]) || !SameNode(b[i+1], a[i+1]) {
			return fmt.Errorf("the key %s changed", b[i].Value)
		}
	}
	return nil
}

// SameNode compares what two nodes hold: kind, tag, value and content, not style, position or
// comments.
func SameNode(a, b *yaml.Node) bool {
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
		return SameNode(a.Alias, b.Alias)
	}
	for i := range a.Content {
		if !SameNode(a.Content[i], b.Content[i]) {
			return false
		}
	}
	return true
}

// WriteKeepingMode replaces the file (through a symlink, its target) whole, its mode kept.
func WriteKeepingMode(path string, data []byte) error {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("cannot resolve %s: %w", path, err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", path, err)
	}
	if err := fsx.WriteAtomic(target, "host_vars", data, info.Mode().Perm()); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	// The umask narrowed the new file's mode; the old file's is kept.
	if err := os.Chmod(target, info.Mode().Perm()); err != nil {
		return fmt.Errorf("cannot keep the mode of %s: %w", path, err)
	}
	return nil
}
