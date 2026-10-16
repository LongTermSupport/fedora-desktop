// Package wsipost is wsi-postprocess: the speech-to-text panel's "Post-Process Last
// Transcription" items. It runs Claude post-processing (wsi-claude-process, the helper
// Ctrl+Insert and Alt+Insert use while recording) on the last transcription, on demand, after
// the dictation has ended (Plan 00208 Phase 3).
//
// What it does, numbered as its tests are:
//
//   - P1. The last transcription is ~/.cache/speech-to-text/last-transcription.txt, the file
//     every recorder saves and Copy Last Transcription reads. Its text, less trailing white
//     space, is run through `<dir>/wsi-claude-process --model <m> --style <s> [--debug] <text>`
//     (dir: this binary's), within 180 s. Claude's text is saved, with a newline, to
//     ~/.cache/speech-to-text/last-postprocessed.txt (0600; the raw file is not touched) and
//     printed on stdout, exit 0. The panel puts it on the clipboard.
//   - P2. --model is sonnet by default; --debug is passed on.
//   - P3. A `speech-to-text:"<text>"` wrap (the Wrap with Marker setting) is removed first.
//   - P4. No file: `No transcription yet: dictate one first`; only white space: `The last
//     transcription is empty`. Exit 1, nothing run.
//   - P5. wsi-claude-process failing, or exiting 0 with no output: `Claude post-processing
//     failed: <its stderr, newlines as spaces, less "wsi-claude-process: ">` (empty: `exit
//     <n>, no output`). Exit 1, nothing saved or printed.
//   - P6. A command line it cannot run (--style not corporate or natural, or missing; --model
//     not sonnet, opus or haiku; a value missing; an unknown option): the reason and the usage
//     line, exit 64, nothing run.
//   - P7. -h/--help: the usage on stdout, exit 0.
//   - P8. A result that cannot be saved: `Could not save the post-processed text: <why>`,
//     exit 1, nothing printed.
//
// Every message goes to stderr as `wsi-postprocess: <message>`; stdout is only Claude's text.
package wsipost

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	usage         = "usage: wsi-postprocess --style corporate|natural [--model sonnet|opus|haiku] [--debug]\n"
	claudeHelper  = "wsi-claude-process"
	claudeTimeout = 180 * time.Second // wsi-claude-process gives up on claude itself at 170 s
	marker        = `speech-to-text:"`
	cacheDir      = ".cache/speech-to-text"
	optStyle      = "--style"
	optModel      = "--model"
)

var (
	styles = []string{"corporate", "natural"}
	models = []string{"sonnet", "opus", "haiku"}
)

// Env is everything a run touches outside its own memory and the two cache files.
type Env struct {
	Runner execx.Runner
	Stdout io.Writer
	Stderr io.Writer
	Home   string
	// BinDir holds wsi-claude-process, beside this binary.
	BinDir string
}

// NewEnv is the production environment.
func NewEnv() (Env, error) {
	home := os.Getenv("HOME")
	if home == "" {
		return Env{}, errors.New("HOME is not set")
	}
	self, err := os.Executable()
	if err != nil {
		return Env{}, fmt.Errorf("where is this binary: %w", err)
	}
	return Env{Runner: execx.OS{}, Stdout: os.Stdout, Stderr: os.Stderr, Home: home, BinDir: filepath.Dir(self)}, nil
}

// App is one wsi-postprocess invocation.
type App struct {
	Env Env
}

type options struct {
	style, model string
	debug, help  bool
}

// errFailed is a failure already reported.
var errFailed = errors.New("failed")

// Run is P1-P8.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	o, err := parseArgs(args)
	switch {
	case err != nil:
		a.say(err.Error())
		if _, werr := io.WriteString(a.Env.Stderr, usage); werr != nil {
			return term.ExitFailure
		}
		return term.ExitUsage
	case o.help:
		if _, werr := io.WriteString(a.Env.Stdout, usage); werr != nil {
			return term.ExitFailure
		}
		return term.ExitOK
	}
	text, err := a.postProcess(ctx, o)
	if err != nil {
		return term.ExitFailure
	}
	if _, err := io.WriteString(a.Env.Stdout, text+"\n"); err != nil {
		a.say("could not write the text to stdout: " + err.Error())
		return term.ExitFailure
	}
	return term.ExitOK
}

func parseArgs(args []string) (options, error) {
	o := options{model: "sonnet"}
	for rest := args; len(rest) > 0; rest = rest[1:] {
		switch arg := rest[0]; arg {
		case "-h", "--help":
			o.help = true
			return o, nil
		case "--debug", "-d":
			o.debug = true
		case optStyle, optModel:
			if len(rest) == 1 {
				return o, fmt.Errorf("%s requires a value", arg)
			}
			rest = rest[1:]
			if arg == optStyle {
				o.style = rest[0]
			} else {
				o.model = rest[0]
			}
		default:
			return o, fmt.Errorf("unknown option: %s", arg)
		}
	}
	switch {
	case o.style == "":
		return o, errors.New("--style is required")
	case !slices.Contains(styles, o.style):
		return o, errors.New("--style requires corporate or natural, got: " + o.style)
	case !slices.Contains(models, o.model):
		return o, errors.New("--model requires sonnet, opus or haiku, got: " + o.model)
	}
	return o, nil
}

// postProcess is P1-P5 and P8: Claude's text, saved, or a failure already reported.
func (a App) postProcess(ctx context.Context, o options) (string, error) {
	text, err := a.lastTranscription()
	if err != nil {
		return "", err
	}
	args := []string{"--model", o.model, "--style", o.style}
	if o.debug {
		args = append(args, "--debug")
	}
	ctx, cancel := context.WithTimeout(ctx, claudeTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	_, err = a.Env.Runner.Run(ctx, execx.Cmd{
		Name: filepath.Join(a.Env.BinDir, claudeHelper), Args: append(args, text), Stdout: &stdout, Stderr: &stderr,
	})
	processed := strings.TrimSpace(stdout.String())
	if err != nil || processed == "" {
		reason := strings.TrimSpace(strings.ReplaceAll(stderr.String(), "\n", " "))
		reason = strings.TrimPrefix(reason, claudeHelper+": ")
		if reason == "" {
			reason = fmt.Sprintf("exit %d, no output", exitCode(err))
		}
		a.say("Claude post-processing failed: " + reason)
		return "", errFailed
	}
	path := filepath.Join(a.Env.Home, cacheDir, "last-postprocessed.txt")
	if err := fsx.WriteAtomic(path, "post-processed text", []byte(processed+"\n"), 0o600); err != nil {
		a.say("Could not save the post-processed text: " + err.Error())
		return "", errFailed
	}
	return processed, nil
}

// lastTranscription is P1's input, P3's unwrapping and P4's failures.
func (a App) lastTranscription() (string, error) {
	data, err := os.ReadFile(filepath.Join(a.Env.Home, cacheDir, "last-transcription.txt"))
	switch {
	case errors.Is(err, os.ErrNotExist):
		a.say("No transcription yet: dictate one first")
		return "", errFailed
	case err != nil:
		a.say("Cannot read the last transcription: " + err.Error())
		return "", errFailed
	}
	text := strings.TrimSpace(string(data))
	if strings.HasPrefix(text, marker) && strings.HasSuffix(text, `"`) && len(text) > len(marker) {
		text = strings.TrimSpace(text[len(marker) : len(text)-1])
	}
	if text == "" {
		a.say("The last transcription is empty")
		return "", errFailed
	}
	return text, nil
}

func (a App) say(message string) {
	if _, err := fmt.Fprintf(a.Env.Stderr, "wsi-postprocess: %s\n", message); err != nil {
		return // stderr itself is gone: nowhere left to say it, and the exit code still does
	}
}

func exitCode(err error) int {
	if exit, ok := errors.AsType[*execx.ExitError](err); ok {
		return exit.Code
	}
	if err != nil {
		return 1
	}
	return 0
}
