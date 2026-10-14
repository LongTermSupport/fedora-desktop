// Package published is what the file-scoped QA gates share about scope: the files git would
// publish (tracked, or untracked and not ignored), the table of directory trees a gate must not
// gate on (scripts/qa-trees.tsv), and the first line of a file for classifying it by what it is.
package published

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// gitTimeout bounds each git listing.
const gitTimeout = 2 * time.Minute

// headBytes is how much of a file is read to see its first line.
const headBytes = 1024

// Files is the files git would publish from root that are still in the work tree, sorted.
func Files(ctx context.Context, runner proc.Runner, root string) ([]string, error) {
	listed, err := gitList(ctx, runner, root, "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	deleted, err := gitList(ctx, runner, root, "--deleted")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, rel := range listed {
		if !slices.Contains(deleted, rel) {
			files = append(files, rel)
		}
	}
	slices.Sort(files)
	return slices.Compact(files), nil
}

func gitList(ctx context.Context, runner proc.Runner, root string, flags ...string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	res, err := runner.Run(ctx, proc.Cmd{Argv: append([]string{"git", "ls-files", "-z"}, flags...), Dir: root})
	switch {
	case err != nil:
	case res.TimedOut:
		err = fmt.Errorf("git did not finish within %s", gitTimeout)
	case res.Leftover:
		err = errors.New("git left a process holding its output open")
	case res.Code != 0:
		err = fmt.Errorf("git exited %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	if err != nil {
		return nil, fmt.Errorf("cannot list the files git would publish under %s: %w", root, err)
	}
	var files []string
	for p := range strings.SplitSeq(res.Stdout, "\x00") {
		if p != "" {
			files = append(files, p)
		}
	}
	return files, nil
}

// Trees reads the directory names of one list (the first column) from the table. Finding none is
// an error: an empty list would gate trees that are not ours.
func Trees(file, list string) ([]string, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, fmt.Errorf("cannot read the excluded trees: %w", err)
	}
	trees, err := parseTrees(f, file, list)
	if closeErr := f.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("%s: %w", file, closeErr)
	}
	return trees, err
}

func parseTrees(r io.Reader, name, list string) ([]string, error) {
	var trees []string
	scanner := bufio.NewScanner(r)
	for n := 1; scanner.Scan(); n++ {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 2 || fields[0] == "" || fields[1] == "" {
			return nil, fmt.Errorf("%s:%d: expected '<list><TAB><dir>', got %q", name, n, line)
		}
		if fields[0] == list {
			trees = append(trees, fields[1])
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if len(trees) == 0 {
		return nil, fmt.Errorf("%s has no '%s' row: with no excluded trees the gate would check trees that are not ours", name, list)
	}
	return trees, nil
}

// Excluded is whether the path lies inside an excluded tree: /dir/ occurs as whole segments
// anywhere in /path/.
func Excluded(rel string, trees []string) bool {
	within := "/" + rel + "/"
	return slices.ContainsFunc(trees, func(dir string) bool { return strings.Contains(within, "/"+dir+"/") })
}

// FirstLine is the first line of the file (at most the first kilobyte), without its newline.
func FirstLine(full string) (string, error) {
	f, err := os.Open(full)
	if err != nil {
		return "", err
	}
	head, err := io.ReadAll(io.LimitReader(f, headBytes))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	text := string(head)
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	return text, nil
}
