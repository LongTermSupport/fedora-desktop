package declare

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/pycompat"
)

// value is declare.py's _value (SPEC-view.md V8): one value as the play reads it, in the
// subset ccy-teams writes. In v1's order: a block list when the line has no value and lines
// are nested under it; a JSON string when the value starts with '"' and decodes as one; JSON
// when it starts '[' or '{'; a quoted scalar's text; true or false; an integer; else the text.
func value(rest string, nested []string, where string) (pycompat.Value, error) {
	plain := pycompat.Strip(cutComment(rest))
	text, err := scalar(rest, where)
	if err != nil {
		return nil, err
	}
	if plain == "" {
		if len(nested) == 0 {
			return nil, refusedf("%s has no value", where)
		}
		return blockList(nested, where)
	}
	if len(nested) > 0 {
		return nil, refusedf("%s has a value and lines nested under it", where)
	}
	if quoted, ok := jsonString(rest); ok {
		return pycompat.Str(quoted), nil
	}
	switch {
	case plain[0] == '[' || plain[0] == '{':
		v, err := pycompat.Loads(plain, pycompat.LoadOptions{})
		if err != nil {
			if crash := jsonCrash(err, where); crash != nil {
				return nil, crash
			}
			return nil, refusedf("%s is a flow value ccy-teams cannot read (write it as JSON)", where)
		}
		return v, nil
	case plain[0] == '"' || plain[0] == '\'':
		return pycompat.Str(text), nil
	case text == "true" || text == "false":
		return pycompat.Bool(text == "true"), nil
	case intRE.MatchString(text):
		return integer(text, where)
	}
	return pycompat.Str(text), nil
}

// integer is python's int() of an ASCII `-?[0-9]+`: more than 4300 digits is the ValueError
// v1 crashed on.
func integer(text, where string) (pycompat.Value, error) {
	if len(strings.TrimPrefix(text, "-")) > maxIntDigits {
		return nil, crashf("%s: an integer of more than %d digits, which python's int() refuses", where, maxIntDigits)
	}
	n, ok := new(big.Int).SetString(text, 10)
	if !ok {
		return nil, crashf("%s: %q is not an integer", where, text)
	}
	return pycompat.Int{V: n}, nil
}

// jsonString is declare.py's _json_string: a double-quoted scalar read as the JSON string it
// is (a hash inside it is text, an escaped quote a quote), when nothing but a comment follows.
func jsonString(rest string) (string, bool) {
	text := pycompat.Strip(rest)
	if !strings.HasPrefix(text, `"`) {
		return "", false
	}
	s, end, err := pycompat.RawDecodeString(text)
	if err != nil {
		return "", false
	}
	tail := pycompat.Strip(text[end:])
	return s, tail == "" || strings.HasPrefix(tail, "#")
}

// blockItem is one item of a block list: a scalar's value, or a one-line mapping's keys.
type blockItem struct {
	value   pycompat.Value
	mapping *[]rawKey
}

// blockScan is _block_list's loop state: the items so far and the mapping being read.
type blockScan struct {
	indent  int
	where   string
	out     []blockItem
	pending *[]rawKey
}

func (s *blockScan) line(line string) error {
	body := pycompat.Strip(line)
	if body == "" || strings.HasPrefix(body, "#") {
		return nil
	}
	this := spaces(line)
	switch {
	case this == s.indent && strings.HasPrefix(body, "- "):
		item := pycompat.Strip(body[2:])
		key, rest, ok := keyLine(item)
		if !ok {
			v, err := value(item, nil, s.where)
			if err != nil {
				return err
			}
			s.out, s.pending = append(s.out, blockItem{value: v}), nil
			return nil
		}
		s.pending = &[]rawKey{{key: key, rest: rest}}
		s.out = append(s.out, blockItem{mapping: s.pending})
	case this > s.indent && s.pending != nil:
		key, rest, ok := keyLine(body)
		if _, dup := find(*s.pending, key); !ok || dup {
			return refusedf("%s holds a list item ccy-teams cannot read", s.where)
		}
		*s.pending = append(*s.pending, rawKey{key: key, rest: rest})
	default:
		return refusedf("%s is not a block list ccy-teams can read", s.where)
	}
	return nil
}

// blockList is declare.py's _block_list: a block list of scalars or of one-line mappings. A
// scalar item is judged during the scan, a mapping's values after it, as v1 judges them.
func blockList(lines []string, where string) (pycompat.Value, error) {
	s := blockScan{indent: spaces(lines[0]), where: where}
	for _, line := range lines {
		if err := s.line(line); err != nil {
			return nil, err
		}
	}
	out := s.out
	list := make(pycompat.List, 0, len(out))
	for _, item := range out {
		if item.mapping == nil {
			list = append(list, item.value)
			continue
		}
		d := pycompat.NewDict()
		for _, k := range *item.mapping {
			v, err := value(k.rest, nil, where+": "+k.key)
			if err != nil {
				return nil, err
			}
			d.Set(k.key, v)
		}
		list = append(list, d)
	}
	return list, nil
}

// rawValue is declare.py's _raw_value: one key's value as the play reads it, None when the
// entry does not set it (value itself never gives None); the rest of the entry is not read.
func rawValue(e entry, key string) (pycompat.Value, error) {
	k, ok := find(e.raw, key)
	if !ok {
		return pycompat.None{}, nil
	}
	return value(k.rest, k.nested, fmt.Sprintf("%s's %s on line %d", e.team, key, k.index+1))
}
