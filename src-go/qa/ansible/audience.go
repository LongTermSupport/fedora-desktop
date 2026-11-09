package ansible

import (
	"regexp"
	"strings"
)

// The audience a play is for (Plan 00228, CLAUDE/AnsibleStyle.md "Install Audience"): every
// play declares vars.audience, all or developer, and a developer play ends itself on a
// consumer install with this one task.
const (
	audienceGuardName = "Audience guard — end play on a consumer install"
	audienceGuardWhen = "install_audience != 'developer'"
	audienceDeveloper = "developer"
	// tasksKey is a play's tasks keyword, and the name of the top-level directory of task files.
	tasksKey = "tasks"
)

var (
	audienceKey  = regexp.MustCompile(`^    audience:[[:space:]]*`)
	listTaskName = regexp.MustCompile(`^    - name:[[:space:]]*`)
	taskItem     = regexp.MustCompile(`^    - `)
	runSection   = regexp.MustCompile(`^  (pre_tasks|roles|tasks):[[:space:]]*$`)
)

// audienceInfo is what the line scan reads from a play: the vars.audience entries and the
// audience guard tasks.
type audienceInfo struct {
	count   int
	value   string
	guards  int
	guardOK bool
	// guardFirst: the first task the play runs (pre_tasks, then roles, then tasks, whatever
	// order the file lists them in) is the audience guard, the scope guard aside.
	guardFirst bool
}

// audienceScanner is the state of the audience line scan.
type audienceScanner struct {
	info    audienceInfo
	inVars  bool
	inGuard bool
	meta    string
	when    string
	// section is the play's run section being read (pre_tasks, roles, tasks), or "".
	section string
	// pre and tasks are the names of each section's tasks in order ("" for an unnamed one).
	pre, tasks []string
	roles      int
}

// scanAudience reads vars.audience and every audience guard task, in tasks or pre_tasks.
func scanAudience(lines []string) audienceInfo {
	var s audienceScanner
	for _, line := range lines {
		s.line(line)
	}
	s.endGuard()
	s.info.guardFirst = s.guardIsFirst()
	return s.info
}

// guardIsFirst says whether the first task the play runs is the audience guard. Ansible runs
// pre_tasks, then roles, then tasks; in tasks only the scope guard's two tasks may precede it.
func (s *audienceScanner) guardIsFirst() bool {
	if len(s.pre) > 0 {
		return s.pre[0] == audienceGuardName
	}
	if s.roles > 0 {
		return false
	}
	rest := s.tasks
	if len(rest) >= 2 && rest[0] == guardAssertName && rest[1] == guardEndName {
		rest = rest[2:]
	}
	return len(rest) > 0 && rest[0] == audienceGuardName
}

// trackSection follows which run section a line is in and records each of its list items.
func (s *audienceScanner) trackSection(line string) {
	if m := runSection.FindStringSubmatch(line); m != nil {
		s.section = m[1]
		return
	}
	if startsNonBlank.MatchString(line) || indent2Key.MatchString(line) {
		s.section = ""
		return
	}
	if !taskItem.MatchString(line) {
		return
	}
	name := ""
	if loc := listTaskName.FindStringIndex(line); loc != nil {
		name = line[loc[1]:]
	}
	switch s.section {
	case "pre_tasks":
		s.pre = append(s.pre, name)
	case "roles":
		s.roles++
	case tasksKey:
		s.tasks = append(s.tasks, name)
	}
}

func (s *audienceScanner) line(line string) {
	if varsHeader.MatchString(line) {
		s.inVars = true
		s.section = ""
		return
	}
	if s.inVars && s.varsLine(line) {
		return
	}
	s.trackSection(line)
	if taskItem.MatchString(line) || startsNonBlank.MatchString(line) || indent2Key.MatchString(line) {
		s.endGuard()
	}
	if loc := listTaskName.FindStringIndex(line); loc != nil && line[loc[1]:] == audienceGuardName {
		s.inGuard = true
		s.info.guards++
		return
	}
	if !s.inGuard {
		return
	}
	if loc := metaKey.FindStringIndex(line); loc != nil {
		s.meta = cleanValue(line[loc[1]:])
	}
	if loc := whenKey.FindStringIndex(line); loc != nil {
		s.when = cleanValue(line[loc[1]:])
	}
}

// varsLine handles a line inside the play's vars block and says whether it consumed it.
func (s *audienceScanner) varsLine(line string) bool {
	if loc := audienceKey.FindStringIndex(line); loc != nil {
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

// endGuard closes the guard task being read, recording whether it is the canonical one.
func (s *audienceScanner) endGuard() {
	if s.inGuard && s.meta == guardEndMeta && s.when == audienceGuardWhen {
		s.info.guardOK = true
	}
	s.inGuard, s.meta, s.when = false, "", ""
}

// audience checks each playbook's vars.audience and, for developer, its guard. A multi-play
// file is the scope check's finding, and an archived play is not checked.
func audience(playbooks []playbook, root string) found {
	var result found
	for _, pb := range playbooks {
		if pb.hosts > 1 || strings.Contains(root+"/"+pb.rel, archivedPath) {
			continue
		}
		if p := audienceProblem(scanAudience(pb.lines)); p.message != "" {
			result.add("  ERROR (audience): "+pb.rel+" — "+p.message, Entry{File: pb.rel, Why: p.short})
		}
	}
	return result
}

// audienceProblem is the audience violation of a scanned play, if it has one.
func audienceProblem(info audienceInfo) problem {
	switch {
	case info.count == 0:
		return problem{"missing vars.audience (need exactly one of all|developer, CLAUDE/AnsibleStyle.md)", "missing vars.audience"}
	case info.count > 1:
		return problem{"multiple vars.audience entries declared (exactly one required)", "multiple vars.audience entries"}
	case info.value != "all" && info.value != audienceDeveloper:
		return problem{
			"invalid vars.audience value: " + info.value + " (must be exactly one of all|developer)",
			"invalid vars.audience: " + info.value,
		}
	case info.value == audienceDeveloper && (info.guards != 1 || !info.guardOK):
		return problem{
			"audience=developer requires exactly one audience guard task (meta: end_play when: " + audienceGuardWhen + ")",
			"audience=developer missing/incorrect guard",
		}
	case info.value == audienceDeveloper && !info.guardFirst:
		return problem{
			"the audience guard must be the first task the play runs (only the scope guard may precede it, CLAUDE/AnsibleStyle.md)",
			"audience guard not the first task",
		}
	case info.value == "all" && info.guards > 0:
		return problem{"unnecessary audience guard on an audience=all play (remove it)", "unnecessary guard on audience=all play"}
	}
	return problem{}
}
