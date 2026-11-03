package rules

import (
	"go/ast"
	"go/types"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// hostnameFile is the one file of teamchat that may read the machine's hostname: the reader of
// the declaration reads it to refuse a site that spells it (Plan 00244 SPEC-cross-network X26).
// It is matched as a path suffix on a whole element, so the fixtures and the real module share it.
const hostnameFile = "internal/teamchat/declare/machinehost.go"

// hostnameTrees are what the rule judges: teamchat's packages and its two commands.
var hostnameTrees = transportTrees

// hostnameReads are the functions and methods that name a machine, by package path: the machine's
// own hostname, and an address's name by reverse lookup. recv is the receiver type name of a
// method, "" for a package function.
var hostnameReads = []struct{ pkg, recv, name string }{
	{"os", "", "Hostname"},
	{"net", "", "LookupAddr"},
	{"net", "Resolver", "LookupAddr"},
}

const hostnameFix = " (project_hostname_read): a handle's site is declared per machine (teamchat_site) and never read from the machine; use the hostname seam of declare, whose one file reads it for the refusal of a site that spells it"

// HostnameRead confines the reading of a machine's hostname, and the reverse lookup of an
// address, to one file of teamchat.
var HostnameRead = &analysis.Analyzer{
	Name: "project_hostname_read",
	Doc: `project_hostname_read flags, in internal/teamchat, cmd/teamchat and cmd/teamchat-admin and
below, every use of os.Hostname, net.LookupAddr and (*net.Resolver).LookupAddr, except in the one
file internal/teamchat/declare/machinehost.go:

- a call, and a reference that is not called (a function value, a package-level seam), through
  any import name;
- the three spellings of the reverse lookup: net.LookupAddr, net.DefaultResolver.LookupAddr and a
  resolver's own LookupAddr.

Test files are judged as the code is: the hostname arrives in a test through the seam, as a fake.
The exemption is the whole path, so another file of the same package, and a file of the same
name in another package, are judged. Nothing else is exempt, and there is no allowance table.

A handle's site is declared per machine (teamchat_site) and no other source feeds it (Plan 00244
SPEC-cross-network X25): a machine's hostname in a handle is a hostname in every roster, mention
and log of every team the machine joins, and cannot be taken back (a handle is permanent). The
one read of the hostname refuses a declared site that spells it (X26); it is best effort, so the
rule keeps the other places from reading it at all.

Not caught: net.LookupHost and net.LookupIP of a name that came from a handle; a hostname read
through exec (the hostname program) or from /etc/hostname, /proc or the environment; and a name
built by reflection.

How to fix: declare the site (teamchat_site), or take the hostname through declare's seam
(declare.MachineHostname handed to the function that needs it); never read it where the handle
is made.`,
	Run:        perPass(runHostnameRead),
	ResultType: noResultType,
}

func runHostnameRead(pass *analysis.Pass) {
	if !slices.ContainsFunc(hostnameTrees, func(tree string) bool { return inTree(pass.Pkg.Path(), tree) }) {
		return
	}
	for _, f := range pass.Files {
		if hostnameExempt(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if fn, ok := pass.TypesInfo.Uses[id].(*types.Func); ok && hostnameRead(fn) {
				pass.Reportf(id.Pos(), "%s.%s read outside teamchat/declare/machinehost.go%s", fn.Pkg().Name(), fn.Name(), hostnameFix)
			}
			return true
		})
	}
}

// hostnameExempt is whether f is the one file that reads the machine's hostname.
func hostnameExempt(pass *analysis.Pass, f *ast.File) bool {
	name := filepath.ToSlash(pass.Fset.Position(f.Pos()).Filename)
	return name == hostnameFile || strings.HasSuffix(name, "/"+hostnameFile)
}

// hostnameRead is whether fn is one of hostnameReads.
func hostnameRead(fn *types.Func) bool {
	if fn.Pkg() == nil {
		return false
	}
	recv := ""
	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
		named := receiverNamed(fn)
		if named == nil {
			return false
		}
		recv = named.Obj().Name()
	}
	return slices.ContainsFunc(hostnameReads, func(r struct{ pkg, recv, name string }) bool {
		return r.pkg == fn.Pkg().Path() && r.recv == recv && r.name == fn.Name()
	})
}
