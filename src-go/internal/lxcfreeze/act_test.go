package lxcfreeze

// The act hook, RUN (spec 5.5): the verb maps to LXC's two commands, and a thaw is an unfreeze
// followed by a DHCP renewal inside the container. The bash read the hook's text; these assert
// the argv that reaches the Fake, which is what the text scan stood in for.

import (
	"context"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
)

const (
	nmcliStatus  = "nmcli -t -f DEVICE,TYPE device status"
	nmcliConnect = "nmcli device connect"
)

// actFake answers the commands an act makes for container demo: everything succeeds, and
// NetworkManager reports one ethernet device.
func actFake() *execx.Fake {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "-v"}, execx.Result{})
	f.On([]string{"sudo", "-n", "lxc-freeze", "-n", "demo"}, execx.Result{})
	f.On([]string{"sudo", "-n", "lxc-unfreeze", "-n", "demo"}, execx.Result{})
	f.On([]string{"sudo", "-n", "lxc-attach", "-n", "demo", "--", "nmcli", "-t", "-f", "DEVICE,TYPE", "device", "status"},
		execx.Result{Stdout: []byte("eth0:ethernet\nlo:loopback\n")})
	f.On([]string{"sudo", "-n", "lxc-attach", "-n", "demo", "--", "nmcli", "device", "connect"}, execx.Result{})
	return f
}

func lines(f *execx.Fake) []string {
	calls := f.Calls()
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.Line())
	}
	return out
}

func act(f *execx.Fake, verb freeze.Verb) (string, error) {
	return newEngine(f).Act(context.Background(), verb, "demo")
}

func TestActFreezeIsLxcFreezeAndNothingElse(t *testing.T) {
	f := actFake()
	out, err := act(f, freeze.VerbFreeze)
	if err != nil || out != "" {
		t.Fatalf("got %q, %v", out, err)
	}
	// Freeze never touches the network: a freeze that reconnected would drop the address it is
	// about to freeze.
	eqStrings(t, "calls", lines(f), []string{"sudo -n lxc-freeze -n demo"})
}

func TestActThawIsUnfreezeThenTheRenewalOnEachEthernetDevice(t *testing.T) {
	f := actFake()
	f.On([]string{"sudo", "-n", "lxc-attach", "-n", "demo", "--", "nmcli", "-t", "-f", "DEVICE,TYPE", "device", "status"},
		execx.Result{Stdout: []byte("eth0:ethernet\nlo:loopback\nwlan0:wifi\neth1:ethernet\nnot-a-record\n")})
	f.On([]string{"sudo", "-n", "lxc-attach", "-n", "demo", "--", "nmcli", "device", "connect", "eth1"}, execx.Result{})
	if _, err := act(f, freeze.VerbThaw); err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "calls", lines(f), []string{
		"sudo -n lxc-unfreeze -n demo",
		"sudo -n lxc-attach -n demo -- " + nmcliStatus,
		"sudo -n lxc-attach -n demo -- " + nmcliConnect + " eth0",
		"sudo -n lxc-attach -n demo -- " + nmcliConnect + " eth1",
	})
}

// THE load-bearing case: a failed unfreeze fails the hook even though the renewal would have
// succeeded (a container that never thawed may still answer at its old address), and the renewal
// does not run — its own failure text says "thawed, but", which would be a lie.
func TestActAFailedUnfreezeFailsAndNeverRenews(t *testing.T) {
	f := actFake()
	f.On([]string{"sudo", "-n", "lxc-unfreeze", "-n", "demo"}, exit1("lxc-unfreeze: demo: failed to unfreeze\n"))
	out, err := act(f, freeze.VerbThaw)
	if err == nil {
		t.Fatal("the hook reported success for a container that is still frozen")
	}
	if out != "lxc-unfreeze: demo: failed to unfreeze" {
		t.Fatalf("the engine's own words: %q", out)
	}
	for _, l := range lines(f) {
		if strings.Contains(l, "nmcli") {
			t.Fatalf("the renewal ran after a failed unfreeze: %v", lines(f))
		}
	}
}

// The renewal's own failure still fails the hook, so the two guards are independent.
func TestActARenewalFailureAfterAGoodUnfreezeFailsAndSaysItIsThawed(t *testing.T) {
	f := actFake()
	f.On([]string{"sudo", "-n", "lxc-attach", "-n", "demo", "--", "nmcli", "device", "connect", "eth0"}, exit1("Error: Connection activation failed\n"))
	out, err := act(f, freeze.VerbThaw)
	if err == nil {
		t.Fatal("the hook succeeded")
	}
	want := "thawed, but the DHCP lease on eth0 was not renewed — nmcli failed: Error: Connection activation failed"
	if out != want {
		t.Fatalf("got %q", out)
	}
}

func TestActAFailedFreezeFailsAndNeverTouchesTheNetwork(t *testing.T) {
	f := actFake()
	f.On([]string{"sudo", "-n", "lxc-freeze", "-n", "demo"}, exit1("lxc-freeze: demo: failed\n"))
	out, err := act(f, freeze.VerbFreeze)
	if err == nil || out != "lxc-freeze: demo: failed" {
		t.Fatalf("got %q, %v", out, err)
	}
	for _, l := range lines(f) {
		if strings.Contains(l, "nmcli") {
			t.Fatalf("the freeze branch ran nmcli: %v", lines(f))
		}
	}
}

// The control: both halves succeed, so the hook succeeds. Without it every failure case above
// could be passing because the hook fails unconditionally. (TestActThawIsUnfreezeThen... reaches
// the renewal; this one names the property.)
func TestActAThawWhoseUnfreezeAndRenewalBothSucceedSucceeds(t *testing.T) {
	f := actFake()
	out, err := act(f, freeze.VerbThaw)
	if err != nil || out != "" {
		t.Fatalf("got %q, %v", out, err)
	}
	if !f.Called("sudo -n lxc-attach -n demo -- " + nmcliConnect) {
		t.Fatal("and it did reach the renewal")
	}
}

func TestRenewalMessages(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*execx.Fake)
		want  string
	}{
		{
			"nmcli itself fails",
			func(f *execx.Fake) {
				f.On([]string{"sudo", "-n", "lxc-attach", "-n", "demo", "--", "nmcli", "-t", "-f"}, exit1("Error: NetworkManager is not running.\n"))
			},
			"thawed, but the DHCP lease was not renewed — nmcli failed: Error: NetworkManager is not running.",
		},
		{
			"NetworkManager manages no ethernet device",
			func(f *execx.Fake) {
				f.On([]string{"sudo", "-n", "lxc-attach", "-n", "demo", "--", "nmcli", "-t", "-f"}, execx.Result{Stdout: []byte("lo:loopback\n")})
			},
			"thawed, but the DHCP lease was not renewed — NetworkManager manages no ethernet device",
		},
		{
			"a status with no output at all",
			func(f *execx.Fake) {
				f.On([]string{"sudo", "-n", "lxc-attach", "-n", "demo", "--", "nmcli", "-t", "-f"}, execx.Result{})
			},
			"thawed, but the DHCP lease was not renewed — NetworkManager manages no ethernet device",
		},
	}
	for _, c := range cases {
		f := actFake()
		c.setup(f)
		out, err := act(f, freeze.VerbThaw)
		if err == nil || out != c.want {
			t.Errorf("%s: got %q, %v\nwant %q", c.name, out, err, c.want)
		}
	}
}

// A renewal that fails on the first device stops there: the first message is the one reported.
func TestRenewalStopsAtTheFirstFailingDevice(t *testing.T) {
	f := actFake()
	f.On([]string{"sudo", "-n", "lxc-attach", "-n", "demo", "--", "nmcli", "-t", "-f", "DEVICE,TYPE", "device", "status"},
		execx.Result{Stdout: []byte("eth0:ethernet\neth1:ethernet\n")})
	f.On([]string{"sudo", "-n", "lxc-attach", "-n", "demo", "--", "nmcli", "device", "connect", "eth0"}, exit1("boom\n"))
	if _, err := act(f, freeze.VerbThaw); err == nil {
		t.Fatal("expected a failure")
	}
	if f.Called("sudo -n lxc-attach -n demo -- nmcli device connect eth1") {
		t.Fatalf("eth1 was attempted after eth0 failed: %v", lines(f))
	}
}

// lxc-attach chowns the file its stderr points at (AgentNotes), so both streams of every call
// must be pipes: the runner is handed no writers of its own.
func TestActGivesEveryCommandPipesNeverAFileOrTheTerminal(t *testing.T) {
	f := actFake()
	if _, err := act(f, freeze.VerbThaw); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.Calls() {
		if c.Stdout != nil || c.Stderr != nil {
			t.Errorf("%s was handed a writer", c.Line())
		}
	}
}

func TestActWithNoVerbIsAnInternalError(t *testing.T) {
	f := actFake()
	_, err := act(f, freeze.VerbDerived)
	if msg := dieText(t, err); msg != "internal error: act was handed no verb for 'demo'." {
		t.Fatalf("got %q", msg)
	}
	if len(f.Calls()) != 0 {
		t.Fatalf("nothing ran: %v", lines(f))
	}
}

func TestActACommandThatCannotStartIsAFailureOfThatContainer(t *testing.T) {
	f := actFake()
	f.OnError([]string{"sudo", "-n", "lxc-freeze"}, &execx.NotInstalledError{Name: "sudo"})
	out, err := act(f, freeze.VerbFreeze)
	if err == nil || !strings.Contains(out, "sudo is not installed") {
		t.Fatalf("got %q, %v", out, err)
	}
}

// ---- before a batch of acts (bug 6) ----

func TestBeforeActReadiesSudoSoAnIdleMenuDoesNotPromptWithoutAWord(t *testing.T) {
	f := actFake()
	e := newEngine(f)
	if err := e.BeforeAct(context.Background()); err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "calls", lines(f), []string{"sudo -n -v"})
}

func TestBeforeActRefusedSudoChangesNothingAndSaysSo(t *testing.T) {
	f := actFake()
	f.On([]string{"sudo", "-n", "-v"}, exit1("sudo: a password is required\n"))
	err := newEngine(f).BeforeAct(context.Background())
	want := "sudo was refused, so nothing was frozen or thawed.\n" +
		"  Nothing was changed. This is NOT 'nothing to do' — the question was\n" +
		"  never asked."
	if msg := dieText(t, err); msg != want {
		t.Fatalf("got %q", msg)
	}
}
