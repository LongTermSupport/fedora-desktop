//go:build busfailpoints

package failpoint

import (
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// childEnv marks a re-run of this test binary as the process a point kills.
const childEnv = "TEAMCHAT_FAILPOINT_TEST_CHILD"

func TestParseArmsTheNamedPoints(t *testing.T) {
	armed, err := parse(string(KeeperBeforeEnd) + "," + string(LinkInRemove))
	check(t, err)
	if !armed[KeeperBeforeEnd] || !armed[LinkInRemove] || len(armed) != 2 {
		t.Fatalf("armed %v", armed)
	}
	none, err := parse("")
	check(t, err)
	if len(none) != 0 {
		t.Fatalf("an empty list armed %v", none)
	}
}

// A misspelt point would never fire, so the test that armed it would pass without its crash.
func TestParseRefusesAnUnknownOrEmptyName(t *testing.T) {
	for _, list := range []string{"no-such-point", string(LinkInRemove) + ",", "," + string(LinkInRemove)} {
		if _, err := parse(list); !errors.Is(err, errUnknown) {
			t.Errorf("%q: got %v", list, err)
		}
	}
}

func TestEveryDeclaredPointIsKnown(t *testing.T) {
	for _, n := range declared(t) {
		if !known[Name(n)] {
			t.Errorf("%s is declared but not in known", n)
		}
	}
	if len(known) != len(declared(t)) {
		t.Errorf("known has %d points, names.go declares %d", len(known), len(declared(t)))
	}
}

func TestFireSignalsOnlyAnArmedPoint(t *testing.T) {
	var sent []syscall.Signal
	kill := func(pid int, sig syscall.Signal) error {
		if pid != os.Getpid() {
			t.Errorf("signalled pid %d, not its own", pid)
		}
		sent = append(sent, sig)
		return nil
	}
	points := map[Name]bool{LinkInRemove: true}
	if fire(KeeperBeforeEnd, points, nil, kill) || len(sent) != 0 {
		t.Fatal("an unarmed point fired")
	}
	if !fire(LinkInRemove, points, nil, kill) || len(sent) != 1 || sent[0] != syscall.SIGKILL {
		t.Fatalf("sent %v", sent)
	}
	// Not armed in this process's environment: Hit returns.
	Hit(LinkInRemove)
}

func TestFirePanicsOnABadListOrAFailedKill(t *testing.T) {
	for name, call := range map[string]func(){
		"bad list": func() { fire(LinkInRemove, nil, errUnknown, nil) },
		"kill failed": func() {
			fire(LinkInRemove, map[Name]bool{LinkInRemove: true}, nil, func(int, syscall.Signal) error { return syscall.EPERM })
		},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("no panic")
				}
			}()
			call()
		})
	}
}

// TestHitKillsItsOwnProcess re-runs this test binary, which hits an armed point and must die
// of SIGKILL before it prints its marker; a point not armed lets it live.
func TestHitKillsItsOwnProcess(t *testing.T) {
	if os.Getenv(childEnv) != "" {
		Hit(LinkInRemove)
		check(t, say("survived"))
		Hit(KeeperBeforeEnd)
		check(t, say("not reached"))
		return
	}
	res, err := runSelf(t, string(KeeperBeforeEnd))
	var exit *execx.ExitError
	if !errors.As(err, &exit) || exit.Signal != syscall.SIGKILL {
		t.Fatalf("got %v, want death by SIGKILL", err)
	}
	if out := string(res.Stdout); !strings.Contains(out, "survived") || strings.Contains(out, "not reached") {
		t.Fatalf("stdout %q", out)
	}
}

func TestAnUnknownArmedNamePanicsAtTheFirstHit(t *testing.T) {
	if os.Getenv(childEnv) != "" {
		Hit(LinkInRemove)
		return
	}
	_, err := runSelf(t, "no-such-point")
	var exit *execx.ExitError
	if !errors.As(err, &exit) || exit.Code != 2 {
		t.Fatalf("got %v, want the panic's exit 2", err)
	}
}

func say(line string) error {
	_, err := os.Stdout.WriteString(line + "\n")
	return err
}

// runSelf is the test binary re-run for the calling test alone, with points armed.
func runSelf(t *testing.T, armed string) (execx.Result, error) {
	t.Helper()
	return execx.OS{}.Run(t.Context(), execx.Cmd{
		Name: os.Args[0],
		Args: []string{"-test.run=^" + t.Name() + "$"},
		Env:  []string{childEnv + "=1", envName + "=" + armed},
	})
}
