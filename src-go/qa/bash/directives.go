package bash

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// allowlistName is the ratchet of recorded disable directives, in the scripts directory
// (SPEC-qa-bash.md S17).
const allowlistName = "shellcheck-disable.allowlist"

// codeAll is the disable value that hides every check; it is never allowable.
const codeAll = "all"

// directive is one shellcheck directive comment found in a file.
type directive struct {
	line  int
	codes []string // the normalised disable codes; nil when the comment has no disable key
	shell string   // the shell= value, or ""
}

// dfinding is one thing wrong with suppression: file and line (0: the file as a whole).
type dfinding struct {
	file string
	line int
	msg  string
}

// directiveRe finds a directive comment: a # that begins a word, optional blanks, shellcheck, a
// blank, then key=value words.
var directiveRe = regexp.MustCompile(`(?:^|[\s;&|(){])#\s*shellcheck\s+((?:[A-Za-z][A-Za-z-]*=\S*\s*)+)`)

// codeRe is a disable value that shellcheck accepts and the allowlist can record.
var codeRe = regexp.MustCompile(`^SC[0-9]+(?:-SC[0-9]+)?$`)

// normaliseCode makes 2086, sc2086 and SC2086 one token, and keeps a range as one token.
func normaliseCode(raw string) string {
	if strings.EqualFold(raw, codeAll) {
		return codeAll
	}
	parts := strings.Split(strings.ToUpper(raw), "-")
	for i, p := range parts {
		if p != "" && p[0] >= '0' && p[0] <= '9' {
			parts[i] = "SC" + p
		}
	}
	return strings.Join(parts, "-")
}

// scanDirectives finds every directive comment in a file's text.
func scanDirectives(text string) []directive {
	var out []directive
	for n, line := range strings.Split(text, "\n") {
		m := directiveRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		d := directive{line: n + 1}
		for word := range strings.FieldsSeq(m[1]) {
			kv := strings.SplitN(word, "=", 2)
			switch kv[0] {
			case "disable":
				d.codes = []string{}
				for code := range strings.SplitSeq(kv[1], ",") {
					if code != "" {
						d.codes = append(d.codes, normaliseCode(code))
					}
				}
			case "shell":
				d.shell = kv[1]
			}
		}
		out = append(out, d)
	}
	return out
}

// shebangDialect is the shell a first line names: the basename of its interpreter, or for env the
// first word that is neither an option nor an assignment. "" when there is no shebang.
func shebangDialect(text string) string {
	for first := range strings.SplitSeq(text, "\n") {
		return dialectOf(strings.TrimRight(first, "\r"))
	}
	return ""
}

// dialectOf is shebangDialect for the first line alone.
func dialectOf(first string) string {
	rest, ok := strings.CutPrefix(first, "#!")
	if !ok {
		return ""
	}
	words := strings.Fields(rest)
	if len(words) == 0 {
		return ""
	}
	interp := path.Base(words[0])
	if interp != "env" {
		return interp
	}
	for _, w := range words[1:] {
		if !strings.HasPrefix(w, "-") && !strings.Contains(w, "=") {
			return path.Base(w)
		}
	}
	return ""
}

// record is one allowlist entry: how many disable directives a file may hold, and which codes.
type record struct {
	count int
	codes []string
}

// parseAllowlist reads the allowlist's text; every malformed line is an error naming it.
func parseAllowlist(text string) (map[string]record, error) {
	out := map[string]record{}
	for n, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		file, rec, err := parseRecord(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n+1, err)
		}
		if _, dup := out[file]; dup {
			return nil, fmt.Errorf("line %d: %s is listed twice", n+1, file)
		}
		out[file] = rec
	}
	return out, nil
}

// parseRecord reads one allowlist line: path, count, codes.
func parseRecord(line string) (string, record, error) {
	fields := strings.Split(line, "\t")
	if len(fields) != 3 || fields[0] == "" {
		return "", record{}, fmt.Errorf("want path<TAB>count<TAB>codes, got %q", line)
	}
	count, err := strconv.Atoi(fields[1])
	if err != nil || count < 1 {
		return "", record{}, fmt.Errorf("%q is not a count of at least 1", fields[1])
	}
	codes := strings.Split(fields[2], ",")
	for _, c := range codes {
		if c == codeAll {
			return "", record{}, errors.New("disable=all is never allowable")
		}
		if !codeRe.MatchString(c) {
			return "", record{}, fmt.Errorf("%q is not a shellcheck code (SC1234 or SC1000-SC1100)", c)
		}
	}
	if !slices.IsSorted(codes) || len(slices.Compact(slices.Clone(codes))) != len(codes) {
		return "", record{}, errors.New("the codes must be sorted and unique")
	}
	return fields[0], record{count: count, codes: codes}, nil
}

// allowlistRel is the allowlist's path relative to the root, slash-separated.
func (g *gate) allowlistRel() string {
	full := filepath.Join(g.script, allowlistName)
	if rel, err := filepath.Rel(g.root, full); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(full)
}

// loadAllowlist reads the allowlist; an absent file is an empty list.
func (g *gate) loadAllowlist() (map[string]record, error) {
	data, err := os.ReadFile(filepath.Join(g.root, g.allowlistRel()))
	if os.IsNotExist(err) {
		return map[string]record{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", g.allowlistRel(), err)
	}
	list, err := parseAllowlist(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", g.allowlistRel(), err)
	}
	return list, nil
}

// observed is what one file holds: its directives and the dialect its shebang names.
type observed struct {
	directives []directive
	dialect    string
}

// disables are the number of disable directives, the line of the first, and the sorted unique
// codes they name apart from all.
func (o observed) disables() (int, int, []string) {
	n, first := 0, 0
	var codes []string
	for _, d := range o.directives {
		if d.codes == nil {
			continue
		}
		if n == 0 {
			first = d.line
		}
		n++
		for _, c := range d.codes {
			if c != codeAll && !slices.Contains(codes, c) {
				codes = append(codes, c)
			}
		}
	}
	slices.Sort(codes)
	return n, first, codes
}

// judgeFile is every suppression finding for one file against its record (rec may be nil).
func (g *gate) judgeFile(file string, o observed, rec *record) []dfinding {
	var out []dfinding
	for _, d := range o.directives {
		if slices.Contains(d.codes, codeAll) {
			out = append(out, dfinding{file, d.line, "disable=all hides every check and is never allowable"})
		}
		if d.shell != "" && o.dialect != "" && d.shell != o.dialect {
			out = append(out, dfinding{file, d.line, fmt.Sprintf("shell=%s contradicts the %s shebang and hides that dialect's findings", d.shell, o.dialect)})
		}
	}
	n, first, codes := o.disables()
	list := g.allowlistRel()
	switch {
	case rec == nil && n > 0:
		out = append(out, dfinding{file, first, fmt.Sprintf("%d shellcheck disable directive(s) (%s) not recorded in %s: remove them (the list only shrinks)", n, strings.Join(codes, ","), list)})
	case rec != nil && n == 0:
		out = append(out, dfinding{file, 0, fmt.Sprintf("stale record in %s: the file holds no disable directive; remove the entry", list)})
	case rec != nil:
		if n > rec.count {
			out = append(out, dfinding{file, first, fmt.Sprintf("grew past its record in %s: %d disable directives, %d recorded", list, n, rec.count)})
		}
		if extra := missingFrom(codes, rec.codes); len(extra) > 0 {
			out = append(out, dfinding{file, first, fmt.Sprintf("disable of %s is not in its record in %s", strings.Join(extra, ","), list)})
		}
		if lost := missingFrom(rec.codes, codes); n < rec.count || len(lost) > 0 {
			out = append(out, dfinding{file, 0, fmt.Sprintf("under its record in %s (%d directives, %d recorded; no longer disabled: %s): tighten the record", list, n, rec.count, strings.Join(lost, ","))})
		}
	}
	return out
}

// missingFrom are the codes of a that b lacks.
func missingFrom(a, b []string) []string {
	var out []string
	for _, c := range a {
		if !slices.Contains(b, c) {
			out = append(out, c)
		}
	}
	return out
}

// scanFiles reads each file under the root and finds its directives.
func (g *gate) scanFiles(files []string) (map[string]observed, error) {
	out := map[string]observed{}
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(g.root, rel))
		if err != nil {
			return nil, fmt.Errorf("cannot read %s to scan it for directives: %w", rel, err)
		}
		text := string(data)
		out[rel] = observed{directives: scanDirectives(text), dialect: shebangDialect(text)}
	}
	return out, nil
}

// directiveFindings is S16-S19 over files. whole also judges the records of paths that are not
// among them and compares the list with the base.
func (g *gate) directiveFindings(ctx context.Context, files []string, whole bool) ([]dfinding, error) {
	list, err := g.loadAllowlist()
	if err != nil {
		return nil, err
	}
	seen, err := g.scanFiles(files)
	if err != nil {
		return nil, err
	}
	var out []dfinding
	for _, rel := range files {
		var rec *record
		if r, ok := list[rel]; ok {
			rec = &r
		}
		out = append(out, g.judgeFile(rel, seen[rel], rec)...)
	}
	if whole {
		for rel := range list {
			if _, gated := seen[rel]; !gated {
				out = append(out, dfinding{rel, 0, fmt.Sprintf("stale record in %s: the path is not a gated shell file; remove the entry", g.allowlistRel())})
			}
		}
		more, err := g.againstBase(ctx, list)
		if err != nil {
			return nil, err
		}
		out = append(out, more...)
	}
	slices.SortStableFunc(out, func(a, b dfinding) int {
		if c := strings.Compare(a.file, b.file); c != 0 {
			return c
		}
		return a.line - b.line
	})
	return out, nil
}

// git runs git from the root and returns its stdout; a non-zero status is an error.
func (g *gate) git(ctx context.Context, args ...string) (string, error) {
	res, err := g.runTool(ctx, probeTimeout, proc.Cmd{Argv: append([]string{"git"}, args...)})
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", fmt.Errorf("git %s exited %d: %s", strings.Join(args, " "), res.Code, strings.TrimSpace(res.Stderr))
	}
	return res.Stdout, nil
}

// againstBase compares the list with the copy at the merge-base of HEAD and origin's default
// branch: nothing added, nothing raised. A list with no record is not compared.
func (g *gate) againstBase(ctx context.Context, list map[string]record) ([]dfinding, error) {
	if len(list) == 0 {
		return nil, nil
	}
	rel := g.allowlistRel()
	head, err := g.git(ctx, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD")
	if err != nil {
		return nil, fmt.Errorf("no base to compare %s with: origin's default branch is not known here (run: git remote set-head origin --auto): %w", rel, err)
	}
	base, err := g.git(ctx, "merge-base", "HEAD", strings.TrimSpace(head))
	if err != nil {
		return nil, fmt.Errorf("no base to compare %s with: %w", rel, err)
	}
	ref := strings.TrimSpace(base)
	listed, err := g.git(ctx, "ls-tree", "--name-only", ref, "--", rel)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(listed) == "" {
		return nil, nil
	}
	text, err := g.git(ctx, "show", ref+":"+rel)
	if err != nil {
		return nil, err
	}
	old, err := parseAllowlist(text)
	if err != nil {
		return nil, fmt.Errorf("%s at the base: %w", rel, err)
	}
	var out []dfinding
	for file, rec := range list {
		was, had := old[file]
		switch {
		case !had:
			out = append(out, dfinding{file, 0, fmt.Sprintf("record added in %s (the base has none for this file); the list only shrinks, so remove the directive instead", rel)})
		case rec.count > was.count:
			out = append(out, dfinding{file, 0, fmt.Sprintf("record raised in %s (%d > %d at the base); records only go down", rel, rec.count, was.count)})
		default:
			if extra := missingFrom(rec.codes, was.codes); len(extra) > 0 {
				out = append(out, dfinding{file, 0, fmt.Sprintf("record raised in %s (new codes %s); records only go down", rel, strings.Join(extra, ","))})
			}
		}
	}
	return out, nil
}

// text is the finding as one stdout line (S19).
func (f dfinding) text() string {
	if f.line == 0 {
		return fmt.Sprintf("✗ suppression: %s: %s", f.file, f.msg)
	}
	return fmt.Sprintf("✗ suppression: %s:%d %s", f.file, f.line, f.msg)
}

// errorPart is the finding as a piece of the file's error string.
func (f dfinding) errorPart() string {
	if f.line == 0 {
		return "directive: " + f.msg
	}
	return fmt.Sprintf("directive:%d %s", f.line, f.msg)
}

// runDirectives is --directives: S16-S19 over the named files alone (S20).
func (g *gate) runDirectives(ctx context.Context, files []string) (ExitCode, error) {
	found, err := g.directiveFindings(ctx, files, false)
	if err != nil {
		return ExitCannotRun, err
	}
	for _, f := range found {
		fmt.Fprintln(&g.stdout, f.text())
	}
	if len(found) > 0 {
		return ExitFailed, nil
	}
	return ExitOK, nil
}
