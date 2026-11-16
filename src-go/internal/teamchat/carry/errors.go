package carry

import (
	"errors"
	"fmt"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// ErrRefused is a carry verb a rule said no to (exit 70).
var ErrRefused = errors.New("carry: refused")

// RefusedError is ErrRefused with what refused it.
type RefusedError struct{ Problem string }

func (e *RefusedError) Error() string { return e.Problem }

// Is makes a RefusedError ErrRefused.
func (e *RefusedError) Is(target error) bool { return target == ErrRefused }

func refusedf(format string, args ...any) error {
	return &RefusedError{Problem: fmt.Sprintf(format, args...)}
}

// reader reads one record or manifest strictly and keeps its first refusal, which names the
// field and never the value.
type reader struct {
	file string
	err  error
}

func (r *reader) fail(at, problem string) {
	if r.err == nil {
		r.err = fmt.Errorf("%s: %s %s", r.file, at, problem)
	}
}

// object is v's members when v is an object whose keys are exactly required plus any of
// optional.
func (r *reader) object(v proto.Value, at string, required []string, optional ...string) map[string]proto.Value {
	out := map[string]proto.Value{}
	if v.Kind() != proto.KindObject {
		r.fail(at, "is not an object")
		return out
	}
	for _, m := range v.Members() {
		if !slices.Contains(required, m.Key) && !slices.Contains(optional, m.Key) {
			r.fail(at, "has a key that is not known")
		}
		out[m.Key] = m.Val
	}
	for _, k := range required {
		if _, ok := out[k]; !ok {
			r.fail(at+"."+k, "is required")
		}
	}
	return out
}

// integer is v as a whole number in [lo, hi].
func (r *reader) integer(v proto.Value, at string, lo, hi int64) int64 {
	if v.Kind() != proto.KindInt || v.Int() < lo || v.Int() > hi {
		r.fail(at, fmt.Sprintf("is not a whole number from %d to %d", lo, hi))
		return 0
	}
	return v.Int()
}

// str is v's string, which must pass valid.
func (r *reader) str(v proto.Value, at string, valid func(string) bool) string {
	if v.Kind() != proto.KindString || !valid(v.Str()) {
		r.fail(at, "is not valid")
		return ""
	}
	return v.Str()
}
