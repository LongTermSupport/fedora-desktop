package ftpcamera

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The media taxonomy, mirroring the photo library: extension to class (E18, E20).
var mediaClass = map[string]string{extJPG: classPhoto, extJPEG: classPhoto, extARW: classPhoto, "mp4": "video", "mov": "video"}

// classPhoto is the media class whose date is the shutter press (E20).
const classPhoto = "photo"

const (
	photoDir = "photos"
	videoDir = "videos"
)

func classDir(class string) string {
	if class == classPhoto {
		return photoDir
	}
	return videoDir
}

// typeDir is the extension upper-cased, ARW being RAW (E21).
func typeDir(ext string) string {
	if t := strings.ToUpper(ext); t != "ARW" {
		return t
	}
	return "RAW"
}

func extOf(path string) string { return strings.TrimPrefix(filepath.Ext(path), ".") }

// classOf is a file's media class, "" for anything else.
func classOf(path string) string { return mediaClass[strings.ToLower(extOf(path))] }

// exifDate is the capture date as YYYY/MM/DD, or "" (E20). exiftool exits non-zero when none of
// the tags exist, so its status is not the answer; only a run that could not happen is "".
func (c *cli) exifDate(ctx context.Context, path, class string) string {
	tags := []string{"-CreateDate", "-MediaCreateDate"}
	if class == classPhoto {
		tags = []string{"-DateTimeOriginal", "-CreateDate"}
	}
	args := append(append([]string{exifShort, "-d", "%Y/%m/%d"}, tags...), path)
	res, err := c.env.Runner.Run(ctx, execx.Cmd{Name: exiftoolBin, Args: args})
	if err != nil {
		if exitErr, ok := errors.AsType[*execx.ExitError](err); !ok || exitErr == nil {
			return ""
		}
	}
	for line := range strings.SplitSeq(string(res.Stdout), "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}

// sizeText is a file's size in bytes, or "?" when it cannot be read.
func sizeText(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "?"
	}
	return strconv.FormatInt(info.Size(), 10)
}

// isRegular is whether path is a regular file now (an arrival the camera's retry already moved
// is not).
func isRegular(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// moveAsRoot files path into dest, as root (E21). A file of the same name already there is
// never replaced (B24): cameras number their files in a cycle, so it is a different shot.
func (c *cli) moveAsRoot(ctx context.Context, path, dest string) error {
	target := filepath.Join(dest, filepath.Base(path))
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("%s is already in the library — rename one of them, then sort again", c.rel(target))
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("check %s: %w", c.rel(target), err)
	}
	if err := c.asRoot(ctx, "mkdir", "-p", dest); err != nil {
		return err
	}
	return c.asRoot(ctx, "mv", path, dest+"/")
}

// sweepEmptyDirs removes the directories left empty under the upload directory (E22).
func (c *cli) sweepEmptyDirs(ctx context.Context) error {
	res, err := c.rootCapture(ctx, "find", c.upload, "-mindepth", "1", "-type", "d", "-empty", "-delete")
	if err != nil {
		return c.failErr("could not remove the empty directories under %s: %s", c.upload, failureText(res, err))
	}
	return nil
}

// failureText is what a failed command said, or the error when it said nothing.
func failureText(res execx.Result, err error) string {
	if s := strings.TrimSpace(string(res.Stderr)); s != "" {
		return s
	}
	return err.Error()
}

type sortTally struct{ moved, skipped, truncated int }

// sortPass sorts the upload root (E17-E22).
func (c *cli) sortPass(ctx context.Context) error {
	if !isDir(c.upload) {
		return c.failErr("Upload directory not found: %s", c.upload)
	}
	if !c.onPath(exiftoolBin) {
		return c.failErr("exiftool not found — run play-photography.yml first")
	}
	c.out.f("%sSorting uploads by EXIF date...%s\n", c.out.b, c.out.n)
	entries, err := os.ReadDir(c.upload)
	if err != nil {
		return c.failErr("could not read %s: %v", c.upload, err)
	}
	var tally sortTally
	for _, e := range entries {
		if !e.Type().IsRegular() || classOf(e.Name()) == "" {
			continue
		}
		if err := c.sortListed(ctx, filepath.Join(c.upload, e.Name()), &tally); err != nil {
			return err
		}
	}
	if err := c.sweepEmptyDirs(ctx); err != nil {
		return err
	}
	o := c.out
	o.f("\n")
	if tally.truncated > 0 {
		o.f("%s%s%d file(s) were TRUNCATED in transfer and left in place.%s\n", o.r, o.b, tally.truncated, o.n)
		o.f("  These are fragments, not photos — the link dropped the data\n")
		o.f("  connection mid-transfer and the FTP server could not tell that\n")
		o.f("  from a normal end-of-file. They were NOT added to the library.\n\n")
		c.warnWeakLink(fmt.Sprintf("%d file(s) arrived truncated during this session", tally.truncated))
	}
	o.f("%sSort complete:%s %s%d%s moved, %s%d%s skipped\n", o.b, o.n, o.c, tally.moved, o.n, o.c, tally.skipped, o.n)
	return nil
}

func (c *cli) sortListed(ctx context.Context, path string, tally *sortTally) error {
	name := filepath.Base(path)
	if completeness(ctx, c.env.Runner, c.onPath, path) == truncated {
		c.err.f("  %spartial%s  %s  (truncated at %s bytes — left in place)\n", c.err.r, c.err.n, name, sizeText(path))
		tally.truncated++
		return nil
	}
	class := classOf(path)
	date := c.exifDate(ctx, path, class)
	if date == "" {
		c.out.f("  %sskip%s  %s  (no EXIF date)\n", c.out.y, c.out.n, name)
		tally.skipped++
		return nil
	}
	rel := classDir(class) + "/" + date + "/" + typeDir(extOf(path))
	if err := c.moveAsRoot(ctx, path, filepath.Join(c.upload, rel)); err != nil {
		return c.failErr("could not move %s: %v", name, err)
	}
	c.out.f("  %smove%s  %s  → %s/\n", c.out.g, c.out.n, name, rel)
	tally.moved++
	return nil
}
