package bashlimits

import (
	"errors"
	"fmt"
	"io/fs"
)

// Scan is the result of measuring a tree.
type Scan struct {
	Measured map[string]Measurement // every non-exempt bash file
	Exempt   int                    // bash files an Exemption covers
}

// ScanTree measures each bash file among files. A listed file that is gone from disk, or is not
// a regular file (a symlink, a submodule), is not scanned; any other failure stops the scan.
func ScanTree(fsys fs.FS, files []string) (Scan, error) {
	scan := Scan{Measured: map[string]Measurement{}}
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
	if exempt {
		scan.Exempt++
		return nil
	}
	src, err := fs.ReadFile(fsys, p)
	if err != nil {
		return fmt.Errorf("read %s: %w", p, err)
	}
	scan.Measured[p] = Measure(src)
	return nil
}
