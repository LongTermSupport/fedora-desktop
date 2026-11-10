package admin

import (
	"context"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/unitfiles"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The `units` verbs are how the play learns the files it installs (Plan 00246 INSTALL-PATH.md,
// stage A): it holds neither a text nor a path of its own. They read nothing and hold no
// secret, so every class may run them; the play runs them on the target as root.

// unitsList is `units list`: one line per installed file, its name and its install path.
func unitsList(_ context.Context, e Env, args []string) term.ExitCode {
	if len(args) > 0 {
		return fail(e.Stderr, term.ExitUsage, "units list takes no arguments")
	}
	var b strings.Builder
	for _, f := range unitfiles.Files() {
		b.WriteString(f.Name + "\t" + f.Path + "\n")
	}
	return answer(e, b.String())
}

// unitsPrint is `units print <name>`: the named file's text, exactly as the play installs it.
func unitsPrint(_ context.Context, e Env, args []string) term.ExitCode {
	known := "the files are " + strings.Join(unitfiles.Names(), ", ")
	if len(args) != 1 {
		return fail(e.Stderr, term.ExitUsage, "units print takes one file name: "+known)
	}
	f, found := unitfiles.Lookup(args[0])
	if !found {
		return fail(e.Stderr, term.ExitUsage, "units print: no file "+strconv.Quote(args[0])+": "+known)
	}
	return answer(e, f.Text)
}
