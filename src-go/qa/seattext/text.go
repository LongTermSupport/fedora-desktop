package seattext

import (
	"path"
	"regexp"
	"strings"
)

// Kind is how a file in scope is read.
type Kind int

// The kinds of file: out of scope, text judged line by line, a launcher judged on its messages.
const (
	KindNone Kind = iota
	KindText
	KindLauncher
)

// ScanPaths are the trees git is asked to list.
func ScanPaths() []string {
	return []string{"docs", "src-go", "files/var/local/claude-yolo", "files/var/local/claude-code"}
}

// InScope is how p, a slash path from the root, is read (S3).
func InScope(p string) Kind {
	switch {
	case strings.HasPrefix(p, "files/var/local/claude-yolo/"), strings.HasPrefix(p, "files/var/local/claude-code/"):
		return KindLauncher
	case strings.HasPrefix(p, "docs/") && strings.HasSuffix(p, ".md"):
		if p == "docs/ccy-changelog.md" {
			return KindNone
		}
		return KindText
	case strings.HasPrefix(p, "src-go/"):
		if base := path.Base(p); base == "usage.txt" || base == "help.txt" {
			return KindText
		}
	}
	return KindNone
}

// Line is one judged line: its number, its template and the names of its filled-in values.
type Line struct {
	No       int
	Text     string
	Template string
	Operands []string
}

var (
	// A handle's format and a path's directory: what the machine names (S4).
	machineShapeRe = regexp.MustCompile(`\.<seat>\+|/<seat>/`)
	// A message line of a launcher (S5).
	messageRe = regexp.MustCompile(`(?:^|[\s;&|(])(?:print_error|echo|printf)\b`)
	// A here-document's start: its delimiter, and whether it is indented (<<-); never a <<<.
	hereDocRe = regexp.MustCompile(`(?:^|[^<])<<(-?)\s*['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?`)
	// A shell variable: $name or ${name...}.
	shellVarRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)[^}]*\}|\$([A-Za-z_][A-Za-z0-9_]*)`)
)

// Lines is the lines of text a person reads, by its kind.
func Lines(kind Kind, text string) []Line {
	switch kind {
	case KindText:
		var lines []Line
		for i, raw := range strings.Split(text, "\n") {
			lines = append(lines, Line{No: i + 1, Text: raw, Template: machineShapeRe.ReplaceAllString(raw, "")})
		}
		return lines
	case KindLauncher:
		return launcherLines(text)
	case KindNone:
	}
	return nil
}

// launcherLines is a launcher's message lines and here-document lines; comments are not read.
func launcherLines(text string) []Line {
	var lines []Line
	hereEnd, hereIndent := "", false
	for i, raw := range strings.Split(text, "\n") {
		if hereEnd != "" {
			end := raw
			if hereIndent {
				end = strings.TrimLeft(raw, "\t")
			}
			if end == hereEnd {
				hereEnd = ""
			} else {
				lines = append(lines, launcherLine(i+1, raw))
			}
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(raw), "#") {
			continue
		}
		if m := hereDocRe.FindStringSubmatch(raw); m != nil {
			hereEnd, hereIndent = m[2], m[1] == "-"
		}
		if messageRe.MatchString(raw) {
			lines = append(lines, launcherLine(i+1, raw))
		}
	}
	return lines
}

// launcherLine is a launcher's message line with each shell variable as a filled-in value.
func launcherLine(no int, raw string) Line {
	var operands []string
	for _, m := range shellVarRe.FindAllStringSubmatch(raw, -1) {
		operands = append(operands, m[1]+m[2])
	}
	return Line{No: no, Text: raw, Template: shellVarRe.ReplaceAllString(raw, Value), Operands: operands}
}
