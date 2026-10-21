package state

import (
	"fmt"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// keyOrder is which key fault v1 reports first: config.py and inbox.py name an unknown key
// before a missing one, membership.py a missing one first.
type keyOrder int

const (
	unknownFirst keyOrder = iota
	missingFirst
)

// checkKeys refuses an object whose keys are not the required ones plus some optional ones.
func checkKeys(path string, o pyjson.Ordered, required, optional []string, order keyOrder) error {
	var unknown, missing error
	for _, k := range o.Keys() {
		if !slices.Contains(required, k) && !slices.Contains(optional, k) {
			unknown = refuse(path, ClassUnknownKey, "an object has a key its format does not name")
			break
		}
	}
	for _, k := range required {
		if _, ok := o.Get(k); !ok {
			missing = refuse(path, ClassMissingKey, "an object lacks "+k)
			break
		}
	}
	if order == missingFirst && missing != nil {
		return missing
	}
	if unknown != nil {
		return unknown
	}
	return missing
}

// version is `v` (or `protocol`) being Python's int 1 (S5, SB6).
func version(path string, v any) error {
	n, ok := v.(pyjson.Number)
	switch {
	case !ok || n.Kind != pyjson.Int:
		return refuse(path, ClassType, "v is not an integer")
	case !n.IsInt(1):
		return refuse(path, ClassVersion, "v is not 1")
	}
	return nil
}

// anyInt is v as Python's int (never a bool or float), within int64.
func anyInt(path, name string, v any) (int64, error) {
	n, ok := v.(pyjson.Number)
	if !ok || n.Kind != pyjson.Int {
		return 0, refuse(path, ClassType, name+" is not an integer")
	}
	i, ok := n.Int64()
	if !ok {
		return 0, refuse(path, ClassGrammar, name+" is out of range")
	}
	return i, nil
}

// intField is v as an integer of at least lowest.
func intField(path, name string, v any, lowest int64) (int64, error) {
	i, err := anyInt(path, name, v)
	if err != nil {
		return 0, err
	}
	if i < lowest {
		return 0, refuse(path, ClassGrammar, fmt.Sprintf("%s is below %d", name, lowest))
	}
	return i, nil
}

// strField is v as a string that ok accepts.
func strField(path, name string, v any, ok func(string) bool) (string, error) {
	s, isText := v.(string)
	if !isText {
		return "", refuse(path, ClassType, name+" is not a string")
	}
	if !ok(s) {
		return "", refuse(path, ClassGrammar, name+" fails its grammar")
	}
	return s, nil
}

// nullableStr is v as null (set false) or a string that ok accepts.
func nullableStr(path, name string, v any, ok func(string) bool) (string, bool, error) {
	if _, isNull := v.(pyjson.Null); isNull {
		return "", false, nil
	}
	s, err := strField(path, name, v, ok)
	return s, err == nil, err
}

// ptr is s when set, else nil.
func ptr[T any](v T, set bool) *T {
	if !set {
		return nil
	}
	return &v
}

// listField is v as a list of no more than most entries.
func listField(path, name string, v any, most int) ([]any, error) {
	l, ok := v.([]any)
	if !ok {
		return nil, refuse(path, ClassType, name+" is not a list")
	}
	if len(l) > most {
		return nil, refuse(path, ClassSize, fmt.Sprintf("%s has more than %d entries", name, most))
	}
	return l, nil
}

// mapField is v as an object of no more than most keys.
func mapField(path, name string, v any, most int) (pyjson.Ordered, error) {
	o, ok := v.(pyjson.Ordered)
	if !ok {
		return nil, refuse(path, ClassType, name+" is not an object")
	}
	if len(o) > most {
		return nil, refuse(path, ClassSize, fmt.Sprintf("%s has more than %d keys", name, most))
	}
	return o, nil
}

// boolField is v as a bool.
func boolField(path, name string, v any) (bool, error) {
	b, ok := v.(bool)
	if !ok {
		return false, refuse(path, ClassType, name+" is not true or false")
	}
	return b, nil
}

// nullable is s for pyjson.Encode: null for nil.
func nullable(s *string) any {
	if s == nil {
		return pyjson.Null{}
	}
	return *s
}

// intOrNull is n for pyjson.Encode: null for nil.
func intOrNull(n *int64) any {
	if n == nil {
		return pyjson.Null{}
	}
	return *n
}

// strings2list is a []string for pyjson.Encode.
func strings2list(items []string) []any {
	out := make([]any, len(items))
	for i, s := range items {
		out[i] = s
	}
	return out
}
