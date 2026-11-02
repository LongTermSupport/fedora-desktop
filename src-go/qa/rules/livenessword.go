package rules

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// livenessOwners may spell a liveness word: the packages that read it from a source that knows
// whether something runs (agentbus, the bus's LIVE lines and the registry's park word it is
// parsed beside; systemd, a unit's ActiveState), and the rules package.
var livenessOwners = []string{agentbusPkg, systemdPkg, rulesPkg}

// livenessWords are the words a person reads as "this is running now".
var livenessWords = []string{wordActive, "online", "listening", "alive"}

const wordActive = "active"

// livenessAllowance is a word a package may spell, with why it is not a status label.
type livenessAllowance struct {
	pkg, word, reason string
}

// livenessAllowances are the words other programs print, spelled to read them, never shown.
var livenessAllowances = []livenessAllowance{
	{pkg: sttPkg, word: wordActive, reason: "the phone app's own SessionState word, read from its manifest to decide it is still recording"},
	{pkg: "internal/teams/teamtest", word: wordActive, reason: "the fake machine's systemctl is-active answer, as systemd prints it"},
}

// LivenessWord keeps every liveness word to the packages that read liveness from a live source.
var LivenessWord = &analysis.Analyzer{
	Name: "project_liveness_word",
	Doc: `project_liveness_word forbids a liveness word spelled outside the packages that read liveness.

A status a person reads as running ("active", "online", "listening", "alive") must come from a
source that knows whether it runs. ccy-teams called every member that was not parked "active",
and painted it green, from the registry's park state: members whose sessions had stopped hours
before showed as active while pingbus status called them stale.

Flagged, outside internal/agentbus, internal/systemd and qa/rules, in files that are not tests:
a string literal that is one of those words, whatever its case and surrounding space. A struct
tag and an import path are not flagged; a word another program prints, spelled only to read
it, is excused by a row of livenessAllowances with its reason.

Not caught: the claim in other words ("up" or "running" for a member), a word inside a longer
text, one assembled at run time, and a wrong source inside an owner.

How to fix: take the word from the owner that read it (agentbus.LiveListening, the member's
LIVE line), or name the state for what its source says (parked, not parked).`,
	Run:        perPass(runLivenessWord),
	ResultType: noResultType,
}

func runLivenessWord(pass *analysis.Pass) {
	if ownedByAny(pass.Pkg.Path(), livenessOwners) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.ImportSpec:
				return false
			case *ast.Field:
				inspectFieldSkippingTag(pass, n)
				return false
			case *ast.BasicLit:
				checkLivenessLiteral(pass, n)
			}
			return true
		})
	}
}

// inspectFieldSkippingTag judges a field's type and names, never its tag.
func inspectFieldSkippingTag(pass *analysis.Pass, field *ast.Field) {
	ast.Inspect(field.Type, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok {
			checkLivenessLiteral(pass, lit)
		}
		return true
	})
}

func checkLivenessLiteral(pass *analysis.Pass, lit *ast.BasicLit) {
	if lit.Kind != token.STRING {
		return
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return
	}
	word := strings.ToLower(strings.TrimSpace(s))
	if !slices.Contains(livenessWords, word) || livenessAllowed(pass.Pkg.Path(), word) {
		return
	}
	pass.Reportf(lit.Pos(), "liveness word %q from no live source: take it from internal/agentbus (the member's LIVE line) "+
		"or name the state for what its source says (project_liveness_word)", s)
}

// livenessAllowed is whether a row of livenessAllowances excuses word in the package.
func livenessAllowed(pkgPath, word string) bool {
	return slices.ContainsFunc(livenessAllowances, func(a livenessAllowance) bool {
		return a.word == word && ownedBy(pkgPath, a.pkg)
	})
}
