package wsi

import (
	"context"
	"testing"
)

func TestW27TheClipboardToolFollowsTheSessionType(t *testing.T) {
	cases := []struct {
		session, selection string
		exact              bool
		line, data         string
	}{
		{"wayland", selectionClipboard, true, "wl-copy", "text"},
		{"wayland", selectionPrimary, false, "wl-copy -p", "text\n"},
		{"x11", selectionClipboard, false, "xsel -ib", "text\n"},
		{"x11", selectionPrimary, true, "xsel -ip", "text"},
		// The Claude failure path copies whatever the session says it is (B14)
		{"unknown", selectionClipboard, true, "wl-copy", "text"},
	}
	for _, c := range cases {
		k := newKit(t)
		r, err := newRun(k.app().Env, options{})
		if err != nil {
			t.Fatal(err)
		}
		r.session = c.session
		if err := r.copyText(context.Background(), c.selection, "text", c.exact); err != nil {
			t.Fatal(err)
		}
		if got, ok := k.copied[c.line]; !ok || got != c.data {
			t.Errorf("%+v: copied %q", c, k.copied)
		}
	}
}
