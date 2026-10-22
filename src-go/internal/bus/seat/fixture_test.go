package seat

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// Placeholder values only (CLAUDE/ExampleValues.md).
const (
	testTeam   = "alpha"
	testServer = "bus.example.com"
	testHandle = "demo.1+box.podman"
)

var testRoom = "!" + strings.Repeat("A", 43)

// kv is one member.json key and its raw JSON value, in file order.
type kv struct{ key, raw string }

func baseMember(team string) []kv {
	return []kv{
		{"protocol", "1"},
		{"team", js(team)},
		{"user_id", js("@" + testHandle + ":" + testServer)},
		{"server_name", js(testServer)},
		{"base_url", js("https://" + testServer)},
		{"plain_http_hosts", `["192.0.2.1", "2001:db8::1"]`},
		{"token_file", js("token")},
		{"admin", js("@admin:" + testServer)},
		{"room", js(testRoom)},
	}
}

type edit func([]kv) []kv

// with sets key to raw: in place when present, else appended.
func with(key, raw string) edit {
	return func(doc []kv) []kv {
		for i := range doc {
			if doc[i].key == key {
				doc[i].raw = raw
				return doc
			}
		}
		return append(doc, kv{key, raw})
	}
}

// also appends key even when it is present (a duplicate key).
func also(key, raw string) edit {
	return func(doc []kv) []kv { return append(doc, kv{key, raw}) }
}

func without(key string) edit {
	return func(doc []kv) []kv {
		out := doc[:0]
		for _, e := range doc {
			if e.key != key {
				out = append(out, e)
			}
		}
		return out
	}
}

// memberDoc is a member.json for team: the valid base with the edits applied.
func memberDoc(team string, edits ...edit) string {
	doc := baseMember(team)
	for _, e := range edits {
		doc = e(doc)
	}
	parts := make([]string, 0, len(doc))
	for _, e := range doc {
		parts = append(parts, js(e.key)+": "+e.raw)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// js is s as a JSON string.
func js(s string) string {
	out, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(out)
}

// isRefusal reports whether err refuses the whole list (v1's exit 78 class).
func isRefusal(err error) bool {
	refusal, ok := errors.AsType[*RefusalError](err)
	return ok && refusal != nil
}

// realTemp is a fresh directory with its symlinks resolved, as git reports a top level.
func realTemp(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// newCheckout is a top with ccy's directory and nothing else.
func newCheckout(t *testing.T) string {
	t.Helper()
	top := realTemp(t)
	fx{t}.mkdirs(filepath.Join(top, ".claude", "ccy"))
	return top
}

func seatsRoot(top string) string {
	return filepath.Join(top, ".claude", "ccy", "pingbus", "seats")
}

// fx builds fixture trees on disk; any failure stops the test.
type fx struct{ t *testing.T }

func (f fx) check(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f fx) mkdirs(dir string) { f.t.Helper(); f.check(os.MkdirAll(dir, 0o700)) }
func (f fx) writeFile(path, data string) {
	f.t.Helper()
	f.check(os.WriteFile(path, []byte(data), 0o600))
}
func (f fx) symlink(target, link string) { f.t.Helper(); f.check(os.Symlink(target, link)) }
func (f fx) mkfifo(path string)          { f.t.Helper(); f.check(unix.Mkfifo(path, 0o600)) }
func (f fx) mksock(path string)          { f.t.Helper(); f.check(unix.Mknod(path, unix.S_IFSOCK|0o600, 0)) }
func (f fx) remove(path string)          { f.t.Helper(); f.check(os.RemoveAll(path)) }
func (f fx) rename(from, to string)      { f.t.Helper(); f.check(os.Rename(from, to)) }

// addSeat is a seat directory under top, with no files in it.
func (f fx) addSeat(top, team, seat string) string {
	f.t.Helper()
	dir := filepath.Join(seatsRoot(top), team, seat)
	f.mkdirs(dir)
	return dir
}

// addMember is a seat with a valid member.json for its team, the edits applied.
func (f fx) addMember(top, team, seat string, edits ...edit) string {
	f.t.Helper()
	dir := f.addSeat(top, team, seat)
	f.writeFile(filepath.Join(dir, memberFile), memberDoc(team, edits...))
	return dir
}
