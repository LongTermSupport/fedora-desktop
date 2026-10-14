package lxcfreeze

// The engine's own work: what it declares to the library, the preflight, and the inventory the
// real Load builds from lxc-ls, lxc-info and the container config (spec 5.1 to 5.3).

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/freeze"
)

// ---- what the tool declares to the library ----

func TestConfigDeclaresLXCsWords(t *testing.T) {
	cfg := Config()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Tool != "lxcfreeze" || cfg.StateRunning != "RUNNING" || cfg.StateFrozen != "FROZEN" {
		t.Fatalf("got %+v", cfg)
	}
	// The unknown-name error gains LXC's extra sentence: a STOPPED container is not in the
	// inventory, and without saying so the message reads as "no such container".
	mustContain(t, "the list note explains the STOPPED exclusion", cfg.ListNote, "STOPPED")
}

// What a long freeze costs, said before it costs it: thaw renews the lease, so the container
// returns reachable, but every ssh session into it died with the frozen connection.
func TestConfigFreezeNote(t *testing.T) {
	note := Config().FreezeNote
	if note == "" {
		t.Fatal("the tool declares a freeze-time note")
	}
	mustContain(t, "it names the connection loss", note, "ssh")
	mustContain(t, "and says reconnecting is the fix", note, "Reconnect")
	mustContain(t, "and that the lease itself is renewed", note, "lease")
}

// ---- the preflight (spec 5.1) ----

func TestPreflightNamesEveryMissingLXCTool(t *testing.T) {
	e := newEngine(&execx.Fake{})
	e.LookPath = func(name string) (string, error) {
		if name == "lxc-ls" || name == "lxc-freeze" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	msg := dieText(t, e.Preflight(context.Background()))
	want := "LXC is not installed on this host — missing: lxc-ls lxc-freeze\n" +
		"  This is not 'no containers': nothing here can answer the question at all.\n" +
		"  LXC is declared in playbooks/imports/play-lxc-install-config.yml, which\n" +
		"  playbook-main.yml imports. Deploy it with:\n" +
		"    ansible-playbook playbooks/imports/play-lxc-install-config.yml\n" +
		"  Do NOT install it by hand."
	if msg != want {
		t.Fatalf("got %q", msg)
	}
}

func TestPreflightWithNoSudoSaysSo(t *testing.T) {
	e := newEngine(&execx.Fake{})
	e.LookPath = func(name string) (string, error) {
		if name == "sudo" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	if msg := dieText(t, e.Preflight(context.Background())); msg != "no sudo on this host, and every LXC query needs root." {
		t.Fatalf("got %q", msg)
	}
}

// Both guards run, and neither alone: LXC absent and sudo absent are different failures with
// different remedies. LXC is checked first.
func TestPreflightChecksBothGuardsAndLXCFirst(t *testing.T) {
	e := newEngine(&execx.Fake{})
	var asked []string
	e.LookPath = func(name string) (string, error) {
		asked = append(asked, name)
		return "", errors.New("not found")
	}
	msg := dieText(t, e.Preflight(context.Background()))
	mustContain(t, "LXC is the first failure", msg, "LXC is not installed")
	eqStrings(t, "every tool asked about", asked, []string{"lxc-ls", "lxc-info", "lxc-freeze", "lxc-unfreeze"})

	asked = nil
	e.LookPath = func(name string) (string, error) {
		asked = append(asked, name)
		return "/usr/bin/" + name, nil
	}
	if err := e.Preflight(context.Background()); err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "and then sudo", asked, []string{"lxc-ls", "lxc-info", "lxc-freeze", "lxc-unfreeze", "sudo"})
}

// ---- Load (spec 5.2) ----

func TestLoadBuildsTheInventoryInLxcLsOrderWithEachContainersBridgeAndAddress(t *testing.T) {
	_, inv := fixture(t)
	eqStrings(t, "names", inv.Names(), []string{"zulu", "mike", "alpha", "kilo", "bravo"})
	want := map[string]struct{ state, bridge, addr string }{
		"zulu":  {"FROZEN", "lxcbr0", "192.0.2.11"},
		"mike":  {"FROZEN", "virbr1", ""},
		"alpha": {"RUNNING", "lxcbr0", "192.0.2.12"},
		"kilo":  {"FROZEN", bridgeNone, ""},
		"bravo": {"RUNNING", "virbr1", "198.51.100.5"},
	}
	for name, w := range want {
		e := entryOf(t, inv, name)
		rec := recordFor(t, inv, name)
		if e.State != w.state || rec.bridge != w.bridge || rec.ipv4 != w.addr {
			t.Errorf("%s: %s %+v, want %+v", name, e.State, rec, w)
		}
	}
}

// The fixture's addresses are index-parallel with its names: the Go record travels with the
// entry, so a fixture one short cannot hand a row the wrong address.
func TestLoadEachEntryCarriesItsOwnRecord(t *testing.T) {
	_, inv := fixture(t)
	for _, e := range inv.Entries() {
		if _, ok := e.Data.(record); !ok {
			t.Errorf("%s carries %T", e.Name, e.Data)
		}
	}
}

func TestLoadAStoppedContainerIsDroppedSilently(t *testing.T) {
	boxes := append(fixtureBoxes(), box{name: "off", info: stateLine("STOPPED"), config: bridgeConfig("lxcbr0")})
	e, inv := load(t, boxes)
	if _, ok := inv.Entry("off"); ok {
		t.Fatal("a STOPPED container is in neither list")
	}
	if said(t, e) != "" {
		t.Fatalf("by design, not a warning: %q", said(t, e))
	}
}

// A container whose state cannot be read is in NO group and gets no verb. It is announced once
// per load, naming it.
func TestLoadAnUnreadableContainerIsNamedOnceAndHasNoEntry(t *testing.T) {
	boxes := fixtureBoxes()
	boxes[1].infoFail = true                                                         // mike vanished between lxc-ls and lxc-info
	boxes = append(boxes, box{name: "odd", info: stateLine("ABORTING")})             // a state word we do not know
	boxes = append(boxes, box{name: "mute", info: "lxc-info: no State line here\n"}) // no State line at all
	e, inv := load(t, boxes)
	eqStrings(t, "inventory", inv.Names(), []string{"zulu", "alpha", "kilo", "bravo"})
	want := "lxcfreeze: NOTE — 3 container(s) could not be read,\n" +
		"  so they are in NO group, NO count above and NO selection:\n" +
		"    mike\n    odd\n    mute\n" +
		"  Either they went away between 'lxc-ls' and 'lxc-info' — a real race, and\n" +
		"  harmless — or 'sudo lxc-info -n NAME -s' fails for them. Run that to see\n" +
		"  which. A container of unknown state is never sent a verb.\n"
	if said(t, e) != want {
		t.Fatalf("got %q", said(t, e))
	}
}

func TestLoadWarnsOncePerLoadNotOncePerRun(t *testing.T) {
	boxes := fixtureBoxes()
	boxes[0].infoFail = true
	e := newEngine(scripted(boxes))
	for range 2 {
		if _, err := e.Load(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(said(t, e), "NOTE — 1 container(s) could not be read"); n != 2 {
		t.Fatalf("one note per load, two loads: %d in %q", n, said(t, e))
	}
}

func TestLoadAnUnreadableConfigKeepsTheContainerWithAnHonestLabel(t *testing.T) {
	boxes := fixtureBoxes()
	boxes[2].configFail = true
	_, inv := load(t, boxes)
	rec := recordFor(t, inv, "alpha")
	if rec.bridge != bridgeUnreadable || rec.ipv4 != "192.0.2.12" {
		t.Fatalf("got %+v", rec)
	}
	if entryOf(t, inv, "alpha").State != "RUNNING" {
		t.Fatal("a failed config probe never drops the container")
	}
}

func TestLoadAFailedAddressProbeIsUnknownNotBlank(t *testing.T) {
	boxes := fixtureBoxes()
	boxes[2].addrFail = true
	_, inv := load(t, boxes)
	if rec := recordFor(t, inv, "alpha"); rec.ipv4 != ipv4Unreadable || rec.bridge != "lxcbr0" {
		t.Fatalf("got %+v", rec)
	}
	// ...and a probe that worked and found nothing is BLANK: the two are different answers.
	if rec := recordFor(t, inv, "mike"); rec.ipv4 != "" {
		t.Fatalf("got %+v", rec)
	}
}

// Bug 1: stderr was merged into the parsed output, so a `sudo: unable to resolve host` line from
// lxc-ls became a "container" that lxc-info then rejected as unreadable.
func TestLoadSudoNoiseOnStderrIsNotAContainer(t *testing.T) {
	f := scripted(fixtureBoxes())
	f.On([]string{"sudo", "-n", "lxc-ls", "-1"}, execx.Result{
		Stdout: []byte(names(fixtureBoxes())),
		Stderr: []byte("sudo: unable to resolve host box: Name or service not known\n"),
	})
	e := newEngine(f)
	inv, err := e.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inv.Len() != 5 || said(t, e) != "" {
		t.Fatalf("inventory %v, said %q", inv.Names(), said(t, e))
	}
}

func TestLoadBlankLinesAndPaddingInLxcLsAreIgnored(t *testing.T) {
	f := scripted(fixtureBoxes())
	f.On([]string{"sudo", "-n", "lxc-ls", "-1"}, execx.Result{Stdout: []byte("\n  zulu  \n\n\talpha\n")})
	e := newEngine(f)
	inv, err := e.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "names", inv.Names(), []string{"zulu", "alpha"})
}

// An empty answer to a query that could not be asked must never read as an empty machine.
func TestLoadAFailedLxcLsIsFatalAndSaysWhatItSaid(t *testing.T) {
	f := scripted(fixtureBoxes())
	f.On([]string{"sudo", "-n", "lxc-ls", "-1"}, exit1("lxc-ls: cannot open /var/lib/lxc\n"))
	_, err := newEngine(f).Load(context.Background())
	if msg := dieText(t, err); msg != "lxc-ls failed, so the container list is unknown: lxc-ls: cannot open /var/lib/lxc" {
		t.Fatalf("got %q", msg)
	}
}

func TestLoadANoContainersMachineIsAnEmptyInventory(t *testing.T) {
	e, inv := load(t, nil)
	if inv.Len() != 0 || said(t, e) != "" {
		t.Fatalf("inventory %v, said %q", inv.Names(), said(t, e))
	}
}

// ---- root (bug 6) ----

func TestLoadReadiesSudoFirstAndThenUsesDashN(t *testing.T) {
	f := scripted(fixtureBoxes())
	e := newEngine(f)
	if _, err := e.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if got := calls[0].Line(); got != "sudo -n -v" {
		t.Fatalf("first call %q", got)
	}
	for _, c := range calls {
		if c.Name != "sudo" || c.Args[0] != "-n" {
			t.Fatalf("every call is sudo -n: %v", c.Line())
		}
	}
	// 1 ready + lxc-ls + (info, config, address) per container.
	if len(calls) != 1+1+3*5 {
		t.Fatalf("%d calls", len(calls))
	}
}

func TestLoadASudoRefusedIsNotAnEmptyMachine(t *testing.T) {
	f := scripted(fixtureBoxes())
	f.On([]string{"sudo", "-n", "-v"}, exit1("sudo: a password is required\n"))
	e := newEngine(f)
	_, err := e.Load(context.Background())
	want := "sudo was refused, so no LXC state could be read.\n" +
		"  Nothing was changed. This is NOT 'no containers found' — the question was\n" +
		"  never asked, and reporting an empty machine here would be a guess."
	if msg := dieText(t, err); msg != want {
		t.Fatalf("got %q", msg)
	}
	if f.Called("sudo -n lxc-ls") {
		t.Fatal("nothing is listed without root")
	}
}

// A credential that expires between the probe and a command is fatal, not "unreadable": the
// question was never asked.
func TestLoadAPasswordWantedMidwayIsFatalNotUnreadable(t *testing.T) {
	f := scripted(fixtureBoxes())
	f.On([]string{"sudo", "-n", "lxc-info", "-n", "mike", "-s"}, exit1("sudo: a password is required\n"))
	_, err := newEngine(f).Load(context.Background())
	mustContain(t, "says sudo wants a password", dieText(t, err), "password")
}

func TestLoadAnLXCToolThatCannotBeStartedIsFatal(t *testing.T) {
	f := scripted(fixtureBoxes())
	f.OnError([]string{"sudo", "-n", "lxc-info", "-n", "zulu", "-s"}, &execx.NotInstalledError{Name: "sudo"})
	_, err := newEngine(f).Load(context.Background())
	mustContain(t, "names the command", dieText(t, err), "lxc-info")
}

// ---- the list footer ----

func TestListFooterOnlyWhenNothingRunsAndNothingWasUnreadable(t *testing.T) {
	e, inv := load(t, nil)
	want := "\n  LXC is installed and has no running or frozen containers.\n" +
		"  (A STOPPED container is not counted — see 'sudo lxc-ls -f'.)\n"
	if got := e.ListFooter(inv); got != want {
		t.Fatalf("got %q", got)
	}
	// An unreadable container means the machine is not known to be empty.
	boxes := []box{{name: "gone", info: stateLine("RUNNING"), infoFail: true}}
	e, inv = load(t, boxes)
	if got := e.ListFooter(inv); got != "" {
		t.Fatalf("an unreadable list is not an empty machine: %q", got)
	}
	e, inv = fixture(t)
	if got := e.ListFooter(inv); got != "" {
		t.Fatalf("a machine with containers has no footer: %q", got)
	}
}

// ---- Engine satisfies what the library asks of it ----

var _ freeze.Engine = (*Engine)(nil)
