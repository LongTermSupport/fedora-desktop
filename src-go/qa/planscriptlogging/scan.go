package planscriptlogging

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/discovery"
)

// planRoot holds the plan folders.
const planRoot = "CLAUDE/Plan"

// headBytes is how much of a file is read to see its shebang.
const headBytes = 1024

// activePlan is the name of a plan folder still being worked on: digits, a hyphen, then a name.
var activePlan = regexp.MustCompile(`^[0-9]+-.`)

// hit is one offending line of a script.
type hit struct {
	path string
	line int
	text string
}

// result is what a scan found: the scripts examined, each offence as the text that is printed
// for it, and the reasons the run could not complete.
type result struct {
	examined int
	findings []string
	problems []string

	scriptHits []hit
	dirHits    []string
}

// scan walks every active plan folder.
func scan(fsys fs.FS) result {
	var r result
	entries, err := fs.ReadDir(fsys, planRoot)
	if err != nil {
		r.problems = append(r.problems, fmt.Sprintf("cannot read %s: %v", planRoot, err))
		return r
	}
	folders := 0
	for _, entry := range entries {
		if !activePlan.MatchString(entry.Name()) {
			continue
		}
		folder := path.Join(planRoot, entry.Name())
		switch {
		case entry.Type()&fs.ModeSymlink != 0:
			r.problems = append(r.problems, folder+" is a symlink; it is not followed, so its scripts would go unread")
		case entry.IsDir():
			folders++
			r.walkFolder(fsys, folder)
		}
	}
	if folders == 0 && len(r.problems) == 0 {
		r.problems = append(r.problems, "no active plan folder under "+planRoot+"; nothing was checked")
	}
	slices.SortStableFunc(r.scriptHits, func(a, b hit) int {
		return cmp.Or(strings.Compare(a.path, b.path), cmp.Compare(a.line, b.line))
	})
	for _, h := range r.scriptHits {
		r.findings = append(r.findings, fmt.Sprintf("%s:%d:%s", h.path, h.line, h.text))
	}
	slices.Sort(r.dirHits)
	r.findings = append(r.findings, r.dirHits...)
	return r
}

// walkFolder examines one plan folder.
func (r *result) walkFolder(fsys fs.FS, folder string) {
	err := fs.WalkDir(fsys, folder, func(p string, d fs.DirEntry, walkErr error) error {
		switch {
		case walkErr != nil:
			r.problems = append(r.problems, fmt.Sprintf("cannot read %s: %v", p, walkErr))
			return fs.SkipDir
		case d.IsDir():
			if d.Name() == "logs" {
				r.dirHits = append(r.dirHits, p+": plan-local run-log directory")
			}
		case d.Type().IsRegular():
			r.examine(fsys, p)
		}
		return nil
	})
	if err != nil {
		r.problems = append(r.problems, fmt.Sprintf("cannot read %s: %v", folder, err))
	}
}

// examine reads a file; a plan script is parsed and its offences recorded.
func (r *result) examine(fsys fs.FS, p string) {
	if !strings.HasSuffix(p, ".bash") && !strings.HasSuffix(p, ".sh") {
		head, err := readHead(fsys, p)
		if err != nil {
			r.problems = append(r.problems, fmt.Sprintf("cannot read %s: %v", p, err))
			return
		}
		if !discovery.ShellShebang(firstLine(head)) {
			return
		}
	}
	src, err := fs.ReadFile(fsys, p)
	if err != nil {
		r.problems = append(r.problems, fmt.Sprintf("cannot read %s: %v", p, err))
		return
	}
	lines, err := offences(p, src)
	if err != nil {
		r.problems = append(r.problems, fmt.Sprintf("cannot parse %s: %v", p, err))
		return
	}
	r.examined++
	text := strings.Split(string(src), "\n")
	for _, n := range lines {
		r.scriptHits = append(r.scriptHits, hit{path: p, line: n, text: strings.TrimSpace(text[n-1])})
	}
}

// firstLine is the text up to the first newline.
func firstLine(data []byte) string {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		data = data[:i]
	}
	return string(data)
}

// readHead is the first headBytes bytes of a file.
func readHead(fsys fs.FS, p string) ([]byte, error) {
	f, err := fsys.Open(p)
	if err != nil {
		return nil, err
	}
	head, readErr := io.ReadAll(io.LimitReader(f, headBytes))
	return head, errors.Join(readErr, f.Close())
}

// offences parses a bash script and returns the first line of every bare `exec` that redirects
// into an output process substitution.
func offences(name string, src []byte) ([]int, error) {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(bytes.NewReader(src), name)
	if err != nil {
		return nil, err
	}
	var lines []int
	syntax.Walk(file, func(n syntax.Node) bool {
		if stmt, ok := n.(*syntax.Stmt); ok && isBareExec(stmt) && redirectsToProcSubst(stmt) {
			lines = append(lines, int(stmt.Pos().Line()))
		}
		return true
	})
	slices.Sort(lines)
	return lines, nil
}

// isBareExec is a statement whose command is `exec` with no command after it, so the shell
// stays and its own redirections last.
func isBareExec(stmt *syntax.Stmt) bool {
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	return ok && len(call.Args) == 1 && call.Args[0].Lit() == "exec"
}

// redirectsToProcSubst is a redirection whose target is an output process substitution `>(...)`.
func redirectsToProcSubst(stmt *syntax.Stmt) bool {
	return slices.ContainsFunc(stmt.Redirs, func(r *syntax.Redirect) bool {
		if r.Word == nil {
			return false
		}
		return slices.ContainsFunc(r.Word.Parts, func(part syntax.WordPart) bool {
			sub, ok := part.(*syntax.ProcSubst)
			return ok && sub.Op == syntax.CmdOut
		})
	})
}
