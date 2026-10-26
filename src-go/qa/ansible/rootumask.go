package ansible

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The root-umask check (CLAUDE/QA.md, "root-umask"). Ansible has no umask setting for a command,
// so a file a root task creates through a command takes the umask the run inherited, and under a
// host umask of 0077 it is root's alone: users cannot read it, and the program that wanted it
// quietly falls back or fails (fontconfig fell back to a stale per-user cache). The check reports
// a root task that creates files users read in one of four ways without making them readable:
//
//   - a command that rebuilds a system cache or index (fc-cache, the icon, MIME, schema and
//     desktop databases, dconf update, ...) with no umask 022 before it;
//   - a command that writes a file under /usr, /opt or /etc (wget -O, curl -o, a redirect, tee)
//     with no umask 022 before it and no chmod to a mode others can read after it;
//   - the git module cloning under /usr or /opt without umask: '0022';
//   - the unarchive module extracting under /usr or /opt without mode:.
//
// A task is taken as run as root when become is in effect for it (a play's become, or the
// task's or an enclosing block's) with no become_user other than root. A task list outside a
// play (tasks/, a role's tasks or handlers) cannot see the play that includes it, so it is
// taken as run as root unless it says otherwise: those files are included by become plays.

// cacheTool is a cache rebuild tool run as a command word: at the start of the text or after
// whitespace, a shell operator or a directory, and ending the word. ldconfig is not one: it
// sets /etc/ld.so.cache to 0644 itself, whatever the umask.
var cacheTool = regexp.MustCompile(`(?:^|[\s;&|(/])(fc-cache|update-mime-database|gtk4?-update-icon-cache|` +
	`glib-compile-schemas|update-desktop-database|gdk-pixbuf-query-loaders(?:-64)?|gio-querymodules(?:-64)?|` +
	`mkfontdir|mkfontscale|mandb|update-ca-trust|systemd-hwdb|dconf\s+update|udevadm\s+hwdb)(?:$|[\s;&|)])`)

// systemWrite is a command writing a file under a system path: wget's output document, curl's
// output, a redirect or tee. The path is the first group.
var systemWrite = regexp.MustCompile(`(?:\bwget\b[^\n;|&]*?(?:\s-q?O\s*|\s--output-document[=\s]\s*)|` +
	`\bcurl\b[^\n;|&]*?\s(?:-[a-zA-Z]*o|--output)\s+|>>?\s*|\btee\s+(?:-a\s+)?)["']?(/(?:usr|opt|etc)/[^\s"';|&)]+)`)

// readableChmod is a chmod that leaves a file readable by others: a numeric mode whose last
// digit has the read bit, or a symbolic one that gives others or all read.
var readableChmod = regexp.MustCompile(`\bchmod\s+(?:-\S+\s+)*(?:0?[0-7]{2}[4-7]\b|[0-7]?[0-7]{2}[4-7]\b|[ugo]*[ao][ugo]*[+=][rwxXst]*r|[+=][rwxXst]*r)`)

// umask022 is the umask that leaves the files world-readable.
var umask022 = regexp.MustCompile(`(?:^|[\s;&|(])umask\s+0?022(?:$|[\s;&|)])`)

// systemDest is a destination users read from: under /usr or /opt.
var systemDest = regexp.MustCompile(`^/(?:usr|opt)(?:/|$)`)

// taskListKeys are the keys of a play or a block that hold task lists.
var taskListKeys = map[string]bool{
	tasksKey: true, "pre_tasks": true, "post_tasks": true, "handlers": true,
	"block": true, "rescue": true, "always": true,
}

// privilege is what a task runs as: whether become is in effect (or cannot be known) and the user.
// It also carries the play's plain variables, so a destination given as one of them resolves.
type privilege struct {
	become bool
	user   string
	vars   map[string]string
}

func (p privilege) root() bool { return p.become && (p.user == "" || p.user == "root") }

// wholeVar is a value that is one variable and nothing else.
var wholeVar = regexp.MustCompile(`^\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}$`)

// resolve is a value with a play variable that is the whole of it put in.
func (p privilege) resolve(value string) string {
	if m := wholeVar.FindStringSubmatch(value); m != nil {
		if v, ok := p.vars[m[1]]; ok {
			return v
		}
	}
	return value
}

// playVars are the scalar variables of a play's vars: mapping.
func playVars(play *yaml.Node) map[string]string {
	vars := map[string]string{}
	if v := mappingValue(play, "vars"); v != nil && v.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(v.Content); i += 2 {
			if v.Content[i+1].Kind == yaml.ScalarNode {
				vars[v.Content[i].Value] = strings.TrimSpace(v.Content[i+1].Value)
			}
		}
	}
	return vars
}

// rootUmask returns the root-umask findings of one YAML file, "<rel>:<line>: <why>", in document
// order. A file the YAML library cannot parse is a finding.
func rootUmask(rel string, data []byte) []string {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var findings []string
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return findings
		}
		if err != nil {
			line := "1"
			if m := yamlErrorLine.FindStringSubmatch(err.Error()); m != nil {
				line = m[1]
			}
			return append(findings, rel+":"+line+": cannot be parsed as YAML, so the files its root tasks create cannot be judged: "+err.Error())
		}
		for _, top := range doc.Content {
			findings = visitTop(top, rel, findings)
		}
	}
}

// visitTop judges a document's top-level list: plays, or the tasks of a task list.
func visitTop(top *yaml.Node, rel string, findings []string) []string {
	if top.Kind != yaml.SequenceNode {
		return findings
	}
	for _, item := range top.Content {
		switch {
		case item.Kind != yaml.MappingNode:
		case mappingValue(item, "hosts") != nil:
			// A play: become is off unless it says so.
			findings = visitTasks(item, applyPrivilege(item, privilege{vars: playVars(item)}), rel, findings)
		default:
			findings = visitTask(item, privilege{become: true}, rel, findings)
		}
	}
	return findings
}

// mappingValue is the value under key in a mapping, or nil.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// scalarValue is the trimmed value under key in a mapping, or "" when there is no scalar.
func scalarValue(m *yaml.Node, key string) string {
	if v := mappingValue(m, key); v != nil && v.Kind == yaml.ScalarNode {
		return strings.TrimSpace(v.Value)
	}
	return ""
}

// applyPrivilege is the privilege inside a play, block or task given the one it inherits. A
// become that is not a plain boolean (a template) is taken as on.
func applyPrivilege(m *yaml.Node, inherited privilege) privilege {
	p := inherited
	if v := mappingValue(m, "become"); v != nil {
		var on bool
		if v.Decode(&on) == nil {
			p.become = on
		} else {
			p.become = true
		}
	}
	if user := scalarValue(m, "become_user"); user != "" {
		p.user = user
	}
	return p
}

// visitTasks walks the task lists of a play or block.
func visitTasks(m *yaml.Node, p privilege, rel string, findings []string) []string {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if !taskListKeys[m.Content[i].Value] || m.Content[i+1].Kind != yaml.SequenceNode {
			continue
		}
		for _, task := range m.Content[i+1].Content {
			if task.Kind == yaml.MappingNode {
				findings = visitTask(task, p, rel, findings)
			}
		}
	}
	return findings
}

// visitTask judges one task, or walks a block.
func visitTask(task *yaml.Node, inherited privilege, rel string, findings []string) []string {
	p := applyPrivilege(task, inherited)
	if mappingValue(task, "block") != nil {
		return visitTasks(task, p, rel, findings)
	}
	if !p.root() {
		return findings
	}
	for i := 0; i+1 < len(task.Content); i += 2 {
		key := task.Content[i]
		var why string
		switch key.Value[strings.LastIndex(key.Value, ".")+1:] {
		case "command", "shell", "raw":
			text := commandText(task.Content[i+1])
			if text == "" {
				if args := mappingValue(task, "args"); args != nil {
					text = commandText(args)
				}
			}
			why = judgeCommand(text)
		case "git":
			args := moduleArgs(task, task.Content[i+1])
			why = judgeGit(args, p.resolve(scalarValue(args, "dest")))
		case "unarchive":
			args := moduleArgs(task, task.Content[i+1])
			why = judgeUnarchive(args, p.resolve(scalarValue(args, "dest")))
		}
		if why != "" {
			findings = append(findings, rel+":"+strconv.Itoa(key.Line)+": "+why)
		}
	}
	return findings
}

// moduleArgs is a module's argument mapping, or the task's args: when the module has none.
func moduleArgs(task, v *yaml.Node) *yaml.Node {
	if v.Kind == yaml.MappingNode {
		return v
	}
	if args := mappingValue(task, "args"); args != nil && args.Kind == yaml.MappingNode {
		return args
	}
	return &yaml.Node{Kind: yaml.MappingNode}
}

// commandText is a command module's command line: the free-form scalar, or its cmd, or its
// argv joined by spaces. Whole-line shell comments are dropped.
func commandText(v *yaml.Node) string {
	var text string
	switch v.Kind {
	case yaml.ScalarNode:
		text = v.Value
	case yaml.MappingNode:
		if cmd := mappingValue(v, "cmd"); cmd != nil && cmd.Kind == yaml.ScalarNode {
			text = cmd.Value
		} else if argv := mappingValue(v, "argv"); argv != nil && argv.Kind == yaml.SequenceNode {
			words := make([]string, 0, len(argv.Content))
			for _, w := range argv.Content {
				words = append(words, w.Value)
			}
			text = strings.Join(words, " ")
		}
	case yaml.DocumentNode, yaml.SequenceNode, yaml.AliasNode:
	}
	lines := strings.Split(text, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// umaskBefore is whether umask 022 is set in text before offset at.
func umaskBefore(text string, at int) bool {
	mask := umask022.FindStringIndex(text)
	return mask != nil && mask[0] < at
}

// judgeCommand says what is wrong with a root command line, or "".
func judgeCommand(text string) string {
	if tool := cacheTool.FindStringSubmatchIndex(text); tool != nil && !umaskBefore(text, tool[2]) {
		name := strings.Join(strings.Fields(text[tool[2]:tool[3]]), " ")
		return name + " runs as root under the inherited umask, so a restrictive one (0077) leaves the files it " +
			"writes unreadable to users; run it as a shell command that starts umask 022 &&"
	}
	for _, w := range systemWrite.FindAllStringSubmatchIndex(text, -1) {
		if umaskBefore(text, w[0]) || readableChmod.MatchString(text[w[1]:]) {
			continue
		}
		return text[w[2]:w[3]] + " is written as root under the inherited umask, so a restrictive one (0077) " +
			"leaves it unreadable to users; set umask 022 first, or chmod it to a mode others can read"
	}
	return ""
}

// judgeGit says what is wrong with a root clone, or "".
func judgeGit(args *yaml.Node, dest string) string {
	if !systemDest.MatchString(dest) {
		return ""
	}
	if mask := strings.Trim(scalarValue(args, "umask"), `'"`); mask == "0022" || mask == "022" {
		return ""
	}
	return "the clone into " + dest + " is checked out as root under the inherited umask, so a restrictive one " +
		"(0077) leaves it unreadable to users; set umask: '0022'"
}

// judgeUnarchive says what is wrong with a root extraction, or "".
func judgeUnarchive(args *yaml.Node, dest string) string {
	if !systemDest.MatchString(dest) || mappingValue(args, "mode") != nil {
		return ""
	}
	return "the archive extracted into " + dest + " as root takes the inherited umask where it stores no mode, so a " +
		"restrictive one (0077) can leave it unreadable to users; set mode: u=rwX,go=rX"
}

// scanRootUmask runs the check over the YAML files under dirs (roles/vendor skipped).
func scanRootUmask(fsys fs.FS, dirs []string, f *Findings) error {
	for _, dir := range dirs {
		files, err := yamlFiles(fsys, dir, true)
		if err != nil {
			return err
		}
		for _, rel := range files {
			data, err := fs.ReadFile(fsys, rel)
			if err != nil {
				return fmt.Errorf("a file could not be read: %w", err)
			}
			for _, v := range rootUmask(rel, data) {
				f.Out = append(f.Out, "  ERROR (root-umask): "+v, rootUmaskAdvice)
				f.RootUmask = append(f.RootUmask, v)
			}
		}
	}
	return nil
}

// rootUmaskAdvice is the line printed under each finding.
const rootUmaskAdvice = "    Why and how to fix: CLAUDE/QA.md#root-umask"
