package wsi

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestW2HelpIsTheUsageOnStderrAndNothingRuns(t *testing.T) {
	k := newKit(t)
	if code := k.exec("--help"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if k.errb.String() != usageText || k.out.Len() != 0 || len(k.calls()) != 0 {
		t.Fatalf("stderr %q, calls %q", k.errb.String(), k.calls())
	}
}

func TestW3AnArgumentErrorIsReportedToThePanelWithTheTokenAndExits2(t *testing.T) {
	k := newKit(t)
	if code := k.exec("--paste-with-shift", "7", "--session-token", "tok-9"); code != 2 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("[ERROR] --paste-with-shift requires 0 (Ctrl+V) or 1 (Ctrl+Shift+V), got: 7")
	calls := k.calls()
	if len(calls) == 0 || !strings.HasSuffix(calls[0], "'--paste-with-shift requires 0 (Ctrl+V) or 1 (Ctrl+Shift+V), got: 7' 'tok-9'") {
		t.Fatalf("calls %q", calls)
	}
	if !slices.Equal(k.states(), []string{"ERROR"}) || k.rec.started != 0 {
		t.Fatalf("states %q, recorder %d", k.states(), k.rec.started)
	}
}

func TestW4AServerThatDoesNotAnswerIsExit1BeforeAnythingElse(t *testing.T) {
	k := newKit(t)
	k.run.On([]string{"curl", "-s"}, execx.Result{Stdout: []byte("000"), ExitCode: 7})
	if code := k.exec("-n"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("[ERROR] Can't connect to whisper.cpp server at 127.0.0.1:58080 (HTTP: 000)")
	if k.rec.started != 0 {
		t.Fatal("the microphone was opened")
	}
	k = newKit(t)
	k.run.On([]string{"curl", "-s"}, execx.Result{ExitCode: 6})
	if code := k.exec("host:1"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("(HTTP: connection failed)")
}

func TestW21AServerTranscribesWithCurlAndNeedsNoModel(t *testing.T) {
	k := newKit(t)
	k.run.On([]string{"curl", "-S"}, execx.Result{Stdout: []byte(" from the server\n")})
	k.missing = []string{"faster-whisper-transcribe"}
	if code := k.exec("server:9"); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	if k.out.String() != "From the server\n" {
		t.Fatalf("stdout %q", k.out.String())
	}
	want := "curl -S -s server:9/inference -H Content-Type: multipart/form-data -F file=@" + k.wav() +
		" -F temperature=0.0 -F temperature_inc=0.2 -F response_format=text"
	if !slices.Contains(k.calls(), want) || k.called(filepath.Join(k.bin, "wsi-resolve-model")) {
		t.Fatalf("calls %q", k.calls())
	}
	k = newKit(t)
	k.run.On([]string{"curl", "-S"}, execx.Result{ExitCode: 7})
	if code := k.exec("server:9"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("[ERROR] The whisper.cpp server request failed (curl exit 7)")
}

func TestW5ALiveRecorderIsLeftAloneAndExit1(t *testing.T) {
	k := newKit(t)
	if err := os.WriteFile(k.pidFile(), []byte("777\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	k.alive[777] = true
	if code := k.exec("--session-token", "t"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if k.errb.String() != "wsi: already recording (PID 777); not starting a second recorder\n" || len(k.calls()) != 0 {
		t.Fatalf("stderr %q calls %q", k.errb.String(), k.calls())
	}
	if data, err := os.ReadFile(k.pidFile()); err != nil || string(data) != "777\n" {
		t.Fatalf("the PID file changed: %q %v", data, err)
	}
}

func TestW1ToW26AWholeDictationWithNoDesktopPrintsTheText(t *testing.T) {
	k := newKit(t)
	if err := os.WriteFile(k.pidFile(), []byte("777\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := k.exec("--session-token", "tok"); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	if k.out.String() != "Hello world\n" {
		t.Fatalf("stdout %q", k.out.String())
	}
	if !slices.Equal(k.states(), []string{"PREPARING", "RECORDING", "TRANSCRIBING", "IDLE"}) {
		t.Fatalf("states %q", k.states())
	}
	for _, path := range []string{k.pidFile(), k.wav(), k.wav() + ".resampled.wav"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s is left behind: %v", path, err)
		}
	}
	saved, err := os.ReadFile(filepath.Join(k.home, ".cache/speech-to-text/last-transcription.txt"))
	if err != nil || string(saved) != "Hello world\n" {
		t.Fatalf("saved %q %v", saved, err)
	}
	for _, want := range []string{
		filepath.Join(k.bin, "wsi-stop-grace"),
		filepath.Join(k.bin, "wsi-resolve-model") + " --mode batch --language en auto",
		"sox " + k.wav() + " " + k.wav() + ".resampled.wav rate 16k",
		"soxi -D " + k.wav(),
		"faster-whisper-transcribe " + k.wav(),
	} {
		if !slices.Contains(k.calls(), want) {
			t.Errorf("not run: %q", want)
		}
	}
	for _, c := range k.run.Calls() {
		if c.Name == "faster-whisper-transcribe" && !slices.Equal(c.Env, []string{"WHISPER_LANGUAGE=en", "WHISPER_MODEL=small"}) {
			t.Errorf("transcriber env %q", c.Env)
		}
	}
	if slices.Contains(k.sleeps, time.Duration(0)) {
		t.Errorf("a zero sleep: %v", k.sleeps)
	}
}

func TestW6AnotherClientsPIDFileIsLeftInPlace(t *testing.T) {
	k := newKit(t)
	k.onState["RECORDING"] = func() {
		if err := os.WriteFile(k.pidFile(), []byte("999\n"), 0o600); err != nil {
			t.Error(err)
		}
		k.signals <- syscall.SIGTERM
	}
	if code := k.exec(); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	if data, err := os.ReadFile(k.pidFile()); err != nil || string(data) != "999\n" {
		t.Fatalf("pid file %q %v", data, err)
	}
}

func TestW11DebugChecksEveryDependencyFirst(t *testing.T) {
	k := newKit(t)
	k.desktop()
	k.missing = []string{"curl", "ydotool"}
	if code := k.exec("-d"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("WSI - Whisper Speech Input")
	k.wantStderr("Configuration:\n  Temp directory:     " + k.tmp)
	k.wantStderr("[ERROR]   curl: NOT FOUND")
	k.wantStderr("[ERROR]     Install: sudo dnf install ydotool")
	k.wantStderr("[ERROR] Dependency check failed: 2 errors found")
	if k.rec.started != 0 {
		t.Fatal("recorded")
	}
}

func TestW12EachPreflightFailureIsExit1BeforeTheMicrophone(t *testing.T) {
	cases := []struct {
		name  string
		setup func(k *kit)
		args  []string
		want  string
	}{
		{"sox", func(k *kit) { k.missing = []string{"sox"} }, nil, "Required command not found: sox"},
		{"whisper", func(k *kit) { k.missing = []string{"faster-whisper-transcribe"} }, nil, "Whisper not found: faster-whisper-transcribe"},
		{"ydotool", func(k *kit) { k.missing = []string{"ydotool"} }, []string{"-a"}, "ydotool not found (required for auto-paste)"},
		{"socket", func(k *kit) { k.missing = []string{"socket"} }, []string{"-a"}, "ydotool socket not found: "},
		{"socket writable", func(k *kit) { k.missing = []string{"writable:" + k.socket} }, []string{"-a"}, "ydotool socket not writable"},
		{"wl-copy", func(k *kit) { k.desktop(); k.missing = []string{"wl-copy"} }, nil, "wl-copy not found (required for Wayland clipboard)"},
		{"xsel", func(k *kit) { k.env["XDG_SESSION_TYPE"] = "x11"; k.missing = []string{"xsel"} }, nil, "xsel not found (required for X11 clipboard)"},
		{"temp", func(k *kit) { k.missing = []string{"writable:" + k.tmp} }, nil, "Temp directory not writable: "},
		{"space", func(k *kit) { k.free = 100 }, nil, "Insufficient space in "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k := newKit(t)
			c.setup(k)
			if code := k.exec(c.args...); code != 1 {
				t.Fatalf("exit %d", code)
			}
			k.wantStderr("[ERROR] " + c.want)
			if k.rec.started != 0 || !slices.Contains(k.states(), "ERROR") {
				t.Fatalf("recorder %d, states %q", k.rec.started, k.states())
			}
		})
	}
}

func TestW13NoGraceNoRecording(t *testing.T) {
	for _, res := range []execx.Result{{ExitCode: 1}, {Stdout: []byte("uint32 3\n")}} {
		k := newKit(t)
		k.run.On([]string{filepath.Join(k.bin, "wsi-stop-grace")}, res)
		if code := k.exec(); code != 1 {
			t.Fatalf("exit %d", code)
		}
		k.wantStderr("[ERROR] Cannot read the stop grace (" + filepath.Join(k.bin, "wsi-stop-grace") + " failed; re-run play-speech-to-text.yml)")
		if k.rec.started != 0 {
			t.Fatal("recorded")
		}
	}
}

func TestW14TheLanguageAndTheModel(t *testing.T) {
	k := newKit(t)
	k.env["LANG"] = "de_DE.UTF-8"
	k.env["WHISPER_MODEL"] = "large-v3"
	k.unset = nil
	if code := k.exec(); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	if !k.called(filepath.Join(k.bin, "wsi-resolve-model") + " --mode batch --language de large-v3") {
		t.Fatalf("calls %q", k.calls())
	}

	k = newKit(t)
	k.env["WHISPER_LANGUAGE"] = ""
	k.unset = []string{"WHISPER_MODEL"}
	if code := k.exec(); code != 0 || k.called(filepath.Join(k.bin, "wsi-setting")) {
		t.Fatalf("exit %d: an empty WHISPER_LANGUAGE is a language (detection): %q", code, k.calls())
	}

	k = newKit(t)
	if code := k.exec("--language", "fr"); code != 0 || !k.called(filepath.Join(k.bin, "wsi-resolve-model")+" --mode batch --language fr auto") {
		t.Fatalf("exit %d %q", code, k.calls())
	}
}

func TestW14ResolverAndSettingFailures(t *testing.T) {
	k := newKit(t)
	k.run.On([]string{filepath.Join(k.bin, "wsi-resolve-model")}, execx.Result{ExitCode: 3, Stderr: []byte("note\nwsi-resolve-model: No speech model is downloaded. Open Manage Whisper Models.\n")})
	if code := k.exec(); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("[ERROR] No speech model is downloaded. Open Manage Whisper Models.\n")
	if k.rec.started != 0 || !strings.HasPrefix(k.notes()[0], "'No speech model is downloaded. Open Manage Whisper Models.") {
		t.Fatalf("notes %q", k.notes())
	}

	k = newKit(t)
	k.run.On([]string{filepath.Join(k.bin, "wsi-resolve-model")}, execx.Result{ExitCode: 1, Stderr: []byte("wsi-resolve-model: cannot ask CTranslate2\n")})
	if code := k.exec(); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("Cannot choose the Whisper model (" + filepath.Join(k.bin, "wsi-resolve-model") + " failed: wsi-resolve-model: cannot ask CTranslate2)")

	k = newKit(t)
	k.run.On([]string{filepath.Join(k.bin, "wsi-setting")}, execx.Result{ExitCode: 1})
	if code := k.exec(); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("Cannot read the language setting (" + filepath.Join(k.bin, "wsi-setting") + " failed; reason above)")
}

func TestW19AnEmptyRecordingIsAnError(t *testing.T) {
	k := newKit(t)
	k.silent = true
	if code := k.exec(); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("[ERROR] Recording file empty or missing: " + k.wav())
	if k.called("sox") {
		t.Fatal("resampled nothing")
	}
}

func TestW20AudioToolFailures(t *testing.T) {
	k := newKit(t)
	k.run.On([]string{"sox"}, execx.Result{ExitCode: 2, Stderr: []byte("sox FAIL")})
	if code := k.exec(); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("[ERROR] Resampling the recording failed: sox exited 2: sox FAIL")

	k = newKit(t)
	k.run.On([]string{"soxi", "-D"}, execx.Result{ExitCode: 1, Stderr: []byte("bad header")})
	if code := k.exec(); code != 0 {
		t.Fatalf("a soxi failure is a problem only: exit %d", code)
	}
	k.wantStderr("[ERROR] Could not read the audio duration (soxi): bad header")
	k.wantStderr("[ERROR]   Skipping the minimum-length pad; transcription may reject short audio.")

	k = newKit(t)
	k.run.On([]string{"soxi", "-D"}, execx.Result{Stdout: []byte("0.400000\n")})
	if code := k.exec(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !k.called("sox " + k.wav() + " " + k.wav() + ".padded pad 0 1") {
		t.Fatalf("not padded: %q", k.calls())
	}
}

func TestW21AndW22TranscriberFailureAndNoSpeech(t *testing.T) {
	k := newKit(t)
	k.run.On([]string{"faster-whisper-transcribe"}, execx.Result{ExitCode: 4})
	if code := k.exec(); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("[ERROR] faster-whisper failed with exit code 4")
	if n := k.notes(); !strings.HasPrefix(n[len(n)-1], "'Transcription failed (code 4)'") {
		t.Fatalf("notes %q", n)
	}

	k = newKit(t)
	k.run.On([]string{"faster-whisper-transcribe"}, execx.Result{Stdout: []byte("[BLANK_AUDIO]\n")})
	if code := k.exec(); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("[ERROR] No transcription result (empty string)")
	if n := k.notes(); n[len(n)-1] != "'No speech detected'|'dialog-warning'|3000" {
		t.Fatalf("notes %q", n)
	}
}

func TestW23ClaudeReplacesTheTextOrLeavesTheRawTextOnTheClipboard(t *testing.T) {
	k := newKit(t)
	k.run.On([]string{"wsi-claude-process"}, execx.Result{Stdout: []byte("Polished.\n")})
	if code := k.exec("--claude-process", "--claude-model", "opus", "-w"); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	if k.out.String() != "Polished.\n" || !k.called("wsi-claude-process --model opus --style corporate Hello world") {
		t.Fatalf("stdout %q calls %q", k.out.String(), k.calls())
	}

	k = newKit(t)
	k.desktop()
	k.run.On([]string{"wsi-claude-process"}, execx.Result{ExitCode: 2, Stderr: []byte("wsi-claude-process: the API said no\nretry later\n")})
	if code := k.exec("--claude-process", "-d"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("[ERROR] Claude: the API said no retry later")
	if k.copied["wl-copy"] != "Hello world" {
		t.Fatalf("clipboard %q", k.copied)
	}
	n := k.notes()
	if n[len(n)-1] != "'Claude post-processing failed: the API said no retry later . Nothing was pasted. The raw transcript is on the clipboard: Ctrl+V.'|'dialog-error'|0" {
		t.Fatalf("notes %q", n)
	}
	if !k.called("wsi-claude-process --model sonnet --style corporate --debug Hello world") {
		t.Fatalf("calls %q", k.calls())
	}

	// The panel passes --no-notify unless Show Notifications is on (its default is off): the
	// failure must still be seen, or Ctrl+Insert looks as if it did nothing (Plan 00208 Task 3.1)
	k = newKit(t)
	k.desktop()
	k.run.On([]string{"wsi-claude-process"}, execx.Result{ExitCode: 1, Stderr: []byte("wsi-claude-process: Not logged in\n")})
	if code := k.exec("--claude-process", "--no-notify"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if n := k.notes(); len(n) != 1 || n[0] != "'Claude post-processing failed: Not logged in . Nothing was pasted. The raw transcript is on the clipboard: Ctrl+V.'|'dialog-error'|0" {
		t.Fatalf("with --no-notify, notes %q", n)
	}

	k = newKit(t)
	k.missing = []string{"wsi-claude-process"}
	if code := k.exec("--claude-process"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("wsi-claude-process not found (re-run play-speech-to-text.yml)")
}

// Plan 00187 (#100): the panel is told when the transcript is done and Claude is working on
// it, with the session's token, before wsi-claude-process runs.
func TestW23ClaudeProcessingIsSignalledBeforeTheClaudeStep(t *testing.T) {
	k := newKit(t)
	k.run.On([]string{"wsi-claude-process"}, execx.Result{Stdout: []byte("Polished.\n")})
	if code := k.exec("--claude-process", "--session-token", "tok-7"); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	if !slices.Equal(k.states(), []string{"PREPARING", "RECORDING", "TRANSCRIBING", "CLAUDE_PROCESSING", "IDLE"}) {
		t.Fatalf("states %q", k.states())
	}
	signalled, claude := -1, -1
	for i, line := range k.calls() {
		if strings.Contains(line, signalIface+"."+stateChanged+" CLAUDE_PROCESSING 'tok-7'") {
			signalled = i
		}
		if strings.HasPrefix(line, "wsi-claude-process ") {
			claude = i
		}
	}
	if signalled < 0 || claude < 0 || signalled > claude {
		t.Fatalf("CLAUDE_PROCESSING (call %d, token-tagged) must come before wsi-claude-process (call %d): %q",
			signalled, claude, k.calls())
	}

	k = newKit(t)
	k.desktop()
	k.run.On([]string{"wsi-claude-process"}, execx.Result{ExitCode: 1, Stderr: []byte("wsi-claude-process: Not logged in\n")})
	if code := k.exec("--claude-process"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !slices.Equal(k.states(), []string{"PREPARING", "RECORDING", "TRANSCRIBING", "CLAUDE_PROCESSING", "ERROR"}) {
		t.Fatalf("a failed Claude step: states %q", k.states())
	}

	// No Claude step to run: the panel is never told one runs
	k = newKit(t)
	k.missing = []string{"wsi-claude-process"}
	if code := k.exec("--claude-process"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if slices.Contains(k.states(), "CLAUDE_PROCESSING") {
		t.Fatalf("CLAUDE_PROCESSING without wsi-claude-process: states %q", k.states())
	}

	k = newKit(t)
	if code := k.exec(); code != 0 || slices.Contains(k.states(), "CLAUDE_PROCESSING") {
		t.Fatalf("a plain dictation: exit %d states %q", code, k.states())
	}
}

func TestW24TheMarkerWrapsTheText(t *testing.T) {
	k := newKit(t)
	if code := k.exec("-w"); code != 0 || k.out.String() != "speech-to-text:\"Hello world\"\n" {
		t.Fatalf("exit %d stdout %q", code, k.out.String())
	}
}

func TestW27AndW28TheClipboardModes(t *testing.T) {
	cases := []struct {
		session, flag, tool, hint string
	}{
		{"wayland", "", "wl-copy -p", "Middle-click to paste"},
		{"wayland", "-c", "wl-copy", "Ctrl+V to paste"},
		{"x11", "", "xsel -ip", "Middle-click to paste"},
		{"x11", "-c", "xsel -ib", "Ctrl+V to paste"},
	}
	for _, c := range cases {
		k := newKit(t)
		k.env["XDG_SESSION_TYPE"] = c.session
		k.env["DISPLAY"] = ":0"
		var args []string
		if c.flag != "" {
			args = append(args, c.flag)
		}
		if code := k.exec(args...); code != 0 {
			t.Fatalf("%v: exit %d %s", c, code, k.errb.String())
		}
		if k.copied[c.tool] != "Hello world\n" {
			t.Fatalf("%v: copied %q", c, k.copied)
		}
		if n := k.notes(); n[len(n)-1] != "'Hello world\\n\\n"+c.hint+"'|'audio-input-microphone'|3000" {
			t.Fatalf("%v: notes %q", c, n)
		}
		if st := k.states(); st[len(st)-1] != "SUCCESS" {
			t.Fatalf("%v: states %q", c, st)
		}
	}
	k := newKit(t)
	k.env["XDG_SESSION_TYPE"] = "tty"
	k.env["DESKTOP_SESSION"] = "x"
	if code := k.exec(); code != 0 || k.out.String() != "Hello world\n" {
		t.Fatalf("exit %d stdout %q", code, k.out.String())
	}
	k.wantStderr("[ERROR] Unknown window manager: tty")

	k = newKit(t)
	k.desktop()
	k.run.On([]string{"wl-copy"}, execx.Result{ExitCode: 1, Stderr: []byte("no seat")})
	if code := k.exec(); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("[ERROR] Copying the text to the PRIMARY selection failed: wl-copy exited 1: no seat")
}

func TestW25AFailedSaveIsAProblemOnly(t *testing.T) {
	k := newKit(t)
	if err := os.MkdirAll(filepath.Join(k.home, ".cache"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(k.home, ".cache/speech-to-text"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := k.exec(); code != 0 || k.out.String() != "Hello world\n" {
		t.Fatalf("exit %d stdout %q", code, k.out.String())
	}
	k.wantStderr("[ERROR] Could not save the text to ")
}

func TestB18AnUnsetHomeIsAnErrorAndAnUnsetUserIsTheAccount(t *testing.T) {
	k := newKit(t)
	delete(k.env, "HOME")
	if code := k.exec(); code != 1 || k.errb.String() != "wsi: HOME is not set\n" {
		t.Fatalf("exit %d stderr %q", code, k.errb.String())
	}
	k = newKit(t)
	delete(k.env, "USER")
	if code := k.exec(); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	if !k.called("sox " + filepath.Join(k.tmp, "wfile-account.wav")) {
		t.Fatalf("calls %q", k.calls())
	}
}

func TestB11APreflightFailureLeavesAFileAtTheRecordingPathAlone(t *testing.T) {
	k := newKit(t)
	k.missing = []string{"sox"}
	if err := os.WriteFile(k.wav(), []byte("not ours"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := k.exec(); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(k.wav()); err != nil {
		t.Fatalf("a run that never recorded removed the file: %v", err)
	}
}

func TestL3TheTranscriberInstallHintIsThePipLine(t *testing.T) {
	k := newKit(t)
	k.missing = []string{"faster-whisper-transcribe"}
	if code := k.exec("-d"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("[ERROR]     Install: pip install --user faster-whisper")
}
