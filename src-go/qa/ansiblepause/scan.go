package ansiblepause

import (
	"regexp"
	"strings"
)

// Finding is one pause task that waits with no register and no reason: the line of its pause key.
type Finding struct{ Line int }

var (
	// pauseKey is a line that opens a pause: optionally a list dash, then the module name.
	pauseKey = regexp.MustCompile(`^ *(- )?(?:ansible\.(?:builtin|legacy)\.)?pause:(?:[ \t]|$)`)
	// pauseOK is the annotation: a comment naming a reason (at least one character after the colon).
	pauseOK = regexp.MustCompile(`#[ \t]*PAUSE-OK:[ \t]*[^ \t]`)
	// dashPrefix is the list dash and its spaces at the start of a line.
	dashPrefix = regexp.MustCompile(`^ *- `)
)

// lineSet is the lines of one file, a trailing carriage return dropped.
type lineSet []string

func newLineSet(text string) lineSet {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// indent is the count of leading spaces; a tab is not indent.
func indent(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

func isBlank(s string) bool { return strings.Trim(s, " \t") == "" }

func isComment(s string) bool { return strings.HasPrefix(strings.TrimLeft(s, " \t"), "#") }

// Scan returns the pause tasks in a YAML text that have neither a register nor a PAUSE-OK reason,
// in line order.
func Scan(text string) []Finding {
	ls := newLineSet(text)
	var findings []Finding
	for i, line := range ls {
		m := pauseKey.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		tk, found := ls.task(i, m[1] != "")
		if !found {
			continue
		}
		tk.stop = ls.end(tk.start, tk.dashIndent)
		if !ls.registered(tk) && !ls.annotated(tk) {
			findings = append(findings, Finding{Line: i + 1})
		}
	}
	return findings
}

// task is the extent of one pause task: the line that heads it, the indent of that head, the
// indent of the task's own keys, and the line after its last.
type task struct{ start, dashIndent, keyIndent, stop int }

// task locates the task of the pause key on line i. false when a key not on a dash line has no head.
func (ls lineSet) task(i int, onDash bool) (task, bool) {
	keyIndent := indent(ls[i])
	if onDash {
		return task{start: i, dashIndent: keyIndent, keyIndent: keyIndent + 2}, true
	}
	for j := i - 1; j >= 0; j-- {
		if isBlank(ls[j]) || isComment(ls[j]) {
			continue
		}
		if indent(ls[j]) < keyIndent {
			return task{start: j, dashIndent: indent(ls[j]), keyIndent: keyIndent}, true
		}
	}
	return task{}, false
}

// end is the line after the task: the next non-blank line at or left of the head's indent.
func (ls lineSet) end(start, dashIndent int) int {
	for j := start + 1; j < len(ls); j++ {
		if !isBlank(ls[j]) && indent(ls[j]) <= dashIndent {
			return j
		}
	}
	return len(ls)
}

// registered is whether the task has a register key of its own, at the task's key indent.
func (ls lineSet) registered(tk task) bool {
	start, keyIndent := tk.start, tk.keyIndent
	for j := start; j < tk.stop; j++ {
		text := strings.TrimLeft(ls[j], " ")
		if j == start {
			text = dashPrefix.ReplaceAllString(ls[j], "")
		}
		if strings.HasPrefix(text, "register:") && (j == start || indent(ls[j]) == keyIndent) {
			return true
		}
	}
	return false
}

// annotated is whether a PAUSE-OK reason sits on a line of the task or in the unbroken run of
// comment lines directly above its head.
func (ls lineSet) annotated(tk task) bool {
	start := tk.start
	for j := start; j < tk.stop; j++ {
		if pauseOK.MatchString(ls[j]) {
			return true
		}
	}
	for j := start - 1; j >= 0 && isComment(ls[j]); j-- {
		if pauseOK.MatchString(ls[j]) {
			return true
		}
	}
	return false
}
