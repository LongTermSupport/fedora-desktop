package changed

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/suitepass"
)

// What a local run leaves to CI. A local qa-changed run is for iterating: it narrows the
// semgrep gates to the changed files (semgrepGate) and does not run again a command suite whose
// inputs passed before (suiteKey). CI (ciEnabled) runs everything as qa-all would judge it.

// buildReason starts the reason a suite was picked for a file in its command's build closure.
const buildReason = "builds from "

// openPasses opens the store of suite passes for a local run; CI has none, and a run with no
// cache directory has none either and says so.
func (s *session) openPasses() {
	if s.ci {
		return
	}
	dir, ok := suitepass.Dir(s.env.Getenv)
	if !ok {
		s.errf("qa-changed: no QA_SUITE_PASSES, XDG_CACHE_HOME or HOME: no suite pass is remembered, every picked suite runs\n")
		return
	}
	s.passes = &suitepass.Store{Dir: dir}
}

// suiteKey is the key a suite's pass is recorded under, and whether the suite may be skipped at
// all: in a local run, only a suite every one of whose reasons is its command's build closure
// (a suite that names, reaches or is a changed file always runs). The key covers every file
// under the closure's directories, src-go's go.mod and go.sum, the suite script, its command
// lines and the Go toolchain this tool was built with. An input that cannot be read is an error.
func (s *session) suiteKey(suite pickedSuite) (string, bool, error) {
	if s.passes == nil || len(s.suiteDirs[suite.name]) == 0 {
		return "", false, nil
	}
	for _, why := range suite.why {
		if !strings.HasPrefix(why, buildReason) {
			return "", false, nil
		}
	}
	files := []string{"src-go/go.mod", "src-go/go.sum", suite.name}
	for _, dir := range s.suiteDirs[suite.name] {
		err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.Type().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(s.root, path)
			if err != nil {
				return fmt.Errorf("cannot place %s under the root: %w", path, err)
			}
			files = append(files, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return "", false, fmt.Errorf("cannot read the build closure of %s: %w", suite.name, err)
		}
	}
	extra := []string{runtime.Version()}
	for _, run := range s.suiteRuns(suite) {
		extra = append(extra, strings.Join(run.argv[2:], " "))
	}
	key, err := suitepass.Key(s.root, files, extra...)
	if err != nil {
		return "", false, err
	}
	return key, true, nil
}

// skipSuite is whether a suite's inputs passed before, reporting the skip when they did. A key
// or a record that cannot be read runs the suite, saying why.
func (s *session) skipSuite(suite pickedSuite) (key string, skip bool) {
	key, skippable, err := s.suiteKey(suite)
	if err == nil && skippable {
		skip, err = s.passes.Passed(key)
	}
	if err != nil {
		s.errf("qa-changed: suite %s runs: %v\n", suite.name, err)
		return "", false
	}
	if skip {
		s.outf("✓ suite %s: skipped, unchanged since a recorded pass (%s); CI runs it\n", strings.TrimPrefix(suite.name, "scripts/"), strings.Join(suite.why, "; "))
	}
	return key, skip
}

// recordPass remembers a suite that passed over its key; a record that cannot be written costs
// only a later rerun, and says so.
func (s *session) recordPass(suite pickedSuite, key string) {
	if key == "" {
		return
	}
	if err := s.passes.Record(key); err != nil {
		s.errf("qa-changed: suite %s passed but its pass is not remembered: %v\n", suite.name, err)
	}
}
