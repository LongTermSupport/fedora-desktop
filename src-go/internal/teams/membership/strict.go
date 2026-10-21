package membership

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// A membership file is decoded as pingbus decodes it (helpers/pingbus/membership.py _read_json
// and _object): CPython's JSON with a key given twice refused, then every object's keys checked
// against the keys its section names, exactly, case included, every required one present, and
// every value of its JSON type. The value's grammar is then each file's check. The files are
// decoded by hand from pyjson's values, not by encoding/json, which matches a key in any case,
// keeps the last of a duplicate, takes null for any field and cannot tell a missing key from a
// zero one. The one departure: an integer past int64 is refused, where Python's int takes it;
// no writer writes one.

// decodeDoc is the one JSON document in data.
func decodeDoc(data []byte) (any, error) {
	return pyjson.DecodeUTF8(data, pyjson.Options{NoDuplicateKeys: true})
}

// decoder keeps the first thing wrong with a document, so a file's decoding reads as the list
// of its keys; after it, every read is a zero value.
type decoder struct{ err error }

func (d *decoder) failf(format string, args ...any) {
	if d.err == nil {
		d.err = fmt.Errorf(format, args...)
	}
}

// object is one JSON object of a document, where its path says ("" for the top level).
type object struct {
	d     *decoder
	m     pyjson.Object
	where string
}

// object is v, an object whose keys are every one of required and any of optional.
func (d *decoder) object(v any, where string, required []string, optional ...string) object {
	o := object{d: d, where: where}
	if d.err != nil {
		return o
	}
	m, ok := v.(pyjson.Object)
	if !ok {
		d.failf("%sis not an object", at(where))
		return o
	}
	given := slices.Sorted(maps.Keys(m))
	known := slices.Concat(required, optional)
	missing := slices.DeleteFunc(slices.Sorted(slices.Values(required)), func(k string) bool { return slices.Contains(given, k) })
	unknown := slices.DeleteFunc(given, func(k string) bool { return slices.Contains(known, k) })
	// A key in the wrong case is both missing and unknown; it is named as what it is.
	for _, key := range unknown {
		if i := slices.IndexFunc(known, func(k string) bool { return strings.EqualFold(k, key) }); i >= 0 {
			d.failf("%sthe key %q is spelt %q here: keys are matched exactly, case included", at(where), key, known[i])
			return o
		}
	}
	switch {
	case len(missing) > 0:
		d.failf("%smissing %s", at(where), strings.Join(missing, ", "))
	case len(unknown) > 0:
		d.failf("%sunknown key %q", at(where), unknown[0])
	default:
		o.m = m
	}
	return o
}

// at is where as the start of a message.
func at(where string) string {
	if where == "" {
		return ""
	}
	return where + ": "
}

func (o object) path(key string) string {
	if o.where == "" {
		return key
	}
	return o.where + "." + key
}

func (o object) has(key string) bool {
	_, ok := o.m[key]
	return ok
}

func (o object) str(key string) string {
	s, ok := o.m[key].(string)
	if !ok {
		o.d.failf("%s is not a string", o.path(key))
	}
	return s
}

func (o object) boolean(key string) bool {
	b, ok := o.m[key].(bool)
	if !ok {
		o.d.failf("%s is not true or false", o.path(key))
	}
	return b
}

// integer is a key's int: a literal Python reads as an int (1, never 1.0 or true).
func (o object) integer(key string) int { return int(o.integer64(key)) }

func (o object) integer64(key string) int64 {
	n, ok := o.m[key].(pyjson.Number)
	v, fits := n.Int64()
	if !ok || !fits {
		o.d.failf("%s is not an integer", o.path(key))
	}
	return v
}

// objects is a key's list of objects, no longer than most, each of exactly keys.
func (o object) objects(key string, most int, keys ...string) []object {
	list, ok := o.m[key].([]any)
	if !ok || len(list) > most {
		o.d.failf("%s is not a list of at most %d", o.path(key), most)
		return nil
	}
	out := make([]object, 0, len(list))
	for i, item := range list {
		out = append(out, o.d.object(item, fmt.Sprintf("%s[%d]", o.path(key), i), keys))
	}
	return out
}

// child is a key's object, of every one of keys.
func (o object) child(key string, keys ...string) object {
	return o.d.object(o.m[key], o.path(key), keys)
}
