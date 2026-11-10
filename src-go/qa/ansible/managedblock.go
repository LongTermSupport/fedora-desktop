package ansible

import (
	"bytes"
	"cmp"
	"errors"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The duplicate-managed-block rule (CLAUDE/QA.md, "duplicate-managed-block"). A blockinfile
// task owns the region of its file between its markers, and a lineinfile task the line its
// regexp (or search_string, or line) finds. When two tasks own the same region of the same
// file with different content, each run of one rewrites what the other wrote: both report
// changed on every run, and what the file holds is whichever ran last.
//
// The rule reads every task of every YAML file the gate reads together (a play's pre_tasks,
// tasks, post_tasks and handlers, blocks opened, and a task list outside a play). A
// blockinfile owns (path, marker with marker_begin and marker_end, defaults filled in); a
// lineinfile owns (path, regexp, else search_string, else line). Its content is its state and,
// when present, its block (trailing newline dropped, as blockinfile splits it into lines) or
// line. Two owners of one region whose contents differ are each reported, naming the other.
// A loop or with_items given as a literal list is expanded, each item put in place of
// {{ item }} and {{ item.key }}; a task whose path or region names an item the rule cannot read
// (a loop over a variable) names no file it can know, and is not judged. Template whitespace is
// normalised ({{x}} is {{ x }}), so one variable spelt two ways is one path.

// ruleManagedBlock is the rule ID, printed with every finding and the anchor of its section.
const ruleManagedBlock = "duplicate-managed-block"

// yamlSource is one file the rule reads: its path from the root and its bytes.
type yamlSource struct {
	rel  string
	data []byte
}

// blockOwner is one task's claim on a region of a file.
type blockOwner struct {
	rel     string
	line    int
	key     string
	what    string
	target  string
	content string
}

// place is the owner's "<rel>:<line>".
func (o blockOwner) place() string { return o.rel + ":" + strconv.Itoa(o.line) }

// duplicateManagedBlocks returns the rule's findings over a set of files, "<rel>:<line>:
// <rule>: <why>", sorted by file and line, one per task. A file the YAML library cannot parse
// is left out: the per-file rules report it.
func duplicateManagedBlocks(sources []yamlSource) []string {
	groups := map[string][]blockOwner{}
	var order []string
	for _, src := range sources {
		for _, o := range fileOwners(src) {
			if _, seen := groups[o.key]; !seen {
				order = append(order, o.key)
			}
			groups[o.key] = append(groups[o.key], o)
		}
	}
	var reported []blockOwner
	seen := map[string]bool{}
	for _, key := range order {
		for _, o := range groups[key] {
			other, found := differingOwner(o, groups[key])
			if !found || seen[o.place()] {
				continue
			}
			seen[o.place()] = true
			o.what = o.what + " in " + o.target + " is also managed by " + other.place()
			reported = append(reported, o)
		}
	}
	slices.SortFunc(reported, func(a, b blockOwner) int {
		return cmp.Or(cmp.Compare(a.rel, b.rel), cmp.Compare(a.line, b.line))
	})
	findings := make([]string, 0, len(reported))
	for _, o := range reported {
		findings = append(findings, o.place()+": "+ruleManagedBlock+": "+o.what+
			" with different content, so each run rewrites the other's; give it one owner")
	}
	return findings
}

// differingOwner is the first owner of the same region, in another task, whose content differs.
func differingOwner(o blockOwner, group []blockOwner) (blockOwner, bool) {
	for _, other := range group {
		if other.place() != o.place() && other.content != o.content {
			return other, true
		}
	}
	return blockOwner{}, false
}

// fileOwners is every claim the tasks of one file make, in document order.
func fileOwners(src yamlSource) []blockOwner {
	dec := yaml.NewDecoder(bytes.NewReader(src.data))
	var owners []blockOwner
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return owners
		}
		if err != nil {
			return nil
		}
		for _, top := range doc.Content {
			for _, t := range ownerTasks(top) {
				owners = append(owners, taskOwners(src.rel, t)...)
			}
		}
	}
}

// ownerTasks is every task of a document's top-level list: runTasks, and each play's handlers.
func ownerTasks(top *yaml.Node) []uxTask {
	tasks := runTasks(top)
	if top.Kind != yaml.SequenceNode {
		return tasks
	}
	for _, item := range top.Content {
		if item.Kind == yaml.MappingNode && mappingValue(item, "hosts") != nil {
			flatten(mappingValue(item, keyHandlers), false, &tasks)
		}
	}
	return tasks
}

// Module names the rule reads.
const (
	modBlockInFile = "blockinfile"
	modLineInFile  = "lineinfile"
)

// defaultMarker is blockinfile's marker when the task names none.
const defaultMarker = "# {mark} ANSIBLE MANAGED BLOCK"

// taskOwners is a blockinfile or lineinfile task's claims, one per loop item.
func taskOwners(rel string, t uxTask) []blockOwner {
	if t.module != modBlockInFile && t.module != modLineInFile {
		return nil
	}
	args := managedArgs(t)
	items, readable := loopItems(t.node)
	if !readable {
		items = []*yaml.Node{nil}
	}
	var owners []blockOwner
	for _, item := range items {
		get := func(key string) string { return normaliseTemplate(withItem(scalarArg(args, key), item)) }
		target := cmp.Or(get("path"), get("dest"), get("destfile"), get("name"))
		var what, content string
		state := cmp.Or(get("state"), "present")
		if t.module == modBlockInFile {
			what = "the block " + strconv.Quote(cmp.Or(get("marker"), defaultMarker))
			if begin, end := get("marker_begin"), get("marker_end"); begin != "" || end != "" {
				what += " (" + cmp.Or(begin, "BEGIN") + "/" + cmp.Or(end, "END") + ")"
			}
			content = strings.TrimSuffix(get("block"), "\n")
		} else {
			switch {
			case get("regexp") != "":
				what = "the line matching " + strconv.Quote(get("regexp"))
			case get("search_string") != "":
				what = "the line holding " + strconv.Quote(get("search_string"))
			default:
				what = "the line " + strconv.Quote(get("line"))
			}
			content = get("line")
		}
		if target == "" || namesItem(target) || namesItem(what) {
			continue
		}
		if state != "present" {
			content = ""
		}
		owners = append(owners, blockOwner{
			rel: rel, line: t.moduleKey.Line, key: t.module + "\x00" + target + "\x00" + what,
			what: what, target: target, content: state + "\n" + content,
		})
	}
	return owners
}

// managedArgs is a task's arguments: its module's mapping, with args under it.
func managedArgs(t uxTask) []*yaml.Node {
	var holders []*yaml.Node
	for _, holder := range []*yaml.Node{t.moduleValue(), mappingValue(t.node, "args")} {
		if holder != nil && holder.Kind == yaml.MappingNode {
			holders = append(holders, holder)
		}
	}
	return holders
}

// scalarArg is the first scalar value of a key among the argument mappings, or "".
func scalarArg(holders []*yaml.Node, key string) string {
	for _, holder := range holders {
		if v := mappingValue(holder, key); v != nil && v.Kind == yaml.ScalarNode {
			return v.Value
		}
	}
	return ""
}

// loopItems is a task's loop items when its loop is a literal list; no loop is one nil item.
// The second result is false for a loop the rule cannot read (a variable, a lookup).
func loopItems(task *yaml.Node) ([]*yaml.Node, bool) {
	for _, key := range []string{"loop", "with_items", "with_list"} {
		if v := mappingValue(task, key); v != nil {
			if v.Kind != yaml.SequenceNode {
				return nil, false
			}
			return v.Content, true
		}
	}
	return []*yaml.Node{nil}, true
}

// itemRef is a reference to the loop item or one of its keys inside a template.
var itemRef = regexp.MustCompile(`\{\{\s*item(?:\.([A-Za-z_][A-Za-z0-9_]*)|\[['"]([^'"]+)['"]\])?\s*\}\}`)

// withItem puts a loop item's values in place of the references to it that it can answer.
func withItem(text string, item *yaml.Node) string {
	if item == nil {
		return text
	}
	return itemRef.ReplaceAllStringFunc(text, func(ref string) string {
		m := itemRef.FindStringSubmatch(ref)
		field := m[1] + m[2]
		switch {
		case field == "" && item.Kind == yaml.ScalarNode:
			return item.Value
		case field != "" && item.Kind == yaml.MappingNode:
			if v := mappingValue(item, field); v != nil && v.Kind == yaml.ScalarNode {
				return v.Value
			}
		}
		return ref
	})
}

// itemLeft is a template that still reads the loop item.
var itemLeft = regexp.MustCompile(`\{\{[^}]*\bitem\b`)

// namesItem is whether a text still reads a loop item the rule could not put in place.
func namesItem(text string) bool { return itemLeft.MatchString(text) }

// templateSpan is one {{ ... }} with the whitespace inside its braces.
var templateSpan = regexp.MustCompile(`\{\{\s*(.*?)\s*\}\}`)

// normaliseTemplate spells every template with one space inside its braces.
func normaliseTemplate(text string) string { return templateSpan.ReplaceAllString(text, "{{ $1 }}") }
