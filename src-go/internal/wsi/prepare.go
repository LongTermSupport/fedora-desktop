package wsi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The session types the clipboard tools serve, and the programs more than one file names.
const (
	sessionWayland = "wayland"
	sessionX11     = "x11"
	soxBin         = "sox"
	soxiBin        = "soxi"
	curlBin        = "curl"
	recorderBin    = "pw-record"
	xselBin        = "xsel"
	wlCopyBin      = "wl-copy"
)

const (
	transcriber = "faster-whisper-transcribe"
	minFreeKB   = 10240
	resolverTag = "wsi-resolve-model: "
)

var wholeSeconds = regexp.MustCompile(`^[0-9]+$`)

const banner = `┌─────────────────────────────────────────────────────────────────┐
│  WSI - Whisper Speech Input v2.0 (Enhanced)                    │
│  Speech-to-text orchestrator for GNOME extension               │
└─────────────────────────────────────────────────────────────────┘`

// probeServer is W4.
func (r *run) probeServer(ctx context.Context) error {
	if r.o.server == "" {
		return nil
	}
	var stdout bytes.Buffer
	_, err := r.env.Runner.Run(ctx, execx.Cmd{
		Name: curlBin, Args: []string{"-s", "-o", "/dev/null", "-w", "%{http_code}", r.o.server}, Stdout: &stdout,
	})
	code := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(code, "2") {
		if code == "" {
			code = "connection failed"
		}
		return r.fail(ctx, "Can't connect to whisper.cpp server at "+r.o.server+" (HTTP: "+code+")")
	}
	r.log.debug("Using whisper.cpp server: " + r.o.server + " (HTTP " + code + ")" + errSuffix(err))
	return nil
}

func errSuffix(err error) string {
	if err == nil {
		return ""
	}
	return "; " + err.Error()
}

func (r *run) has(name string) bool {
	_, err := r.env.LookPath(name)
	return err == nil
}

// dep is one tool the debug check looks for.
type dep struct{ cmd, pkg, purpose string }

// debugChecks is W11.
func (r *run) debugChecks(ctx context.Context) error {
	if !r.o.debug {
		return nil
	}
	r.log.say(banner)
	r.printConfig()
	r.log.say("")
	r.log.debug("Checking dependencies...")
	deps := []dep{
		{recorderBin, "pipewire", "Audio recording (PipeWire)"},
		{soxBin, soxBin, "Audio resampling"},
		{soxiBin, soxBin, "Audio file info"},
		{curlBin, curlBin, "HTTP requests to whisper.cpp server"},
	}
	paste := dep{ydotoolBin, ydotoolBin, "Auto-paste (Wayland/X11)"}
	failed := 0
	switch r.session {
	case sessionWayland:
		deps = append(deps, dep{wlCopyBin, "wl-clipboard", "Clipboard management (Wayland)"}, paste)
	case sessionX11:
		deps = append(deps, dep{xselBin, xselBin, "Clipboard management (X11)"}, paste)
	default:
		r.log.errorLine("Unknown window manager: " + r.session)
		failed++
	}
	deps = append(deps, dep{transcriber, "faster-whisper (pip install --user faster-whisper)", "Speech transcription"})
	failed += r.missingDeps(deps)
	if !r.env.Writable(r.env.TempDir) {
		r.log.errorLine("  Temp directory: " + r.env.TempDir + " (not writable)")
		failed++
	}
	if failed > 0 {
		return r.fail(ctx, fmt.Sprintf("Dependency check failed: %d errors found", failed))
	}
	r.log.debug("All dependencies satisfied")
	r.log.say("")
	r.log.info("Starting speech recording with full debugging...")
	return nil
}

// missingDeps names each missing tool with its purpose and package, and counts them.
func (r *run) missingDeps(deps []dep) int {
	missing := 0
	for _, d := range deps {
		if path, err := r.env.LookPath(d.cmd); err == nil {
			r.log.debug("  " + d.cmd + ": " + path)
			continue
		}
		r.log.errorLine("  " + d.cmd + ": NOT FOUND")
		r.log.errorLine("    Purpose: " + d.purpose)
		r.log.errorLine("    Install: sudo dnf install " + d.pkg)
		missing++
	}
	return missing
}

func (r *run) printConfig() {
	mode := "PRIMARY"
	if r.o.clipboard {
		mode = "CLIPBOARD"
	}
	if r.o.autoPaste {
		mode = "AUTO-PASTE"
	}
	path, err := r.env.LookPath(transcriber)
	if err != nil {
		path = "NOT FOUND"
	}
	r.log.say("Configuration:\n" +
		"  Temp directory:     " + r.env.TempDir + "\n" +
		"  Temp audio file:    " + r.wav + "\n" +
		"  Whisperfile:        " + transcriber + " (" + path + ")\n" +
		"  Paste mode:         " + mode + "\n" +
		"  Window manager:     " + r.session + "\n" +
		"  Log file:           " + r.log.file)
}

// preflight is W12 and W13.
func (r *run) preflight(ctx context.Context) error {
	r.log.debug("Running preflight sanity checks...")
	for _, c := range [][2]string{{recorderBin, "pipewire"}, {soxBin, soxBin}, {soxiBin, soxBin}} {
		if !r.has(c[0]) {
			return r.failWithHint(ctx, "Required command not found: "+c[0], "Install with: sudo dnf install "+c[1])
		}
	}
	if r.o.server == "" && !r.has(transcriber) {
		return r.failWithHint(ctx, "Whisper not found: "+transcriber, "Install with: pip install --user faster-whisper")
	}
	if err := r.pasteTools(ctx); err != nil {
		return err
	}
	if !r.env.Writable(r.env.TempDir) {
		return r.fail(ctx, "Temp directory not writable: "+r.env.TempDir)
	}
	free, err := r.env.FreeKB(r.env.TempDir)
	if err != nil {
		return r.fail(ctx, "Cannot read the free space in "+r.env.TempDir+": "+err.Error())
	}
	if free < minFreeKB {
		r.logError(ctx, fmt.Sprintf("Insufficient space in %s (%dKB available, need 10MB)", r.env.TempDir, free))
		r.panel.notifyState(ctx, stateError, "Disk space low - cannot record", "")
		return errFailed
	}
	r.log.debug(fmt.Sprintf("Available space: %dKB", free))
	return r.readGrace(ctx)
}

func (r *run) failWithHint(ctx context.Context, msg, hint string) error {
	err := r.fail(ctx, msg)
	r.log.toFile(hint)
	return err
}

// pasteTools is the half of W12 that depends on how the text is delivered.
func (r *run) pasteTools(ctx context.Context) error {
	if r.o.autoPaste {
		switch {
		case !r.has(ydotoolBin):
			return r.failWithHint(ctx, "ydotool not found (required for auto-paste)", "Install with: sudo dnf install ydotool")
		case !r.env.IsSocket(r.socket):
			err := r.failWithHint(ctx, "ydotool socket not found: "+r.socket, "ydotool service not running")
			r.log.toFile("Fix: sudo systemctl restart ydotool")
			return err
		case !r.env.Writable(r.socket):
			return r.failWithHint(ctx, "ydotool socket not writable", "Fix: sudo systemctl restart ydotool")
		}
		return nil
	}
	switch {
	case r.session == sessionWayland && !r.has(wlCopyBin):
		return r.failWithHint(ctx, "wl-copy not found (required for Wayland clipboard)", "Install with: sudo dnf install wl-clipboard")
	case r.session == sessionX11 && !r.has(xselBin):
		return r.failWithHint(ctx, "xsel not found (required for X11 clipboard)", "Install with: sudo dnf install xsel")
	}
	return nil
}

// sibling runs a helper that lives beside this binary, as the bash ran its siblings; it
// returns the helper's stdout less its trailing newlines and the path it ran.
func (r *run) sibling(ctx context.Context, name string, stderr io.Writer, args ...string) (string, string, error) {
	path := filepath.Join(r.env.BinDir, name)
	var stdout bytes.Buffer
	_, err := r.env.Runner.Run(ctx, execx.Cmd{Name: path, Args: args, Stdout: &stdout, Stderr: stderr})
	return strings.TrimRight(stdout.String(), "\n"), path, err
}

// readGrace is W13.
func (r *run) readGrace(ctx context.Context) error {
	out, path, err := r.sibling(ctx, "wsi-stop-grace", r.env.Stderr)
	if err == nil && wholeSeconds.MatchString(out) {
		r.grace, err = strconv.Atoi(out)
	}
	if err != nil || !wholeSeconds.MatchString(out) {
		return r.fail(ctx, "Cannot read the stop grace ("+path+" failed; re-run play-speech-to-text.yml)")
	}
	r.log.debug(fmt.Sprintf("Stop grace: %ds", r.grace))
	return nil
}

// systemLanguage is the language of the locale lang names, as the panel computes it.
func systemLanguage(lang string) string {
	if lang == "" {
		lang = "en_GB.UTF-8"
	}
	return strings.SplitN(lang, "_", 2)[0]
}

// chooseModel is W14: the language, then the model, before the microphone opens.
func (r *run) chooseModel(ctx context.Context) error {
	if r.o.server != "" {
		r.log.debug("All preflight checks passed")
		return nil
	}
	if err := r.chooseLanguage(ctx); err != nil {
		return err
	}
	wanted := r.env.Getenv("WHISPER_MODEL")
	if wanted == "" {
		wanted = "auto"
	}
	var said bytes.Buffer
	model, path, err := r.sibling(ctx, "wsi-resolve-model", &said, "--mode", "batch", "--language", r.language, wanted)
	reason := strings.TrimRight(said.String(), "\n")
	if reason != "" {
		r.log.debug(reason)
	}
	switch {
	case exitCodeOf(err) == 3:
		if i := strings.LastIndex(reason, resolverTag); i >= 0 {
			reason = reason[i+len(resolverTag):]
		}
		return r.fail(ctx, reason)
	case err != nil:
		return r.fail(ctx, "Cannot choose the Whisper model ("+path+" failed: "+reason+")")
	}
	r.model = model
	r.log.debug("Whisper model: " + model)
	r.log.debug("All preflight checks passed")
	return nil
}

// chooseLanguage is --language, else $WHISPER_LANGUAGE (even empty), else the panel's setting.
func (r *run) chooseLanguage(ctx context.Context) error {
	r.language = r.o.language
	if r.language != "" {
		return nil
	}
	if lang, ok := r.env.LookupEnv("WHISPER_LANGUAGE"); ok {
		r.language = lang
		return nil
	}
	setting, path, err := r.sibling(ctx, "wsi-setting", r.env.Stderr, "language")
	if err != nil {
		return r.fail(ctx, "Cannot read the language setting ("+path+" failed; reason above)")
	}
	r.language = setting
	if setting == "system" {
		r.language = systemLanguage(r.env.Getenv("LANG"))
	}
	return nil
}
