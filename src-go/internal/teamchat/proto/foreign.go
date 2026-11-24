package proto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"unicode/utf8"
)

// ForeignObject decodes a JSON object a foreign producer wrote (Claude Code's hook payload),
// which follows none of A 2.1's rules for the members teamchat does not read. It must be one
// JSON object in valid UTF-8 with nothing after it; each member named in keys is parsed by
// Parse's rules and refused if repeated (which of two values was meant cannot be told), and
// every other member is skipped whatever its form: a float, an integer past 2^53, a repeated
// key, a lone surrogate escape, a nested value. A key read that is absent is absent from the
// result (fedora-desktop#394, rule project_foreign_strict_parse).
func ForeignObject(data []byte, keys ...string) (map[string]Value, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("foreign JSON: not valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("foreign JSON: not an object")
	}
	out := map[string]Value{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("foreign JSON: %w", err)
		}
		key, isKey := tok.(string)
		if !isKey {
			return nil, errors.New("foreign JSON: a member with no key")
		}
		var raw json.RawMessage
		if err = dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("foreign JSON: the member %q: %w", key, err)
		}
		if !slices.Contains(keys, key) {
			continue
		}
		if _, seen := out[key]; seen {
			return nil, fmt.Errorf("foreign JSON: the key %q is repeated", key)
		}
		v, err := Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("foreign JSON: the key %q: %w", key, err)
		}
		out[key] = v
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, errors.New("foreign JSON: the object is not closed")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("foreign JSON: content after the object")
	}
	return out, nil
}
