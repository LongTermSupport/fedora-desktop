// Package watchlist keeps the watch list (CLAUDE/Watchlist.md): the owner's GitHub stars and the
// repositories added by hand, each cloned in full under its licence, triaged into a study group
// or set aside, and studied in runs that record the commit each study read. It clones, fetches,
// records and checks; judging what is worth adopting is the study agents' work. Spec: Plan 00258,
// SPEC-watchlist-runs.md, whose item numbers (E, S, A, T, R, W, M, U, G) the tests cite.
package watchlist

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// none is the list's mark for an empty column.
const none = "-"

// The enum values of the list's columns.
const (
	sourceStar  = "star"
	sourceAdded = "added"

	reuseLift      = "lift"
	reuseWholeFile = "whole-file"
	reuseCleanRoom = "clean-room"

	stateNew       = "new"
	stateStudy     = "study"
	stateExcluded  = "excluded"
	stateUnstarred = "unstarred"

	licenceNone = "NONE"
)

const header = `# The watch list: the owner's GitHub stars and repositories added by hand (CLAUDE/Watchlist.md).
# Written by the watchlist command (sync, add, set); read strictly, a malformed row is an error.
# One row per repository, sorted by repo, tab-separated:
#
# repo <TAB> source <TAB> licence <TAB> reuse <TAB> state <TAB> group <TAB> licence_file <TAB> licence_sha
#   repo          <owner>/<repo> on github.com
#   source        star (from the owner's stars) or added (by watchlist add)
#   licence       SPDX id, LicenseRef-<name>, NONE, or NOASSERTION (not yet verified)
#   reuse         derived from licence: lift (MIT, Apache-2.0, ISC, 0BSD, BSD-*),
#                 whole-file (MPL-2.0), clean-room (everything else)
#   state         new (to triage), study, excluded (set aside) or unstarred
#   group         the study group, lower-case kebab; - only when state is not study
#   licence_file  the licence file at the clone's root, or -
#   licence_sha   the first 16 hex digits of that file's sha256, or -
`

var (
	repoPattern        = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)
	licencePattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+-]*$`)
	groupPattern       = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	licenceFilePattern = regexp.MustCompile(`(?i)^(licen[cs]e|copying)`)
	licenceSHAPattern  = regexp.MustCompile(`^[0-9a-f]{16}$`)
	sources            = []string{sourceStar, sourceAdded}
	states             = []string{stateNew, stateStudy, stateExcluded, stateUnstarred}
)

// Entry is one repository on the list. Every field holds the column's text, "-" included.
type Entry struct {
	Repo        string
	Source      string
	Licence     string
	Reuse       string
	State       string
	Group       string
	LicenceFile string
	LicenceSHA  string
}

// ListPath is the tracked list file under root.
func ListPath(root string) string { return filepath.Join(root, "CLAUDE", "Watchlist.tsv") }

// CloneDir is where the clone of the repository lives under root: the excluded tree for an
// excluded row, the starred tree otherwise, by licence then owner and repository.
func (e Entry) CloneDir(root string) string {
	tree := "starred-repos"
	if e.State == stateExcluded {
		tree = "starred-repos-excluded"
	}
	return filepath.Join(root, "untracked", tree, e.Licence, filepath.FromSlash(e.Repo))
}

// Reuse is what a licence allows (E5).
func Reuse(licence string) string {
	switch {
	case licence == "MIT", licence == "Apache-2.0", licence == "ISC", licence == "0BSD", strings.HasPrefix(licence, "BSD-"):
		return reuseLift
	case licence == "MPL-2.0":
		return reuseWholeFile
	default:
		return reuseCleanRoom
	}
}

func checkRepo(repo string) error {
	if !repoPattern.MatchString(repo) || strings.HasSuffix(repo, ".git") {
		return fmt.Errorf("repo %q is not <owner>/<repo>", repo)
	}
	return nil
}

func checkLicence(licence string) error {
	if !licencePattern.MatchString(licence) {
		return fmt.Errorf("licence %q is not an SPDX id, LicenseRef-<name>, NONE or NOASSERTION", licence)
	}
	return nil
}

func checkGroup(group string) error {
	if group != none && !groupPattern.MatchString(group) {
		return fmt.Errorf("group %q is not a lower-case kebab name or -", group)
	}
	return nil
}

// check is E1 to E9 for one entry.
func (e Entry) check() error {
	var errs []error
	errs = append(errs, checkRepo(e.Repo))
	if !slices.Contains(sources, e.Source) {
		errs = append(errs, fmt.Errorf("source %q is not star or added", e.Source))
	}
	if err := checkLicence(e.Licence); err != nil {
		errs = append(errs, err)
	} else if want := Reuse(e.Licence); e.Reuse != want {
		errs = append(errs, fmt.Errorf("reuse %q is inconsistent with licence %s, which is %s", e.Reuse, e.Licence, want))
	}
	if !slices.Contains(states, e.State) {
		errs = append(errs, fmt.Errorf("state %q is not new, study, excluded or unstarred", e.State))
	}
	errs = append(errs, checkGroup(e.Group))
	if e.State == stateStudy && e.Group == none {
		errs = append(errs, errors.New("a study row needs a group"))
	}
	if (e.LicenceFile == none) != (e.LicenceSHA == none) {
		errs = append(errs, errors.New("licence_file and licence_sha must both be - or both be set"))
	}
	if e.LicenceFile != none && (!licenceFilePattern.MatchString(e.LicenceFile) || strings.ContainsAny(e.LicenceFile, "/\\")) {
		errs = append(errs, fmt.Errorf("licence_file %q is not a licence file name at the clone's root", e.LicenceFile))
	}
	if e.LicenceSHA != none && !licenceSHAPattern.MatchString(e.LicenceSHA) {
		errs = append(errs, fmt.Errorf("licence_sha %q is not 16 lowercase hex digits", e.LicenceSHA))
	}
	return errors.Join(errs...)
}

// Parse reads the list text strictly; file names it in the errors.
func Parse(data []byte, file string) ([]Entry, error) {
	var entries []Entry
	seen := map[string]int{}
	for i, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			return nil, fmt.Errorf("%s:%d: want 8 tab-separated fields (repo, source, licence, reuse, state, group, licence_file, licence_sha), got %d", file, i+1, len(f))
		}
		e := Entry{Repo: f[0], Source: f[1], Licence: f[2], Reuse: f[3], State: f[4], Group: f[5], LicenceFile: f[6], LicenceSHA: f[7]}
		if err := e.check(); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", file, i+1, err)
		}
		key := strings.ToLower(e.Repo)
		if first, dup := seen[key]; dup {
			return nil, fmt.Errorf("%s:%d: %s is already listed on line %d", file, i+1, e.Repo, first)
		}
		seen[key] = i + 1
		entries = append(entries, e)
	}
	return entries, nil
}

// Format is the list file text: the header, then one row per entry, sorted by repo.
func Format(entries []Entry) []byte {
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, func(a, b Entry) int { return strings.Compare(a.Repo, b.Repo) })
	var b strings.Builder
	b.WriteString(header)
	for _, e := range sorted {
		b.WriteString(strings.Join([]string{e.Repo, e.Source, e.Licence, e.Reuse, e.State, e.Group, e.LicenceFile, e.LicenceSHA}, "\t") + "\n")
	}
	return []byte(b.String())
}

// LicenceOf is the licence file of the clone at dir and the first 16 hex digits of its sha256
// (E8, E9): the first regular file at the root, by name, whose name starts licence, license or
// copying in any case; "-" and "-" when there is none. A directory such as LICENSES/ is not it.
func LicenceOf(dir string) (string, string, error) {
	names, err := os.ReadDir(dir)
	if err != nil {
		return "", "", fmt.Errorf("cannot list the clone %s: %w", dir, err)
	}
	for _, n := range names {
		if !licenceFilePattern.MatchString(n.Name()) {
			continue
		}
		path := filepath.Join(dir, n.Name())
		info, statErr := os.Stat(path)
		if statErr != nil {
			return "", "", fmt.Errorf("cannot read the licence file: %w", statErr)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		data, readErr := os.ReadFile(filepath.Clean(path))
		if readErr != nil {
			return "", "", fmt.Errorf("cannot read the licence file: %w", readErr)
		}
		sum := sha256.Sum256(data)
		return n.Name(), hex.EncodeToString(sum[:])[:16], nil
	}
	return none, none, nil
}
