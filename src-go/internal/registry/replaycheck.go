package registry

import (
	"fmt"
	"strings"
)

// ReplayWordError is a word of a record that a restore will not replay: Word, and Why it is not
// a word a launch of the record's prefix records.
type ReplayWordError struct {
	Prefix, Word, Why string
}

func (e *ReplayWordError) Error() string {
	return fmt.Sprintf("its recorded arguments hold '%s', which %s", e.Word, e.Why)
}

// ReplayRefusal is why a restore will not replay rec's arguments, or nil (Plan 00266 Task A.5).
// They are judged as a restore starts them, with the conversation selectors removed
// (withoutSelectors), so restore, restore --check and declare agree.
//
//   - cc forwards every word but its own to claude on the host, so a cc record replays only
//     --bus and --teams <seats> (or =<seats>), and claude's --model <name> (or =<name>) and
//     --verbose, which load no code, configuration or prompt: any other word, `--` included,
//     is refused.
//   - ccy forwards a word it does not know, and every word after `--`, to the claude inside its
//     container, so those replay. A flag ccy acts on but a launch never records (the drop
//     classes of FlagClass: a token export, a network removal, a custom image build, --compose,
//     which the record's compose= answer gives) is refused, as is a value-taking flag with no
//     value and a bare word in a place no launch records one (ReplayArgs keeps a bare word only
//     as the value of the flag before it).
//
// A record is a file any process of the user can write, so a word is refused by name rather
// than dropped: the person declares the session again with the words it should have.
func ReplayRefusal(rec Record) error {
	args := withoutSelectors(rec.Prefix, rec.Args)
	if rec.Prefix == prefixCC {
		return ccReplayRefusal(args)
	}
	return ccyReplayRefusal(args)
}

// ccClaudeModel and ccClaudeVerbose are the claude words a cc record may replay: they choose a
// model and the output's detail, and load no code, configuration or prompt.
const (
	ccClaudeModel   = "--model"
	ccClaudeVerbose = "--verbose"
)

func ccReplayRefusal(args []string) error {
	refuse := func(word, why string) error { return &ReplayWordError{Prefix: prefixCC, Word: word, Why: why} }
	for i := 0; i < len(args); i++ {
		switch w := args[i]; {
		case w == flagBus, w == ccClaudeVerbose, hasValue(w, flagTeams), hasValue(w, ccClaudeModel):
		case w == flagTeams, w == ccClaudeModel:
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return refuse(w, "needs a value after it")
			}
			i++
		default:
			return refuse(w, "cc would pass to claude on this machine; a cc session is restored with only its own --bus and --teams, and claude's --model and --verbose")
		}
	}
	return nil
}

// hasValue is whether w is flag=<something>.
func hasValue(w, flag string) bool { return strings.HasPrefix(w, flag+"=") && len(w) > len(flag)+1 }

func ccyReplayRefusal(args []string) error {
	refuse := func(word, why string) error { return &ReplayWordError{Prefix: prefixCCY, Word: word, Why: why} }
	valueSlot := false
	for i := 0; i < len(args); i++ {
		w := args[i]
		if w == "--" {
			return nil
		}
		class, slot := FlagClass(w), valueSlot
		valueSlot = false
		switch class {
		case ClassKeep:
		case ClassKeepValue:
			if i+1 >= len(args) {
				return refuse(w, "needs a value after it")
			}
			i++
		case ClassDrop, ClassDropValue:
			return refuse(w, "ccy acts on at launch and no launch records")
		case ClassUnknown:
			isFlag := strings.HasPrefix(w, "-")
			if !isFlag && !slot {
				return refuse(w, "is a word no launch records in that place (a first message is never replayed)")
			}
			valueSlot = isFlag
		}
	}
	return nil
}
