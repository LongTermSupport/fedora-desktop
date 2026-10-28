package nvidiastatus

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// reply is one command's answer: its streams and exit status, or a failure to run at all.
type reply struct {
	out, err string
	code     int
	fail     error
}

func said(out string) reply { return reply{out: out} }

// world is a hybrid-GPU laptop's answers to every command the report asks, healthy by default;
// a test breaks the part it is about.
type world struct {
	sb, testKey, listEnrolled reply
	keyFile                   bool
	rpm                       map[string]reply
	modinfo                   reply
	modules                   string
	noModules                 bool // /proc/modules unreadable
	missing                   map[string]bool
	smi, glx, vk, va, journal reply
	env                       map[string]string
	tty                       bool
	stdout                    *bytes.Buffer
	stderr                    *bytes.Buffer
	fake                      *execx.Fake
}

const healthyModules = "nvidia_uvm 4956160 0 - Live 0x0000000000000000\n" +
	"nvidia_drm 139264 3 - Live 0x0000000000000000\n" +
	"nvidia_modeset 1720320 2 nvidia_drm, Live 0x0000000000000000\n" +
	"nvidia 104890368 30 nvidia_uvm,nvidia_modeset, Live 0x0000000000000000 (POE)\n" +
	"i915 4636672 47 - Live 0x0000000000000000\n"

const hybridGLX = "name of display: :0\ndisplay: :0  screen: 0\ndirect rendering: Yes\n" +
	"Extended renderer info (GLX_MESA_query_renderer):\n    Vendor: Intel (0x8086)\n" +
	"OpenGL vendor string: Intel\n" +
	"OpenGL renderer string: Mesa Intel(R) Graphics (RPL-P)\n" +
	"OpenGL core profile version string: 4.6 (Core Profile) Mesa 25.1.9\n" +
	"OpenGL version string: 4.6 (Compatibility Profile) Mesa 25.1.9\n"

const hybridVulkan = "==========\nVULKANINFO\n==========\n\nVulkan Instance Version: 1.4.313\n\n" +
	"Devices:\n========\nGPU0:\n" +
	"\tapiVersion         = 1.4.312\n\tdriverVersion      = 580.95.5.0\n" +
	"\tvendorID           = 0x10de\n\tdeviceType         = PHYSICAL_DEVICE_TYPE_DISCRETE_GPU\n" +
	"\tdeviceName         = NVIDIA RTX 500 Ada Generation Laptop GPU\n" +
	"GPU1:\n\tapiVersion         = 1.4.311\n\tdriverVersion      = 25.1.9\n" +
	"\tvendorID           = 0x8086\n\tdeviceType         = PHYSICAL_DEVICE_TYPE_INTEGRATED_GPU\n" +
	"\tdeviceName         = Intel(R) Graphics (RPL-P)\n"

const intelVainfo = "Trying display: wayland\nlibva info: VA-API version 1.22.0\n" +
	"vainfo: VA-API version: 1.22 (libva 2.22.0)\n" +
	"vainfo: Driver version: Intel iHD driver for Intel(R) Gen Graphics - 25.2.6 ()\n" +
	"vainfo: Supported profile and entrypoints\n" +
	"      VAProfileNone                   :\tVAEntrypointVideoProc\n" +
	"      VAProfileH264Main               :\tVAEntrypointVLD\n"

func notInstalled(name string) reply {
	return reply{out: "package " + name + " is not installed\n", code: 1}
}

func healthy() *world {
	return &world{
		sb:      said("SecureBoot enabled\n"),
		keyFile: true,
		testKey: said("/etc/pki/akmods/certs/public_key.der is already enrolled\n"),
		rpm: map[string]reply{
			"akmod-nvidia":             said("580.95.05\n"),
			"xorg-x11-drv-nvidia-cuda": said("580.95.05\n"),
			"libva-nvidia-driver":      said("0.0.14\n"),
			"vulkan-loader":            said("1.4.313.0\n"),
			"vulkan-tools":             said("1.4.313.0\n"),
		},
		modinfo: said("580.95.05\n"),
		modules: healthyModules,
		missing: map[string]bool{},
		smi:     said("NVIDIA RTX 500 Ada Generation Laptop GPU, 580.95.05, 4094 MiB\n"),
		glx:     said(hybridGLX),
		vk:      said(hybridVulkan),
		va:      said(intelVainfo),
		journal: said(""),
		env:     map[string]string{},
	}
}

func answer(r reply) func(execx.Cmd) (execx.Result, error) {
	return func(execx.Cmd) (execx.Result, error) {
		if r.fail != nil {
			return execx.Result{}, r.fail
		}
		return execx.Result{Stdout: []byte(r.out), Stderr: []byte(r.err), ExitCode: r.code}, nil
	}
}

// run is one invocation over this world: its exit code, stdout and stderr.
func (w *world) run(t *testing.T, args ...string) (term.ExitCode, string, string) {
	t.Helper()
	w.fake = &execx.Fake{}
	w.fake.OnFunc([]string{"mokutil", "--sb-state"}, answer(w.sb))
	w.fake.OnFunc([]string{"mokutil", "--test-key", akmodsKey}, answer(w.testKey))
	w.fake.OnFunc([]string{"mokutil", "--list-enrolled"}, answer(w.listEnrolled))
	for _, name := range []string{"akmod-nvidia", "xorg-x11-drv-nvidia-cuda", "libva-nvidia-driver", "vulkan-loader", "vulkan-tools"} {
		r, ok := w.rpm[name]
		if !ok {
			r = notInstalled(name)
		}
		w.fake.OnFunc([]string{"rpm", "-q", "--queryformat", `%{VERSION}\n`, name}, answer(r))
	}
	w.fake.OnFunc([]string{"modinfo", "-F", "version", "nvidia"}, answer(w.modinfo))
	w.fake.OnFunc([]string{"nvidia-smi", "--query-gpu=name,driver_version,memory.total", "--format=csv,noheader"}, answer(w.smi))
	w.fake.OnFunc([]string{"glxinfo", "-B"}, answer(w.glx))
	w.fake.OnFunc([]string{"vulkaninfo", "--summary"}, answer(w.vk))
	w.fake.OnFunc([]string{"vainfo"}, answer(w.va))
	w.fake.OnFunc([]string{"journalctl", "-k", "-b", "-p", "err", "--no-pager"}, answer(w.journal))
	proc := fstest.MapFS{}
	if !w.noModules {
		proc["modules"] = &fstest.MapFile{Data: []byte(w.modules)}
	}
	if w.stdout == nil {
		w.stdout = &bytes.Buffer{}
	}
	w.stderr = &bytes.Buffer{}
	env := Env{
		Runner: w.fake,
		Getenv: func(k string) string { return w.env[k] },
		LookPath: func(name string) (string, error) {
			if w.missing[name] {
				return "", exec.ErrNotFound
			}
			return "/usr/bin/" + name, nil
		},
		Stat: func(path string) (fs.FileInfo, error) {
			if path == akmodsKey && w.keyFile {
				return fs.Stat(fstest.MapFS{"public_key.der": &fstest.MapFile{}}, "public_key.der")
			}
			return nil, fs.ErrNotExist
		},
		Proc:             proc,
		Stdout:           w.stdout,
		Stderr:           w.stderr,
		StdoutIsTerminal: w.tty,
	}
	code := App{Env: env}.Run(context.Background(), args)
	return code, w.stdout.String(), w.stderr.String()
}

// report runs the full report and returns its code and stdout, failing on any stderr.
func (w *world) report(t *testing.T) (term.ExitCode, string) {
	t.Helper()
	code, out, errOut := w.run(t)
	if errOut != "" {
		t.Fatalf("stderr: %q", errOut)
	}
	return code, out
}

func wantLines(t *testing.T, out string, lines ...string) {
	t.Helper()
	for _, l := range lines {
		if !strings.Contains(out, l) {
			t.Errorf("missing %q in:\n%s", l, out)
		}
	}
}

func wantNot(t *testing.T, out string, lines ...string) {
	t.Helper()
	for _, l := range lines {
		if strings.Contains(out, l) {
			t.Errorf("unexpected %q in:\n%s", l, out)
		}
	}
}

func wantCode(t *testing.T, got, want term.ExitCode) {
	t.Helper()
	if got != want {
		t.Errorf("exit %d, want %d", got, want)
	}
}

var errStart = errors.New("fork/exec: resource temporarily unavailable")
