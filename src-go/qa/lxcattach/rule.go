package lxcattach

import (
	"bytes"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// RuleID names the rule in every finding; CLAUDE/QA.md documents it under this name.
const RuleID = "lxc-attach-file-stream"

// FixturePath is the rule's annotated fixture, relative to the repository root.
const FixturePath = ".semgrep/lxc-attach-streams.bash"

// Source is one shell script to judge.
type Source struct {
	Path string
	Text []byte
}

// Finding is one command that reaches lxc-attach while a descriptor lxc-attach takes as a
// standard stream is a file.
type Finding struct {
	Path      string
	Line      int
	Stream    string
	RedirLine int
	fd        int
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s: %s is a file (redirected on line %d), and lxc-attach makes it root's",
		f.Path, f.Line, RuleID, f.Stream, f.RedirLine)
}

// toolName is a word that names the lxc-attach program: the bare name or any path ending in it.
var toolName = regexp.MustCompile(`(^|/)lxc-attach$`)

// fds is how many descriptors the walk follows: the three standard streams and the ones a script
// opens to carry them (`3>&1`, then `1>&3`).
const fds = 10

// toolStreams are the descriptors lxc-attach takes over.
var toolStreams = map[int]bool{0: true, 1: true, 2: true}

// streamName names a descriptor.
func streamName(fd int) string {
	if fd < 3 {
		return [3]string{"stdin", "stdout", "stderr"}[fd]
	}
	return "descriptor " + strconv.Itoa(fd)
}

// target is what one descriptor points at: a file (redirected on a line), the descriptor of the
// same number or another that the enclosing function or script was handed (from), or something
// lxc-attach leaves the caller able to use: a pipe, a device, a closed descriptor (from -1).
type target struct {
	file bool
	line int
	from int
}

// safe is a pipe, a device or a closed descriptor.
var safe = target{from: -1}

// state is every followed descriptor.
type state [fds]target

// handed is the descriptors on entry to a script or function: each the caller's own.
func handed() state {
	var st state
	for i := range st {
		st[i] = target{from: i}
	}
	return st
}

// Analyze judges the scripts together: a function or variable that reaches lxc-attach in one of
// them reaches it in all, as a plan's acceptance sources its stage files. The findings are in
// path, line and descriptor order.
func Analyze(sources []Source) ([]Finding, error) {
	files := make([]*syntax.File, 0, len(sources))
	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	for _, src := range sources {
		file, err := parser.Parse(bytes.NewReader(src.Text), src.Path)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %s: %w", src.Path, err)
		}
		files = append(files, file)
	}
	r := newReach(files)
	var found []Finding
	for i, src := range sources {
		w := &walker{reach: r, path: src.Path, seen: map[string]bool{}}
		w.list(files[i].Stmts, handed())
		found = append(found, w.found...)
	}
	slices.SortStableFunc(found, func(a, b Finding) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return a.fd - b.fd
	})
	return found, nil
}

// reach is the variables that hold lxc-attach and, for each function that runs it, the
// descriptors of its caller that become a stream of lxc-attach. A function that hands
// lxc-attach only pipes has none, and is no hazard however it is called.
type reach struct {
	vars  map[string]bool
	funcs map[string]map[int]bool
}

// newReach finds the variables to a fixed point (a value that names the tool or such a variable),
// then each function's descriptors to a fixed point (a function calling another takes on the
// descriptors it passes through to it).
func newReach(files []*syntax.File) *reach {
	bodies, values := definitions(files)
	r := &reach{vars: map[string]bool{}, funcs: map[string]map[int]bool{}}
	for more := true; more; {
		more = false
		for name, words := range values {
			if !r.vars[name] && slices.ContainsFunc(words, r.namesTool) {
				r.vars[name], more = true, true
			}
		}
	}
	for more := true; more; {
		more = false
		for name, list := range bodies {
			for _, body := range list {
				more = r.summarise(name, body) || more
			}
		}
	}
	return r
}

// definitions is every function's bodies and every variable's stored words, by name.
func definitions(files []*syntax.File) (map[string][]*syntax.Stmt, map[string][]*syntax.Word) {
	bodies := map[string][]*syntax.Stmt{}
	values := map[string][]*syntax.Word{}
	for _, file := range files {
		syntax.Walk(file, func(n syntax.Node) bool {
			switch x := n.(type) {
			case *syntax.FuncDecl:
				if x.Name != nil {
					bodies[x.Name.Value] = append(bodies[x.Name.Value], x.Body)
				}
			case *syntax.Assign:
				if x.Name != nil {
					values[x.Name.Value] = append(values[x.Name.Value], assignWords(x)...)
				}
			}
			return true
		})
	}
	return bodies, values
}

// summarise adds to a function the caller's descriptors one body passes to lxc-attach, and says
// whether that added any.
func (r *reach) summarise(name string, body *syntax.Stmt) bool {
	w := &walker{reach: r, seen: map[string]bool{}, passed: map[int]bool{}}
	w.stmt(body, handed())
	added := false
	for fd := range w.passed {
		if r.funcs[name] == nil {
			r.funcs[name] = map[int]bool{}
		}
		if !r.funcs[name][fd] {
			r.funcs[name][fd], added = true, true
		}
	}
	return added
}

// assignWords is the words an assignment stores.
func assignWords(a *syntax.Assign) []*syntax.Word {
	var words []*syntax.Word
	if a.Value != nil {
		words = append(words, a.Value)
	}
	if a.Array != nil {
		for _, elem := range a.Array.Elems {
			if elem.Value != nil {
				words = append(words, elem.Value)
			}
		}
	}
	return words
}

// streams is the descriptors of a command that become a stream of lxc-attach, in order: a word
// that names the tool, a variable holding it or a function running it is the command itself, or
// follows a program that runs the words after it (`"${LXC_ATTACH[@]}" -n c`, `sudo -n
// lxc-attach`, `best_effort_rm what sudo lxc-attach`, `timeout 5 in_guest`). The tool named as an
// argument to anything else (`echo`, `grep`) is not run.
func (r *reach) streams(call *syntax.CallExpr) []int {
	for i, arg := range call.Args {
		if i > 0 && !slices.ContainsFunc(call.Args[:i], isRunner) {
			continue
		}
		if r.namesTool(arg) {
			return slices.Sorted(maps.Keys(toolStreams))
		}
		if passed, ok := r.funcs[flatten(arg)]; ok {
			return slices.Sorted(maps.Keys(passed))
		}
	}
	return nil
}

// runners are the programs that run the command their arguments name.
var runners = map[string]bool{
	"sudo": true, "doas": true, "runuser": true, "timeout": true, "env": true, "nice": true,
	"ionice": true, "nohup": true, "setsid": true, "stdbuf": true, "exec": true, "command": true,
	"builtin": true, "xargs": true, "flock": true, "time": true, "unbuffer": true, "chrt": true,
}

// isRunner is a word that is one of the runners, by name or path.
func isRunner(w *syntax.Word) bool {
	text := flatten(w)
	return runners[text[strings.LastIndexByte(text, '/')+1:]]
}

// namesTool is whether one word names the tool or expands a variable holding it.
func (r *reach) namesTool(w *syntax.Word) bool {
	if toolName.MatchString(flatten(w)) {
		return true
	}
	hit := false
	syntax.Walk(w, func(n syntax.Node) bool {
		switch x := n.(type) {
		case *syntax.ParamExp:
			if x.Param != nil && r.vars[x.Param.Value] {
				hit = true
			}
		case *syntax.CmdSubst, *syntax.ProcSubst:
			return false
		}
		return !hit
	})
	return hit
}

// flatten is a word's literal text with quotes removed; an expansion becomes a NUL, so it never
// matches a name.
func flatten(w *syntax.Word) string {
	var b strings.Builder
	var parts func([]syntax.WordPart)
	parts = func(ps []syntax.WordPart) {
		for _, p := range ps {
			switch x := p.(type) {
			case *syntax.Lit:
				b.WriteString(x.Value)
			case *syntax.SglQuoted:
				b.WriteString(x.Value)
			case *syntax.DblQuoted:
				parts(x.Parts)
			default:
				b.WriteByte(0)
			}
		}
	}
	parts(w.Parts)
	return b.String()
}

// walker follows the descriptors through one script or, when summarising a function (passed is
// set), through its body, collecting the caller's descriptors it passes to lxc-attach.
type walker struct {
	reach  *reach
	path   string
	found  []Finding
	seen   map[string]bool
	passed map[int]bool
}

// stmt judges a statement under the descriptors it inherits. A pipe is set up before the
// statement's own redirections, as bash does.
func (w *walker) stmt(stmt *syntax.Stmt, in state) {
	for _, r := range stmt.Redirs {
		if r.Word != nil {
			w.inner(r.Word, in)
		}
	}
	st := apply(stmt.Redirs, in)
	switch x := stmt.Cmd.(type) {
	case *syntax.BinaryCmd:
		left, right := st, st
		if x.Op == syntax.Pipe || x.Op == syntax.PipeAll {
			left[1], right[0] = safe, safe
		}
		if x.Op == syntax.PipeAll {
			left[2] = safe
		}
		w.stmt(x.X, left)
		w.stmt(x.Y, right)
	case *syntax.CallExpr:
		w.report(stmt, st, w.reach.streams(x))
		w.inner(x, st)
	case nil:
	default:
		w.inner(x, st)
	}
}

// inner judges what a node holds: its statements under the same descriptors, a command
// substitution with stdout a pipe, a process substitution with its far end a pipe, and a
// function body under its callers' descriptors (each call is judged where it is made). A function
// nested in one being summarised is summarised on its own.
func (w *walker) inner(n syntax.Node, st state) {
	syntax.Walk(n, func(c syntax.Node) bool {
		switch x := c.(type) {
		case *syntax.Stmt:
			w.stmt(x, st)
			return false
		case *syntax.CmdSubst:
			sub := st
			sub[1] = safe
			w.list(x.Stmts, sub)
			return false
		case *syntax.ProcSubst:
			sub := st
			if x.Op == syntax.CmdIn {
				sub[1] = safe
			} else {
				sub[0] = safe
			}
			w.list(x.Stmts, sub)
			return false
		case *syntax.FuncDecl:
			if w.passed == nil {
				w.stmt(x.Body, handed())
			}
			return false
		}
		return true
	})
}

func (w *walker) list(stmts []*syntax.Stmt, st state) {
	for _, s := range stmts {
		w.stmt(s, st)
	}
}

// report records each descriptor of a reaching command that is a file, once, and when
// summarising, each that is still the caller's.
func (w *walker) report(stmt *syntax.Stmt, st state, streams []int) {
	line := int(stmt.Pos().Line())
	for _, fd := range streams {
		t := st[fd]
		if t.from >= 0 && w.passed != nil {
			w.passed[t.from] = true
		}
		if !t.file {
			continue
		}
		key := fmt.Sprintf("%d:%d", line, fd)
		if w.seen[key] {
			continue
		}
		w.seen[key] = true
		w.found = append(w.found, Finding{Path: w.path, Line: line, Stream: streamName(fd), RedirLine: t.line, fd: fd})
	}
}

// apply is the descriptors after a statement's redirections, in order.
func apply(redirs []*syntax.Redirect, in state) state {
	st := in
	for _, r := range redirs {
		fd, ok := redirFD(r)
		if !ok {
			continue
		}
		line := int(r.Pos().Line())
		switch kinds[r.Op] {
		case kindBoth:
			t := fileTarget(r.Word, line, st)
			st[1], st[2] = t, t
		case kindDup:
			dup(&st, fd, r, line)
		case kindHere:
			st[fd] = safe
		case kindFile:
			st[fd] = fileTarget(r.Word, line, st)
		}
	}
	return st
}

// redirKind is what a redirection operator does to its descriptor.
type redirKind int

const (
	kindFile redirKind = iota // < > >> <> >|: the descriptor is the word's file
	kindBoth                  // &> &>>: stdout and stderr are the word's file
	kindDup                   // <& >&: another descriptor, closed, or (csh) a file
	kindHere                  // << <<- <<<: the shell's own here-document
)

// kinds is every operator that is not a plain file redirection.
var kinds = map[syntax.RedirOperator]redirKind{
	syntax.RdrAll: kindBoth, syntax.AppAll: kindBoth, syntax.RdrAllClob: kindBoth, syntax.AppAllClob: kindBoth,
	syntax.DplOut: kindDup, syntax.DplIn: kindDup,
	syntax.Hdoc: kindHere, syntax.DashHdoc: kindHere, syntax.WordHdoc: kindHere,
}

// inputOps are the operators whose default descriptor is stdin.
var inputOps = map[syntax.RedirOperator]bool{
	syntax.RdrIn: true, syntax.RdrInOut: true, syntax.DplIn: true,
	syntax.Hdoc: true, syntax.DashHdoc: true, syntax.WordHdoc: true,
}

// dup is `N>&M`, `N<&M`, `N>&-` and the csh form `>&FILE` (both output streams to a file).
func dup(st *state, fd int, r *syntax.Redirect, line int) {
	text := flatten(r.Word)
	if text == "-" {
		st[fd] = safe
		return
	}
	if m, err := strconv.Atoi(text); err == nil {
		if m >= 0 && m < fds {
			st[fd] = st[m]
		} else {
			st[fd] = safe
		}
		return
	}
	t := fileTarget(r.Word, line, *st)
	if r.N == nil && r.Op == syntax.DplOut {
		st[1], st[2] = t, t
		return
	}
	st[fd] = t
}

// redirFD is the descriptor a redirection sets: its number, else 0 for input and 1 for output. A
// named descriptor (`{fd}>`) and one past those followed are not followed.
func redirFD(r *syntax.Redirect) (int, bool) {
	if r.N != nil {
		n, err := strconv.Atoi(r.N.Value)
		return n, err == nil && n < fds
	}
	if inputOps[r.Op] {
		return 0, true
	}
	return 1, true
}

// devFD is a /dev path that is another of this process's descriptors.
var devFD = map[string]int{"/dev/stdin": 0, "/dev/stdout": 1, "/dev/stderr": 2}

// fileTarget is what a redirection to a word points the descriptor at: a process substitution
// is a pipe, a /dev path is a device lxc-attach leaves the caller able to use (or, for
// /dev/std* and /dev/fd/N, that descriptor again), and anything else is a file.
func fileTarget(word *syntax.Word, line int, st state) target {
	if word == nil {
		return safe
	}
	if len(word.Parts) == 1 {
		if _, isProc := word.Parts[0].(*syntax.ProcSubst); isProc {
			return safe
		}
	}
	text := flatten(word)
	if m, ok := devFD[text]; ok {
		return st[m]
	}
	if rest, ok := strings.CutPrefix(text, "/dev/fd/"); ok {
		if m, err := strconv.Atoi(rest); err == nil && m >= 0 && m < fds {
			return st[m]
		}
	}
	if strings.HasPrefix(text, "/dev/") {
		return safe
	}
	return target{file: true, line: line, from: -1}
}

// fixtureRe is the comment that marks the next line of the fixture as one the rule reports.
var fixtureRe = regexp.MustCompile(`^\s*# ruleid: ` + regexp.QuoteMeta(RuleID) + `\s*$`)

// FixtureMarks is the lines of the fixture its `# ruleid:` comments mark.
func FixtureMarks(text string) []int {
	var marks []int
	for number, line := range strings.Split(text, "\n") {
		if fixtureRe.MatchString(line) {
			marks = append(marks, number+2)
		}
	}
	return marks
}

// FixtureFindings is the distinct lines the rule reports in the fixture.
func FixtureFindings(text string) ([]int, error) {
	found, err := Analyze([]Source{{Path: FixturePath, Text: []byte(text)}})
	if err != nil {
		return nil, err
	}
	lines := make([]int, 0, len(found))
	for _, f := range found {
		lines = append(lines, f.Line)
	}
	return slices.Compact(lines), nil
}
