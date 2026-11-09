package ftpcamera

import (
	"bufio"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// orphans are the RAW files under photos/ whose JPG sibling is gone (E37), in walk order.
func orphans(photos string) ([]string, error) {
	var found []string
	err := filepath.WalkDir(photos, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || d.Name() != "RAW" {
			return nil
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.Type().IsRegular() && strings.EqualFold(extOf(e.Name()), extARW) && !hasJPG(filepath.Dir(path), e.Name()) {
				found = append(found, filepath.Join(path, e.Name()))
			}
		}
		return nil
	})
	return found, err
}

// hasJPG is whether dateDir/JPG or dateDir/JPEG holds <stem>.jpg or <stem>.jpeg, any case.
func hasJPG(dateDir, raw string) bool {
	stem := strings.TrimSuffix(raw, filepath.Ext(raw))
	for _, sibling := range []string{"JPG", "JPEG"} {
		entries, err := os.ReadDir(filepath.Join(dateDir, sibling))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.Type().IsRegular() && (strings.EqualFold(e.Name(), stem+".jpg") || strings.EqualFold(e.Name(), stem+".jpeg")) {
				return true
			}
		}
	}
	return false
}

// prune lists the orphan RAWs, asks, and removes them as root (E36-E40).
func (c *cli) prune(ctx context.Context) error {
	if !isDir(c.upload) {
		return c.failErr("Upload directory not found: %s", c.upload)
	}
	o := c.out
	photos := filepath.Join(c.upload, photoDir)
	if !isDir(photos) {
		o.f("%sNo %s/ subtree — nothing to prune.%s\n", o.g, photoDir, o.n)
		return nil
	}
	o.f("%sScanning for orphan RAW files...%s\n", o.b, o.n)
	found, err := orphans(photos)
	if err != nil {
		return c.failErr("could not scan %s: %v", photos, err)
	}
	if len(found) == 0 {
		o.f("%sNo orphan RAW files found.%s\n", o.g, o.n)
		return nil
	}
	var total int64
	for _, f := range found {
		info, err := os.Stat(f)
		if err != nil {
			return c.failErr("could not read %s: %v", f, err)
		}
		total += info.Size()
	}
	size := iec(total, true) + "B"
	o.f("\n%sFound %s%d%s%s orphan RAW file(s) (%s%s%s total):%s\n", o.b, o.c, len(found), o.n, o.b, o.c, size, o.b, o.n)
	for _, f := range found {
		o.f("  %s%s%s\n", o.y, c.rel(f), o.n)
	}
	o.f("\n")
	yes, answerErr := c.confirmDelete()
	if answerErr != nil {
		return c.failErr("could not read the answer: %v", answerErr)
	}
	if !yes {
		o.f("%sAborted — no files removed.%s\n", o.y, o.n)
		return nil
	}
	removed := 0
	for _, f := range found {
		if err := c.asRoot(ctx, "rm", "-f", f); err != nil {
			c.err.f("  %sfailed%s  %s  (%v)\n", c.err.r, c.err.n, c.rel(f), howEnded(err))
			continue
		}
		removed++
	}
	if err := c.sweepEmptyDirs(ctx); err != nil {
		return err
	}
	o.f("\n%sPrune complete:%s %s%d%s of %s%d%s files removed (%s%s%s freed)\n", o.b, o.n, o.c, removed, o.n, o.c, len(found), o.n, o.c, size, o.n)
	return nil
}

// rel is a path under the upload directory, relative to it.
func (c *cli) rel(path string) string { return strings.TrimPrefix(path, c.upload+"/") }

// confirmDelete asks the [y/N] question on a terminal and reads one line; only y or Y is yes,
// and end of input is no (E39). The answer is typed with the terminal's echo on; a read that
// fails otherwise, or an echo that cannot be given back, is the error.
func (c *cli) confirmDelete() (bool, error) {
	if c.env.IsTerminal(c.env.Stdin) {
		c.err.f("Delete these files? [y/N] ")
	}
	var line string
	err := c.env.Echo.Given(func() error {
		var readErr error
		line, readErr = bufio.NewReader(c.env.Stdin).ReadString('\n')
		return readErr
	})
	switch {
	case errors.Is(err, io.EOF):
		c.err.f("\n")
		return false, nil
	case err != nil:
		return false, err
	}
	reply := strings.TrimSuffix(line, "\n")
	return reply == "y" || reply == "Y", nil
}
