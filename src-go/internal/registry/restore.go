package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/screen"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/shellword"
)

const (
	prefixCCY       = "ccy"
	flagSupervise   = "--supervise"
	flagNoSupervise = "--no-supervise"
	flagCompose     = "--compose"
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
	dropFlags      = []string{"--rebuild", "--create-token", "--list-tokens", "--token-usage", "--custom", "--custom-docker", "--top", "--prevent", "--debug", "--headless", "--disable-custom-docker", "--ssh-agent", "--no-restore", "--help", "--version", "-h", "-v"}
	dropValueFlags = []string{"--update-token", "--export-token", "--connect", "--disconnect", "--prompt", "--run-for", "--until", flagCompose}
	keepValueFlags = []string{"--token", "--ssh-key", "--network", "--engine", "--max-age", "--teams"}
	keepFlags      = []string{"--no-ssh", "--github-443", "--no-network", flagSupervise, flagNoSupervise, "--no-bus"}
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
	if prefix == "" {
		return nil, errors.New("restore arguments need a launcher prefix")
	}
	if !SessionID(id) {
		return nil, fmt.Errorf("refusing to resume '%s': it is not a Claude session id", id)
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
	out = append(out, ccyFlags...)
	return append(out, "--resume", id), nil
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

// Trampoline is the `bash -c` string a session's tmux pane runs. It runs the launcher (the
// pane's remaining arguments), removes the record the moment the launcher RETURNS (a returned
// launcher is an ended session, whatever its status) and, on a non-zero status, holds the window
// so the error can be read. The removal is here and nowhere else, because this is the one place
// that runs after the session's command and does not run when the session is killed: a reboot
// kills the pane's bash before it gets here, and the record stays. That asymmetry is the whole
// feature.
func Trampoline(recordPath, prefix string) string {
	return `"$@"; rc=$?; rm -f -- ` + shellword.Quote(recordPath) +
		`; if [ "$rc" -ne 0 ]; then printf '\n` + formatInSingleQuotes(prefix) + ` exited with status %s. ` + screen.EndedText +
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
