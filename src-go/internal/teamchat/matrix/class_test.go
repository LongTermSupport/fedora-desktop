package matrix

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
)

// Class is an error's fixed failure class, never its text: what a record may carry.
func TestClass(t *testing.T) {
	for want, err := range map[string]error{
		"rate limited":                         &Error{kind: ErrRateLimited, Detail: "member text"},
		"the homeserver did not answer usably": fmt.Errorf("wrapped: %w", &Error{kind: ErrUnreachable}),
		"the homeserver refused":               &Error{},
		"cancelled":                            fmt.Errorf("x: %w", context.Canceled),
		"timed out":                            context.DeadlineExceeded,
		"not a homeserver answer":              errors.New("member text"),
	} {
		if got := Class(err); got != want {
			t.Errorf("Class(%v) = %q, want %q", err, got, want)
		}
	}
}

// StatusError is the error a call answered with status gets, classed as the call itself would
// class it, so a package that judges failures can be tested without a server.
func TestStatusError(t *testing.T) {
	cases := []struct {
		status    int
		kind      error
		transient bool
	}{
		{401, ErrAuthRefused, false},
		{403, ErrForbidden, false},
		{404, ErrNotFound, false},
		{502, ErrRefused, true},
		{302, ErrRefused, false},
		{204, ErrRefused, false},
		{400, ErrRefused, false},
	}
	for _, c := range cases {
		e := StatusError(http.MethodGet, "/x", c.status, "M_X")
		if !errors.Is(e, c.kind) || e.Transient() != c.transient || e.Status != c.status || e.Errcode != "M_X" || e.Method != http.MethodGet || e.Path != "/x" {
			t.Errorf("StatusError(%d) = %+v", c.status, e)
		}
	}
	if e := StatusError(http.MethodGet, "/x", 200, ""); e != nil {
		t.Errorf("a 200 is an error: %v", e)
	}
}
