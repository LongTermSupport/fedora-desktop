package ansible

import (
	"cmp"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
	"mvdan.cc/sh/v3/syntax"
)

// The shell-snapshot-dropped-name rule (CLAUDE/QA.md, "shell-snapshot-dropped-name"). Claude
// Code's Bash tool does not run the login shell's startup files: it replays a snapshot of them,
// which keeps every function except those whose name starts with a single underscore (the
// filter `[[ "$func" == _[!_]* ]]`, so completion functions are left out; a double underscore
// is kept) and of the variables only those exported. A function of an interactive shell include
// that calls a single-underscore function, or reads a global the include sets without exporting
// it, is defined there but fails when it is called (Plan 00265 R72: the git() wrapper of the
// GitHub accounts include, in a repository with an account's remote).
//
// The rule reads every task that writes an interactive shell include (a file under
// ~/.bashrc-includes/ or /etc/profile.d/, or .bashrc, .bash_profile, .profile): a blockinfile's
// block, a copy's content, or the file a copy or template names as src under {{ root_dir }},
// with literal loop items put in place. The text is parsed as bash with each Jinja tag made
// neutral ({{ ... }} a word, {% ... %} and {# ... #} nothing). In each function whose own name
// the snapshot keeps, it reports a command named with a single leading underscore, and a read of
// a variable the include assigns outside every function and does not export, unless the function
// assigns or declares that name itself. A source that cannot be read or parsed is reported: an
// include the rule cannot judge is not clean.

// ruleSnapshotDropped is the rule ID, printed with every finding and the anchor of its section.
const ruleSnapshotDropped = "shell-snapshot-dropped-name"

// shellInclude is a destination an interactive bash reads its startup from.
var shellInclude = regexp.MustCompile(`(?:/\.bashrc-includes/[^/]+|/etc/profile\.d/[^/]+|/\.bashrc|/\.bash_profile|/\.profile)$`)

// rootDirPrefix is how a play names a file of the checkout.
const rootDirPrefix = "{{ root_dir }}/"

// droppedFunction is a function name the snapshot leaves out.
var droppedFunction = regexp.MustCompile(`^_[^_]`)

// jinjaSpan is one Jinja expression, statement or comment.
var jinjaSpan = regexp.MustCompile(`(?s)\{\{.*?\}\}|\{%.*?%\}|\{#.*?#\}`)

// includeText is one shell include to judge: its text, where its first line is, and the task.
type includeText struct {
	rel      string // the file the text is in
	first    int    // the line of that file the text's first line is
	text     string
	taskAt   string // "<rel>:<line>" of the task that writes it
	readFail string // why the text could not be read, or ""
}

// snapshotDroppedNames returns the rule's findings over the plays and task files, each
// "<rel>:<line>: <rule>: <why>", in file then line order, each once. A file the YAML library
// cannot parse is skipped: command-always-changed reports it.
func snapshotDroppedNames(fsys fs.FS, sources []yamlSource) []string {
	var findings []string
	var parsed []parsedInclude
	globals := map[string]bool{}
	for _, inc := range allIncludes(fsys, sources) {
		p, fail := parseInclude(inc)
		if fail != "" {
			findings = append(findings, fail)
			continue
		}
		for name := range topLevelGlobals(p.file, p.funcs) {
			globals[name] = true
		}
		parsed = append(parsed, p)
	}
	for _, p := range parsed {
		findings = append(findings, judgeInclude(p, globals)...)
	}
	var out []string
	seen := map[string]bool{}
	for _, f := range findings {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

// allIncludes is every shell include the tasks of the files write, in file then task order.
func allIncludes(fsys fs.FS, sources []yamlSource) []includeText {
	var incs []includeText
	for _, src := range sources {
		for _, doc := range parsedDocs(src.data) {
			for _, top := range doc.Content {
				for _, t := range runTasks(top) {
					incs = append(incs, includeTexts(fsys, src.rel, t)...)
				}
			}
		}
	}
	return incs
}

// includeTexts is the shell include text a task writes, one per loop item; none when the task
// writes no include.
func includeTexts(fsys fs.FS, rel string, t uxTask) []includeText {
	if t.module != modBlockInFile && t.module != modCopy && t.module != modTemplate {
		return nil
	}
	args := managedArgs(t)
	if scalarArg(args, "state") == stateAbsent {
		return nil
	}
	items, readable := loopItems(t.node)
	if !readable {
		items = []*yaml.Node{nil}
	}
	at := rel + ":" + t.line()
	var out []includeText
	for _, item := range items {
		dest := normaliseTemplate(withItem(cmp.Or(scalarArg(args, "path"), scalarArg(args, "dest")), item))
		if !shellInclude.MatchString(dest) {
			continue
		}
		key := "content"
		if t.module == modBlockInFile {
			key = "block"
		}
		if node := argNode(args, key); node != nil && t.module != modTemplate {
			first := node.Line
			if node.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
				first++
			}
			out = append(out, includeText{rel: rel, first: first, text: node.Value, taskAt: at})
			continue
		}
		out = append(out, sourceText(fsys, normaliseTemplate(withItem(scalarArg(args, "src"), item)), at))
	}
	return out
}

// sourceText reads the file a copy or template task names as its src.
func sourceText(fsys fs.FS, src, at string) includeText {
	rel, ok := strings.CutPrefix(src, rootDirPrefix)
	if !ok || strings.Contains(rel, "{{") {
		return includeText{taskAt: at, readFail: fmt.Sprintf("its source %q is not a file of the checkout named by %s", src, rootDirPrefix)}
	}
	data, err := fs.ReadFile(fsys, rel)
	if err != nil {
		return includeText{taskAt: at, readFail: fmt.Sprintf("its source %s cannot be read: %v", rel, err)}
	}
	return includeText{rel: rel, first: 1, text: string(data), taskAt: at}
}

// argNode is the first value of a key among the argument mappings, or nil.
func argNode(holders []*yaml.Node, key string) *yaml.Node {
	for _, holder := range holders {
		if v := mappingValue(holder, key); v != nil && v.Kind == yaml.ScalarNode {
			return v
		}
	}
	return nil
}

// neutralJinja makes each Jinja span neutral for the bash parser, keeping every line where it
// was: an expression becomes one word, a statement or comment nothing.
func neutralJinja(text string) string {
	return jinjaSpan.ReplaceAllStringFunc(text, func(span string) string {
		keep := strings.Repeat("\n", strings.Count(span, "\n"))
		if strings.HasPrefix(span, "{{") {
			return "J" + keep
		}
		return keep
	})
}

// parsedInclude is an include's text parsed as bash, with its functions.
type parsedInclude struct {
	inc   includeText
	file  *syntax.File
	funcs []*syntax.FuncDecl
}

// ruleHead is the start of a finding at a place.
func ruleHead(at string) string { return at + ": " + ruleSnapshotDropped + ": " }

// parseInclude parses an include, or says why it cannot be judged.
func parseInclude(inc includeText) (parsedInclude, string) {
	if inc.readFail != "" {
		return parsedInclude{}, ruleHead(inc.taskAt) + "this task writes a shell include the rule cannot judge: " + inc.readFail
	}
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(neutralJinja(inc.text)), inc.rel)
	if err != nil {
		return parsedInclude{}, ruleHead(inc.taskAt) + "this task writes a shell include that cannot be parsed as bash, so the rule cannot judge it: " + err.Error()
	}
	return parsedInclude{inc: inc, file: file, funcs: functionsOf(file)}, ""
}

// judgeInclude says what in one include fails after the snapshot replays it. globals is every
// unexported global the includes set, since every include is sourced into the same shell.
func judgeInclude(p parsedInclude, globals map[string]bool) []string {
	var found []string
	for _, fn := range p.funcs {
		if droppedFunction.MatchString(fn.Name.Value) {
			continue
		}
		for _, d := range droppedUses(fn, globals) {
			found = append(found, ruleHead(p.inc.rel+":"+strconv.Itoa(p.inc.first+d.line-1))+d.why)
		}
	}
	return found
}

// functionsOf is every function the include defines, in source order.
func functionsOf(file *syntax.File) []*syntax.FuncDecl {
	var funcs []*syntax.FuncDecl
	syntax.Walk(file, func(node syntax.Node) bool {
		if fn, ok := node.(*syntax.FuncDecl); ok {
			funcs = append(funcs, fn)
		}
		return true
	})
	return funcs
}

// inside is whether a node lies within one of the functions.
func inside(node syntax.Node, funcs []*syntax.FuncDecl) bool {
	return slices.ContainsFunc(funcs, func(fn *syntax.FuncDecl) bool {
		return node.Pos().Offset() >= fn.Pos().Offset() && node.End().Offset() <= fn.End().Offset()
	})
}

// topLevelGlobals is every variable the include assigns outside its functions and does not
// export: what the snapshot leaves out.
func topLevelGlobals(file *syntax.File, funcs []*syntax.FuncDecl) map[string]bool {
	globals := map[string]bool{}
	syntax.Walk(file, func(node syntax.Node) bool {
		if node == nil || inside(node, funcs) {
			return false
		}
		for _, name := range unexportedAssigned(node) {
			globals[name] = true
		}
		return true
	})
	return globals
}

// shellState is the variables bash itself keeps for an interactive shell. An include may set
// them, but they are the shell's state, not the include's data: a function reading one is
// reading the shell it runs in, whichever shell that is.
var shellState = map[string]bool{
	"HISTFILE": true, "HISTFILESIZE": true, "HISTSIZE": true, "HISTCONTROL": true, "HISTIGNORE": true,
	"HISTTIMEFORMAT": true, "PROMPT_COMMAND": true, "PS0": true, "PS1": true, "PS2": true, "PS3": true, "PS4": true,
	"READLINE_LINE": true, "READLINE_POINT": true, "READLINE_MARK": true, "IFS": true, "PWD": true, "OLDPWD": true,
}

// unexportedAssigned is the names a statement assigns without exporting them: a bare X=...
// (not an environment prefix of a command), or a declaration that does not export.
func unexportedAssigned(node syntax.Node) []string {
	var assigns []*syntax.Assign
	switch n := node.(type) {
	case *syntax.CallExpr:
		if len(n.Args) == 0 {
			assigns = n.Assigns
		}
	case *syntax.DeclClause:
		if !exported(n) {
			assigns = n.Args
		}
	}
	var names []string
	for _, a := range assigns {
		if a.Name != nil && !shellState[a.Name.Value] {
			names = append(names, a.Name.Value)
		}
	}
	return names
}

// exported is whether a declaration exports what it declares (an exported scalar reaches the
// snapshot through the environment).
func exported(d *syntax.DeclClause) bool {
	if d.Variant.Value == "export" {
		return true
	}
	for _, a := range d.Args {
		if a.Name == nil && a.Value != nil {
			if flag := a.Value.Lit(); strings.HasPrefix(flag, "-") && strings.Contains(flag, "x") {
				return true
			}
		}
	}
	return false
}

// droppedUse is one call or read in a function that the snapshot breaks.
type droppedUse struct {
	line int
	why  string
}

// droppedUses is every call of a dropped function and read of a dropped global in a function.
func droppedUses(fn *syntax.FuncDecl, globals map[string]bool) []droppedUse {
	own := map[string]bool{}
	syntax.Walk(fn.Body, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.Assign:
			if n.Name != nil {
				own[n.Name.Value] = true
			}
		case *syntax.WordIter:
			own[n.Name.Value] = true
		}
		return true
	})
	name := fn.Name.Value
	var uses []droppedUse
	syntax.Walk(fn.Body, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.CallExpr:
			if len(n.Args) > 0 {
				if cmd := n.Args[0].Lit(); droppedFunction.MatchString(cmd) {
					uses = append(uses, droppedUse{line: int(n.Args[0].Pos().Line()), why: fmt.Sprintf(
						"%s calls %s, a single-underscore function, which Claude Code's shell snapshot leaves out, so %s fails "+
							"there; inline it, or rename it with no leading single underscore (a double underscore is kept)", name, cmd, name)})
				}
			}
		case *syntax.ParamExp:
			if v := n.Param.Value; globals[v] && !own[v] {
				uses = append(uses, droppedUse{line: int(n.Pos().Line()), why: fmt.Sprintf(
					"%s reads %s, a global this include sets without exporting, which Claude Code's shell snapshot leaves "+
						"out; resolve it when %s is called (a case in the function, or a function the snapshot keeps)", name, v, name)})
			}
		}
		return true
	})
	return uses
}
