package ansible

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The filter-list-unchecked rule (CLAUDE/QA.md, "filter-list-unchecked"). A list a person
// writes (an inventory variable) that picks items from another list by membership drops, with
// no word, every name that matches nothing there: a typo, or a name the other list was cut
// down to leave out, is never acted on and the play reports success.
//
// The rule reads each play, and the task list outside a play, with every name the play
// supplies (its vars and vars_prompt, block and task vars, registers, set_fact, loop
// variables) and the repository's own variables. A membership filter is selectattr or
// rejectattr with the in or "not in" test, select or reject with in, intersect, or a loop whose
// when tests item (or an attribute of it) in a list. Its list is the variable after the test
// and its source the head of the pipe chain it filters (the loop, for a when). The rule
// reports a list that is an input (no name the play or repository supplies) when no assert's
// that and no fail's when in the same play checks it against what it picks from: the list
// followed by difference(...) or "is subset(...)", whose argument names a source of one of its
// filters, or a variable that source is cut from by no other input list. A source whose head
// the rule cannot read (a parenthesised sum) accepts any such check. One finding per list, at
// its first filter.

// ruleFilterList is the rule ID, printed with every finding and the anchor of its section.
const ruleFilterList = "filter-list-unchecked"

// keyFailedWhen is a task's failure condition.
const keyFailedWhen = "failed_when"

// memberFilter is one membership filter: the list that picks, what it picks from, its line.
type memberFilter struct {
	list   string
	source string
	line   int
}

// filterScope is one play's (or task list's) names, definitions, filters and checks.
type filterScope struct {
	known   map[string]bool
	defs    map[string][]string
	filters []memberFilter
	checks  []string
}

// Membership filters in a template, the list the first group.
var (
	filterByAttr  = regexp.MustCompile(`\b(?:select|reject)attr\(\s*['"][^'"]*['"]\s*,\s*['"](?:in|not in)['"]\s*,\s*([A-Za-z_]\w*)`)
	filterByValue = regexp.MustCompile(`\b(?:select|reject)\(\s*['"](?:in|not in)['"]\s*,\s*([A-Za-z_]\w*)`)
	filterBySet   = regexp.MustCompile(`\bintersect\(\s*([A-Za-z_]\w*)`)
	itemIn        = regexp.MustCompile(`\bitem(?:\.\w+|\[[^\]]*\])*\s+(?:not\s+)?in\s+([A-Za-z_]\w*)`)
	// chainHead is the variable a pipe chain ending at the filter starts from.
	chainHead = regexp.MustCompile(`([A-Za-z_]\w*)(?:\.\w+|\[[^\]]*\])*\s*(?:\|\s*\w+(?:\((?:[^()]|\([^()]*\))*\))?\s*)*\|\s*$`)
	// loopHead is the variable a loop's template starts from.
	loopHead = regexp.MustCompile(`^\s*\{\{\s*([A-Za-z_]\w*)`)
	// identifier is a variable named in a template.
	identifier = regexp.MustCompile(`(?:^|[^\w.'"])([A-Za-z_]\w*)`)
)

// filterListUnchecked returns the rule's findings of one YAML file, "<rel>:<line>: <rule>:
// <why>", in document order. A file the YAML library cannot parse is left out: playUX reports it.
func filterListUnchecked(rel string, data []byte, repoVars map[string]bool) []string {
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
			for _, s := range filterScopes(top) {
				findings = append(findings, s.judge(rel, repoVars)...)
			}
		}
	}
}

// filterScopes reads a document's top-level list: one scope per play, and one for the tasks
// outside a play.
func filterScopes(top *yaml.Node) []*filterScope {
	if top.Kind != yaml.SequenceNode {
		return nil
	}
	var scopes []*filterScope
	loose := newFilterScope()
	for _, item := range top.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if mappingValue(item, "hosts") != nil {
			scopes = append(scopes, readPlayScope(item))
			continue
		}
		if mappingValue(item, "import_playbook") == nil {
			loose.readTasks(&yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{item}})
		}
	}
	return append(scopes, loose)
}

// readPlayScope reads one play: its vars, vars_prompt and every task list.
func readPlayScope(play *yaml.Node) *filterScope {
	s := newFilterScope()
	s.readVars(mappingValue(play, keyVars))
	if prompts := mappingValue(play, "vars_prompt"); prompts != nil && prompts.Kind == yaml.SequenceNode {
		for _, p := range prompts.Content {
			if p.Kind == yaml.MappingNode {
				s.known[scalarValue(p, "name")] = true
			}
		}
	}
	for _, key := range []string{keyPreTasks, tasksKey, keyPostTasks, keyHandlers} {
		s.readTasks(mappingValue(play, key))
	}
	return s
}

func newFilterScope() *filterScope {
	return &filterScope{known: map[string]bool{}, defs: map[string][]string{}}
}

// readVars records a vars mapping: each name supplied, its definition, and the filters in it.
func (s *filterScope) readVars(vars *yaml.Node) {
	if vars == nil || vars.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(vars.Content); i += 2 {
		name := vars.Content[i].Value
		s.known[name] = true
		s.defs[name] = append(s.defs[name], allText(vars.Content[i+1]))
		s.readFilters(vars.Content[i+1])
	}
}

// readTasks records a task list, blocks opened.
func (s *filterScope) readTasks(list *yaml.Node) {
	if list == nil || list.Kind != yaml.SequenceNode {
		return
	}
	for _, task := range list.Content {
		if task.Kind != yaml.MappingNode {
			continue
		}
		s.readVars(mappingValue(task, keyVars))
		if mappingValue(task, keyBlock) != nil {
			for _, key := range []string{keyBlock, keyRescue, keyAlways} {
				s.readTasks(mappingValue(task, key))
			}
			continue
		}
		s.readTask(task)
	}
}

// readTask records one task: the names it supplies, its checks and its filters.
func (s *filterScope) readTask(task *yaml.Node) {
	key := moduleOf(task)
	module := ""
	if key != nil {
		module = shortName(key.Value)
		producedBy(task, module, func(n string) { s.known[n] = true })
	}
	if module == modSetFact {
		s.readVars(mappingValue(task, key.Value))
	}
	// A filter inside a check is the check's own arithmetic, which reports what it finds: no
	// name is dropped there without a word. Vars and set_fact were read as definitions.
	skip := map[string]bool{keyVars: true, keyFailedWhen: true}
	switch module {
	case modAssert:
		if args := mappingValue(task, key.Value); args != nil && args.Kind == yaml.MappingNode {
			s.checks = append(s.checks, items(mappingValue(args, "that"))...)
		}
		skip[key.Value] = true
	case modFail:
		s.checks = append(s.checks, items(mappingValue(task, "when"))...)
		skip["when"] = true
	case modSetFact:
		skip[key.Value] = true
	}
	for i := 0; i+1 < len(task.Content); i += 2 {
		if !skip[task.Content[i].Value] {
			s.readFilters(task.Content[i+1])
		}
	}
	s.readLoopFilter(task)
}

// readLoopFilter records a loop whose when picks items by membership.
func (s *filterScope) readLoopFilter(task *yaml.Node) {
	when := mappingValue(task, "when")
	if when == nil {
		return
	}
	for _, key := range loopKeys {
		loop := mappingValue(task, key)
		if loop == nil || loop.Kind != yaml.ScalarNode {
			continue
		}
		source := ""
		if m := loopHead.FindStringSubmatch(loop.Value); m != nil {
			source = m[1]
		}
		for _, item := range itemNodes(when) {
			for _, m := range itemIn.FindAllStringSubmatch(item.Value, -1) {
				s.filters = append(s.filters, memberFilter{list: m[1], source: source, line: item.Line})
			}
		}
	}
}

// itemNodes is a condition's scalar items.
func itemNodes(v *yaml.Node) []*yaml.Node {
	switch v.Kind {
	case yaml.ScalarNode:
		return []*yaml.Node{v}
	case yaml.SequenceNode:
		var out []*yaml.Node
		for _, item := range v.Content {
			if item.Kind == yaml.ScalarNode {
				out = append(out, item)
			}
		}
		return out
	case yaml.DocumentNode, yaml.MappingNode, yaml.AliasNode:
	}
	return nil
}

// items is a condition's texts, one per item.
func items(v *yaml.Node) []string {
	if v == nil {
		return nil
	}
	var out []string
	for _, n := range itemNodes(v) {
		out = append(out, n.Value)
	}
	return out
}

// allText is every scalar under a node, joined by newlines.
func allText(v *yaml.Node) string {
	var parts []string
	walkScalars(v, func(n *yaml.Node) { parts = append(parts, n.Value) })
	return strings.Join(parts, "\n")
}

// walkScalars calls visit with every scalar under a node, keys of mappings left out.
func walkScalars(v *yaml.Node, visit func(*yaml.Node)) {
	if v == nil {
		return
	}
	switch v.Kind {
	case yaml.ScalarNode:
		visit(v)
	case yaml.MappingNode:
		for i := 1; i < len(v.Content); i += 2 {
			walkScalars(v.Content[i], visit)
		}
	case yaml.SequenceNode, yaml.DocumentNode:
		for _, c := range v.Content {
			walkScalars(c, visit)
		}
	case yaml.AliasNode:
	}
}

// readFilters records the membership filters in every scalar under a node.
func (s *filterScope) readFilters(v *yaml.Node) {
	walkScalars(v, func(n *yaml.Node) { s.filters = append(s.filters, filtersIn(n.Value, n.Line)...) })
}

// filtersIn is the membership filters of one text.
func filtersIn(text string, line int) []memberFilter {
	var out []memberFilter
	for _, re := range []*regexp.Regexp{filterByAttr, filterByValue, filterBySet} {
		for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
			prefix := text[:m[0]]
			if open := strings.LastIndex(prefix, "{{"); open >= 0 {
				prefix = prefix[open+2:]
			}
			source := ""
			if h := chainHead.FindStringSubmatch(prefix); h != nil {
				source = h[1]
			}
			out = append(out, memberFilter{list: text[m[2]:m[3]], source: source, line: line})
		}
	}
	return out
}

// isInput is whether a name is a setting a person writes: supplied by neither the play nor the
// repository, nor by Ansible.
func (s *filterScope) isInput(name string, repoVars map[string]bool) bool {
	return !s.known[name] && !repoVars[name] && !magicVars[name] && !strings.HasPrefix(name, "ansible_")
}

// judge is the scope's findings: one per input list no check covers, at its first filter.
func (s *filterScope) judge(rel string, repoVars map[string]bool) []string {
	var order []string
	byList := map[string][]memberFilter{}
	for _, f := range s.filters {
		if !s.isInput(f.list, repoVars) {
			continue
		}
		if _, seen := byList[f.list]; !seen {
			order = append(order, f.list)
		}
		byList[f.list] = append(byList[f.list], f)
	}
	var findings []string
	for _, list := range order {
		filters := byList[list]
		if s.checked(list, filters, repoVars) {
			continue
		}
		first := slices.MinFunc(filters, func(a, b memberFilter) int { return a.line - b.line })
		findings = append(findings, rel+":"+strconv.Itoa(first.line)+": "+ruleFilterList+": "+list+" picks from "+
			sources(filters)+" by membership, and nothing checks that each of its names is there, so a name that "+
			"matches nothing is dropped without a word; assert "+list+" | difference(<what it picks from>) is empty first")
	}
	return findings
}

// sources is the filters' sources, for the message.
func sources(filters []memberFilter) string {
	var names []string
	for _, f := range filters {
		name := f.source
		if name == "" {
			name = "an expression"
		}
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return strings.Join(names, " and ")
}

// checked is whether a check covers the list: it names a source of one of the list's filters,
// or a variable that source is cut from by no other input list.
func (s *filterScope) checked(list string, filters []memberFilter, repoVars map[string]bool) bool {
	against := s.checkedAgainst(list)
	if len(against) == 0 {
		return false
	}
	for _, f := range filters {
		if f.source == "" || s.reaches(f.source, against, list, repoVars) {
			return true
		}
	}
	return false
}

// checkedAgainst is every name the checks of a list compare it against.
func (s *filterScope) checkedAgainst(list string) map[string]bool {
	check := regexp.MustCompile(`(?:^|[^\w.])` + regexp.QuoteMeta(list) +
		`\b(?:\s*\|\s*default\((?:[^()]|\([^()]*\))*\))?(?:\s*\|\s*list)?\s*(?:\|\s*difference|is\s+subset)\(`)
	against := map[string]bool{}
	for _, text := range s.checks {
		for _, m := range check.FindAllStringIndex(text, -1) {
			for _, id := range identifier.FindAllStringSubmatch(argumentOf(text[m[1]:]), -1) {
				against[id[1]] = true
			}
		}
	}
	return against
}

// argumentOf is the text up to the parenthesis that closes the one just opened.
func argumentOf(rest string) string {
	depth := 1
	for i, r := range rest {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return rest[:i]
			}
		}
	}
	return rest
}

// reaches is whether a source is one of the names, or is cut, through definitions that filter
// by no input list but the one judged, from one of them.
func (s *filterScope) reaches(source string, against map[string]bool, list string, repoVars map[string]bool) bool {
	seen := map[string]bool{}
	next := []string{source}
	for len(next) > 0 {
		name := next[len(next)-1]
		next = next[:len(next)-1]
		if seen[name] {
			continue
		}
		seen[name] = true
		if against[name] {
			return true
		}
		if s.cutByOtherList(name, list, repoVars) {
			continue
		}
		for _, def := range s.defs[name] {
			for _, id := range identifier.FindAllStringSubmatch(def, -1) {
				next = append(next, id[1])
			}
		}
	}
	return false
}

// cutByOtherList is whether a variable's definition filters by an input list other than one.
func (s *filterScope) cutByOtherList(name, list string, repoVars map[string]bool) bool {
	for _, def := range s.defs[name] {
		for _, f := range filtersIn(def, 0) {
			if f.list != list && s.isInput(f.list, repoVars) {
				return true
			}
		}
	}
	return false
}
