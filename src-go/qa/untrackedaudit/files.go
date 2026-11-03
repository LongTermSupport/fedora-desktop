package untrackedaudit

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"
)

// untrackedDir is the ignored scratch tree the file audit walks.
const untrackedDir = "untracked"

// Where each kind of file belongs instead (the asset-home table of Plan 00245's design).
const (
	homeAsset    = "a plan's RESEARCH-*.md or DESIGN-*.md, a subagent-reports/ file, or an anonymised GitHub issue; then delete this copy"
	homePatch    = "a commit on a pushed branch (a WIP commit is fine); never a patch file"
	homeDecision = "a GitHub issue (anonymised) or a plan's decision section; never a sheet in untracked/"
	homeUnshaped = "a tracked home (a plan file, a GitHub issue) if it is worth keeping, else delete it"
)

// walker collects what the walk of untracked/ finds.
type walker struct {
	fsys     fs.FS
	pol      Policy
	now      time.Time
	findings []Finding
	oldInDae map[string]int
}

// auditFiles walks untracked/ and returns the findings and the daemon-store warnings.
func auditFiles(fsys fs.FS, pol Policy, now time.Time) ([]Finding, []Warning, error) {
	w := &walker{fsys: fsys, pol: pol, now: now, oldInDae: map[string]int{}}
	if err := fs.WalkDir(fsys, untrackedDir, w.visit); err != nil {
		return nil, nil, err
	}
	return w.findings, storeWarnings(pol, w.oldInDae), nil
}

func (w *walker) visit(p string, d fs.DirEntry, err error) error {
	if err != nil {
		if p == untrackedDir && errors.Is(err, fs.ErrNotExist) {
			return fs.SkipAll
		}
		return fmt.Errorf("cannot read %s: %w", p, err)
	}
	top, _, nested := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(p, untrackedDir), "/"), "/")
	if d.IsDir() {
		if p != untrackedDir && !nested && slices.Contains(w.pol.Stores, top) {
			return fs.SkipDir
		}
		return nil
	}
	info, err := d.Info()
	if err != nil {
		return fmt.Errorf("cannot stat %s: %w", p, err)
	}
	age := w.now.Sub(info.ModTime())
	if age <= w.pol.floor() {
		return nil
	}
	w.judge(p, top, nested, age)
	return nil
}

// judge records an old file: a daemon store's file as a count, any other as a finding.
func (w *walker) judge(p, top string, nested bool, age time.Duration) {
	if nested && slices.Contains(w.pol.DaemonStores, top) {
		w.oldInDae[top]++
		return
	}
	if f, found := judgeFile(w.pol, p, age); found {
		w.findings = append(w.findings, f)
	}
}

// judgeFile classifies one file already known to be older than the floor.
func judgeFile(pol Policy, p string, age time.Duration) (Finding, bool) {
	base := path.Base(p)
	ext := strings.ToLower(path.Ext(base))
	switch {
	case pol.isAssetExt(base) && (ext == ".patch" || ext == ".diff"):
		return Finding{Kind: KindPatch, Subject: p, Age: age, Home: homePatch}, true
	case pol.isAssetExt(base) && strings.Contains(strings.ToLower(base), "decision"):
		return Finding{Kind: KindDecision, Subject: p, Age: age, Home: homeDecision}, true
	case pol.isAssetExt(base):
		return Finding{Kind: KindAsset, Subject: p, Age: age, Home: homeAsset}, true
	case pol.isScratchShape(base):
		return Finding{}, false
	default:
		return Finding{Kind: KindUnshaped, Subject: p, Age: age, Home: homeUnshaped}, true
	}
}

func storeWarnings(pol Policy, counts map[string]int) []Warning {
	var out []Warning
	for _, name := range pol.DaemonStores {
		if n := counts[name]; n > 0 {
			out = append(out, Warning{Store: untrackedDir + "/" + name, Count: n})
		}
	}
	return out
}
