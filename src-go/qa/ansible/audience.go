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
)

var (
	audienceKey  = regexp.MustCompile(`^    audience:[[:space:]]*`)
	listTaskName = regexp.MustCompile(`^    - name:[[:space:]]*`)
	taskItem     = regexp.MustCompile(`^    - `)
)

// audienceInfo is what the line scan reads from a play: the vars.audience entries and the
// audience guard tasks.
type audienceInfo struct {
	count   int
	value   string
	guards  int
	guardOK bool
}

// audienceScanner is the state of the audience line scan.
type audienceScanner struct {
	info    audienceInfo
	inVars  bool
	inGuard bool
	meta    string
	when    string
}

// scanAudience reads vars.audience and every audience guard task, in tasks or pre_tasks.
func scanAudience(lines []string) audienceInfo {
	var s audienceScanner
	for _, line := range lines {
		s.line(line)
	}
	s.endGuard()
	return s.info
}

func (s *audienceScanner) line(line string) {
	if varsHeader.MatchString(line) {
		s.inVars = true
		return
	}
	if s.inVars && s.varsLine(line) {
		return
	}
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
	case info.value != "all" && info.value != "developer":
		return problem{
			"invalid vars.audience value: " + info.value + " (must be exactly one of all|developer)",
			"invalid vars.audience: " + info.value,
		}
	case info.value == "developer" && (info.guards != 1 || !info.guardOK):
		return problem{
			"audience=developer requires exactly one audience guard task (meta: end_play when: " + audienceGuardWhen + ")",
			"audience=developer missing/incorrect guard",
		}
	case info.value == "all" && info.guards > 0:
		return problem{"unnecessary audience guard on an audience=all play (remove it)", "unnecessary guard on audience=all play"}
	}
	return problem{}
}
