// Package badlisting has functions that list a directory: some return the entries in directory
// order, which is the filesystem's, and some sort them.
package badlisting

import (
	"io/fs"
	"os"
	"slices"
	"sort"
	"strings"
)

// Entry is one name of a listed directory.
type Entry struct {
	Name string
	Kind int
}

// Dir is a directory.
type Dir struct{ path string }

// List is the originating shape: the entries as the filesystem gives them.
func (d Dir) List() ([]Entry, error) { // want List:"returns a listing in directory order"
	f, err := os.Open(d.path)
	if err != nil {
		return nil, err
	}
	dirents, err := f.ReadDir(-1)
	var out []Entry
	for _, e := range dirents {
		out = append(out, Entry{Name: e.Name()})
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return out, err
}

// Names is the names of the directory: Readdirnames, returned as it came.
func (d Dir) Names() ([]string, error) { // want Names:"returns a listing in directory order"
	f, err := os.Open(d.path)
	if err != nil {
		return nil, err
	}
	names, err := f.Readdirnames(-1)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return names, err
}

// Infos is Readdir, returned as it came.
func (d Dir) Infos() ([]fs.FileInfo, error) { // want Infos:"returns a listing in directory order"
	f, err := os.Open(d.path)
	if err != nil {
		return nil, err
	}
	infos, err := f.Readdir(-1)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return infos, err
}

// Wrapped hands List's entries on as names: a wrapper of a function that returns directory
// order is one too.
func (d Dir) Wrapped() ([]string, error) { // want Wrapped:"returns a listing in directory order"
	entries, err := d.List()
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	return names, err
}

// MustNames is Names without the error: a function that returns only the listing.
func (d Dir) MustNames() []string { // want MustNames:"returns a listing in directory order"
	names, err := d.Names()
	if err != nil {
		panic(err)
	}
	return names
}

// Opened lists through an fs.ReadDirFile, whose ReadDir is as unordered as the file's.
func Opened(f fs.ReadDirFile) ([]fs.DirEntry, error) { // want Opened:"returns a listing in directory order"
	return f.ReadDir(-1)
}

// SortedList is List with the order made: it returns a promise, not the filesystem's order.
func (d Dir) SortedList() ([]Entry, error) {
	entries, err := d.List()
	slices.SortFunc(entries, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	return entries, err
}

// SortedNames sorts through package sort.
func (d Dir) SortedNames() ([]string, error) {
	names, err := d.Names()
	sort.Strings(names)
	return names, err
}

// Sorted is the standard library's own: os.ReadDir sorts by name, so it is not a listing call.
func (d Dir) Sorted() ([]string, error) {
	entries, err := os.ReadDir(d.path)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, err
}

// Count lists and answers a number: no order leaves it.
func (d Dir) Count() (int, error) {
	names, err := d.Names()
	return len(names), err
}

// Set lists and answers a map: an order does not survive it.
func (d Dir) Set() (map[string]bool, error) {
	names, err := d.Names()
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	return set, err
}
