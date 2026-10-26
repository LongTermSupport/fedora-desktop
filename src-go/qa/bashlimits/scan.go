package bashlimits

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
)

// Scan is the result of measuring a tree.
type Scan struct {
	Measured map[string]Measurement // every non-exempt bash file
	Exempt   map[string]bool        // bash files an Exemption covers
	Present  map[string]bool        // every regular file that was looked at, bash or not
	// DaemonOwned are the exempt files of DaemonOwnedFiles; VerifyDaemonOwned keeps only
	// those an upgrade shipped.
	DaemonOwned map[string]bool
	// Judged are the bash files the rule assignment-masks-substitution-failure read (every one
	// but the Unjudged); Masked holds its findings in those files.
	Judged map[string]bool
	Masked map[string][]MaskedSubstitution
}

// ScanTree measures each bash file among files. A listed file that is gone from disk, or is not
// a regular file (a symlink, a submodule), is not scanned; any other failure stops the scan.
func ScanTree(fsys fs.FS, files []string) (Scan, error) {
	scan := Scan{
		Measured: map[string]Measurement{}, Exempt: map[string]bool{}, Present: map[string]bool{},
		DaemonOwned: map[string]bool{}, Judged: map[string]bool{}, Masked: map[string][]MaskedSubstitution{},
	}
	for _, p := range files {
		info, err := fs.Lstat(fsys, p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return Scan{}, fmt.Errorf("stat %s: %w", p, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		scan.Present[p] = true
		if err := scanFile(fsys, p, &scan); err != nil {
			return Scan{}, err
		}
	}
	return scan, nil
}

func scanFile(fsys fs.FS, p string, scan *Scan) error {
	bash, err := IsCandidate(fsys, p)
	if err != nil || !bash {
		return err
	}
	_, exempt, err := ExemptionFor(p)
	if err != nil {
		return err
	}
	unjudged, err := IsUnjudged(p)
	if err != nil {
		return err
	}
	if exempt {
		scan.Exempt[p] = true
		if IsDaemonOwnedFile(p) {
			scan.DaemonOwned[p] = true
		}
	}
	if unjudged {
		return nil
	}
	src, err := fs.ReadFile(fsys, p)
	if err != nil {
		return fmt.Errorf("read %s: %w", p, err)
	}
	measure, judge := Measure, MaskedSubstitutions
	if path.Ext(p) == jinjaExt {
		measure, judge = MeasureTemplate, MaskedSubstitutionsTemplate
	}
	masked, err := judge(src)
	if err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	scan.Judged[p] = true
	if len(masked) > 0 {
		scan.Masked[p] = masked
	}
	if exempt {
		return nil
	}
	measured, err := measure(src)
	if err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	scan.Measured[p] = measured
	return nil
}
