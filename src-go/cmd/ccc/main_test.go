package main

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccylimits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestZoneName(t *testing.T) {
	for link, want := range map[string]string{
		"/usr/share/zoneinfo/Europe/London": "Europe/London",
		"../usr/share/zoneinfo/UTC":         "UTC",
		"/somewhere/else":                   "",
		"":                                  "",
	} {
		if got := zoneName(link, nil); got != want {
			t.Fatalf("%q: got %q", link, got)
		}
	}
}

func TestZoneNameOfAMissingLink(t *testing.T) {
	if got := zoneName("/usr/share/zoneinfo/UTC", os.ErrNotExist); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestIdentityOf(t *testing.T) {
	got, err := identityOf(&user.User{Username: "u", Name: "A B,,,"}, nil)
	if err != nil || got.Login != "u" || got.FullName != "A B" {
		t.Fatalf("got %+v %v", got, err)
	}
	if _, err := identityOf(nil, os.ErrNotExist); err == nil {
		t.Fatal("want an error")
	}
}

func TestTerminalFd(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cerr := devNull.Close(); cerr != nil {
			t.Error(cerr)
		}
	})
	if got := terminalFd(devNull); got != -1 {
		t.Fatalf("/dev/null: %d", got)
	}
	ptmx, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cerr := ptmx.Close(); cerr != nil {
			t.Error(cerr)
		}
	})
	if got := terminalFd(ptmx); got != int(ptmx.Fd()) {
		t.Fatalf("a terminal: %d", got)
	}
}

func TestFullName(t *testing.T) {
	for gecos, want := range map[string]string{"Pat Example,,,": "Pat Example", "": "", "Pat": "Pat"} {
		if got := fullName(&user.User{Name: gecos}); got != want {
			t.Fatalf("%q: got %q", gecos, got)
		}
	}
}

func TestNoHomeFails(t *testing.T) {
	t.Setenv("HOME", "")
	if code := run(context.Background(), nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
}

func TestHelpRunsWithoutTheBuildVersion(t *testing.T) {
	if code := run(context.Background(), []string{"--help"}); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
}

// ccc's containers get ccy's limit set, settled from the machine as ccy settles it; a refusal
// or an unreadable meminfo is an error, never a container run without the caps.
func TestLimitsSettleFromTheMachine(t *testing.T) {
	dir := t.TempDir()
	meminfo := filepath.Join(dir, "meminfo")
	config := filepath.Join(dir, "config")
	env := func(name string) string {
		if name == "XDG_CONFIG_HOME" {
			return config
		}
		return ""
	}
	if _, err := limits(env, meminfo); err == nil || !strings.Contains(err.Error(), meminfo) {
		t.Fatalf("no meminfo: %v", err)
	}
	if err := os.WriteFile(meminfo, []byte("MemTotal:       67108864 kB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := limits(env, meminfo); err == nil || !strings.Contains(err.Error(), ccylimits.Play) {
		t.Fatalf("no slice unit: %v", err)
	}
	unit := ccylimits.SliceUnit(env)
	if err := os.MkdirAll(filepath.Dir(unit), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unit, []byte("[Slice]\nMemoryMax=49152M\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := limits(env, meminfo)
	if err != nil || got != (ccylimits.Limits{Pids: "16384", MemoryMiB: 49152}) {
		t.Fatalf("limits = %+v, %v", got, err)
	}
}
