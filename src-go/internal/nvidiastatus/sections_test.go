package nvidiastatus

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E7, B7: Secure Boot disabled and unknown, never one read as the other.
func TestSecureBootStates(t *testing.T) {
	w := healthy()
	w.sb = said("SecureBoot disabled\n")
	w.keyFile = false
	code, out := w.report(t)
	wantCode(t, code, term.ExitOK)
	wantLines(t, out,
		"  ⚠  Secure Boot: Disabled\n",
		"  ℹ  akmods MOK Key: Not needed (Secure Boot disabled)\n",
		"NVIDIA drivers are working (Secure Boot disabled).",
	)
	wantNot(t, out, "Secure Boot signature", "Loaded and signed")

	w = healthy()
	w.sb = reply{err: "EFI variables are not supported on this system\n", code: 1}
	w.keyFile = false
	code, out = w.report(t)
	wantCode(t, code, term.ExitOK)
	wantLines(t, out,
		"  ⚠  Secure Boot: Could not determine (mokutil exit 1)\n",
		"  ℹ  Output: EFI variables are not supported on this system\n",
		"  ℹ  This is NOT the same as 'disabled' — the state is unknown\n",
		"  ⚠  akmods MOK Key: Cannot say — Secure Boot state is unknown\n",
		"NVIDIA drivers are working (Secure Boot state unknown).",
	)
	wantNot(t, out, "Secure Boot disabled")

	w = healthy()
	w.sb = reply{fail: errStart}
	_, out = w.report(t)
	wantLines(t, out, "Could not determine (mokutil did not run: "+errStart.Error()+")", "Output: (none)")
}

// E8, E9, E13, B6: the MOK key's states, and the verdicts each leads to.
func TestMokKey(t *testing.T) {
	notLoaded := func(w *world) *world { w.modules = ""; return w }

	w := notLoaded(healthy())
	w.testKey = said("/etc/pki/akmods/certs/public_key.der is not enrolled\n")
	code, out := w.report(t)
	wantCode(t, code, term.ExitFailure)
	wantLines(t, out, "  ❌ akmods MOK Key: Not enrolled (requires reboot and enrollment)\n",
		"⚠  ACTION REQUIRED: MOK ENROLLMENT", "mok_password")

	// B14: a key that is not enrolled matters only under Secure Boot.
	w = healthy()
	w.sb = said("SecureBoot disabled\n")
	w.testKey = said("/etc/pki/akmods/certs/public_key.der is not enrolled\n")
	_, out = w.report(t)
	wantLines(t, out, "  ℹ  akmods MOK Key: Not needed (Secure Boot disabled)\n")
	wantNot(t, out, "Not enrolled (requires reboot and enrollment)")

	w = healthy()
	w.sb = reply{code: 1, err: "EFI variables are not supported on this system\n"}
	w.testKey = said("/etc/pki/akmods/certs/public_key.der is not enrolled\n")
	_, out = w.report(t)
	wantLines(t, out, "  ⚠  akmods MOK Key: Not enrolled, which matters only if Secure Boot is on\n")
	wantNot(t, out, "Not enrolled (requires reboot and enrollment)")

	w = notLoaded(healthy())
	w.testKey = said("something new\n")
	_, out = w.report(t)
	wantLines(t, out, "  ⚠  akmods MOK Key: Unknown status (something new)\n", "⚠  REBOOT REQUIRED")
	wantNot(t, out, "MOK ENROLLMENT")

	w = healthy()
	w.keyFile = false
	w.listEnrolled = said("[key 1]\nSHA1 Fingerprint: aa\n[key 2]\nSHA1 Fingerprint: bb\n")
	_, out = w.report(t)
	wantLines(t, out, "  ⚠  akmods key file not found, but 2 MOK key(s) enrolled\n",
		"  ℹ  MOK verification: Enrollment confirmed by loaded signed modules\n")

	w = notLoaded(healthy())
	w.keyFile = false
	w.listEnrolled = reply{code: 1, err: "boom\n"}
	_, out = w.report(t)
	wantLines(t, out, "  ⚠  akmods MOK Key: Could not check — mokutil failed (mokutil exited 1: boom)\n",
		"This is NOT the same as 'no keys enrolled'", "⚠  REBOOT REQUIRED")

	w = notLoaded(healthy())
	w.keyFile = false
	w.listEnrolled = said("")
	_, out = w.report(t)
	wantLines(t, out, "  ❌ akmods MOK Key: Not found and no MOK keys enrolled\n", "MOK ENROLLMENT")
}

// E10, B1: the packages, the VA-API one under its current name.
func TestPackages(t *testing.T) {
	w := healthy()
	delete(w.rpm, "libva-nvidia-driver")
	delete(w.rpm, "vulkan-tools")
	w.rpm["xorg-x11-drv-nvidia-cuda"] = said("580.95.05\n575.64.05\n")
	code, out := w.report(t)
	wantCode(t, code, term.ExitOK)
	wantLines(t, out,
		"  ❌ VA-API video acceleration: Not installed\n",
		"  ⚠  Vulkan tools: Not installed\n",
		"  ✅ CUDA/NVENC/NVDEC support: 580.95.05, 575.64.05\n",
		"  ❌ libva-nvidia-driver: Not installed\n",
		"Run: ansible-playbook playbooks/imports/optional/hardware-specific/play-nvidia.yml",
		"⚠  Missing optional package: libva-nvidia-driver",
	)

	w = healthy()
	w.rpm["akmod-nvidia"] = reply{code: 2, err: "error: rpmdb open failed\n"}
	_, out = w.report(t)
	wantLines(t, out, "  ⚠  NVIDIA driver (akmod): could not query rpm (rpm exited 2: error: rpmdb open failed)\n")
}

// E11, E12, B5, B8: the module built and loaded, by exact name.
func TestModules(t *testing.T) {
	w := healthy()
	w.modules = "nvidia_drm 139264 3 - Live 0x0\nnvidiafb 1 0 - Live 0x0\n"
	code, out := w.report(t)
	wantCode(t, code, term.ExitFailure)
	wantLines(t, out, "  ❌ Loaded modules: nvidia_drm (nvidia not loaded; requires reboot)\n", "⚠  REBOOT REQUIRED")
	wantNot(t, out, "Secure Boot signature")

	w = healthy()
	w.modules = "i915 1 0 - Live 0x0\n"
	_, out = w.report(t)
	wantLines(t, out, "  ❌ Loaded modules: None loaded (requires reboot)\n")

	w = healthy()
	w.noModules = true
	_, out = w.report(t)
	wantLines(t, out, "  ⚠  Loaded modules: could not read /proc/modules", "❌ INSTALLATION INCOMPLETE")

	w = healthy()
	w.modules = ""
	w.modinfo = reply{code: 1, err: "modinfo: ERROR: Module nvidia not found.\n"}
	_, out = w.report(t)
	wantLines(t, out, "  ❌ nvidia module: Not found (module not built yet)\n", "⚠  KERNEL MODULE STILL BUILDING")

	w = healthy()
	w.modules = ""
	w.modinfo = reply{fail: errStart}
	_, out = w.report(t)
	wantLines(t, out, "  ⚠  nvidia module: could not check (modinfo did not run: "+errStart.Error()+")\n",
		"❌ INSTALLATION INCOMPLETE")

	w = healthy()
	w.modules = ""
	delete(w.rpm, "akmod-nvidia")
	w.modinfo = reply{code: 1, err: "modinfo: ERROR: Module nvidia not found.\n"}
	_, out = w.report(t)
	wantLines(t, out, "⚠  NVIDIA DRIVER NOT INSTALLED", "play-nvidia.yml")
	wantNot(t, out, "STILL BUILDING")

	w = healthy()
	w.modinfo = said("\n")
	_, out = w.report(t)
	wantLines(t, out, "  ✅ nvidia module built: Version unknown\n")
}

// E14: nvidia-smi missing or failing.
func TestDriverFunctionality(t *testing.T) {
	w := healthy()
	w.missing["nvidia-smi"] = true
	code, out := w.report(t)
	wantCode(t, code, term.ExitFailure)
	wantLines(t, out, "  ❌ nvidia-smi: Command not found\n", "❌ INSTALLATION INCOMPLETE")

	w = healthy()
	w.smi = reply{code: 9, out: "NVIDIA-SMI has failed because it couldn't communicate with the NVIDIA driver.\n"}
	_, out = w.report(t)
	wantLines(t, out, "  ❌ nvidia-smi: Failed to communicate with GPU\n",
		"      nvidia-smi exited 9: NVIDIA-SMI has failed because it couldn't communicate with the NVIDIA driver.\n")
	wantNot(t, out, "Hybrid graphics detected")
}

// E15-E18: OpenGL.
func TestOpenGL(t *testing.T) {
	w := healthy()
	w.missing["glxinfo"] = true
	_, out := w.report(t)
	wantLines(t, out, "  ⚠  glxinfo: Not available (mesa-demos, installed by play-nvidia.yml)\n")

	w = healthy()
	w.glx = reply{code: 1, err: "Error: unable to open display\n"}
	code, out := w.report(t)
	wantCode(t, code, term.ExitOK)
	wantLines(t, out, "  ⚠  OpenGL: Could not query — glxinfo failed (glxinfo exited 1: Error: unable to open display)\n",
		"Usually no display/GL context (headless or SSH session), not a driver fault")
	wantNot(t, out, "expected: NVIDIA Corporation", "OpenGL: Using")

	w = healthy()
	w.glx = said("OpenGL vendor string: NVIDIA Corporation\nOpenGL renderer string: NVIDIA RTX 500/PCIe/SSE2\n" +
		"OpenGL version string: 4.6.0 NVIDIA 580.95.05\n")
	_, out = w.report(t)
	wantLines(t, out, "  ✅ OpenGL vendor: NVIDIA Corporation\n", "      Renderer: NVIDIA RTX 500/PCIe/SSE2\n",
		"      Version: 4.6.0 NVIDIA 580.95.05\n", "OpenGL: Using NVIDIA renderer")
	wantNot(t, out, "Hybrid Graphics System Detected")

	w = healthy()
	w.glx = said("OpenGL vendor string: Mesa\nOpenGL renderer string: llvmpipe (LLVM 19.1.7, 256 bits): x\n")
	_, out = w.report(t)
	wantLines(t, out, "  ❌ OpenGL vendor: Mesa (expected: NVIDIA Corporation)\n",
		"  ℹ  Using: llvmpipe (LLVM 19.1.7, 256 bits): x\n")
}

// E19, E20: Vulkan.
func TestVulkan(t *testing.T) {
	w := healthy()
	w.missing["vulkaninfo"] = true
	_, out := w.report(t)
	wantLines(t, out, "  ❌ vulkaninfo: Not available (install vulkan-tools, by play-nvidia.yml)\n")

	w = healthy()
	w.missing["vulkaninfo"] = true
	delete(w.rpm, "vulkan-loader")
	_, out = w.report(t)
	wantLines(t, out, "  ⚠  vulkaninfo: Not installed (Vulkan support not configured)\n")

	w = healthy()
	w.vk = reply{code: 1, err: "ERROR: [Loader Message] Code 0 : no ICD\nmore\n"}
	_, out = w.report(t)
	wantLines(t, out, "  ❌ Vulkan runtime: Failed to initialize (ERROR: [Loader Message] Code 0 : no ICD)\n")

	w = healthy()
	w.vk = said("Devices:\n========\nGPU0:\n\tapiVersion = 1.3.0\n\tdeviceName = llvmpipe (LLVM 19.1.7, 256 bits)\n")
	code, out := w.report(t)
	wantCode(t, code, term.ExitOK)
	wantLines(t, out, "  ❌ Vulkan GPU: llvmpipe (LLVM 19.1.7, 256 bits) (expected: NVIDIA device)\n")
	wantNot(t, out, "Vulkan: Working")

	w = healthy()
	w.vk = said("Devices:\n========\n")
	_, out = w.report(t)
	wantLines(t, out, "  ❌ Vulkan GPU: none (expected: NVIDIA device)\n")
}

// E21, E22, B9: VA-API.
func TestVAAPI(t *testing.T) {
	w := healthy()
	w.missing["vainfo"] = true
	_, out := w.report(t)
	wantLines(t, out, "  ⚠  vainfo: Not available (libva-utils, installed by play-nvidia.yml)\n")

	w = healthy()
	w.va = reply{code: 255, out: "libva info: VA-API version 1.22.0\nvaInitialize failed with error code -1 (unknown libva error),exit\n"}
	_, out = w.report(t)
	wantLines(t, out, "  ⚠  VA-API (default driver): Could not initialise (vaInitialize failed with error code -1 (unknown libva error),exit)\n")
	wantNot(t, out, "no profiles")

	w = healthy()
	w.va = said("vainfo: Driver version: X\nvainfo: Supported profile and entrypoints\n")
	_, out = w.report(t)
	wantLines(t, out, "  ⚠  VA-API (default driver): Driver loaded but no profiles\n")
}

// E23, E24, B2: the kernel journal, filtered here, never an unread one called clean.
func TestKernelJournal(t *testing.T) {
	w := healthy()
	w.journal = said("-- Boot 1a2b --\n" +
		"Oct 28 09:00:01 host kernel: usb 3-1: device descriptor read/64, error -71\n" +
		"Oct 28 09:00:02 host kernel: NVRM: GPU 0000:01:00.0: RmInitAdapter failed!\n" +
		"Oct 28 09:00:03 host kernel: nvidia 0000:01:00.0: one\n" +
		"Oct 28 09:00:04 host kernel: nvidia-modeset: two\n" +
		"Oct 28 09:00:05 host kernel: NVIDIA three\n" +
		"Oct 28 09:00:06 host kernel: nvidia four\n" +
		"Oct 28 09:00:07 host kernel: nvidia five\n")
	code, out := w.report(t)
	wantCode(t, code, term.ExitOK)
	wantLines(t, out, "  ⚠  Recent kernel errors found:\n",
		"      Oct 28 09:00:03 host kernel: nvidia 0000:01:00.0: one\n",
		"      Oct 28 09:00:07 host kernel: nvidia five\n")
	wantNot(t, out, "RmInitAdapter", "usb 3-1", "-- Boot", "None found")

	w = healthy()
	w.journal = said("-- No entries --\n")
	_, out = w.report(t)
	wantLines(t, out, "  ✅ Recent errors: None found\n")

	w = healthy()
	w.journal = reply{out: "-- No entries --\n", err: "Hint: You are currently not seeing messages from other users and the system.\n"}
	_, out = w.report(t)
	wantLines(t, out,
		"  ⚠  Recent errors: Could not read the kernel journal in full (Hint: You are currently not seeing messages from other users and the system.)\n",
		"  ℹ  Not the same as 'no errors' — try: sudo journalctl -k -b -p err --no-pager\n")
	wantNot(t, out, "None found")

	w = healthy()
	w.journal = reply{code: 1, err: "No journal files were found.\n"}
	_, out = w.report(t)
	wantLines(t, out, "Could not read the kernel journal in full (journalctl exited 1: No journal files were found.)")
}
