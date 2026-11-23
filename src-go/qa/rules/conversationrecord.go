package rules

import (
	"go/ast"
	"go/constant"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const (
	// conversationRecordFile is the name both systems give a session's conversation record.
	conversationRecordFile = "conversation.json"
	// conversationLocator is the one function that names the record's path.
	conversationLocator = "Locate"
	// conversationFileConst is the v1 record's name, declared in package conversation.
	conversationFileConst = "File"
	teamchatMemberTree    = "internal/teamchat/member"
)

// conversationRecordOwners write and read a member's own record (the member side of teamchat):
// they are the record, not a host reader of it.
var conversationRecordOwners = []string{teamchatPathsPkg, rulesPkg}

const conversationRecordFix = "a conversation record's path named outside conversation.Locate (project_conversation_record): ask conversation.Locate, which knows both systems' records, or read through conversation.For, Transcript or StartPlan"

// ConversationRecord keeps the naming of a session's conversation record in conversation.Locate.
var ConversationRecord = &analysis.Analyzer{
	Name: "project_conversation_record",
	Doc: `project_conversation_record flags, in the non-test files of every package but the record's
owners (internal/teamchat/paths and internal/teamchat/member and below, the member's own writer and
reader), a naming of a session's conversation record's path anywhere but the function Locate of
internal/conversation:

- a constant string expression, judged whole, whose value is "conversation.json" or a path ending
  in "/conversation.json" (so a name spelt in pieces is the name); a constant is reported where it is declared, and the one declaration
  allowed is internal/conversation's File;
- a use of internal/conversation's File, through any import name;
- a use of internal/teamchat/paths' Layout.ConversationFile: a call, a method value or a method
  expression.

The host reads a session's conversation to stop, restart, restore and wind it down. v1's record
is <checkout>/.claude/ccy/pingbus/sessions/<session>/conversation.json, written by the pingbus
hook; a v2 session's is <seat store>/sessions/<session>/conversation.json, written by its link.
The reader joined membership.SessionDir and File itself, so no host reader found a v2 session's
conversation (Plan 00209 DESIGN-host-cc-v2.md section 4). The rule holds the single-reader shape:
one locator decides which record a session has by the system its teams are live on. That the
locator knows both systems is held by internal/conversation's own tests, not by this rule.

Tests are not judged: a test plants a record where the locator reads it.

Not caught: the path built at run time from pieces that are not constant; a v1 directory joined
with a name read from elsewhere; a member package outside teamchat/member writing the record.

How to fix: ask conversation.Locate for the record, or read through conversation.For,
conversation.Transcript or conversation.StartPlan.`,
	Run:        perPass(runConversationRecord),
	ResultType: noResultType,
}

func runConversationRecord(pass *analysis.Pass) {
	path := pass.Pkg.Path()
	if ownedByAny(path, conversationRecordOwners) || inTree(path, teamchatMemberTree) {
		return
	}
	inConversation := ownedBy(path, conversationPkg)
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			if inConversation && funcName(d) == conversationLocator {
				continue
			}
			ast.Inspect(d, func(n ast.Node) bool {
				if inConversation && declaresFile(n) {
					return false
				}
				if namesConversationRecord(pass, n) {
					pass.Reportf(n.Pos(), "%s", conversationRecordFix)
					return false
				}
				return true
			})
		}
	}
}

// declaresFile is whether n is the declaration of package conversation's File.
func declaresFile(n ast.Node) bool {
	spec, ok := n.(*ast.ValueSpec)
	return ok && len(spec.Names) == 1 && spec.Names[0].Name == conversationFileConst
}

// namesConversationRecord is whether n is a constant expression whose value is the record's
// name, a use of conversation.File, or a use of Layout.ConversationFile.
func namesConversationRecord(pass *analysis.Pass, n ast.Node) bool {
	if id, ok := n.(*ast.Ident); ok {
		switch obj := pass.TypesInfo.Uses[id].(type) {
		case *types.Const:
			return obj.Name() == conversationFileConst && obj.Pkg() != nil && ownedBy(obj.Pkg().Path(), conversationPkg)
		case *types.Func:
			named := receiverNamed(obj)
			return obj.Name() == "ConversationFile" && named != nil && named.Obj().Name() == "Layout" &&
				obj.Pkg() != nil && ownedBy(obj.Pkg().Path(), teamchatPathsPkg)
		}
		return false
	}
	e, ok := n.(ast.Expr)
	if !ok {
		return false
	}
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return false
	}
	value := constant.StringVal(tv.Value)
	return value == conversationRecordFile || strings.HasSuffix(value, "/"+conversationRecordFile)
}
