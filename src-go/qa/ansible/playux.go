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

// The play-UX rules (CLAUDE/QA.md, one section per rule ID). Each defends a defect class an
// optional play hit on a real install, found only by running it there:
//
//   - setting-checked-late: a play checks a setting the install supplies (a host_vars variable
//     or a vault secret) only after it has changed the system, so a missing value stops the run
//     half done. A setting is a variable whose definedness an assert's `that` or a fail's `when`
//     tests (`is defined`, `is not defined`, `is undefined`, `| default(...) | length`; a flag
//     read through `| default(false) | bool` is its value, not a presence check), or a block that
//     ends the play tests in its when (a skip), that the repository's
//     own vars (vars/, environment/**/group_vars/), the play's vars, a task's vars, a loop or
//     an earlier task (register, set_fact, vars_prompt) do not supply. A change is any task but
//     the read-only modules, a command with changed_when false, and a check_mode run; an include
//     is taken as a change, because what it runs is not read here, except the shared asker
//     tasks/ask-and-save-secret.yml, which supplies the secret its vars name (secret_name).
//   - vault-secret-by-hand: an assert, fail or skip on a secret (a setting named like a password,
//     key, token, secret, psk, passphrase or webhook) whose message sends the human to
//     ansible-vault, vault.bash or a vault-encrypted hand edit, where the play could ask for it
//     and save it.
//   - ansible-cli-without-chdir: a command, shell or raw task that runs an Ansible command-line
//     tool (ansible, ansible-vault, ansible-playbook, ...) without chdir to {{ root_dir }} (or a
//     cd there first): the tool reads ansible.cfg from its working directory, and the vault
//     password file, inventory and roles path it names are relative to that.
//   - no-log-hides-failure: a task under no_log that can fail on the machine (any module but
//     set_fact, pause, debug and meta) and is not a probe: failed_when false, registered, and
//     read by a later fail or assert that is not itself under no_log, nor in a block whose
//     rescue has a fail or assert not under no_log. Ansible replaces a no_log
//     task's whole result, its error included, with "the output has been hidden".
//   - module-built-for-running-kernel: akmods, dkms autoinstall/install/build or dracut --force
//     run for the running kernel (no kernel named, or uname -r): after the system upgrade the
//     next boot runs a newer kernel, the only one kernel-devel is installed for.
//   - probe-before-install: a probe (a command with failed_when false) of a program that a later
//     package task of the same play installs: on a machine without it the probe cannot run, and
//     failed_when false takes "not installed" as the probe's answer.

// Task and play keys and module names the rules read.
const (
	keyVars         = "vars"
	keyEnvironment  = "environment"
	keyBlock        = "block"
	keyRescue       = "rescue"
	keyAlways       = "always"
	keyPreTasks     = "pre_tasks"
	keyPostTasks    = "post_tasks"
	keyHandlers     = "handlers"
	modAssert       = "assert"
	modFail         = "fail"
	modSetFact      = "set_fact"
	modDebug        = "debug"
	modCommand      = "command"
	modShell        = "shell"
	modRaw          = "raw"
	modMeta         = "meta"
	modIncludeTasks = "include_tasks"
	modImportTasks  = "import_tasks"
	// modEndPlayBlock is a block that ends the play: a skip, judged as a check of its when.
	modEndPlayBlock = "block ending the play"
)

// The rule IDs, printed with every finding and the anchors of their CLAUDE/QA.md sections.
const (
	ruleSettingLate = "setting-checked-late"
	ruleVaultByHand = "vault-secret-by-hand"
	ruleCLIChdir    = "ansible-cli-without-chdir"
	ruleNoLogHides  = "no-log-hides-failure"
	ruleProbeFirst  = "probe-before-install"
	ruleRunningKern = "module-built-for-running-kernel"
)

// uxTask is a task with its module and whether no_log is in effect for it.
type uxTask struct {
	node      *yaml.Node
	module    string
	moduleKey *yaml.Node
	noLog     bool
	// rescued is whether the task is in a block whose rescue explains a failure readably.
	rescued bool
}

// taskKeywords are the keys of a task that are not its module.
var taskKeywords = map[string]bool{
	"name": true, "when": true, "register": true, "become": true, "become_user": true, "become_method": true,
	"become_flags": true, "become_exe": true, "changed_when": true, "failed_when": true, "ignore_errors": true,
	"ignore_unreachable": true, "no_log": true, "tags": true, keyVars: true, "loop": true, "loop_control": true,
	"until": true, "retries": true, "delay": true, "notify": true, "args": true, keyEnvironment: true,
	"delegate_to": true, "delegate_facts": true, "run_once": true, "check_mode": true, "diff": true,
	keyBlock: true, keyRescue: true, keyAlways: true, "any_errors_fatal": true, "async": true, "poll": true,
	"listen": true, "timeout": true, "throttle": true, "connection": true, "module_defaults": true,
	"collections": true, "debugger": true, "remote_user": true, "port": true,
}

// moduleOf is a task's module key, or nil (a block, or a task with only keywords).
func moduleOf(task *yaml.Node) *yaml.Node {
	for i := 0; i+1 < len(task.Content); i += 2 {
		key := task.Content[i].Value
		if !taskKeywords[key] && !strings.HasPrefix(key, "with_") {
			return task.Content[i]
		}
	}
	return nil
}

// shortName is a module's name without its collection.
func shortName(key string) string { return key[strings.LastIndex(key, ".")+1:] }

// boolOf decodes a directive: its value, and whether it is a plain boolean. A template is not.
func boolOf(v *yaml.Node) (bool, bool) {
	if v == nil || v.Kind != yaml.ScalarNode {
		return false, false
	}
	var value bool
	if v.Decode(&value) == nil {
		return value, true
	}
	switch strings.ToLower(strings.TrimSpace(v.Value)) {
	case "false", "no", "off":
		return false, true
	case "true", "yes", "on":
		return true, true
	}
	return false, false
}

// noLogOf is whether no_log is on for a task or block given the one it inherits; a templated
// no_log is taken as on.
func noLogOf(m *yaml.Node, inherited bool) bool {
	v := mappingValue(m, "no_log")
	if v == nil {
		return inherited
	}
	if value, plain := boolOf(v); plain {
		return value
	}
	return true
}

// isFalse is whether a directive is the plain boolean false.
func isFalse(v *yaml.Node) bool {
	value, plain := boolOf(v)
	return plain && !value
}

// flatten lists the tasks of a task list in run order, blocks opened (block, rescue, always).
func flatten(list *yaml.Node, noLog bool, out *[]uxTask) {
	flattenIn(list, noLog, false, out)
}

// flattenIn is flatten inside blocks: rescued is whether an enclosing block's rescue explains
// a failure of its tasks.
func flattenIn(list *yaml.Node, noLog, rescued bool, out *[]uxTask) {
	if list == nil || list.Kind != yaml.SequenceNode {
		return
	}
	for _, task := range list.Content {
		if task.Kind != yaml.MappingNode {
			continue
		}
		nl := noLogOf(task, noLog)
		if block := mappingValue(task, keyBlock); block != nil {
			if endsPlay(block) {
				*out = append(*out, uxTask{node: task, module: modEndPlayBlock, moduleKey: blockKey(task)})
			}
			flattenIn(block, nl, rescued || readableRescue(mappingValue(task, keyRescue), nl), out)
			flattenIn(mappingValue(task, keyRescue), nl, rescued, out)
			flattenIn(mappingValue(task, keyAlways), nl, rescued, out)
			continue
		}
		t := uxTask{node: task, noLog: nl, rescued: rescued}
		if key := moduleOf(task); key != nil {
			t.module, t.moduleKey = shortName(key.Value), key
		} else {
			t.moduleKey = task.Content[0]
		}
		*out = append(*out, t)
	}
}

// readableRescue is whether a rescue list holds a fail or assert not under no_log: what it
// prints (ansible_failed_result's msg) is not hidden.
func readableRescue(rescue *yaml.Node, noLog bool) bool {
	if rescue == nil || rescue.Kind != yaml.SequenceNode {
		return false
	}
	for _, task := range rescue.Content {
		if task.Kind != yaml.MappingNode {
			continue
		}
		if key := moduleOf(task); key != nil {
			module := shortName(key.Value)
			if (module == modFail || module == modAssert) && !noLogOf(task, noLog) {
				return true
			}
		}
	}
	return false
}

// endsPlay is whether a block's own tasks end the play or host: a skip.
func endsPlay(block *yaml.Node) bool {
	if block.Kind != yaml.SequenceNode {
		return false
	}
	for _, task := range block.Content {
		if task.Kind != yaml.MappingNode {
			continue
		}
		if key := moduleOf(task); key != nil && shortName(key.Value) == modMeta {
			if v := strings.TrimSpace(nodeText(mappingValue(task, key.Value))); v == "end_play" || v == "end_host" {
				return true
			}
		}
	}
	return false
}

// blockKey is a block's "block" key node, the line a skip is reported at.
func blockKey(task *yaml.Node) *yaml.Node {
	for i := 0; i+1 < len(task.Content); i += 2 {
		if task.Content[i].Value == keyBlock {
			return task.Content[i]
		}
	}
	return task.Content[0]
}

// moduleValue is the module's argument node.
func (t uxTask) moduleValue() *yaml.Node {
	return mappingValue(t.node, t.moduleKey.Value)
}

// line is the task's module line.
func (t uxTask) line() string { return strconv.Itoa(t.moduleKey.Line) }

// nodeText is a scalar's value, or a sequence's scalar items joined by newlines.
func nodeText(v *yaml.Node) string {
	if v == nil {
		return ""
	}
	switch v.Kind {
	case yaml.ScalarNode:
		return v.Value
	case yaml.SequenceNode:
		parts := make([]string, 0, len(v.Content))
		for _, item := range v.Content {
			parts = append(parts, nodeText(item))
		}
		return strings.Join(parts, "\n")
	case yaml.DocumentNode, yaml.MappingNode, yaml.AliasNode:
	}
	return ""
}

// checkText is what an assert or fail decides on: an assert's that, a fail's when.
func (t uxTask) checkText() string {
	switch t.module {
	case modAssert:
		if args := t.moduleValue(); args != nil && args.Kind == yaml.MappingNode {
			return nodeText(mappingValue(args, "that"))
		}
	case modFail, modEndPlayBlock:
		return nodeText(mappingValue(t.node, "when"))
	}
	return ""
}

// message is an assert's or fail's message text, or what a skip's debug tasks print.
func (t uxTask) message() string {
	if t.module == modEndPlayBlock {
		var parts []string
		for _, task := range mappingValue(t.node, keyBlock).Content {
			if key := moduleOf(task); task.Kind == yaml.MappingNode && key != nil && shortName(key.Value) == modDebug {
				parts = append(parts, uxTask{node: task, moduleKey: key}.message())
			}
		}
		return strings.Join(parts, "\n")
	}
	args := t.moduleValue()
	if args == nil || args.Kind != yaml.MappingNode {
		return ""
	}
	return nodeText(mappingValue(args, "fail_msg")) + "\n" + nodeText(mappingValue(args, "msg"))
}

// conditionText is every condition of a task: when, that, failed_when.
func (t uxTask) conditionText() string {
	return nodeText(mappingValue(t.node, "when")) + "\n" + t.checkText() + "\n" + nodeText(mappingValue(t.node, "failed_when"))
}

// definednessTest is a variable whose definedness is tested, the base name the first group.
var definednessTest = regexp.MustCompile(`(?:^|[^\w.'"\]])([A-Za-z_][A-Za-z0-9_]*)(?:\.[A-Za-z_][A-Za-z0-9_]*|\[[^\]]*\])*` +
	`(?:\s+is\s+(?:not\s+)?(?:defined|undefined)\b|\s*\|\s*default\([^)]*\)\s*\|\s*length\b)`)

// magicVars are names Ansible supplies, which are no setting.
var magicVars = map[string]bool{
	"hostvars": true, "groups": true, "group_names": true, "inventory_hostname": true, "inventory_hostname_short": true,
	"play_hosts": true, "playbook_dir": true, "role_path": true, "omit": true, "item": true, keyVars: true,
	"lookup": true, "query": true, "true": true, "false": true, "none": true, "True": true, "False": true, "None": true,
}

// settingsIn is the settings a check's text tests, in order: tested names the play does not
// supply.
func settingsIn(text string, known func(string) bool) []string {
	var found []string
	for _, m := range definednessTest.FindAllStringSubmatch(text, -1) {
		name := m[1]
		if strings.HasPrefix(name, "ansible_") || magicVars[name] || known(name) {
			continue
		}
		found = append(found, name)
	}
	return found
}

// producedBy is the names a task supplies to the tasks after it.
func producedBy(task *yaml.Node, module string, add func(string)) {
	if r := scalarValue(task, "register"); r != "" {
		add(r)
	}
	if lc := mappingValue(task, "loop_control"); lc != nil && lc.Kind == yaml.MappingNode {
		if lv := scalarValue(lc, "loop_var"); lv != "" {
			add(lv)
		}
	}
	if module == modIncludeTasks || module == modImportTasks {
		if vars := mappingValue(task, keyVars); vars != nil && vars.Kind == yaml.MappingNode {
			if name := scalarValue(vars, "secret_name"); name != "" {
				add(name)
			}
		}
	}
	if module == modSetFact {
		if args := mappingValue(task, moduleOf(task).Value); args != nil && args.Kind == yaml.MappingNode {
			for i := 0; i < len(args.Content); i += 2 {
				add(args.Content[i].Value)
			}
		}
	}
}

// mappingKeys calls add with each key of a mapping.
func mappingKeys(m *yaml.Node, add func(string)) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i < len(m.Content); i += 2 {
		add(m.Content[i].Value)
	}
}

// readOnlyModules change nothing on the machine.
var readOnlyModules = map[string]bool{
	modAssert: true, modFail: true, modDebug: true, modSetFact: true, "include_vars": true, "stat": true,
	"getent": true, "find": true, "slurp": true, "package_facts": true, "service_facts": true, "setup": true,
	"gather_facts": true, "pause": true, modMeta: true, "wait_for": true, "ping": true,
	modEndPlayBlock: true,
}

// askerFile is the shared task file that asks for a secret and saves it to the checkout's
// localhost.yml: it changes no part of the machine a play installs.
const askerFile = "/ask-and-save-secret.yml"

// asksForSecret is whether a task includes the shared asker.
func asksForSecret(t uxTask) bool {
	if t.module != modIncludeTasks && t.module != modImportTasks {
		return false
	}
	v := t.moduleValue()
	file := nodeText(v)
	if v != nil && v.Kind == yaml.MappingNode {
		file = scalarValue(v, "file")
	}
	return strings.HasSuffix(strings.TrimSpace(file), askerFile)
}

// changes is whether a task changes the machine.
func changes(t uxTask) bool {
	if readOnlyModules[t.module] || asksForSecret(t) {
		return false
	}
	if on, plain := boolOf(mappingValue(t.node, "check_mode")); plain && on {
		return false
	}
	switch t.module {
	case modCommand, modShell, modRaw:
		return !isFalse(mappingValue(t.node, "changed_when"))
	case "uri":
		args := t.moduleValue()
		method := ""
		if args != nil && args.Kind == yaml.MappingNode {
			method = strings.ToUpper(scalarValue(args, "method"))
		}
		return method != "" && method != "GET" && method != "HEAD"
	}
	return true
}

// secretName is a setting named like a secret.
var secretName = regexp.MustCompile(`(?i)password|passphrase|secret|token|psk|api_key|_key$|webhook`)

// byHandRemedy is a message that sends the human to make a vault secret by hand.
var byHandRemedy = regexp.MustCompile(`(?i)ansible-vault|vault-encrypt|encrypt_string|vault\.bash`)

// judgeSettingChecks applies setting-checked-late (when ordered) and vault-secret-by-hand to
// a play's or task list's tasks in run order. known says whether a name is supplied by the
// play; the names the tasks produce are added as they are passed.
func judgeSettingChecks(rel string, tasks []uxTask, ordered bool, known, repoVars map[string]bool) []string {
	var findings []string
	var change *uxTask
	for i := range tasks {
		t := tasks[i]
		settings := t.settings(known, repoVars)
		if len(settings) > 0 && ordered && change != nil {
			findings = append(findings, rel+":"+t.line()+": "+ruleSettingLate+": the setting "+settings[0]+
				" is checked here, after the "+change.module+" task at line "+change.line()+
				" has already changed the system; check it, or ask for it, before the first change")
		}
		if f := judgeByHand(rel, t, settings); f != "" {
			findings = append(findings, f)
		}
		if t.module != "" {
			producedBy(t.node, t.module, func(n string) { known[n] = true })
		}
		if change == nil && changes(t) {
			change = &tasks[i]
		}
	}
	return findings
}

// settings is the settings a task's check tests: names neither the repository, the play, the
// tasks before it, nor the task's own vars and loop variable supply.
func (t uxTask) settings(known, repoVars map[string]bool) []string {
	local := map[string]bool{"item": true}
	mappingKeys(mappingValue(t.node, keyVars), func(n string) { local[n] = true })
	if lc := mappingValue(t.node, "loop_control"); lc != nil && lc.Kind == yaml.MappingNode {
		local[scalarValue(lc, "loop_var")] = true
	}
	return settingsIn(t.checkText(), func(n string) bool { return known[n] || repoVars[n] || local[n] })
}

// judgeByHand applies vault-secret-by-hand to a task's check of settings.
func judgeByHand(rel string, t uxTask, settings []string) string {
	for _, s := range settings {
		if !secretName.MatchString(s) {
			continue
		}
		if m := byHandRemedy.FindString(t.message()); m != "" {
			return rel + ":" + t.line() + ": " + ruleVaultByHand + ": the secret " + s +
				" is demanded by hand (the message sends the human to " + strings.ToLower(m) +
				"); ask for it and save it with tasks/ask-and-save-secret.yml"
		}
	}
	return ""
}

// kernelBuild is a kernel module build or initramfs rebuild, the tool the first group: akmods,
// dkms autoinstall/install/build, or dracut --force.
var kernelBuild = regexp.MustCompile(`(?:^|[\s;&|(/])(akmods|dkms\s+(?:autoinstall|install|build)|dracut)(?:$|\s)`)

// kernelNamed is a kernel named on the command line, not the running one: akmods --kernels,
// dkms -k / --kernelver, dracut --kver or --regenerate-all, given anything but uname -r.
var kernelNamed = regexp.MustCompile(`(?:--kernels|\s-k|--kernelver|--kver)[\s=]+["']?(?:[^\s"'$]|\$[A-Za-z{])|--regenerate-all`)

// unameKernel is the running kernel spelled into a command.
var unameKernel = regexp.MustCompile(`\$\(\s*uname\s+-r\s*\)|\x60\s*uname\s+-r\s*\x60|ansible_facts(?:\.kernel|\[["']kernel["']\])|ansible_kernel`)

// judgeRunningKernel applies module-built-for-running-kernel: a kernel module build or
// initramfs rebuild for the running kernel, by default or by uname -r. A play that has just
// upgraded the system boots a newer kernel next, and kernel-devel exists only for that one.
func judgeRunningKernel(rel string, t uxTask) string {
	if t.module != modCommand && t.module != modShell {
		return ""
	}
	v := t.moduleValue()
	text := commandText(v)
	if args := mappingValue(t.node, "args"); text == "" && args != nil {
		text = commandText(args)
	}
	for _, line := range regexp.MustCompile(`\n|&&|;|\|\|`).Split(text, -1) {
		m := kernelBuild.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		tool := strings.Join(strings.Fields(m[1]), " ")
		if tool == "dracut" && !strings.Contains(line, "--force") {
			continue
		}
		if kernelNamed.MatchString(line) && !unameKernel.MatchString(line) {
			continue
		}
		return rel + ":" + t.line() + ": " + ruleRunningKern + ": " + tool + " builds for the running kernel, but after " +
			"a system upgrade the next boot runs a newer one, the only one with kernel-devel; name the boot kernel " +
			"(tasks/boot-kernel-version.yml sets boot_kernel_version)"
	}
	return ""
}

// templatedName is a variable a template puts in, the name the first group.
var templatedName = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*(?:\||\}\})`)

// judgePlainSave applies vault-secret-by-hand's second shape: a blockinfile or lineinfile that
// writes a secret into the install's host_vars as plain text, to be vaulted by hand later.
func judgePlainSave(rel string, t uxTask) string {
	if !lineModules[t.module] {
		return ""
	}
	args := t.moduleValue()
	if args == nil || args.Kind != yaml.MappingNode || !strings.Contains(scalarValue(args, "path"), "host_vars/") {
		return ""
	}
	text := scalarValue(args, "block") + "\n" + scalarValue(args, "line")
	for _, m := range templatedName.FindAllStringSubmatch(text, -1) {
		if secretName.MatchString(m[1]) {
			return rel + ":" + t.line() + ": " + ruleVaultByHand + ": the secret " + m[1] + " is saved to host_vars as " +
				"plain text, to be vault-encrypted by hand; ask for it and save it with tasks/ask-and-save-secret.yml"
		}
	}
	return ""
}

// ansibleCLI is an Ansible command-line tool run as a command: at the start of the text or
// after a shell operator, behind any sudo, exec, env or assignments, by name or path.
var ansibleCLI = regexp.MustCompile(`(?m)(?:^|[;&|(\x60]|\$\()\s*(?:(?:sudo|exec|env|command|nice|time)\s+(?:-\S+\s+)*)*` +
	`(?:[A-Za-z_][A-Za-z0-9_]*=\S*\s+)*(?:\S*/)?(ansible(?:-[a-z]+)?)(?:\s|$)`)

// cdRoot is a shell cd to the checkout.
var cdRoot = regexp.MustCompile(`\bcd\s+["']?\{\{\s*root_dir\s*\}\}`)

// judgeCLIChdir applies ansible-cli-without-chdir to one task.
func judgeCLIChdir(rel string, t uxTask) string {
	if t.module != modCommand && t.module != modShell && t.module != modRaw {
		return ""
	}
	v := t.moduleValue()
	args := mappingValue(t.node, "args")
	text := commandText(v)
	if text == "" && args != nil {
		text = commandText(args)
	}
	m := ansibleCLI.FindStringSubmatchIndex(text)
	if m == nil {
		return ""
	}
	chdir := ""
	for _, holder := range []*yaml.Node{v, args} {
		if holder != nil && holder.Kind == yaml.MappingNode && chdir == "" {
			chdir = scalarValue(holder, "chdir")
		}
	}
	if strings.Contains(chdir, "root_dir") {
		return ""
	}
	if cd := cdRoot.FindStringIndex(text); cd != nil && cd[0] < m[2] {
		return ""
	}
	return rel + ":" + t.line() + ": " + ruleCLIChdir + ": " + text[m[2]:m[3]] + " runs outside the checkout, so it cannot " +
		"find ansible.cfg (whose vault password file, inventory and roles path are relative to it); add chdir: \"{{ root_dir }}\""
}

// noFailModules cannot fail on the machine: what they do is decided by the play's own text.
var noFailModules = map[string]bool{modSetFact: true, "pause": true, modDebug: true, modMeta: true, modEndPlayBlock: true}

// judgeNoLog applies no-log-hides-failure to the tasks of a play or task list, in run order.
func judgeNoLog(rel string, tasks []uxTask) []string {
	var findings []string
	for i, t := range tasks {
		if !t.noLog || t.rescued || noFailModules[t.module] || t.module == "" {
			continue
		}
		if readByLaterCheck(t, tasks[i+1:]) {
			continue
		}
		findings = append(findings, rel+":"+t.line()+": "+ruleNoLogHides+": no_log hides why this "+t.module+
			" task failed, so the run stops with no reason; register it with failed_when: false (# FAIL-FAST-OK) "+
			"and follow it with a fail task whose msg gives the non-secret reason")
	}
	return findings
}

// readByLaterCheck is whether a task is a probe whose result a later fail or assert, itself not
// under no_log, reads.
func readByLaterCheck(t uxTask, later []uxTask) bool {
	register := scalarValue(t.node, "register")
	if register == "" || !isFalse(mappingValue(t.node, "failed_when")) {
		return false
	}
	reads := regexp.MustCompile(`\b` + regexp.QuoteMeta(register) + `\b`)
	for _, next := range later {
		if (next.module == modFail || next.module == modAssert) && !next.noLog && reads.MatchString(next.conditionText()) {
			return true
		}
	}
	return false
}

// playUX returns the six rules' findings of one YAML file, "<rel>:<line>: <rule>: <why>", in
// document order by rule. A file the YAML library cannot parse is a finding.
func playUX(rel string, data []byte, repoVars map[string]bool) []string {
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
			return append(findings, rel+":"+line+": cannot be parsed as YAML, so its plays cannot be judged: "+err.Error())
		}
		for _, top := range doc.Content {
			findings = append(findings, judgeTop(rel, top, repoVars)...)
		}
	}
}

// judgeTop judges a document's top-level list: plays, or the tasks of a task list.
func judgeTop(rel string, top *yaml.Node, repoVars map[string]bool) []string {
	if top.Kind != yaml.SequenceNode {
		return nil
	}
	var findings []string
	var loose []uxTask
	for _, item := range top.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if mappingValue(item, "hosts") == nil {
			if mappingValue(item, "import_playbook") == nil {
				flatten(&yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{item}}, false, &loose)
			}
			continue
		}
		findings = append(findings, judgePlay(rel, item, repoVars)...)
	}
	if len(loose) > 0 {
		findings = append(findings, judgeTasks(rel, loose, nil, false, map[string]bool{}, repoVars)...)
	}
	return findings
}

// judgePlay judges one play: its tasks in run order, then its handlers.
func judgePlay(rel string, play *yaml.Node, repoVars map[string]bool) []string {
	noLog := noLogOf(play, false)
	var ordered, handlers []uxTask
	for _, key := range []string{keyPreTasks, tasksKey, keyPostTasks} {
		flatten(mappingValue(play, key), noLog, &ordered)
	}
	flatten(mappingValue(play, keyHandlers), noLog, &handlers)
	known := map[string]bool{}
	mappingKeys(mappingValue(play, keyVars), func(n string) { known[n] = true })
	if prompts := mappingValue(play, "vars_prompt"); prompts != nil && prompts.Kind == yaml.SequenceNode {
		for _, p := range prompts.Content {
			if p.Kind == yaml.MappingNode {
				known[scalarValue(p, "name")] = true
			}
		}
	}
	for _, key := range []string{keyPreTasks, tasksKey, keyPostTasks, keyHandlers} {
		markBlockVars(mappingValue(play, key), known)
	}
	return judgeTasks(rel, ordered, handlers, true, known, repoVars)
}

// markBlockVars adds the vars of every block to known: a block's vars are its own tasks'.
func markBlockVars(list *yaml.Node, known map[string]bool) {
	if list == nil || list.Kind != yaml.SequenceNode {
		return
	}
	for _, task := range list.Content {
		if task.Kind != yaml.MappingNode || mappingValue(task, keyBlock) == nil {
			continue
		}
		mappingKeys(mappingValue(task, keyVars), func(n string) { known[n] = true })
		for _, key := range []string{keyBlock, keyRescue, keyAlways} {
			markBlockVars(mappingValue(task, key), known)
		}
	}
}

// judgeTasks runs the six rules over a play's or task list's tasks.
func judgeTasks(rel string, ordered, handlers []uxTask, isPlay bool, known, repoVars map[string]bool) []string {
	all := append(append([]uxTask{}, ordered...), handlers...)
	findings := judgeSettingChecks(rel, ordered, isPlay, known, repoVars)
	for _, t := range all {
		if f := judgeCLIChdir(rel, t); f != "" {
			findings = append(findings, f)
		}
		if f := judgePlainSave(rel, t); f != "" {
			findings = append(findings, f)
		}
		if f := judgeRunningKernel(rel, t); f != "" {
			findings = append(findings, f)
		}
	}
	findings = append(findings, judgeNoLog(rel, all)...)
	return append(findings, judgeProbeFirst(rel, ordered)...)
}

// packageModules install packages by name.
var packageModules = map[string]bool{"dnf": true, "dnf5": true, "package": true, "yum": true}

// probeProgram is the program a probe runs: the first word of its command, by base name.
var probeProgram = regexp.MustCompile(`^\s*(?:\S*/)?([A-Za-z0-9][A-Za-z0-9._+-]*)`)

// judgeProbeFirst applies probe-before-install: a probe (failed_when false) whose program a later
// package task of the same play installs. On a machine without it the probe cannot run, and its
// failed_when false reads "not installed" as the probe's answer.
func judgeProbeFirst(rel string, tasks []uxTask) []string {
	var findings []string
	for i, t := range tasks {
		if (t.module != modCommand && t.module != modShell) || !isFalse(mappingValue(t.node, "failed_when")) {
			continue
		}
		m := probeProgram.FindStringSubmatch(commandText(t.moduleValue()))
		if m == nil || installedBefore(tasks[:i], m[1]) {
			continue
		}
		for _, later := range tasks[i+1:] {
			if packageModules[later.module] && installs(later, m[1]) {
				findings = append(findings, rel+":"+t.line()+": "+ruleProbeFirst+": this probe runs "+m[1]+", which the "+
					later.module+" task at line "+later.line()+" installs only later, so on a machine without it the probe "+
					"fails and failed_when: false takes that as its answer; install "+m[1]+" before the probe")
				break
			}
		}
	}
	return findings
}

// installedBefore is whether an earlier package task installs a program.
func installedBefore(earlier []uxTask, name string) bool {
	for _, t := range earlier {
		if packageModules[t.module] && installs(t, name) {
			return true
		}
	}
	return false
}

// installs is whether a package task names a package.
func installs(t uxTask, name string) bool {
	args := t.moduleValue()
	if args == nil || args.Kind != yaml.MappingNode {
		return false
	}
	for line := range strings.SplitSeq(nodeText(mappingValue(args, "name")), "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}

// repositoryVars is the top-level keys of the repository's own variable files: vars/ and the
// inventory's group_vars. A host_vars file is the install's settings and is not read.
func repositoryVars(fsys fs.FS) (map[string]bool, error) {
	names := map[string]bool{}
	var files []string
	for _, dir := range []string{keyVars, keyEnvironment} {
		if !dirExists(fsys, dir) {
			continue
		}
		found, err := yamlFiles(fsys, dir, false)
		if err != nil {
			return nil, err
		}
		for _, rel := range found {
			if dir == keyVars || strings.Contains(rel, "/group_vars/") {
				files = append(files, rel)
			}
		}
	}
	for _, rel := range files {
		data, err := fs.ReadFile(fsys, rel)
		if err != nil {
			return nil, fmt.Errorf("a file could not be read: %w", err)
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("%s cannot be parsed as YAML, so its variables cannot be known: %w", rel, err)
		}
		if len(doc.Content) == 1 {
			mappingKeys(doc.Content[0], func(n string) { names[n] = true })
		}
	}
	return names, nil
}

// scanPlayUX runs the six rules over the YAML files under dirs (roles/vendor skipped).
func scanPlayUX(fsys fs.FS, dirs []string, f *Findings) error {
	repoVars, err := repositoryVars(fsys)
	if err != nil {
		return err
	}
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
			for _, v := range playUX(rel, data, repoVars) {
				f.Out = append(f.Out, "  ERROR (play-ux): "+v, playUXAdvice(v))
				f.PlayUX = append(f.PlayUX, v)
			}
		}
	}
	return nil
}

// findingRule is the rule ID of a finding line.
var findingRule = regexp.MustCompile(`^[^:]+:[0-9]+: ([a-z-]+): `)

// playUXAdvice is the line printed under a finding: where its rule is documented.
func playUXAdvice(finding string) string {
	if m := findingRule.FindStringSubmatch(finding); m != nil {
		return "    Why and how to fix: CLAUDE/QA.md#" + m[1]
	}
	return "    A file that is not YAML cannot be judged; fix its syntax"
}
