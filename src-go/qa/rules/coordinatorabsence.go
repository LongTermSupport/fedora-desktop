package rules

import (
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// teamchatPkg is the tree the coordinator-absence rule judges.
const teamchatPkg = "internal/teamchat"

// absenceWording matches the wordings of an absence clause: a team that has no coordinator.
var absenceWording = regexp.MustCompile(`(?i)\b(?:no|without a) coordinator\b|coordinator: none`)

// absenceTexts are the constants and variables that may hold an absence wording outside a
// ClaimCoordinator case, each with why it is not a session being told an absence from a record
// not known to be current.
var absenceTexts = []struct{ pkg, name, reason string }{
	{pkg: "internal/teamchat/member/hooktext", name: "tmplTeamNoCoordinator", reason: "the frame's absence clause; teamLines fills it only in the CoordinatorAbsent case of ClaimCoordinator"},
	{pkg: "internal/teamchat/member/engine", name: "msgNoCoordinator", reason: "the refusal of a send or a reply to a coordinator, given only after the roster was judged verified and unverified teams were answered first"},
	{pkg: "internal/teamchat/proto", name: "closedTopic", reason: "the topic of the questions room built from the roster, room state and not a claim made to a session"},
}

// CoordinatorAbsence keeps every absence clause a session reads to the cases of ClaimCoordinator.
var CoordinatorAbsence = &analysis.Analyzer{
	Name: "project_coordinator_absence",
	Doc: `project_coordinator_absence forbids an absence clause written outside ClaimCoordinator.

"The team has no coordinator" is a definite claim: it is true only of a record verified current.
A seat was told it from a record not known to be current, after a handover, and acted on the
absence (issue 192). proto.ClaimCoordinator is the one decision of what a session is told of its
team's coordinator, and every clause a session reads is written in the CoordinatorAbsent case of
a switch on it.

Flagged, under internal/teamchat in files that are not tests: a string literal containing "no
coordinator", "without a coordinator" or "coordinator: none", whatever its case, unless it is
inside a case clause naming CoordinatorAbsent of a switch whose tag is a call to ClaimCoordinator,
or it initialises a constant or variable that absenceTexts names, each with its reason.

Not caught: the claim in other words, a wording assembled at run time, and a use of an
absenceTexts name outside the case it was allowed for.

How to fix: decide the clause with ClaimCoordinator and write the text in its CoordinatorAbsent
case; for text that is not a claim to a session, name it in absenceTexts with the reason.`,
	Run:        perPass(runCoordinatorAbsence),
	ResultType: noResultType,
}

func runCoordinatorAbsence(pass *analysis.Pass) {
	if !inTeamchat(pass.Pkg.Path()) {
		return
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.ImportSpec, *ast.Field:
				return false
			case *ast.CaseClause:
				return !absentCase(n) || !claimSwitchOf(f, n)
			case *ast.ValueSpec:
				if namedAbsenceText(pass.Pkg.Path(), n) {
					return false
				}
			case *ast.BasicLit:
				checkAbsenceLiteral(pass, n)
			}
			return true
		})
	}
}

// inTeamchat is whether the package path (the test variants of it included) is internal/teamchat
// or under it, matched on whole path elements so the fixtures and the real module share it.
func inTeamchat(pkgPath string) bool {
	path := pkgPath
	if i := strings.IndexByte(path, ' '); i >= 0 {
		path = path[:i]
	}
	return strings.Contains("/"+path+"/", "/"+teamchatPkg+"/")
}

// absentCase is whether the case clause names CoordinatorAbsent.
func absentCase(c *ast.CaseClause) bool {
	for _, e := range c.List {
		switch e := e.(type) {
		case *ast.Ident:
			if e.Name == "CoordinatorAbsent" {
				return true
			}
		case *ast.SelectorExpr:
			if e.Sel.Name == "CoordinatorAbsent" {
				return true
			}
		}
	}
	return false
}

// claimSwitchOf is whether c belongs to a switch whose tag is a call to ClaimCoordinator.
func claimSwitchOf(f *ast.File, c *ast.CaseClause) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok || found || sw.Body == nil || c.Pos() < sw.Body.Lbrace || c.End() > sw.Body.Rbrace {
			return !found
		}
		found = callsClaimCoordinator(sw.Tag)
		return !found
	})
	return found
}

func callsClaimCoordinator(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name == "ClaimCoordinator"
	case *ast.SelectorExpr:
		return fn.Sel.Name == "ClaimCoordinator"
	}
	return false
}

// namedAbsenceText is whether the declaration names one of absenceTexts in the package.
func namedAbsenceText(pkgPath string, v *ast.ValueSpec) bool {
	for _, name := range v.Names {
		for _, a := range absenceTexts {
			if a.name == name.Name && ownedBy(pkgPath, a.pkg) {
				return true
			}
		}
	}
	return false
}

func checkAbsenceLiteral(pass *analysis.Pass, lit *ast.BasicLit) {
	if lit.Kind != token.STRING {
		return
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil || !absenceWording.MatchString(s) {
		return
	}
	pass.Reportf(lit.Pos(), "an absence clause (%q) outside the CoordinatorAbsent case of proto.ClaimCoordinator: a record not known "+
		"to be current must not be told as an absence; decide it with ClaimCoordinator (project_coordinator_absence)", s)
}
