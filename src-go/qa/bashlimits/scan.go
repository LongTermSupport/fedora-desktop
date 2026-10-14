package bashlimits

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// Scan is the result of measuring a tree.
type Scan struct {
	Measured map[string]Measurement     // every non-exempt bash file
	Exempt   map[string]bool            // bash files an Exemption covers
	Present  map[string]bool            // every regular file that was looked at, bash or not
	Suites   map[string]map[string]bool // Go command -> the suites scripts/go-binary-suites.tsv gives it
}

// suitesTable is the file that says which suites drive each deployed Go command.
const suitesTable = "scripts/go-binary-suites.tsv"

// ScanTree measures each bash file among files. A listed file that is gone from disk, or is not
// a regular file (a symlink, a submodule), is not scanned; any other failure stops the scan.
func ScanTree(fsys fs.FS, files []string) (Scan, error) {
	scan := Scan{Measured: map[string]Measurement{}, Exempt: map[string]bool{}, Present: map[string]bool{}}
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
	if scan.Present[suitesTable] {
		suites, err := readSuites(fsys)
		if err != nil {
			return Scan{}, err
		}
		scan.Suites = suites
	}
	return scan, nil
}

// readSuites parses the suites table: "command<TAB>suite suite ...", with # comments.
func readSuites(fsys fs.FS) (map[string]map[string]bool, error) {
	raw, err := fs.ReadFile(fsys, suitesTable)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", suitesTable, err)
	}
	suites := map[string]map[string]bool{}
	for line := range strings.SplitSeq(string(raw), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		command, list, found := strings.Cut(line, "\t")
		if !found {
			return nil, fmt.Errorf("%s: %q has no tab between the command and its suites", suitesTable, line)
		}
		suites[command] = map[string]bool{}
		for suite := range strings.FieldsSeq(list) {
			suites[command][suite] = true
		}
	}
	return suites, nil
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
		scan.Exempt[p] = true
		return nil
	}
	src, err := fs.ReadFile(fsys, p)
	if err != nil {
		return fmt.Errorf("read %s: %w", p, err)
	}
	measure := Measure
	if path.Ext(p) == jinjaExt {
		measure = MeasureTemplate
	}
	measured, err := measure(src)
	if err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	scan.Measured[p] = measured
	return nil
}
