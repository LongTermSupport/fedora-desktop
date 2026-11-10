package ansible

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The install-guard-trusts-record rule (CLAUDE/QA.md, "install-guard-trusts-record"). An
// install that is skipped when a record says it was done (a checksum, version or stamp file
// the install itself writes) is skipped too when what it installed has since been deleted or
// damaged while the record stayed: the play reports success, and what was installed is not
// there.
//
// The rule reads each play, and the task list outside a play, in run order, a block's when
// carried down to every task inside it. It reports two shapes.
//
// A record guard: a stat (path) or slurp (src) task with a register and no loop reads a file.
// The work it guards is every later task whose condition reads that register, and every task
// whose condition reads the register of a task already in the work (so a record written
// "when: install is changed" is the work's too). The rule reports the guard when the work
// makes the file it reads from nothing (copy with content, file state touch), so the file is
// a record rather than an artefact, and the work also changes something the condition does not
// check: a path written by copy, template, get_url, unarchive, file, lineinfile or blockinfile
// (or a command's creates) that no stat read by the condition covers, or a change with no path
// the rule can know (a command, a package, a service) when the condition reads no read-only
// probe of its own (a stat, slurp, find or command with changed_when false whose register is
// neither the record's nor the work's). Scratch is no change: a path under /tmp or /var/tmp, or
// under a tempfile task's path.
//
// A self-made marker: a command or shell task whose creates names a file its own text touches
// or redirects into, alongside other work: the marker records that the work ran once, and the
// work is never checked again.

// ruleRecordGuard is the rule ID, printed with every finding and the anchor of its section.
const ruleRecordGuard = "install-guard-trusts-record"

// guardedTask is a task with every condition it runs under: its own when and its blocks'.
type guardedTask struct {
	node   *yaml.Node
	module string
	when   string
}

// Argument keys, values and module names the rule reads.
const (
	argDest     = "dest"
	argPath     = "path"
	argCreates  = "creates"
	modFile     = "file"
	modCopy     = "copy"
	modStat     = "stat"
	modSlurp    = "slurp"
	modTempfile = "tempfile"
)

// loopKeys are the task keys that run a task over a list.
var loopKeys = []string{"loop", "with_items", "with_list"}

// writeModules are the modules whose target path the rule reads, and the keys that name it.
var writeModules = map[string][]string{
	modCopy: {argDest}, "template": {argDest}, "get_url": {argDest}, "unarchive": {argDest},
	modFile: {argPath, argDest}, modLineInFile: {argPath, argDest}, modBlockInFile: {argPath, argDest},
	modCommand: {argCreates}, modShell: {argCreates},
}

// probeModules read without changing; a register of one is a probe a condition can read.
var probeModules = map[string]bool{modStat: true, modSlurp: true, "find": true, "getent": true, "package_facts": true}

// scratchPath is a path under the system's temporary directories.
var scratchPath = regexp.MustCompile(`^/(?:var/)?tmp/`)

// installGuardTrustsRecord returns the rule's findings of one YAML file, "<rel>:<line>: <rule>:
// <why>", in document order. A file the YAML library cannot parse is left out: playUX reports it.
func installGuardTrustsRecord(rel string, data []byte) []string {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var findings []string
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return findings
		}
		if err != nil {
			return nil
		}
		for _, top := range doc.Content {
			for _, list := range taskSequences(top) {
				findings = append(findings, judgeRecordGuards(rel, list)...)
				findings = append(findings, judgeSelfMadeMarkers(rel, list)...)
			}
		}
	}
}

// taskSequences is the task lists of a document's top-level list that run in one sequence:
// each play's pre_tasks, tasks and post_tasks together, and the loose tasks outside a play
// together.
func taskSequences(top *yaml.Node) [][]guardedTask {
	if top.Kind != yaml.SequenceNode {
		return nil
	}
	var lists [][]guardedTask
	var loose []guardedTask
	for _, item := range top.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if mappingValue(item, "hosts") == nil {
			if mappingValue(item, "import_playbook") == nil {
				guardedTasks(&yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{item}}, "", &loose)
			}
			continue
		}
		var play []guardedTask
		for _, key := range []string{keyPreTasks, tasksKey, keyPostTasks} {
			guardedTasks(mappingValue(item, key), "", &play)
		}
		lists = append(lists, play)
	}
	return append(lists, loose)
}

// guardedTasks lists a task list's tasks in run order, blocks opened, each with the conditions
// it inherits joined to its own.
func guardedTasks(list *yaml.Node, inherited string, out *[]guardedTask) {
	if list == nil || list.Kind != yaml.SequenceNode {
		return
	}
	for _, task := range list.Content {
		if task.Kind != yaml.MappingNode {
			continue
		}
		when := inherited + "\n" + nodeText(mappingValue(task, "when"))
		if mappingValue(task, keyBlock) != nil {
			for _, key := range []string{keyBlock, keyRescue, keyAlways} {
				guardedTasks(mappingValue(task, key), when, out)
			}
			continue
		}
		t := guardedTask{node: task, when: when}
		if key := moduleOf(task); key != nil {
			t.module = shortName(key.Value)
		}
		*out = append(*out, t)
	}
}

// uxTaskOf is a guarded task as the shared task shape, for the shared argument readers.
func uxTaskOf(t guardedTask) uxTask {
	key := moduleOf(t.node)
	if key == nil {
		key = t.node.Content[0]
	}
	return uxTask{node: t.node, module: t.module, moduleKey: key}
}

// args is a task's argument mappings.
func (t guardedTask) args() []*yaml.Node { return managedArgs(uxTaskOf(t)) }

// looped is whether a task runs over a list.
func (t guardedTask) looped() bool {
	return slices.ContainsFunc(loopKeys, func(k string) bool { return mappingValue(t.node, k) != nil })
}

// statGuard is a stat or slurp task the rule can read: its register, its path and its line.
type statGuard struct {
	register string
	path     string
	line     string
	isStat   bool
}

// recordScan is what one sequence of tasks offers the rule.
type recordScan struct {
	tasks   []guardedTask
	guards  []statGuard
	scratch map[string]bool
	probes  map[string]bool
}

// judgeRecordGuards applies the record-guard shape to one sequence of tasks: one finding per
// file read, at its first guard.
func judgeRecordGuards(rel string, tasks []guardedTask) []string {
	s := recordScan{tasks: tasks, scratch: map[string]bool{}, probes: map[string]bool{}}
	for _, t := range tasks {
		register := scalarValue(t.node, "register")
		if register == "" {
			continue
		}
		switch {
		case t.module == modTempfile:
			s.scratch[register] = true
		case probeModules[t.module] || (t.module == modCommand || t.module == modShell) && isFalse(mappingValue(t.node, "changed_when")):
			s.probes[register] = true
		}
		if (t.module == modStat || t.module == modSlurp) && !t.looped() {
			key := map[bool]string{true: argPath, false: "src"}[t.module == modStat]
			if path := normaliseTemplate(scalarArg(t.args(), key)); path != "" {
				s.guards = append(s.guards, statGuard{register: register, path: path, line: uxTaskOf(t).line(), isStat: t.module == modStat})
			}
		}
	}
	var findings []string
	var judged []string
	for _, g := range s.guards {
		if slices.Contains(judged, g.path) {
			continue
		}
		judged = append(judged, g.path)
		if f := s.judge(rel, g); f != "" {
			findings = append(findings, f)
		}
	}
	return findings
}

// identifierRead matches a text that reads a variable by name.
func identifierRead(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?:^|[^\w.])` + regexp.QuoteMeta(name) + `\b`)
}

// readsAny is whether a condition reads any of the names.
func readsAny(when string, names map[string]bool) bool {
	for name := range names {
		if identifierRead(name).MatchString(when) {
			return true
		}
	}
	return false
}

// guardedWork is the work a record guard skips: the tasks other than the ones writing the record,
// the registers of the record, the work and the record's sources, and whether the work makes the
// file the guard reads as a record (from nothing). False for no record.
func (s recordScan) guardedWork(path string) ([]guardedTask, map[string]bool, bool) {
	work := map[string]bool{}
	for _, g := range s.guards {
		if g.path == path {
			work[g.register] = true
		}
	}
	var tasks []guardedTask
	var recordText strings.Builder
	recordMade := false
	for _, t := range s.tasks {
		if !readsAny(t.when, work) {
			continue
		}
		if r := scalarValue(t.node, "register"); r != "" {
			work[r] = true
		}
		if !slices.Contains(writtenPaths(t), path) {
			tasks = append(tasks, t)
			continue
		}
		if !makesFromNothing(t) {
			return nil, nil, false // The guard reads a file the work installs: an artefact, not a record.
		}
		recordMade = true
		recordText.WriteString("\n" + nodeText(mappingArgValue(t.args(), "content")))
	}
	// A probe the record is made from (the source's checksum) checks the source, not what was installed.
	for probe := range s.probes {
		if identifierRead(probe).MatchString(recordText.String()) {
			work[probe] = true
		}
	}
	return tasks, work, recordMade
}

// judge is the finding for the file one or more guards read, or "".
func (s recordScan) judge(rel string, first statGuard) string {
	tasks, work, recordMade := s.guardedWork(first.path)
	if !recordMade {
		return ""
	}
	var unchecked []string
	for _, t := range tasks {
		for _, what := range s.uncheckedBy(t, writtenPaths(t), work) {
			if !slices.Contains(unchecked, what) {
				unchecked = append(unchecked, what)
			}
		}
	}
	if len(unchecked) == 0 {
		return ""
	}
	return rel + ":" + first.line + ": " + ruleRecordGuard + ": the work this guard skips writes " + first.path +
		" as its record, and also " + strings.Join(unchecked, ", ") + ", which its condition never checks, so what " +
		"the record vouches for is never put back once it is gone; check each artefact too and skip only when all are in place"
}

// uncheckedBy is what a task of the work changes that its condition does not check.
func (s recordScan) uncheckedBy(t guardedTask, paths []string, work map[string]bool) []string {
	if len(paths) > 0 {
		var out []string
		for _, p := range paths {
			if !s.isScratch(p) && !s.statedBy(p, t.when) {
				out = append(out, p)
			}
		}
		return out
	}
	if t.module != modTempfile && changes(uxTaskOf(t)) && !s.probedApart(t.when, work) {
		return []string{"what the " + t.module + " task at line " + uxTaskOf(t).line() + " changes"}
	}
	return nil
}

// makesFromNothing is whether a task writes its file from no source: a copy of content, a touch.
func makesFromNothing(t guardedTask) bool {
	args := t.args()
	switch t.module {
	case modCopy:
		return scalarArg(args, "src") == "" && mappingArg(args, "content")
	case modFile:
		return scalarArg(args, "state") == "touch"
	}
	return false
}

// mappingArgValue is the first value of a key among the argument mappings, or nil.
func mappingArgValue(holders []*yaml.Node, key string) *yaml.Node {
	for _, h := range holders {
		if v := mappingValue(h, key); v != nil {
			return v
		}
	}
	return nil
}

// mappingArg is whether any argument mapping has the key.
func mappingArg(holders []*yaml.Node, key string) bool {
	return mappingArgValue(holders, key) != nil
}

// statedBy is whether a condition reads a stat guard of the path.
func (s recordScan) statedBy(path, when string) bool {
	return slices.ContainsFunc(s.guards, func(g statGuard) bool {
		return g.isStat && g.path == path && identifierRead(g.register).MatchString(when)
	})
}

// probedApart is whether a condition reads a probe that is neither the record's nor the work's.
func (s recordScan) probedApart(when string, work map[string]bool) bool {
	for probe := range s.probes {
		if !work[probe] && identifierRead(probe).MatchString(when) {
			return true
		}
	}
	return false
}

// writtenPaths is the paths a task writes or removes, template whitespace normalised.
func writtenPaths(t guardedTask) []string {
	keys, writes := writeModules[t.module]
	if !writes {
		return nil
	}
	args := t.args()
	for _, key := range keys {
		if path := normaliseTemplate(scalarArg(args, key)); path != "" {
			return []string{path}
		}
	}
	return nil
}

// isScratch is whether a path is under a temporary directory: the system's, or a tempfile's.
func (s recordScan) isScratch(path string) bool {
	if scratchPath.MatchString(path) {
		return true
	}
	for register := range s.scratch {
		if strings.HasPrefix(path, "{{ "+register+".path") {
			return true
		}
	}
	return false
}

// judgeSelfMadeMarkers applies the self-made-marker shape: a command whose creates names a file
// its own text makes, beside other work.
func judgeSelfMadeMarkers(rel string, tasks []guardedTask) []string {
	var findings []string
	for _, t := range tasks {
		if t.module != modCommand && t.module != modShell {
			continue
		}
		marker := normaliseTemplate(scalarArg(t.args(), argCreates))
		text := normaliseTemplate(commandText(uxTaskOf(t).moduleValue()))
		if marker == "" || !makesMarker(text, marker) || !doesMore(text, marker) {
			continue
		}
		findings = append(findings, rel+":"+uxTaskOf(t).line()+": "+ruleRecordGuard+": this command makes its own "+
			"creates marker "+marker+", so once it has run the work beside the touch is never checked or redone; "+
			"probe what the work makes, or use modules that say when it is in place")
	}
	return findings
}

// makesMarker is whether a command's text touches or redirects into the marker.
func makesMarker(text, marker string) bool {
	quoted := regexp.QuoteMeta(marker)
	return regexp.MustCompile(`(?:\btouch\s+(?:-\S+\s+)*["']?|>>?\s*["']?)` + quoted + `["']?(?:\s|$|;|&)`).MatchString(text)
}

// doesMore is whether a command's text holds a step other than making the marker and shell
// settings.
func doesMore(text, marker string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "set ") || strings.Contains(line, marker) {
			continue
		}
		return true
	}
	return false
}
