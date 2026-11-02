package forge_test

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/forge"
)

func TestSD5TokenNeverFormatted(t *testing.T) {
	run := &execx.Fake{}
	run.On(ghArgv, execx.Result{Stdout: []byte(firstTok + "\n")})
	l := forge.NewLogin(run, clock.NewFake(time.Hour, wall0))
	tok, err := l.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d", "%T"} {
		if got := fmt.Sprintf(verb, tok); strings.Contains(got, firstTok) || strings.Contains(got, fmt.Sprintf("%x", firstTok)) {
			t.Fatalf("%s of a Token shows it: %s", verb, got)
		}
	}
	if tok.String() != tok.GoString() || strings.Contains(tok.String(), firstTok) {
		t.Fatalf("String %q, GoString %q: want the same redacted text", tok.String(), tok.GoString())
	}
	if got := fmt.Sprint(tok, &tok, []forge.Token{tok}); strings.Contains(got, firstTok) {
		t.Fatalf("a Token printed inside a value shows it: %s", got)
	}
}

// TestSD5TokenReachesOnlyTheHeader reads the package's own source: the token's value (the
// field credential) is read only by Token.header, and header's one caller hands it straight to
// the request's Header.Set as the Authorization value, so no writer call is handed it.
func TestSD5TokenReachesOnlyTheHeader(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	s := &tokenScan{t: t, fset: fset}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				s.inHeader = fd.Name.Name == "header" && fd.Recv != nil
				ast.Inspect(fd, s.visit)
			}
		}
	}
	if s.reads != 1 || s.headerCalls != 1 {
		t.Fatalf("credential read %d times and header called %d times, want 1 and 1", s.reads, s.headerCalls)
	}
}

type tokenScan struct {
	t                  *testing.T
	fset               *token.FileSet
	inHeader           bool
	reads, headerCalls int
}

func (s *tokenScan) visit(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.SelectorExpr:
		if x.Sel.Name != "credential" {
			return true
		}
		s.reads++
		if !s.inHeader {
			s.t.Errorf("%s: the token's value is read outside Token.header", s.fset.Position(x.Pos()))
		}
	case *ast.CallExpr:
		for _, a := range x.Args {
			if !isHeaderCall(a) {
				continue
			}
			s.headerCalls++
			if !isAuthorizationSet(x) {
				s.t.Errorf("%s: Token.header is handed to something other than Header.Set(\"Authorization\", ...)", s.fset.Position(x.Pos()))
			}
		}
	}
	return true
}

func isHeaderCall(e ast.Expr) bool {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	s, ok := c.Fun.(*ast.SelectorExpr)
	return ok && s.Sel.Name == "header"
}

func isAuthorizationSet(c *ast.CallExpr) bool {
	s, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || s.Sel.Name != "Set" || len(c.Args) != 2 {
		return false
	}
	recv, ok := s.X.(*ast.SelectorExpr)
	lit, isLit := c.Args[0].(*ast.BasicLit)
	return ok && recv.Sel.Name == "Header" && isLit && lit.Value == `"Authorization"`
}

// hungGh is a gh that hangs until its context ends, or answers firstTok once answer is set.
type hungGh struct {
	mu      sync.Mutex
	runs    int
	answer  bool
	started chan struct{}
}

func (g *hungGh) Run(ctx context.Context, _ execx.Cmd) (execx.Result, error) {
	g.mu.Lock()
	g.runs++
	answer := g.answer
	g.mu.Unlock()
	if answer {
		return execx.Result{Stdout: []byte(firstTok + "\n")}, nil
	}
	g.started <- struct{}{}
	<-ctx.Done()
	return execx.Result{}, ctx.Err()
}

func (g *hungGh) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.runs
}

// SD5-4 (n3d-D31): gh runs under its own 10 s bound; a caller whose context ends is released at
// once even while another caller's gh run hangs, and a failed read is held 30 s.
func TestSD5_4GhRunsUnderItsOwnBound(t *testing.T) {
	gh := &hungGh{started: make(chan struct{}, 1)}
	clk := clock.NewFake(time.Hour, wall0)
	l := forge.NewLogin(gh, clk)
	first := make(chan error, 1)
	go func() {
		_, err := l.Token(context.Background())
		first <- err
	}()
	<-gh.started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.Token(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled caller: %v, want its context's error", err)
	}
	if err := clk.AwaitDue(t.Context(), 10*time.Second); err != nil {
		t.Fatal(err)
	}
	clk.Advance(10 * time.Second)
	if err := <-first; !errors.Is(err, forge.ErrLogin) {
		t.Fatalf("gh hung past its bound: %v, want ErrLogin", err)
	}
	gh.mu.Lock()
	gh.answer = true
	gh.mu.Unlock()
	clk.Advance(30*time.Second - time.Millisecond)
	if _, err := l.Token(context.Background()); !errors.Is(err, forge.ErrLogin) || gh.count() != 1 {
		t.Fatalf("inside 30 s: %v after %d runs; want the held ErrLogin and no second run", err, gh.count())
	}
	clk.Advance(time.Millisecond)
	if _, err := l.Token(context.Background()); err != nil || gh.count() != 2 {
		t.Fatalf("at 30 s: %v after %d runs; want a token from a second run", err, gh.count())
	}
}
