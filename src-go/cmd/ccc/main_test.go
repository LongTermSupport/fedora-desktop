package main

import (
	"context"
	"os"
	"os/user"
	"testing"

	"golang.org/x/sys/unix"

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
