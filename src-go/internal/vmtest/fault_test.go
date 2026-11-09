package vmtest

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Fault injection over whole runs (E28-E47, "fail-closed at every step"): each command a
// passing run makes is made to fail in turn. Whatever fails, the run ends with a status it
// owns (pass, or exit 1 with a reason), never a panic; and once the guest is booted, the
// teardown still asks libvirt to remove it.

// faultRunner is the kit's fake with its at'th command exiting 1.
type faultRunner struct {
	inner execx.Runner
	at    int
	mu    sync.Mutex
	n     int
}

func (f *faultRunner) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	f.mu.Lock()
	f.n++
	hit := f.n == f.at
	f.mu.Unlock()
	if hit {
		return execx.Result{ExitCode: 1}, &execx.ExitError{Name: c.Name, Code: 1, Stderr: "injected fault"}
	}
	return f.inner.Run(ctx, c)
}

func (k *kit) runner() execx.Runner {
	if k.faultAt > 0 {
		return &faultRunner{inner: k.fake, at: k.faultAt}
	}
	return k.fake
}

// faultScenarios are passing runs, each built fresh by its function.
var faultScenarios = map[string]func(t *testing.T) (*kit, []string){
	"server": func(t *testing.T) (*kit, []string) {
		t.Helper()
		return runKit(t, "pass"), []string{"run", "server-fast-provision"}
	},
	"server-reboot": func(t *testing.T) (*kit, []string) {
		t.Helper()
		k := runKit(t, "pass")
		k.scenario("server-fast-provision", map[string]string{"reboot_before_checks": "true"})
		k.write("guest-prepare-server-fast-provision.bash", "fixture\n")
		boots := 0
		k.gw.remote = func(remote string) (execx.Result, bool) {
			switch {
			case strings.Contains(remote, "guest-prepare.bash"):
				return execx.Result{Stdout: []byte(prepareDoneMarker + "\n")}, true
			case remote == bootIDCommand:
				boots++
				return execx.Result{Stdout: []byte(map[bool]string{true: "first\n", false: "second\n"}[boots == 1])}, true
			}
			return execx.Result{}, false
		}
		return k, []string{"run", "server-fast-provision"}
	},
	"server-host-only": func(t *testing.T) (*kit, []string) {
		t.Helper()
		k := runKit(t, "pass")
		k.scenario(hostOnlyID, map[string]string{"host_only": "true"})
		k.write(hostOnlyFile, hostOnlyID+"\n")
		k.write(scrubLibrary, "lib\n")
		k.write(scrubFront, "front\n")
		dir := t.TempDir()
		token, pp := filepath.Join(dir, "token"), filepath.Join(dir, "pp")
		k.writeAbs(token, plantedToken+"\n")
		k.writeAbs(pp, passphraseValue+"\n\n")
		k.fake.On([]string{"bash"}, execx.Result{})
		return k, []string{"run", hostOnlyID, "--github-account", "throwaway-lab-user", "--github-token-file", token, "--github-ssh-passphrase-file", pp}
	},
	"desktop-developer": func(t *testing.T) (*kit, []string) {
		t.Helper()
		k := runKit(t, "pass")
		k.scenario("desktop-fresh-install", map[string]string{"base": "desktop", "base_name": "desktop-44", "profile": "desktop", "reboot_before_checks": "true"})
		k.write(allowlistFile, "desktop-fresh-install\n")
		k.write("guest-acceptance-desktop.bash", "checker\n")
		k.builtBase("desktop-44")
		k.write("bases/desktop-44/luks-passphrase", "pp\n")
		k.gw.remote = func(remote string) (execx.Result, bool) {
			switch {
			case strings.HasPrefix(remote, "loginctl"):
				return seat0(remote), true
			case remote == "cat /var/lib/vmtest/run.exit":
				return execx.Result{Stdout: []byte("0\n")}, true
			}
			return execx.Result{}, false
		}
		return k, []string{"run", "desktop-fresh-install"}
	},
	"desktop-consumer": func(t *testing.T) (*kit, []string) {
		t.Helper()
		k := runKit(t, "pass")
		k.scenario("desktop-consumer-install", map[string]string{"base": "desktop", "base_name": "desktop-44", "profile": "desktop", "audience": "consumer", "reboot_before_checks": "true"})
		k.write(allowlistFile, "desktop-consumer-install\n")
		k.write("guest-acceptance-desktop-consumer-install.bash", "checker\n")
		k.builtBase("desktop-44")
		k.write("bases/desktop-44/luks-passphrase", "pp\n")
		k.gw.remote = func(remote string) (execx.Result, bool) {
			if strings.HasPrefix(remote, "loginctl") {
				return seat0(remote), true
			}
			return execx.Result{}, false
		}
		return k, []string{"run", "desktop-consumer-install"}
	},
}

func TestFaultInjectionOverWholeRuns(t *testing.T) {
	for name, build := range faultScenarios {
		t.Run(name, func(t *testing.T) {
			k, args := build(t)
			k.wantCode(k.run(args...), term.ExitOK)
			total := len(k.fake.Calls())
			if total < 10 {
				t.Fatalf("a passing run made only %d calls", total)
			}
			for at := 1; at <= total; at++ {
				runWithFault(t, build, at)
			}
		})
	}
}

// runWithFault runs the scenario with its at'th command failing and judges how it ended.
func runWithFault(t *testing.T, build func(*testing.T) (*kit, []string), at int) {
	t.Helper()
	k, args := build(t)
	k.faultAt = at
	code := k.run(args...)
	if code != term.ExitOK && code != term.ExitFailure {
		t.Errorf("fault at call %d: exit %d", at, code)
	}
	if code == term.ExitFailure && !strings.Contains(k.err.String(), "ERROR: ") && !strings.Contains(k.out.String(), "verdict=") {
		t.Errorf("fault at call %d: exit 1 with no reason:\n%s", at, k.err.String())
	}
	calls := k.fake.Calls()
	booted := callIndex(calls, "--name vmtest-run-")
	if booted >= 0 && booted < at-1 && callIndex(calls[booted:], "undefine --nvram vmtest-run-") < 0 {
		t.Errorf("fault at call %d: the booted guest was never removed", at)
	}
}

// callIndex is the first call whose line contains sub, or -1.
func callIndex(calls []execx.Cmd, sub string) int {
	for i, c := range calls {
		if strings.Contains(c.Line(), sub) {
			return i
		}
	}
	return -1
}
