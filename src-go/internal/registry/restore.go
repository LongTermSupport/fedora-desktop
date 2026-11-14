package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/screen"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/shellword"
)

const (
	prefixCCY       = "ccy"
	flagSupervise   = "--supervise"
	flagNoSupervise = "--no-supervise"
	flagCompose     = "--compose"
	// ccy's --opencode and its short spelling (Plan 00215).
	flagOpencode      = "--opencode"
	flagOpencodeShort = "-oc"
)

// Class is what a restore does with one word of a recorded launch.
type Class string

// The classes of a launcher word, as lib/session-registry.bash names them.
const (
	// ClassDrop is a flag that is not replayed.
	ClassDrop Class = "drop"
	// ClassDropValue is a flag that is not replayed, with the word after it.
	ClassDropValue Class = "drop-value"
	// ClassKeepValue is a flag that is replayed with the word after it.
	ClassKeepValue Class = "keep-value"
	// ClassKeep is a flag that is replayed.
	ClassKeep Class = "keep"
	// ClassUnknown is a word the launcher's flag list does not name.
	ClassUnknown Class = "unknown"
)

// The four flag groups are one list with the launcher's parser: a value-taking flag consumes the
// NEXT word whatever it is.
var (
	dropFlags      = []string{"--rebuild", "--create-token", "--list-tokens", "--token-usage", "--custom", "--custom-docker", "--top", "--prevent", "--debug", "--headless", "--disable-custom-docker", "--ssh-agent", flagNoRestore, "--list-containers", "--stop-containers", "--help", "--version", "-h", "-v", "--build-lock", "--read-launch-config", "--check-token", "--check-network", "--claude-versions", "--remove-network", "--stop", "--secret-screen"}
	dropValueFlags = []string{"--update-token", "--export-token", "--connect", "--disconnect", "--prompt", "--run-for", "--until", flagCompose}
	keepValueFlags = []string{"--token", "--ssh-key", "--network", "--engine", "--max-age", "--teams"}
	keepFlags      = []string{"--no-ssh", "--github-443", "--no-network", flagSupervise, flagNoSupervise, "--no-bus", flagOpencode, flagOpencodeShort}
)

// FlagClass classifies one launcher word.
func FlagClass(word string) Class {
	switch {
	case strings.HasPrefix(word, "--rebuild="), strings.HasPrefix(word, "--update-token="):
		return ClassDrop
	case strings.HasPrefix(word, "--teams="):
		return ClassKeep
	case slices.Contains(dropFlags, word):
		return ClassDrop
	case slices.Contains(dropValueFlags, word):
		return ClassDropValue
	case slices.Contains(keepValueFlags, word):
		return ClassKeepValue
	case slices.Contains(keepFlags, word):
		return ClassKeep
	}
	return ClassUnknown
}

// sessionID is a Claude Code session id: a UUID (lib/restart-request.bash's
// CCY_RESTART_SESSION_ID_PATTERN).
var sessionID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// SessionID is whether id is a Claude Code session id.
func SessionID(id string) bool { return sessionID.MatchString(id) }

// ResumeArgs is the arguments a restore or a team restart starts the launcher with, to resume
// the conversation id (Plan 00203 SPEC-team-add-and-restart C5): the recorded set with every
// conversation selector removed (-c, --continue, -r/--resume and their value, --session-id and
// its value, from either side of `--`, as the supervisor's relaunch does in
// lib/restart-request.bash), `--compose start` (compose outcome started or running: `up -d`
// leaves running services alone) or `--compose skip` (declined) for ccy when the record holds
// an outcome, `--supervise` for ccy unless the record already says --supervise or
// --no-supervise, then one `--resume <id>`. Never --continue: it takes the newest conversation
// in a transcript directory that several sessions share, and refuses one a stale background
// record holds (the continue-trap investigation). cc forwards every argument to claude and has
// no compose question, and --compose and --supervise are ccy's flags, so cc gets the resume
// only. A record with no outcome gets no --compose: the launcher then asks.
//
// After a recorded `--` every word is claude's, so ccy's flags go in before it, and a
// --supervise after it is claude's word rather than ccy's. --resume is claude's flag and
// reaches claude from either side, so it is appended.
func ResumeArgs(compose, prefix string, args []string, id string) ([]string, error) {
	if !SessionID(id) {
		return nil, fmt.Errorf("refusing to resume '%s': it is not a Claude session id", id)
	}
	out, err := FreshArgs(compose, prefix, args)
	if err != nil {
		return nil, err
	}
	return append(out, "--resume", id), nil
}

// FreshArgs is ResumeArgs without the resume: the arguments a restore starts a record marked
// fresh-if-no-conversation with when no conversation is recorded (Plan 00135 Phase 13). Every
// conversation selector is removed, so the session starts a new conversation, never the newest
// in the folder.
func FreshArgs(compose, prefix string, args []string) ([]string, error) {
	if prefix == "" {
		return nil, errors.New("restore arguments need a launcher prefix")
	}
	var ccyFlags []string
	if prefix == prefixCCY && compose != "" {
		composeFlags, err := composeArgs(compose)
		if err != nil {
			return nil, err
		}
		ccyFlags = composeFlags
	}
	args = withoutSelectors(prefix, args)
	if prefix == prefixCCY && !recordedSupervise(args) {
		ccyFlags = append(ccyFlags, flagSupervise)
	}
	out := make([]string, 0, len(args)+len(ccyFlags)+2)
	afterDD := false
	for _, a := range args {
		if a == "--" && !afterDD {
			afterDD = true
			out = append(out, ccyFlags...)
			ccyFlags = nil
		}
		out = append(out, a)
	}
	return append(out, ccyFlags...), nil
}

// withoutSelectors is args with claude's conversation selectors removed. Before `--` a ccy flag
// that takes a value consumes the next word, as the launcher's parser does, so `--token -c`
// keeps its token; cc has no flags of its own. A bare --resume or -r (the picker) takes the
// next word only when it is not a flag.
func withoutSelectors(prefix string, args []string) []string {
	out := make([]string, 0, len(args))
	keepNext, dropNext, resumeValue, afterDD := false, false, false, false
	for _, w := range args {
		if resumeValue {
			resumeValue = false
			dropNext = !strings.HasPrefix(w, "-")
		}
		switch {
		case dropNext:
			dropNext = false
			continue
		case keepNext:
			keepNext = false
			out = append(out, w)
			continue
		case w == "--" && !afterDD:
			afterDD = true
			out = append(out, w)
			continue
		case prefix == prefixCCY && !afterDD && takesValue(w):
			keepNext = true
			out = append(out, w)
			continue
		}
		switch selectorOf(w) {
		case selectorAlone:
		case selectorMaybeValue:
			resumeValue = true
		case selectorWithValue:
			dropNext = true
		case selectorNone:
			out = append(out, w)
		}
	}
	return out
}

// takesValue is whether a ccy flag consumes the next word.
func takesValue(w string) bool {
	c := FlagClass(w)
	return c == ClassKeepValue || c == ClassDropValue
}

// selector is what a word is as one of claude's conversation selectors.
type selector int

const (
	selectorNone       selector = iota // not a selector
	selectorAlone                      // -c, --continue, --resume=<id>, --session-id=<id>
	selectorMaybeValue                 // -r, --resume: the next word is its id unless it is a flag
	selectorWithValue                  // --session-id: the next word is its id
)

func selectorOf(w string) selector {
	switch {
	case w == "-c", w == "--continue", strings.HasPrefix(w, "--resume="), strings.HasPrefix(w, "--session-id="):
		return selectorAlone
	case w == "-r", w == "--resume":
		return selectorMaybeValue
	case w == "--session-id":
		return selectorWithValue
	}
	return selectorNone
}

// composeArgs is the --compose flag a compose outcome maps to.
func composeArgs(outcome string) ([]string, error) {
	switch outcome {
	case "started", "running":
		return []string{flagCompose, "start"}, nil
	case "declined":
		return []string{flagCompose, "skip"}, nil
	}
	return nil, fmt.Errorf("compose outcome '%s' is not one of %s", outcome, strings.Join(composeOutcomes, " "))
}

// recordedSupervise says whether the recorded words already carry a supervise choice before
// any `--`.
func recordedSupervise(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == flagSupervise || a == flagNoSupervise {
			return true
		}
	}
	return false
}

// ResumeID is the conversation id the launch resumes: the value of --resume <id>, --resume=<id>
// or -r <id>; empty for --continue or no flag. A ccy flag that takes a value consumes the next
// word, as the launcher's parser does, so `--token -r` names a token, not a resume.
func ResumeID(args []string) string {
	id, skip, afterDD := "", false, false
	for i := 0; i < len(args); i++ {
		word := args[i]
		switch {
		case skip:
			skip = false
		case word == "--":
			afterDD = true
		case strings.HasPrefix(word, "--resume="):
			id = strings.TrimPrefix(word, "--resume=")
		case word == "--resume", word == "-r":
			id = ""
			if i+1 < len(args) {
				i++
				id = args[i]
			}
		case !afterDD:
			skip = FlagClass(word) == ClassKeepValue || FlagClass(word) == ClassDropValue
		}
	}
	return id
}

// Opencode reports whether a ccy record's launch ran opencode instead of Claude Code: ccy's
// --opencode or -oc (Plan 00215), an option of ccy's own, so only before a bare "--". Such a
// session carries no bus kit and no supervisor, and has no Claude conversation to resume.
func Opencode(prefix string, args []string) bool {
	if prefix != prefixCCY {
		return false
	}
	for _, word := range args {
		switch word {
		case "--":
			return false
		case flagOpencode, flagOpencodeShort:
			return true
		}
	}
	return false
}

// Supervised is whether a ccy supervisor carries the session on after a compaction. Only ccy
// has one; it is on unless the launch says --no-supervise (the last of --supervise and
// --no-supervise before `--` wins, as in the launcher's flat loop).
func Supervised(prefix string, args []string) bool {
	if prefix != prefixCCY {
		return false
	}
	answer := true
	for _, word := range args {
		if word == "--" {
			break
		}
		switch word {
		case flagSupervise:
			answer = true
		case flagNoSupervise:
			answer = false
		}
	}
	return answer
}

// ExitUnattendedStop is the launcher's status when a launch nobody attends (a restore, a
// restart's relaunch, a headless launch) stops where only a person could go on
// (CCY_EXIT_UNATTENDED_STOP, ccy_unattended_stop in lib/unattended-stop.bash). No session
// started, so the trampoline keeps its record, and the launcher has written why to the
// session's stop file (StopFile).
const ExitUnattendedStop = 88

// StopEnv names the stop file a launcher is given by its trampoline.
const StopEnv = "CCY_UNATTENDED_STOP_FILE"

// stoppedDir is the directory beside the registry that holds the stop files.
const stoppedDir = "stopped"

// stopPath is the stop file of the record at recordPath, spelled as the trampoline spells it:
// <registry>/../stopped/<session>, beside the registry, never in it (every file in the registry
// is read as a record).
func stopPath(recordPath string) string {
	return filepath.Dir(recordPath) + "/../" + stoppedDir + "/" + filepath.Base(recordPath)
}

// StopFile is the file holding why the latest start of the session named in the registry regdir
// stopped where nobody could answer: "<reason> <sentence>", written by the launcher, cleared by
// the trampoline when the session is next started.
func StopFile(regdir, name string) string {
	return filepath.Join(filepath.Dir(regdir), stoppedDir, name)
}

// stopReason is a stop file's reason word: lower-case letters, digits and hyphens.
var stopReason = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Stop is why a session's latest start stopped where nobody could answer.
type Stop struct {
	Reason   string // one word, as verify-restore prints it
	Sentence string // what a person must do
}

// ErrBadStop is a stop file that is not "<reason> <sentence>" on one line.
var ErrBadStop = errors.New("is not one line of a reason word and a sentence")

// ReadStop reads the session's stop file: found false when there is none.
func ReadStop(regdir, name string) (Stop, bool, error) {
	file := StopFile(regdir, name)
	raw, err := os.ReadFile(filepath.Clean(file))
	if errors.Is(err, os.ErrNotExist) {
		return Stop{}, false, nil
	}
	if err != nil {
		return Stop{}, false, fmt.Errorf("could not read the stop file %s: %w", file, err)
	}
	line, rest, _ := strings.Cut(string(raw), "\n")
	reason, sentence, _ := strings.Cut(line, " ")
	if !stopReason.MatchString(reason) || strings.TrimSpace(rest) != "" {
		return Stop{}, false, fmt.Errorf("the stop file %s %w", file, ErrBadStop)
	}
	return Stop{Reason: reason, Sentence: sentence}, true, nil
}

// Trampoline is the `bash -c` string a session's tmux pane runs. It clears the session's stop
// file, runs the launcher (the pane's remaining arguments) with StopEnv naming it, removes the
// record the moment the launcher RETURNS (a returned launcher is an ended session, whatever its
// status) and, on a non-zero status, holds the window so the error can be read. The removal is
// here and nowhere else, because this is the one place that runs after the session's command
// and does not run when the session is killed: a reboot kills the pane's bash before it gets
// here, and the record stays. That asymmetry is the whole feature. The one returned launcher
// that is not an ended session is ExitUnattendedStop: its record is kept, and the held line says
// so (screen.UnattendedStopText).
func Trampoline(recordPath, prefix string) string {
	name := formatInSingleQuotes(prefix)
	stop := shellword.Quote(stopPath(recordPath))
	return `rm -f -- ` + stop + `; ` + StopEnv + `=` + stop + ` "$@"; rc=$?; if [ "$rc" -eq ` + strconv.Itoa(ExitUnattendedStop) +
		` ]; then printf '\n` + name + ` ` + screen.UnattendedStopText + ` ` + screen.EndedText + `\n'; read -r; exit "$rc"; fi; rm -f -- ` +
		shellword.Quote(recordPath) + `; if [ "$rc" -ne 0 ]; then printf '\n` + name + ` exited with status %s. ` + screen.EndedText +
		`\n' "$rc"; read -r; fi; exit "$rc"`
}

// formatInSingleQuotes makes s safe to splice into a single-quoted printf format: printf's own
// escapes (backslash, percent) are doubled so it prints s literally, then the single quote,
// the one character a single-quoted shell word cannot hold, is closed, escaped and reopened.
func formatInSingleQuotes(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", "%%")
	return strings.ReplaceAll(s, "'", `'\''`)
}

// Problem is a record file that could not be read, with the reader's reason.
type Problem struct {
	File string
	Err  error
}

// Reading is every record in the registry, with the reason each unreadable one was left out.
type Reading struct {
	Records  []Record
	Problems []Problem
}

// ReadAll reads every record in byte order of file name, skipping writes in flight, and keeps
// the reason each unreadable record failed, so a restore can report it and count it as a
// failure. An absent registry directory is an empty registry.
func (s *Store) ReadAll() (Reading, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return Reading{}, nil
	}
	if err != nil {
		return Reading{}, fmt.Errorf("could not list the session registry %s: %w", s.dir, err)
	}
	var out Reading
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			continue
		}
		file := filepath.Join(s.dir, e.Name())
		rec, readErr := ReadFile(file)
		if readErr != nil {
			out.Problems = append(out.Problems, Problem{File: file, Err: readErr})
			continue
		}
		out.Records = append(out.Records, rec)
	}
	return out, nil
}
