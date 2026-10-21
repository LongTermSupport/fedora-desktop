package seat

import (
	"fmt"
	"slices"
	"strings"
	"syscall"
	"unicode/utf8"
)

// fsRunes is a file name as python decodes it (the file system encoding with
// surrogateescape): an undecodable byte b is the lone surrogate U+DC00+b.
func fsRunes(name string) []rune {
	out := make([]rune, 0, len(name))
	for i := 0; i < len(name); {
		r, size := utf8.DecodeRuneInString(name[i:])
		if r == utf8.RuneError && size == 1 {
			r = 0xDC00 + rune(name[i])
		}
		out = append(out, r)
		i += size
	}
	return out
}

// pathText is a path as v1's messages print it: python's stderr writes the lone surrogate of
// an undecodable byte as \udcXX (backslashreplace) and every other character as itself.
func pathText(path string) string {
	var b strings.Builder
	for i := 0; i < len(path); {
		r, size := utf8.DecodeRuneInString(path[i:])
		if r == utf8.RuneError && size == 1 {
			fmt.Fprintf(&b, `\udc%02x`, path[i])
		} else {
			b.WriteString(path[i : i+size])
		}
		i += size
	}
	return b.String()
}

// sortNames sorts file names as python's sorted() sorts the paths of one directory: by the
// code points of fsRunes.
func sortNames(names []string) {
	slices.SortFunc(names, func(a, b string) int { return slices.Compare(fsRunes(a), fsRunes(b)) })
}

// strerror is the C library's text for errno, which python's OSError.strerror carries: Go's
// table is the same text with a lower-case first letter. Where Go's table has no entry
// ("errno N"), glibc's own text is used.
func strerror(errno syscall.Errno) string {
	if errno == ehwpoison {
		return "Memory page has hardware error"
	}
	text := errno.Error()
	if number, unknown := strings.CutPrefix(text, "errno "); unknown {
		return "Unknown error " + number
	}
	return strings.ToUpper(text[:1]) + text[1:]
}

// ehwpoison is Linux's EHWPOISON, missing from Go's table.
const ehwpoison syscall.Errno = 133
