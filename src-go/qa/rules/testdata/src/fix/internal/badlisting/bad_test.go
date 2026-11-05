package badlisting

import (
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

func byName(a, b Entry) int { return strings.Compare(a.Name, b.Name) }

// The originating test: the directory order of two names, as one machine gave it.
func TestOriginating(t *testing.T) {
	d := Dir{path: t.TempDir()}
	got, err := d.List()
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{{Name: "2"}, {Name: "5"}}
	if !slices.Equal(got, want) { // want `got is a directory listing in directory order`
		t.Errorf("List = %v, want %v", got, want)
	}
}

func TestEveryListingAPI(t *testing.T) {
	d := Dir{path: t.TempDir()}
	names, err := d.Names()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"2", "5"}) { // want `names is a directory listing in directory order`
		t.Errorf("Names = %v", names)
	}
	infos, err := d.Infos()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(infos, infos[:0]) { // want `infos is a directory listing in directory order`
		t.Errorf("Infos = %v", infos)
	}
	wrapped, err := d.Wrapped()
	if err != nil {
		t.Fatal(err)
	}
	if slices.Compare(wrapped, []string{"2"}) != 0 { // want `wrapped is a directory listing in directory order`
		t.Errorf("Wrapped = %v", wrapped)
	}
	f, err := os.Open(d.path)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := Opened(f)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.EqualFunc(opened, opened, func(a, b os.DirEntry) bool { return a.Name() == b.Name() }) { // want `opened is a directory listing in directory order`
		t.Errorf("Opened = %v", opened)
	}
}

// A function that returns only the listing is judged where it is called.
func TestACallAsTheArgument(t *testing.T) {
	d := Dir{path: t.TempDir()}
	if !slices.Equal(d.MustNames(), []string{"2", "5"}) { // want `the result of MustNames is a directory listing in directory order`
		t.Error("MustNames")
	}
}

// The test's own helper that lists the way the file gave it is marked too.
func listNames(t *testing.T, f *os.File) []string { // want listNames:"returns a listing in directory order"
	t.Helper()
	names, err := f.Readdirnames(-1)
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func TestATestHelperAndAFileDirectly(t *testing.T) {
	f, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(listNames(t, f), []string{"a"}) { // want `the result of listNames is a directory listing in directory order`
		t.Error("listNames")
	}
	direct, err := f.Readdirnames(-1)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(direct, []string{"a"}) { // want `direct is a directory listing in directory order`
		t.Error("Readdirnames")
	}
}

// A listing read again after the sort is unsorted again, and a sort of another variable does not
// sort this one.
func TestAReadAfterTheSortAndASortOfAnother(t *testing.T) {
	d := Dir{path: t.TempDir()}
	got, err := d.List()
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(got, byName)
	got, err = d.List()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, nil) { // want `got is a directory listing in directory order`
		t.Error("List again")
	}
	other := []string{"b", "a"}
	names, err := d.Names()
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(other)
	if !slices.Equal(names, other) { // want `names is a directory listing in directory order`
		t.Error("Names")
	}
}

// Sorted before the comparison, through each way of sorting, the listing is a promise.
func TestSortedFirstIsClean(t *testing.T) {
	d := Dir{path: t.TempDir()}
	got, err := d.List()
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(got, byName)
	if !slices.Equal(got, []Entry{{Name: "2"}, {Name: "5"}}) {
		t.Error("List")
	}
	names, err := d.Names()
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, []string{"2", "5"}) {
		t.Error("Names")
	}
	more, err := d.Names()
	if err != nil {
		t.Fatal(err)
	}
	more = slices.Sorted(slices.Values(more))
	if !slices.Equal(more, []string{"2", "5"}) {
		t.Error("Names, Sorted")
	}
}

// A function that sorts, or lists through the sorted standard library, or answers a count or a
// set, returns no directory order.
func TestFunctionsThatPromiseOrNoOrderAreClean(t *testing.T) {
	d := Dir{path: t.TempDir()}
	sorted, err := d.SortedList()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sorted, []Entry{{Name: "2"}}) {
		t.Error("SortedList")
	}
	names, err := d.SortedNames()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"2"}) {
		t.Error("SortedNames")
	}
	std, err := d.Sorted()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(std, []string{"2"}) {
		t.Error("Sorted")
	}
	set, err := d.Set()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(set, map[string]bool{"2": true}) {
		t.Error("Set")
	}
	if n, err := d.Count(); err != nil || n != 1 {
		t.Error("Count")
	}
}

// A conversion, a call through a function value and a call of a builtin as an argument have no
// function to judge, and are not a listing.
func TestArgumentsThatAreNotListingCalls(t *testing.T) {
	names := func() []string { return []string{"2"} }
	if !slices.Equal([]string(nil), names()) {
		t.Error("conversion and function value")
	}
	if !slices.Equal(append([]string(nil), "2"), []string{"2"}) {
		t.Error("builtin")
	}
}

// A listing used for its length, or compared with nothing in order, is not an order asserted.
func TestAListingNotCompared(t *testing.T) {
	d := Dir{path: t.TempDir()}
	got, err := d.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("List = %v", got)
	}
}
