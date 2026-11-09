package ansible

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The sudoers-rule check (CLAUDE/QA.md, "sudoers-rule"): a sudoers rule this repository installs
// must not let the user it is granted to steer what it runs. A scoped rule grants one narrow
// action; if that user can write the target, a directory above it, or the environment it runs
// in, or pass it arguments the rule did not mean, the narrow grant is root. The check reads every
// task that writes sudoers text (a copy or template under /etc/sudoers.d, a blockinfile or
// lineinfile on a sudoers file, a command that names one), the text itself, and every task (and
// root installer's put_file line) that deploys a command the text names, and reports:
//
//   - on the task: no visudo validation; a whole drop-in not root:root 0440; a sudoers file
//     written by a command, or text that cannot be read;
//   - in the text: a Defaults that loosens the environment (env_keep, setenv, !env_reset,
//     secure_path, env_file, ...); an include; a User_Alias; a rule for no fixed user (ALL, a
//     group, a template other than user_login); a runas list with ALL; SETENV; a negated
//     command; sudoedit; a command path that is relative or has a wildcard; a command with no
//     argument list (any arguments), a wildcard argument, or an expression not anchored at both
//     ends or with a bare dot;
//   - for each command the text names: no task deploys it; it is deployed owned by a user other
//     than root, with a mode the group or others can write, or with no mode; it is a link to a
//     path that is not itself such a target; or a task makes a directory above it owned by
//     another user or writable by group or others.
//
// A rule whose commands are ALL is not a scoped rule: it grants full root on purpose, so there is
// no narrow action to subvert, and it is not judged. Comment lines are not read.

// sudoersAdvice is the line printed under each finding.
const sudoersAdvice = "    Why and how to fix: CLAUDE/QA.md#sudoers-rule"

// task is one task of a YAML file: where it is, its module's short name and arguments.
type task struct {
	rel    string
	line   int
	module string
	args   *yaml.Node
	p      privilege
}

func (t task) at() string { return t.rel + ":" + strconv.Itoa(t.line) }

// arg is a module argument, with a play variable that is the whole of it resolved.
func (t task) arg(key string) string {
	return t.p.resolve(strings.Trim(scalarValue(t.args, key), `'"`))
}

// dest is the path a file-writing module writes: dest, or path.
func (t task) dest() string {
	if d := t.arg("dest"); d != "" {
		return d
	}
	return t.arg("path")
}

// deploy is one way a path is put on the machine: a task, or an installer's put_file line.
type deploy struct {
	at, owner, mode, link string
}

// The modules the check reads, by short name: those that run a command line, those that write a
// whole file, the file module (links and directories), and those that edit lines of one.
var (
	commandModules   = map[string]bool{"command": true, "shell": true, rawModule: true}
	wholeFileModules = map[string]bool{"copy": true, "template": true}
	lineModules      = map[string]bool{"blockinfile": true, "lineinfile": true}
)

const (
	fileModule = "file"
	rawModule  = "raw"
	rootUser   = "root"
)

func judgedModule(short string) bool {
	return commandModules[short] || wholeFileModules[short] || lineModules[short] || short == fileModule
}

// collectTasks lists every task of the YAML files under dirs that runs a judged module.
func collectTasks(fsys fs.FS, dirs []string) ([]task, error) {
	var tasks []task
	for _, dir := range dirs {
		if !dirExists(fsys, dir) {
			continue
		}
		files, err := yamlFiles(fsys, dir, true)
		if err != nil {
			return nil, err
		}
		for _, rel := range files {
			data, err := fs.ReadFile(fsys, rel)
			if err != nil {
				return nil, fmt.Errorf("a file could not be read: %w", err)
			}
			tasks = append(tasks, fileTasks(rel, data)...)
		}
	}
	return tasks, nil
}

// fileTasks are the judged tasks of one YAML file. One the YAML library cannot parse is left to
// the root-umask check, which reports it.
func fileTasks(rel string, data []byte) []task {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var tasks []task
	for {
		var doc yaml.Node
		if err := dec.Decode(&doc); err != nil {
			return tasks
		}
		for _, top := range doc.Content {
			if top.Kind != yaml.SequenceNode {
				continue
			}
			for _, item := range top.Content {
				if item.Kind != yaml.MappingNode {
					continue
				}
				p := privilege{vars: map[string]string{}}
				if mappingValue(item, "hosts") != nil {
					p.vars = playVars(item)
					tasks = taskLists(item, rel, p, tasks)
					continue
				}
				tasks = oneTask(item, rel, p, tasks)
			}
		}
	}
}

func taskLists(m *yaml.Node, rel string, p privilege, tasks []task) []task {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if !taskListKeys[m.Content[i].Value] || m.Content[i+1].Kind != yaml.SequenceNode {
			continue
		}
		for _, t := range m.Content[i+1].Content {
			if t.Kind == yaml.MappingNode {
				tasks = oneTask(t, rel, p, tasks)
			}
		}
	}
	return tasks
}

func oneTask(m *yaml.Node, rel string, p privilege, tasks []task) []task {
	if mappingValue(m, "block") != nil {
		return taskLists(m, rel, p, tasks)
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		key := m.Content[i]
		short := key.Value[strings.LastIndex(key.Value, ".")+1:]
		if !judgedModule(short) {
			continue
		}
		args := moduleArgs(m, m.Content[i+1])
		if commandModules[short] {
			args = &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "cmd"}, {Kind: yaml.ScalarNode, Value: commandText(m.Content[i+1])},
			}}
		}
		return append(tasks, task{rel: rel, line: key.Line, module: short, args: args, p: p})
	}
	return tasks
}

// sudoersRule returns the sudoers-rule findings over the YAML under dirs, "<rel>:<line>: <why>".
func sudoersRule(fsys fs.FS, dirs []string) ([]string, error) {
	tasks, err := collectTasks(fsys, dirs)
	if err != nil {
		return nil, err
	}
	deploys, err := installerDeploys(fsys)
	if err != nil {
		return nil, err
	}
	for _, t := range tasks {
		if wholeFileModules[t.module] || t.module == fileModule {
			if d := t.dest(); d != "" && t.arg("state") != "absent" {
				deploys[d] = append(deploys[d], deploy{at: t.at(), owner: t.arg("owner"), mode: t.arg("mode"), link: linkSource(t)})
			}
		}
	}
	var findings []string
	for _, t := range tasks {
		for _, why := range judgeSudoersTask(fsys, t, deploys) {
			findings = append(findings, t.at()+": "+why)
		}
	}
	return findings, nil
}

func linkSource(t task) string {
	if t.module == fileModule && t.arg("state") == "link" {
		return t.arg("src")
	}
	return ""
}

// sudoersPath is a sudoers file: /etc/sudoers itself or one under /etc/sudoers.d.
func sudoersPath(p string) bool {
	return p == "/etc/sudoers" || strings.HasPrefix(p, "/etc/sudoers.d/")
}

// judgeSudoersTask says what is wrong with a task that writes sudoers text, or nothing.
func judgeSudoersTask(fsys fs.FS, t task, deploys map[string][]deploy) []string {
	if commandModules[t.module] {
		if strings.Contains(t.arg("cmd"), "/etc/sudoers") {
			return []string{"a command names /etc/sudoers, so the rule it writes cannot be judged or validated; " +
				"install sudoers text with a template that sets validate: visudo -cf %s"}
		}
		return nil
	}
	if t.module == fileModule || !sudoersPath(t.dest()) || t.arg("state") == "absent" {
		return nil
	}
	var why []string
	if !strings.Contains(t.arg("validate"), "visudo -c") {
		why = append(why, t.dest()+" is written without validate: visudo -cf %s, so a broken rule can lock sudo out")
	}
	if wholeFileModules[t.module] && strings.HasPrefix(t.dest(), "/etc/sudoers.d/") &&
		(t.arg("owner") != rootUser || t.arg("group") != rootUser || strings.TrimLeft(t.arg("mode"), "0") != "440") {
		why = append(why, t.dest()+" must be owner root, group root, mode \"0440\"")
	}
	text, src, err := sudoersText(fsys, t)
	if err != nil {
		return append(why, err.Error())
	}
	return append(why, judgeSudoersText(text, src, deploys)...)
}

// sudoersText is the sudoers text a task writes and where it comes from.
func sudoersText(fsys fs.FS, t task) (string, string, error) {
	switch t.module {
	case "blockinfile":
		return scalarValue(t.args, "block"), "its block", nil
	case "lineinfile":
		return scalarValue(t.args, "line"), "its line", nil
	}
	if content := scalarValue(t.args, "content"); content != "" {
		return content, "its content", nil
	}
	src := strings.TrimPrefix(t.arg("src"), "{{ root_dir }}/")
	if src == "" || strings.Contains(src, "{{") {
		return "", "", fmt.Errorf("the sudoers source %q is not a repository path, so the rule cannot be judged", t.arg("src"))
	}
	data, err := fs.ReadFile(fsys, src)
	if err != nil {
		return "", "", fmt.Errorf("the sudoers source %s cannot be read, so the rule cannot be judged: %w", src, err)
	}
	return string(data), src, nil
}

// sudoersLine is a logical line of sudoers text: continuations joined, and its first line.
type sudoersLine struct {
	text string
	line int
}

// jinjaExpr is a template expression; inner spaces are dropped so it is one word.
var jinjaExpr = regexp.MustCompile(`\{\{\s*([^}]*?)\s*\}\}`)

func sudoersLines(text string) []sudoersLine {
	var lines []sudoersLine
	var cur strings.Builder
	start := 0
	for i, raw := range strings.Split(text, "\n") {
		if cur.Len() == 0 {
			start = i + 1
			if strings.HasPrefix(strings.TrimSpace(raw), "#") && !strings.HasPrefix(strings.TrimSpace(raw), "#include") {
				continue
			}
		}
		body, more := strings.CutSuffix(strings.TrimRight(raw, " \t\r"), "\\")
		cur.WriteString(body + " ")
		if more {
			continue
		}
		if s := strings.TrimSpace(cur.String()); s != "" {
			lines = append(lines, sudoersLine{text: jinjaExpr.ReplaceAllString(s, "{{$1}}"), line: start})
		}
		cur.Reset()
	}
	return lines
}

// loosening is a Defaults option that hands the caller's environment or PATH to the command.
var loosening = regexp.MustCompile(`(?:^|[\s,])(?:(env_keep|env_file|restricted_env_file|secure_path|exempt_group)\b|` +
	`(env_delete|env_check)\s*-=|!\s*(env_reset)\b|(setenv)\b)`)

// userSpec is "USER HOSTS = (RUNAS) TAGS: COMMANDS".
var userSpec = regexp.MustCompile(`^(\S+)\s+[^=\s]+\s*=\s*(?:\(([^)]*)\)\s*)?((?:[A-Z_]+:\s*)*)(.*)$`)

// fixedUser is the play's user variable or a literal account name.
var fixedUser = regexp.MustCompile(`^(?:\{\{user_login\}\}|[a-z_][a-z0-9_-]*)$`)

// judgeSudoersText says what is wrong with sudoers text, naming its source and line.
func judgeSudoersText(text, src string, deploys map[string][]deploy) []string {
	var why []string
	aliases := map[string]string{}
	for _, l := range sudoersLines(text) {
		at := src + " line " + strconv.Itoa(l.line) + ": "
		fields := strings.Fields(l.text)
		switch first := fields[0]; {
		case strings.HasPrefix(first, "Defaults"):
			why = append(why, judgeDefaults(l.text, first, at)...)
		case strings.HasPrefix(first, "#include"), strings.HasPrefix(first, "@include"):
			why = append(why, at+"an include pulls in rules this check cannot see; write the rule here")
		case first == "Cmnd_Alias":
			if name, list, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(l.text, first)), "="); ok {
				aliases[strings.TrimSpace(name)] = list
			}
		case strings.HasSuffix(first, "_Alias"):
			why = append(why, at+"an alias ("+first+") hides who or where the rule is for; name the user in the rule")
		default:
			why = append(why, judgeUserSpec(l.text, at, aliases, deploys)...)
		}
	}
	return why
}

// judgeDefaults says what a Defaults line loosens. Negating env_keep or setenv tightens, so
// "!setenv" is not a finding; "!env_reset" always is.
func judgeDefaults(text, first, at string) []string {
	m := loosening.FindStringSubmatch(strings.TrimPrefix(text, first))
	if m == nil {
		return nil
	}
	name := strings.Join(m[1:], "")
	if m[3] == "" && strings.Contains(text, "!"+name) {
		return nil
	}
	if m[3] != "" {
		name = "!" + name
	}
	return []string{at + "Defaults sets " + name + ", which lets the caller's environment or PATH reach the command; " +
		"no sudoers rule here may loosen env_reset or secure_path"}
}

func judgeUserSpec(text, at string, aliases map[string]string, deploys map[string][]deploy) []string {
	m := userSpec.FindStringSubmatch(text)
	if m == nil {
		return []string{at + "this line is not a rule the check can read: " + text}
	}
	cmds := splitCommands(m[4], aliases)
	if len(cmds) == 1 && cmds[0] == "ALL" {
		return nil
	}
	var why []string
	if !fixedUser.MatchString(m[1]) {
		why = append(why, at+"the rule names no fixed user ("+m[1]+"); grant it to {{ user_login }} alone")
	}
	for r := range strings.SplitSeq(m[2], ",") {
		if strings.TrimSpace(r) == "ALL" {
			why = append(why, at+"the runas list holds ALL, so the command runs as anyone; name the account, (root)")
		}
	}
	if strings.Contains(m[3], "SETENV:") {
		why = append(why, at+"SETENV lets the caller set the environment the command runs in; remove it")
	}
	for _, c := range cmds {
		why = append(why, judgeSudoersCommand(c, at, deploys)...)
	}
	return why
}

// splitCommands splits a command list on its unescaped commas, expanding command aliases.
func splitCommands(list string, aliases map[string]string) []string {
	var cmds []string
	var cur strings.Builder
	flush := func() {
		c := strings.TrimSpace(cur.String())
		cur.Reset()
		if expansion, ok := aliases[c]; ok {
			cmds = append(cmds, splitCommands(expansion, nil)...)
		} else if c != "" {
			cmds = append(cmds, c)
		}
	}
	for i := 0; i < len(list); i++ {
		switch {
		case list[i] == '\\' && i+1 < len(list):
			cur.WriteByte(list[i])
			cur.WriteByte(list[i+1])
			i++
		case list[i] == ',':
			flush()
		default:
			cur.WriteByte(list[i])
		}
	}
	flush()
	return cmds
}

// wildcard is a glob character sudo expands in a path or an argument.
const wildcard = "*?["

func judgeSudoersCommand(c, at string, deploys map[string][]deploy) []string {
	if strings.HasPrefix(c, "!") {
		return []string{at + "a negated command (" + c + ") is bypassed by a copy or a link under another name; grant only what is allowed"}
	}
	target, args, hasArgs := strings.Cut(c, " ")
	args = strings.TrimSpace(args)
	switch {
	case target == "sudoedit":
		return []string{at + "sudoedit lets the caller edit files as root through an editor; this check cannot judge it"}
	case !strings.HasPrefix(target, "/"):
		return []string{at + target + " is not an absolute path"}
	case strings.ContainsAny(target, wildcard):
		return []string{at + target + " has a wildcard, so it matches paths the caller may create"}
	}
	var why []string
	switch {
	case !hasArgs || args == "":
		why = append(why, at+target+" admits any arguments; pin each argument list the target accepts "+
			"(\"\" for none, a literal, or an anchored ^...$ expression)")
	case strings.HasPrefix(args, "^") != strings.HasSuffix(args, "$"):
		why = append(why, at+target+" "+args+" is an expression not anchored at both ends, so it admits more than it shows")
	case strings.HasPrefix(args, "^"):
		if bareDot(args) {
			why = append(why, at+target+" "+args+" has a bare dot, which matches any character; use a bracket of the characters allowed")
		}
	case strings.ContainsAny(args, wildcard):
		why = append(why, at+target+" "+args+" has a wildcard argument, which matches any text, spaces and paths included")
	}
	return append(why, judgeTarget(target, at, deploys, 0)...)
}

// bareDot is whether an expression has a dot outside a bracket that no backslash escapes.
func bareDot(expr string) bool {
	inBracket := false
	for i := 0; i < len(expr); i++ {
		switch c := expr[i]; {
		case c == '\\':
			i++
		case c == '[':
			inBracket = true
		case c == ']':
			inBracket = false
		case c == '.' && !inBracket:
			return true
		}
	}
	return false
}

// judgeTarget says what lets the caller change a command a rule names, following links.
func judgeTarget(target, at string, deploys map[string][]deploy, depth int) []string {
	found := deploys[target]
	if len(found) == 0 {
		return []string{at + target + " is deployed by no task or installer here, so nothing shows it is root's alone; deploy it owner root, mode 0755"}
	}
	var why []string
	for _, d := range found {
		switch {
		case d.link != "":
			if depth > 4 {
				return append(why, at+target+" is a chain of links too long to follow")
			}
			why = append(why, judgeTarget(d.link, at, deploys, depth+1)...)
		case d.owner != rootUser:
			why = append(why, at+target+" is deployed by "+d.at+" with owner "+strconv.Quote(d.owner)+", not root")
		case d.mode == "":
			why = append(why, at+target+" is deployed by "+d.at+" with no mode, so its mode is not known; set mode: \"0755\"")
		case groupOrOtherWritable(d.mode):
			why = append(why, at+target+" is deployed by "+d.at+" with mode "+d.mode+", which the group or others can write")
		}
	}
	for dir := path.Dir(target); dir != "/" && dir != "."; dir = path.Dir(dir) {
		for _, d := range deploys[dir] {
			if (d.owner != "" && d.owner != rootUser) || groupOrOtherWritable(d.mode) {
				why = append(why, at+"the directory "+dir+" above "+target+" is made by "+d.at+" owner "+
					strconv.Quote(d.owner)+" mode "+strconv.Quote(d.mode)+", so the caller can replace the target")
			}
		}
	}
	return why
}

// symbolicWrite is a symbolic mode giving the group, others or all write.
var symbolicWrite = regexp.MustCompile(`[ugo]*[goa][ugo]*[+=][rwxXst]*w`)

func groupOrOtherWritable(mode string) bool {
	if n, err := strconv.ParseUint(mode, 8, 32); err == nil {
		return n&0o022 != 0
	}
	return symbolicWrite.MatchString(mode)
}

// installerDir holds the root installers whose put_file lines deploy files.
const installerDir = "files/usr/local/sbin"

// putFile is an installer's `put_file LABEL SRC "$DIR/name" MODE OWNER GROUP`.
var putFile = regexp.MustCompile(`(?m)^\s*put_file\s+"[^"]*"\s+"[^"]*"\s+"\$\{?([A-Z_]+)\}?/([^"/$]+)"\s+(\S+)\s+"?(\$?\{?[A-Za-z_]+\}?)"?`)

// installerDirVar is `[readonly ]NAME=[$PREFIX]/abs/path`.
var installerDirVar = regexp.MustCompile(`(?m)^\s*(?:readonly\s+)?([A-Z_]+)=(?:\$\{?PREFIX\}?)?(/[^\s;]+)\s*$`)

// installerDeploys are the files the root installers under installerDir put in place.
func installerDeploys(fsys fs.FS) (map[string][]deploy, error) {
	deploys := map[string][]deploy{}
	entries, err := fs.ReadDir(fsys, installerDir)
	if errors.Is(err, fs.ErrNotExist) {
		return deploys, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", installerDir, err)
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		rel := path.Join(installerDir, e.Name())
		data, err := fs.ReadFile(fsys, rel)
		if err != nil {
			return nil, fmt.Errorf("a file could not be read: %w", err)
		}
		addPutFiles(rel, string(data), deploys)
	}
	return deploys, nil
}

func addPutFiles(rel, text string, deploys map[string][]deploy) {
	dirs := map[string]string{}
	for _, m := range installerDirVar.FindAllStringSubmatch(text, -1) {
		dirs[m[1]] = m[2]
	}
	for _, idx := range putFile.FindAllStringSubmatchIndex(text, -1) {
		dir, ok := dirs[text[idx[2]:idx[3]]]
		if !ok {
			continue
		}
		owner := strings.Trim(text[idx[8]:idx[9]], "${}")
		if owner != rootUser && regexp.MustCompile(`(?m)^\s*(?:readonly\s+)?`+regexp.QuoteMeta(owner)+`=root\s*$`).MatchString(text) {
			owner = rootUser
		}
		at := rel + ":" + strconv.Itoa(1+strings.Count(text[:idx[0]], "\n"))
		target := dir + "/" + text[idx[4]:idx[5]]
		deploys[target] = append(deploys[target], deploy{at: at, owner: owner, mode: text[idx[6]:idx[7]]})
	}
}

// scanSudoers runs the check over the YAML under dirs and records its findings.
func scanSudoers(fsys fs.FS, dirs []string, f *Findings) error {
	findings, err := sudoersRule(fsys, dirs)
	if err != nil {
		return err
	}
	for _, v := range findings {
		f.Out = append(f.Out, "  ERROR (sudoers-rule): "+v, sudoersAdvice)
		f.Sudoers = append(f.Sudoers, v)
	}
	return nil
}
