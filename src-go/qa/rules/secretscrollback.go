package rules

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// secretSource is a function or method whose string result is a secret. recv is the receiver
// type name, "" for a package function.
type secretSource struct {
	pkg, recv, name string
	reason          string
}

const (
	ccylaunchPkg   = "internal/ccylaunch"
	teamchatCtlPkg = "internal/teamchat/ctl"
	cccPkg         = "internal/ccc"
	vmtestPkg      = "internal/vmtest"
	cliRecv        = "cli"
	bootstrapRecv  = "bootstrap"
	errorMethod    = "Error"
	revealMethod   = "Reveal"
)

// teamchatAdminPkg is teamchat-admin's verbs.
const teamchatAdminPkg = "internal/teamchat/admin"

// secretSources are the places a Go program comes to hold a secret's value. Every method named
// Reveal that returns a string is one too: it is how a secret type (matrix.Token, seat.Secret)
// hands its value out.
var secretSources = []secretSource{
	{pkg: agentbusPkg, recv: "Client", name: "HumanPassword", reason: "a person's new Element password, issued by agent-bus as root"},
	{pkg: ftpcameraPkg, recv: cliRecv, name: "hotspotPassword", reason: "the camera hotspot's Wi-Fi password, read from NetworkManager"},
	{pkg: ftpcameraPkg, recv: "", name: "newHotspotPassword", reason: "a new hotspot Wi-Fi password"},
	{pkg: uiPkg, recv: "Lines", name: "Secret", reason: "what a person typed at a hidden prompt"},
	{pkg: cccPkg, recv: "", name: "readToken", reason: "a saved Claude sign-in token"},
	{pkg: cccPkg, recv: "", name: "extractToken", reason: "the Claude sign-in token claude setup-token printed"},
	{pkg: teamchatCtlPkg, recv: bootstrapRecv, name: "token", reason: "a team's admin token"},
	{pkg: teamchatCtlPkg, recv: bootstrapRecv, name: "password", reason: "a team's stored admin password"},
	{pkg: teamchatCtlPkg, recv: bootstrapRecv, name: "freshPassword", reason: "a team's new admin password"},
	{pkg: vmtestPkg, recv: cliRecv, name: "randomToken", reason: "a guest's random token"},
}

// secretField is a struct field that holds a secret's value from where it is read.
type secretField struct {
	pkg, typ, field string
	reason          string
}

// secretFields are the fields a secret is held in with no source call the rule can follow
// between: read into from a file, or encoded into a document through types it does not follow.
var secretFields = []secretField{
	{pkg: ccylaunchPkg, typ: "export", field: "token", reason: "the Claude sign-in token ccy --export-token prints"},
	{pkg: teamchatAdminPkg, typ: "work", field: "doc", reason: "a member verb's answer document, which carries the issued credential's bundle"},
}

// secretDisplays are the functions that write a secret, and so are not judged: the secret
// screen, which leaves no copy of it in the scrollback.
var secretDisplays = []allowance{
	{pkg: termPkg, fn: "SecretScreen.Show", reason: "the one display of a secret: the terminal's alternate screen, left once the person is done with it; a stream that is not a terminal has no scrollback"},
	{pkg: termPkg, fn: "SecretScreen.Write", reason: "Show as an io.Writer, for a child's output that holds a secret"},
}

const secretMsg = "secret from %s written to the terminal through %s (project_secret_scrollback): show it with term.SecretScreen, which leaves no copy in the scrollback"

// SecretScrollback forbids writing a secret where it outlives its display.
var SecretScrollback = &analysis.Analyzer{
	Name: "project_secret_scrollback",
	Doc: `project_secret_scrollback flags a secret written to a terminal other than through
term.SecretScreen.

A terminal keeps what was written to its normal screen in its scrollback, and tmux in a pane's
history, for as long as the window lives: a password shown "once" stays there after the program
has moved on, for anyone who scrolls up, shares the screen or logs the terminal (issue 255:
ccy-teams' new Element password, still there after going back to the list).

A secret is the value of a source: a function in the rule's table (agent-bus's HumanPassword,
the hotspot's Wi-Fi password, a hidden prompt's answer, the token readers), a method named
Reveal returning a string, or a field the table names. It is followed through assignments, +,
conversions, fmt.Sprint*, fmt.Errorf, errors.New and the strings functions that keep their
argument, String, Bytes and Error of a value holding it, a buffer written with it, a field it is
stored in (across packages), a parameter of a function of the same package, and the result of a
function that returns it (across packages).

A terminal write is fmt's Print and Fprint functions, log's, io.WriteString, Write or
WriteString on an io.Writer or an *os.File, and any function or method, in any package, that
passes a parameter on to one (term.Printer.Print, flow.Kit.Tell, a package's own printer): the
call that hands a secret to such a parameter is the finding. term.SecretScreen.Show is not a
write: it is the display. Writing into a bytes.Buffer or strings.Builder is followed, not
flagged. Test files are not judged.

Not caught: a secret read from a file or a child's output with no source between (add the
reader to the table); a secret printed by a child process given the terminal (the caller runs
it on the secret screen: term.SecretScreen.Open); a struct holding a secret printed whole with
%v; a secret handed to a function value, an interface method, or a function of another package
that only stores it.

How to fix: show the secret with term.SecretScreen (flow.Kit.ShowSecret in ccy-teams), and
leave the screen (Close) once the person is done with it: the terminal's alternate screen keeps
no scrollback, and tmux keeps it out of the pane's history. On a stream that is not a terminal
SecretScreen writes the secret plainly, as the payload its reader asked for.`,
	Run:        perPass(runSecretScrollback),
	ResultType: noResultType,
	FactTypes:  []analysis.Fact{new(secretSinkFact), new(secretPassFact), new(secretSourceFact), new(secretFieldFact)},
}

// secretSinkFact marks a function whose parameters at Params reach a terminal write.
type secretSinkFact struct{ Params []int }

func (*secretSinkFact) AFact() {}

func (f *secretSinkFact) String() string { return "writes parameters " + indexWords(f.Params) }

// secretPassFact marks a function whose result holds its parameters at Params.
type secretPassFact struct{ Params []int }

func (*secretPassFact) AFact() {}

func (f *secretPassFact) String() string { return "returns parameters " + indexWords(f.Params) }

// secretSourceFact marks a function that returns the secret of From.
type secretSourceFact struct{ From string }

func (*secretSourceFact) AFact() {}

func (f *secretSourceFact) String() string { return "returns a secret from " + f.From }

// secretFieldFact marks a field that holds the secret of From.
type secretFieldFact struct{ From string }

func (*secretFieldFact) AFact() {}

func (f *secretFieldFact) String() string { return "holds a secret from " + f.From }

// paramLabel marks a value as holding a parameter, while a function's summary is worked out.
const paramLabel = "#"

// secretFunc is one function declaration of the package under check.
type secretFunc struct {
	decl *ast.FuncDecl
	fn   *types.Func
}

// secretRun is the rule over one package. sinks and passes are the package's own functions'
// summaries; sources its functions that return a secret; tainted every object holding one.
type secretRun struct {
	pass    *analysis.Pass
	funcs   []secretFunc
	sinks   map[*types.Func]map[int]bool
	passes  map[*types.Func]map[int]bool
	sources map[*types.Func]string
	tainted map[types.Object]string
	// withSources is false while the summaries are worked out: they are about parameters only.
	withSources bool
}

// secretLookup is the labels of the secrets an object holds.
type secretLookup func(types.Object) []string

// secretAdd records that an object holds the secret of a label, and says whether it was new.
type secretAdd func(types.Object, string) bool

func runSecretScrollback(pass *analysis.Pass) {
	r := &secretRun{
		pass: pass, sinks: map[*types.Func]map[int]bool{}, passes: map[*types.Func]map[int]bool{},
		sources: map[*types.Func]string{}, tainted: map[types.Object]string{},
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if fn, isFn := pass.TypesInfo.Defs[fd.Name].(*types.Func); isFn && !isSecretDisplay(fn) {
				r.funcs = append(r.funcs, secretFunc{decl: fd, fn: fn})
			}
		}
	}
	for r.summarise() {
	}
	r.withSources = true
	for r.propagate() {
	}
	r.exportFacts()
	r.report()
}

// summarise works out, once, each function's parameters that reach a write or its result, and
// says whether any summary grew.
func (r *secretRun) summarise() bool {
	changed := false
	for _, sf := range r.funcs {
		changed = r.summariseFunc(sf) || changed
	}
	return changed
}

// summariseFunc is summarise for one function.
func (r *secretRun) summariseFunc(sf secretFunc) bool {
	sig, ok := sf.fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	local := map[types.Object][]string{}
	for i := range sig.Params().Len() {
		local[sig.Params().At(i)] = []string{paramLabel + strconv.Itoa(i)}
	}
	look := func(o types.Object) []string { return local[o] }
	addLocal := func(o types.Object, label string) bool {
		if o == nil || o.Parent() == nil || o.Parent() == r.pass.Pkg.Scope() || slices.Contains(local[o], label) {
			return false
		}
		local[o] = append(local[o], label)
		return true
	}
	for r.spread(sf.decl.Body, look, addLocal) {
	}
	changed := false
	r.eachWrite(sf.decl.Body, look, func(_ *ast.CallExpr, _, label string) {
		if i, isParam := paramIndex(label); isParam {
			changed = addIndex(r.sinks, sf.fn, i) || changed
		}
	})
	r.eachReturn(sf.decl.Body, look, func(label string) {
		if i, isParam := paramIndex(label); isParam {
			changed = addIndex(r.passes, sf.fn, i) || changed
		}
	})
	return changed
}

// taintedLook is the lookup of the package's secrets.
func (r *secretRun) taintedLook(o types.Object) []string {
	if l, found := r.tainted[o]; found {
		return []string{l}
	}
	return nil
}

// addTainted records a secret held by o.
func (r *secretRun) addTainted(o types.Object, label string) bool {
	if o == nil || strings.HasPrefix(label, paramLabel) {
		return false
	}
	if _, had := r.tainted[o]; had {
		return false
	}
	r.tainted[o] = label
	return true
}

// propagate follows the secrets through the package once, and says whether anything new holds one.
func (r *secretRun) propagate() bool {
	changed := false
	for _, sf := range r.funcs {
		for r.spread(sf.decl.Body, r.taintedLook, r.addTainted) {
			changed = true
		}
		changed = r.intoParams(sf.decl.Body) || changed
		r.eachReturn(sf.decl.Body, r.taintedLook, func(label string) {
			if _, had := r.sources[sf.fn]; !had && !strings.HasPrefix(label, paramLabel) {
				r.sources[sf.fn] = label
				changed = true
			}
		})
	}
	return changed
}

// intoParams marks the parameter of a function of this package that a call hands a secret to,
// unless that parameter reaches a write: the call itself is then the finding.
func (r *secretRun) intoParams(body ast.Node) bool {
	changed := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		f := calledFunc(r.pass, call)
		if f == nil || f.Pkg() != r.pass.Pkg {
			return true
		}
		sig, isSig := f.Type().(*types.Signature)
		if !isSig || sig.Params().Len() == 0 {
			return true
		}
		for i, arg := range call.Args {
			p := min(i, sig.Params().Len()-1)
			if r.sinks[f][p] {
				continue
			}
			changed = r.addAll(sig.Params().At(p), r.labels(arg, r.taintedLook), r.addTainted) || changed
		}
		return true
	})
	return changed
}

// spread follows a secret through the assignments, declarations, ranges, composite literals
// and buffer writes of body, once; it says whether add took anything new.
func (r *secretRun) spread(body ast.Node, look secretLookup, add secretAdd) bool {
	changed := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			changed = r.assign(n.Lhs, n.Rhs, look, add) || changed
		case *ast.ValueSpec:
			lhs := make([]ast.Expr, len(n.Names))
			for i, name := range n.Names {
				lhs[i] = name
			}
			changed = r.assign(lhs, n.Values, look, add) || changed
		case *ast.RangeStmt:
			if n.Value != nil {
				changed = r.addAll(r.target(n.Value), r.labels(n.X, look), add) || changed
			}
		case *ast.CompositeLit:
			changed = r.fieldsOf(n, look, add) || changed
		case *ast.CallExpr:
			changed = r.intoBuffer(n, look, add) || changed
		}
		return true
	})
	return changed
}

// assign follows a secret from the right of an assignment to its left.
func (r *secretRun) assign(lhs, rhs []ast.Expr, look secretLookup, add secretAdd) bool {
	changed := false
	if len(lhs) == len(rhs) {
		for i := range lhs {
			changed = r.addAll(r.target(lhs[i]), r.labels(rhs[i], look), add) || changed
		}
		return changed
	}
	if len(rhs) != 1 {
		return false
	}
	call, ok := ast.Unparen(rhs[0]).(*ast.CallExpr)
	if !ok {
		return false
	}
	label, ok := r.sourceCall(call)
	if !ok {
		return false
	}
	for _, e := range lhs {
		if t := r.pass.TypesInfo.TypeOf(e); t != nil && secretText(t) {
			changed = add(r.target(e), label) || changed
		}
	}
	return changed
}

// intoBuffer follows a secret written into an in-memory buffer to the buffer.
func (r *secretRun) intoBuffer(call *ast.CallExpr, look secretLookup, add secretAdd) bool {
	w, ok := r.write(call)
	if !ok || !w.buffer {
		return false
	}
	changed := false
	for _, arg := range w.content {
		changed = r.addAll(r.target(w.writer), r.labels(arg, look), add) || changed
	}
	return changed
}

// fieldsOf marks the fields a composite literal sets from a secret.
func (r *secretRun) fieldsOf(lit *ast.CompositeLit, look secretLookup, add secretAdd) bool {
	changed := false
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		field, ok := r.pass.TypesInfo.ObjectOf(key).(*types.Var)
		if !ok || !field.IsField() {
			continue
		}
		changed = r.addAll(field, r.labels(kv.Value, look), add) || changed
	}
	return changed
}

// target is the object an assignment's left side stores into: a variable, a field, the
// container of an element.
func (r *secretRun) target(e ast.Expr) types.Object {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		return r.pass.TypesInfo.ObjectOf(e)
	case *ast.SelectorExpr:
		return r.pass.TypesInfo.ObjectOf(e.Sel)
	case *ast.IndexExpr:
		return r.target(e.X)
	case *ast.StarExpr:
		return r.target(e.X)
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return r.target(e.X)
		}
	}
	return nil
}

// addAll records each of labels for o, and says whether any was new.
func (r *secretRun) addAll(o types.Object, labels []string, add secretAdd) bool {
	changed := false
	for _, label := range labels {
		changed = add(o, label) || changed
	}
	return changed
}

// labels are the labels of every secret e holds: a summary needs each parameter, not the first.
func (r *secretRun) labels(e ast.Expr, look secretLookup) []string {
	one := func(label string, ok bool) []string {
		if ok {
			return []string{label}
		}
		return nil
	}
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		return look(r.pass.TypesInfo.ObjectOf(e))
	case *ast.SelectorExpr:
		obj := r.pass.TypesInfo.ObjectOf(e.Sel)
		if labels := look(obj); len(labels) > 0 {
			return labels
		}
		if r.withSources {
			return one(r.fieldSecret(obj))
		}
		return nil
	case *ast.CallExpr:
		return r.callLabels(e, look)
	}
	var out []string
	for _, sub := range keptParts(e) {
		out = append(out, r.labels(sub, look)...)
	}
	return out
}

// keptParts are the parts of e whose text e keeps: both sides of +, what & or * or an index or
// a slice is taken of, a composite literal's values.
func keptParts(e ast.Expr) []ast.Expr {
	switch e := e.(type) {
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			return []ast.Expr{e.X, e.Y}
		}
	case *ast.StarExpr:
		return []ast.Expr{e.X}
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			return []ast.Expr{e.X}
		}
	case *ast.SliceExpr:
		return []ast.Expr{e.X}
	case *ast.IndexExpr:
		return []ast.Expr{e.X}
	case *ast.CompositeLit:
		out := make([]ast.Expr, 0, len(e.Elts))
		for _, el := range e.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				el = kv.Value
			}
			out = append(out, el)
		}
		return out
	}
	return nil
}

// callLabels are the secrets a call's result holds: a source's, or those of the arguments or
// the receiver its result keeps.
func (r *secretRun) callLabels(call *ast.CallExpr, look secretLookup) []string {
	if tv, ok := r.pass.TypesInfo.Types[call.Fun]; ok && tv.IsType() {
		if len(call.Args) == 1 {
			return r.labels(call.Args[0], look)
		}
		return nil
	}
	if label, ok := r.sourceCall(call); ok {
		return []string{label}
	}
	f := calledFunc(r.pass, call)
	if f == nil {
		return nil
	}
	if t := r.pass.TypesInfo.TypeOf(call); t == nil || !secretCarrier(t) {
		return nil
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && receiverPassesOn(f) {
		return r.labels(sel.X, look)
	}
	if f.Pkg() == nil {
		return nil
	}
	var out []string
	for _, arg := range argsAt(f, call, r.passIndexes(f)) {
		out = append(out, r.labels(arg, look)...)
	}
	return out
}

// sourceCall is the label of the secret call returns, when it calls a source.
func (r *secretRun) sourceCall(call *ast.CallExpr) (string, bool) {
	if !r.withSources {
		return "", false
	}
	f := calledFunc(r.pass, call)
	if f == nil || f.Pkg() == nil {
		return "", false
	}
	if isSecretSource(f) {
		return secretFuncLabel(f), true
	}
	if label, ok := r.sources[f]; ok {
		return label, true
	}
	var fact secretSourceFact
	if r.pass.ImportObjectFact(f, &fact) {
		return fact.From, true
	}
	return "", false
}

// fieldSecret is the label of the secret a field holds, from the table or another package.
func (r *secretRun) fieldSecret(obj types.Object) (string, bool) {
	field, ok := obj.(*types.Var)
	if !ok || !field.IsField() || field.Pkg() == nil {
		return "", false
	}
	var fact secretFieldFact
	if field.Pkg() != r.pass.Pkg && r.pass.ImportObjectFact(field, &fact) {
		return fact.From, true
	}
	for _, row := range secretFields {
		if ownedBy(field.Pkg().Path(), row.pkg) && field.Name() == row.field && fieldOwner(field) == row.typ {
			return field.Pkg().Name() + "." + row.typ + "." + row.field, true
		}
	}
	return "", false
}

// fieldOwner is the name of the struct type field is declared in, "" when it cannot be told.
func fieldOwner(field *types.Var) string {
	scope := field.Pkg().Scope()
	for _, name := range scope.Names() {
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok {
			continue
		}
		st, ok := tn.Type().Underlying().(*types.Struct)
		if ok && slices.Contains(slices.Collect(st.Fields()), field) {
			return name
		}
	}
	return ""
}

// secretWrite is a call that writes to a stream: the arguments it writes, the writer it writes
// to when there is one, and whether that writer is an in-memory buffer rather than a stream.
type secretWrite struct {
	label   string
	content []ast.Expr
	writer  ast.Expr
	buffer  bool
}

// noWriter marks a standard write whose every argument is written, to the process's output.
const noWriter = -1

// stdWrites are the standard library's writes, by package path and name ("Type.method" for a
// method), with the index of the writer argument; every argument after it is written.
var stdWrites = map[string]int{
	"fmt.Print": noWriter, "fmt.Printf": noWriter, "fmt.Println": noWriter,
	"fmt.Fprint": 0, "fmt.Fprintf": 0, "fmt.Fprintln": 0,
	"io.WriteString": 0,
	"log.Print":      noWriter, "log.Printf": noWriter, "log.Println": noWriter,
	"log.Fatal": noWriter, "log.Fatalf": noWriter, "log.Fatalln": noWriter,
	"log.Panic": noWriter, "log.Panicf": noWriter, "log.Panicln": noWriter,
	"log.Logger.Print": noWriter, "log.Logger.Printf": noWriter, "log.Logger.Println": noWriter,
	"log.Logger.Fatal": noWriter, "log.Logger.Fatalf": noWriter, "log.Logger.Fatalln": noWriter,
	"log.Logger.Panic": noWriter, "log.Logger.Panicf": noWriter, "log.Logger.Panicln": noWriter,
}

// write is call as a write, when it is one.
func (r *secretRun) write(call *ast.CallExpr) (secretWrite, bool) {
	f := calledFunc(r.pass, call)
	if f == nil || f.Pkg() == nil || isSecretDisplay(f) {
		return secretWrite{}, false
	}
	label := secretFuncLabel(f)
	if at, ok := stdWrites[f.Pkg().Path()+"."+secretFuncName(f)]; ok {
		if at == noWriter {
			return secretWrite{label: label, content: call.Args}, true
		}
		if len(call.Args) <= at {
			return secretWrite{}, false
		}
		return r.writeTo(label, call.Args[at], call.Args[at+1:]), true
	}
	if recv := receiverNamed(f); recv != nil && (f.Name() == "Write" || f.Name() == "WriteString") {
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if ok && r.streamWriter(recv, sel.X) {
			return r.writeTo(label, sel.X, call.Args), true
		}
		return secretWrite{}, false
	}
	if idx := r.sinkIndexes(f); len(idx) > 0 {
		return secretWrite{label: label, content: argsAt(f, call, idx)}, true
	}
	return secretWrite{}, false
}

// writeTo is a write of content to writer, which is a buffer or a stream by its type.
func (r *secretRun) writeTo(label string, writer ast.Expr, content []ast.Expr) secretWrite {
	t := r.pass.TypesInfo.TypeOf(writer)
	return secretWrite{label: label, content: content, writer: writer, buffer: t != nil && isMemoryBuffer(t)}
}

// eachWrite calls found for each argument of a write in body that holds a secret.
func (r *secretRun) eachWrite(body ast.Node, look secretLookup, found func(call *ast.CallExpr, through, label string)) {
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		w, ok := r.write(call)
		if !ok || w.buffer {
			return true
		}
		for _, arg := range w.content {
			for _, label := range r.labels(arg, look) {
				found(call, w.label, label)
			}
		}
		return true
	})
}

// eachReturn calls found for each secret a return statement of body (not of a function literal
// in it) hands back.
func (r *secretRun) eachReturn(body ast.Node, look secretLookup, found func(string)) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			for _, e := range n.Results {
				for _, label := range r.labels(e, look) {
					found(label)
				}
			}
		}
		return true
	})
}

// report flags each write of a secret, once per call, and each tee of a secret's buffer.
func (r *secretRun) report() {
	reported := map[*ast.CallExpr]bool{}
	for _, sf := range r.funcs {
		r.eachWrite(sf.decl.Body, r.taintedLook, func(call *ast.CallExpr, through, from string) {
			if !reported[call] {
				reported[call] = true
				r.pass.Reportf(call.Pos(), secretMsg, from, through)
			}
		})
		r.reportTees(sf.decl.Body)
	}
}

const secretTeeMsg = "output holding a secret from %s teed to the terminal (project_secret_scrollback): tee it to a term.SecretScreen, which leaves no copy in the scrollback"

// reportTees flags an io.MultiWriter that sends a child's output both to a stream the person
// reads and to a buffer a secret is then read out of (ccc's sign-in: claude setup-token prints
// the token, which is extracted from the buffer).
func (r *secretRun) reportTees(body ast.Node) {
	read := r.secretBuffers(body)
	if len(read) == 0 {
		return
	}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		f := calledFunc(r.pass, call)
		if f == nil || f.Pkg() == nil || f.Pkg().Path() != "io" || f.Name() != "MultiWriter" {
			return true
		}
		from, buffered := "", false
		streamed := false
		for _, arg := range call.Args {
			if label, ok := read[r.target(arg)]; ok {
				from, buffered = label, true
				continue
			}
			streamed = streamed || r.isStream(arg)
		}
		if buffered && streamed {
			r.pass.Reportf(call.Pos(), secretTeeMsg, from)
		}
		return true
	})
}

// secretBuffers are the in-memory buffers of body a source reads a secret out of, with the
// source's label.
func (r *secretRun) secretBuffers(body ast.Node) map[types.Object]string {
	read := map[types.Object]string{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		label, ok := r.sourceCall(call)
		if !ok {
			return true
		}
		for _, arg := range call.Args {
			ast.Inspect(arg, func(m ast.Node) bool {
				if id, isIdent := m.(*ast.Ident); isIdent {
					if t := r.pass.TypesInfo.TypeOf(id); t != nil && isMemoryBuffer(t) {
						read[r.pass.TypesInfo.ObjectOf(id)] = label
					}
				}
				return true
			})
		}
		return true
	})
	return read
}

// isStream is whether e is a writer the person reads: an io.Writer handed in, os.Stdout or
// os.Stderr.
func (r *secretRun) isStream(e ast.Expr) bool {
	t := r.pass.TypesInfo.TypeOf(e)
	if t == nil {
		return false
	}
	if _, ok := t.Underlying().(*types.Interface); ok {
		return true
	}
	if p, ok := t.(*types.Pointer); ok {
		if named, isNamedType := types.Unalias(p.Elem()).(*types.Named); isNamedType && isNamed(named, "os", "File") {
			return r.streamWriter(named, e)
		}
	}
	return false
}

// exportFacts publishes the package's summaries for the packages that import it.
func (r *secretRun) exportFacts() {
	for _, sf := range r.funcs {
		if idx := sortedIndexes(r.sinks[sf.fn]); len(idx) > 0 {
			r.pass.ExportObjectFact(sf.fn, &secretSinkFact{Params: idx})
		}
		if idx := sortedIndexes(r.passes[sf.fn]); len(idx) > 0 {
			r.pass.ExportObjectFact(sf.fn, &secretPassFact{Params: idx})
		}
		if label, ok := r.sources[sf.fn]; ok {
			r.pass.ExportObjectFact(sf.fn, &secretSourceFact{From: label})
		}
	}
	for obj, label := range r.tainted {
		if v, ok := obj.(*types.Var); ok && v.IsField() && v.Pkg() == r.pass.Pkg {
			r.pass.ExportObjectFact(v, &secretFieldFact{From: label})
		}
	}
}

// sinkIndexes are the parameters of f that reach a write.
func (r *secretRun) sinkIndexes(f *types.Func) []int {
	if f.Pkg() == r.pass.Pkg {
		return sortedIndexes(r.sinks[f])
	}
	var fact secretSinkFact
	if r.pass.ImportObjectFact(f, &fact) {
		return fact.Params
	}
	return nil
}

// passIndexes are the parameters of f whose value its result keeps.
func (r *secretRun) passIndexes(f *types.Func) []int {
	if idx, ok := keepingFuncs[f.Pkg().Path()+"."+f.Name()]; ok && receiverNamed(f) == nil {
		return idx
	}
	if f.Pkg() == r.pass.Pkg {
		return sortedIndexes(r.passes[f])
	}
	var fact secretPassFact
	if r.pass.ImportObjectFact(f, &fact) {
		return fact.Params
	}
	return nil
}

// isSecretDisplay is whether f is the secret screen.
func isSecretDisplay(f *types.Func) bool {
	return f.Pkg() != nil && slices.ContainsFunc(secretDisplays, func(a allowance) bool {
		return ownedBy(f.Pkg().Path(), a.pkg) && secretFuncName(f) == a.fn
	})
}

// allArgs stands for every argument of a call in keepingFuncs.
const allArgs = -1

// keepingFuncs are the standard library functions whose result holds the arguments at the
// indexes given (allArgs: every one).
var keepingFuncs = map[string][]int{
	"fmt.Sprint": {allArgs}, "fmt.Sprintf": {allArgs}, "fmt.Sprintln": {allArgs}, "fmt.Errorf": {allArgs},
	"fmt.Append": {allArgs}, "fmt.Appendf": {allArgs}, "fmt.Appendln": {allArgs},
	"errors.New": {0}, "errors.Join": {allArgs},
	"strings.Join": {0, 1}, "strings.Repeat": {0}, "strings.TrimSpace": {0}, "strings.Trim": {0},
	"strings.TrimLeft": {0}, "strings.TrimRight": {0}, "strings.TrimPrefix": {0}, "strings.TrimSuffix": {0},
	"strings.ToLower": {0}, "strings.ToUpper": {0}, "strings.Replace": {0, 2}, "strings.ReplaceAll": {0, 2},
	"strings.Fields": {0}, "strings.Split": {0}, "strings.SplitN": {0}, "strings.Clone": {0},
	"bytes.TrimSpace": {0}, "bytes.TrimRight": {0}, "bytes.Join": {0, 1}, "bytes.Clone": {0},
	"strconv.Quote": {0}, "strconv.QuoteToASCII": {0},
}

// argsAt are the arguments of call at the parameter indexes idx of f; a variadic parameter is
// every argument from its index on.
func argsAt(f *types.Func, call *ast.CallExpr, idx []int) []ast.Expr {
	sig, ok := f.Type().(*types.Signature)
	if !ok {
		return nil
	}
	var out []ast.Expr
	for i, arg := range call.Args {
		p := i
		if sig.Variadic() && i >= sig.Params().Len()-1 {
			p = sig.Params().Len() - 1
		}
		if slices.Contains(idx, allArgs) || slices.Contains(idx, p) {
			out = append(out, arg)
		}
	}
	return out
}

// receiverPassesOn is whether f's result is its receiver's content: String, Bytes or Error.
func receiverPassesOn(f *types.Func) bool {
	sig, ok := f.Type().(*types.Signature)
	return ok && sig.Recv() != nil && sig.Params().Len() == 0 && slices.Contains([]string{"String", "Bytes", errorMethod}, f.Name())
}

// isSecretSource is whether f is in the source table or a secret type's Reveal.
func isSecretSource(f *types.Func) bool {
	sig, ok := f.Type().(*types.Signature)
	if !ok {
		return false
	}
	if f.Name() == revealMethod && sig.Recv() != nil && sig.Params().Len() == 0 && sig.Results().Len() == 1 && secretText(sig.Results().At(0).Type()) {
		return true
	}
	recv := ""
	if named := receiverNamed(f); named != nil {
		recv = named.Obj().Name()
	}
	return slices.ContainsFunc(secretSources, func(s secretSource) bool {
		return ownedBy(f.Pkg().Path(), s.pkg) && s.recv == recv && s.name == f.Name()
	})
}

// secretFuncName is "func" or "Type.method", as an allowance row names it.
func secretFuncName(f *types.Func) string {
	if named := receiverNamed(f); named != nil {
		return named.Obj().Name() + "." + f.Name()
	}
	return f.Name()
}

// secretFuncLabel is f as a finding names it: "pkg.func" or "pkg.Type.method".
func secretFuncLabel(f *types.Func) string {
	return f.Pkg().Name() + "." + secretFuncName(f)
}

// secretText is whether t is text a secret is held in: a string or a byte slice.
func secretText(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Info()&types.IsString != 0
	case *types.Slice:
		b, ok := u.Elem().Underlying().(*types.Basic)
		return ok && (b.Kind() == types.Byte || b.Kind() == types.Rune)
	}
	return false
}

// secretCarrier is whether a value of type t can hold a secret's text: text, an error or any
// other interface, or a slice of them.
func secretCarrier(t types.Type) bool {
	if secretText(t) {
		return true
	}
	switch u := t.Underlying().(type) {
	case *types.Interface:
		return true
	case *types.Slice:
		return secretCarrier(u.Elem())
	}
	return false
}

// streamWriter is whether a Write method on named, called on x, writes to a stream the person
// reads: an interface (an io.Writer handed in, such as a tool's Stderr), os.Stdout or os.Stderr,
// or an in-memory buffer (a write the rule follows). Any other *os.File is a file the program
// opened: every /dev/tty it opens is opened to read.
func (r *secretRun) streamWriter(named *types.Named, x ast.Expr) bool {
	if _, ok := named.Underlying().(*types.Interface); ok {
		return true
	}
	if isMemoryBuffer(named) {
		return true
	}
	if !isNamed(named, "os", "File") {
		return false
	}
	sel, ok := ast.Unparen(x).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	v, ok := r.pass.TypesInfo.ObjectOf(sel.Sel).(*types.Var)
	return ok && v.Pkg() != nil && v.Pkg().Path() == "os" && (v.Name() == "Stdout" || v.Name() == "Stderr")
}

// isMemoryBuffer is whether t (or what it points to) is a bytes.Buffer or a strings.Builder.
func isMemoryBuffer(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	t = types.Unalias(t)
	return isNamed(t, "bytes", "Buffer") || isNamed(t, "strings", "Builder")
}

// paramIndex is the parameter a summary label names.
func paramIndex(label string) (int, bool) {
	rest, ok := strings.CutPrefix(label, paramLabel)
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(rest)
	return i, err == nil
}

// addIndex adds i to f's set in m, and says whether it was new.
func addIndex(m map[*types.Func]map[int]bool, f *types.Func, i int) bool {
	if m[f][i] {
		return false
	}
	if m[f] == nil {
		m[f] = map[int]bool{}
	}
	m[f][i] = true
	return true
}

// sortedIndexes is the set as a sorted list.
func sortedIndexes(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for i := range set {
		out = append(out, i)
	}
	slices.Sort(out)
	return out
}

// indexWords is a list of parameter indexes as a fact says them: "0 2".
func indexWords(idx []int) string {
	words := make([]string, len(idx))
	for i, n := range idx {
		words[i] = strconv.Itoa(n)
	}
	return strings.Join(words, " ")
}
