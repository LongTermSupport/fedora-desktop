package nvidiastatus

import (
	"context"
	"io/fs"
	"regexp"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

type sbState int

const (
	sbUnknown sbState = iota
	sbEnabled
	sbDisabled
)

type mokState int

const (
	mokNotEnrolled mokState = iota
	mokEnrolled
	mokUncertain
	mokNotNeeded
)

// tri is a fact that may not be known: a package installed, a module built.
type tri int

const (
	no tri = iota
	yes
	unknown
)

// pkg is one package of section 2 (E10).
type pkg struct {
	name, label string
	essential   bool // missing is ❌, not ⚠
	state       tri
	version     string
	o           outcome
}

var packages = []pkg{
	{name: "akmod-nvidia", label: "NVIDIA driver (akmod)", essential: true},
	{name: "xorg-x11-drv-nvidia-cuda", label: "CUDA/NVENC/NVDEC support", essential: true},
	{name: "libva-nvidia-driver", label: "VA-API video acceleration", essential: true},
	{name: "vulkan-loader", label: "Vulkan runtime"},
	{name: "vulkan-tools", label: "Vulkan tools"},
}

// driverModules are the modules section 3 looks for; the driver is loaded when the first is.
var driverModules = []string{"nvidia", "nvidia_drm", "nvidia_modeset", "nvidia_uvm"}

type vkDevice struct{ name, api string }

// facts is everything the report says and the verdict judges, each program asked once.
type facts struct {
	sb                          sbState
	sbProbe                     outcome
	keyFile                     bool
	testKey, listEnrolled       outcome
	enrolled                    int // SHA1 fingerprints listed
	mok                         mokState
	found                       mokState // mok as section 1 found it, before E13
	mokProven                   bool     // E13 turned an uncertain key into an enrolled one
	pkgs                        []pkg
	built                       tri
	builtVersion                string
	modinfo                     outcome
	loaded                      []string
	modulesErr                  error
	nvidiaLoaded                bool
	smiPresent                  bool
	smi                         outcome
	gpus                        [][3]string
	glxPresent                  bool
	glx                         outcome
	vendor, renderer, glVersion string
	vkPresent                   bool
	vk                          outcome
	devices                     []vkDevice
	vaPresent                   bool
	va                          outcome
	journal                     outcome
	journalLines                []string
}

func (f *facts) pkg(name string) pkg {
	for _, p := range f.pkgs {
		if p.name == name {
			return p
		}
	}
	panic("nvidiastatus: no package " + name)
}

func (f *facts) driverOK() bool { return f.smiPresent && f.smi.ok() }

func (f *facts) glNvidia() bool { return f.glx.ok() && containsFold(f.vendor, "nvidia") }

// hybrid is E17: the default OpenGL renderer is an integrated GPU while nvidia-smi works.
func (f *facts) hybrid() bool {
	return f.driverOK() && f.glx.ok() && integratedGPU.MatchString(f.renderer)
}

var (
	integratedGPU = regexp.MustCompile(`(?i)intel|radeon|amd`)
	nvidiaLine    = regexp.MustCompile(`(?i)nvidia|nvrm`)
)

func (f *facts) vulkanOK() bool {
	if !f.vkPresent || !f.vk.ok() {
		return false
	}
	for _, d := range f.devices {
		if containsFold(d.name, "nvidia") {
			return true
		}
	}
	return false
}

// journalRead is E24: the kernel journal was read in full, so no line found means none.
func (f *facts) journalRead() bool {
	return f.journal.ok() && strings.TrimSpace(f.journal.errOut) == ""
}

// runFunc runs one reading command through probe.
type runFunc func(execx.Cmd) outcome

const mokutil = "mokutil"

// gather asks every program once, in the report's order (D4).
func gather(ctx context.Context, env Env) *facts {
	f := &facts{}
	run := func(c execx.Cmd) outcome { return probe(ctx, env.Runner, c) }
	present := func(name string) bool {
		_, err := env.LookPath(name)
		return err == nil
	}
	_, statErr := env.Stat(akmodsKey)
	f.keyFile = statErr == nil
	f.secureBoot(run)
	f.packages(run)
	f.modules(run, env.Proc)
	f.gpu(run, present)
	f.kernelJournal(run)
	return f
}

// secureBoot is E7-E9.
func (f *facts) secureBoot(run runFunc) {
	f.sbProbe = run(execx.Cmd{Name: mokutil, Args: []string{"--sb-state"}})
	switch said := f.sbProbe.both(); {
	case strings.Contains(said, "SecureBoot enabled"):
		f.sb = sbEnabled
	case strings.Contains(said, "SecureBoot disabled"):
		f.sb = sbDisabled
	}
	switch {
	case f.keyFile:
		f.testKey = run(execx.Cmd{Name: mokutil, Args: []string{"--test-key", akmodsKey}})
		switch said := f.testKey.both(); {
		case strings.Contains(said, "is already enrolled"):
			f.mok = mokEnrolled
		case strings.Contains(said, "is not enrolled"):
			f.mok = mokNotEnrolled
		default:
			f.mok = mokUncertain
		}
	case f.sb == sbEnabled:
		f.listEnrolled = run(execx.Cmd{Name: mokutil, Args: []string{"--list-enrolled"}})
		for _, l := range lines(f.listEnrolled.out) {
			if strings.HasPrefix(l, "SHA1 Fingerprint:") {
				f.enrolled++
			}
		}
		f.mok = mokNotEnrolled
		if !f.listEnrolled.ok() || f.enrolled > 0 {
			f.mok = mokUncertain
		}
	case f.sb == sbDisabled:
		f.mok = mokNotNeeded
	default:
		f.mok = mokUncertain
	}
	f.found = f.mok
}

// packages is E10.
func (f *facts) packages(run runFunc) {
	for _, p := range packages {
		p.o = run(execx.Cmd{Name: "rpm", Args: []string{"-q", "--queryformat", `%{VERSION}\n`, p.name}})
		switch {
		case p.o.ok():
			p.state, p.version = yes, strings.Join(lines(p.o.out), ", ")
		case p.o.problem == "" && p.o.code == 1 && strings.Contains(p.o.both(), "is not installed"):
			p.state = no
		default:
			p.state = unknown
		}
		f.pkgs = append(f.pkgs, p)
	}
}

// modules is E11-E13.
func (f *facts) modules(run runFunc, proc fs.FS) {
	f.modinfo = run(execx.Cmd{Name: "modinfo", Args: []string{"-F", "version", "nvidia"}})
	switch {
	case f.modinfo.ok():
		f.built, f.builtVersion = yes, firstLine(f.modinfo.out)
		if f.builtVersion == "" {
			f.builtVersion = "unknown"
		}
	case f.modinfo.problem == "" && containsFold(f.modinfo.both(), "not found"):
		f.built = no
	default:
		f.built = unknown
	}
	f.readModules(proc)
	if f.sb == sbEnabled && f.nvidiaLoaded && (f.mok == mokUncertain || f.mok == mokNotEnrolled) {
		f.mok, f.mokProven = mokEnrolled, true
	}
}

// gpu is E14-E22: nvidia-smi, glxinfo, vulkaninfo and vainfo, each only when on PATH.
func (f *facts) gpu(run runFunc, present func(string) bool) {
	if f.smiPresent = present("nvidia-smi"); f.smiPresent {
		f.smi = run(execx.Cmd{Name: "nvidia-smi", Args: []string{
			"--query-gpu=name,driver_version,memory.total", "--format=csv,noheader",
		}})
		for _, l := range lines(f.smi.out) {
			var gpu [3]string
			for i, field := range strings.SplitN(l, ",", 3) {
				gpu[i] = strings.TrimSpace(field)
			}
			f.gpus = append(f.gpus, gpu)
		}
	}

	if f.glxPresent = present("glxinfo"); f.glxPresent {
		f.glx = run(execx.Cmd{Name: "glxinfo", Args: []string{"-B"}})
		f.vendor = glxString(f.glx.out, "OpenGL vendor string:")
		f.renderer = glxString(f.glx.out, "OpenGL renderer string:")
		f.glVersion = glxString(f.glx.out, "OpenGL version string:")
	}

	if f.vkPresent = present("vulkaninfo"); f.vkPresent {
		f.vk = run(execx.Cmd{Name: "vulkaninfo", Args: []string{"--summary"}})
		f.devices = vulkanDevices(f.vk.out)
	}

	if f.vaPresent = present("vainfo"); f.vaPresent {
		f.va = run(execx.Cmd{Name: "vainfo"})
	}
}

// glxString is E16: the text after a glxinfo label, colons inside kept.
func glxString(out, label string) string {
	for _, l := range lines(out) {
		if v, ok := strings.CutPrefix(l, label); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// kernelJournal is E23: the NVIDIA lines of this boot's kernel errors, the last 5.
func (f *facts) kernelJournal(run runFunc) {
	f.journal = run(execx.Cmd{Name: "journalctl", Args: []string{"-k", "-b", "-p", "err", "--no-pager"}})
	for _, l := range lines(f.journal.out) {
		if strings.HasPrefix(l, "-- ") && strings.HasSuffix(l, " --") {
			continue
		}
		if nvidiaLine.MatchString(l) {
			f.journalLines = append(f.journalLines, l)
		}
	}
	if n := len(f.journalLines); n > 5 {
		f.journalLines = f.journalLines[n-5:]
	}
}

// readModules is E12: /proc/modules, matched by the exact module name.
func (f *facts) readModules(proc fs.FS) {
	data, err := fs.ReadFile(proc, "modules")
	if err != nil {
		f.modulesErr = err
		return
	}
	names := map[string]bool{}
	for _, l := range lines(string(data)) {
		names[strings.Fields(l)[0]] = true
	}
	for _, m := range driverModules {
		if names[m] {
			f.loaded = append(f.loaded, m)
		}
	}
	f.nvidiaLoaded = names[driverModules[0]]
}

// vulkanDevices is E20: each GPUn: block's deviceName and apiVersion.
func vulkanDevices(summary string) []vkDevice {
	var devices []vkDevice
	for _, l := range lines(summary) {
		if strings.HasPrefix(l, "GPU") && strings.HasSuffix(l, ":") {
			devices = append(devices, vkDevice{})
			continue
		}
		key, value, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key != "apiVersion" && key != "deviceName" {
			continue
		}
		if len(devices) == 0 {
			devices = append(devices, vkDevice{})
		}
		d := &devices[len(devices)-1]
		if key == "apiVersion" {
			d.api = value
		} else {
			d.name = value
		}
	}
	var named []vkDevice
	for _, d := range devices {
		if d.name != "" {
			named = append(named, d)
		}
	}
	return named
}
