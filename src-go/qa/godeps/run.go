// Package godeps is the pair of gates on src-go's dependencies (Plan 00204 decision D2:
// vendored, pinned, allowlisted). qa-go-deps holds src-go/go.mod to src-go/go-deps.allowlist
// (every module listed with its reason, a pseudo-version or a replace only with its own row,
// no row go.mod does not need) and proves the vendor tree is the one go.mod and go.sum describe;
// qa-go-vuln runs the qa module's pinned govulncheck over src-go, which reads vendor/. The
// specification is the plan's SPEC-qa-go-deps.md.
package godeps

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/emit"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootargs"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/rootpath"
)

// ExitCode is the status both commands exit with.
type ExitCode int

// The statuses callers read: 0 pass, 1 a finding, 2 the run could not judge.
const (
	ExitOK        ExitCode = 0
	ExitFailed    ExitCode = 1
	ExitCannotRun ExitCode = 2
)

// The files the gates read, relative to the repository root.
const (
	srcGoDir      = "src-go"
	goModFile     = "src-go/go.mod"
	allowlistFile = "src-go/go-deps.allowlist"
	vendorDir     = "src-go/vendor"
	qaModuleDir   = "src-go/qa"
)

// govulncheckFound is govulncheck's status when the code calls a vulnerable function.
const govulncheckFound = 3

// goEnv is the environment of every go command (G10): no -mod or other flag from the caller,
// no workspace, the installed toolchain only, and the public module proxy and checksum database
// with no module exempted from either, so nothing in the caller's environment changes a verdict
// or its wording.
var goEnv = []string{
	"GOFLAGS=", "GOWORK=off", "GOTOOLCHAIN=local",
	"GOPROXY=https://proxy.golang.org,direct", "GOSUMDB=sum.golang.org",
	"GONOSUMDB=", "GONOPROXY=", "GOPRIVATE=", "GOINSECURE=",
}

// checksumMismatch is in go's output when a module's content disagrees with go.sum or the
// checksum database: a finding, whatever else the output says.
const checksumMismatch = "SECURITY ERROR"

// unreachableMarks are in go's output when it could not fetch a module (a cold module cache
// with no network, or a proxy that does not answer): the step could not judge (G12).
var unreachableMarks = []string{
	"module lookup disabled", "dial tcp", "no such host", "connection refused",
	"network is unreachable", "i/o timeout", "TLS handshake timeout", "proxyconnect",
}

// unreachable is whether a failed go command's output says it could not fetch a module.
func unreachable(output string) bool {
	if strings.Contains(output, checksumMismatch) {
		return false
	}
	for _, mark := range unreachableMarks {
		if strings.Contains(output, mark) {
			return true
		}
	}
	return false
}

// failed records a go command that failed: a step that could not reach the module proxy could not
// judge; any other failure is the finding what says.
func (v *verdict) failed(step, what, output string) {
	if unreachable(output) {
		v.unjudged = append(v.unjudged, step+" could not reach the module proxy, so it could not judge:\n"+output)
		return
	}
	v.findings = append(v.findings, what+":\n"+output)
}

// Env is everything the commands touch outside the repository tree.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	// Cwd is the default root when neither --root nor --scripts is given.
	Cwd string
	// Resolve makes a root free of symlinks (filepath.EvalSymlinks).
	Resolve rootpath.Eval
	// Runner starts the go commands.
	Runner proc.Runner
	// TempDir makes the scratch directory go mod vendor writes into; the gate removes it.
	TempDir func() (string, error)
}

const depsUsage = `usage: qa-go-deps [--root DIR] [--scripts DIR]

Plan 00204: every module src-go/go.mod requires is on src-go/go-deps.allowlist with its reason,
a pseudo-version or a replace directive has its own row, no row is stale, go mod verify passes,
go.mod and go.sum are tidy, and src-go/vendor is exactly what go mod vendor writes.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 clean, 1 a finding, 2 bad usage or the run could not judge.
`

const vulnUsage = `usage: qa-go-vuln [--root DIR] [--scripts DIR]

Plan 00204: govulncheck, pinned by the tool directive of src-go/qa/go.mod, finds no known
vulnerability that src-go's code (built from src-go/vendor) reaches.

  --root DIR      the repository root (default: the parent of --scripts, else the current directory)
  --scripts DIR   the scripts directory; the root is its parent
  -h, --help      this text

Exit codes: 0 clean, 1 a vulnerability is reached, 2 bad usage or govulncheck could not judge.
`

// Main runs qa-go-deps and returns the exit status.
func Main(args []string, env Env) ExitCode {
	return start(args, env, depsUsage, deps)
}

// MainVuln runs qa-go-vuln and returns the exit status.
func MainVuln(args []string, env Env) ExitCode {
	return start(args, env, vulnUsage, vuln)
}

// start is what both commands share: the command line, the root, and the exit-code rule for a
// stream that cannot be written.
func start(args []string, env Env, usage string, body func(string, Env, *emit.Emitter, *emit.Emitter) ExitCode) ExitCode {
	out, errOut := emit.New(env.Stdout), emit.New(env.Stderr)
	return emit.Finish(out, errOut, func() ExitCode {
		parsed, err := rootargs.Strict(args)
		if err != nil {
			errOut.Line("ERROR: " + err.Error())
			if unknown, isUnknown := errors.AsType[*rootargs.UnknownArgError](err); isUnknown && unknown != nil {
				errOut.Block(usage)
			}
			return ExitCannotRun
		}
		if parsed.Help {
			out.Block(usage)
			return ExitOK
		}
		root, err := rootpath.RootVia(env.Resolve, env.Cwd, parsed.Root, parsed.Scripts)
		if err != nil {
			errOut.Line("ERROR: " + err.Error())
			return ExitCannotRun
		}
		return body(root, env, out, errOut)
	}(), ExitCannotRun)
}

// goRun runs one go command with the fixed environment.
func goRun(env Env, dir string, argv ...string) (proc.Result, error) {
	return env.Runner.Run(context.Background(), proc.Cmd{Argv: argv, Dir: dir, Env: goEnv, Merge: true})
}

// deps is qa-go-deps (G1-G11).
func deps(root string, env Env, out, errOut *emit.Emitter) ExitCode {
	v, err := readAndJudge(root)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	files, err := syncFindings(root, env, &v)
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	if err := keepoutFindings(root, env, &v); err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	report := func(heading string, items []string) {
		errOut.Line(heading)
		for _, f := range items {
			errOut.Line("  " + strings.TrimRight(strings.ReplaceAll(f, "\n", "\n    "), " \n"))
		}
	}
	if len(v.unjudged) > 0 {
		report(fmt.Sprintf("ERROR: %d step(s) could not judge src-go's dependencies (no network or proxy, and the module cache is cold):", len(v.unjudged)), v.unjudged)
	}
	if len(v.findings) > 0 {
		report(fmt.Sprintf("ERROR: %d finding(s) on src-go's dependencies (CLAUDE/Plan/00204-go-tui-polish/SPEC-qa-go-deps.md):", len(v.findings)), v.findings)
		return ExitFailed
	}
	if len(v.unjudged) > 0 {
		return ExitCannotRun
	}
	out.Line(fmt.Sprintf("GO-DEPS-OK %d module(s) allowlisted (%d at a pseudo-version, %d replaced); go.sum verified, tidy, vendor/ in sync (%d files)",
		v.modules, v.pseudo, v.replaced, files))
	return ExitOK
}

// readAndJudge reads the allowlist and go.mod and judges them; anything unreadable is an error.
func readAndJudge(root string) (verdict, error) {
	repo := os.DirFS(root)
	allowText, err := fs.ReadFile(repo, allowlistFile)
	if err != nil {
		return verdict{}, fmt.Errorf("cannot read the allowlist: %w", err)
	}
	allow, err := parseAllowlist(string(allowText))
	if err != nil {
		return verdict{}, fmt.Errorf("%s: %w", allowlistFile, err)
	}
	goMod, err := fs.ReadFile(repo, goModFile)
	if err != nil {
		return verdict{}, fmt.Errorf("cannot read go.mod: %w", err)
	}
	v, err := judge(goMod, allow)
	if err != nil {
		return verdict{}, fmt.Errorf("cannot parse go.mod: %w", err)
	}
	return v, nil
}

// syncFindings runs go mod verify, go mod tidy -diff and go mod vendor into a scratch tree, adds
// a finding for each that fails or differs, and returns the number of files go mod vendor wrote.
func syncFindings(root string, env Env, v *verdict) (int, error) {
	srcGo := filepath.Join(root, srcGoDir)
	for _, step := range []struct {
		argv []string
		what string
	}{
		{[]string{"go", "mod", "verify"}, "go mod verify failed: a module in the cache has changed since it was downloaded (go.sum is checked by tidy and vendor)"},
		{[]string{"go", "mod", "tidy", "-diff"}, "go.mod or go.sum is not tidy (fix: cd src-go && go mod tidy && go mod vendor)"},
	} {
		res, runErr := goRun(env, srcGo, step.argv...)
		if runErr != nil {
			return 0, runErr
		}
		if res.Code != 0 {
			v.failed(strings.Join(step.argv, " "), step.what, res.Stdout)
		}
	}
	scratch, err := env.TempDir()
	if err != nil {
		return 0, fmt.Errorf("cannot make the scratch directory for go mod vendor: %w", err)
	}
	files, err := vendorFindings(root, env, filepath.Join(scratch, "vendor"), v)
	if rmErr := os.RemoveAll(scratch); rmErr != nil && err == nil {
		err = fmt.Errorf("cannot remove the scratch directory: %w", rmErr)
	}
	return files, err
}

// keepoutFindings runs go list -deps over each keepout row's package and adds a finding when
// any package it builds from, directly or through another, is in the kept-out module (GD1).
func keepoutFindings(root string, env Env, v *verdict) error {
	for _, r := range v.keepouts {
		res, err := goRun(env, filepath.Join(root, srcGoDir), "go", "list", "-deps", "-f", "{{with .Module}}{{.Path}}{{end}}", r.pkg)
		if err != nil {
			return err
		}
		if res.Code != 0 {
			v.failed("go list -deps "+r.pkg, "go list failed over "+r.pkg+", so the build is broken", res.Stdout)
			continue
		}
		if slices.Contains(strings.Split(res.Stdout, "\n"), r.module) {
			v.findings = append(v.findings, fmt.Sprintf("%s depends on %s, which %s:%d keeps out of it: %s", r.pkg, r.module, allowlistFile, r.line, r.reason))
		}
	}
	return nil
}

// vendorFindings has go mod vendor write the tree go.mod and go.sum describe into written, adds
// a finding for each file src-go/vendor has different, and returns the number of files written.
func vendorFindings(root string, env Env, written string, v *verdict) (int, error) {
	res, err := goRun(env, filepath.Join(root, srcGoDir), "go", "mod", "vendor", "-o", written)
	if err != nil {
		return 0, err
	}
	if res.Code != 0 {
		v.failed("go mod vendor", "go mod vendor failed, so vendor/ could not be compared", res.Stdout)
		return 0, nil
	}
	diffs, files, err := compareTrees(written, filepath.Join(root, vendorDir))
	if err != nil {
		return 0, fmt.Errorf("cannot compare vendor/: %w", err)
	}
	for _, d := range diffs {
		v.findings = append(v.findings, "vendor/"+d+" (fix: cd src-go && go mod vendor)")
	}
	return files, nil
}

// vuln is qa-go-vuln (V1-V3).
func vuln(root string, env Env, out, errOut *emit.Emitter) ExitCode {
	srcGo := filepath.Join(root, srcGoDir)
	res, err := goRun(env, filepath.Join(root, qaModuleDir), "go", "tool", "govulncheck", "-C", srcGo, "./...")
	if err != nil {
		errOut.Line("ERROR: " + err.Error())
		return ExitCannotRun
	}
	switch res.Code {
	case 0:
		out.Line("GO-VULN-OK src-go: govulncheck found no vulnerability the code reaches")
		return ExitOK
	case govulncheckFound:
		errOut.Text(res.Stdout)
		errOut.Line("ERROR: govulncheck found a vulnerability src-go's code reaches: bump the module (go get <module>@<fixed version>, go mod vendor) or the Go toolchain")
		return ExitFailed
	default:
		errOut.Text(res.Stdout)
		errOut.Line(fmt.Sprintf("ERROR: govulncheck could not judge (status %d); a scan that did not run is not a pass", res.Code))
		return ExitCannotRun
	}
}
