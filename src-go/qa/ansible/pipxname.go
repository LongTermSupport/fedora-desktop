package ansible

import (
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The pipx-name-not-canonical rule (CLAUDE/QA.md, "pipx-name-not-canonical"). pipx keys each
// venv by the canonical form of its package name (lower case, each run of -, _ and . one -),
// and community.general.pipx and pipx_info filter `pipx list --json` by the name (and suffix)
// exactly as written. A name in any other form never matches: the module sees no app, so
// install, present and latest reinstall it and report changed on every run, absent removes
// nothing, and pipx_info returns nothing (issue 338: play-python's Hugging Face CLI task,
// name huggingface_hub, changed on every pass with the version unchanged).
//
// The rule reports a pipx or pipx_info task whose name, with any extras, version specifier,
// marker or URL removed, joined to its suffix, is not canonical. A literal name is judged, and
// one that reads the loop item, once per item of a literal loop list; any other template is
// not resolved.

// rulePipxName is the rule ID, printed with every finding and the anchor of its section.
const rulePipxName = "pipx-name-not-canonical"

// pipxModules are the modules that look an app up by its name as written.
var pipxModules = map[string]bool{"pipx": true, "pipx_info": true}

// pipxSeparators is a run of the characters pipx folds into one -.
var pipxSeparators = regexp.MustCompile(`[-_.]+`)

// requirementEnd is the first character after a requirement's name.
var requirementEnd = regexp.MustCompile(`[\s\[<>=!~;@(]`)

// pipxNameNotCanonical returns the rule's findings of one YAML file, "<rel>:<line>: <rule>:
// <why>", in document order. A file the YAML library cannot parse is skipped:
// command-always-changed reports it.
func pipxNameNotCanonical(rel string, data []byte) []string {
	var findings []string
	for _, doc := range parsedDocs(data) {
		for _, top := range doc.Content {
			for _, t := range runTasks(top) {
				if !pipxModules[t.module] {
					continue
				}
				for _, written := range pipxNames(t) {
					if canon := pipxCanonical(written); canon != written {
						findings = append(findings, rel+":"+t.line()+": "+rulePipxName+": pipx names this app's venv "+
							strconv.Quote(canon)+" but the module looks for "+strconv.Quote(written)+", so it never sees "+
							"the app installed: it reinstalls and reports changed on every run, and absent removes nothing; "+
							"write the name as "+strconv.Quote(canon))
						break
					}
				}
			}
		}
	}
	return findings
}

// pipxNames is each venv name a pipx task looks up as written: its name's requirement name
// joined to its suffix, once per loop item where the name is the item; none where a template
// is not resolved.
func pipxNames(t uxTask) []string {
	name, suffix := pipxArg(t, "name"), pipxArg(t, "suffix")
	if name == "" {
		return nil
	}
	items, literal := loopItems(t.node)
	if !literal {
		return nil
	}
	var out []string
	for _, item := range items {
		n, s := withItem(name, item), withItem(suffix, item)
		if !strings.Contains(n+s, "{{") {
			out = append(out, requirementName(n)+s)
		}
	}
	return out
}

// pipxArg is a scalar argument of the task's module, or of its args.
func pipxArg(t uxTask, key string) string {
	for _, holder := range []*yaml.Node{t.moduleValue(), mappingValue(t.node, "args")} {
		if holder == nil || holder.Kind != yaml.MappingNode {
			continue
		}
		if v := mappingValue(holder, key); v != nil && v.Kind == yaml.ScalarNode {
			return v.Value
		}
	}
	return ""
}

// requirementName is a requirement's package name: what precedes its extras, version
// specifier, marker or URL.
func requirementName(req string) string {
	req = strings.TrimSpace(req)
	if loc := requirementEnd.FindStringIndex(req); loc != nil {
		return req[:loc[0]]
	}
	return req
}

// pipxCanonical is the name pipx gives a package's venv: lower case, each run of -, _ and .
// one -.
func pipxCanonical(name string) string {
	return pipxSeparators.ReplaceAllString(strings.ToLower(name), "-")
}
