// Package nokill is qa-nokill-containerwatch: the L0 gate that holds the container-watch watchdog
// to never signalling a process and never ending a container, with one audited exception
// (helpers/containerwatch/containment.py may build a graceful stop and nothing else). It reads
// the watchdog's files as text and matches call-site syntax, so the word "kill" stays free in
// guidance strings and prose. The only process it needs, git, is injected through Env.Published.
// SPEC-qa-nokill-containerwatch.md is its specification.
package nokill

import (
	"regexp"
	"slices"
	"strings"
)

// Finding is one forbidden call site.
type Finding struct {
	Path string
	Line int
	Rule string
	Text string
}

// rule is a named set of patterns. A pattern's named group "at" marks where the finding is
// reported (the call may start on an earlier line than the word that condemns it); without
// one, the start of the match is.
type rule struct {
	name     string
	patterns []*regexp.Regexp
}

func newRule(name string, patterns ...string) rule {
	r := rule{name: name}
	for _, p := range patterns {
		r.patterns = append(r.patterns, regexp.MustCompile(p))
	}
	return r
}

const (
	// lifecycleVerbs end or freeze a container, which no signal rule can see: `podman stop`
	// delivers no signal to any pid, it asks the engine to.
	lifecycleVerbs = `(?:stop|kill|rm|pause|unpause|restart)`
	// killNames are the programs that deliver a signal.
	killNames = `(?:kill|pkill|killall)`
	quoted    = `["']`
)

// signalRules apply to every file, containment.py included: it may ask an engine to stop a
// container and may never signal a process itself.
var signalRules = []rule{
	newRule("os-kill", `\bos\.(?:kill|killpg|abort)\s*\(`),
	newRule("os-import-kill", `(?m)^[ \t]*from[ \t]+(?:os|signal)[ \t]+import\b[^\n#]*\b(?:kill|killpg|abort|pthread_kill|raise_signal|SIG[A-Z_0-9]*)\b`),
	newRule("signal-const", `\bsignal\.(?:SIG[A-Z]|raise_signal\b|pthread_kill\b)`),
	newRule("send-signal", `\.(?:pidfd_)?send_signal\s*\(`),
	newRule("force-exit", `\.force_exit\s*\(`),
	newRule("proc-kill", `\.(?:kill|terminate)\s*\(`),
	newRule("argv-kill",
		`[\[(]\s*(?P<at>`+quoted+killNames+quoted+`)`,
		`(?m)(?:^|,)\s*(?P<at>`+quoted+killNames+quoted+`)\s*[,\])]`),
	newRule("shell-kill", `\b(?:pkill|killall)[ \t]|\bkill[ \t]+-`),
}

// lifecycleRules apply to every file except the exception. They match an argv, never prose: the
// report tells a human `podman stop <container>`, and a gate that could not tell advice from an
// argv would force the advice out of the report to stay green.
var lifecycleRules = []rule{
	newRule("engine-literal",
		`(?P<at>`+quoted+`(?:[^"'\n]*/)?(?:podman|docker)`+quoted+`)\s*,\s*(?:`+quoted+`container`+quoted+`\s*,\s*)?`+quoted+lifecycleVerbs+quoted),
	newRule("engine-variable",
		`\b(?P<at>(?:\w+\.)*\w*engine)\s*,\s*(?:`+quoted+`container`+quoted+`\s*,\s*)?`+quoted+lifecycleVerbs+quoted),
}

// containmentRules are what the one exception may not name at all: each verb is a different harm
// (kill denies the workload its shutdown path, rm destroys it, pause freezes it while it still
// holds the transient units that are the damage, a force or signal flag turns a graceful stop
// into any of them).
var containmentRules = []rule{
	newRule("containment-verb", quoted+`(?:kill|rm|pause|unpause|restart)`+quoted),
	newRule("containment-force", `--force|`+quoted+`-f`+quoted),
	newRule("containment-signal", `--signal`),
}

// Judge finds the forbidden call sites of one file's text, in line order, each line once under
// the first rule that matches it. exception is true for containment.py alone.
func Judge(path, text string, exception bool) []Finding {
	rules := slices.Concat(signalRules, containmentOrLifecycle(exception))
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	byLine := map[int]string{}
	for _, r := range rules {
		for _, re := range r.patterns {
			at := max(re.SubexpIndex("at"), 0)
			for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
				line := strings.Count(text[:m[2*at]], "\n") + 1
				if _, seen := byLine[line]; !seen {
					byLine[line] = r.name
				}
			}
		}
	}
	found := make([]Finding, 0, len(byLine))
	for line, name := range byLine {
		found = append(found, Finding{Path: path, Line: line, Rule: name, Text: lines[line-1]})
	}
	slices.SortFunc(found, func(a, b Finding) int { return a.Line - b.Line })
	return found
}

func containmentOrLifecycle(exception bool) []rule {
	if exception {
		return containmentRules
	}
	return lifecycleRules
}
