package freeze

// Case accounting for the port of scripts/test-freezelib.bash (240 cases; the suite went with the
// bash library, last at commit 67c20b7f, and the golden replay in internal/freeze/golden holds what
// the bash printed), kept here so the
// disposition of every bash case is recorded next to the tests that replace it.
//
//	ported   211  same assertion, same intent (the decision table runs under both vocabularies)
//	adapted   17  the bash probed a shell mechanism; the Go test asserts the same rule on its
//	              own mechanism
//	retired   12  has no meaning in Go
//
// Adapted (17):
//   - contract, "refuses to load without an engine" (15): Config.Validate returns an error
//     naming the field. LOADED-says-nothing-else becomes "Validate returns nil".  config_test.go
//   - assert_on_host default marker paths (2): the paths are constants and NewEnv uses them.
//     host_test.go
//
// Retired (12):
//   - "it is a library, not a program" refuses to run / names "source it" (2) and the two
//     "sourcing does not change shell options" cases (2): there is no sourcing in Go.
//   - freeze_assert_contract, "the suite's own engine satisfies it" + 7 hooks named (8): the
//     Engine interface is checked by the compiler.
//
// Cases that change on purpose (bug numbers are those of SPEC-freeze.md section 10):
//   - bug 4: names given twice are acted on once (inventory_test.go, choice_test.go).
//   - bug 7: the thaw hint and the freeze note list only what was frozen (action_test.go).
//   - bug 8: an emptied machine returns the carried status (loop_test.go).
//   - the "select hook BROKE" case names the engine's error rather than "status 1".
//
// New cases with no bash twin are named TestNew... and cover the gaps of spec section 9.3 that
// belong to the library.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// vocab is one engine's pair of state words. Every decision runs under both, so a library
// that hard-coded either word fails one of them.
type vocab struct{ name, running, frozen string }

var vocabs = []vocab{
	{"podman", "running", "paused"},
	{"lxc", "RUNNING", "FROZEN"},
}

func testConfig(v vocab) Config {
	return Config{
		Tool:         "testfreeze",
		StateRunning: v.running,
		StateFrozen:  v.frozen,
		HostOnlyNote: "  the engine is not reachable from in here.",
		TargetHint:   "NAME..., or --all",
	}
}

// extraOf is what the test engine keeps per container in Entry.Data.
type extraOf struct{ mark, extra string }

// fixtureEntries is one inventory, deliberately out of order and mixed-state: charlie is a
// running container sitting at index 2 behind two frozen ones, so a predicate that reads only
// the first name it is handed gets several answers wrong.
func fixtureEntries(v vocab) []Entry {
	names := []string{"alpha", "bravo", "charlie", "delta", "echo"}
	states := []string{v.frozen, v.frozen, v.running, v.frozen, v.running}
	entries := make([]Entry, len(names))
	for i, n := range names {
		entries[i] = Entry{Name: n, State: states[i], Data: extraOf{fmt.Sprintf("m%d", i), "e-" + n}}
	}
	return entries
}

func fixtureInventory(v vocab) Inventory {
	return NewInventory(testConfig(v), fixtureEntries(v))
}

// fakeEngine is the test's engine. It records every call so a case can prove the library
// called it, and how, rather than only what it printed.
type fakeEngine struct {
	entries   []Entry // what the next Load returns
	loads     int
	acts      []string // "freeze charlie"
	actErr    error
	actOutput string
	selectFn  func(key string) ([]string, error)
	groupsFn  func(inv Inventory) []Group
	footer    string
	cfg       Config
}

func (f *fakeEngine) Preflight(context.Context) error { return nil }

func (f *fakeEngine) Load(context.Context) (Inventory, error) {
	f.loads++
	return NewInventory(f.cfg, f.entries), nil
}

func (f *fakeEngine) Groups(inv Inventory) []Group {
	if f.groupsFn != nil {
		return f.groupsFn(inv)
	}
	return []Group{{Key: "all", Description: "everything", Members: inv.Names()}}
}

func (f *fakeEngine) Select(_ context.Context, inv Inventory, key string) ([]string, error) {
	if f.selectFn != nil {
		return f.selectFn(key)
	}
	if key == "all" {
		return inv.SelectAll(), nil
	}
	return nil, Die("internal error: unknown target key '%s'.", key)
}

func (f *fakeEngine) Act(_ context.Context, verb Verb, name string) (string, error) {
	f.acts = append(f.acts, string(verb)+" "+name)
	if f.actErr != nil {
		return f.actOutput, f.actErr
	}
	return "", nil
}

func (f *fakeEngine) Columns() string { return fmt.Sprintf("%-4s %s", "MARK", "EXTRA") }

func (f *fakeEngine) Row(e Entry) string {
	d, ok := e.Data.(extraOf)
	if !ok {
		return "?"
	}
	return fmt.Sprintf("%-4s %s", d.mark, d.extra)
}

func (f *fakeEngine) ListFooter(Inventory) string { return f.footer }

func (f *fakeEngine) actsLine() string { return strings.Join(f.acts, ";") + ";" }

// scriptedChooser answers the two screens from a script.
type scriptedChooser struct {
	keys    []string // one per Group call; exhausted means quit
	groups  int
	members func(MemberScreen) (Choice, error)
	screens []MemberScreen
}

func (c *scriptedChooser) Group(_ context.Context, _, _ []string) (string, error) {
	if c.groups >= len(c.keys) {
		c.groups++
		return "", ErrQuit
	}
	k := c.keys[c.groups]
	c.groups++
	return k, nil
}

func (c *scriptedChooser) Members(_ context.Context, s MemberScreen) (Choice, error) {
	c.screens = append(c.screens, s)
	if c.members != nil {
		return c.members(s)
	}
	return Choice{All: true}, nil
}

// harness is a Session wired to buffers and the fakes.
type harness struct {
	s       *Session
	eng     *fakeEngine
	chooser *scriptedChooser
	out     *bytes.Buffer
	err     *bytes.Buffer
}

func newHarness(t *testing.T, v vocab) *harness {
	t.Helper()
	cfg := testConfig(v)
	h := &harness{
		eng:     &fakeEngine{entries: fixtureEntries(v), cfg: cfg},
		chooser: &scriptedChooser{},
		out:     &bytes.Buffer{},
		err:     &bytes.Buffer{},
	}
	env := Env{Out: h.out, Err: h.err}
	h.s = NewSession(cfg, env, h.eng, h.chooser)
	if err := h.s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	return h
}

func mustContain(t *testing.T, what, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("%s: wanted a mention of %q in:\n%s", what, needle, haystack)
	}
}

func mustLack(t *testing.T, what, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("%s: did not want a mention of %q in:\n%s", what, needle, haystack)
	}
}

func mustEq(t *testing.T, what string, got, want any) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%s: got %q, want %q", what, fmt.Sprint(got), fmt.Sprint(want))
	}
}

// dieMsg is the message of a fatal usage/engine error, or "" when err is not one.
func dieMsg(err error) string {
	d, ok := errors.AsType[*DieError](err)
	if !ok {
		return ""
	}
	return d.Msg
}
