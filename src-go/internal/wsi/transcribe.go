package wsi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// checkRecording is W19.
func (r *run) checkRecording(ctx context.Context) error {
	r.log.debug("Recording complete: " + r.wav)
	info, err := os.Stat(r.wav)
	if err != nil || info.Size() == 0 {
		r.logError(ctx, "Recording file empty or missing: "+r.wav)
		r.panel.notifyState(ctx, stateError, "Recording failed", "")
		return errFailed
	}
	return nil
}

// audioTool runs sox, its stderr shown only with -d, and puts its output in place of the
// recording (W20).
func (r *run) audioTool(ctx context.Context, what, output string, args ...string) error {
	stderr := io.Writer(nil)
	if r.o.debug {
		stderr = r.env.Stderr
	}
	argv := append([]string{r.wav, output}, args...)
	if _, err := r.env.Runner.Run(ctx, execx.Cmd{Name: "sox", Args: argv, Stderr: stderr}); err != nil {
		return r.fail(ctx, what+" failed: "+err.Error())
	}
	if err := os.Rename(output, r.wav); err != nil {
		return r.fail(ctx, what+" failed: "+err.Error())
	}
	return nil
}

// wholeSecondsOf is soxi's duration up to its decimal point, as the bash read it.
func wholeSecondsOf(out string) (int, bool) {
	whole := strings.TrimSpace(out)
	if i := strings.IndexByte(whole, '.'); i >= 0 {
		whole = whole[:i]
	}
	n, err := strconv.Atoi(whole)
	return n, err == nil
}

// duration is soxi's whole seconds; false, said as a problem, when soxi cannot say.
func (r *run) duration(ctx context.Context) (int, bool) {
	out, err := execx.Combined(ctx, r.env.Runner, execx.Cmd{Name: soxiBin, Args: []string{"-D", r.wav}})
	seconds, ok := wholeSecondsOf(out)
	if err == nil && ok {
		return seconds, true
	}
	r.problem(ctx, "Could not read the audio duration (soxi): "+strings.TrimSpace(out))
	r.problem(ctx, "  Skipping the minimum-length pad; transcription may reject short audio.")
	return 0, false
}

// prepareAudio is W20: 16 kHz, and at least a second long.
func (r *run) prepareAudio(ctx context.Context) error {
	r.log.debug("Resampling audio from 44100Hz to 16000Hz...")
	if err := r.audioTool(ctx, "Resampling the recording", r.wav+".resampled.wav", "rate", "16k"); err != nil {
		return err
	}
	seconds, ok := r.duration(ctx)
	if !ok {
		return nil
	}
	r.log.debug(fmt.Sprintf("Audio duration: %ds", seconds))
	if seconds >= 1 {
		return nil
	}
	r.log.debug("Audio too short, padding to 1 second...")
	return r.audioTool(ctx, "Padding the recording", r.wav+".padded", "pad", "0", "1")
}

// transcribe is W20-W24: the text the dictation delivers.
func (r *run) transcribe(ctx context.Context) (string, error) {
	if err := r.prepareAudio(ctx); err != nil {
		return "", err
	}
	r.panel.notifyState(ctx, "TRANSCRIBING", "Transcribing...", "")
	raw, err := r.speechToText(ctx)
	if err != nil {
		return "", err
	}
	r.log.debug("Raw transcription: '" + raw + "'")
	text := cleanTranscript(raw)
	if text == "" {
		r.logError(ctx, "No transcription result (empty string)")
		r.log.debug("Possible causes: no speech detected, background noise too high, or the microphone is not working")
		r.panel.notifyState(ctx, stateError, "No speech detected", "dialog-warning")
		return "", errFailed
	}
	r.log.debug("Processed result: '" + text + "'")
	if r.o.claude {
		if text, err = r.claude(ctx, text); err != nil {
			return "", err
		}
	} else if r.o.wrap {
		text = `speech-to-text:"` + text + `"`
		r.log.debug("Wrapped with marker: '" + text + "'")
	}
	return text, nil
}

// speechToText is W21: the server or faster-whisper.
func (r *run) speechToText(ctx context.Context) (string, error) {
	var stdout bytes.Buffer
	if r.o.server != "" {
		_, err := r.env.Runner.Run(ctx, execx.Cmd{
			Name: "curl",
			Args: []string{
				"-S", "-s", r.o.server + "/inference", "-H", "Content-Type: multipart/form-data",
				"-F", "file=@" + r.wav, "-F", "temperature=0.0", "-F", "temperature_inc=0.2", "-F", "response_format=text",
			},
			Stdout: &stdout, Stderr: r.env.Stderr,
		})
		if err != nil {
			return "", r.fail(ctx, fmt.Sprintf("The whisper.cpp server request failed (curl exit %d)", exitCodeOf(err)))
		}
		return stdout.String(), nil
	}
	path, err := r.env.LookPath(transcriber)
	if err != nil {
		r.logError(ctx, "No transcription backend available!")
		r.logError(ctx, "Expected: "+transcriber+" command in PATH")
		r.panel.notifyState(ctx, stateError, "No transcription backend", "")
		return "", errFailed
	}
	r.log.debug("Using faster-whisper: " + path + " (language: " + r.language + ", model: " + r.model + ")")
	_, err = r.env.Runner.Run(ctx, execx.Cmd{
		Name: transcriber, Args: []string{r.wav}, Stdout: &stdout,
		Env: []string{"WHISPER_LANGUAGE=" + r.language, "WHISPER_MODEL=" + r.model},
	})
	if code := exitCodeOf(err); code != 0 {
		r.logError(ctx, fmt.Sprintf("faster-whisper failed with exit code %d", code))
		r.panel.notifyState(ctx, stateError, fmt.Sprintf("Transcription failed (code %d)", code), "")
		return "", errFailed
	}
	return stdout.String(), nil
}

// claude is W23: Claude's text, or the raw text left on the clipboard and an error.
func (r *run) claude(ctx context.Context, text string) (string, error) {
	if !r.has("wsi-claude-process") {
		return "", r.fail(ctx, "wsi-claude-process not found (re-run play-speech-to-text.yml)")
	}
	r.log.debug("Post-processing through Claude Code (model: " + r.o.claudeModel + ")")
	r.panel.notifyState(ctx, stateClaude, "Claude is post-processing...", "")
	args := []string{"--model", r.o.claudeModel, "--style", r.o.claudeStyle}
	if r.o.debug {
		args = append(args, "--debug")
	}
	var stdout, stderr bytes.Buffer
	_, err := r.env.Runner.Run(ctx, execx.Cmd{
		Name: "wsi-claude-process", Args: append(args, text), Stdout: &stdout, Stderr: &stderr,
	})
	processed := strings.TrimRight(stdout.String(), "\n")
	if err == nil && processed != "" {
		r.log.debug("Claude processing successful: '" + processed + "'")
		return processed, nil
	}
	reason := strings.TrimPrefix(strings.ReplaceAll(stderr.String(), "\n", " "), "wsi-claude-process: ")
	if reason == "" {
		reason = fmt.Sprintf("exit %d, no output", exitCodeOf(err))
	}
	r.logError(ctx, "Claude: "+reason)
	if cerr := r.copyText(ctx, selectionClipboard, text, true); cerr != nil {
		r.log.debug("The raw transcript could not be copied either")
	}
	r.panel.notify(ctx, "Claude post-processing failed: "+reason+". Nothing was pasted. The raw transcript is on the clipboard: Ctrl+V.",
		errorIcon, notifyStaysMS)
	return "", errFailed
}
