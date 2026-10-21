package hooks

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

// H9: the failure classes worst first; the worst of several is named; relay is never one.
func TestWorstClass(t *testing.T) {
	cases := []struct {
		found []string
		want  string
	}{
		{nil, ""},
		{[]string{classMissing}, classMissing},
		{[]string{classMissing, classState, classUntrusted}, classState},
		{[]string{classInject, classConfig}, classConfig},
		{[]string{classInternal, classConfig}, classInternal},
		{[]string{"", classUntrusted}, classUntrusted},
	}
	for _, c := range cases {
		if got := worst(c.found...); got != c.want {
			t.Errorf("worst(%v) = %q, want %q", c.found, got, c.want)
		}
	}
}

// Each Python exception class the hooks catch by name is its own Go type, found by errors.As
// through wrapping; an OS error stays its errno.
func TestErrorKinds(t *testing.T) {
	wrapped := errors.Join(errors.New("context"), configErr("team %s: refused", "alpha"))
	if c, ok := errors.AsType[*configError](wrapped); !ok || c.Error() != "team alpha: refused" {
		t.Errorf("config: %v", wrapped)
	}
	if s, ok := errors.AsType[*stateError](stateErr("%s is a symlink", "/x")); !ok || s.Error() != "/x is a symlink" {
		t.Error("state")
	}
	if u, ok := errors.AsType[*untrustedError](untrustedErr("no record")); !ok || u.Error() != "no record" {
		t.Error("untrusted")
	}
	if m, ok := errors.AsType[*membershipError](membershipErr("refused %d", 1)); !ok || m.Error() != "refused 1" {
		t.Error("membership")
	}
	if i, ok := errors.AsType[*internalError](internalErr("TypeError")); !ok || i.Error() != "TypeError" {
		t.Error("internal")
	}
	if strerror(&os.PathError{Op: "open", Path: "/x", Err: syscall.ENOENT}) != "no such file or directory" {
		t.Errorf("strerror: %q", strerror(syscall.ENOENT))
	}
	if strerror(errors.New("plain")) != "plain" {
		t.Error("a non-errno error is its text")
	}
}
