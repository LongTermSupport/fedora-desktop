package nvidiastatus

import (
	"context"
	"errors"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestHelpPrintsTheUsage(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		code, out, errOut := healthy().run(t, arg)
		wantCode(t, code, term.ExitOK)
		wantLines(t, out, "nvidia-status [--check]")
		if errOut != "" {
			t.Errorf("%s: stderr %q", arg, errOut)
		}
	}
}

// E2: a bad command line is refused before any command runs.
func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"--frobnicate"}, {"--check", "--check"}, {"extra"}, {"--check", "x"}} {
		w := healthy()
		code, out, errOut := w.run(t, args...)
		wantCode(t, code, term.ExitUsage)
		if out != "" {
			t.Errorf("%v: stdout %q", args, out)
		}
		if !strings.HasPrefix(errOut, "nvidia-status: ") || !strings.HasSuffix(errOut, " — see 'nvidia-status --help'.\n") {
			t.Errorf("%v: stderr %q", args, errOut)
		}
		if n := len(w.fake.Calls()); n != 0 {
			t.Errorf("%v: %d commands ran", args, n)
		}
	}
}

// The healthy hybrid laptop: every section reports and the verdict is complete (E25.1).
func TestHealthyHybridLaptop(t *testing.T) {
	code, out := healthy().report(t)
	wantCode(t, code, term.ExitOK)
	wantLines(t, out,
		"NVIDIA DRIVER INSTALLATION STATUS CHECK",
		"1. SECURE BOOT & MOK STATUS",
		"  ✅ Secure Boot: Enabled\n",
		"  ✅ akmods MOK Key: Successfully enrolled (/etc/pki/akmods/certs/public_key.der)\n",
		"2. PACKAGE INSTALLATION",
		"  ✅ NVIDIA driver (akmod): 580.95.05\n",
		"  ✅ VA-API video acceleration: 0.0.14\n",
		"3. KERNEL MODULE STATUS",
		"  ✅ nvidia module built: Version 580.95.05\n",
		"  ✅ Loaded modules: nvidia nvidia_drm nvidia_modeset nvidia_uvm\n",
		"  ✅ nvidia driver loaded: Version 580.95.05\n",
		"  ✅ Secure Boot signature: Valid (modules working with Secure Boot)\n",
		"4. DRIVER FUNCTIONALITY",
		"  ✅ nvidia-smi: Working\n",
		"      GPU: NVIDIA GeForce RTX 3050 Laptop GPU\n",
		"      Driver: 580.95.05\n",
		"      VRAM: 4094 MiB\n",
		"5. OPENGL STATUS",
		"  ⚠  OpenGL default GPU: Intel (integrated graphics)\n",
		"__NV_PRIME_RENDER_OFFLOAD=1 __GLX_VENDOR_LIBRARY_NAME=nvidia <command>",
		"6. VULKAN STATUS",
		"  ✅ Vulkan runtime: Working\n",
		"      GPU: NVIDIA GeForce RTX 3050 Laptop GPU (API 1.4.312)\n",
		"      GPU: Intel(R) Iris(R) Xe Graphics (API 1.4.311)\n",
		"7. HARDWARE VIDEO ACCELERATION",
		"  ✅ libva-nvidia-driver: Installed\n",
		"  ✅ VA-API (default driver): Driver loaded\n",
		"      Driver: Intel iHD driver for Intel(R) Gen Graphics - 25.2.6 ()\n",
		"  ✅ VA-API (NVIDIA driver): Driver loaded\n",
		"      Driver: VA-API NVDEC driver [direct backend]\n",
		"8. RECENT DRIVER LOGS (THIS BOOT)",
		"  ✅ Recent errors: None found\n",
		"SUMMARY",
		"✅ INSTALLATION COMPLETE AND FUNCTIONAL",
		"NVIDIA drivers are properly installed with Secure Boot enabled.",
		"Modules: Loaded and signed",
		"Vulkan: Working",
		"OpenGL: Using integrated GPU (hybrid graphics - normal)",
		"Hybrid Graphics System Detected",
	)
	wantNot(t, out, "\x1b[", "Missing optional package")
}

// B9: each command runs once, and the old VA-API package name is never asked (B1).
func TestEachCommandRunsOnce(t *testing.T) {
	w := healthy()
	w.report(t)
	seen := map[string]int{}
	for _, c := range w.fake.Calls() {
		seen[strings.TrimSpace(strings.Join(c.Env, " ")+" "+c.Line())]++
		if strings.Contains(c.Line(), "nvidia-vaapi-driver") {
			t.Errorf("asked for the old package name: %s", c.Line())
		}
	}
	for line, n := range seen {
		if n != 1 {
			t.Errorf("%q ran %d times", line, n)
		}
	}
	if len(seen) != 14 {
		t.Errorf("%d distinct commands, want 14: %v", len(seen), seen)
	}
}

// E4, B3: --check prints nothing and exits with the verdict.
func TestCheckPrintsNothing(t *testing.T) {
	code, out, errOut := healthy().run(t, "--check")
	wantCode(t, code, term.ExitOK)
	if out != "" || errOut != "" {
		t.Errorf("stdout %q stderr %q", out, errOut)
	}
	w := healthy()
	w.modules = ""
	code, out, errOut = w.run(t, "--check")
	wantCode(t, code, term.ExitFailure)
	if out != "" || errOut != "" {
		t.Errorf("not loaded: stdout %q stderr %q", out, errOut)
	}
}

// E5, B10: colour on a terminal only, and never with NO_COLOR.
func TestColourOnlyOnATerminal(t *testing.T) {
	w := healthy()
	w.tty = true
	_, out := w.report(t)
	wantLines(t, out, "\x1b[0;32m✅\x1b[0m")
	w = healthy()
	w.tty = true
	w.env["NO_COLOR"] = "1"
	_, out = w.report(t)
	wantNot(t, out, "\x1b[")
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// E3: a report nobody could read is a failure, said on stderr.
func TestUnwritableStdoutFails(t *testing.T) {
	code, errOut := runInto(t, brokenWriter{})
	wantCode(t, code, term.ExitFailure)
	if errOut != "nvidia-status: cannot write the report: broken pipe\n" {
		t.Errorf("stderr %q", errOut)
	}
}

// hangingRunner is execx.OS's answer to a child that does not answer before its context's
// deadline: it waits for the deadline, then reports what os/exec's Wait does for the child the
// cancel sent SIGTERM, an ExitError with the signal, never the context's error.
type hangingRunner struct{}

func (hangingRunner) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	<-ctx.Done()
	return execx.Result{ExitCode: -1}, &execx.ExitError{Name: c.Name, Code: -1, Signal: syscall.SIGTERM}
}

// E6: the production bound and the production error. In a synctest bubble the 30s pass at
// once, when the runner blocks on the deadline; the probe must name the deadline, not the kill.
func TestTimeoutIsNamedNotTheKill(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		o := probe(context.Background(), hangingRunner{}, execx.Cmd{Name: "nvidia-smi"})
		if o.problem != "nvidia-smi did not answer within 30s" {
			t.Errorf("problem %q", o.problem)
		}
	})
}

// E6, B11: a command that does not answer in time is said in its section, and the run goes on.
func TestHungCommandIsReported(t *testing.T) {
	w := healthy()
	w.smi = reply{fail: context.DeadlineExceeded}
	code, out := w.report(t)
	wantCode(t, code, term.ExitFailure)
	wantLines(t, out,
		"  ❌ nvidia-smi: Failed to communicate with GPU\n",
		"      nvidia-smi did not answer within 30s\n",
		"6. VULKAN STATUS",
		"❌ INSTALLATION INCOMPLETE",
	)
}
