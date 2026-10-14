package businstall

import (
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const readyUnit = "agent-bus-hs@alpha.service"

func TestWaitReadyReturnsAtOnceWhenTheHomeserverAnswers(t *testing.T) {
	h := newHarness(t)
	h.sim.unit(readyUnit).active = true
	if err := h.app.waitReady(readyUnit, 8448); err != nil {
		t.Fatal(err)
	}
	contains(t, "stderr", h.stderr(), "agent-bus-install: agent-bus-hs@alpha.service answers on 127.0.0.1:8448\n")
	if h.sleeps != 0 || h.sim.lines(`^curl `) != 1 {
		t.Fatalf("sleeps %d, curls %d", h.sleeps, h.sim.lines(`^curl `))
	}
	if h.sim.lines(`^curl --fail --silent --show-error --noproxy \* --max-time 2 http://127.0.0.1:8448/_tuwunel/server_version$`) != 1 {
		t.Fatalf("log %v", h.sim.log)
	}
}

// failFirst makes the first n runs of commands starting with prefix fail, then answers as the
// simulated host does.
func failFirst(h *harness, prefix string, n int) {
	seen := 0
	h.fake.OnFunc(nil, func(c execx.Cmd) (execx.Result, error) {
		if strings.HasPrefix(c.Line(), prefix) && seen < n {
			seen++
			h.sim.log = append(h.sim.log, c.Line())
			return refuse(7, "curl: (7) Failed to connect")
		}
		return h.sim.answer(c)
	})
}

func TestWaitReadyPollsOneSecondApartWhileTheUnitIsActive(t *testing.T) {
	h := newHarness(t)
	h.sim.unit(readyUnit).active = true
	failFirst(h, "curl ", 3)
	if err := h.app.waitReady(readyUnit, 8448); err != nil {
		t.Fatal(err)
	}
	if h.sleeps != 3 {
		t.Fatalf("slept %d times", h.sleeps)
	}
}

func TestWaitReadyFailsAsSoonAsTheUnitIsNotActiveAndPrintsItsJournal(t *testing.T) {
	h := newHarness(t)
	h.sim.unit(readyUnit).failed = true
	err := h.app.waitReady(readyUnit, 8448)
	f, ok := errors.AsType[*failure](err)
	if !ok || f.code != term.ExitFailure {
		t.Fatalf("got %v", err)
	}
	want := "readiness: agent-bus-hs@alpha.service is failed (auto-restart), not active: it did not start (an unknown or " +
		"malformed tuwunel.toml key is one cause). See: systemctl status --no-pager agent-bus-hs@alpha.service; " +
		"journalctl -u agent-bus-hs@alpha.service --no-pager"
	if f.msg != want {
		t.Fatalf("message %q", f.msg)
	}
	contains(t, "stderr", h.stderr(), "agent-bus-install: the last 40 journal lines of agent-bus-hs@alpha.service:\n")
	contains(t, "stderr", h.stderr(), "stub journal: tuwunel exited")
	if h.sim.lines(`^journalctl --unit agent-bus-hs@alpha.service --lines 40 --no-pager --output short-iso$`) != 1 {
		t.Fatalf("log %v", h.sim.log)
	}
	if h.sleeps != 0 {
		t.Fatal("it slept for a unit that is not running")
	}
}

func TestWaitReadyGivesUpAfterSixtyTriesWithTheCurlOutput(t *testing.T) {
	h := newHarness(t)
	h.sim.unit(readyUnit).active = true
	failFirst(h, "curl ", 1000)
	err := h.app.waitReady(readyUnit, 8448)
	f, ok := errors.AsType[*failure](err)
	if !ok || f.code != term.ExitFailure {
		t.Fatalf("got %v", err)
	}
	want := "readiness: agent-bus-hs@alpha.service is active but http://127.0.0.1:8448/_tuwunel/server_version did not answer in 60 s: curl: (7) Failed to connect"
	if !strings.HasPrefix(f.msg, want) {
		t.Fatalf("message %q", f.msg)
	}
	if h.sleeps != 60 || h.sim.lines(`^curl `) != 60 {
		t.Fatalf("slept %d, curls %d", h.sleeps, h.sim.lines(`^curl `))
	}
	contains(t, "stderr", h.stderr(), "the last 40 journal lines")
}

func TestWaitReadyNotesAJournalThatCannotBeRead(t *testing.T) {
	h := newHarness(t)
	h.sim.unit(readyUnit).failed = true
	h.sim.fails = []failRule{{prefix: "journalctl", code: 1}}
	if err := h.app.waitReady(readyUnit, 8448); err == nil {
		t.Fatal("no error")
	}
	contains(t, "stderr", h.stderr(), "journalctl could not read the journal of agent-bus-hs@alpha.service")
}

func TestWaitReadyReportsAStateItCannotRead(t *testing.T) {
	h := newHarness(t)
	h.sim.fails = []failRule{{prefix: "systemctl show", code: 1}}
	err := h.app.waitReady(readyUnit, 8448)
	if f, ok := errors.AsType[*failure](err); !ok || f.code != term.ExitFailure {
		t.Fatalf("got %v", err)
	}
	h2 := newHarness(t)
	h2.sim.unit(readyUnit).failed = true
	h2.sim.fails = []failRule{{prefix: "systemctl show -P SubState", code: 1}}
	if err := h2.app.waitReady(readyUnit, 8448); err == nil {
		t.Fatal("no error")
	}
}

func TestRequireRunningNamesTheUnitAndItsState(t *testing.T) {
	h := newHarness(t)
	if err := h.app.requireRunning("firewalld.service", "NetworkManager.service"); err != nil {
		t.Fatal(err)
	}
	h.sim.units["firewalld.service"].active = false
	f, ok := errors.AsType[*failure](h.app.requireRunning("NetworkManager.service", "firewalld.service"))
	if !ok || f.code != term.ExitFailure || f.msg != "firewalld.service is inactive; the homeserver needs it running" {
		t.Fatalf("got %v", f)
	}
	h.sim.fails = []failRule{{prefix: "systemctl show", code: 1}}
	if err := h.app.requireRunning("firewalld.service"); err == nil {
		t.Fatal("no error")
	}
}

func TestAssertListeningAcceptsExactlyLoopbackAndTheListenAddresses(t *testing.T) {
	h := newHarness(t)
	h.sim.unit(readyUnit).active = true
	if err := h.app.assertListening(8448, []string{"192.0.2.10"}); err != nil {
		t.Fatal(err)
	}
	h.sim.ssAddrs = []string{"127.0.0.1", "192.0.2.10", "198.51.100.9"}
	f, ok := errors.AsType[*failure](h.app.assertListening(8448, []string{"192.0.2.10"}))
	if !ok || f.code != term.ExitFailure ||
		f.msg != "port 8448 is bound on [127.0.0.1 192.0.2.10 198.51.100.9], not exactly [127.0.0.1 192.0.2.10]" {
		t.Fatalf("got %v", f)
	}
	h.sim.fails = []failRule{{prefix: "ss ", code: 1}}
	if err := h.app.assertListening(8448, nil); err == nil {
		t.Fatal("a failed ss returned no error")
	}
}
