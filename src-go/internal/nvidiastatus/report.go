package nvidiastatus

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// verdict is the summary's finding (E25); only complete exits 0.
type verdict int

const (
	complete verdict = iota
	mokRequired
	driverMissing
	building
	rebootRequired
	incomplete
)

// judge is E25: the first verdict that holds.
func judge(f *facts) verdict {
	switch {
	case f.nvidiaLoaded && f.driverOK():
		return complete
	case f.sb == sbEnabled && f.mok == mokNotEnrolled:
		return mokRequired
	case f.pkg("akmod-nvidia").state == no:
		return driverMissing
	case f.built == no:
		return building
	case f.built == yes && !f.nvidiaLoaded && f.modulesErr == nil:
		return rebootRequired
	default:
		return incomplete
	}
}

const (
	green  = "\x1b[0;32m"
	red    = "\x1b[0;31m"
	yellow = "\x1b[1;33m"
	blue   = "\x1b[0;34m"
	banner = "========================================================================"
	play   = "playbooks/imports/optional/hardware-specific/play-nvidia.yml"
)

// report writes the report on stdout (E5); a write that fails is kept in p for finish.
type report struct {
	w      io.Writer
	p      term.Printer
	colour bool
}

func (r *report) line(s string) { r.p.Print(r.w, s+"\n") }

func (r *report) paint(code, s string) string {
	if !r.colour {
		return s
	}
	return code + s + term.Reset
}

func (r *report) good(s string)   { r.line("  " + r.paint(green, "✅") + " " + s) }
func (r *report) bad(s string)    { r.line("  " + r.paint(red, "❌") + " " + s) }
func (r *report) warn(s string)   { r.line("  " + r.paint(yellow, "⚠") + "  " + s) }
func (r *report) info(s string)   { r.line("  " + r.paint(yellow, "ℹ") + "  " + s) }
func (r *report) detail(s string) { r.line("      " + s) }

func (r *report) section(title string) {
	r.line(r.paint(blue+term.Bold, title))
	r.line(r.paint(blue, strings.Repeat("─", utf8.RuneCountInString(title))))
}

func (r *report) heading(title string) {
	r.line(r.paint(term.Bold, banner))
	r.line(r.paint(term.Bold, strings.Repeat(" ", (len(banner)-len(title))/2)+title))
	r.line(r.paint(term.Bold, banner))
}

func (r *report) render(f *facts, v verdict) {
	r.heading("NVIDIA DRIVER INSTALLATION STATUS CHECK")
	r.line("")
	for _, s := range []func(*facts){
		r.secureBoot, r.packages, r.modules, r.driver, r.openGL, r.vulkan, r.video, r.journal,
	} {
		s(f)
		r.line("")
	}
	r.heading("SUMMARY")
	r.summary(f, v)
	r.line(r.paint(term.Bold, banner))
	r.line("")
}

// secureBoot is E7-E9.
func (r *report) secureBoot(f *facts) {
	r.section("1. SECURE BOOT & MOK STATUS")
	switch f.sb {
	case sbEnabled:
		r.good("Secure Boot: Enabled")
	case sbDisabled:
		r.warn("Secure Boot: Disabled")
	case sbUnknown:
		status := f.sbProbe.problem
		if status == "" {
			status = fmt.Sprintf("mokutil exit %d", f.sbProbe.code)
		}
		said := oneLine(f.sbProbe.both())
		if said == "" {
			said = "(none)"
		}
		r.warn("Secure Boot: Could not determine (" + status + ")")
		r.info("Output: " + said)
		r.info("This is NOT the same as 'disabled' — the state is unknown")
	}
	r.mokKey(f)
}

// mokKey is E8 and E9: what section 1 found, before E13 may prove the key.
func (r *report) mokKey(f *facts) {
	switch {
	case f.keyFile && f.found == mokEnrolled:
		r.good("akmods MOK Key: Successfully enrolled (" + akmodsKey + ")")
	case f.keyFile && f.found == mokNotEnrolled:
		r.bad("akmods MOK Key: Not enrolled (requires reboot and enrollment)")
	case f.keyFile && f.testKey.problem != "":
		r.warn("akmods MOK Key: Unknown status (" + f.testKey.problem + ")")
	case f.keyFile:
		r.warn("akmods MOK Key: Unknown status (" + oneLine(f.testKey.both()) + ")")
	case f.sb == sbEnabled && !f.listEnrolled.ok():
		r.warn("akmods MOK Key: Could not check — mokutil failed (" + f.listEnrolled.why() + ")")
		r.info("This is NOT the same as 'no keys enrolled' — the query did not run")
	case f.sb == sbEnabled && f.enrolled > 0:
		r.warn(fmt.Sprintf("akmods key file not found, but %d MOK key(s) enrolled", f.enrolled))
		r.info("Keys may be from a previous Fedora version or a manual enrollment")
	case f.sb == sbEnabled:
		r.bad("akmods MOK Key: Not found and no MOK keys enrolled")
	case f.sb == sbDisabled:
		r.info("akmods MOK Key: Not needed (Secure Boot disabled)")
	default:
		r.warn("akmods MOK Key: Cannot say — Secure Boot state is unknown")
	}
}

// packages is E10.
func (r *report) packages(f *facts) {
	r.section("2. PACKAGE INSTALLATION")
	for _, p := range f.pkgs {
		switch {
		case p.state == yes:
			r.good(p.label + ": " + p.version)
		case p.state == unknown:
			r.warn(p.label + ": could not query rpm (" + p.o.why() + ")")
		case p.essential:
			r.bad(p.label + ": Not installed")
		default:
			r.warn(p.label + ": Not installed")
		}
	}
}

// modules is E11-E13.
func (r *report) modules(f *facts) {
	r.section("3. KERNEL MODULE STATUS")
	switch f.built {
	case yes:
		r.good("nvidia module built: Version " + f.builtVersion)
	case no:
		r.bad("nvidia module: Not found (module not built yet)")
	case unknown:
		r.warn("nvidia module: could not check (" + f.modinfo.why() + ")")
	}
	switch {
	case f.modulesErr != nil:
		r.warn("Loaded modules: could not read /proc/modules: " + f.modulesErr.Error())
	case f.nvidiaLoaded:
		r.good("Loaded modules: " + strings.Join(f.loaded, " "))
		var missing []string
		for _, m := range driverModules {
			if !strings.Contains(" "+strings.Join(f.loaded, " ")+" ", " "+m+" ") {
				missing = append(missing, m)
			}
		}
		if len(missing) > 0 {
			r.info("Not loaded: " + strings.Join(missing, " "))
		}
	case len(f.loaded) > 0:
		r.bad("Loaded modules: " + strings.Join(f.loaded, " ") + " (nvidia not loaded; requires reboot)")
	default:
		r.bad("Loaded modules: None loaded (requires reboot)")
	}
	if f.sb == sbEnabled && f.nvidiaLoaded {
		r.good("Secure Boot signature: Valid (modules working with Secure Boot)")
	}
	if f.mokProven {
		r.info("MOK verification: Enrollment confirmed by loaded signed modules")
	}
}

// driver is E14.
func (r *report) driver(f *facts) {
	r.section("4. DRIVER FUNCTIONALITY")
	switch {
	case !f.smiPresent:
		r.bad("nvidia-smi: Command not found")
	case f.smi.ok():
		r.good("nvidia-smi: Working")
		for _, gpu := range f.gpus {
			r.detail("GPU: " + gpu[0])
			r.detail("Driver: " + gpu[1])
			r.detail("VRAM: " + gpu[2])
		}
	default:
		r.bad("nvidia-smi: Failed to communicate with GPU")
		r.detail(f.smi.why())
	}
}

// openGL is E15-E18.
func (r *report) openGL(f *facts) {
	r.section("5. OPENGL STATUS")
	switch {
	case !f.glxPresent:
		r.warn("glxinfo: Not available (mesa-demos, installed by play-nvidia.yml)")
	case !f.glx.ok():
		r.warn("OpenGL: Could not query — glxinfo failed (" + f.glx.why() + ")")
		r.info("Usually no display/GL context (headless or SSH session), not a driver fault")
	case f.glNvidia():
		r.good("OpenGL vendor: " + f.vendor)
		r.detail("Renderer: " + f.renderer)
		r.detail("Version: " + f.glVersion)
	case f.hybrid():
		r.warn("OpenGL default GPU: " + f.vendor + " (integrated graphics)")
		r.detail("Default renderer: " + f.renderer)
		r.info("Hybrid graphics detected - NVIDIA available but not default")
		r.detail(r.paint(yellow, "To use NVIDIA GPU:"))
		r.detail("__NV_PRIME_RENDER_OFFLOAD=1 __GLX_VENDOR_LIBRARY_NAME=nvidia <command>")
		r.detail("Or configure NVIDIA as primary GPU in system settings")
	default:
		r.bad("OpenGL vendor: " + f.vendor + " (expected: NVIDIA Corporation)")
		r.info("Using: " + f.renderer)
	}
}

// vulkan is E19 and E20.
func (r *report) vulkan(f *facts) {
	r.section("6. VULKAN STATUS")
	switch {
	case !f.vkPresent && f.pkg("vulkan-loader").state == yes:
		r.bad("vulkaninfo: Not available (install vulkan-tools, by play-nvidia.yml)")
	case !f.vkPresent:
		r.warn("vulkaninfo: Not installed (Vulkan support not configured)")
	case !f.vk.ok():
		why := firstLine(f.vk.errOut)
		if why == "" || f.vk.problem != "" {
			why = f.vk.why()
		}
		r.bad("Vulkan runtime: Failed to initialize (" + why + ")")
	case f.vulkanOK():
		r.good("Vulkan runtime: Working")
		for _, d := range f.devices {
			if d.api == "" {
				r.detail("GPU: " + d.name)
				continue
			}
			r.detail("GPU: " + d.name + " (API " + d.api + ")")
		}
	default:
		names := make([]string, 0, len(f.devices))
		for _, d := range f.devices {
			names = append(names, d.name)
		}
		found := strings.Join(names, ", ")
		if found == "" {
			found = "none"
		}
		r.bad("Vulkan GPU: " + found + " (expected: NVIDIA device)")
	}
}

// video is E21 and E22.
func (r *report) video(f *facts) {
	r.section("7. HARDWARE VIDEO ACCELERATION")
	switch f.pkg("libva-nvidia-driver").state {
	case yes:
		r.good("libva-nvidia-driver: Installed")
	case no:
		r.bad("libva-nvidia-driver: Not installed")
		r.info("Installed by play-nvidia.yml; run it again")
		r.detail("Run: ansible-playbook " + play)
	case unknown:
		r.warn("libva-nvidia-driver: could not query rpm (section 2)")
	}
	const label = "VA-API (default driver): "
	switch {
	case !f.vaPresent:
		r.warn("vainfo: Not available (libva-utils, installed by play-nvidia.yml)")
	case !f.va.ok():
		why := lastLine(f.va.both())
		if why == "" || f.va.problem != "" {
			why = f.va.why()
		}
		r.warn(label + "Could not initialise (" + why + ")")
	case strings.Contains(f.va.both(), "VAProfile"):
		r.good(label + "Driver loaded")
		for _, l := range lines(f.va.both()) {
			if _, version, ok := strings.Cut(l, "Driver version:"); ok && strings.TrimSpace(version) != "" {
				r.detail("Driver: " + strings.TrimSpace(version))
				break
			}
		}
	default:
		r.warn(label + "Driver loaded but no profiles")
	}
}

// journal is E23 and E24.
func (r *report) journal(f *facts) {
	r.section("8. RECENT DRIVER LOGS (THIS BOOT)")
	if !f.journalRead() {
		why := firstLine(f.journal.errOut)
		if !f.journal.ok() {
			why = f.journal.why()
		}
		r.warn("Recent errors: Could not read the kernel journal in full (" + why + ")")
		r.info("Not the same as 'no errors' — try: sudo journalctl -k -b -p err --no-pager")
	}
	if len(f.journalLines) > 0 {
		r.warn("Recent kernel errors found:")
		for _, l := range f.journalLines {
			r.detail(l)
		}
		return
	}
	if f.journalRead() {
		r.good("Recent errors: None found")
	}
}

func (r *report) say(s string) { r.line("   " + s) }

// complete is E25.1's words.
func (r *report) complete(f *facts) {
	r.line(r.paint(green+term.Bold, "✅ INSTALLATION COMPLETE AND FUNCTIONAL"))
	switch f.sb {
	case sbEnabled:
		r.say("NVIDIA drivers are properly installed with Secure Boot enabled.")
	case sbDisabled:
		r.say("NVIDIA drivers are working (Secure Boot disabled).")
	case sbUnknown:
		r.say("NVIDIA drivers are working (Secure Boot state unknown).")
	}
	r.say("GPU: Detected and working")
	if f.sb == sbEnabled {
		r.say("Modules: Loaded and signed")
	}
	if f.vulkanOK() {
		r.say("Vulkan: Working")
	}
	if f.glNvidia() {
		r.say("OpenGL: Using NVIDIA renderer")
	} else if f.hybrid() {
		r.say("OpenGL: Using integrated GPU (hybrid graphics - normal)")
	}
	if f.hybrid() {
		r.line("")
		r.say(r.paint(yellow, "ℹ  Hybrid Graphics System Detected"))
		r.say("Your laptop has both integrated and NVIDIA GPUs.")
		r.say("System uses integrated GPU by default for power savings.")
		r.say("Use PRIME render offload to run apps on NVIDIA GPU.")
	}
	if f.pkg("libva-nvidia-driver").state == no {
		r.line("")
		r.say(r.paint(yellow, "⚠  Missing optional package: libva-nvidia-driver"))
		r.say("Re-run playbook to install: play-nvidia.yml")
	}
}

// summary is E25's words.
func (r *report) summary(f *facts, v verdict) {
	say := r.say
	switch v {
	case complete:
		r.complete(f)
	case mokRequired:
		r.line(r.paint(yellow+term.Bold, "⚠  ACTION REQUIRED: MOK ENROLLMENT"))
		say("1. Reboot your system")
		say("2. Enroll the MOK key when prompted at the blue screen")
		say("3. Use the password from your vault configuration (mok_password)")
		say("4. System will reboot and NVIDIA drivers will work")
	case driverMissing:
		r.line(r.paint(yellow+term.Bold, "⚠  NVIDIA DRIVER NOT INSTALLED"))
		say("akmod-nvidia is not installed. Install it with:")
		say("./" + play)
	case building:
		r.line(r.paint(yellow+term.Bold, "⚠  KERNEL MODULE STILL BUILDING"))
		say("The akmod-nvidia module is still being compiled.")
		say("This can take 5-10 minutes after installation.")
		say("Check status with: modinfo -F version nvidia")
		say("When complete, reboot to load the driver.")
	case rebootRequired:
		r.line(r.paint(yellow+term.Bold, "⚠  REBOOT REQUIRED"))
		say("NVIDIA kernel modules are built but not loaded.")
		say("Reboot to activate the NVIDIA drivers.")
	case incomplete:
		r.line(r.paint(red+term.Bold, "❌ INSTALLATION INCOMPLETE"))
		say("Some components are not properly configured.")
		say("Please review the errors above.")
	}
}
