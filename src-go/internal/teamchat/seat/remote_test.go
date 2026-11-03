package seat

import (
	"errors"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
)

// A seat on a machine that does not run the team's homeserver (Plan 00246 X7, X11).

// unitName is how paths names a team's homeserver and fence units and the directory of the
// homeserver's runtime state: what a take on a member machine must not depend on.
var unitName = regexp.MustCompile(`^(ServerUnit|ServerUnitDropIn|ServerRuntimeDirectory|FenceUnit)$`)

// X7: a seat is taken from the pool by checkout and team id, with no local homeserver unit: the
// package imports nothing that can ask whether a unit is active, and names no unit of the team.
func TestASeatNeedsNoHomeserverUnit(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range pkg.Imports {
		for _, askers := range []string{"internal/systemd", "internal/execx", "teamchat/host", "teamchat/admin", "teamchat/status", "teamchat/health", "teamchat/hostnet"} {
			if strings.HasSuffix(imp, askers) || strings.Contains(imp, askers+"/") {
				t.Errorf("the seat store imports %s, which can ask about a unit", imp)
			}
		}
	}
	fset := token.NewFileSet()
	for _, name := range pkg.GoFiles {
		file, perr := parser.ParseFile(fset, filepath.Join(pkg.Dir, name), nil, 0)
		if perr != nil {
			t.Fatal(perr)
		}
		for _, use := range unitUses(file) {
			t.Errorf("%s names paths.%s", fset.Position(use.Pos()), use.Sel.Name)
		}
	}
}

// unitUses are the places file names a unit of the team through the paths package.
func unitUses(file *ast.File) []*ast.SelectorExpr {
	var uses []*ast.SelectorExpr
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, isIdent := sel.X.(*ast.Ident); isIdent && x.Name == "paths" && unitName.MatchString(sel.Sel.Name) {
			uses = append(uses, sel)
		}
		return true
	})
	return uses
}

// X7: on a machine that holds nothing but the seat store, a seat is placed and taken.
func TestASeatIsTakenOnAMachineThatHoldsOnlyTheSeatStore(t *testing.T) {
	w := newWorld(t)
	if err := w.place(testPassword); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(w.home)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "state" && e.Name() != "run" {
			t.Errorf("the machine holds %s besides the seat store and its lock", e.Name())
		}
	}
	if _, err := w.tryTake("s1", StateNoRoster); err != nil {
		t.Errorf("a take on a machine with nothing but the seat store: %v", err)
	}
}

// X11: a machine with no roster for the team takes a seat the homeserver accepts.
func TestATakeWithNoRosterTakesLikeAnActiveSeat(t *testing.T) {
	w := newWorld(t)
	if err := w.place(testPassword); err != nil {
		t.Fatal(err)
	}
	got, err := w.tryTake("s1", StateNoRoster)
	if err != nil {
		t.Fatal(err)
	}
	if !exists(t, got.Dir+"/seat.json") || !exists(t, got.Dir+"/token") || !exists(t, w.pool().HeldRecord()) {
		t.Errorf("a take with no roster did not hold the seat")
	}
}

// X11: a parked seat (its account's password reset on the homeserver host) is found by the
// refused login of a take with no roster: it is ErrReissue, nothing is held or copied, and the
// pool record is left for the reissue (no roster says it is retired, so nothing is quarantined).
func TestATakeWithNoRosterFindsAParkedSeatByItsRefusedLogin(t *testing.T) {
	w := newWorld(t)
	if err := w.place(testPassword); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.pool().Password(), []byte(otherPassword), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := w.tryTake("s1", StateNoRoster)
	if !errors.Is(err, ErrReissue) || strings.Contains(err.Error(), otherPassword) {
		t.Fatalf("a take of a parked seat with no roster: %v", err)
	}
	if exists(t, w.pool().Held()) || exists(t, w.sessionSeat("s1").Dir()) {
		t.Errorf("a refused login left held/ or a session directory")
	}
	if !exists(t, w.pool().Bundle()) || w.readFile(w.pool().Password()) != otherPassword || len(quarantined(t, w)) != 0 {
		t.Errorf("a refused login changed the pool seat")
	}
}

// X11: a login answered with an authentication refusal (401, a locked account) is a refused login
// too: ErrReissue, whatever else the answer says.
func TestAFirstLoginAnsweredWithAnAuthRefusalIsAReissue(t *testing.T) {
	clk := newClock()
	h := newHomeserver(t, clk)
	h.hs.Inject(fakehs.Fault{Method: http.MethodPost, Path: "/login", Status: http.StatusUnauthorized, Body: []byte(`{"errcode":"M_USER_LOCKED"}`)})
	_, err := Password{Clock: clk}.Mint(t.Context(), h.bundle(), mustSecret(t, testPassword))
	if !errors.Is(err, ErrReissue) || strings.Contains(err.Error(), testPassword) {
		t.Errorf("a login answered 401: %v", err)
	}
}

// X11: a retired seat (its account deactivated) is found by its refused login too. The take fails
// naming the user and shows no password; no roster says it is retired, so the pool record stays.
func TestATakeWithNoRosterFindsARetiredSeatByItsRefusedLogin(t *testing.T) {
	w := newWorld(t)
	if err := w.place(testPassword); err != nil {
		t.Fatal(err)
	}
	w.hs.Inject(fakehs.Fault{Method: http.MethodPost, Path: "/login", Status: http.StatusForbidden, Body: []byte(`{"errcode":"M_USER_DEACTIVATED"}`)})
	_, err := w.tryTake("s1", StateNoRoster)
	if err == nil || !strings.Contains(err.Error(), w.bundle().UserID) || strings.Contains(err.Error(), testPassword) {
		t.Fatalf("a take of a retired seat with no roster: %v", err)
	}
	if exists(t, w.sessionSeat("s1").Dir()) || !exists(t, w.pool().Bundle()) || len(quarantined(t, w)) != 0 {
		t.Errorf("a refused login copied the seat or changed the pool")
	}
}
