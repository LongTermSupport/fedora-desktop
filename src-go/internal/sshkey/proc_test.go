package sshkey

import (
	"os"
	"testing"
)

func TestParentFromStatReadsPastTheCommandName(t *testing.T) {
	cases := map[string]int{
		"4243 (ssh-agent) S 4242 4243 4243 0 -1": 4242,
		"77 (a b) c) (d) R 1 77 77 0 -1 4194560": 1,
	}
	for line, want := range cases {
		got, err := parentFromStat(line)
		if err != nil || got != want {
			t.Errorf("%q: %d %v, want %d", line, got, err, want)
		}
	}
	for _, bad := range []string{"", "4243 (ssh-agent", "4243 (x) S", "4243 (x) S notanumber"} {
		if _, err := parentFromStat(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestProcParentIsThisProcesssParent(t *testing.T) {
	got, err := procParent(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if got != os.Getppid() {
		t.Errorf("parent %d, want %d", got, os.Getppid())
	}
	if _, err := procParent(-1); err == nil {
		t.Error("no error for a pid that cannot exist")
	}
}

func TestNewEnvIsTheProcesss(t *testing.T) {
	env := NewEnv()
	if env.Getpid() != os.Getpid() {
		t.Error("Getpid")
	}
	self, err := env.Self()
	if err != nil || self == "" {
		t.Errorf("Self %q %v", self, err)
	}
	if env.Runner == nil || env.Execer == nil || env.Foregrounder == nil || env.OpenTTY == nil {
		t.Error("an adapter is missing")
	}
	if env.Columns() < 0 {
		t.Error("Columns")
	}
}
