package rules

import (
	"go/ast"
	"go/constant"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// inboxSocketFile is the one file that may name Claude Code's inbox socket directory: the
// resolver that applies Claude Code's own order of places for it. It is matched as a path suffix
// on a whole element, so the fixtures and the real module share it.
const inboxSocketFile = "internal/teamchat/paths/inboxsocket.go"

// inboxSocketRuleFile is this rule, which names the directory to find it.
const inboxSocketRuleFile = "qa/rules/inboxsocketdir.go"

// inboxSocketDirName is the directory name Claude Code keeps its inbox sockets in, under the
// directory its order picks (and, as cc-socks-<uid>, its fallback for a path too long to bind).
const inboxSocketDirName = "cc-socks"

const inboxSocketFix = " (project_inbox_socket_dir): Claude Code picks the directory by its own order (XDG_RUNTIME_DIR, else CLAUDE_CODE_TMPDIR, else the temporary directory, and cc-socks-<uid> for a path too long); ask paths.InboxSocket or paths.IsInboxSocket, the one resolver of it"

// InboxSocketDir confines the naming of Claude Code's inbox socket directory to its one resolver.
var InboxSocketDir = &analysis.Analyzer{
	Name: "project_inbox_socket_dir",
	Doc: `project_inbox_socket_dir flags, in every package of both modules, a constant string
expression whose value contains "cc-socks", the directory Claude Code keeps its inbox sockets
in, except in the one file internal/teamchat/paths/inboxsocket.go, the resolver (and this rule's
own file, qa/rules/inboxsocketdir.go).

Claude Code places the socket of session process pid at <dir>/cc-socks/<pid>.sock, where dir is
XDG_RUNTIME_DIR, else CLAUDE_CODE_TMPDIR, else the temporary directory (TMPDIR, TMP, TEMP, else
/tmp), and at /tmp/cc-socks-<uid>/<pid>.sock when that path is longer than a socket address
holds. A second place that builds or matches the path applies some other order: the teamchat
link took XDG_RUNTIME_DIR alone, so in a ccy container with none set it expected
/cc-socks/<pid>.sock while Claude Code had bound /tmp/cc-socks/<pid>.sock, and refused every
hook of the session as "not a socket of the session's processes".

A constant string expression is judged whole, so the name spelt in pieces ("cc-" + "socks") is
the name, in a call, a comparison, a prefix cut or a constant declaration (reported where it is
declared, not at each use).

Test files are not judged: they spell an expected path, the oracle a resolver is checked
against, and resolve nothing Claude Code's process reaches. The exemption is the whole path, so
another file of package paths, and a file of the same name in another package, are judged.

Not caught: the name built at run time from parts that are not constants; a resolver that is
handed the directory and never names it (a raw XDG_RUNTIME_DIR given where the resolver's
environment is wanted, which the resolver's type, paths.InboxEnv, makes a different value from
a directory); and code in another language.

How to fix: take the socket from paths.InboxSocket(paths.InboxEnvOf(getenv, uid), pid), or judge
one's shape with paths.IsInboxSocket; never build or match the path in place.`,
	Run:        perPass(runInboxSocketDir),
	ResultType: noResultType,
}

func runInboxSocketDir(pass *analysis.Pass) {
	for _, f := range pass.Files {
		name := filepath.ToSlash(pass.Fset.Position(f.Pos()).Filename)
		if strings.HasSuffix(name, "_test.go") || exemptFile(name, inboxSocketFile) || exemptFile(name, inboxSocketRuleFile) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			e, ok := n.(ast.Expr)
			if !ok {
				return true
			}
			if _, ident := e.(*ast.Ident); ident {
				return true
			}
			tv, ok := pass.TypesInfo.Types[e]
			if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
				return true
			}
			if strings.Contains(constant.StringVal(tv.Value), inboxSocketDirName) {
				pass.Reportf(e.Pos(), "Claude Code's inbox socket directory %q named outside teamchat/paths/inboxsocket.go%s", inboxSocketDirName, inboxSocketFix)
			}
			return false
		})
	}
}

// exemptFile is whether name is file, matched as a path suffix on a whole element.
func exemptFile(name, file string) bool { return name == file || strings.HasSuffix(name, "/"+file) }
