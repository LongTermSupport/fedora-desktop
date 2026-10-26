package hiddenbindings

import (
	"bytes"
	"fmt"
	"path"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/fzfbind"
)

// EscRule is the identifier of the Esc-joined findings; CLAUDE/GoProjectRules.md documents it.
// This gate carries the shell half of that Go rule, as it does of project_hidden_bindings.
const EscRule = "project_esc_joined"

// lookups are the wrapper flags that only look a program up: command -v fzf runs nothing.
var lookups = map[string]bool{"-v": true, "-V": true}

// ShellEscFindings judges one shell file for project_esc_joined: every command that runs fzf
// must bind, in its own literal --bind words, every key fzf reads an Esc and the next key as
// (fzfbind.EscJoined) to abort, and none of fzf's word editing keys (fzfbind.WordEditing),
// unless an option makes it read no key (fzfbind.DrawsNoScreen:
// --version, --filter, ...). Bindings spread in from an array or a variable cannot be read,
// so they do not count. A file that does not parse is an error, never clean.
func ShellEscFindings(src []byte, name string) ([]Finding, error) {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(bytes.NewReader(src), name)
	if err != nil {
		return nil, fmt.Errorf("cannot parse %s: %w", name, err)
	}
	var found []Finding
	syntax.Walk(file, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true
		}
		at := fzfAt(call.Args)
		if at < 0 {
			return true
		}
		if slices.ContainsFunc(call.Args[at+1:], func(w *syntax.Word) bool { return fzfbind.DrawsNoScreen(readWord(w).head) }) {
			return true
		}
		specs := literalBinds(call.Args[at+1:])
		line := int(call.Args[at].Pos().Line())
		if missing := fzfbind.NotLeaving(specs); len(missing) > 0 {
			found = append(found, Finding{
				Path: name, Line: line,
				Message: fmt.Sprintf("fzf without the Esc-joined bindings: %s not bound to abort, so an Esc typed just before them is lost; bind every one to abort [%s]",
					strings.Join(missing[:min(len(missing), 3)], ", "), EscRule),
			})
		}
		if aborted := fzfbind.AbortsWordEditing(specs); len(aborted) > 0 {
			found = append(found, Finding{
				Path: name, Line: line,
				Message: fmt.Sprintf("fzf word editing bound to abort: %s, so the query typed is thrown away; leave alt-b, alt-f, alt-d and alt-bs to fzf [%s]",
					strings.Join(aborted, ", "), EscRule),
			})
		}
		return true
	})
	return found, nil
}

// fzfAt is the index of the word that runs fzf, behind command, exec, nohup or env and their
// options, or -1 when the command does not run fzf (command -v fzf only looks it up).
func fzfAt(words []*syntax.Word) int {
	i := 0
	for i < len(words) {
		t := readWord(words[i])
		if t.partial {
			return -1
		}
		if !wrappers[t.head] {
			if fzfPrograms[path.Base(t.head)] {
				return i
			}
			return -1
		}
		i = pastWrapper(t.head, words, i+1)
	}
	return -1
}

// wrappers run the program named after their own options.
var wrappers = map[string]bool{"command": true, "exec": true, "nohup": true, "env": true}

// pastWrapper is the index of the first word after wrapper's options (env's NAME=VALUE too),
// from i; len(words) when command only looks the program up (-v, -V).
func pastWrapper(wrapper string, words []*syntax.Word, i int) int {
	for ; i < len(words); i++ {
		opt := readWord(words[i]).head
		switch {
		case wrapper == "command" && lookups[opt]:
			return len(words)
		case strings.HasPrefix(opt, "-"), wrapper == "env" && strings.Contains(opt, "="):
		default:
			return i
		}
	}
	return i
}

// literalBinds is the binding text of every literal --bind word: "--bind=SPEC", or "--bind"
// and SPEC as the next word.
func literalBinds(words []*syntax.Word) []string {
	var specs []string
	for i := 0; i < len(words); i++ {
		t := readWord(words[i])
		if t.partial {
			continue
		}
		if spec, ok := strings.CutPrefix(t.head, bindFlag+"="); ok {
			specs = append(specs, spec)
			continue
		}
		if t.head == bindFlag && i+1 < len(words) {
			if next := readWord(words[i+1]); !next.partial {
				specs = append(specs, next.head)
				i++
			}
		}
	}
	return specs
}
