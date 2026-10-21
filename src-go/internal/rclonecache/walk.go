package rclonecache

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"
)

// coverage is how much of a directory inside a mount its cache mirror holds (E17).
type coverage struct {
	totalFiles, cachedFiles int64
	totalSize, cachedSize   int64
	top                     []file // the largest uncached files, at most topN
	unreadable              []error
}

type file struct {
	size int64
	path string
}

const topN = 5

// analyse walks dir, without following symlinks, against its mirror under cache. An entry it
// cannot read is recorded and the walk goes on (E19).
func analyse(dir, cache string) coverage {
	var cov coverage
	var uncached []file
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			cov.unreadable = append(cov.unreadable, err)
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			cov.unreadable = append(cov.unreadable, fmt.Errorf("%s: %w", path, err))
			return nil
		}
		cov.totalFiles++
		cov.totalSize += info.Size()
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		mirror := filepath.Join(cache, rel)
		size, cached, err := mirrorSize(mirror)
		switch {
		case err != nil:
			cov.unreadable = append(cov.unreadable, err)
		case cached:
			cov.cachedFiles++
			cov.cachedSize += size
		default:
			uncached = append(uncached, file{size: info.Size(), path: path})
		}
		return nil
	})
	if walkErr != nil {
		cov.unreadable = append(cov.unreadable, walkErr)
	}
	slices.SortFunc(uncached, func(a, b file) int {
		return cmp.Or(cmp.Compare(b.size, a.size), cmp.Compare(a.path, b.path))
	})
	cov.top = uncached[:min(topN, len(uncached))]
	return cov
}

// mirrorSize is the lstat size of a mirror that exists (symlinks followed); a mirror that is
// absent, or below something that is not a directory, is not cached.
func mirrorSize(mirror string) (int64, bool, error) {
	if _, err := os.Stat(mirror); err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return 0, false, nil
		}
		return 0, false, err
	}
	info, err := os.Lstat(mirror)
	if err != nil {
		return 0, false, err
	}
	return info.Size(), true, nil
}
