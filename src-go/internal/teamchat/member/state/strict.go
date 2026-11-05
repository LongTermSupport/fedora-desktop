// Package state is the member's own files (Plan 00209 B 2.3, 2.4.1, 2.5, 2.9, 2.10; SPEC-n3
// MS1, MS2, gap 4): one strict codec per file, and the seat store's writer.
//
// Every decoder is strict (B10): the file is at most its cap, proto's strict JSON (valid UTF-8,
// no repeated key, integers only, nothing after the value), an object whose `v` is 1, decoded
// into its typed struct with no unknown key, and that struct must encode back to the same JSON
// value. The round trip refuses what encoding/json would otherwise let through: a key in
// another case, a key missing, or null where a value is needed. Then the file's own rules run
// (grammars, closed sets, caps). A refusal is an *InvalidError naming the file, a code and the
// rule; it never quotes a value from the file. Every encoder runs its own decoder over what it
// would write, so nothing is written that would not be read back.
//
// Files are read and written only through a homefs root (TB4): a read refuses a link, a file
// that is not regular or not the caller's, and one over its cap; a write is an exclusive temp
// file, fsync, rename and directory fsync, mode 0600. The seat store has no exported encoder:
// it is written only by a StoreWriter, which only a held *lock.SeatLock makes (MS2, LK3).
//
// The import graph is member/state -> member/lock -> member/homefs; seat and health import
// this package for the seat lock record, and this package never imports seat.
package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/homefs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// Version is every member file's `v`.
const Version = 1

// fileMode is every member file's mode (B 2.3.1).
const fileMode = 0o600

// ErrInvalid is a member file that fails its codec (exit 78 where a verb reports it).
var ErrInvalid = errors.New("state: invalid")

// The codes of an invalid file: B 2.3.3's closed set for the desired membership, whose first
// three every file uses.
const (
	CodeJSON    = "json"
	CodeSchema  = "schema"
	CodeVersion = "version"
	CodeSeat    = "seat"
)

// InvalidError is a file refused by its codec: which file, the code, and the rule it broke.
type InvalidError struct {
	File, Code, Rule string
}

func (e *InvalidError) Error() string {
	return "state: " + e.File + " is invalid (" + e.Code + "): " + e.Rule
}

// Is makes errors.Is(err, ErrInvalid) match.
func (e *InvalidError) Is(target error) bool { return target == ErrInvalid }

// rules keeps the first rule a file broke, so a check is a straight list of needs.
type rules struct {
	rule, code string
}

// need records rule, as a schema refusal, unless ok.
func (c *rules) need(ok bool, rule string) { c.needCode(ok, CodeSchema, rule) }

func (c *rules) needCode(ok bool, code, rule string) {
	if !ok && c.rule == "" {
		c.rule, c.code = rule, code
	}
}

// decodeStrict decodes data, the file named file, into out by the package's rules, then runs
// check; it returns the parsed value for the checks a struct cannot make (an entry's size).
func decodeStrict(file string, data []byte, limit int, out any, check func(*rules)) (proto.Value, error) {
	fail := func(code, rule string) (proto.Value, error) {
		return proto.Value{}, &InvalidError{File: file, Code: code, Rule: rule}
	}
	if len(data) > limit {
		return fail(CodeSchema, fmt.Sprintf("larger than its cap of %d bytes", limit))
	}
	v, err := proto.Parse(data)
	if syntax, ok := errors.AsType[*proto.SyntaxError](err); ok {
		return fail(CodeJSON, syntax.Error())
	}
	if v.Kind() != proto.KindObject {
		return fail(CodeSchema, "not an object")
	}
	if ver, ok := v.Get("v"); !ok || ver.Kind() != proto.KindInt || ver.Int() != Version {
		return fail(CodeVersion, "v is not 1")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(out) != nil {
		return fail(CodeSchema, "a key it does not have, or a value not of its key's type or grammar")
	}
	if !sameJSON(out, v) {
		return fail(CodeSchema, "a key missing or in another case, or null where a value is needed")
	}
	c := &rules{}
	check(c)
	if c.rule != "" {
		return fail(c.code, c.rule)
	}
	return v, nil
}

// sameJSON is whether out encodes to the JSON value v.
func sameJSON(out any, v proto.Value) bool {
	again, err := json.Marshal(out)
	if err != nil {
		return false
	}
	back, err := proto.Parse(again)
	return err == nil && back.Equal(v)
}

// encode is v's file, refused unless decode reads it back.
func encode[T any](file string, v T, decode func([]byte) (T, error)) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, &InvalidError{File: file, Code: CodeSchema, Rule: "a value that is not set or not of its grammar"}
	}
	if _, err := decode(data); err != nil {
		return nil, err
	}
	return data, nil
}

// Read decodes the member file full through root by decode, reading at most limit bytes; a
// missing file is fs.ErrNotExist.
func Read[T any](root *homefs.Root, full string, limit int, decode func([]byte) (T, error)) (T, error) {
	var zero T
	f, err := root.Read(full, limit)
	if err != nil {
		return zero, err
	}
	return decode(f.Data)
}

// Encoder is a member file a plain write may replace: every file but the seat store.
type Encoder interface {
	Encode() ([]byte, error)
}

// Write replaces the member file full through root with r, mode 0600.
func Write(root *homefs.Root, full string, r Encoder) error {
	data, err := r.Encode()
	if err != nil {
		return err
	}
	return root.Replace(full, data, fileMode)
}
