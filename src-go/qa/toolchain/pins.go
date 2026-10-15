package toolchain

import (
	"bytes"
	"errors"
	"fmt"
	"go/version"
	"io/fs"
	"strings"

	"golang.org/x/mod/modfile"
	"mvdan.cc/sh/v3/syntax"
)

const (
	pinFile     = ".qa-versions"
	rootGoMod   = "src-go/go.mod"
	qaGoModFile = "src-go/qa/go.mod"
)

// goModFiles are the modules whose go directives floor go.
var goModFiles = []string{rootGoMod, qaGoModFile}

// readPins parses .qa-versions (never executes it) into key -> version. Each problem is a line of
// the error; the file must pin exactly the keys of pinnedTools.
func readPins(fsys fs.FS) (map[string]string, error) {
	data, err := fs.ReadFile(fsys, pinFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s, the single source of truth for the QA tool versions: %w", pinFile, err)
	}
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(bytes.NewReader(data), pinFile)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}
	pins := map[string]string{}
	var problems []error
	for _, stmt := range file.Stmts {
		line := stmt.Pos().Line()
		p, err := assignment(stmt)
		if err != nil {
			problems = append(problems, fmt.Errorf("%s:%d: %w", pinFile, line, err))
			continue
		}
		switch _, dup := pins[p.key]; {
		case !isPinned(p.key):
			problems = append(problems, fmt.Errorf("%s:%d: %s is not a pinned tool, so no assertion reads it", pinFile, line, p.key))
		case dup:
			problems = append(problems, fmt.Errorf("%s:%d: %s is assigned twice", pinFile, line, p.key))
		case p.value == "":
			problems = append(problems, fmt.Errorf("%s:%d: %s has an empty value", pinFile, line, p.key))
		default:
			pins[p.key] = p.value
		}
	}
	for _, t := range pinnedTools {
		if _, ok := pins[t.key]; !ok {
			problems = append(problems, fmt.Errorf("%s: no value for %s", pinFile, t.key))
		}
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return pins, nil
}

// assignment is the KEY and VALUE of a statement that is one plain assignment and nothing else.
func assignment(stmt *syntax.Stmt) (pin, error) {
	as := plainAssign(stmt)
	if as == nil {
		return pin{}, errors.New("only plain KEY=VALUE lines are allowed")
	}
	if as.Value == nil {
		return pin{key: as.Name.Value}, nil
	}
	if len(as.Value.Parts) != 1 {
		return pin{}, errors.New("the value must be one unquoted literal")
	}
	lit, isLit := as.Value.Parts[0].(*syntax.Lit)
	if !isLit || strings.Trim(lit.Value, versionChars) != "" {
		return pin{}, errors.New("the value must be one unquoted literal of letters, digits and . _ + -")
	}
	return pin{key: as.Name.Value, value: lit.Value}, nil
}

// pin is one KEY=VALUE line of .qa-versions.
type pin struct{ key, value string }

// plainAssign is the assignment of a statement that is exactly one KEY=VALUE and nothing else, or
// nil.
func plainAssign(stmt *syntax.Stmt) *syntax.Assign {
	call, isCall := stmt.Cmd.(*syntax.CallExpr)
	if !isCall || len(call.Args) != 0 || len(call.Assigns) != 1 {
		return nil
	}
	if len(stmt.Redirs) != 0 || stmt.Background || stmt.Negated || stmt.Coprocess {
		return nil
	}
	as := call.Assigns[0]
	if as.Append || as.Array != nil || as.Index != nil || as.Naked || as.Name == nil {
		return nil
	}
	return as
}

const versionChars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ._+-"

func isPinned(key string) bool {
	for _, t := range pinnedTools {
		if t.key == key {
			return true
		}
	}
	return false
}

// goFloor is the go version a machine needs, and the go.mod that says so.
type goFloor struct{ version, file string }

// readGoFloor is the higher of the go directives of the two modules (the root one wins a tie):
// a host that can build src-go but not the QA module is not a host the gates can run on.
func readGoFloor(fsys fs.FS) (goFloor, error) {
	var floor goFloor
	for _, file := range goModFiles {
		mod, err := readGoMod(fsys, file)
		if err != nil {
			return goFloor{}, err
		}
		if mod.Go == nil || mod.Go.Version == "" {
			return goFloor{}, fmt.Errorf("%s has no go directive", file)
		}
		if !version.IsValid("go" + mod.Go.Version) {
			return goFloor{}, fmt.Errorf("%s names the Go floor %q, which is not a Go version", file, mod.Go.Version)
		}
		if floor.file == "" || version.Compare("go"+mod.Go.Version, "go"+floor.version) > 0 {
			floor = goFloor{version: mod.Go.Version, file: file}
		}
	}
	return floor, nil
}

func readGoMod(fsys fs.FS, file string) (*modfile.File, error) {
	data, err := fs.ReadFile(fsys, file)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s, which holds a Go floor: %w", file, err)
	}
	mod, err := modfile.ParseLax(file, data, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot parse %s: %w", file, err)
	}
	return mod, nil
}

// goQACIModule is the module the QA module requires and the go-qa binary is built from.
const goQACIModule = "github.com/LongTermSupport/go-qa-ci"

// readGoQACIRequire is the version of go-qa-ci the QA module's go.mod requires.
func readGoQACIRequire(fsys fs.FS) (string, error) {
	mod, err := readGoMod(fsys, qaGoModFile)
	if err != nil {
		return "", err
	}
	for _, r := range mod.Require {
		if r.Mod.Path == goQACIModule {
			return r.Mod.Version, nil
		}
	}
	return "", fmt.Errorf("%s does not require %s, the module the project rules are built against", qaGoModFile, goQACIModule)
}
