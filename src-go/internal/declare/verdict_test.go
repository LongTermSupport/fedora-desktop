package declare

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// A fixed modification time, so a test never reads the clock: every file of the world has it
// until a case moves one.
var verdictTime = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

// verdictWorld is the set of inputs the APPLIED verdict is computed from (SPEC-view.md V40): a
// host_vars, a source checkout with the two package trees, the installer and the Tuwunel pin, an
// applied directory with one stamp, the installed teams and the bridge's addresses.
type verdictWorld struct {
	t         *testing.T
	c         *Client
	f         *execx.Fake
	src       string
	applied   string
	installed []string
	bridge    []string
	bridgeErr error
}

func newVerdictWorld(t *testing.T) *verdictWorld {
	t.Helper()
	root := t.TempDir()
	w := &verdictWorld{
		t: t, f: &execx.Fake{}, src: filepath.Join(root, "src"), applied: filepath.Join(root, "applied"),
		installed: []string{"alpha"}, bridge: []string{"192.0.2.1/24"},
	}
	w.c = &Client{Runner: w.f, Root: "/root", File: filepath.Join(root, "localhost.yml")}
	w.c.Bridge = func() ([]string, error) { return w.bridge, w.bridgeErr }
	w.write(w.c.File, "agent_bus_teams:\n  - team: alpha\n    port: 6167\n")
	w.write(filepath.Join(w.src, "helpers/pingbus/cli.py"), "print('pingbus')\n")
	w.write(filepath.Join(w.src, "helpers/agent_bus/declare.py"), "print('declare')\n")
	w.write(filepath.Join(w.src, "helpers/agent_bus_lxc/derive.py"), "print('derive')\n")
	w.write(filepath.Join(w.src, "files/usr/local/sbin/agent-bus-install"), "#!/bin/sh\n")
	w.write(filepath.Join(w.src, "files/usr/local/share/agent-bus/tuwunel.pin"), "tuwunel_version: 1.0.0\n")
	w.write(filepath.Join(w.applied, "alpha.json"), `{"team": "alpha"}`)
	script(w.f, "status", "STATUS\talpha\tcurrent\t-\n")
	return w
}

// write writes body at path with the world's fixed modification time.
func (w *verdictWorld) write(path, body string) {
	t := w.t
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, verdictTime, verdictTime); err != nil {
		t.Fatal(err)
	}
}

func (w *verdictWorld) status(t *testing.T) []Status {
	t.Helper()
	got, err := w.c.Status(context.Background(), w.src, w.applied, w.installed)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// runs is how many declare.py status processes have started.
func (w *verdictWorld) runs() int {
	n := 0
	for _, c := range w.f.Calls() {
		if len(c.Args) > 5 && c.Args[5] == "status" {
			n++
		}
	}
	return n
}

func TestAnUnchangedWorldReusesTheVerdict(t *testing.T) {
	w := newVerdictWorld(t)
	first := w.status(t)
	second := w.status(t)
	if w.runs() != 1 {
		t.Fatalf("declare status ran %d times over an unchanged world, want 1", w.runs())
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("the reused verdict differs: %+v then %+v", first, second)
	}
	second[0].State = ApplyOutdated
	if again := w.status(t); again[0].State != ApplyCurrent {
		t.Fatalf("a caller's change reached the kept verdict: %+v", again)
	}
}

func TestEachInputOfTheVerdictChangedRunsItAgain(t *testing.T) {
	cases := []struct {
		name   string
		change func(w *verdictWorld)
	}{
		{"the host_vars bytes", func(w *verdictWorld) {
			w.write(w.c.File, "agent_bus_teams:\n  - team: alpha\n    port: 6168\n")
		}},
		{"the installed teams", func(w *verdictWorld) { w.installed = []string{"alpha", "beta"} }},
		{"a stamp's bytes", func(w *verdictWorld) {
			w.write(filepath.Join(w.applied, "alpha.json"), `{"team": "alphb"}`)
		}},
		{"a stamp added", func(w *verdictWorld) {
			w.write(filepath.Join(w.applied, "beta.json"), `{"team": "beta"}`)
		}},
		{"a stamp's name", func(w *verdictWorld) {
			if err := os.Rename(filepath.Join(w.applied, "alpha.json"), filepath.Join(w.applied, "alphb.json")); err != nil {
				w.t.Fatal(err)
			}
		}},
		{"the applied directory gone", func(w *verdictWorld) {
			if err := os.RemoveAll(w.applied); err != nil {
				w.t.Fatal(err)
			}
		}},
		{"the bridge's addresses", func(w *verdictWorld) { w.bridge = []string{"198.51.100.1/24"} }},
		{"the bridge gone", func(w *verdictWorld) {
			w.bridge, w.bridgeErr = nil, errors.New("no such network interface")
		}},
		{"a pingbus file's size", func(w *verdictWorld) {
			w.write(filepath.Join(w.src, "helpers/pingbus/cli.py"), "print('pingbus', 2)\n")
		}},
		{"an agent_bus file's modification time", func(w *verdictWorld) {
			later := verdictTime.Add(time.Second)
			if err := os.Chtimes(filepath.Join(w.src, "helpers/agent_bus/declare.py"), later, later); err != nil {
				w.t.Fatal(err)
			}
		}},
		{"an agent_bus file's mode", func(w *verdictWorld) {
			if err := os.Chmod(filepath.Join(w.src, "helpers/agent_bus/declare.py"), 0o400); err != nil {
				w.t.Fatal(err)
			}
		}},
		{"a file added under agent_bus", func(w *verdictWorld) {
			w.write(filepath.Join(w.src, "helpers/agent_bus/new.py"), "")
		}},
		{"a pingbus file removed", func(w *verdictWorld) {
			if err := os.Remove(filepath.Join(w.src, "helpers/pingbus/cli.py")); err != nil {
				w.t.Fatal(err)
			}
		}},
		{"an agent_bus_lxc file's size", func(w *verdictWorld) {
			w.write(filepath.Join(w.src, "helpers/agent_bus_lxc/derive.py"), "print('derive', 2)\n")
		}},
		{"the installer", func(w *verdictWorld) {
			w.write(filepath.Join(w.src, "files/usr/local/sbin/agent-bus-install"), "#!/bin/bash\n")
		}},
		{"the Tuwunel pin", func(w *verdictWorld) {
			w.write(filepath.Join(w.src, "files/usr/local/share/agent-bus/tuwunel.pin"), "tuwunel_version: 1.0.10\n")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newVerdictWorld(t)
			w.status(t)
			tc.change(w)
			w.status(t)
			if w.runs() != 2 {
				t.Fatalf("after %s changed declare status ran %d times in all, want 2", tc.name, w.runs())
			}
			w.status(t)
			if w.runs() != 2 {
				t.Fatalf("the new verdict was not kept: %d runs", w.runs())
			}
		})
	}
}

func TestADifferentSourceOrAppliedDirectoryRunsItAgain(t *testing.T) {
	w := newVerdictWorld(t)
	w.status(t)
	if _, err := w.c.Status(context.Background(), w.src, t.TempDir(), w.installed); err != nil {
		t.Fatal(err)
	}
	if _, err := w.c.Status(context.Background(), t.TempDir(), w.applied, w.installed); err != nil {
		t.Fatal(err)
	}
	if w.runs() != 3 {
		t.Fatalf("declare status ran %d times, want 3", w.runs())
	}
}

// Python's byte-compiled caches are not inputs: the bundle leaves __pycache__ out
// (helpers/pingbus/bundle.py:82), and the status run itself may write them.
func TestAByteCompiledCacheIsNotAnInput(t *testing.T) {
	w := newVerdictWorld(t)
	w.status(t)
	w.write(filepath.Join(w.src, "helpers/agent_bus/__pycache__/declare.cpython-314.pyc"), "x")
	w.status(t)
	if w.runs() != 1 {
		t.Fatalf("a new .pyc ran declare status again: %d runs", w.runs())
	}
}

// B-S1: the source trees are judged by stat, so an edit that keeps a file's size and
// modification time is not seen.
func TestAnEditKeepingSizeAndTimeIsNotSeen(t *testing.T) {
	w := newVerdictWorld(t)
	w.status(t)
	w.write(filepath.Join(w.src, "helpers/pingbus/cli.py"), "print('PINGBUS')\n")
	w.status(t)
	if w.runs() != 1 {
		t.Fatalf("declare status ran %d times, want 1 (B-S1)", w.runs())
	}
}

func TestAFailedStatusRunIsNotKept(t *testing.T) {
	w := newVerdictWorld(t)
	w.f.On([]string{"env", "PYTHONPATH=/root", "python3", "-P", "-m", "helpers.agent_bus.declare", "status"},
		execx.Result{Stderr: []byte("declare: boom\n"), ExitCode: 78})
	if _, err := w.c.Status(context.Background(), w.src, w.applied, w.installed); !errors.Is(err, ErrRefused) {
		t.Fatalf("got %v", err)
	}
	script(w.f, "status", "STATUS\talpha\tcurrent\t-\n")
	if got := w.status(t); got[0].State != ApplyCurrent || w.runs() != 2 {
		t.Fatalf("got %+v after %d runs", got, w.runs())
	}
}

func TestAStatusAnswerThatDoesNotParseIsNotKept(t *testing.T) {
	w := newVerdictWorld(t)
	script(w.f, "status", "STATUS\talpha\tcurrent\n")
	if _, err := w.c.Status(context.Background(), w.src, w.applied, w.installed); err == nil {
		t.Fatal("accepted a short STATUS line")
	}
	script(w.f, "status", "STATUS\talpha\tcurrent\t-\n")
	w.status(t)
	if w.runs() != 2 {
		t.Fatalf("declare status ran %d times, want 2", w.runs())
	}
}

// The bridge's addresses are read in-process, IPv4 only, as `ip -4 addr show dev` lists them
// for v1 (helpers/agent_bus/bridge.py:51-60); the loopback stands in for the bridge.
func TestABridgesIPv4AddressesAreReadInProcess(t *testing.T) {
	got, err := interfaceIPv4("lo")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got, "127.0.0.1/8") || slices.Contains(got, "::1/128") {
		t.Fatalf("lo's IPv4 addresses %v", got)
	}
	if _, err := interfaceIPv4("no-such-bridge0"); err == nil {
		t.Fatal("a missing interface read as no addresses")
	}
}
