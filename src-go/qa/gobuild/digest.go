// Package gobuild is what every deployed Go command shares at build and at QA: the digest of
// the source a command is built from, and the check that the installed commands carry the
// digest of the checkout. It belongs to no one command: the build
// (tasks/build-go-command.yml) bakes the digest into each of them
// (`go build -ldflags "-X main.SourceDigest=<hex>"`), each prints it for `--version`,
// `agent-bus-install team` stamps it, and `declare` recomputes it from the checkout, so "the
// checkout moved on, the play has not run" shows as out of date.
//
// The digest needs no build, and it moves exactly when a rebuild would produce a different
// program. The definition, all paths relative to `<checkout>/src-go` and written with `/`:
//
//  1. The build closure is `cmd/<command>` and every package of the module it imports,
//     directly or not (imports of other modules and of the standard library are not followed:
//     the module has no dependencies). A package is a directory; its imports are read from
//     every `.go` file in it except `*_test.go`, build constraints ignored, so the closure is
//     the largest the tags allow. agree_test.go proves it equals the closure `go list -deps`
//     reports for the build the play does, for every command under src-go/cmd.
//  2. The files are `go.mod` and every regular file directly in a closure directory,
//     `*_test.go` excepted (`usage.txt`, which a binary embeds, counts; `testdata` does not).
//  3. Sorted by path (bytewise), the digest is the sha256 of the concatenation, per file, of
//     `<path>NUL<sha256 hex of the file's bytes>LF`.
package gobuild

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// ModuleDir is the Go module's directory in the checkout.
const ModuleDir = "src-go"

// modulePath is the module line of <moduleDir>/go.mod.
func modulePath(moduleDir string) (string, error) {
	goMod := filepath.Join(moduleDir, "go.mod")
	file, err := os.Open(filepath.Clean(goMod))
	if err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if fields := strings.Fields(scanner.Text()); len(fields) == 2 && fields[0] == "module" {
			return fields[1], errors.Join(scanner.Err(), file.Close())
		}
	}
	return "", errors.Join(fmt.Errorf("%s names no module", goMod), scanner.Err(), file.Close())
}

// sources are the non-test Go files directly in a package directory, sorted.
func sources(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type().IsRegular() && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			names = append(names, filepath.Join(dir, name))
		}
	}
	slices.Sort(names)
	return names, nil
}

// imports are the import paths one Go file names, comments not being imports.
func imports(path string) ([]string, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("cannot read the imports of %s: %w", path, err)
	}
	paths := make([]string, 0, len(parsed.Imports))
	for _, spec := range parsed.Imports {
		unquoted, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("%s: import %s: %w", path, spec.Path.Value, err)
		}
		paths = append(paths, unquoted)
	}
	return paths, nil
}

// localImports are the packages of the module (as src-go-relative directories) the package
// directly imports, and an error when it has no Go files.
func localImports(moduleDir, prefix, pkg string) ([]string, error) {
	files, err := sources(filepath.Join(moduleDir, filepath.FromSlash(pkg)))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("package %s has no Go files (imported, or the command)", pkg)
	}
	var local []string
	for _, file := range files {
		found, err := imports(file)
		if err != nil {
			return nil, err
		}
		for _, name := range found {
			if rest, ok := strings.CutPrefix(name, prefix); ok {
				local = append(local, rest)
			}
		}
	}
	return local, nil
}

// Closure is the package directories the command's binary is built from, sorted, relative to
// src-go.
func Closure(source, command string) ([]string, error) {
	moduleDir := filepath.Join(source, ModuleDir)
	module, err := modulePath(moduleDir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	todo := []string{"cmd/" + command}
	for len(todo) > 0 {
		pkg := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		if seen[pkg] {
			continue
		}
		seen[pkg] = true
		local, err := localImports(moduleDir, module+"/", pkg)
		if err != nil {
			return nil, err
		}
		todo = append(todo, local...)
	}
	return slices.Sorted(maps.Keys(seen)), nil
}

// Files is every file the digest covers, sorted, relative to src-go.
func Files(source, command string) ([]string, error) {
	closure, err := Closure(source, command)
	if err != nil {
		return nil, err
	}
	moduleDir := filepath.Join(source, ModuleDir)
	names := []string{"go.mod"}
	for _, pkg := range closure {
		entries, err := os.ReadDir(filepath.Join(moduleDir, filepath.FromSlash(pkg)))
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.Type().IsRegular() && !strings.HasSuffix(entry.Name(), "_test.go") {
				names = append(names, pkg+"/"+entry.Name())
			}
		}
	}
	slices.Sort(names)
	return names, nil
}

// SourceDigest is the sha256 (hex) the command's binary built from this checkout carries.
func SourceDigest(source, command string) (string, error) {
	names, err := Files(source, command)
	if err != nil {
		return "", err
	}
	moduleDir := filepath.Join(source, ModuleDir)
	listing := sha256.New()
	for _, name := range names {
		data, err := os.ReadFile(filepath.Clean(filepath.Join(moduleDir, filepath.FromSlash(name))))
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		if _, err := fmt.Fprintf(listing, "%s\x00%s\n", name, hex.EncodeToString(sum[:])); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(listing.Sum(nil)), nil
}
