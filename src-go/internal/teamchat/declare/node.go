package declare

import (
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// FieldError is a declaration the reader refuses: the field by its path
// (`teamchat_teams[1].humans[0]`) and what is wrong with it. It never holds the value (DR2).
type FieldError struct {
	Field, Problem string
}

func (e *FieldError) Error() string { return "declaration: " + e.Field + " " + e.Problem }

// node is one value of the input and its path.
type node struct {
	path string
	v    proto.Value
}

func (n node) fail(problem string) error { return &FieldError{Field: n.path, Problem: problem} }

func (n node) key(k string) node {
	if n.path == "" {
		return node{path: k}
	}
	return node{path: n.path + "." + k}
}

// fields is an object's members by key, for one object read with its allowed keys.
type fields struct {
	n   node
	got map[string]node
}

// object reads n as an object whose keys are all in allowed and include every key of required.
func (n node) object(required []string, optional ...string) (fields, error) {
	if n.v.Kind() != proto.KindObject {
		return fields{}, n.fail("is not an object")
	}
	known := make(map[string]bool, len(required)+len(optional))
	for _, k := range append(append([]string{}, required...), optional...) {
		known[k] = true
	}
	f := fields{n: n, got: map[string]node{}}
	for _, m := range n.v.Members() {
		if !known[m.Key] {
			return fields{}, n.key(m.Key).fail("is not a known key")
		}
		f.got[m.Key] = node{path: n.key(m.Key).path, v: m.Val}
	}
	for _, k := range required {
		if _, ok := f.got[k]; !ok {
			return fields{}, n.key(k).fail("is required")
		}
	}
	return f, nil
}

func (f fields) has(k string) bool {
	_, ok := f.got[k]
	return ok
}

// at is the member k; one not present is a node of kind null at its path, which every typed
// reader refuses.
func (f fields) at(k string) node {
	if got, ok := f.got[k]; ok {
		return got
	}
	return f.n.key(k)
}

func (n node) str() (string, error) {
	if n.v.Kind() != proto.KindString {
		return "", n.fail("is not a string")
	}
	return n.v.Str(), nil
}

func (n node) integer() (int64, error) {
	if n.v.Kind() != proto.KindInt {
		return 0, n.fail("is not an integer")
	}
	return n.v.Int(), nil
}

func (n node) boolean() (bool, error) {
	if n.v.Kind() != proto.KindBool {
		return false, n.fail("is not true or false")
	}
	return n.v.Bool(), nil
}

func (n node) list() ([]node, error) {
	if n.v.Kind() != proto.KindArray {
		return nil, n.fail("is not a list")
	}
	items := n.v.Items()
	out := make([]node, 0, len(items))
	for i, item := range items {
		out = append(out, node{path: n.path + "[" + strconv.Itoa(i) + "]", v: item})
	}
	return out, nil
}

// optStr is k's string, or def when k is absent.
func (f fields) optStr(k, def string) (string, error) {
	if !f.has(k) {
		return def, nil
	}
	return f.at(k).str()
}

// optBool is k's boolean, or def when k is absent.
func (f fields) optBool(k string, def bool) (bool, error) {
	if !f.has(k) {
		return def, nil
	}
	return f.at(k).boolean()
}

// strings reads n as a list of low to high distinct strings, each passing check; the entry at
// fault is named.
func (n node) strings(low, high int, check func(string) bool, problem string) ([]string, error) {
	items, err := n.list()
	if err != nil {
		return nil, err
	}
	if len(items) < low || len(items) > high {
		return nil, n.fail("must hold " + strconv.Itoa(low) + " to " + strconv.Itoa(high) + " entries")
	}
	out := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		s, err := item.str()
		if err != nil {
			return nil, err
		}
		if !check(s) {
			return nil, item.fail(problem)
		}
		if seen[s] {
			return nil, item.fail("is listed twice")
		}
		seen[s] = true
		out = append(out, s)
	}
	return out, nil
}

// checked reads n as a string passing check.
func (n node) checked(check func(string) bool, problem string) (string, error) {
	s, err := n.str()
	if err == nil && !check(s) {
		err = n.fail(problem)
	}
	return s, err
}
