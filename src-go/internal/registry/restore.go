package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/screen"
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
	keepFlags      = []string{"--no-ssh", "--github-443", "--no-network", flagSupervise, flagNoSupervise}
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

// RestoreArgs is the arguments a restore starts the launcher with: the recorded set, with
// `--compose start` (compose outcome started or running: `up -d` leaves running services alone)
// or `--compose skip` (declined) for ccy when the record holds an outcome, and `--supervise`
// for ccy unless the record already says --supervise or --no-supervise, then `--continue`
// unless the record already continues or resumes a conversation. cc forwards every argument to
// claude and has no compose question, and --compose and --supervise are ccy's flags, so cc
// gets --continue only. A record with no outcome gets no --compose: the launcher then asks.
//
// After a recorded `--` every word is claude's, so ccy's flags go in before it, and a
// --supervise after it is claude's word rather than ccy's. --continue is claude's flag and
// reaches claude from either side, so it is appended.
func RestoreArgs(compose, prefix string, args []string) ([]string, error) {
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
	hasSupervise, hasContinue := scanRecorded(args)
	if prefix == prefixCCY && !hasSupervise {
		ccyFlags = append(ccyFlags, flagSupervise)
	}
	out := make([]string, 0, len(args)+len(ccyFlags)+1)
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
	if !hasContinue {
		out = append(out, "--continue")
	}
	return out, nil
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

// scanRecorded says whether the recorded words already carry a supervise choice before any
// `--`, and whether they already continue or resume a conversation (from either side of it).
func scanRecorded(args []string) (bool, bool) {
	supervise, continues, afterDD := false, false, false
	for _, a := range args {
		if a == "--" {
			afterDD = true
			continue
		}
		switch a {
		case flagSupervise, flagNoSupervise:
			supervise = supervise || !afterDD
		case "--continue", "-c", "--resume", "-r":
			continues = true
		}
	}
	return supervise, continues
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
	return `"$@"; rc=$?; rm -f -- ` + shellQuote(recordPath) +
		`; if [ "$rc" -ne 0 ]; then printf '\n` + prefix + ` exited with status %s. ` + screen.EndedText +
		`\n' "$rc"; read -r; fi; exit "$rc"`
}

// shellQuote quotes s for a POSIX shell, leaving a plain path as it is.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_@%+=:./-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
