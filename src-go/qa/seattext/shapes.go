// Package seattext is the shape of a seat number a person is told to type or pick (Plan 00265
// R90: a person never needs a seat number, which only the machine knows), and the gate
// seat-number-text, which looks for it in the text outside Go: the docs, the embedded help and
// the launchers' messages. The Go rule project_seat_number_text (qa/rules) judges Go strings by
// the same Finding. scripts/qa-seat-text.bash builds and runs its command, qa-seat-text.
//
// The expectations, each held by a test of the same number:
//
//   - S1 Finding names each shape: a launch with the seat spelt out (--teams <seat>@, SEAT@,
//     2@, a filled-in seat), a hand-to=<seat> or a seats conversation / seats model naming a seat,
//     a seat placeholder (<seat>, SEAT@, S@T), a --teams filled by a value spelt as a seat or a
//     list, and in a choice's rows a row labelled "seat <name>" or showing a handle.
//   - S2 The command line is rootargs' (-h, --root DIR, --scripts DIR); anything else is exit 2
//     with the usage text.
//   - S3 The scope is what git would publish of: docs/**/*.md but the changelog
//     (docs/ccy-changelog.md, history), src-go/**/usage.txt and src-go/**/help.txt, and the
//     files under files/var/local/claude-yolo/ and files/var/local/claude-code/.
//   - S4 A doc or help line is judged whole. A handle's format (<repo>.<seat>+<host>) and a
//     path's directory (/<seat>/) describe what the machine names, not what a person types, and
//     are not findings.
//   - S5 A launcher line is judged only when it is a message: a print_error, echo or printf
//     line, or a line of a here-document. A whole-line comment is not; a shell variable on the
//     line is a filled-in value spelt as its name.
//   - S6 allowed.tsv lists the agent-facing and machine-facing lines that keep a seat (path,
//     literal, reason): a finding on a line of the path that holds the literal is allowed. A row
//     that allows nothing, or allows lines more than one, is a finding.
//   - S7 A tree with no finding passes: exit 0, one line on stdout. Each finding is
//     path:line: what: text on stderr, and exit 1. A run that cannot judge is exit 2: git
//     failing, no file in scope, the allowlist missing or malformed, a file unreadable.
package seattext

import (
	"regexp"
	"strings"
)

// Value stands, in a template, for a value the program fills in: a Go operand, or a shell
// variable on a launcher's message line.
const Value = "\x00"

// The findings Finding names.
const (
	FindingLaunch      = "a launch that spells out the seat"
	FindingRepair      = "a command that names the seat"
	FindingPlaceholder = "a seat placeholder a person is told to fill"
	FindingList        = "a launch with its seat list filled in"
	FindingRow         = "a choice's row labelled by its seat"
	FindingHandleRow   = "a choice's row that shows a handle, which carries the seat"
)

// Fix is the way on every finding names.
const Fix = "a person never needs a seat number; name the command by team (ccy --teams <team>, cc --teams <team>, ccy-teams) and the seat by its role and what it last ran"

// The shapes of text that has a person type or pick a seat, matched on a template.
var (
	// A launch with the seat spelt out: --teams <seat>@, SEAT@, 2@, dev1@, or a filled-in seat.
	launchRe = regexp.MustCompile(`--teams[ =]+(?:<[^>]*>|[A-Za-z0-9_-]+|` + Value + `)@`)
	// A seat placeholder: <seat>, SEAT@TEAM, S@T.
	placeholderRe = regexp.MustCompile(`<seat>|\bSEAT@|\bS@T\b`)
	// A hand-over told as a seat to type, or a seat repair that names the seat. A hand-to= filled
	// in is the person's own value echoed back, for diagnosis.
	repairRe = regexp.MustCompile(`hand-to=(?:<[^>]*>|\d+)|seats (?:conversation|model)\b[^\n]*(?:<[^>]*>|\d+|` + Value + `)@`)
	// A launch (ccy, cc, or a launcher filled in) whose list is filled in, judged by what fills
	// it; a bare --teams is a record's words, not a command.
	listRe = regexp.MustCompile(`(?:(?:\bccy|\bcc|` + Value + `) --teams[ =]+|seats (?:conversation|model) )` + Value)
	// A row that labels a seat by its name.
	rowRe = regexp.MustCompile(`(?i)\bseat ` + Value)
	// A value that is a seat list, a seat or a handle, by its spelling.
	listOperandRe = regexp.MustCompile(`(?i)seat|ref|list|canonical|handle|member`)
	// A value that reads a handle.
	handleOperandRe = regexp.MustCompile(`\.Handle\b`)
)

// Finding is the finding for a template whose filled-in values are spelt as operands, in order;
// rows is whether the text is a choice's row. "" is clean text.
func Finding(tmpl string, operands []string, rows bool) string {
	switch {
	case launchRe.MatchString(tmpl):
		return FindingLaunch
	case repairRe.MatchString(tmpl):
		return FindingRepair
	case placeholderRe.MatchString(tmpl):
		return FindingPlaceholder
	case listFilled(tmpl, operands):
		return FindingList
	case rows && rowRe.MatchString(tmpl):
		return FindingRow
	case rows && anyHandle(operands):
		return FindingHandleRow
	}
	return ""
}

// listFilled is whether a --teams in tmpl is filled by a value spelt as a seat or a list.
func listFilled(tmpl string, operands []string) bool {
	for _, loc := range listRe.FindAllStringIndex(tmpl, -1) {
		i := strings.Count(tmpl[:loc[1]-1], Value)
		if i < len(operands) && listOperandRe.MatchString(operands[i]) {
			return true
		}
	}
	return false
}

func anyHandle(operands []string) bool {
	for _, o := range operands {
		if handleOperandRe.MatchString(o) {
			return true
		}
	}
	return false
}
