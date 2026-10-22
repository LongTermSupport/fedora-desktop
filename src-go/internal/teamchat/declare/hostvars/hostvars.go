// Package hostvars reads the v2 declarations from an Ansible host_vars file (Plan 00209,
// DESIGN-v2-C-host.md 3.8, DC17; SPEC-n2b.md HV1-HV3). It is the one importer of the YAML
// library, and only ccy-teams imports it: `teamchat-admin` reads JSON alone (the go-deps
// keepout row holds it). Only the v2 top-level keys are read, and inside them YAML is held to
// what JSON can say: strings, integers, true and false, lists and mappings, with no tag, anchor,
// alias, merge key or scalar YAML 1.1 or 1.2 would read as anything else. The result goes
// through the same checks as the JSON reader (declare.FromValue).
package hostvars

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// maxFile is the largest host_vars file read (HV3).
const maxFile = 1 << 20

// v2Keys are the top-level keys read (DR1); every other key is ignored, whatever it holds.
var v2Keys = map[string]bool{
	paths.HostVarAddress: true, paths.HostVarTeams: true, paths.HostVarLXCMembers: true, paths.HostVarElementTeams: true,
}

const (
	mergeKey = "<<"
	theFile  = "the file"
)

var (
	decimal = regexp.MustCompile(`^(0|-?[1-9][0-9]*)$`)
	// notAString is every plain scalar YAML 1.1 (as PyYAML, Ansible's parser, resolves it) or
	// YAML 1.2 (core schema) reads as other than a string: booleans, null, integers, floats,
	// sexagesimal numbers, timestamps, the merge key and the value key.
	notAString = regexp.MustCompile(`^(?i:yes|no|on|off|y|n|true|false|null|~|<<|=)$` +
		`|^$` +
		`|^[-+]?0b[0-1_]+$|^[-+]?0[0-7_]+$|^[-+]?(0|[1-9][0-9_]*)$|^[-+]?0x[0-9a-fA-F_]+$|^0o[0-7]+$|^[-+]?[0-9]+$` +
		`|^[-+]?[0-9][0-9_]*(:[0-5]?[0-9])+(\.[0-9_]*)?$` +
		`|^[-+]?([0-9][0-9_]*)?\.[0-9_]*([eE][-+]?[0-9]+)?$|^[-+]?[0-9]+([eE][-+]?[0-9]+)$` +
		`|^[-+]?\.(inf|Inf|INF)$|^\.(nan|NaN|NAN)$` +
		`|^[0-9]{4}-[0-9]{1,2}-[0-9]{1,2}`)
)

// Read decodes a host_vars file's v2 declarations (HV1, HV2).
func Read(data []byte) (declare.Declaration, error) {
	if len(data) > maxFile {
		return declare.Declaration{}, fmt.Errorf("host_vars: over %d bytes", maxFile)
	}
	root, err := document(data)
	if err != nil {
		return declare.Declaration{}, err
	}
	var members []proto.Member
	seen := map[string]bool{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, val := root.Content[i], root.Content[i+1]
		name, err := topKey(key)
		if err != nil {
			return declare.Declaration{}, err
		}
		if !v2Keys[name] {
			continue
		}
		if seen[name] {
			return declare.Declaration{}, fail(name, "is declared twice")
		}
		seen[name] = true
		v, err := convert(val, name)
		if err != nil {
			return declare.Declaration{}, err
		}
		members = append(members, proto.M(name, v))
	}
	return declare.FromValue(proto.Obj(members...))
}

// document is the file's one document, which must be a mapping.
func document(data []byte) (*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("host_vars: not one YAML mapping: %w", err)
	}
	var next yaml.Node
	if err := dec.Decode(&next); !errors.Is(err, io.EOF) {
		return nil, errors.New("host_vars: holds more than one YAML document")
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("host_vars: is not one YAML mapping")
	}
	return doc.Content[0], nil
}

// topKey is a top-level key's name. A merge key or an alias key could carry a v2 key that is
// not written as one, so both are refused wherever they are.
func topKey(key *yaml.Node) (string, error) {
	if key.Kind != yaml.ScalarNode {
		return "", fail(theFile, "has a top-level key that is an alias or not a string")
	}
	if key.Value == mergeKey && key.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle) == 0 {
		return "", fail(mergeKey, "is a merge key, which is refused at the top level")
	}
	return key.Value, nil
}

func fail(field, problem string) error { return &declare.FieldError{Field: field, Problem: problem} }

// convert is a v2 subtree as the JSON it can only be (HV2); path names the field at fault.
func convert(n *yaml.Node, path string) (proto.Value, error) {
	switch {
	case n.Kind == yaml.AliasNode:
		return proto.Value{}, fail(path, "is an alias, which is refused")
	case n.Anchor != "":
		return proto.Value{}, fail(path, "carries an anchor, which is refused")
	case n.Style&yaml.TaggedStyle != 0 && n.Tag == "!vault":
		return proto.Value{}, fail(path, "is a vaulted value, which a declaration may not hold")
	case n.Style&yaml.TaggedStyle != 0:
		return proto.Value{}, fail(path, "carries a tag, which is refused")
	}
	switch n.Kind {
	case yaml.DocumentNode, yaml.AliasNode:
	case yaml.MappingNode:
		return mapping(n, path)
	case yaml.SequenceNode:
		items := make([]proto.Value, 0, len(n.Content))
		for i, item := range n.Content {
			v, err := convert(item, path+"["+strconv.Itoa(i)+"]")
			if err != nil {
				return proto.Value{}, err
			}
			items = append(items, v)
		}
		return proto.Arr(items...), nil
	case yaml.ScalarNode:
		return scalar(n, path)
	}
	return proto.Value{}, fail(path, "is not a string, integer, true, false, list or mapping")
}

func mapping(n *yaml.Node, path string) (proto.Value, error) {
	members := make([]proto.Member, 0, len(n.Content)/2)
	seen := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i], n.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.Anchor != "" || key.Style&yaml.TaggedStyle != 0 {
			return proto.Value{}, fail(path, "has a key that is not a plain string")
		}
		at := key.Value
		if path != "" {
			at = path + "." + key.Value
		}
		if key.Value == mergeKey && key.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle) == 0 {
			return proto.Value{}, fail(at, "is a merge key, which is refused")
		}
		if seen[key.Value] {
			return proto.Value{}, fail(at, "is declared twice")
		}
		seen[key.Value] = true
		v, err := convert(val, at)
		if err != nil {
			return proto.Value{}, err
		}
		members = append(members, proto.M(key.Value, v))
	}
	return proto.Obj(members...), nil
}

// scalar is a quoted or block scalar as a string, a plain one as true, false, a decimal integer
// or a string, and any other plain scalar refused.
func scalar(n *yaml.Node, path string) (proto.Value, error) {
	if n.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return proto.Str(n.Value), nil
	}
	switch s := n.Value; {
	case s == "true" || s == "false":
		return proto.Bool(s == "true"), nil
	case decimal.MatchString(s):
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return proto.Value{}, fail(path, "is an integer out of range")
		}
		return proto.Int(i), nil
	case notAString.MatchString(s):
		return proto.Value{}, fail(path, "is a plain value YAML reads as other than a string, an integer, true or false; quote it")
	}
	return proto.Str(n.Value), nil
}

// ReadFile reads a host_vars file (HV3). A host_vars directory, at the path or beside it as
// `<host>/`, is refused by name: Ansible merges every file in one, and ccy-teams could not know
// which to edit.
func ReadFile(path string) (declare.Declaration, error) {
	if dir, ok := strings.CutSuffix(path, ".yml"); ok {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return declare.Declaration{}, fmt.Errorf("%s is a host_vars directory; the declarations must be in %s alone", dir, path)
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return declare.Declaration{}, fmt.Errorf("cannot open %s: %w", path, err)
	}
	data, err := readCapped(f, path)
	if closeErr := f.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("cannot close %s: %w", path, closeErr)
	}
	if err != nil {
		return declare.Declaration{}, err
	}
	d, err := Read(data)
	if err != nil {
		return declare.Declaration{}, fmt.Errorf("%s: %w", path, err)
	}
	return d, nil
}

func readCapped(f *os.File, path string) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("cannot stat %s: %w", path, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory; the declarations must be in one host_vars file", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	if len(data) > maxFile {
		return nil, fmt.Errorf("%s is over %d bytes", path, maxFile)
	}
	return data, nil
}
