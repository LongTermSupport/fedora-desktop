package lxcfreeze

// Case accounting for the port of scripts/test-lxcfreeze.bash (116 cases; the suite went with the
// bash tool, last at commit 67c20b7f, and the golden replay in internal/freeze/golden holds what the
// bash printed), kept here so the
// disposition of every bash case is recorded next to the tests that replace it. The suite was
// white-box: it filled bash globals and called functions. Every case here drives the same
// decision through the Go engine, the inventory built by the real Load from a scripted
// execx.Fake behind sudo.Capture.
//
//	ported    100  same assertion, same intent
//	adapted    12  the rule is kept; the mechanism moved
//	retired     4  no meaning in Go
//
// Adapted (12):
//   - the act hook's text scans (9) and the preflight's (2): the bash read function bodies with
//     `declare -f`; the Go tests assert the argv that reaches the Fake (act_test.go,
//     engine_test.go), which is what the text scan stood in for.
//   - "a second selection replaces the first" (1): the bash selection was a global that could
//     go stale; TestSelectReturnsAFreshSelectionEachTime.
//
// Retired (4):
//   - "the parser's word and the library's are the same" and "and so are the frozen pair" (2):
//     two bash variables that had to agree; Config and the parser share one set of constants.
//   - "a broken grep is fatal, not an empty address" and "says grep is what failed" (2): the
//     parsers use no grep. The intent, that a probe which could not answer is never reported as
//     "no address", is held by TestLoadAFailedAddressProbeIsUnknownNotBlank.
//
// Fixes beyond the case map (bug numbers as in SPEC-freeze.md section 10), each with a test:
// 1 stdout and stderr captured apart; 5 a vanished bridge re-prompts from the menu; 6 sudo is
// re-readied before each load and act batch, then used with -n; 9 bridges sort bytewise; 18 an
// IPv4 address is parsed; 22 list with a target is refused before sudo; 25 the unreadable note
// is printed once per load; 26 one load at startup. Bugs 2, 4, 7, 8, 12 live in the shared
// pieces and are tested through the command line. New cases with no bash twin are named
// TestNew..., or live in app_test.go, app_fzf_test.go and usage_test.go.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/sudo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// box is one container as the host would answer for it. The raw fields are what the commands
// print; the fail flags make a command exit non-zero.
type box struct {
	name       string
	info       string // lxc-info -n NAME -s
	config     string // /var/lib/lxc/NAME/config
	addr       string // lxc-info -n NAME -iH
	infoFail   bool
	configFail bool
	addrFail   bool
}

func stateLine(state string) string { return "State:          " + state + "\n" }

func bridgeConfig(bridge string) string {
	return "lxc.uts.name = box\nlxc.net.0.type = veth\nlxc.net.0.link = " + bridge + "\nlxc.net.0.flags = up\n"
}

// The fixture is DELIBERATELY out of order, mixed-state and larger than two entries (the bash
// fixture's reasoning, kept). alpha sits AFTER two frozen entries, kilo is on no bridge, and
// the blank addresses have two different reasons: mike is FROZEN, kilo is on no bridge.
// Addresses are RFC 5737 documentation addresses.
func fixtureBoxes() []box {
	return []box{
		{name: "zulu", info: stateLine("FROZEN"), config: bridgeConfig("lxcbr0"), addr: "192.0.2.11\n"},
		{name: "mike", info: stateLine("FROZEN"), config: bridgeConfig("virbr1")},
		{name: "alpha", info: stateLine("RUNNING"), config: bridgeConfig("lxcbr0"), addr: "192.0.2.12\n"},
		{name: "kilo", info: stateLine("FROZEN"), config: "lxc.uts.name = box\n"},
		{name: "bravo", info: stateLine("RUNNING"), config: bridgeConfig("virbr1"), addr: "198.51.100.5\n"},
	}
}

func names(boxes []box) string {
	var b strings.Builder
	for _, x := range boxes {
		b.WriteString(x.name + "\n")
	}
	return b.String()
}

func exit1(stderr string) execx.Result { return execx.Result{Stderr: []byte(stderr), ExitCode: 1} }

func answer(stdout string, fail bool, stderr string) execx.Result {
	if fail {
		return exit1(stderr)
	}
	return execx.Result{Stdout: []byte(stdout)}
}

// script primes every command a Load makes. Anything not primed fails loudly (ErrUnexpected)
// rather than answering "no containers".
func script(f *execx.Fake, boxes []box) {
	f.On([]string{"sudo", "-n", "-v"}, execx.Result{})
	f.On([]string{"sudo", "-n", "lxc-ls", "-1"}, execx.Result{Stdout: []byte(names(boxes))})
	for _, x := range boxes {
		f.On([]string{"sudo", "-n", "lxc-info", "-n", x.name, "-s"}, answer(x.info, x.infoFail, "lxc-info: "+x.name+": Container is not defined\n"))
		f.On([]string{"sudo", "-n", "cat", "/var/lib/lxc/" + x.name + "/config"}, answer(x.config, x.configFail, "cat: /var/lib/lxc/"+x.name+"/config: Permission denied\n"))
		f.On([]string{"sudo", "-n", "lxc-info", "-n", x.name, "-iH"}, answer(x.addr, x.addrFail, "lxc-info: unrecognised option\n"))
	}
}

func scripted(boxes []box) *execx.Fake {
	f := &execx.Fake{}
	script(f, boxes)
	return f
}

// newSudo is the production Sudo shape over a Fake: no terminal unless a test says so.
func newSudo(f *execx.Fake, tty bool) *sudo.Sudo {
	return &sudo.Sudo{
		Runner: f,
		Term: &term.Term{
			Err:        &bytes.Buffer{},
			IsTerminal: func(any) bool { return tty },
			Getenv:     func(string) string { return "" },
		},
		Stdin:  strings.NewReader(""),
		Stdout: &bytes.Buffer{},
		OpenTTY: func() (io.ReadCloser, error) {
			if !tty {
				return nil, errors.New("no tty")
			}
			return io.NopCloser(strings.NewReader("")), nil
		},
	}
}

func newEngine(f *execx.Fake) *Engine {
	return &Engine{
		Sudo:     newSudo(f, false),
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

// load builds the inventory through the real Load.
func load(t *testing.T, boxes []box) (*Engine, freeze.Inventory[record]) {
	t.Helper()
	e := newEngine(scripted(boxes))
	inv, err := e.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return e, inv
}

func fixture(t *testing.T) (*Engine, freeze.Inventory[record]) {
	t.Helper()
	return load(t, fixtureBoxes())
}

func entryOf(t *testing.T, inv freeze.Inventory[record], name string) freeze.Entry[record] {
	t.Helper()
	e, ok := inv.Entry(name)
	if !ok {
		t.Fatalf("%s is not in the inventory %v", name, inv.Names())
	}
	return e
}

func recordFor(t *testing.T, inv freeze.Inventory[record], name string) record {
	t.Helper()
	e := entryOf(t, inv, name)
	return e.Record
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

func mustContain(t *testing.T, what, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("%s: wanted a mention of %q in:\n%s", what, needle, haystack)
	}
}
