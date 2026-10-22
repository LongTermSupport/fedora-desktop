package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Env is what a run of the binary is given: its arguments after the program name, its
// standard streams, and the source digest the build set.
type Env struct {
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Digest string
}

type runFunc func(Env, Invocation) term.ExitCode

// runs are the verbs this binary builds.
var runs = map[string]runFunc{
	verbVersion:     runVersion,
	verbHelp:        runHelp,
	verbPlaceBundle: runPlaceBundle,
	verbMembership:  runMembership,
	verbGuestRemove: runGuestRemove,
	verbSetAside:    runSetAside,
}

// unbuilt are the verbs a later slice builds, each with its slice (CL2). The guest operations
// and seat set-aside are n3a's own, built beside this package.
var unbuilt = bySlice(map[string][]string{
	"n3c":       {verbRun, "link", "recv", "wait", "inbox", "status", "verify", "history", "seat disconnected", "reach"},
	"n3d":       {"reply", "say", "ping"},
	"n3e":       {"hook", "status-line"},
	"n3f":       {"report", verbAsk, "thread", verbQuestions},
	"n3a guest": {"guest check"},
})

func bySlice(verbs map[string][]string) map[string]string {
	out := map[string]string{}
	for slice, names := range verbs {
		for _, name := range names {
			out[name] = slice
		}
	}
	return out
}

const (
	msgUnbuilt  exits.Template = "teamchat {} is not built in this binary; it lands with slice {}"
	msgNoDigest exits.Template = "this teamchat was built without its source digest; tell a human"
	msgHelpWhat exits.Template = "teamchat help takes a verb or exit-codes; teamchat help lists the verbs"
)

// Run is the binary: `teamchat --version` alone, else one verb.
func Run(e Env) term.ExitCode {
	if len(e.Args) == 1 && e.Args[0] == versionFlag {
		d, err := sourceDigest(e.Digest)
		if err != nil {
			return exits.Write(e.Stderr, err)
		}
		return answer(e, d+"\n")
	}
	inv, err := Parse(e.Args)
	if err != nil {
		return exits.Write(e.Stderr, err)
	}
	if run, ok := runs[inv.Verb.Name]; ok {
		return run(e, inv)
	}
	return exits.Write(e.Stderr, exits.New(exits.CodeInternal, inv.Team, msgUnbuilt,
		exits.Words(inv.Verb.Name), exits.Words(unbuilt[inv.Verb.Name])))
}

func sourceDigest(d string) (string, error) {
	if _, err := paths.ParseDigest(d); err != nil {
		return "", exits.New(exits.CodeInternal, nil, msgNoDigest)
	}
	return d, nil
}

func answer(e Env, text string) term.ExitCode {
	if _, err := io.WriteString(e.Stdout, text); err != nil {
		return exits.Write(e.Stderr, err)
	}
	return term.ExitOK
}

func runVersion(e Env, _ Invocation) term.ExitCode {
	d, err := sourceDigest(e.Digest)
	if err == nil {
		err = records.Write(e.Stdout, records.Version{Digest: d, Records: records.RecordVersion})
	}
	return exits.Write(e.Stderr, err)
}

// runHelp is `help`, `help <verb>` and `help exit-codes`, as text for a reader (B 2.2.1).
func runHelp(e Env, inv Invocation) term.ExitCode {
	topic := strings.Join(inv.Args, " ")
	switch topic {
	case "":
		return answer(e, helpAll())
	case topicCodes:
		return answer(e, helpExitCodes())
	}
	v, ok := lookup(topic)
	if !ok {
		return exits.Write(e.Stderr, usage(msgHelpWhat))
	}
	return answer(e, helpVerb(v))
}

func helpVerb(v Verb) string {
	return Usage(v) + "\n    " + v.Does + "\n"
}

func helpAll() string {
	var b strings.Builder
	b.WriteString("teamchat <verb> [arguments] [flags]: flags follow the verb, as --flag value.\n\nFor agents:\n")
	for _, v := range Verbs() {
		if v.Agent {
			b.WriteString(helpVerb(v))
		}
	}
	b.WriteString("\nLifecycle (run by the launchers, the host and Claude Code; not for agents):\n")
	for _, v := range Verbs() {
		if !v.Agent {
			b.WriteString(helpVerb(v))
		}
	}
	b.WriteString("\nteamchat help <verb> shows one verb; teamchat help exit-codes shows the exit codes.\n")
	return b.String()
}

func helpExitLine(r exits.Row) string {
	codes := "-"
	if r.Codes != nil {
		codes = strings.Join(r.Codes, ", ")
	}
	return fmt.Sprintf("%3d  %-20s  %s; codes: %s; next: %s", int(r.Exit), r.Class, r.Meaning, codes, r.Next)
}

func helpExitCodes() string {
	var b strings.Builder
	b.WriteString("Each exit falls in a class, which says what to do; a failing verb's error object, the last\nline of stderr, names its code and, in its message, the next step.\n\n")
	for _, c := range exits.Classes {
		fmt.Fprintf(&b, "  %-12s  %s\n", c, c.Does())
	}
	b.WriteString("\nexit class                 meaning; codes; next step\n")
	for _, r := range exits.Table() {
		b.WriteString(helpExitLine(r) + "\n")
	}
	return b.String()
}
