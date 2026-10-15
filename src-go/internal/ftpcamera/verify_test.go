package ftpcamera

import (
	"bytes"
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func have(names ...string) func(string) bool {
	return func(n string) bool { return slices.Contains(names, n) }
}

func check(t *testing.T, f *execx.Fake, onPath func(string) bool, name string, data []byte) verdict {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	writeFile(t, path, string(data))
	return completeness(context.Background(), f, onPath, path)
}

// E13: empty is truncated whatever the extension; an unknown extension cannot be verified.
func TestCompletenessEmptyAndUnknown(t *testing.T) {
	f := &execx.Fake{}
	if v := check(t, f, have(), "a.txt", nil); v != truncated {
		t.Errorf("empty: %v", v)
	}
	if v := check(t, f, have(), "a.txt", []byte("x")); v != unverifiable {
		t.Errorf("unknown: %v", v)
	}
	if v := completeness(context.Background(), f, have(), filepath.Join(t.TempDir(), "gone.jpg")); v != truncated {
		t.Errorf("missing: %v", v)
	}
}

// E14: SOI first, EOI last or within the last 64 bytes, byte-aligned (B2).
func TestCompletenessJPEG(t *testing.T) {
	f := &execx.Fake{}
	body := bytes.Repeat([]byte{0x11}, 200)
	jpeg := func(tail ...byte) []byte {
		return append(append([]byte{0xFF, 0xD8}, body...), tail...)
	}
	cases := []struct {
		name string
		data []byte
		want verdict
	}{
		{"eoi last", jpeg(0xFF, 0xD9), complete},
		{"eoi then padding", jpeg(append([]byte{0xFF, 0xD9}, bytes.Repeat([]byte{0}, 40)...)...), complete},
		{"eoi beyond the window", jpeg(append([]byte{0xFF, 0xD9}, bytes.Repeat([]byte{0}, 70)...)...), truncated},
		{"no eoi", jpeg(0x00, 0x00), truncated},
		{"nibble-misaligned ffd9 is not an eoi", jpeg(0x0F, 0xFD, 0x90), truncated},
		{"no soi", append([]byte{0x00, 0x00}, 0xFF, 0xD9), truncated},
		{"one byte", []byte{0xFF}, truncated},
	}
	for _, tc := range cases {
		if v := check(t, f, have(), "x.JPG", tc.data); v != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, v, tc.want)
		}
	}
}

func onExif(f *execx.Fake, prefix []string, out string, code int) {
	f.On(append([]string{"exiftool"}, prefix...), execx.Result{Stdout: []byte(out), ExitCode: code})
}

// E15: a strip that ends past the file is proof; E16 the validator's wording.
func TestCompletenessRawAndVideo(t *testing.T) {
	strips := []string{"-s3", "-a", "-FileSize#", "-StripOffsets", "-StripByteCounts"}
	validate := []string{"-validate", "-warning", "-error", "-a", "-s3"}
	cases := []struct {
		name       string
		stripOut   string
		stripCode  int
		validOut   string
		validCode  int
		want       verdict
		noExiftool bool
	}{
		{name: "no exiftool", noExiftool: true, want: unverifiable},
		{name: "strip past the end", stripOut: "1000\n100, 600\n200 500\n", want: truncated},
		{name: "strips inside", stripOut: "1000\n100 600\n200 400\n", validOut: "", want: complete},
		{name: "no strip tags", stripOut: "1000\n", validOut: "Warning: [minor] Odd maker notes\n", want: complete},
		{name: "strip read fails", stripCode: 1, validOut: "Error: File is truncated\n", want: truncated},
		{name: "premature end", stripOut: "1000\n", validOut: "Warning: Premature end of file\n", want: truncated},
		{name: "missing end", stripOut: "1000\n", validOut: "Warning: Missing EOI end of image\n", want: truncated},
		{name: "validator fails", stripOut: "1000\n", validCode: 2, want: unverifiable},
		{name: "blank line before offsets", stripOut: "1000\n\n100\n950\n", want: truncated},
	}
	for _, tc := range cases {
		f := &execx.Fake{}
		onExif(f, strips, tc.stripOut, tc.stripCode)
		onExif(f, validate, tc.validOut, tc.validCode)
		onPath := have("exiftool")
		if tc.noExiftool {
			onPath = have()
		}
		if v := check(t, f, onPath, "DSC0001.arw", []byte("raw")); v != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, v, tc.want)
		}
	}
}

func TestAwkNumber(t *testing.T) {
	for in, want := range map[string]float64{"123": 123, "12abc": 12, "abc": 0, "": 0, "-4": -4, "1.5e3": 1500, ".5": 0.5} {
		if got := awkNumber(in); got != want {
			t.Errorf("awkNumber(%q) = %v, want %v", in, got, want)
		}
	}
}
