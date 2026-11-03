package restore

import (
	"fmt"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// reader reads one record strictly and keeps its first refusal, which names the field and never
// the value.
type reader struct {
	file string
	err  error
}

func (r *reader) fail(path, problem string) {
	if r.err == nil {
		r.err = fmt.Errorf("%s: %s %s", r.file, path, problem)
	}
}

// object is v's members when v is an object whose keys are exactly required plus any of
// optional.
func (r *reader) object(v proto.Value, path string, required []string, optional ...string) map[string]proto.Value {
	out := map[string]proto.Value{}
	if v.Kind() != proto.KindObject {
		r.fail(path, "is not an object")
		return out
	}
	for _, m := range v.Members() {
		if !slices.Contains(required, m.Key) && !slices.Contains(optional, m.Key) {
			r.fail(path, "has a key that is not known")
		}
		out[m.Key] = m.Val
	}
	for _, k := range required {
		if _, ok := out[k]; !ok {
			r.fail(path+"."+k, "is required")
		}
	}
	return out
}

// integer is v as a whole number in [lo, hi].
func (r *reader) integer(v proto.Value, path string, lo, hi int64) int64 {
	if v.Kind() != proto.KindInt || v.Int() < lo || v.Int() > hi {
		r.fail(path, fmt.Sprintf("is not a whole number from %d to %d", lo, hi))
		return 0
	}
	return v.Int()
}

func (r *reader) boolean(v proto.Value, path string) bool {
	if v.Kind() != proto.KindBool {
		r.fail(path, "is not true or false")
	}
	return v.Bool()
}

// str is v's string, which must pass valid; a nullable field reads null as "".
func (r *reader) str(v proto.Value, path string, nullOK bool, valid func(string) bool) string {
	if nullOK && v.Kind() == proto.KindNull {
		return ""
	}
	if v.Kind() != proto.KindString || !valid(v.Str()) {
		r.fail(path, "is not valid")
		return ""
	}
	return v.Str()
}

// strings is v as a list of strings, each passing valid.
func (r *reader) strings(v proto.Value, path string, valid func(string) bool) []string {
	if v.Kind() != proto.KindArray {
		r.fail(path, "is not a list")
		return nil
	}
	var out []string
	for _, item := range v.Items() {
		out = append(out, r.str(item, path+"[]", false, valid))
	}
	return out
}

func strArray(list []string) proto.Value {
	items := make([]proto.Value, 0, len(list))
	for _, s := range list {
		items = append(items, proto.Str(s))
	}
	return proto.Arr(items...)
}

func nullable(s string) proto.Value {
	if s == "" {
		return proto.Null()
	}
	return proto.Str(s)
}

// nullableInt is n, or null for 0 (a backup number is never 0).
func nullableInt(n int64) proto.Value {
	if n == 0 {
		return proto.Null()
	}
	return proto.Int(n)
}

// parse is raw as one JSON value under its cap.
func parse(raw []byte, file string, limit int) (proto.Value, error) {
	if len(raw) > limit {
		return proto.Value{}, fmt.Errorf("%s: over %d bytes", file, limit)
	}
	v, err := proto.Parse(raw)
	if err != nil {
		return proto.Value{}, fmt.Errorf("%s: %w", file, err)
	}
	return v, nil
}
