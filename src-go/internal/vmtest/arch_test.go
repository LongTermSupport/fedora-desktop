package vmtest

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The lab's guests are the host's architecture: the probe reads that architecture's trees and
// artefacts, the CHECKSUM fetched is that architecture's, and the kernel's console is that
// architecture's serial device (the LUKS prompt and the console log are read from it).
func TestGuestArchFollowsTheHost(t *testing.T) {
	for _, tc := range []struct{ goarch, fedora, console string }{
		{"amd64", "x86_64", "ttyS0"},
		{"arm64", "aarch64", "ttyAMA0"},
	} {
		t.Run(tc.goarch, func(t *testing.T) {
			k := buildKit(t)
			k.goarch = tc.goarch
			k.fake.On(helperArgv("probe_upstream"), execx.Result{Stdout: []byte(k.serveMedia())})
			k.wantCode(k.run("build-base", "server-full"), term.ExitOK)
			if probe := k.mustCall("probe_upstream").Line(); !strings.Contains(probe, "--fedora-version 44 --arch "+tc.fedora+" --manifest ") {
				t.Fatalf("probe %s", probe)
			}
			if render := k.mustCall("render_kickstart").Line(); !strings.Contains(render, "--set SERIAL_CONSOLE="+tc.console+" ") {
				t.Fatalf("render %s", render)
			}
			if install := k.mustCall("--location").Line(); !strings.Contains(install, " console="+tc.console+",115200n8 ") {
				t.Fatalf("install %s", install)
			}
			if k.count("-44-1.1-"+tc.fedora+"-CHECKSUM") == 0 {
				t.Fatalf("no %s CHECKSUM was fetched", tc.fedora)
			}
		})
	}
}

// An architecture the lab has no guest for fails naming it, before anything is probed or run.
func TestGuestArchUnknownFails(t *testing.T) {
	k := buildKit(t)
	k.goarch = "riscv64"
	k.wantCode(k.run("build-base", "server-full"), term.ExitFailure)
	k.wantErr("ERROR: the VM lab has no guest for this host's architecture: riscv64 (it has x86_64 and aarch64)")
	for _, never := range []string{"probe_upstream", "curl", "virt-install", "qemu-img"} {
		if k.count(never) != 0 {
			t.Fatalf("%s ran on an unknown architecture", never)
		}
	}
}
