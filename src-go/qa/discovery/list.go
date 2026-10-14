package discovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// gitTimeout bounds each git process.
const gitTimeout = 2 * time.Minute

// headBytes is how much of a file is read to see its shebang.
const headBytes = 1024

// language says what makes a file one of its kind: a name extension, or a first-line shebang.
// A template (.j2) is not code until rendered, so only the template language looks at one.
type language struct {
	exts    []string
	shebang func(line string) bool
	// template is that the language is the .j2 files whose shebang is the language's.
	template bool
}

var (
	shellLang    = language{exts: []string{".sh", ".bash"}, shebang: shellShebang}
	pythonLang   = language{exts: []string{".py"}, shebang: pythonShebang}
	templateLang = language{shebang: pythonShebang, template: true}
)

// finder lists and classifies files for one run.
type finder struct {
	root   string
	runner proc.Runner
}

// files is the files git would publish (tracked, or untracked and not ignored) when published,
// else those it tracks; each without those listed but gone from the tree. Sorted, no duplicates.
func (f finder) files(ctx context.Context, published bool) ([]string, error) {
	flags := []string{}
	if published {
		flags = []string{"--cached", "--others", "--exclude-standard"}
	}
	listed, err := f.gitList(ctx, flags...)
	if err != nil {
		return nil, err
	}
	deleted, err := f.gitList(ctx, "--deleted")
	if err != nil {
		return nil, err
	}
	var kept []string
	for _, rel := range listed {
		if !slices.Contains(deleted, rel) {
			kept = append(kept, rel)
		}
	}
	slices.Sort(kept)
	return slices.Compact(kept), nil
}

func (f finder) gitList(ctx context.Context, flags ...string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	argv := append([]string{"git", "ls-files", "-z"}, flags...)
	res, err := f.runner.Run(ctx, proc.Cmd{Argv: argv, Dir: f.root})
	switch {
	case err == nil && res.TimedOut:
		err = fmt.Errorf("git did not finish within %s", gitTimeout)
	case err == nil && res.Leftover:
		err = errors.New("git left a process holding its output open")
	case err == nil && res.Code != 0:
		err = fmt.Errorf("git exited %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	if err != nil {
		return nil, fmt.Errorf("cannot list the files git would publish under %s: %w", f.root, err)
	}
	var files []string
	for p := range strings.SplitSeq(res.Stdout, "\x00") {
		if p != "" {
			files = append(files, p)
		}
	}
	return files, nil
}

// regular is whether the entry is a regular file, a symlink being judged by its target. A link
// that cannot be followed is an error when mustResolve (its name claims it is a file of the kind
// asked for), else not a file.
func (f finder) regular(rel string, mustResolve bool) (bool, error) {
	full := filepath.Join(f.root, filepath.FromSlash(rel))
	info, err := os.Lstat(full)
	if err != nil {
		return false, fmt.Errorf("cannot inspect %s: %w", rel, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		info, err = os.Stat(full)
		if err != nil {
			if mustResolve {
				return false, fmt.Errorf("the symlink %s cannot be followed: %w", rel, err)
			}
			return false, nil
		}
	}
	return info.Mode().IsRegular(), nil
}

// isOfKind decides by what the file is: a name of the language, or a shebang of it. Mode bits play
// no part.
func (f finder) isOfKind(rel string, lang language) (bool, error) {
	if lang.template != (path.Ext(rel) == ".j2") {
		return false, nil
	}
	if slices.Contains(lang.exts, path.Ext(rel)) {
		return f.regular(rel, true)
	}
	ok, err := f.regular(rel, false)
	if err != nil || !ok {
		return false, err
	}
	line, err := firstLine(filepath.Join(f.root, filepath.FromSlash(rel)))
	if err != nil {
		return false, fmt.Errorf("cannot read %s: %w", rel, err)
	}
	return lang.shebang(line), nil
}

// firstLine is the file's first line ("" for an empty file); the line need not end in a newline.
func firstLine(full string) (string, error) {
	file, err := os.Open(full)
	if err != nil {
		return "", err
	}
	head, err := io.ReadAll(io.LimitReader(file, headBytes))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	text := string(head)
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	return text, err
}

// pick keeps the files for which keep says yes.
func pick(files []string, keep func(rel string) (bool, error)) ([]string, error) {
	var out []string
	for _, rel := range files {
		ok, err := keep(rel)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, rel)
		}
	}
	return out, nil
}
