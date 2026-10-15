package ftpcamera

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// verdict is what the completeness check says of a received file. vsftpd's OK UPLOAD only
// means the data connection closed, which a dropped link also does, so every file is checked
// before it is sorted or copied.
type verdict int

const (
	complete verdict = iota
	truncated
	unverifiable
)

func (v verdict) String() string {
	return [...]string{"complete", "truncated", "unverifiable"}[v]
}

// jpegTail is how far from the end an EOI marker may sit behind padding (E14).
const jpegTail = 64

var truncationWords = regexp.MustCompile(`(?i)truncat|premature|unexpected end|missing .*end of`)

// completeness checks one file (E13-E16). onPath says whether a program is installed.
func completeness(ctx context.Context, r execx.Runner, onPath func(string) bool, path string) verdict {
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return truncated
	}
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(path), ".")) {
	case extJPG, extJPEG:
		return jpegComplete(path, info.Size())
	case extARW, "mp4", "mov":
		if !onPath(exiftoolBin) {
			return unverifiable
		}
		return exifComplete(ctx, r, path)
	}
	return unverifiable
}

func jpegComplete(path string, size int64) verdict {
	f, err := os.Open(path)
	if err != nil {
		return truncated
	}
	defer closeQuietly(f)
	head := make([]byte, 2)
	if _, err := io.ReadFull(f, head); err != nil || head[0] != 0xFF || head[1] != 0xD8 {
		return truncated
	}
	start := max(size-jpegTail, 0)
	tail := make([]byte, size-start)
	if _, err := f.ReadAt(tail, start); err != nil {
		return truncated
	}
	if bytes.Contains(tail, []byte{0xFF, 0xD9}) {
		return complete
	}
	return truncated
}

// closeQuietly closes a file only read from: nothing was written that a failed close could lose.
func closeQuietly(f *os.File) {
	if err := f.Close(); err != nil {
		return
	}
}

func exifComplete(ctx context.Context, r execx.Runner, path string) verdict {
	res, err := r.Run(ctx, execx.Cmd{Name: exiftoolBin, Args: []string{exifShort, "-a", "-FileSize#", "-StripOffsets", "-StripByteCounts", path}})
	if err == nil && stripsPastEnd(string(res.Stdout)) {
		return truncated
	}
	out, err := execx.Combined(ctx, r, execx.Cmd{Name: exiftoolBin, Args: []string{"-validate", "-warning", "-error", "-a", exifShort, path}})
	if err != nil {
		return unverifiable
	}
	if truncationWords.MatchString(out) {
		return truncated
	}
	return complete
}

// stripsPastEnd reads exiftool's file size, strip offsets and strip byte counts (E15): any
// strip that ends past the file proves bytes are missing.
func stripsPastEnd(out string) bool {
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	var size float64
	var offsets, lengths []float64
	for i, line := range lines {
		if i == 0 {
			if f := strings.Fields(line); len(f) > 0 {
				size = awkNumber(f[0])
			}
			continue
		}
		nums := numbers(line)
		if len(offsets) == 0 {
			offsets = nums
		} else {
			lengths = nums
		}
	}
	if size == 0 || len(offsets) == 0 || len(lengths) == 0 {
		return false
	}
	for i := 0; i < len(offsets) && i < len(lengths); i++ {
		if offsets[i]+lengths[i] > size {
			return true
		}
	}
	return false
}

func numbers(line string) []float64 {
	fields := strings.Fields(strings.ReplaceAll(line, ",", " "))
	nums := make([]float64, 0, len(fields))
	for _, f := range fields {
		nums = append(nums, awkNumber(f))
	}
	return nums
}

var numericPrefix = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?`)

// awkNumber is a field as awk's "+0" reads it: its numeric prefix, or 0.
func awkNumber(s string) float64 {
	n, err := strconv.ParseFloat(numericPrefix.FindString(s), 64)
	if err != nil {
		return 0
	}
	return n
}
