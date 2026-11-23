package teamchatlaunch

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// forbiddenInPrompt is what the session's own text never mentions (Plan 00177): the human's
// Element session, a sign-in, or anyone else's account. The guard is instruction, so a later edit
// that names them would quietly lose it.
var forbiddenInPrompt = regexp.MustCompile(`(?i)element|riot|matrix session|sign|login|log in|owner|human|as anyone|impersonat`)

// promptOf is the --append-system-prompt value of argv.
func promptOf(t *testing.T, argv []string) string {
	t.Helper()
	i := slices.Index(argv, "--append-system-prompt")
	if i < 0 || i+1 >= len(argv) {
		t.Fatalf("no prompt in %q", argv)
	}
	return argv[i+1]
}

// checkPrompt holds what the session reads, on system sys posting through prog: no team named
// (they are in the session context and may change, H32), the one way to post, and nothing about
// Element, sign-ins or other people's accounts.
func checkPrompt(t *testing.T, prompt, sys, prog string) {
	t.Helper()
	if want := "Your " + sys + " teams and seats are given in the session context and may change while you run; the latest " + sys + " membership notice is current."; !strings.Contains(prompt, want) {
		t.Errorf("%s prompt %q names its teams, not the session context", sys, prompt)
	}
	if !strings.Contains(prompt, "only with "+prog) {
		t.Errorf("%s prompt %q does not say to post only with %s", sys, prompt, prog)
	}
	if m := forbiddenInPrompt.FindString(prompt); m != "" {
		t.Errorf("%s prompt %q mentions %q", sys, prompt, m)
	}
}

// The prompt cc-run hands claude on agent-bus, run through cc-run: pingbus is the way to post,
// and the session gets the v1 kit's plugin, whose SessionStart hook writes the per-session teams
// file the status line reads.
func TestCCRunOnAgentBusTellsTheSessionHowToPost(t *testing.T) {
	w := newCC(t)
	if code := w.ccRun("cc-s1", "1@v1team", "false", "--model", "opus"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err)
	}
	execs := w.exec.Execs()
	if len(execs) != 1 {
		t.Fatalf("execs %+v", execs)
	}
	argv := execs[0].Args
	checkPrompt(t, promptOf(t, argv), "agent-bus", "pingbus")
	i := slices.Index(argv, "--plugin-dir")
	if i < 0 || i+1 >= len(argv) || argv[i+1] != w.cc.V1Kit+"/plugin/pingbus" {
		t.Errorf("argv %q does not pass the kit's plugin %s/plugin/pingbus", argv, w.cc.V1Kit)
	}
}

// The prompt cc-run hands claude on teamchat, run through cc-run: teamchat is the way to post,
// and pingbus is not named.
func TestCCRunOnTeamchatTellsTheSessionHowToPost(t *testing.T) {
	w := newCC(t)
	w.pool("repo.1+site.host")
	if code := w.ccRun("cc-s1", "1@alpha", "false"); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.err)
	}
	if len(w.fg.cmds) != 1 {
		t.Fatalf("started %+v", w.fg.cmds)
	}
	prompt := promptOf(t, w.fg.cmds[0].Args)
	checkPrompt(t, prompt, "teamchat", "teamchat")
	if strings.Contains(prompt, "pingbus") {
		t.Errorf("teamchat prompt %q names pingbus", prompt)
	}
}
