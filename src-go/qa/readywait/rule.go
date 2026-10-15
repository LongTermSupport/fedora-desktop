package readywait

import (
	"regexp"
	"slices"
	"strings"
)

// RuleID names the rule in every finding and in the fixture's annotations.
const RuleID = "ready-wait-ignores-child-exit"

// The patterns the rule reads. RE2 has no look-around, so the word boundaries that need it
// (what may stand before and after a word) are the functions wordsIn and callsIn.
const (
	// argsGap is the words of one simple command between its program and the word asked for.
	argsGap = `(?:[ \t]+[^;&|\n \t]+)*?[ \t]+`
	// endClass is what may follow a word.
	endClass = `(?:$|[\s;)&|<>])`
)

var (
	livenessRe = regexp.MustCompile(`\bkill\s+(?:-0|-s\s+0|--signal\s+0)\s|\bwait\s+-n\b|\bps\s+-p\b` +
		`|\blxc-info` + argsGap + `(?:-[A-Za-z]*s[A-Za-z]*|--state)` + endClass +
		`|\blxc-ls` + argsGap + `--(?:running|active)` + endClass +
		`|\bvirsh` + argsGap + `(?:domstate|list)` + endClass)
	managerStartRe = regexp.MustCompile("(?m)(?:^|[;&|(!{`]|\\b(?:then|do|else|elif|if|while|until)\\b)[ \\t]*" +
		`(?:(?:!|exec|nohup|sudo(?:[ \t]+-[\w-]+(?:[ \t]+[^-\s;&|][^\s;&|]*)?)*|[A-Za-z_]\w*=[^ \t;&|]*)[ \t]+)*` +
		`(?P<start>lxc-start|virt-install|virsh` + argsGap + `start)` + endClass)
	fixtureRe  = regexp.MustCompile(`^\s*#\s*ruleid:\s*` + regexp.QuoteMeta(RuleID) + `\s*$`)
	notWaitNRe = regexp.MustCompile(`^\s+-n\b`)
)

var startGroup = managerStartRe.SubexpIndex("start")

// isSpace is what \s matches.
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

// isSep is what may stand before a word: whitespace or one of ;&|(!{.
func isSep(c byte) bool { return isSpace(c) || strings.IndexByte(";&|(!{", c) >= 0 }

// isEnd is what may follow a word: whitespace or one of ;)&|<>.
func isEnd(c byte) bool { return isSpace(c) || strings.IndexByte(";)&|<>", c) >= 0 }

// atWordStart is that a word may begin at i: the start of the text, or after a separator.
func atWordStart(s string, i int) bool { return i == 0 || isSep(s[i-1]) }

// atWordEnd is that a word ending at i is followed by an end: the limit, or an end character.
func atWordEnd(s string, i, hi int) bool { return i >= hi || isEnd(s[i]) }

// word is one shell word found in the text.
type word struct {
	text string
	pos  int
}

// wordsIn lists the words of s[lo:hi] that begin after a separator and run to an end character.
func wordsIn(s string, lo, hi int) []word {
	var found []word
	for i := lo; i < hi; i++ {
		if !atWordStart(s, i) || isEnd(s[i]) {
			continue
		}
		j := i
		for j < hi && !isEnd(s[j]) {
			j++
		}
		found = append(found, word{s[i:j], i})
		i = j - 1
	}
	return found
}

// positionsOf lists where the exact word name stands in s[lo:hi], not overlapping.
func positionsOf(s string, lo, hi int, name string) []int {
	var found []int
	for i := lo; i+len(name) <= hi; i++ {
		if atWordStart(s, i) && s[i:i+len(name)] == name && atWordEnd(s, i+len(name), hi) {
			found = append(found, i)
			i += len(name) - 1
		}
	}
	return found
}

// anyOf says whether s[lo:hi] holds one of the words.
func anyOf(s string, lo, hi int, words ...string) bool {
	for _, w := range wordsIn(s, lo, hi) {
		if slices.Contains(words, w.text) {
			return true
		}
	}
	return false
}

// launchesIn lists the positions of a lone & (not &&, &>, >&, <&, |&) in s[lo:hi].
func launchesIn(s string, lo, hi int) []int {
	var found []int
	for i := lo; i < hi; i++ {
		if s[i] != '&' {
			continue
		}
		if i > 0 && strings.IndexByte("&|<>", s[i-1]) >= 0 {
			continue
		}
		if i+1 < hi && (s[i+1] == '&' || s[i+1] == '>') {
			continue
		}
		found = append(found, i)
	}
	return found
}

// isWait is that the loop at header waits for something rather than sampling: it leaves from its
// body (break, return, exit), or it is a while or until whose condition is a test. A for with
// none of those, or a while true or while ((...)) without them, runs its full course.
func isWait(code string, header, body, done int) bool {
	if anyOf(code, body, done, "break", "return", "exit") {
		return true
	}
	if strings.HasPrefix(code[header:], "for") {
		return false
	}
	const keywordLen = len("while")
	condition := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(code[header+keywordLen:body-2]), ";"))
	return condition != "true" && condition != ":" && !strings.HasPrefix(condition, "((")
}

// unguardedPolls lists the headers of the waits in code[start:end] that sleep between tries and
// never ask whether a process is alive, themselves or through a call to one of the checkers
// (functions whose bodies ask).
func unguardedPolls(code string, start, end int, checkers []string) []int {
	var headers []int
	type open struct{ header, body int }
	var opens []open
	var found []int
	for _, w := range wordsIn(code, start, end) {
		switch w.text {
		case "for", "while", "until":
			headers = append(headers, w.pos)
		case "do":
			if len(headers) > 0 {
				opens = append(opens, open{headers[len(headers)-1], w.pos + 2})
				headers = headers[:len(headers)-1]
			}
		case "done":
			if len(opens) == 0 {
				continue
			}
			loop := opens[len(opens)-1]
			opens = opens[:len(opens)-1]
			whole := code[loop.header : w.pos+len("done")]
			if anyOf(code, loop.body, w.pos, "sleep") &&
				isWait(code, loop.header, loop.body, w.pos) &&
				!backgrounded(code, w.pos+len("done")) &&
				!livenessRe.MatchString(whole) &&
				!callsAny(whole, checkers) {
				found = append(found, loop.header)
			}
		}
	}
	return found
}

// callsAny says whether text calls one of the functions by name.
func callsAny(text string, names []string) bool {
	for _, name := range names {
		if len(positionsOf(text, 0, len(text), name)) > 0 {
			return true
		}
	}
	return false
}

// eventKind is what happens at a position of a scope.
type eventKind int

const (
	evLaunch eventKind = iota
	evDisarm
	evLoop
	evCall
)

type event struct {
	pos  int
	kind eventKind
	name string
}

// managerStarts lists where a manager start stands in code[start:end].
func managerStarts(code string, start, end int) []int {
	var found []int
	window := code[start:end]
	for from := 0; from <= len(window); {
		m := managerStartRe.FindStringSubmatchIndex(window[from:])
		if m == nil {
			break
		}
		at := from + m[2*startGroup]
		found = append(found, start+at)
		from = at + 1
	}
	return found
}

// disarms lists where the child is waited for or a case arm ends in code[start:end].
func disarms(code string, start, end int) []int {
	var found []int
	for _, w := range wordsIn(code, start, end) {
		if w.text == "wait" && !notWaitNRe.MatchString(code[w.pos+len("wait"):end]) {
			found = append(found, w.pos)
		}
	}
	for i := start; i+1 < end; i++ {
		if code[i] == ';' && code[i+1] == ';' {
			found = append(found, i)
			i++
		}
	}
	return found
}

// backgrounded is that the command running on from pos is started with &: a function run in the
// background is a start, not a call whose loops the caller waits in.
func backgrounded(code string, pos int) bool {
	line := code[pos:]
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	cut := len(line)
	for _, sep := range []string{";", "&&", "||"} {
		if i := strings.Index(line, sep); i >= 0 && i < cut {
			cut = i
		}
	}
	piece := line[:cut] + " "
	return len(launchesIn(piece, 0, len(piece))) > 0
}

// FixtureMarks is the lines a fixture marks as must-fire: each line after a ruleid comment.
func FixtureMarks(text string) []int {
	var marks []int
	for number, line := range strings.Split(text, "\n") {
		if fixtureRe.MatchString(line) {
			marks = append(marks, number+2)
		}
	}
	return marks
}
