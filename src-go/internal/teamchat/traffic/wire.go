package traffic

import (
	"errors"
	"fmt"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// Encode is the counts on the child's wire.
func Encode(c Counts) proto.Value {
	rooms := make([]proto.Value, 0, len(c.Rooms))
	for _, r := range c.Rooms {
		rooms = append(rooms, proto.Obj(proto.M("room", proto.Str(r.Room)), proto.M("events", proto.Int(r.Events)), proto.M("pages", proto.Int(r.Pages))))
	}
	rows := make([]proto.Value, 0, len(c.Rows))
	for _, r := range c.Rows {
		rows = append(rows, proto.Obj(
			proto.M("room", proto.Str(r.Room)), proto.M("hour_ms", proto.Int(r.HourMS)), proto.M("type", proto.Str(r.Type)),
			proto.M("typed", proto.Str(r.Typed)), proto.M("rel", proto.Str(r.Rel)), proto.M("membership", proto.Str(r.Membership)),
			proto.M("count", proto.Int(r.Count)),
		))
	}
	return proto.Obj(
		proto.M("since_ms", proto.Int(c.Window.SinceMS)), proto.M("until_ms", proto.Int(c.Window.UntilMS)),
		proto.M("rooms", proto.Arr(rooms...)), proto.M("rows", proto.Arr(rows...)),
	)
}

// Decode reads the counts strictly, then holds them to Validate: they are what a
// less-privileged process wrote.
func Decode(v proto.Value) (Counts, error) {
	f := fields{}
	obj := f.object(v, "since_ms", "until_ms", "rooms", "rows")
	c := Counts{Window: Window{SinceMS: f.int(obj, "since_ms"), UntilMS: f.int(obj, "until_ms")}}
	for _, item := range f.array(obj, "rooms") {
		r := f.object(item, "room", "events", "pages")
		c.Rooms = append(c.Rooms, RoomRead{Room: f.str(r, "room"), Events: f.int(r, "events"), Pages: f.int(r, "pages")})
	}
	for _, item := range f.array(obj, "rows") {
		r := f.object(item, "room", "hour_ms", "type", "typed", "rel", "membership", "count")
		c.Rows = append(c.Rows, Row{Key: Key{
			Room: f.str(r, "room"), HourMS: f.int(r, "hour_ms"), Type: f.str(r, "type"), Typed: f.str(r, "typed"),
			Rel: f.str(r, "rel"), Membership: f.str(r, "membership"),
		}, Count: f.int(r, "count")})
	}
	if f.err != nil {
		return Counts{}, fmt.Errorf("traffic: %w", f.err)
	}
	return c, c.Validate()
}

// fields reads objects of exact keys, keeping the first failure.
type fields struct{ err error }

func (f *fields) fail(err error) {
	if f.err == nil {
		f.err = err
	}
}

func (f *fields) object(v proto.Value, want ...string) map[string]proto.Value {
	if v.Kind() != proto.KindObject {
		f.fail(errors.New("not an object"))
		return nil
	}
	out := map[string]proto.Value{}
	for _, m := range v.Members() {
		if !slices.Contains(want, m.Key) {
			f.fail(fmt.Errorf("unknown key %q", m.Key))
		}
		out[m.Key] = m.Val
	}
	for _, k := range want {
		if _, given := out[k]; !given {
			f.fail(fmt.Errorf("no %s key", k))
		}
	}
	return out
}

func (f *fields) int(obj map[string]proto.Value, key string) int64 {
	v := obj[key]
	if v.Kind() != proto.KindInt {
		f.fail(fmt.Errorf("%s is not an integer", key))
	}
	return v.Int()
}

func (f *fields) str(obj map[string]proto.Value, key string) string {
	v := obj[key]
	if v.Kind() != proto.KindString {
		f.fail(fmt.Errorf("%s is not a string", key))
	}
	return v.Str()
}

func (f *fields) array(obj map[string]proto.Value, key string) []proto.Value {
	v := obj[key]
	if v.Kind() != proto.KindArray {
		f.fail(fmt.Errorf("%s is not an array", key))
	}
	return v.Items()
}
