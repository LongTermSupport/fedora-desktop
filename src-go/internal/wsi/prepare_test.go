package wsi

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestW14SystemMeansTheLocalesLanguage(t *testing.T) {
	cases := map[string]string{"": "en", "de_DE.UTF-8": "de", "C.UTF-8": "C.UTF-8", "pt_BR": "pt"}
	for lang, want := range cases {
		if got := systemLanguage(lang); got != want {
			t.Errorf("systemLanguage(%q) = %q, want %q", lang, got, want)
		}
	}
}

func TestExitCodeOfIsTheShellsStatus(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{&execx.ExitError{Name: "x", Code: 3}, 3},
		{fmt.Errorf("wrapped: %w", &execx.NotInstalledError{Name: "x"}), 127},
		{fmt.Errorf("run: %w", context.DeadlineExceeded), 124},
		{errors.New("other"), 1},
	}
	for _, c := range cases {
		if got := exitCodeOf(c.err); got != c.want {
			t.Errorf("exitCodeOf(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}
