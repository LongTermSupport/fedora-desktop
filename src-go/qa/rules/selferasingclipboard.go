package rules

import (
	"go/ast"
	"go/constant"
	"go/token"
	"path"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// clipboardOwners may name the self-erasing flags and claims: the rules package, whose own
// source has to name what it forbids.
var clipboardOwners = []string{rulesPkg}

// selfErasingClaim is the text that relies on, or promises, a clipboard that empties itself
// after a paste: the wl-copy flag spelled out, and the sentences that tell a person a copied
// secret serves one paste and is then gone.
var selfErasingClaim = regexp.MustCompile(`(?i)paste[- ]once|\b(?:for|to|serves?|after)\s+(?:one|a\s+single|the\s+first)\s+paste\b|\bthe\s+one\s+paste\b|\bgone\s+from\s+the\s+clipboard\b|\bgone\s+after\s+(?:one|a\s+single|the\s+first)\s+paste\b`)

// wlCopyShortOnce is a wl-copy short-flag word holding o, its --paste-once (-o, -no, -on).
var wlCopyShortOnce = regexp.MustCompile(`^-[a-zA-Z]*o[a-zA-Z]*$`)

const selfErasingMsg = "self-erasing clipboard: %s; copy plainly, then clear only while the clipboard still holds the secret (project_self_erasing_clipboard)"

// SelfErasingClipboard forbids keeping a secret short-lived with a clipboard that is meant to
// erase itself after one paste.
var SelfErasingClipboard = &analysis.Analyzer{
	Name: "project_self_erasing_clipboard",
	Doc: `project_self_erasing_clipboard forbids relying on a clipboard that erases itself after a paste.

On GNOME the desktop reads a new selection at once and goes on serving its own copy after
wl-copy --paste-once has exited, so the secret stays pasteable until the next copy while the
person was told it was gone.

Flagged, outside qa/rules and in test files too: an execx.Cmd-shaped literal (Name and Args)
running wl-copy with --paste-once or a short-flag word holding o (-o, -no), or xclip with
-loops N for N above 0; and any string constant or comment saying "paste-once", "for one
paste", "the one paste", "gone from the clipboard" or "gone after one paste" (and the
"a single" and "the first" forms).

Not caught: a flag built at run time, the claim worded otherwise ("vanishes once pasted").

How to fix: copy without the flag, tell the person the secret is on the clipboard, and once
they have pasted it (or a bounded wait ends) clear the clipboard only if it still holds the
secret (login.Login's copy in ccy-teams).`,
	Run:        perPass(runSelfErasingClipboard),
	ResultType: noResultType,
}

func runSelfErasingClipboard(pass *analysis.Pass) {
	if ownedByAny(pass.Pkg.Path(), clipboardOwners) {
		return
	}
	for _, f := range pass.Files {
		for _, group := range f.Comments {
			checkClaimComment(pass, group)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BasicLit:
				checkClaimLiteral(pass, n)
			case *ast.CompositeLit:
				checkClipboardCmd(pass, n)
			}
			return true
		})
	}
}

// checkClaimComment judges a comment group as one text, so a claim wrapped over two lines is
// still read.
func checkClaimComment(pass *analysis.Pass, group *ast.CommentGroup) {
	texts := make([]string, 0, len(group.List))
	for _, c := range group.List {
		texts = append(texts, commentText(c.Text))
	}
	if m := selfErasingClaim.FindString(strings.Join(texts, " ")); m != "" {
		pass.Reportf(group.Pos(), selfErasingMsg, strconv.Quote(strings.Join(strings.Fields(m), " "))+" in a comment")
	}
}

// commentText is a comment without its markers and without an analysistest expectation.
func commentText(text string) string {
	if i := strings.Index(text[min(len(text), 2):], "// want"); i >= 0 {
		text = text[:i+2]
	}
	text = strings.TrimPrefix(strings.TrimPrefix(text, "//"), "/*")
	if strings.HasPrefix(strings.TrimSpace(text), "want ") {
		return ""
	}
	return text
}

func checkClaimLiteral(pass *analysis.Pass, lit *ast.BasicLit) {
	if lit.Kind != token.STRING {
		return
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return
	}
	if m := selfErasingClaim.FindString(s); m != "" {
		pass.Reportf(lit.Pos(), selfErasingMsg, strconv.Quote(m))
	}
}

// checkClipboardCmd judges a literal with a constant Name and an Args list of constants.
func checkClipboardCmd(pass *analysis.Pass, lit *ast.CompositeLit) {
	var name string
	var args []string
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Name":
			if s, isConst := clipConst(pass, kv.Value); isConst {
				name = path.Base(s)
			}
		case "Args":
			args = clipArgs(pass, kv.Value)
		}
	}
	if why := selfErasingArgs(name, args); why != "" {
		pass.Reportf(lit.Pos(), selfErasingMsg, why)
	}
}

// selfErasingArgs is why name with args erases the clipboard after a paste, or "".
func selfErasingArgs(name string, args []string) string {
	for i, a := range args {
		switch {
		case name == "wl-copy" && (a == "--paste-once" || wlCopyShortOnce.MatchString(a)):
			return "wl-copy " + a
		case name == "xclip" && a == "-loops" && i+1 < len(args) && args[i+1] != "0":
			return "xclip -loops " + args[i+1]
		}
	}
	return ""
}

func clipArgs(pass *analysis.Pass, e ast.Expr) []string {
	list, ok := ast.Unparen(e).(*ast.CompositeLit)
	if !ok {
		return nil
	}
	var out []string
	for _, elt := range list.Elts {
		s, isConst := clipConst(pass, elt)
		if !isConst {
			s = ""
		}
		out = append(out, s)
	}
	return out
}

func clipConst(pass *analysis.Pass, e ast.Expr) (string, bool) {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}
