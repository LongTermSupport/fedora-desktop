package ansible

import (
	"regexp"
	"strings"
)

// selfDefaultHead is `<name>: "{{ <name2> | default`; the match is a self-default when the two
// names are the same (the PCRE backreference of the bash). A bare self-mention is not flagged:
// it is usually block-literal text templated into another file.
var selfDefaultHead = regexp.MustCompile(`^[[:space:]]*([A-Za-z_][A-Za-z0-9_]*):[[:space:]]*["']?\{\{[[:space:]]*([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*\|[[:space:]]*default\b`)

// selfDefaultMatch is whether a line defines a variable as its own default, which ansible-core
// 2.19 aborts on ("Recursive loop detected in template").
func selfDefaultMatch(text string) bool {
	m := selfDefaultHead.FindStringSubmatch(text)
	return m != nil && m[1] == m[2]
}

// factNames are the gathered facts injected as top-level ansible_<name> variables, removed in
// ansible-core 2.24. The list is the commonly used subset; extend it when a new fact is adopted.
const factNames = "env|distribution|distribution_version|distribution_major_version|" +
	"distribution_release|hostname|nodename|fqdn|domain|" +
	"user_id|user_dir|user_uid|user_gid|" +
	"architecture|machine|os_family|kernel|kernel_version|system|" +
	"memtotal_mb|memfree_mb|swaptotal_mb|" +
	"processor_vcpus|processor_cores|processor_count|" +
	"python_version|date_time|default_ipv4|default_ipv6|all_ipv4_addresses|" +
	"interfaces|mounts|devices|lsb|machine_id|selinux|" +
	"service_mgr|pkg_mgr|virtualization_type|virtualization_role|" +
	"product_name|product_version|form_factor|bios_version"

// unprefixedFactNames are injected by a module without the ansible_ prefix, so no ansible_
// anchored pattern reaches them.
const unprefixedFactNames = "getent_passwd|getent_group|getent_shadow|getent_hosts|" +
	"getent_services|getent_protocols|getent_ahostsv4|getent_ahostsv6"

var (
	prefixedFact   = regexp.MustCompile(`\bansible_(` + factNames + `)\b`)
	unprefixedFact = regexp.MustCompile(`\b(` + unprefixedFactNames + `)\b`)
)

// factsSubscripts end the text before an unprefixed name that is the correct form,
// ansible_facts['x'], ansible_facts["x"] or ansible_facts.x (the bash's three lookbehinds).
var factsSubscripts = []string{"ansible_facts['", `ansible_facts["`, "ansible_facts."}

// factVarMatch is whether a line names a gathered fact as a top-level variable.
func factVarMatch(text string) bool {
	if prefixedFact.MatchString(text) {
		return true
	}
	for _, loc := range unprefixedFact.FindAllStringIndex(text, -1) {
		if !endsWithAny(text[:loc[0]], factsSubscripts) {
			return true
		}
	}
	return false
}

func endsWithAny(text string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(text, suffix) {
			return true
		}
	}
	return false
}

const runUser = "/run/user/"

// runtimeDirMatch is whether a line builds a user's runtime directory from a guessed uid:
// /run/user/ followed by a digit, or by a template with no getent_passwd after it on the line.
func runtimeDirMatch(text string) bool {
	for rest := text; ; {
		at := strings.Index(rest, runUser)
		if at < 0 {
			return false
		}
		after := rest[at+len(runUser):]
		switch {
		case after != "" && after[0] >= '0' && after[0] <= '9':
			return true
		case strings.HasPrefix(after, "{{") && !strings.Contains(after[len("{{"):], "getent_passwd"):
			return true
		}
		rest = rest[at+1:]
	}
}

// commentOnly is whether a line is a YAML comment (only a comment, not a trailing one).
func commentOnly(text string) bool {
	return strings.HasPrefix(strings.TrimLeft(text, " \t\n\v\f\r"), "#")
}
