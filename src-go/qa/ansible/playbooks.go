package ansible

import (
	"fmt"
	"io/fs"
	"regexp"
	"strings"
)

// playbookShebang is the first line of every playbook, byte for byte;
// scripts/make-playbooks-executable.bash writes it and explains it. The star is its own string
// so that no comment opener appears in the source.
const playbookShebang = `#!/usr/bin/env -S bash -c 'p=$(realpath "$0") && exec "${p%/playbooks/` + "*" + `}/run.bash" "$p" "$@"'`

// The canonical scope guard (CLAUDE/AnsibleStyle.md, "Provisioning Profile Self-Guard"): the
// first two tasks of a gnome or server play.
const (
	guardAssertName = "Scope guard — assert provisioning_profile is recognised"
	guardEndName    = "Scope guard — end play if provisioning_profile does not match declared scope"
	guardEndMeta    = "end_play"
	guardEndWhen    = "(scope == 'gnome' and provisioning_profile == 'server') or (scope == 'server' and provisioning_profile != 'server')"
)

// archivedPath is the part of a path that exempts a playbook from the scope check.
const archivedPath = "/optional/archived/"

// hostsLine is a play: a top-level "- hosts:" line, possibly indented.
var hostsLine = regexp.MustCompile(`^[[:space:]]*-[[:space:]]+hosts:`)

// playbook is a file under playbooks/ with at least one play.
type playbook struct {
	rel        string
	lines      []string
	executable bool
	// hosts is how many lines are plays.
	hosts int
}

// loadPlaybooks reads every YAML file under playbooks/ (vendor directories included, as find
// did) and keeps those with a "- hosts:" line.
func loadPlaybooks(fsys fs.FS) ([]playbook, error) {
	files, err := yamlFiles(fsys, "playbooks", false)
	if err != nil {
		return nil, err
	}
	var playbooks []playbook
	for _, rel := range files {
		pb, isPlaybook, err := readPlaybook(fsys, rel)
		if err != nil {
			return nil, err
		}
		if isPlaybook {
			playbooks = append(playbooks, pb)
		}
	}
	return playbooks, nil
}

func readPlaybook(fsys fs.FS, rel string) (playbook, bool, error) {
	data, err := fs.ReadFile(fsys, rel)
	if err != nil {
		return playbook{}, false, fmt.Errorf("a file could not be read: %w", err)
	}
	info, err := fs.Stat(fsys, rel)
	if err != nil {
		return playbook{}, false, fmt.Errorf("a file could not be examined: %w", err)
	}
	lines := splitLines(data)
	hosts := 0
	for _, line := range lines {
		if hostsLine.MatchString(line) {
			hosts++
		}
	}
	pb := playbook{rel: rel, lines: lines, executable: info.Mode().Perm()&0o111 != 0, hosts: hosts}
	return pb, hosts > 0, nil
}

// found is one check's violations: the stdout lines, and the entries recorded in the report.
type found struct {
	out     []string
	entries []Entry
}

func (f *found) add(line string, entry Entry) {
	f.out = append(f.out, line)
	f.entries = append(f.entries, entry)
}

// hygiene checks each playbook's first line and execute bit.
func hygiene(playbooks []playbook) found {
	var result found
	for _, pb := range playbooks {
		var problems []string
		if len(pb.lines) == 0 || pb.lines[0] != playbookShebang {
			problems = append(problems, "missing shebang")
		}
		if !pb.executable {
			problems = append(problems, "not executable")
		}
		if len(problems) == 0 {
			continue
		}
		why := strings.Join(problems, " + ")
		result.add("  ERROR (hygiene): "+pb.rel+" — "+why, Entry{File: pb.rel, Why: why})
	}
	return result
}

// scopeInfo is what the line scan reads from a play: the vars.scope entries and the first two
// tasks' fields.
type scopeInfo struct {
	count        int
	value        string
	task1, task2 string
	meta, when   string
}

var (
	varsHeader     = regexp.MustCompile(`^  vars:[[:space:]]*$`)
	scopeKey       = regexp.MustCompile(`^    scope:[[:space:]]*`)
	startsNonBlank = regexp.MustCompile(`^[^[:space:]]`)
	indent2Key     = regexp.MustCompile(`^  [^ ]`)
	tasksHeader    = regexp.MustCompile(`^  tasks:[[:space:]]*$`)
	taskName       = regexp.MustCompile(`^    - name:[[:space:]]*`)
	metaKey        = regexp.MustCompile(`^      ansible\.builtin\.meta:[[:space:]]*`)
	whenKey        = regexp.MustCompile(`^      when:[[:space:]]*`)
)

// cleanValue is a scalar read as the awk did: cut at the first #, the whitespace before it
// dropped, then one trailing carriage return.
func cleanValue(rest string) string {
	if at := strings.IndexByte(rest, '#'); at >= 0 {
		rest = strings.TrimRight(rest[:at], " \t\n\v\f\r")
	}
	return strings.TrimSuffix(rest, "\r")
}

// scanner is the state of the line scan of SPEC-qa-ansible.md section 4.4.1.
type scanner struct {
	info      scopeInfo
	inVars    bool
	inTasks   bool
	taskCount int
}

// scanScope reads vars.scope and the first two tasks' fields, rule for rule as the awk did.
func scanScope(lines []string) scopeInfo {
	var s scanner
	for _, line := range lines {
		s.line(line)
	}
	return s.info
}

func (s *scanner) line(line string) {
	if varsHeader.MatchString(line) {
		s.inVars = true
		return
	}
	if s.inVars && s.varsLine(line) {
		return
	}
	if tasksHeader.MatchString(line) {
		s.inTasks, s.taskCount = true, 0
		return
	}
	if s.inTasks {
		s.tasksLine(line)
	}
}

// varsLine handles a line inside the play's vars block and says whether it consumed it.
func (s *scanner) varsLine(line string) bool {
	if loc := scopeKey.FindStringIndex(line); loc != nil {
		if value := cleanValue(line[loc[1]:]); value != "" {
			s.info.count++
			s.info.value = value
		}
		return true
	}
	if startsNonBlank.MatchString(line) || indent2Key.MatchString(line) {
		s.inVars = false
	}
	return false
}

func (s *scanner) tasksLine(line string) {
	if loc := taskName.FindStringIndex(line); loc != nil {
		s.taskCount++
		switch s.taskCount {
		case 1:
			s.info.task1 = line[loc[1]:]
		case 2:
			s.info.task2 = line[loc[1]:]
		default:
			s.inTasks = false
		}
		return
	}
	if s.taskCount == 2 {
		if loc := metaKey.FindStringIndex(line); loc != nil {
			s.info.meta = cleanValue(line[loc[1]:])
			return
		}
		if loc := whenKey.FindStringIndex(line); loc != nil {
			s.info.when = cleanValue(line[loc[1]:])
			return
		}
	}
	if indent2Key.MatchString(line) {
		s.inTasks = false
	}
}

func (i scopeInfo) guardOK() bool {
	return i.task1 == guardAssertName && i.task2 == guardEndName && i.meta == guardEndMeta && i.when == guardEndWhen
}

// scope checks each playbook's vars.scope and, for gnome and server, its canonical guard.
func scope(playbooks []playbook, root string) found {
	var result found
	for _, pb := range playbooks {
		if strings.Contains(root+"/"+pb.rel, archivedPath) {
			continue
		}
		if p := scopeProblem(pb); p.message != "" {
			result.add("  ERROR (scope): "+pb.rel+" — "+p.message, Entry{File: pb.rel, Why: p.short})
		}
	}
	return result
}

// problem is a scope violation in the long text printed and the short text recorded; the zero
// value is no violation.
type problem struct {
	message, short string
}

// scopeProblem is the scope violation of a playbook, if it has one.
func scopeProblem(pb playbook) problem {
	if pb.hosts > 1 {
		return problem{
			fmt.Sprintf("file contains %d separate '- hosts:' plays; this gate cannot safely vouch for a multi-play file. "+
				"Split it (one play per file, matching every other playbook in the repo), then give each its own vars.scope (+ guard if needed).", pb.hosts),
			fmt.Sprintf("multi-play file: %d plays in one file, not supported", pb.hosts),
		}
	}
	info := scanScope(pb.lines)
	switch {
	case info.count == 0:
		return problem{"missing vars.scope (need exactly one of general|gnome|server)", "missing vars.scope"}
	case info.count > 1:
		return problem{"multiple vars.scope entries declared (exactly one required)", "multiple vars.scope entries"}
	case info.value != "general" && info.value != "gnome" && info.value != "server":
		return problem{
			"invalid vars.scope value: " + info.value + " (must be exactly one of general|gnome|server)",
			"invalid vars.scope: " + info.value,
		}
	case info.value == "general":
		if info.task1 == guardAssertName || info.task1 == guardEndName {
			return problem{
				"unnecessary scope guard on a general-scope play (guard never fires for general; remove it)",
				"unnecessary guard on general play",
			}
		}
	case !info.guardOK():
		return problem{
			"scope=" + info.value + " requires the 2-task canonical guard (assert + meta:end_play) as its first two tasks; missing or incorrect",
			"scope=" + info.value + " missing/incorrect guard",
		}
	}
	return problem{}
}
