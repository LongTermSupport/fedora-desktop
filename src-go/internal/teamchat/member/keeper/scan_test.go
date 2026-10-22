package keeper

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
)

const (
	module     = "github.com/LongTermSupport/fedora-desktop/src-go"
	moduleRoot = "../../../.."
	cliPath    = module + "/internal/teamchat/member/cli"
)

// sources are the keeper's own non-test files, parsed.
func sources(t *testing.T) map[string]*ast.File {
	t.Helper()
	names, err := filepath.Glob("*.go")
	mustDo(t, err)
	out := map[string]*ast.File{}
	for _, n := range names {
		if strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(token.NewFileSet(), n, nil, parser.SkipObjectResolution)
		mustDo(t, perr)
		out[n] = f
	}
	if len(out) == 0 {
		t.Fatal("no source read")
	}
	return out
}

// selected is every name selected from x (x.Name) in f.
func selected(f ast.Node, x string) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			if id, isIdent := s.X.(*ast.Ident); isIdent && (x == "" || id.Name == x) {
				out = append(out, s.Sel.Name)
			}
		}
		return true
	})
	return out
}

// B14: the keeper makes no network call. Nothing it imports, or any module package those
// import, reaches an HTTP or TLS client; cli is the exception, since it pulls in the attach's
// guest check, and the keeper uses its types alone: it calls no cli function.
func TestNoNetwork(t *testing.T) {
	for _, tags := range [][]string{nil, {"tuwunel", "busfailpoints"}} {
		seen := map[string]bool{}
		for _, bad := range forbiddenImports(t, tags, seen, module+"/internal/teamchat/member/keeper", ".") {
			t.Errorf("tags %v: %s", tags, bad)
		}
		if !seen[module+"/internal/teamchat/member/wire"] {
			t.Fatal("the import walk missed the wire package")
		}
	}
	guest := module + "/internal/teamchat/member/guest"
	if len(forbiddenImports(t, nil, map[string]bool{}, guest, moduleRoot+"/internal/teamchat/member/guest")) == 0 {
		t.Fatal("the walk cannot see the guest check's client, so it proves nothing")
	}
	for name, f := range sources(t) {
		for _, fn := range cliCalls(f) {
			t.Errorf("%s calls cli.%s", name, fn)
		}
	}
}

// forbiddenImports is each HTTP or TLS client import of path, or of a module package it
// imports (cli apart), built with tags.
func forbiddenImports(t *testing.T, tags []string, seen map[string]bool, path, dir string) []string {
	t.Helper()
	if seen[path] {
		return nil
	}
	seen[path] = true
	ctx := build.Default
	ctx.BuildTags = tags
	pkg, err := ctx.ImportDir(dir, 0)
	mustDo(t, err)
	var out []string
	for _, imp := range pkg.Imports {
		if slices.ContainsFunc(forbidden, func(f string) bool { return imp == f || strings.HasPrefix(imp, f+"/") }) {
			out = append(out, path+" imports "+imp)
		}
		if rest, ok := strings.CutPrefix(imp, module+"/"); ok && imp != cliPath {
			out = append(out, forbiddenImports(t, tags, seen, imp, moduleRoot+"/"+rest)...)
		}
	}
	return out
}

// forbidden are the network clients B14 keeps out of the keeper.
var forbidden = []string{"net/http", "crypto/tls", "net/rpc", "net/smtp"}

// cliCalls is each cli function f calls.
func cliCalls(f ast.Node) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if s, isSel := call.Fun.(*ast.SelectorExpr); isSel {
				if id, isIdent := s.X.(*ast.Ident); isIdent && id.Name == "cli" {
					out = append(out, s.Sel.Name)
				}
			}
		}
		return true
	})
	return out
}

// B15: the keeper learns of Claude Code's end by a pidfd, never by its parent pid or
// PR_SET_PDEATHSIG.
func TestEndsByPidfd(t *testing.T) {
	names := make([]string, 0, 256)
	for _, f := range sources(t) {
		names = append(names, selected(f, "")...)
	}
	for _, bad := range []string{"Prctl", "PR_SET_PDEATHSIG", "Pdeathsig", "Getppid"} {
		if slices.Contains(names, bad) {
			t.Errorf("the keeper uses %s", bad)
		}
	}
	if !slices.Contains(names, "PidfdOpen") || !slices.Contains(names, "PidfdSendSignal") {
		t.Fatal("the keeper holds no pidfd")
	}
}

// writers are the calls that put bytes in a file, a log or the terminal.
var writers = []string{"logf", "Write", "WriteString", "WriteFile", "Fprintf", "Fprintln", "Printf", "Println", "Create", "Marshal", "WriteLockRecord"}

// tokenWrites is each writer call in f given the token, or a value holding it.
func tokenWrites(f ast.Node) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var fn string
		switch c := call.Fun.(type) {
		case *ast.SelectorExpr:
			fn = c.Sel.Name
		case *ast.Ident:
			fn = c.Name
		}
		if !slices.Contains(writers, fn) {
			return true
		}
		for _, a := range call.Args {
			ast.Inspect(a, func(m ast.Node) bool {
				if s, isSel := m.(*ast.SelectorExpr); isSel && strings.EqualFold(s.Sel.Name, "token") {
					out = append(out, fn)
				}
				return true
			})
		}
		return true
	})
	return out
}

// B11: the inbox socket token lives only in the keeper's memory: no writer is handed it, and a
// message carrying it prints without it.
func TestTokenStaysInMemory(t *testing.T) {
	uses := 0
	for name, f := range sources(t) {
		if w := tokenWrites(f); len(w) != 0 {
			t.Errorf("%s hands the token to %v", name, w)
		}
		uses += strings.Count(strings.Join(selected(f, "k"), " "), "token")
	}
	if uses == 0 {
		t.Fatal("the scan found no use of the token, so it proves nothing")
	}
	bad, err := parser.ParseFile(token.NewFileSet(), "bad.go", `package keeper
func (k *keeper) leak() { k.logf("%v", k.token) }`, 0)
	mustDo(t, err)
	if len(tokenWrites(bad)) != 1 {
		t.Fatal("the scan misses a logged token")
	}
	const secret = "tok-0123456789abcdef"
	m := wire.Msg{Kind: wire.KindToken, Token: &wire.Token{Token: secret}}
	if strings.Contains(fmt.Sprint(m), secret) || strings.Contains(fmt.Sprintf("%+v", m), secret) {
		t.Fatal("a token message prints its token")
	}
}
