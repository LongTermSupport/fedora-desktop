package businstall

import (
	"testing"
)

func TestTheFirewalldRecordIsAPrivateFileInRootsInstallState(t *testing.T) {
	h := newHarness(t)
	h.mkdir("var/lib/agent-bus-install/alpha")
	rec, err := h.app.fwRecord("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists, readErr := rec.Read(); readErr != nil || exists {
		t.Fatalf("a record that is not there: exists %v, %v", exists, readErr)
	}
	if writeErr := rec.Write([]byte("public\trule\n")); writeErr != nil {
		t.Fatal(writeErr)
	}
	if h.read("var/lib/agent-bus-install/alpha/firewalld.rules") != "public\trule\n" || h.mode("var/lib/agent-bus-install/alpha/firewalld.rules") != 0o600 {
		t.Fatal("the record is not a 0600 file with the rules")
	}
	if h.stdout() != "" {
		t.Fatalf("writing the record reported a change: %q", h.stdout())
	}
	data, exists, err := rec.Read()
	if err != nil || !exists || string(data) != "public\trule\n" {
		t.Fatalf("%q, %v, %v", data, exists, err)
	}
	if err := rec.Delete(); err != nil || h.exists("var/lib/agent-bus-install/alpha/firewalld.rules") {
		t.Fatalf("delete: %v", err)
	}
	if err := rec.Delete(); err != nil {
		t.Fatalf("deleting what is not there: %v", err)
	}
}

func TestTheRecordOfATeamWithNoInstallStateIsAbsent(t *testing.T) {
	h := newHarness(t)
	rec, err := h.app.fwRecord("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists, err := rec.Read(); err != nil || exists {
		t.Fatalf("exists %v, %v", exists, err)
	}
	if err := rec.Delete(); err != nil {
		t.Fatal(err)
	}
	if err := rec.Write([]byte("x")); err == nil {
		t.Fatal("a record with nowhere to live was written")
	}
}

func TestTheRecordRefusesASymlinkInPlaceOfTheFile(t *testing.T) {
	h := newHarness(t)
	h.mkdir("var/lib/agent-bus-install/alpha")
	rec, err := h.app.fwRecord("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := symlink("/etc/passwd", h.rel("var/lib/agent-bus-install/alpha/firewalld.rules")); err != nil {
		t.Fatal(err)
	}
	if err := rec.Write([]byte("x")); err == nil {
		t.Fatal("wrote through a symlink")
	}
	if _, _, err := rec.Read(); err == nil {
		t.Fatal("read a symlink out of the root")
	}
}
