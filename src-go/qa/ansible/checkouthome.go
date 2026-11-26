package ansible

import (
	"regexp"
	"strconv"
)

// The checkout-by-home-path rule (CLAUDE/QA.md, "checkout-by-home-path"). A play that acts on
// this repository's own checkout must find it as the checkout running the play ({{ root_dir }},
// the directory of the ansible.cfg in use), never by the path run.bash clones it to
// (~/Projects/fedora-desktop). A checkout anywhere else is never touched, and the play's own
// checks pass, because they look at the other checkout (Plan 00265 R84: play-git-hooks-security
// set core.hooksPath on ~/Projects/fedora-desktop, so another checkout ran no git hooks).
//
// The rule reports each line, other than a comment line, that names Projects/fedora-desktop as
// a whole path segment. run.bash's own clone location is bash, outside the trees this gate reads.

// ruleCheckoutHome is the rule ID, printed with every finding and the anchor of its section.
const ruleCheckoutHome = "checkout-by-home-path"

// checkoutHomePath is the usual clone path of the checkout, as a whole path segment.
var checkoutHomePath = regexp.MustCompile(`(?:^|[^\w.-])Projects/fedora-desktop(?:$|[^\w.-])`)

// checkoutByHomePath returns the rule's findings of one YAML file, "<rel>:<line>: <rule>: <why>",
// in line order.
func checkoutByHomePath(rel string, data []byte) []string {
	var findings []string
	for i, text := range splitLines(data) {
		if commentOnly(text) || !checkoutHomePath.MatchString(text) {
			continue
		}
		findings = append(findings, rel+":"+strconv.Itoa(i+1)+": "+ruleCheckoutHome+": names the checkout by its usual "+
			"clone path, so a play run from any other checkout acts on that one instead; use \"{{ root_dir }}\"")
	}
	return findings
}
