// Package badwalk is the fixture of project_walk_open: directory walks whose entries are opened,
// with and without a regular-file check guarding the open.
package badwalk

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var errNotRegular = errors.New("not a regular file")

// The shape the issue found most: a listing, a suffix filter, a read.
func listedThenRead(dir string) ([][]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name())) // want `os.ReadFile opens an entry a directory walk found`
		if err != nil {
			return nil, err
		}
		out = append(out, data)
	}
	return out, nil
}

func globbedThenOpened(pattern string) error {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	for _, m := range matches {
		f, err := os.Open(m) // want `os.Open opens an entry a directory walk found`
		if err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	return nil
}

// `!d.IsDir()` is no check: a named pipe is not a directory.
func walkedNotADirectory(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		_, err = os.ReadFile(path) // want `os.ReadFile opens an entry a directory walk found`
		return err
	})
}

func walkedFS(fsys fs.FS) error {
	return fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		_, err = fs.ReadFile(fsys, p) // want `fs.ReadFile opens an entry a directory walk found`
		return err
	})
}

func walkedWithInfo(root string) error {
	return filepath.Walk(root, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		_, err = os.Open(path) // want `os.Open opens an entry a directory walk found`
		return err
	})
}

// readSpec opens what it is given, so a call of it with a walked name opens that name.
func readSpec(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func listedThenReadByAHelper(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := readSpec(filepath.Join(dir, e.Name())); err != nil { // want `readSpec opens an entry a directory walk found`
			return err
		}
	}
	return nil
}

// gathered collects walked names, and the open comes after the walk.
func gathered(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var paths []string
	for _, e := range entries {
		paths = append(paths, filepath.Join(dir, e.Name()))
	}
	for _, p := range paths {
		if _, err := os.ReadFile(p); err != nil { // want `os.ReadFile opens an entry a directory walk found`
			return err
		}
	}
	return nil
}

func parsedFromAListing(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if _, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0); err != nil { // want `parser.ParseFile opens an entry a directory walk found`
			return err
		}
	}
	return nil
}

// The failed check only warns, then falls through to the open.
func skipAndWarn(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			log.Printf("%s is not a regular file", e.Name())
		}
		if _, err := os.ReadFile(filepath.Join(dir, e.Name())); err != nil { // want `os.ReadFile opens an entry a directory walk found`
			return err
		}
	}
	return nil
}

// A check of something else guards nothing.
func checkOfAnotherName(dir string, other fs.FileInfo) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !other.Mode().IsRegular() {
			continue
		}
		if _, err := os.ReadFile(filepath.Join(dir, e.Name())); err != nil { // want `os.ReadFile opens an entry a directory walk found`
			return err
		}
	}
	return nil
}

// A check joined by || guards nothing either.
func checkJoinedByOr(dir string, force bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if force || e.Type().IsRegular() {
			if _, err := os.ReadFile(filepath.Join(dir, e.Name())); err != nil { // want `os.ReadFile opens an entry a directory walk found`
				return err
			}
		}
	}
	return nil
}

func globbedFS(fsys fs.FS) error {
	matches, err := fs.Glob(fsys, "*.yml")
	if err != nil {
		return err
	}
	for _, m := range matches {
		f, err := fsys.Open(m) // want `Open opens an entry a directory walk found`
		if err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	return nil
}

func readdirnames(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, n := range names {
		if _, err := os.ReadFile(filepath.Join(dir, n)); err != nil { // want `os.ReadFile opens an entry a directory walk found`
			return err
		}
	}
	return nil
}

// A closure over a walked name opens it too.
func inAClosure(dir string, each func(func() error) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := each(func() error {
			_, err := os.ReadFile(filepath.Join(dir, e.Name())) // want `os.ReadFile opens an entry a directory walk found`
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// A clause of a tagless switch that lists a check beside another condition runs when either
// holds, so the check guards nothing.
func caseListedBesideAnother(root string, force bool) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case d.Type().IsRegular(), force:
			_, err = os.ReadFile(p) // want `os.ReadFile opens an entry a directory walk found`
		}
		return err
	})
}

// A switch with a tag compares values; its clauses are not checks.
func taggedSwitch(root string, regular bool) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch regular {
		case d.Type().IsRegular():
			_, err = os.ReadFile(p) // want `os.ReadFile opens an entry a directory walk found`
		}
		return err
	})
}

// --- Clean: the check guards the open. ---

// selfWalking opens what its own walk found, and is found for that where it opens; a name passed
// to it is only a label, so a caller handing it a walked name opens nothing.
func selfWalking(label, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := os.ReadFile(filepath.Join(dir, e.Name())); err != nil { // want `os.ReadFile opens an entry a directory walk found`
			return fmt.Errorf("%s: %w", label, err)
		}
	}
	return nil
}

func labelsFromAListing(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := selfWalking(e.Name(), "/etc"); err != nil {
			return err
		}
	}
	return nil
}

// readEntry checks the entry it is handed and opens the path handed beside it.
func readEntry(p string, d fs.DirEntry) error {
	if !d.Type().IsRegular() {
		return nil
	}
	_, err := os.ReadFile(p)
	return err
}

func walkedThenReadByACheckingHelper(root string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return readEntry(p, d)
	})
}

// An open with no flag that keeps it from waiting blocks on a named pipe like any other.
func openedWithFlags(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		f, err := os.OpenFile(filepath.Join(dir, e.Name()), os.O_RDONLY|syscall.O_CLOEXEC, 0) // want `os.OpenFile opens an entry a directory walk found`
		if err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	return nil
}

// An open that cannot wait: O_NONBLOCK returns at once from a pipe with no writer, and
// O_DIRECTORY refuses anything that is not a directory before opening it.
func openedWithoutWaiting(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		f, err := os.OpenFile(filepath.Join(dir, e.Name()), os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		d, err := os.OpenFile(filepath.Join(dir, e.Name()), os.O_RDONLY|syscall.O_DIRECTORY, 0)
		if err != nil {
			return err
		}
		if err := d.Close(); err != nil {
			return err
		}
	}
	return nil
}

// regularNames keeps only the names of regular files: the check travels with each name it keeps.
func regularNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		names = append(names, e.Name())
	}
	return names, nil
}

func readsWhatACheckingListerKept(dir string) error {
	names, err := regularNames(dir)
	if err != nil {
		return err
	}
	for _, n := range names {
		if _, err := os.ReadFile(filepath.Join(dir, n)); err != nil {
			return err
		}
	}
	return nil
}

// Names kept in the branch a check holds are names of regular files, read after the loop.
func keptWhereRegularThenRead(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var kept []string
	for _, e := range entries {
		if e.Type().IsRegular() {
			kept = append(kept, filepath.Join(dir, e.Name()))
		}
	}
	for _, p := range kept {
		if _, err := os.ReadFile(p); err != nil {
			return err
		}
	}
	return nil
}

// The clause of a tagless switch runs only when its check holds.
func walkedRegularByCase(root string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir():
			return nil
		case d.Type().IsRegular():
			_, err = os.ReadFile(p)
		}
		return err
	})
}

func skippedUnlessRegular(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		if _, err := os.ReadFile(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func openedOnlyWhenRegular(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") && e.Type().IsRegular() {
			if _, err := os.ReadFile(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func statThenRefused(pattern string) error {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s: %w", m, errNotRegular)
		}
		if _, err := os.ReadFile(m); err != nil {
			return err
		}
	}
	return nil
}

func walkedRegularOnly(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		_, err = os.ReadFile(path)
		return err
	})
}

func walkedWithInfoRegularOnly(root string) error {
	return filepath.Walk(root, func(path string, info fs.FileInfo, err error) error {
		if err != nil || !info.Mode().IsRegular() {
			return err
		}
		_, err = os.ReadFile(path)
		return err
	})
}

func entryInfoChecked(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := readChecked(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// readChecked checks what it is given itself, so a call of it with a walked name is safe.
func readChecked(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s: %w", path, errNotRegular)
	}
	_, err = os.ReadFile(path)
	return err
}

func readByAHelperThatChecks(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := readChecked(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// ParseFile given the source does not open the name.
func parsedFromSource(dir string, src []byte) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if _, err := parser.ParseFile(fset, e.Name(), src, 0); err != nil {
			return err
		}
	}
	return nil
}

func listedOnly(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

func notWalked(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// listing returns walked names, so a call of it is a walk.
func listing(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.yml"))
	if err != nil {
		return nil, err
	}
	return matches, nil
}

func readsWhatAListerReturned(dir string) error {
	names, err := listing(dir)
	if err != nil {
		return err
	}
	for _, n := range names {
		if err := hashOf(n); err != nil { // want `hashOf opens an entry a directory walk found`
			return err
		}
	}
	return nil
}

// A listing taken where a check holds is still a listing: its entries were never checked.
func listedUnderAnotherCheck(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var inner []fs.DirEntry
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		more, err := os.ReadDir(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		inner = append(inner, more...)
	}
	for _, e := range inner {
		if _, err := os.ReadFile(filepath.Join(dir, e.Name())); err != nil { // want `os.ReadFile opens an entry a directory walk found`
			return err
		}
	}
	return nil
}

// hashOf opens what it is given.
func hashOf(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return f.Close()
}

// countOf returns only a number and an error from a listing: neither is a name, so the path it is
// given later is not walked.
func countOf(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

func errorOfAListingIsNoName(dir, config string) error {
	if _, err := countOf(dir); err != nil {
		return err
	}
	_, err := os.ReadFile(config)
	return err
}

// record is a walked name kept with what was learnt of it.
type record struct{ path string }

func records(dir string) ([]record, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var recs []record
	for _, e := range entries {
		recs = append(recs, record{path: filepath.Join(dir, e.Name())})
	}
	return recs, nil
}

// digest opens the record's own path.
func (r *record) digest() error {
	f, err := os.Open(r.path)
	if err != nil {
		return err
	}
	return f.Close()
}

func digestsOfRecords(dir string) error {
	recs, err := records(dir)
	if err != nil {
		return err
	}
	for i := range recs {
		if err := recs[i].digest(); err != nil { // want `digest opens an entry a directory walk found`
			return err
		}
	}
	return nil
}

// fake keeps walked names in one field; its other fields and the fake itself are not walked.
type fake struct {
	files  []string
	config string
}

func (f *fake) load(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		f.files = append(f.files, filepath.Join(dir, e.Name()))
	}
	if _, err := os.ReadFile(f.config); err != nil {
		return err
	}
	for _, p := range f.files {
		if _, err := os.ReadFile(p); err != nil { // want `os.ReadFile opens an entry a directory walk found`
			return err
		}
	}
	return nil
}

// textChecked checks what it is given, then returns its text: what a file says is content.
func textChecked(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s: %w", path, errNotRegular)
	}
	data, err := os.ReadFile(path)
	return string(data), err
}

// A name read out of a walked file is not a name the walk found.
func namesReadFromAListedFile(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		text, err := textChecked(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		for _, name := range strings.Fields(text) {
			if _, err := os.ReadFile(name); err != nil {
				return err
			}
		}
	}
	return nil
}

// The unguarded read is reported once; the name it read is not reported again.
func nameReadFromAnUncheckedEntry(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name())) // want `os.ReadFile opens an entry a directory walk found`
		if err != nil {
			return err
		}
		if _, err := os.ReadFile(strings.TrimSpace(string(data))); err != nil {
			return err
		}
	}
	return nil
}

// Content joined to a walked directory does not launder it: the walked part still counts.
func contentJoinedToAWalkedDirectory(pattern string) error {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	for _, m := range matches {
		text, err := textChecked(m)
		if err != nil {
			return err
		}
		if _, err := os.ReadFile(filepath.Join(filepath.Dir(m), text)); err != nil { // want `os.ReadFile opens an entry a directory walk found`
			return err
		}
	}
	return nil
}

// prefixed returns what a file says, or the name it is handed, or both: the name passes through.
func prefixed(dir, name string) string {
	b, err := os.ReadFile(filepath.Join(dir, ".prefix"))
	if err != nil {
		return name
	}
	return string(b) + name
}

// A helper that returns content does not launder a walked name it also returns.
func nameThroughAContentHelper(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		f, err := os.Open(prefixed(dir, e.Name())) // want `os.Open opens an entry a directory walk found`
		if err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	return nil
}

// O_NONBLOCK does not make a read safe: a named pipe with no writer reads as empty, one whose
// writer is silent still blocks, and a device reads without end.
func readWithoutWaitingUnchecked(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		f, err := os.OpenFile(filepath.Join(dir, e.Name()), os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		_, readErr := io.ReadAll(f) // want `io.ReadAll reads an entry a directory walk found`
		if err := errors.Join(readErr, f.Close()); err != nil {
			return err
		}
	}
	return nil
}

// A read through a method of the handle is a read too.
func readMethodWithoutWaitingUnchecked(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	buf := make([]byte, 16)
	for _, e := range entries {
		f, err := os.OpenFile(filepath.Join(dir, e.Name()), os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := f.Read(buf); err != nil { // want `Read reads an entry a directory walk found`
			return err
		}
	}
	return nil
}

// The open file's own mode, checked before the read, guards it.
func readWithoutWaitingChecked(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		f, err := os.OpenFile(filepath.Join(dir, e.Name()), os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		info, err := f.Stat()
		if err != nil {
			return errors.Join(err, f.Close())
		}
		if !info.Mode().IsRegular() {
			return errors.Join(errNotRegular, f.Close())
		}
		_, readErr := io.ReadAll(f)
		if err := errors.Join(readErr, f.Close()); err != nil {
			return err
		}
	}
	return nil
}

// readOpen checks the open file it is handed before it reads it.
func readOpen(f *os.File) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errNotRegular
	}
	return io.ReadAll(f)
}

// drain reads the open file it is handed, unchecked.
func drain(f *os.File) ([]byte, error) {
	return io.ReadAll(f)
}

func readWithoutWaitingByHelpers(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		f, err := os.OpenFile(filepath.Join(dir, e.Name()), os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := readOpen(f); err != nil {
			return err
		}
		if _, err := drain(f); err != nil { // want `drain reads an entry a directory walk found`
			return err
		}
	}
	return nil
}

// readNamed opens the name O_NONBLOCK and hands the file to a helper declared after it, which
// checks it: so a walked name handed to readNamed is safe.
func readNamed(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	data, err := readOpenDeclaredLater(f)
	return data, errors.Join(err, f.Close())
}

func readNamedFromAListing(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := readNamed(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func readOpenDeclaredLater(f *os.File) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errNotRegular
	}
	return io.ReadAll(f)
}

// Bytes are content wherever they came from: a reader the rule cannot see into (a function value
// here, another package's reader in the tree) still returns no names.
func namesFromAReaderOutOfSight(dir string, read func(string) ([]byte, error)) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		data, err := read(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		if _, err := os.ReadFile(strings.TrimSpace(string(data))); err != nil {
			return err
		}
	}
	return nil
}
