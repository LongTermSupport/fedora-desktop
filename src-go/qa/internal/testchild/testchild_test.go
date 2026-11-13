package testchild

import "testing"

func TestStderrTakesOutTheCoverageLineOnly(t *testing.T) {
	cases := map[string]string{
		"":                                      "",
		"program not built with -cover\n":       "",
		"boom\n":                                "boom\n",
		"program not built with -cover\nboom\n": "boom\n",
		"boom\nprogram not built with -cover\n": "boom\n",
		"program not built with -cover":         "program not built with -cover",
		"xprogram not built with -cover\n":      "xprogram not built with -cover\n",
	}
	for in, want := range cases {
		if got := Stderr(in); got != want {
			t.Errorf("Stderr(%q) = %q, want %q", in, got, want)
		}
	}
}
