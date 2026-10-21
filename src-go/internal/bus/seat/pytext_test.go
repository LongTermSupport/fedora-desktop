package seat

import (
	"context"
	"encoding/json"
	"slices"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestPathTextIsWhatPythonsStderrPrints(t *testing.T) {
	for in, want := range map[string]string{
		"/top/alpha":         "/top/alpha",
		"/top/é":             "/top/é",
		"/top/a\xffb":        `/top/a\udcffb`,
		"/top/\xed\xa0\x80":  `/top/\udced\udca0\udc80`,
		"/top/\xe2\x82":      `/top/\udce2\udc82`,
		"/top/�":             "/top/�",
		"/top/line\nbreak\t": "/top/line\nbreak\t",
	} {
		if got := pathText(in); got != want {
			t.Errorf("pathText(%q) = %q, want %q", in, got, want)
		}
	}
}

// sorted() of names python decoded with surrogateescape: an undecodable byte is a code point
// U+DC80-U+DCFF, which sorts before U+E000 though its byte is greater than 0xEE.
func TestSortNamesIsPythonsOrder(t *testing.T) {
	names := []string{"", "a", "\xff", "Z", "10", "1", "2", "é", "alpha-b", "alpha"}
	sortNames(names)
	want := []string{"1", "10", "2", "Z", "a", "alpha", "alpha-b", "é", "\xff", ""}
	if !slices.Equal(names, want) {
		t.Fatalf("got %q\nwant %q", names, want)
	}
}

func TestStrerrorIsPythons(t *testing.T) {
	res, err := execx.OS{}.Run(context.Background(), execx.Cmd{
		Name: "python3", Args: []string{"-I", "-c", "import json, os; print(json.dumps([os.strerror(n) for n in range(1, 134)]))"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	if err := json.Unmarshal(res.Stdout, &want); err != nil {
		t.Fatal(err)
	}
	for i, w := range want {
		if got := strerror(syscall.Errno(i + 1)); got != w {
			t.Errorf("errno %d: got %q, python %q", i+1, got, w)
		}
	}
}
