// Package selftestfake is semgrep as the gates' fakes answer semgrepreport's self-test: a report
// that reports exactly the lines the fixture's copy marks, or none of them. It reads the copy
// semgrep was handed, the last argument, so a fake judges the fixture the gate really copied.
package selftestfake

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/semgrepreport"
)

// IsSelfTest is whether argv is semgrepreport's self-test: semgrep's scan subcommand, which the
// gates' own scans do not name.
func IsSelfTest(argv []string) bool {
	return len(argv) > 1 && argv[0] == "semgrep" && argv[1] == "scan"
}

// Passing answers the self-test with every marked line reported.
func Passing(argv []string) (proc.Result, error) { return answer(argv, true) }

// Missing answers the self-test with nothing reported, so every marked line is missed.
func Missing(argv []string) (proc.Result, error) { return answer(argv, false) }

// readCopy is the copy's text, read inside its own directory.
func readCopy(copyPath string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(copyPath))
	if err != nil {
		return nil, err
	}
	data, err := root.ReadFile(filepath.Base(copyPath))
	return data, errors.Join(err, root.Close())
}

func answer(argv []string, hit bool) (proc.Result, error) {
	copyPath := argv[len(argv)-1]
	data, err := readCopy(copyPath)
	if err != nil {
		return proc.Result{}, fmt.Errorf("selftestfake: cannot read the copy: %w", err)
	}
	results := []map[string]any{}
	if hit {
		marked := semgrepreport.ExpectedLines(string(data))
		for _, id := range slices.Sorted(maps.Keys(marked)) {
			for _, line := range marked[id] {
				results = append(results, map[string]any{"check_id": id, "path": copyPath, "start": map[string]int{"line": line}})
			}
		}
	}
	doc := map[string]any{"results": results, "errors": []any{}, "paths": map[string][]string{"scanned": {copyPath}}}
	out, err := json.Marshal(doc)
	if err != nil {
		return proc.Result{}, fmt.Errorf("selftestfake: %w", err)
	}
	return proc.Result{Stdout: string(out)}, nil
}
