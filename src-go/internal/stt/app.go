// Package stt is the stt command: audio recordings transcribed into `.stt.md` sidecars by the
// warm speech-to-text server, so file transcription and dictation share one model in GPU
// memory (Plan 00179).
//
//	stt [--list | --short] [--language CODE] [--min-seconds N] PATH...
//
// A PATH is a recording folder (it holds the phone app's manifest.json), an audio file, or a
// folder of those, read one level deep. What it does, numbered as its tests are:
//
//   - S1. Each pending recording's parts, in manifest order, go to the server in one
//     FILE_START (absolute paths), after `<dir>/wsi-stream --ensure-server` (dir: this
//     binary's) has started the server if need be. Each FILE_PROGRESS names the job FILE_START
//     returned and the count of segments already read, so the text is neither repeated nor
//     another recording's. The sidecar is written beside the recording, atomically; its path
//     is the line on stdout.
//   - S2. The sidecar is YAML front matter (source, parts, duration, language, model,
//     transcribed) and the text in paragraphs, a new one after a pause of 3 s or more, each
//     opened by its time: `**[00:04:12]**`.
//   - S3. No speech is a sidecar saying so, so the recording is not sent again.
//   - S4. A recording whose sidecar opens with stt's front matter is done and is not sent; a
//     sidecar that is empty or does not (cut short, emptied, someone else's file) is a
//     failure, never quietly done.
//   - S5. A manifest whose state is not COMPLETE, or a part not there yet, is waiting: a line
//     on stderr, not a failure.
//   - S6. A part whose size or MD5 is not the manifest's is a failure.
//   - S7. A manifest that does not parse, names no parts, names a part by a path, or lacks a
//     checksum or a state, is a failure naming the manifest.
//   - S8. A loose audio file (.ogg .opus .oga .m4a .mp3 .wav .flac .webm .aac) is a recording of
//     one part; its sidecar is its name plus .stt.md. Other files, and folders with no
//     manifest, are not recordings.
//   - S9. A PATH may name one recording.
//   - S10. Recordings run in name order; the first failure ends the run, exit 1.
//   - S11. A server that refuses the recording (another file is being transcribed), cannot be
//     reached, reports a failure, or cannot be started, is a failure.
//   - S12. The language: --language, else the panel's `language` setting through
//     `<dir>/wsi-setting` ("system" is the locale's, from LANG); "auto" is detection.
//   - S13. Each new segment's time and text go to stderr as they arrive.
//   - S14. --list prints `done|pending|waiting|short|corrupt<TAB>path[<TAB>reason]` and runs
//     nothing; it does not read the audio, so a part with a bad checksum shows as pending.
//   - S15. No PATH, an unknown flag, a flag without its value, or --list with --short, is exit
//     64 and runs nothing; a PATH that is missing or not audio is exit 1 before anything
//     runs; -h/--help the usage.
//   - S16. The last stderr line counts the run: `transcribed N, already done N, waiting N`,
//     and `, too short N (...)` when there are any.
//   - S17. A complete recording whose manifest length (recordedMillis, else the parts'
//     durations) is under --min-seconds (default 3; accidental taps) is short: nothing is
//     sent and no sidecar is written. --short prints only those paths, a line each, so they
//     can be reviewed and removed. A loose audio file has no manifest length and is never short.
//   - S18. A loose audio file changed less than a minute before the run may still be being
//     copied in: it is waiting (as S5), so any folder can be watched. A recording folder's
//     manifest already says when it is whole.
package stt

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	tool  = "stt"
	usage = `usage: stt [--list | --short] [--language CODE] [--min-seconds N] PATH...

Transcribe audio recordings into <recording>.stt.md sidecars with the warm speech-to-text
server (the dictation model). A PATH is a recording folder (with the phone app's
manifest.json), an audio file, or a folder of those. Recordings with a sidecar are skipped,
and so are recordings shorter than --min-seconds (accidental taps).

  --list            show each recording as done, pending, waiting or short; transcribe nothing
  --short           print only the recordings too short to transcribe, a path a line
  --language CODE   the spoken language (default: the panel's setting; auto = detect)
  --min-seconds N   the shortest recording worth transcribing (default: 3)
`
	defaultMinSeconds = 3
	// languageKey is the panel setting, the server's request field and the sidecar's key.
	languageKey  = "language"
	pollInterval = 2 * time.Second
	replyLimit   = 64 << 20 // a long recording's every segment, in one reply
	dialTimeout  = 5 * time.Second
)

// DefaultSocket is where wsi-stream-server listens.
func DefaultSocket() string {
	return fmt.Sprintf("/run/user/%d/wsi-stream.socket", os.Getuid())
}

// Env is everything a run touches outside its own memory and the recordings.
type Env struct {
	Runner execx.Runner
	Stdout io.Writer
	Stderr io.Writer
	// BinDir holds wsi-stream and wsi-setting, beside this binary.
	BinDir string
	// Socket is the warm server's Unix socket.
	Socket string
	Now    func() time.Time
	Getenv func(string) string
	// Sleep waits between progress polls; it returns ctx's error if ctx ends first.
	Sleep func(ctx context.Context, d time.Duration) error
}

// NewEnv is the production environment.
func NewEnv() (Env, error) {
	self, err := os.Executable()
	if err != nil {
		return Env{}, fmt.Errorf("where is this binary: %w", err)
	}
	return Env{
		Runner: execx.OS{}, Stdout: os.Stdout, Stderr: os.Stderr, BinDir: filepath.Dir(self),
		Socket: DefaultSocket(), Now: time.Now, Getenv: os.Getenv, Sleep: sleep,
	}, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("stopped while waiting: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

// App is one stt invocation.
type App struct {
	Env Env
}

// errFailed is a failure already reported.
var errFailed = errors.New("failed")

type options struct {
	list, short bool
	language    string
	minMillis   int64
	paths       []string
}

// Run is S1-S17.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	o, code, ok := a.parse(args)
	if !ok {
		return code
	}
	recs, err := a.find(o.paths)
	if err != nil {
		return term.ExitFailure
	}
	if o.list || o.short {
		return a.list(recs, o)
	}
	return a.transcribeAll(ctx, o, recs)
}

func (a App) parse(args []string) (options, term.ExitCode, bool) {
	o := options{minMillis: defaultMinSeconds * 1000}
	if slices.Contains(args, "-h") || slices.Contains(args, "--help") {
		return o, a.write(a.Env.Stdout, usage, term.ExitOK), false
	}
	for i := 0; i < len(args); i++ {
		next, problem := o.take(args, i)
		if problem != "" {
			return o, a.usageError(problem), false
		}
		i = next
	}
	switch {
	case len(o.paths) == 0:
		return o, a.usageError("no recording or folder given"), false
	case o.list && o.short:
		return o, a.usageError("--list and --short are two different reports; give one"), false
	}
	return o, term.ExitOK, true
}

// take reads the argument at i (and its value); it returns the index of the last argument
// read, or what is wrong.
func (o *options) take(args []string, i int) (int, string) {
	switch arg := args[i]; {
	case arg == "--list":
		o.list = true
	case arg == "--short":
		o.short = true
	case arg == "--min-seconds":
		secs, err := strconv.ParseFloat(value(args, i), 64)
		if err != nil || secs < 0 || math.IsInf(secs, 0) || math.IsNaN(secs) {
			return i, "--min-seconds needs a number of seconds, 0 or more"
		}
		o.minMillis = int64(secs * 1000)
		return i + 1, ""
	case arg == "--language":
		if value(args, i) == "" {
			return i, "--language needs a language code, or auto"
		}
		o.language = args[i+1]
		return i + 1, ""
	case strings.HasPrefix(arg, "-"):
		return i, "unknown option: " + arg
	default:
		o.paths = append(o.paths, arg)
	}
	return i, ""
}

func (a App) usageError(problem string) term.ExitCode {
	a.say(problem)
	return a.write(a.Env.Stderr, usage, term.ExitUsage)
}

// value is the argument after a flag at i, or "" when there is none or it is another flag.
func value(args []string, i int) string {
	if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
		return ""
	}
	return args[i+1]
}

// list is S14 and S17's --short.
func (a App) list(recs []recording, o options) term.ExitCode {
	var out strings.Builder
	for _, r := range recs {
		state, reason := r.state(check{minMillis: o.minMillis, now: a.Env.Now()})
		if o.short {
			if state == stateShort {
				out.WriteString(r.dir() + "\n")
			}
			continue
		}
		line := state.String() + "\t" + r.dir()
		if reason != "" {
			line += "\t" + reason
		}
		out.WriteString(line + "\n")
	}
	return a.write(a.Env.Stdout, out.String(), term.ExitOK)
}

// transcribeAll is S1-S13 and S16.
func (a App) transcribeAll(ctx context.Context, o options, recs []recording) term.ExitCode {
	var transcribed, already, waiting, short int
	ready := false
	language := ""
	for _, r := range recs {
		state, reason := r.state(check{verify: true, minMillis: o.minMillis, now: a.Env.Now()})
		switch state {
		case stateDone:
			already++
			continue
		case stateShort:
			short++
			continue
		case stateWaiting:
			waiting++
			a.say("waiting: " + r.dir() + " (" + reason + ")")
			continue
		case stateCorrupt:
			a.say(r.dir() + ": " + reason)
			return term.ExitFailure
		case statePending:
		}
		if !ready {
			var err error
			if language, err = a.language(ctx, o.language); err != nil {
				return term.ExitFailure
			}
			if err := a.ensureServer(ctx); err != nil {
				return term.ExitFailure
			}
			ready = true
		}
		if err := a.transcribe(ctx, r, language); err != nil {
			return term.ExitFailure
		}
		transcribed++
	}
	summary := fmt.Sprintf("transcribed %d, already done %d, waiting %d", transcribed, already, waiting)
	if short > 0 {
		summary += fmt.Sprintf(", too short %d (stt --short lists them)", short)
	}
	a.say(summary)
	return term.ExitOK
}

// language is S12: "" is detection.
func (a App) language(ctx context.Context, flag string) (string, error) {
	lang := flag
	if lang == "" {
		var stdout strings.Builder
		_, err := a.Env.Runner.Run(ctx, execx.Cmd{
			Name: filepath.Join(a.Env.BinDir, "wsi-setting"),
			Args: []string{languageKey}, Stdout: &stdout, Stderr: a.Env.Stderr,
		})
		if err != nil {
			a.say("cannot read the language setting (re-run play-speech-to-text.yml): " + err.Error())
			return "", errFailed
		}
		lang = strings.TrimSpace(stdout.String())
	}
	switch lang {
	case "auto", "":
		return "", nil
	case "system":
		locale := a.Env.Getenv("LANG")
		if locale == "" {
			locale = "en_GB.UTF-8"
		}
		return strings.SplitN(locale, "_", 2)[0], nil
	}
	return lang, nil
}

func (a App) ensureServer(ctx context.Context) error {
	cmd := execx.Cmd{
		Name: filepath.Join(a.Env.BinDir, "wsi-stream"), Args: []string{"--ensure-server"},
		Stderr: a.Env.Stderr,
	}
	if _, err := a.Env.Runner.Run(ctx, cmd); err != nil {
		a.say("the speech-to-text server could not be started (" + cmd.Line() + ": " + err.Error() + ")")
		return errFailed
	}
	return nil
}

// transcribe is one recording through the server, S1-S3, S10, S11 and S13.
func (a App) transcribe(ctx context.Context, r recording, language string) error {
	a.say(fmt.Sprintf("transcribing %s (%d %s)", r.dir(), len(r.parts), plural(len(r.parts), "part")))
	params := map[string]any{"paths": r.parts, languageKey: nil}
	if language != "" {
		params[languageKey] = language
	}
	reply, err := a.ask(ctx, "FILE_START", params)
	if err != nil {
		return a.fail(r, err.Error())
	}
	if reply.Status != "running" {
		return a.fail(r, "the speech-to-text server refused the recording: "+reply.Message)
	}
	if reply.Job == "" {
		return a.fail(r, "the speech-to-text server named no job (it is older than this stt; re-run play-speech-to-text.yml)")
	}
	job := reply.Job
	var segments []segment
	for {
		reply, err = a.ask(ctx, "FILE_PROGRESS", map[string]any{"job": job, "since": len(segments)})
		if err != nil {
			return a.fail(r, err.Error())
		}
		for _, s := range reply.Segments {
			a.say("  [" + clock(s.Start) + "] " + s.Text)
		}
		segments = append(segments, reply.Segments...)
		switch reply.Status {
		case "running":
			if err := a.Env.Sleep(ctx, pollInterval); err != nil {
				return a.fail(r, err.Error())
			}
			continue
		case "done":
		case "failed":
			return a.fail(r, "the speech-to-text server failed: "+reply.Error)
		default:
			return a.fail(r, "the speech-to-text server answered "+reply.Status+": "+reply.Message)
		}
		break
	}
	text := render(r, reply, segments, a.Env.Now())
	if err := fsx.WriteAtomic(r.sidecar, "transcript", []byte(text), 0o644); err != nil {
		return a.fail(r, err.Error())
	}
	return a.writeErr(a.Env.Stdout, r.sidecar+"\n")
}

// reply is the part of the server's answers this reads.
type reply struct {
	Status   string    `json:"status"`
	Job      string    `json:"job"`
	Message  string    `json:"message"`
	Error    string    `json:"error"`
	Segments []segment `json:"segments"`
	Duration float64   `json:"duration"`
	Language string    `json:"language"`
	Model    string    `json:"model"`
}

type segment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// ask sends one command and reads its one-line reply.
func (a App) ask(ctx context.Context, command string, params map[string]any) (reply, error) {
	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "unix", a.Env.Socket)
	if err != nil {
		return reply{}, fmt.Errorf("cannot reach the speech-to-text server at %s: %w", a.Env.Socket, err)
	}
	r, err := exchange(conn, command, params)
	if cerr := conn.Close(); cerr != nil && err == nil {
		err = fmt.Errorf("closing the connection to the speech-to-text server: %w", cerr)
	}
	return r, err
}

func exchange(conn net.Conn, command string, params map[string]any) (reply, error) {
	request, err := json.Marshal(map[string]any{"command": command, "params": params})
	if err != nil {
		return reply{}, fmt.Errorf("encoding %s: %w", command, err)
	}
	if _, err = conn.Write(append(request, '\n')); err != nil {
		return reply{}, fmt.Errorf("sending %s to the speech-to-text server: %w", command, err)
	}
	line, err := bufio.NewReader(io.LimitReader(conn, replyLimit)).ReadBytes('\n')
	if err != nil {
		return reply{}, fmt.Errorf("the speech-to-text server did not answer %s: %w", command, err)
	}
	var r reply
	if err := json.Unmarshal(line, &r); err != nil {
		return reply{}, fmt.Errorf("the speech-to-text server's answer to %s is not JSON: %w", command, err)
	}
	return r, nil
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func (a App) fail(r recording, reason string) error {
	a.say(r.dir() + ": " + reason)
	return errFailed
}

func (a App) write(w io.Writer, text string, code term.ExitCode) term.ExitCode {
	if _, err := io.WriteString(w, text); err != nil {
		return term.ExitFailure
	}
	return code
}

func (a App) writeErr(w io.Writer, text string) error {
	if _, err := io.WriteString(w, text); err != nil {
		return fmt.Errorf("writing the output: %w", err)
	}
	return nil
}

func (a App) say(message string) {
	if _, err := fmt.Fprintf(a.Env.Stderr, "%s: %s\n", tool, message); err != nil {
		return // stderr itself is gone: nowhere left to say it, and the exit code still does
	}
}
