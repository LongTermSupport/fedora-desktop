// Package admin is `teamchat-admin`, the host program of the agent-team bus v2 (Plan 00209,
// DESIGN-v2-C-host.md 3.3; the slice spec is SPEC-n2a.md, CA1-CA7 and CC1-CC6): its verb table,
// the caller class each verb belongs to, C 3.3's one exit-code table, and the boundary of the
// `teamchat-ctl` child (the typed start, the request and answer on the pipe, and the child's
// check of how it was started). The verbs themselves are built slice by slice; one the table
// names that this build lacks says so and fails.
package admin

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// C 3.3's exit codes beyond term's.
const (
	ExitUnreachable term.ExitCode = 69
	ExitRefused     term.ExitCode = 70
	ExitBusy        term.ExitCode = 75
	ExitClass       term.ExitCode = 77
	ExitConfig      term.ExitCode = 78
)

// Env is one run: the arguments after the program name, the streams, the process, and the
// source digest baked into the binary.
type Env struct {
	Args           []string
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Proc           Process
	Digest         string
}

// Run is `teamchat-admin`: it finds the verb, decides the caller class, refuses the wrong one
// (CA3), and runs the verb.
func Run(e Env) term.ExitCode {
	v, rest, ok := lookup(e.Args)
	if !ok {
		return usage(e.Stderr)
	}
	class, err := classOf(e.Proc)
	if err != nil {
		return fail(e.Stderr, ExitConfig, err.Error())
	}
	if v.class != ClassAny && v.class != class {
		return fail(e.Stderr, ExitClass, v.name+" must be run by "+v.class.String())
	}
	if v.run == nil {
		return fail(e.Stderr, term.ExitFailure, v.name+" is not built yet")
	}
	return v.run(e, rest)
}

// fail says what went wrong on stderr and returns code. A stderr that cannot be written to
// leaves nothing else to report on, so the code stands.
func fail(stderr io.Writer, code term.ExitCode, message string) term.ExitCode {
	if _, err := fmt.Fprintf(stderr, "%s: %s\n", paths.AdminBinary, message); err != nil {
		return code
	}
	return code
}

// answer writes the payload to stdout; one that cannot be written is a failure (CA7).
func answer(e Env, payload string) term.ExitCode {
	if _, err := io.WriteString(e.Stdout, payload); err != nil {
		return fail(e.Stderr, term.ExitFailure, "cannot write the answer: "+err.Error())
	}
	return term.ExitOK
}

func usage(stderr io.Writer) term.ExitCode {
	var b strings.Builder
	b.WriteString("usage: " + paths.AdminBinary + " <verb> [flags]\n")
	for _, class := range []Class{ClassRoot, ClassCtl, ClassOther, ClassAny} {
		b.WriteString("verbs run by " + class.String() + ":\n")
		for _, v := range verbTable {
			if v.class == class {
				b.WriteString("  " + v.name + "\n")
			}
		}
	}
	return fail(stderr, term.ExitUsage, strings.TrimSuffix(b.String(), "\n"))
}

// version prints the source digest the build carries (CA6).
func version(e Env, args []string) term.ExitCode {
	if len(args) > 0 {
		return fail(e.Stderr, term.ExitUsage, "version takes no arguments")
	}
	d, err := paths.ParseDigest(e.Digest)
	if err != nil {
		return fail(e.Stderr, term.ExitFailure, "this build carries no valid source digest")
	}
	return answer(e, d.String()+"\n")
}

// verb is one row of C 3.3's table; run is nil while this build lacks it.
type verb struct {
	name  string
	class Class
	run   func(e Env, args []string) term.ExitCode
}

// verbTable is C 3.3's verbs (CA1).
var verbTable = buildTable()

func buildTable() []verb {
	var table []verb
	group := func(class Class, head string, words ...string) {
		if len(words) == 0 {
			table = append(table, verb{name: head, class: class})
		}
		for _, w := range words {
			table = append(table, verb{name: head + " " + w, class: class})
		}
	}
	group(ClassRoot, "team", "apply", "approve", "remove")
	group(ClassRoot, "member", "add", "return", "park", "retire", "reissue", "set")
	group(ClassRoot, "role", "apply")
	group(ClassRoot, "human", "password", "devices", "logout", "lock", "unlock")
	group(ClassRoot, "redact")
	group(ClassRoot, "backup")
	group(ClassRoot, "backups")
	group(ClassRoot, "restore")
	group(ClassRoot, "health")
	group(ClassRoot, "lxc", "apply", "restart")
	group(ClassRoot, "check")
	group(ClassRoot, "migrate", "preflight", "stage", "fence", "fence-status", "capture", "carry", "commit",
		"accept", "archive", "archive-check", "unfence", "rollback")
	group(ClassRoot, "footprint")
	table = append(table, verb{name: "ctl-child", class: ClassCtl, run: ctlChild})
	group(ClassOther, "declare", "check")
	group(ClassOther, "element", "check", "apply")
	group(ClassOther, "status")
	return append(table, verb{name: "version", class: ClassAny, run: version})
}

// lookup is the verb args name, two words before one (CA1), and the arguments after it.
func lookup(args []string) (verb, []string, bool) {
	for _, n := range []int{2, 1} {
		if len(args) < n {
			continue
		}
		name := strings.Join(args[:n], " ")
		if i := slices.IndexFunc(verbTable, func(v verb) bool { return v.name == name }); i >= 0 {
			return verbTable[i], args[n:], true
		}
	}
	return verb{}, nil, false
}
