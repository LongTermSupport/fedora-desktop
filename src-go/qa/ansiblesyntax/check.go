package ansiblesyntax

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

const statusPass, statusFail, reportType = "pass", "fail", "ansible-syntax"

// entry is one checked playbook in the machine report.
type entry struct {
	File   string `json:"file"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// checkAll syntax-checks every playbook, a few at a time, and returns the entries in the order
// of the playbooks whatever order the checks finished in. A check that could not be judged
// (the engine would not start, hung or left a process behind) ends the run; the first such error
// in path order is the one reported.
func (g *gate) checkAll(ctx context.Context, playbooks []string) ([]entry, error) {
	results := make([]entry, len(playbooks))
	errs := make([]error, len(playbooks))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for i := range jobs {
				results[i], errs[i] = g.check(ctx, playbooks[i])
			}
		})
	}
	for i := range playbooks {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("%s: %w", playbooks[i], err)
		}
	}
	return results, nil
}

// check is the verdict on one playbook.
func (g *gate) check(ctx context.Context, rel string) (entry, error) {
	res, err := g.runTool(ctx, checkTimeout, "ansible-playbook", "--syntax-check", filepath.Join(g.root, rel))
	if err != nil {
		return entry{}, err
	}
	out := entry{File: rel, Type: reportType, Status: statusPass}
	if res.Code != 0 {
		out.Status = statusFail
		out.Error = strings.ReplaceAll(strings.TrimRight(res.Stderr, " \t\r\n"), g.root+string(filepath.Separator), "")
		if out.Error == "" {
			out.Error = fmt.Sprintf("ansible-playbook exited %d with no output", res.Code)
		}
	}
	return out, nil
}
