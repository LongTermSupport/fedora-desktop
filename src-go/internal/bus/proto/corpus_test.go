package proto

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// corpusDir is the shared corpus; its format is described in
// tests/helpers/pingbus/test_protocol_corpus.py. The Go answers every case's "expect".
const corpusDir = "../../../../tests/fixtures/agent-bus-protocol"

// adapter runs one corpus function on a case's arguments.
type adapter func(a *args) (any, error)

// corpusAdapters is every suite's adapters; a name given twice fails the walk.
func corpusAdapters(t *testing.T) map[string]adapter {
	t.Helper()
	all := map[string]adapter{}
	for _, group := range []map[string]adapter{
		grammarAdapters(), pingAdapters(), roomsAdapters(), limitsAdapters(), envelopeAdapters(),
	} {
		for name, fn := range group {
			if _, dup := all[name]; dup {
				t.Fatalf("adapter %q is defined twice", name)
			}
			all[name] = fn
		}
	}
	return all
}

func readJSON(t *testing.T, path string) *args {
	t.Helper()
	raw, err := fs.ReadFile(os.DirFS(corpusDir), filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	v, err := pyjson.DecodeUTF8(raw, pyjson.Options{NoDuplicateKeys: true})
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	obj, ok := v.(pyjson.Object)
	if !ok {
		t.Fatalf("%s: not an object", path)
	}
	return &args{obj: obj}
}

func loadContexts(t *testing.T) map[string]*Context {
	t.Helper()
	contexts := readJSON(t, filepath.Join(corpusDir, "contexts.json")).object("contexts")
	out := map[string]*Context{}
	for name := range contexts.obj {
		a := contexts.object(name)
		ctx := &Context{
			ServerName: a.str("server_name"), Humans: a.strs("humans"), Roles: map[string]string{},
			Repos: map[string][]string{}, PathPrefixes: a.strs("path_prefixes"), Exclude: a.strs("exclude"),
		}
		if org, given := a.optStr("org"); given {
			ctx.Org = org
		}
		roles := a.object("roles")
		for user := range roles.obj {
			ctx.Roles[user] = roles.str(user)
		}
		repos := a.object("repos")
		for repo := range repos.obj {
			ctx.Repos[repo] = repos.strs(repo)
		}
		for _, err := range []error{contexts.err, a.err, roles.err, repos.err} {
			if err != nil {
				t.Fatalf("context %s: %v", name, err)
			}
		}
		out[name] = ctx
	}
	return out
}

func TestCorpus(t *testing.T) {
	adapters := corpusAdapters(t)
	contexts := loadContexts(t)
	files, err := filepath.Glob(filepath.Join(corpusDir, "*.json"))
	if err != nil || len(files) < 2 {
		t.Fatalf("corpus files: %v %v", files, err)
	}
	for _, path := range files {
		suite := strings.TrimSuffix(filepath.Base(path), ".json")
		if suite == "contexts" {
			continue
		}
		data := readJSON(t, path)
		if !pyjson.EqualsInt(data.value("v"), 1) || data.str("suite") != suite {
			t.Fatalf("%s: not a v1 suite named %s", path, suite)
		}
		cases := data.list("cases")
		if data.err != nil || len(cases) == 0 {
			t.Fatalf("%s: no cases (%v)", path, data.err)
		}
		for _, raw := range cases {
			obj, ok := raw.(pyjson.Object)
			if !ok {
				t.Fatalf("%s: a case is not an object", suite)
			}
			runCase(t, suite, &args{obj: obj}, adapters, contexts)
		}
	}
}

func runCase(t *testing.T, suite string, c *args, adapters map[string]adapter, contexts map[string]*Context) {
	t.Helper()
	t.Run(suite+"/"+c.str("id"), func(t *testing.T) {
		fn, known := adapters[c.str("fn")]
		if !known {
			t.Fatalf("no adapter %q", c.str("fn"))
		}
		a := c.object("args")
		if name, named := c.obj["ctx"].(string); named {
			if a.ctx = contexts[name]; a.ctx == nil {
				t.Fatalf("no context %q", name)
			}
		}
		got, err := fn(a)
		if a.err != nil || c.err != nil {
			t.Fatalf("case: %v %v", c.err, a.err)
		}
		want := pyjson.Canonical(c.value("expect"))
		if have := pyjson.Canonical(toPy(verdictOf(got, err))); have != want {
			t.Errorf("%s: got %s, want %s", c.str("spec"), have, want)
		}
	})
}

// verdictOf spells an error as the corpus does.
func verdictOf(v any, err error) any {
	var refusal *RefusalError
	var untrusted *UntrustedError
	switch {
	case errors.As(err, &refusal):
		return pyjson.Object{"refused": string(refusal.Reason)}
	case errors.As(err, &untrusted):
		return pyjson.Object{"untrusted": untrusted.Reason}
	case err != nil:
		return pyjson.Object{"error": true}
	}
	return v
}

// toPy turns an adapter's Go result into the decoded-JSON values Canonical compares.
func toPy(v any) any {
	switch x := v.(type) {
	case nil:
		return pyjson.Null{}
	case int:
		return pyjson.Number{Lit: strconv.Itoa(x), Kind: pyjson.Int}
	case int64:
		return pyjson.Number{Lit: strconv.FormatInt(x, 10), Kind: pyjson.Int}
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = toPy(item)
		}
		return out
	case map[string]any:
		out := pyjson.Object{}
		for k, item := range x {
			out[k] = toPy(item)
		}
		return out
	case pyjson.Object:
		return toPy(map[string]any(x))
	}
	return v
}

// args reads a case's arguments, keeping the first one missing or of the wrong type.
type args struct {
	obj pyjson.Object
	ctx *Context
	err error
}

func (a *args) fail(key, want string) {
	if a.err == nil {
		a.err = fmt.Errorf("argument %q: want %s, have %s", key, want, pyjson.Canonical(a.obj[key]))
	}
}

func (a *args) value(key string) any {
	v, ok := a.obj[key]
	if !ok {
		a.fail(key, "a value")
	}
	return v
}

func (a *args) str(key string) string {
	s, ok := a.value(key).(string)
	if !ok {
		a.fail(key, "a string")
	}
	return s
}

// optStr is a string, or false for null.
func (a *args) optStr(key string) (string, bool) {
	if _, null := a.value(key).(pyjson.Null); null {
		return "", false
	}
	return a.str(key), true
}

func (a *args) list(key string) []any {
	items, ok := a.value(key).([]any)
	if !ok {
		a.fail(key, "a list")
	}
	return items
}

func (a *args) strs(key string) []string {
	items := a.list(key)
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, isStr := item.(string)
		if !isStr {
			a.fail(key, "a list of strings")
		}
		out = append(out, s)
	}
	return out
}

func (a *args) object(key string) *args {
	obj, ok := a.value(key).(pyjson.Object)
	if !ok {
		a.fail(key, "an object")
	}
	return &args{obj: obj, ctx: a.ctx}
}

func (a *args) int(key string) int64 {
	n, isNum := a.value(key).(pyjson.Number)
	v, fits := n.Int64()
	if !isNum || !fits {
		a.fail(key, "an int")
	}
	return v
}

func (a *args) context() *Context {
	if a.ctx == nil {
		a.fail("ctx", "a context")
		return &Context{}
	}
	return a.ctx
}

func TestCorpusPlumbing(t *testing.T) {
	got := pyjson.Canonical(toPy(map[string]any{"a": []string{"x"}, "b": nil, "c": int64(2), "d": 3}))
	if got != `{"a":["x"],"b":null,"c":i2,"d":i3}` {
		t.Fatal(got)
	}
	if v := verdictOf(nil, errors.New("caller")); pyjson.Canonical(v) != `{"error":true}` {
		t.Fatal(v)
	}
	a := &args{obj: pyjson.Object{"n": "x"}}
	if a.int("n"); a.err == nil {
		t.Fatal("a string read as an int")
	}
	if (&args{}).context(); a.err == nil {
		t.Fatal("no context")
	}
}
