package hiddenbindings

import (
	"bytes"
	"fmt"
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/fzfbind"
)

// Rule is the identifier every finding carries; CLAUDE/GoProjectRules.md documents it.
const Rule = "project_hidden_bindings"

// The fzf flags that bind keys.
const (
	bindFlag   = "--bind"
	expectFlag = "--expect"
)

// Finding is one action reachable only by a key.
type Finding struct {
	Path    string
	Line    int
	Message string
}

// fzfPrograms are the names fzf is run by.
var fzfPrograms = map[string]bool{"fzf": true, "fzf-tmux": true}

// ShellFindings judges one shell file. A file that never runs fzf builds no fzf argv and is
// not judged; in one that does, every word of every command and array assignment is: an fzf
// argv is often built in an array and spread into the call. A file that does not parse is an
// error, never clean.
func ShellFindings(src []byte, name string) ([]Finding, error) {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(bytes.NewReader(src), name)
	if err != nil {
		return nil, fmt.Errorf("cannot parse %s: %w", name, err)
	}
	if !runsFzf(file) {
		return nil, nil
	}
	j := &shellJudge{name: name}
	syntax.Walk(file, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.CallExpr:
			j.judgeList(n.Args)
		case *syntax.Assign:
			switch {
			case n.Array != nil:
				words := make([]*syntax.Word, 0, len(n.Array.Elems))
				for _, e := range n.Array.Elems {
					words = append(words, e.Value)
				}
				j.judgeList(words)
			case n.Value != nil:
				j.judgeItems(scalarItems(n.Value))
			}
		}
		return true
	})
	return j.found, nil
}

// runsFzf is whether any command of the file names fzf (directly, by path, or behind a
// wrapper such as command or env).
func runsFzf(file *syntax.File) bool {
	found := false
	syntax.Walk(file, func(node syntax.Node) bool {
		if call, ok := node.(*syntax.CallExpr); ok {
			for _, w := range call.Args {
				if fzfPrograms[path.Base(readWord(w).last())] {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// wordText is a word's literal text up to its first expansion (head), and the literal pieces
// after it (tail); partial when it has an expansion.
type wordText struct {
	head    string
	tail    []string
	partial bool
}

// last is the literal text after the word's last expansion: the program of "$HOME/bin/fzf".
func (t wordText) last() string {
	if !t.partial {
		return t.head
	}
	return t.tail[len(t.tail)-1]
}

func readWord(w *syntax.Word) wordText {
	var t wordText
	var add func(parts []syntax.WordPart)
	add = func(parts []syntax.WordPart) {
		for _, p := range parts {
			var text string
			switch p := p.(type) {
			case *syntax.Lit:
				text = p.Value
			case *syntax.SglQuoted:
				text = p.Value
			case *syntax.DblQuoted:
				add(p.Parts)
				continue
			default:
				t.partial = true
				t.tail = append(t.tail, "")
				continue
			}
			if t.partial {
				t.tail[len(t.tail)-1] += text
			} else {
				t.head += text
			}
		}
	}
	add(w.Parts)
	return t
}

// shellJudge collects the findings of one file.
type shellJudge struct {
	name  string
	found []Finding
}

func (j *shellJudge) reportf(w *syntax.Word, format string, args ...any) {
	j.found = append(j.found, Finding{
		Path: j.name, Line: int(w.Pos().Line()), Message: fmt.Sprintf(format, args...) + " [" + Rule + "]",
	})
}

// item is one argv word as the rule reads it: where it is, and its text.
type item struct {
	at   *syntax.Word
	text wordText
}

// judgeList judges each word, with the word after a bare --bind as its binding.
func (j *shellJudge) judgeList(words []*syntax.Word) {
	items := make([]item, 0, len(words))
	for _, w := range words {
		items = append(items, item{w, readWord(w)})
	}
	j.judgeItems(items)
}

// scalarItems is the words a scalar's value becomes when it is spread unquoted into a
// command: its literal text split on whitespace, the last word open when an expansion
// follows it. Only a value that starts with an option is read as options; any other text (a
// label, a message) is not argv.
func scalarItems(w *syntax.Word) []item {
	t := readWord(w)
	fields := strings.Fields(t.head)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "-") {
		return nil
	}
	items := make([]item, 0, len(fields))
	for _, f := range fields {
		items = append(items, item{w, wordText{head: f}})
	}
	if t.partial && !strings.HasSuffix(t.head, " ") {
		last := &items[len(items)-1].text
		last.partial, last.tail = true, t.tail
	}
	return items
}

// judgeItems judges each item, with the item after a bare --bind as its binding.
func (j *shellJudge) judgeItems(items []item) {
	for i := 0; i < len(items); i++ {
		w, t := items[i].at, items[i].text
		switch {
		case t.head == expectFlag || strings.HasPrefix(t.head, expectFlag+"="):
			j.reportf(w, "fzf --expect: a key the script acts on with no visible choice; offer the action as a row")
		case t.head == bindFlag && !t.partial:
			if i+1 == len(items) {
				j.reportf(w, "fzf --bind with no binding beside it: the rule cannot judge it")
				continue
			}
			i++
			j.judgeSpec(items[i].at, items[i].text, "")
		case strings.HasPrefix(t.head, bindFlag+"="):
			j.judgeSpec(w, t, bindFlag+"=")
		}
	}
}

// judgeSpec judges a binding: its literal head is read, and a run-time rest must sit inside
// an argument the head leaves open and add no binding of its own.
func (j *shellJudge) judgeSpec(w *syntax.Word, t wordText, prefix string) {
	spec := strings.TrimPrefix(t.head, prefix)
	if spec == "" {
		j.reportf(w, "fzf --bind with a binding built at run time: the rule cannot judge it; spell it literally")
		return
	}
	bindings, open, err := fzfbind.Read(spec)
	if err != nil {
		j.reportf(w, "fzf --bind %q cannot be read as bindings: spell it as fzf documents it", spec)
		return
	}
	for _, b := range bindings {
		if a := fzfbind.HiddenAction(b); a != "" {
			j.reportf(w, "fzf --bind %s:%s: a key bound to an action no visible choice offers; offer the action as a row", b.Trigger, a)
			return
		}
	}
	if !t.partial {
		return
	}
	if open == "" {
		j.reportf(w, "fzf --bind with a binding built at run time: the rule cannot judge it; spell it literally")
		return
	}
	for _, piece := range t.tail {
		if fzfbind.ClosesThenBinds(open, piece) {
			j.reportf(w, "fzf --bind with a binding after a run-time argument: the rule cannot judge it")
			return
		}
	}
}
