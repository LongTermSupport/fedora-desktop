// Package privatenames is the gate for the defect class "an install-specific name reaching a
// commit" (Plan 00245, Phase 3): the names of agent teams, seats, containers, hosts and client
// projects. This is a public repository, and CLAUDE.md "Public Repository" says none of them
// belongs in it. The localhost.yml denylist cannot know them (the file is absent where agents
// commit, and never held them), so this package learns them from the places an agent session can
// read: the agent bus's own status report and the machine's host names.
//
// The terms are written to a list inside the git directory (never tracked, never committable),
// read back by the next run so a team that left the bus stays guarded, and only ever reported by
// source label, never by value. The same matcher then judges the staged change (pre-commit), the
// commits being pushed (pre-push) and a whole tree (the QA gate).
//
// A term must be specific before it may block anything: see Derive.
package privatenames

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// The source labels a finding is reported under. A finding never carries the term itself.
const (
	SourceTeam     = "agent-bus team"
	SourceMember   = "agent-bus member"
	SourceHostname = "host name"
)

// MinSpecificLength is the shortest term that may block a commit. A shorter word is a word, not
// a name: it appears in ordinary prose and code, and a gate that blocks ordinary prose is
// bypassed with --no-verify. Compound names (a hyphen, dot or underscore inside) are measured
// whole, so a seat handle such as name.1 is long enough while its bare project word is not.
const MinSpecificLength = 6

// generic are the words that name a kind of thing, not a particular one. A term whose every
// word is here (dev-infra, test-server) is dropped, and a name's first word is not indexed on its
// own when it is here. These are public words: the list holds no install's names.
var generic = map[string]bool{
	"dev": true, "test": true, "tests": true, "infra": true, "infrastructure": true,
	"desktop": true, "local": true, "host": true, "podman": true, "docker": true,
	"main": true, "work": true, "team": true, "teams": true, "apps": true, "app": true,
	"android": true, "server": true, "servers": true, "prod": true, "production": true,
	"staging": true, "docs": true, "tools": true, "core": true, "common": true,
	"shared": true, "default": true, "project": true, "projects": true, "system": true,
	"worker": true, "human": true, "coordinator": true, "claude": true, "agent": true,
	"agents": true, "bus": true, "book": true, "books": true, "recorder": true,
	"speech": true, "viewer": true, "code": true, "hooks": true, "daemon": true,
	"reports": true, "questions": true, "workspace": true, "container": true, "session": true,
}

// Term is one private name and where it was learned.
type Term struct {
	Source string
	Text   string
}

// Inputs is everything Derive reads.
type Inputs struct {
	// Status is the agent bus's status report: TEAM, MEMBER and SEAT lines, tab separated.
	Status string
	// Hostnames are this machine's names (its own and the host's).
	Hostnames []string
	// Public are the tokens that are public by design (the allowlist); never terms.
	Public []string
}

// Derive turns the sources into the terms that may block a commit. The rule that keeps a short
// or generic term from blocking everything, in order: a public token is never a term; a term of
// fewer than MinSpecificLength characters is dropped; a term whose every word is generic is
// dropped; a name's first word is indexed alone only when it is itself long and not generic.
// Output is sorted and free of duplicates.
func Derive(in Inputs) []Term {
	public := map[string]bool{}
	for _, p := range in.Public {
		public[strings.ToLower(strings.TrimSpace(p))] = true
	}
	d := &deriver{public: public, seen: map[string]bool{}}
	for line := range strings.SplitSeq(in.Status, "\n") {
		cols := strings.Split(strings.TrimRight(line, "\r"), "\t")
		switch cols[0] {
		case "TEAM":
			if len(cols) > 1 {
				d.name(SourceTeam, cols[1])
			}
			if len(cols) > 2 {
				d.handle(cols[2])
			}
		case "MEMBER":
			if len(cols) > 2 {
				d.handle(cols[2])
			}
		case "SEAT":
			if len(cols) > 5 {
				d.handle(cols[5])
			}
		}
	}
	for _, h := range in.Hostnames {
		d.add(SourceHostname, strings.TrimSpace(h))
	}
	sort.Slice(d.out, func(i, j int) bool { return d.out[i].Text < d.out[j].Text })
	return d.out
}

type deriver struct {
	public map[string]bool
	seen   map[string]bool
	out    []Term
}

// isPublic is whether a term names nothing private: a public token itself, or a handle form built
// on one (token.1, token.1+local.podman) whose every other word is a seat number or a generic
// word. A handle whose site is a specific word (token.1+somehost.podman) is not public.
func isPublic(text string, public map[string]bool) bool {
	key := strings.ToLower(text)
	if public[key] {
		return true
	}
	project, rest, found := strings.Cut(key, ".")
	if !found || !public[project] {
		return false
	}
	return !Specific(rest)
}

func (d *deriver) add(source, text string) {
	key := strings.ToLower(text)
	if d.seen[key] || !Specific(text) || isPublic(text, d.public) {
		return
	}
	d.seen[key] = true
	d.out = append(d.out, Term{Source: source, Text: text})
}

// before is s up to the first sep.
func before(s, sep string) string {
	head, _, found := strings.Cut(s, sep)
	if !found {
		return s
	}
	return head
}

// name adds a name and, for a compound one that is not public, its first word.
func (d *deriver) name(source, name string) {
	d.add(source, name)
	if first := before(name, "-"); first != name && !generic[strings.ToLower(first)] && !d.public[strings.ToLower(name)] {
		d.add(source, first)
	}
}

// addHandle adds a member handle (project.seat+where) in each form a document might write it:
// the whole handle, project.seat, and the project name with its first word.
func (d *deriver) handle(handle string) {
	handle = strings.TrimSpace(handle)
	d.add(SourceMember, handle)
	base := before(handle, "+")
	d.add(SourceMember, base)
	d.name(SourceMember, before(base, "."))
}

// Specific is whether a term may block a commit: long enough, and not made only of generic words.
func Specific(text string) bool {
	if len(text) < MinSpecificLength {
		return false
	}
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, w := range words {
		if !generic[w] && !allDigits(w) {
			return true
		}
	}
	return false
}

func allDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return s != ""
}

// Format is the list file's text: one SOURCE TAB TERM line per term.
func Format(terms []Term) string {
	var b strings.Builder
	for _, t := range terms {
		b.WriteString(t.Source + "\t" + t.Text + "\n")
	}
	return b.String()
}

// ParseError is a list line that is not SOURCE TAB TERM. It names the line, never its text.
type ParseError struct{ Line int }

func (e *ParseError) Error() string {
	return "the private-term list is damaged at line " + strconv.Itoa(e.Line)
}

// Parse reads the list file. A line without a tab is an error: a list read half is not a list.
func Parse(text string) ([]Term, error) {
	var out []Term
	for n, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		source, term, ok := strings.Cut(line, "\t")
		if !ok || term == "" {
			return nil, &ParseError{Line: n + 1}
		}
		out = append(out, Term{Source: source, Text: term})
	}
	return out, nil
}

// Union merges lists, the first spelling of a term (case-folded) winning.
func Union(lists ...[]Term) []Term {
	seen := map[string]bool{}
	var out []Term
	for _, l := range lists {
		for _, t := range l {
			if k := strings.ToLower(t.Text); !seen[k] {
				seen[k] = true
				out = append(out, t)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Text < out[j].Text })
	return out
}
