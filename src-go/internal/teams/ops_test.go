package teams

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestDefaultOperatorIsTheLiveOne(t *testing.T) {
	h := newHarness(t)
	if _, ok := h.app.Ops.(liveOps); !ok {
		t.Fatalf("Ops is %T", h.app.Ops)
	}
}

func TestWireKeepsAnOperatorThatIsAlreadySet(t *testing.T) {
	h := newHarness(t)
	kept := &fakeOps{}
	h.app.Ops = kept
	h.app.wire()
	if h.app.Ops != teamOperator(kept) {
		t.Fatal("wire replaced the operator")
	}
}

func TestSubcommandsReachTheirRealFlowsWithTheirUsageErrors(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"create", "--bogus"}, "ccy-teams: unknown create option '--bogus'. See ccy-teams --help."},
		{[]string{"apply"}, "ccy-teams: 'apply' takes one team, or --all. See ccy-teams --help."},
		{[]string{"remove"}, "ccy-teams: 'remove' takes one team (and no --yes: it always asks). See ccy-teams --help."},
		{[]string{"backup"}, "ccy-teams: 'backup' takes one team. See ccy-teams --help."},
		{[]string{"restore"}, "ccy-teams: "},
	}
	for _, c := range cases {
		h := newHarness(t)
		if code := h.app.Run(c.args); code != term.ExitUsage {
			t.Fatalf("%v: exit %d, stderr %q", c.args, code, h.errOut.String())
		}
		if !strings.Contains(h.errOut.String(), c.want) {
			t.Fatalf("%v: stderr %q, want %q", c.args, h.errOut.String(), c.want)
		}
	}
}

func TestCreateApplyAndRemoveRefuseWhenTheCheckoutHasGone(t *testing.T) {
	for _, sub := range []string{"create", "apply", "remove"} {
		h := newHarness(t)
		if err := os.Remove(filepath.Join(h.root, "helpers", "agent_bus", "declare.py")); err != nil {
			t.Fatal(err)
		}
		args := []string{sub, "alpha"}
		if sub == "create" {
			args = []string{sub, "--yes"}
		}
		if code := h.app.Run(args); code != term.ExitFailure {
			t.Fatalf("%s: exit %d", sub, code)
		}
		if !strings.Contains(h.errOut.String(), "has gone") {
			t.Fatalf("%s: stderr %q", sub, h.errOut.String())
		}
	}
}
