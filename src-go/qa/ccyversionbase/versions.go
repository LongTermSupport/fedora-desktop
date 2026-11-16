package ccyversionbase

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// versioned is a file holding a version: its path and the start of the line the version is
// quoted on. The first such line is the version, as the hook's awk reads it.
type versioned struct{ path, key string }

// versionFiles are the two versions a ccy change bumps: the launcher's and the container's.
var versionFiles = []versioned{
	{"files/var/local/claude-yolo/claude-yolo", "CCY_VERSION="},
	{"files/var/local/claude-yolo/Dockerfile", "LABEL claude-yolo-version="},
}

// Behind lists each version the staged tree holds lower than a parent's (B7), one line each.
func Behind(ctx context.Context, env Env, parents []string) ([]string, error) {
	var behind []string
	for _, f := range versionFiles {
		staged, err := version(ctx, env, "", f)
		switch {
		case err != nil:
			return nil, err
		case staged == nil:
			continue
		}
		for _, parent := range parents {
			theirs, err := version(ctx, env, parent, f)
			if err != nil {
				return nil, err
			}
			if theirs != nil && compare(staged, theirs) < 0 {
				behind = append(behind, fmt.Sprintf("%s %s in the merge is below %s in parent %s (%s)",
					strings.TrimSuffix(lastWord(f.key), "="), dotted(staged), dotted(theirs), parent, f.path))
			}
		}
	}
	return behind, nil
}

// version is f's version at rev ("" is the index), or nil when rev has no such file or line.
func version(ctx context.Context, env Env, rev string, f versioned) ([]int, error) {
	blob, found, err := revParse(ctx, env, "--verify", "--quiet", rev+":"+f.path)
	if err != nil || !found {
		return nil, err
	}
	res, err := git(ctx, env, "cat-file", "blob", blob)
	switch {
	case err != nil:
		return nil, err
	case res.Code != 0:
		return nil, fmt.Errorf("git cat-file blob %s (%s:%s): exit %d: %s", blob, rev, f.path, res.Code, strings.TrimSpace(res.Stderr))
	}
	for line := range strings.Lines(res.Stdout) {
		if rest, ok := strings.CutPrefix(line, f.key); ok {
			fields := strings.Split(rest, `"`)
			if len(fields) < 3 {
				return nil, fmt.Errorf("%s:%s: %q quotes no version", rev, f.path, strings.TrimSpace(line))
			}
			return parse(fields[1], rev, f.path)
		}
	}
	return nil, nil
}

// parse reads a dotted version of numbers; anything else cannot be ordered.
func parse(text, rev, path string) ([]int, error) {
	parts := strings.Split(text, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("%s:%s: version %q is not dotted numbers, so it cannot be ordered", rev, path, text)
		}
		nums = append(nums, n)
	}
	return nums, nil
}

// compare orders two versions, a missing part reading as 0 (3.122 is 3.122.0).
func compare(a, b []int) int {
	for i := range max(len(a), len(b)) {
		x, y := part(a, i), part(b, i)
		if x != y {
			return x - y
		}
	}
	return 0
}

func part(v []int, i int) int {
	if i < len(v) {
		return v[i]
	}
	return 0
}

func dotted(v []int) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}

func lastWord(s string) string { return s[strings.LastIndex(s, " ")+1:] }
