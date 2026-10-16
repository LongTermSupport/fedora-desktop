package wsipost

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

type kit struct {
	t          *testing.T
	run        *execx.Fake
	home, bin  string
	out, errb  bytes.Buffer
	lastPath   string
	resultPath string
}

func newKit(t *testing.T) *kit {
	t.Helper()
	root := t.TempDir()
	k := &kit{t: t, run: &execx.Fake{}, home: filepath.Join(root, "home"), bin: filepath.Join(root, "bin")}
	k.lastPath = filepath.Join(k.home, ".cache/speech-to-text/last-transcription.txt")
	k.resultPath = filepath.Join(k.home, ".cache/speech-to-text/last-postprocessed.txt")
	return k
}

func (k *kit) last(text string) {
	k.t.Helper()
	if err := os.MkdirAll(filepath.Dir(k.lastPath), 0o750); err != nil {
		k.t.Fatal(err)
	}
	if err := os.WriteFile(k.lastPath, []byte(text), 0o600); err != nil {
		k.t.Fatal(err)
	}
}

func (k *kit) claudeSays(stdout, stderr string, code int) {
	k.run.On([]string{filepath.Join(k.bin, "wsi-claude-process")},
		execx.Result{Stdout: []byte(stdout), Stderr: []byte(stderr), ExitCode: code})
}

func (k *kit) exec(args ...string) term.ExitCode {
	k.t.Helper()
	app := App{Env: Env{Runner: k.run, Stdout: &k.out, Stderr: &k.errb, Home: k.home, BinDir: k.bin}}
	return app.Run(context.Background(), args)
}

func (k *kit) wantStderr(part string) {
	k.t.Helper()
	if !strings.Contains(k.errb.String(), part) {
		k.t.Fatalf("stderr %q lacks %q", k.errb.String(), part)
	}
}

func (k *kit) noResult() {
	k.t.Helper()
	if _, err := os.Stat(k.resultPath); !os.IsNotExist(err) {
		k.t.Fatalf("a result was saved after a failure (stat: %v)", err)
	}
	if k.out.Len() != 0 {
		k.t.Fatalf("stdout %q after a failure", k.out.String())
	}
}

func TestP1TheLastTranscriptionIsPostProcessedSavedApartAndPrinted(t *testing.T) {
	k := newKit(t)
	k.last("Hello world\n")
	k.claudeSays("Hello, world.\n", "", 0)
	if code := k.exec("--style", "natural", "--model", "opus"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	want := filepath.Join(k.bin, "wsi-claude-process") + " --model opus --style natural Hello world"
	if calls := k.run.Calls(); len(calls) != 1 || calls[0].Line() != want {
		t.Fatalf("calls %v, want %q", calls, want)
	}
	if k.out.String() != "Hello, world.\n" {
		t.Fatalf("stdout %q", k.out.String())
	}
	saved, err := os.ReadFile(k.resultPath)
	if err != nil || string(saved) != "Hello, world.\n" {
		t.Fatalf("saved %q, %v", saved, err)
	}
	info, err := os.Stat(k.resultPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, %v", info, err)
	}
	raw, err := os.ReadFile(k.lastPath)
	if err != nil || string(raw) != "Hello world\n" {
		t.Fatalf("the raw transcription changed: %q, %v", raw, err)
	}
}

func TestP2TheModelDefaultsToSonnetAndDebugIsPassedOn(t *testing.T) {
	k := newKit(t)
	k.last("Hello world")
	k.claudeSays("Polished.", "", 0)
	if code := k.exec("--style", "corporate", "--debug"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	if !k.run.Called(filepath.Join(k.bin, "wsi-claude-process") + " --model sonnet --style corporate --debug Hello world") {
		t.Fatalf("calls %v", k.run.Calls())
	}
}

func TestP3TheMarkerWrapIsNotSentToClaude(t *testing.T) {
	k := newKit(t)
	k.last("speech-to-text:\"Hello world\"\n")
	k.claudeSays("Polished.", "", 0)
	if code := k.exec("--style", "corporate"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	if !k.run.Called(filepath.Join(k.bin, "wsi-claude-process") + " --model sonnet --style corporate Hello world") {
		t.Fatalf("calls %v", k.run.Calls())
	}
}

func TestP4NoTranscriptionYetIsAFailureThatSaysSo(t *testing.T) {
	k := newKit(t)
	if code := k.exec("--style", "corporate"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("wsi-postprocess: No transcription yet: dictate one first")
	k.noResult()
	if len(k.run.Calls()) != 0 {
		t.Fatalf("claude ran with nothing to process: %v", k.run.Calls())
	}

	k = newKit(t)
	k.last("  \n")
	if code := k.exec("--style", "corporate"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("wsi-postprocess: The last transcription is empty")
	k.noResult()
}

func TestP5AClaudeFailureSaysWhyAndSavesNothing(t *testing.T) {
	k := newKit(t)
	k.last("Hello world")
	k.claudeSays("", "wsi-claude-process: claude failed (exit 1): Not logged in\n", 1)
	if code := k.exec("--style", "natural"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("wsi-postprocess: Claude post-processing failed: claude failed (exit 1): Not logged in\n")
	k.noResult()

	k = newKit(t)
	k.last("Hello world")
	k.claudeSays("", "", 0)
	if code := k.exec("--style", "natural"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("Claude post-processing failed: exit 0, no output")
	k.noResult()

	k = newKit(t)
	k.last("Hello world")
	k.claudeSays("half an answer", "", 3)
	if code := k.exec("--style", "natural"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("Claude post-processing failed: exit 3, no output")
	k.noResult()
}

func TestP6AWrongCommandLineIsAUsageErrorThatRunsNothing(t *testing.T) {
	cases := map[string][]string{
		"--style requires corporate or natural":  {"--style", "article"},
		"--style is required":                    {},
		"--model requires sonnet, opus or haiku": {"--style", "natural", "--model", "gpt"},
		"unknown option: --paste":                {"--style", "natural", "--paste"},
		"--model requires a value":               {"--style", "natural", "--model"},
	}
	for want, args := range cases {
		k := newKit(t)
		k.last("Hello world")
		if code := k.exec(args...); code != term.ExitUsage {
			t.Fatalf("%v: exit %d", args, code)
		}
		k.wantStderr("wsi-postprocess: " + want)
		k.wantStderr("usage: wsi-postprocess --style corporate|natural")
		if len(k.run.Calls()) != 0 {
			t.Fatalf("%v ran %v", args, k.run.Calls())
		}
	}
}

func TestP7HelpGoesToStdout(t *testing.T) {
	k := newKit(t)
	if code := k.exec("--help"); code != term.ExitOK || !strings.HasPrefix(k.out.String(), "usage: wsi-postprocess") {
		t.Fatalf("exit %d stdout %q", code, k.out.String())
	}
}

func TestP8ASaveThatFailsIsAFailureAndPrintsNothing(t *testing.T) {
	k := newKit(t)
	k.last("Hello world")
	k.claudeSays("Polished.", "", 0)
	if err := os.Mkdir(k.resultPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if code := k.exec("--style", "natural"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("wsi-postprocess: Could not save the post-processed text")
	if k.out.Len() != 0 {
		t.Fatalf("stdout %q", k.out.String())
	}
}

func TestNewEnvFindsTheHelpersBesideTheBinary(t *testing.T) {
	t.Setenv("HOME", "/somewhere")
	env, err := NewEnv()
	if err != nil || env.Home != "/somewhere" || env.BinDir == "" {
		t.Fatalf("env %+v, %v", env, err)
	}
	t.Setenv("HOME", "")
	if _, err := NewEnv(); err == nil {
		t.Fatal("no HOME was accepted")
	}
}
