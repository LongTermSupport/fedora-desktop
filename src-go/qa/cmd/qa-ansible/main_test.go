package main

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/ansible"
)

func TestRunDispatchesThroughTheCommand(t *testing.T) {
	if code := run([]string{"--help"}); code != int(ansible.ExitOK) {
		t.Fatalf("--help: exit %d", code)
	}
	if code := run([]string{"--nope"}); code != int(ansible.ExitCannotRun) {
		t.Fatalf("--nope: exit %d", code)
	}
	// A root with no Ansible directories is a run that could not complete.
	if code := run([]string{"--root", t.TempDir()}); code != int(ansible.ExitCannotRun) {
		t.Fatalf("empty root: exit %d", code)
	}
}
