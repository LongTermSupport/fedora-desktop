package podfreeze

// Case accounting for the port of scripts/test-podfreeze.bash (187 cases; the suite went with the
// bash tool, last at commit 67c20b7f, and the golden replay in internal/freeze/golden holds what the
// bash printed), kept here so the
// disposition of every bash case is recorded next to the tests that replace it. The suite is
// white-box: it filled bash globals and called functions, so every case here drives the same
// decision through the Go engine, with the inventory built by the real Load from a scripted
// execx.Fake.
//
//	ported    169  same assertion, same intent
//	adapted    13  the rule is kept; the mechanism or the answer moved (listed below)
//	changed     1  the expectation flipped on purpose (bug 4)
//	retired     4  bash state leakage between cases
//
// Retired (4): "the fixture itself resets" (stale identity key, stale CCY key, inventory size: 3)
// and row_verb's "the fixture clears the explicit verb" (1). Each Go test builds its own
// inventory and passes the verb as an argument, so nothing can leak between cases. The spec
// counted 3 retired; the fourth is the same kind of case, filed under row_verb.
//
// Changed on purpose (bug numbers are those of SPEC-freeze.md section 10):
//   - bug 4: select_names with a repeated name keeps it once (select_test.go). The sibling
//     count_in_state case keeps its assertion (a repeated name counts twice, as the spec pins,
//     because selection is what de-duplicates); the user-visible consequence, "FREEZE 2 for one
//     container", is now unreachable and TestNewARepeatedNameLabelsOneContainerOnce says so.
//
// Adapted (13):
//   - network_names "and does not fail" (1): a Go lookup has no status to be zero.
//   - identity_matches glob (2): the bash drove a cwd with a file in it; the Go split has no
//     cwd, so "an ssh-key label of * is a literal word" (bug 3) is asserted directly, for the
//     ssh-key axis and the whole-value github axis.
//   - identity_values "a literal 'none' in the map IS offered" (1): the Go record never holds
//     "none" (the loader drops it); the loader case "none is not stored" is the same pin.
//   - warn_identity_blind_spot "nothing goes to stdout" (1): the Go engine has no stdout writer.
//   - load_inventory "a labelled container outside the inventory is in the maps / in no
//     selection" (2): the Go record lives on inventory entries, so it is simply not there.
//   - select_network (6): "returns, it does not exit" and "the previous selection is left
//     standing" (2) become the error taxonomy (a vanished network is recoverable from the menu,
//     a returned error from the command line, nothing selected); the 4 "known network" cases
//     adapt to bug 13: the members are the inventory's, in inventory order, with no fourth
//     podman query, and a second selection cannot inherit the first because there is no global.
//
// Fixes the port makes beyond the case map (bug numbers as above; each has a test):
//   - bug 1: stdout and stderr captured separately (TestNewStderrNoiseOnSuccessIsNotARecord).
//   - bug 3: ssh-key values split on whitespace only (identity_test.go).
//   - bug 5: a vanished identity value re-prompts from the menu (select_test.go).
//   - bug 9: network and identity values sort bytewise (TestNewIdentityValuesSortBytewise).
//   - bug 13: a network group comes from the inventory (select_test.go).
//   - bug 14: the label record is exactly four fields (engine_test.go).
//   - bugs 12, 22, 26 and 2 live in the shared pieces and are tested through the command line
//     here (no terminal, list with a target, one load at startup, a crashed fzf).
//
// New cases with no bash twin are named TestNew... or live in app_test.go, app_fzf_test.go and
// usage_test.go, and cover spec 9.3 for the engine and the command line; argument parsing is
// in internal/freeze/cli.

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
)

const (
	psFormat    = "{{.Names}}|{{.State}}|{{.Networks}}"
	labelFormat = `{{.Names}}|{{index .Labels "ccy-github"}}|{{index .Labels "ccy-token"}}|{{index .Labels "ccy-ssh-keys"}}`
)

var (
	psPrefix      = []string{"podman", "ps", "--all", "--format"}
	labelPrefix   = []string{"podman", "ps", "--all", "--filter", "label=ccy=true", "--format"}
	networkPrefix = []string{"podman", "network", "ls", "--format", "{{.Name}}"}
)

// The fixture is DELIBERATELY out of order, mixed-state, mixed-CCY and larger than two
// entries (the bash fixture's reasoning, kept). The four CCY sessions are four different
// kinds:
//
//	zulu_yolo      CCY by name only: a session from before CCY 3.40.0, no labels at all
//	alpha_yolo_2   labelled, a value on all three identity axes
//	bravo_browser  labelled, shares a token and an ssh key with alpha but not a github account
//	echo_yolo_3    labelled, "none" on every axis: its identity is empty exactly as zulu's is,
//	               and only HasLabels tells the two apart
//
// mike and kilo are not CCY at all; mike is running and sits at index 1.
const (
	fixturePS = `zulu_yolo|paused|podman
mike|running|
alpha_yolo_2|paused|podman, appnet
kilo|running|appnet
bravo_browser|running|podman
echo_yolo_3|paused|none
`
	fixtureLabels = `alpha_yolo_2|octo|work|key_alpha key_shared
bravo_browser|hubot|work|key_shared
echo_yolo_3|none|none|none
`
	fixtureNetworks = "podman\nappnet\nbridge\n"
)

// uniformPS: every CCY session labelled, one token between them, nothing else. The state in
// which an identity axis is a second name for "all CCY containers".
const (
	uniformPS     = "one_yolo|running|none\ntwo_yolo|paused|none\n"
	uniformLabels = "one_yolo|none|work|none\ntwo_yolo|none|work|none\n"
)

// scripted is the Fake with the three podman queries primed. Anything not primed fails
// loudly (ErrUnexpected) rather than answering "no containers".
func scripted(ps, labels, networks string) *execx.Fake {
	f := &execx.Fake{}
	f.On(psPrefix, execx.Result{Stdout: []byte(ps)})
	f.On(labelPrefix, execx.Result{Stdout: []byte(labels)})
	f.On(networkPrefix, execx.Result{Stdout: []byte(networks)})
	return f
}

func newEngine(f *execx.Fake) *Engine {
	return &Engine{
		Runner:   f,
		LookPath: func(name string) (string, error) { return "/usr/bin/" + name, nil },
		Err:      &bytes.Buffer{},
	}
}

// said is everything the engine wrote to its stderr so far.
func said(t *testing.T, e *Engine) string {
	t.Helper()
	buf, ok := e.Err.(*bytes.Buffer)
	if !ok {
		t.Fatalf("the test engine writes to a buffer, not %T", e.Err)
	}
	return buf.String()
}

func entryOf(t *testing.T, inv freeze.Inventory, name string) freeze.Entry {
	t.Helper()
	e, ok := inv.Entry(name)
	if !ok {
		t.Fatalf("%s is not in the inventory %v", name, inv.Names())
	}
	return e
}

// fixture loads the standard inventory through the real Load.
func fixture(t *testing.T) (*Engine, freeze.Inventory) {
	t.Helper()
	return load(t, fixturePS, fixtureLabels, fixtureNetworks)
}

func load(t *testing.T, ps, labels, networks string) (*Engine, freeze.Inventory) {
	t.Helper()
	e := newEngine(scripted(ps, labels, networks))
	inv, err := e.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return e, inv
}

func recordFor(t *testing.T, inv freeze.Inventory, name string) record {
	t.Helper()
	e := entryOf(t, inv, name)
	rec, ok := e.Data.(record)
	if !ok {
		t.Fatalf("%s carries %T, not a record", name, e.Data)
	}
	return rec
}

func eqStrings(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %q, want %q", what, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: got %q, want %q", what, got, want)
		}
	}
}

func dieText(t *testing.T, err error) string {
	t.Helper()
	die, ok := errors.AsType[*freeze.DieError](err)
	if !ok {
		t.Fatalf("want a *freeze.DieError, got %T: %v", err, err)
	}
	return die.Msg
}
