package rules

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// Input from outside teamchat's protocol is never decoded whole by its strict wire parser
// (fedora-desktop#394). The fixtures are the defect (Claude Code's hook stdin handed to
// proto.Parse), the ways round it (a trimmed copy, a helper of the package, another package's
// wrapper, proto's own wrapper and a two-call relay inside proto, a line of stdin, an
// environment variable by os.Getenv, os.LookupEnv and an Env's Getenv, stdin handed back by a
// function of the package and by another package's method, a closure), and the clean forms
// (proto.ForeignObject, teamchat's own file and socket, a length of stdin, a constant beside an
// environment variable, a test, a package whose stdin is a teamchat protocol). The root is its
// own, because the protocol-stdin packages are matched by package path.
func TestForeignStrictParse(t *testing.T) {
	root := filepath.Join(analysistest.TestData(), "foreignparse")
	analysistest.Run(t, root, ForeignStrictParse,
		"fix/internal/badforeign", "fix/internal/goodforeign", "fix/internal/teamchat/element",
		"fix/internal/teamchat/proto", "fix/internal/wrap", "fix/internal/stdinread")
}
