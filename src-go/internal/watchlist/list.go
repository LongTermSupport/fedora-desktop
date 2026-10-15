// Package watchlist keeps the state of the repository watch list (CLAUDE/Watchlist.md): which
// public repositories are watched, the commit last reviewed in each and the run that recorded
// it, and a local mirror of each so a reviewer can read what changed. It reports and records;
// judging what is worth adopting is the watchlist-scout agent's work. Spec: Plan 00200,
// SPEC-watchlist.md, whose numbers the tests cite.
package watchlist

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// none is the list file's mark for "never reviewed" in the sha and run columns.
const none = "-"

const header = `# The watch list: public repositories scanned at intervals for ideas (CLAUDE/Watchlist.md).
# Edited by the watchlist command (add, mark); read strictly, a malformed row is an error.
#
# url <TAB> branch <TAB> last_sha <TAB> run
#   url       https://github.com/<owner>/<repo>, no .git suffix
#   branch    the branch that is reviewed (the remote's default branch when added)
#   last_sha  the commit last reviewed (40 hex digits), or - when never reviewed
#   run       the run or document that recorded last_sha (no spaces), or - with it
`

var (
	urlPattern = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+)$`)
	shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	runPattern = regexp.MustCompile(`^\S+$`)
)

// Entry is one watched repository. LastSHA and Run are empty until it has been reviewed.
type Entry struct {
	URL     string
	Branch  string
	LastSHA string
	Run     string
}

// Name is owner/repo, as the list and the reports spell it.
func (e Entry) Name() string {
	owner, repo := splitURL(e.URL)
	return owner + "/" + repo
}

// MirrorDir is where the local clone of the repository lives under root.
func (e Entry) MirrorDir(root string) string {
	owner, repo := splitURL(e.URL)
	return filepath.Join(root, "untracked", "watchlist", owner+"__"+repo)
}

// ListPath is the tracked list file under root.
func ListPath(root string) string { return filepath.Join(root, "CLAUDE", "Watchlist.tsv") }

func splitURL(url string) (string, string) {
	m := urlPattern.FindStringSubmatch(url)
	if m == nil {
		return "", ""
	}
	return m[1], m[2]
}

// validateURL is W5: an https URL naming exactly an owner and a repository.
func validateURL(url string) error {
	m := urlPattern.FindStringSubmatch(url)
	if m == nil || strings.HasSuffix(m[2], ".git") {
		return fmt.Errorf("%q is not an https://github.com/<owner>/<repo> URL (no .git suffix, no slash, query or credentials)", url)
	}
	return nil
}

// Parse reads the list text; file names it in the errors (W4 to W8).
func Parse(data []byte, file string) ([]Entry, error) {
	var entries []Entry
	seen := map[string]int{}
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		e, err := parseRow(line)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", file, i+1, err)
		}
		key := strings.ToLower(e.Name())
		if first, dup := seen[key]; dup {
			return nil, fmt.Errorf("%s:%d: %s is already listed on line %d", file, i+1, e.Name(), first)
		}
		seen[key] = i + 1
		entries = append(entries, e)
	}
	return entries, nil
}

func parseRow(line string) (Entry, error) {
	f := strings.Split(line, "\t")
	if len(f) != 4 {
		return Entry{}, fmt.Errorf("want 4 tab-separated fields (url, branch, last_sha, run), got %d", len(f))
	}
	if err := validateURL(f[0]); err != nil {
		return Entry{}, err
	}
	if f[1] == "" || strings.ContainsAny(f[1], " \t") {
		return Entry{}, fmt.Errorf("branch %q must be non-empty and hold no whitespace", f[1])
	}
	if (f[2] == none) != (f[3] == none) {
		return Entry{}, errors.New("last_sha and run must both be - or both be set")
	}
	e := Entry{URL: f[0], Branch: f[1]}
	if f[2] == none {
		return e, nil
	}
	if !shaPattern.MatchString(f[2]) {
		return Entry{}, fmt.Errorf("last_sha %q is not 40 lowercase hex digits or -", f[2])
	}
	if !runPattern.MatchString(f[3]) {
		return Entry{}, fmt.Errorf("run %q must be a single token with no whitespace", f[3])
	}
	e.LastSHA, e.Run = f[2], f[3]
	return e, nil
}

// Format is the list file text for entries: the header, then one row each, in order (W9).
func Format(entries []Entry) []byte {
	var b strings.Builder
	b.WriteString(header)
	for _, e := range entries {
		sha, run := none, none
		if e.LastSHA != "" {
			sha, run = e.LastSHA, e.Run
		}
		b.WriteString(strings.Join([]string{e.URL, e.Branch, sha, run}, "\t") + "\n")
	}
	return []byte(b.String())
}
