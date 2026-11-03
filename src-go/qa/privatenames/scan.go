package privatenames

import (
	"bytes"
	"strconv"
	"strings"
)

// Hit is one place a private term stands. It carries the place and the source label, never the term.
type Hit struct {
	Where  string
	Line   int
	Source string
	// Term is the term's position in the list (a number, never the term).
	Term int
	// Text is the line that matched; it holds the term, so it is only ever printed masked.
	Text string
}

// ScanDiff reports the added lines of a unified diff (and the added paths). Only added lines
// count: what a change leaves in place was judged when it arrived. label prefixes each place
// (a commit), "" for the staged change.
func ScanDiff(m *Matcher, label, diff string) []Hit {
	var hits []Hit
	path, prev, next := "", "", 0
	for line := range strings.SplitSeq(diff, "\n") {
		header := strings.HasPrefix(line, "+++ ") && strings.HasPrefix(prev, "--- ")
		prev = line
		switch {
		case header:
			path = strings.TrimPrefix(line, "+++ ")
			if path == "/dev/null" {
				path = ""
				continue
			}
			path = strings.TrimPrefix(path, "b/")
			if src, ok := m.Find(path); ok {
				hits = append(hits, Hit{Where: label + path, Source: src, Term: m.termOf(path)})
			}
		case strings.HasPrefix(line, "@@"):
			next = newStart(line)
		case strings.HasPrefix(line, "+") && path != "":
			if src, ok := m.Find(line[1:]); ok {
				hits = append(hits, Hit{Where: label + path, Line: next, Source: src, Term: m.termOf(line[1:]), Text: line[1:]})
			}
			next++
		}
	}
	return hits
}

// newStart is the first new-side line of a hunk header "@@ -a,b +c,d @@": c.
func newStart(header string) int {
	_, rest, ok := strings.Cut(header, " +")
	if !ok {
		return 0
	}
	num := rest
	if i := strings.IndexAny(rest, ", "); i >= 0 {
		num = rest[:i]
	}
	n, err := strconv.Atoi(num)
	if err != nil {
		return 0
	}
	return n
}

// ScanBlob reports every line of a file's content, and the file's path.
func ScanBlob(m *Matcher, path string, content []byte) []Hit {
	var hits []Hit
	if src, ok := m.Find(path); ok {
		hits = append(hits, Hit{Where: path, Source: src, Term: m.termOf(path)})
	}
	if bytes.IndexByte(content[:min(len(content), 8000)], 0) >= 0 {
		return hits
	}
	for n, line := range strings.Split(string(content), "\n") {
		if src, ok := m.Find(line); ok {
			hits = append(hits, Hit{Where: path, Line: n + 1, Source: src, Term: m.termOf(line), Text: line})
		}
	}
	return hits
}

// LogFormat is the --format that ScanLog reads: for each commit, SOH, its id, US, its message, STX,
// and then git's own patch.
const LogFormat = "%x01%H%x1f%B%x02"

// ScanLog reports the message and the added lines of every commit in `git log -p` output made
// with LogFormat. Each place is prefixed with the commit's short id.
func ScanLog(m *Matcher, log string) []Hit {
	var hits []Hit
	for _, rec := range strings.Split(log, "\x01")[1:] {
		id, rest := splitOnce(rec, "\x1f")
		msg, patch := splitOnce(rest, "\x02")
		id = id[:min(len(id), 12)]
		for n, line := range strings.Split(msg, "\n") {
			if src, ok := m.Find(line); ok {
				hits = append(hits, Hit{Where: id + ":(commit message)", Line: n + 1, Source: src, Term: m.termOf(line), Text: line})
			}
		}
		hits = append(hits, ScanDiff(m, id+":", patch)...)
	}
	return hits
}

// splitOnce is s split at the first sep; with no sep the whole of s is the first part.
func splitOnce(s, sep string) (string, string) {
	head, tail, found := strings.Cut(s, sep)
	if !found {
		return s, ""
	}
	return head, tail
}
