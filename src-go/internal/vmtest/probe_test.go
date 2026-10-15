package vmtest

import (
	"os"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E13: a single-valued marker occurs exactly once (B10).
func TestE13SingleValuedMarkers(t *testing.T) {
	p := probe{lines: []string{"VMTEST-FRESHNESS-REVISION 12", "VMTEST-FRESHNESS-COMPOSE-ID a", "VMTEST-FRESHNESS-COMPOSE-ID b"}}
	if v, err := p.single("VMTEST-FRESHNESS-REVISION"); err != nil || v != "12" {
		t.Fatalf("got %q %v", v, err)
	}
	if _, err := p.single("VMTEST-FRESHNESS-COMPOSE-ID"); err == nil {
		t.Fatal("two compose ids accepted")
	}
	if _, err := p.single("VMTEST-FRESHNESS-TREE-URL"); err == nil || !strings.Contains(err.Error(), "probe printed no VMTEST-FRESHNESS-TREE-URL line") {
		t.Fatalf("got %v", err)
	}
}

func TestProbeArtefactsAndPrefixes(t *testing.T) {
	p := probe{lines: []string{
		"VMTEST-FRESHNESS-ARTEFACT desktop-44 Fedora-Everything-netinst-x86_64-44.iso " + strings.Repeat("c", 64) + " label=44-1.1 variant=Everything url=https://h/a/Fedora-Everything-netinst-x86_64-44.iso",
		"VMTEST-FRESHNESS-ARTEFACT desktop-44 Fedora-Workstation-Live-44.iso " + strings.Repeat("d", 64) + " label=44-1.1 variant=Workstation url=https://h/b/Fedora-Workstation-Live-44.iso",
		"VMTEST-FRESHNESS-ARTEFACT other-44 x.iso " + strings.Repeat("e", 64) + " label=l variant=v url=https://h/x.iso",
		"VMTEST-FRESHNESS-TREE-URL everything https://tree/os",
	}}
	arts, err := p.artefacts("desktop-44")
	if err != nil || len(arts) != 2 || arts[1].Variant != "Workstation" || arts[0].URL != "https://h/a/Fedora-Everything-netinst-x86_64-44.iso" {
		t.Fatalf("got %+v %v", arts, err)
	}
	if f, err := p.artefactWithPrefix("desktop-44", "Fedora-Workstation-Live-"); err != nil || f != "Fedora-Workstation-Live-44.iso" {
		t.Fatalf("got %q %v", f, err)
	}
	if _, err := p.artefactWithPrefix("desktop-44", "Fedora-Server-"); err == nil {
		t.Fatal("a missing prefix was accepted")
	}
	if u, err := p.treeURL("everything"); err != nil || u != "https://tree/os" {
		t.Fatalf("got %q %v", u, err)
	}
	bad := probe{lines: []string{"VMTEST-FRESHNESS-ARTEFACT b f sha label=l variant=v"}}
	if _, err := bad.artefacts("b"); err == nil {
		t.Fatal("a malformed artefact line was accepted")
	}
}

// E12: an unreadable probe copies its UNREADABLE lines to stderr and fails; the file is removed.
func TestE12ProbeFailureAndCleanup(t *testing.T) {
	k := newKit(t)
	k.builtBase("server-fast-44")
	k.fake.On(helperArgv("probe_upstream"), execx.Result{
		Stdout:   []byte("VMTEST-FRESHNESS-UNREADABLE revision https://r boom\nVMTEST-FRESHNESS-DONE unreadable=1\n"),
		ExitCode: 1,
	})
	k.wantCode(k.run("ensure-base", "server-fast"), term.ExitFailure)
	k.wantErr("VMTEST-FRESHNESS-UNREADABLE revision https://r boom\n")
	k.wantErr("ERROR: upstream freshness signal unavailable — cannot prove what the published media is")
	assertNoProbeFiles(t, k)

	k = newKit(t)
	k.builtBase("server-fast-44")
	k.gate(0, "current")
	k.wantCode(k.run("ensure-base", "server-fast"), term.ExitOK)
	assertNoProbeFiles(t, k)
}

func assertNoProbeFiles(t *testing.T, k *kit) {
	t.Helper()
	env := k.environment()
	entries, err := os.ReadDir(env.TempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("left behind: %v", entries)
	}
}
